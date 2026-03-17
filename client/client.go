package client

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/gorilla/websocket"

	"liveshare/protocol"
)

type Client struct {
	Conn     *websocket.Conn
	FilePath string
	FileName string
	FileSize int64
	ShareID  string
	OneTime  bool
}

func New(serverURL, filePath, displayName string, oneTime bool) (*Client, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("stat file: %w", err)
	}

	conn, _, err := websocket.DefaultDialer.Dial(serverURL, nil)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}

	c := &Client{
		Conn:     conn,
		FilePath: filePath,
		FileName: displayName,
		FileSize: info.Size(),
		OneTime:  oneTime,
	}

	if err := c.register(); err != nil {
		conn.Close()
		return nil, err
	}

	return c, nil
}

func (c *Client) register() error {
	msg := protocol.Message{
		Type:     protocol.MsgRegister,
		FileName: c.FileName,
		FileSize: c.FileSize,
		OneTime:  c.OneTime,
	}
	if err := c.Conn.WriteJSON(msg); err != nil {
		return fmt.Errorf("send register: %w", err)
	}

	_, data, err := c.Conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read register response: %w", err)
	}

	var resp protocol.Message
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("parse register response: %w", err)
	}
	if resp.Type == protocol.MsgError {
		return fmt.Errorf("server error: %s", resp.Error)
	}
	if resp.Type != protocol.MsgRegistered {
		return fmt.Errorf("unexpected response type: %s", resp.Type)
	}

	c.ShareID = resp.ShareID
	slog.Info("registered", "file", c.FileName, "size", c.FileSize, "share_id", c.ShareID)
	return nil
}

func (c *Client) Run() error {
	defer c.Conn.Close()

	for {
		_, data, err := c.Conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read message: %w", err)
		}

		var msg protocol.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			slog.Error("invalid message", "err", err)
			continue
		}

		switch msg.Type {
		case protocol.MsgFileRequest:
			if err := c.handleFileRequest(msg); err != nil {
				slog.Error("file request failed", "err", err, "request_id", msg.RequestID)
				errMsg := protocol.Message{
					Type:      protocol.MsgError,
					RequestID: msg.RequestID,
					Error:     err.Error(),
				}
				c.Conn.WriteJSON(errMsg)
			} else if c.OneTime {
				slog.Info("one-time share complete, disconnecting")
				return nil
			}
		default:
			slog.Warn("unexpected message type", "type", msg.Type)
		}
	}
}

func (c *Client) handleFileRequest(msg protocol.Message) error {
	f, err := os.Open(c.FilePath)
	if err != nil {
		return err
	}
	defer f.Close()

	if msg.Offset > 0 {
		if _, err := f.Seek(msg.Offset, io.SeekStart); err != nil {
			return err
		}
	}

	header := protocol.Message{
		Type:      protocol.MsgFileHeader,
		RequestID: msg.RequestID,
		FileName:  c.FileName,
		FileSize:  c.FileSize,
	}
	if err := c.Conn.WriteJSON(header); err != nil {
		return err
	}

	buf := make([]byte, 64*1024)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if writeErr := c.Conn.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
				return writeErr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	end := protocol.Message{
		Type:      protocol.MsgFileEnd,
		RequestID: msg.RequestID,
	}
	return c.Conn.WriteJSON(end)
}
