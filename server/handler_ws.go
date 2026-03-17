package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"

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

	shareID := uuid.NewString()[:8]
	item := &ShareItem{
		Token:    token,
		ShareID:  shareID,
		FileName: msg.FileName,
		FileSize: msg.FileSize,
		OneTime:  msg.OneTime,
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
	defer func() {
		s.Store.Delete(item)
		item.Conn.Close()
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
	}
	if err := item.Conn.WriteJSON(msg); err != nil {
		return err
	}

	for m := range msgCh {
		if m.msgType == websocket.TextMessage {
			var resp protocol.Message
			if err := json.Unmarshal(m.data, &resp); err != nil {
				return err
			}
			switch resp.Type {
			case protocol.MsgFileHeader:
				continue
			case protocol.MsgFileEnd:
				return nil
			case protocol.MsgError:
				return fmt.Errorf("client error: %s", resp.Error)
			}
		}

		if m.msgType == websocket.BinaryMessage {
			if !item.OneTime && !item.CacheDone && req.Offset == 0 {
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
				return err
			}
			if f, ok := req.Writer.(http.Flusher); ok {
				f.Flush()
			}
		}
	}

	return fmt.Errorf("connection closed during transfer")
}
