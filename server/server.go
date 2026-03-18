package server

import (
	"bufio"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"liveshare/web"

	"github.com/gorilla/websocket"
)

type Server struct {
	Store    *Store
	Upgrader websocket.Upgrader

	tokenMu      sync.RWMutex
	tokens       map[string]string // token -> name
	tokenFile    string
	tokenModTime time.Time
}

func New() *Server {
	return &Server{
		Store:  NewStore(),
		tokens: make(map[string]string),
		Upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func (s *Server) LoadTokens(path string) error {
	s.tokenFile = path

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if err := s.readTokenFile(path); err != nil {
		return err
	}
	s.tokenModTime = info.ModTime()
	return nil
}

func (s *Server) readTokenFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	tokens := make(map[string]string)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		token := parts[0]
		name := ""
		if len(parts) > 1 {
			name = parts[1]
		}
		tokens[token] = name
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	s.tokenMu.Lock()
	s.tokens = tokens
	s.tokenMu.Unlock()
	return nil
}

func (s *Server) reloadTokensIfChanged() {
	if s.tokenFile == "" {
		return
	}
	info, err := os.Stat(s.tokenFile)
	if err != nil {
		return
	}
	if !info.ModTime().After(s.tokenModTime) {
		return
	}
	if err := s.readTokenFile(s.tokenFile); err != nil {
		slog.Error("reload tokens failed", "err", err)
		return
	}
	s.tokenModTime = info.ModTime()
	slog.Info("tokens reloaded", "count", len(s.tokens))
}

func (s *Server) validToken(token string) bool {
	s.tokenMu.RLock()
	_, ok := s.tokens[token]
	s.tokenMu.RUnlock()
	if ok {
		return true
	}

	s.reloadTokensIfChanged()

	s.tokenMu.RLock()
	_, ok = s.tokens[token]
	s.tokenMu.RUnlock()
	return ok
}

func (s *Server) TokenCount() int {
	s.tokenMu.RLock()
	defer s.tokenMu.RUnlock()
	return len(s.tokens)
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/{token}", s.handleWS)
	mux.HandleFunc("GET /d/{id}/{path...}", s.handleDownload)
	mux.Handle("GET /", web.Handler())
	return mux
}

func (s *Server) ListenAndServe(addr string) error {
	slog.Info("server starting", "addr", addr)
	return http.ListenAndServe(addr, s.Handler())
}

func (s *Server) Addr(listen string, port int) string {
	return fmt.Sprintf("%s:%d", listen, port)
}
