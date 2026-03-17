package server

import (
	"io"
	"sync"

	"github.com/gorilla/websocket"
)

const maxCacheSize = 1 << 20 // 1MB

type FileRequest struct {
	RequestID string
	Offset    int64
	Writer    io.Writer
	Done      chan error
}

type ShareItem struct {
	Token     string
	ShareID   string
	FileName  string
	FileSize  int64
	OneTime   bool
	Cache     []byte
	CacheDone bool
	Conn      *websocket.Conn
	mu        sync.Mutex
	reqCh     chan *FileRequest
}

type Store struct {
	mu       sync.RWMutex
	byToken  map[string]*ShareItem
	byShareID map[string]*ShareItem
}

func NewStore() *Store {
	return &Store{
		byToken:   make(map[string]*ShareItem),
		byShareID: make(map[string]*ShareItem),
	}
}

func (s *Store) GetByToken(token string) *ShareItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byToken[token]
}

func (s *Store) GetByShareID(id string) *ShareItem {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.byShareID[id]
}

func (s *Store) Set(item *ShareItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byToken[item.Token] = item
	s.byShareID[item.ShareID] = item
}

func (s *Store) Delete(item *ShareItem) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.byToken, item.Token)
	delete(s.byShareID, item.ShareID)
}
