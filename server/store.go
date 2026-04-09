package server

import (
	"context"
	"io"
	"sync"

	"github.com/gorilla/websocket"
)

const defaultMaxCacheSize = 1 << 20 // 1MB

type FileRequest struct {
	RequestID string
	Offset    int64
	FilePath  string
	MimeType  string
	Writer    io.Writer
	Done      chan error
	Ctx       context.Context
}

type ShareItem struct {
	Token     string
	ShareID   string
	FileName  string
	FileSize  int64
	OneTime   bool
	NoCache   bool
	Persist   bool
	DirMode   bool
	MimeType  string
	cacheMu   sync.RWMutex
	Cache     []byte
	CacheDone bool
	Conn      *websocket.Conn
	reqCh     chan *FileRequest
}

type Store struct {
	mu        sync.RWMutex
	byShareID map[string]*ShareItem
}

func NewStore() *Store {
	return &Store{
		byShareID: make(map[string]*ShareItem),
	}
}

func (s *Store) GetByShareID(id string) *ShareItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byShareID[id]
}

func (s *Store) Set(item *ShareItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byShareID[item.ShareID] = item
}

func (s *Store) Delete(item *ShareItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byShareID, item.ShareID)
}
