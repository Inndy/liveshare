package server

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"go.inndy.tw/base256"

	"liveshare/protocol"
)

const pingInterval = 30 * time.Second

type wsMsg struct {
	msgType int
	data    []byte
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	token := r.PathValue("token")

	if !s.validToken(token) {
		http.Error(w, "invalid token", http.StatusForbidden)
		return
	}

	if existing := s.Store.GetByToken(token); existing != nil {
		http.Error(w, "token already in use", http.StatusConflict)
		return
	}

	conn, err := s.Upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("websocket upgrade failed", "err", err)
		return
	}

	slog.Info("websocket connected", "token", token)

	_, msgBytes, err := conn.ReadMessage()
	if err != nil {
		slog.Error("read register message failed", "err", err)
		conn.Close()
		return
	}

	var msg protocol.Message
	if err := json.Unmarshal(msgBytes, &msg); err != nil || msg.Type != protocol.MsgRegister {
		slog.Error("invalid register message", "err", err)
		conn.Close()
		return
	}

	var shareID string
	if msg.Persist {
		h := sha256.Sum256([]byte(token + "/" + msg.FileName))
		shareID = base256.Encode(h[:4], "-")

		if existing := s.Store.GetByShareID(shareID); existing != nil && existing.Conn != nil {
			errResp := protocol.Message{Type: protocol.MsgError, Error: "share ID already active"}
			conn.WriteJSON(errResp)
			conn.Close()
			return
		}
	} else {
		idBytes := make([]byte, 4)
		rand.Read(idBytes)
		shareID = base256.Encode(idBytes, "-")
	}

	item := &ShareItem{
		Token:    token,
		ShareID:  shareID,
		FileName: msg.FileName,
		FileSize: msg.FileSize,
		OneTime:  msg.OneTime,
		NoCache:  msg.NoCache,
		Persist:  msg.Persist,
		DirMode:  msg.DirMode,
		MimeType: msg.MimeType,
		Conn:     conn,
		reqCh:    make(chan *FileRequest, 16),
	}
	s.Store.Set(item)

	resp := protocol.Message{Type: protocol.MsgRegistered, ShareID: shareID}
	if err := conn.WriteJSON(resp); err != nil {
		slog.Error("write registered response failed", "err", err)
		s.Store.Delete(item)
		conn.Close()
		return
	}

	slog.Info("file registered", "token", token, "share_id", shareID, "file", msg.FileName, "size", msg.FileSize)

	s.wsLoop(item)
}

func (s *Server) wsLoop(item *ShareItem) {
	conn := item.Conn
	defer func() {
		s.Store.Delete(item)
		conn.Close()
		slog.Info("client disconnected", "token", item.Token, "share_id", item.ShareID)
	}()

	// Periodic ping to keep connection alive through proxies/NAT
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(pingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := item.Conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(10*time.Second)); err != nil {
					return
				}
			case <-done:
				return
			}
		}
	}()

	// Single reader goroutine — all WebSocket reads go through msgCh
	msgCh := make(chan wsMsg, 1)
	go func() {
		defer close(msgCh)
		for {
			mt, data, err := item.Conn.ReadMessage()
			if err != nil {
				return
			}
			msgCh <- wsMsg{mt, data}
		}
	}()

	for {
		select {
		case req, ok := <-item.reqCh:
			if !ok {
				return
			}
			req.Done <- s.processFileRequest(item, req, msgCh)

		case _, ok := <-msgCh:
			if !ok {
				return
			}
		}
	}
}

func (s *Server) processFileRequest(item *ShareItem, req *FileRequest, msgCh <-chan wsMsg) error {
	msg := protocol.Message{
		Type:      protocol.MsgFileRequest,
		RequestID: req.RequestID,
		Offset:    req.Offset,
		FilePath:  req.FilePath,
	}
	if err := item.Conn.WriteJSON(msg); err != nil {
		return err
	}

	shouldCache := !item.OneTime && !item.NoCache && !item.DirMode && !item.CacheDone && req.Offset == 0

	for {
		select {
		case <-req.Ctx.Done():
			drainRequest(msgCh)
			return req.Ctx.Err()
		case m, ok := <-msgCh:
			if !ok {
				return fmt.Errorf("connection closed during transfer")
			}

			if m.msgType == websocket.TextMessage {
				var resp protocol.Message
				if err := json.Unmarshal(m.data, &resp); err != nil {
					return err
				}
				switch resp.Type {
				case protocol.MsgFileHeader:
					if resp.MimeType != "" {
						req.MimeType = resp.MimeType
					}
					continue
				case protocol.MsgFileEnd:
					return nil
				case protocol.MsgError:
					return fmt.Errorf("client error: %s", resp.Error)
				}
			}

			if m.msgType == websocket.BinaryMessage {
				if shouldCache {
					remaining := maxCacheSize - len(item.Cache)
					if remaining > 0 {
						toCache := m.data
						if len(toCache) > remaining {
							toCache = toCache[:remaining]
						}
						item.Cache = append(item.Cache, toCache...)
						if len(item.Cache) >= maxCacheSize {
							item.CacheDone = true
						}
					}
				}

				if _, err := req.Writer.Write(m.data); err != nil {
					drainRequest(msgCh)
					return err
				}
				if f, ok := req.Writer.(http.Flusher); ok {
					f.Flush()
				}
			}
		}
	}
}

func drainRequest(msgCh <-chan wsMsg) {
	for m := range msgCh {
		if m.msgType == websocket.TextMessage {
			var resp protocol.Message
			if err := json.Unmarshal(m.data, &resp); err != nil {
				return
			}
			switch resp.Type {
			case protocol.MsgFileEnd, protocol.MsgError:
				return
			}
		}
	}
}
