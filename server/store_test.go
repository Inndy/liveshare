package server

import (
	"sync"
	"testing"
)

func TestStore_SetAndGet(t *testing.T) {
	s := NewStore()
	item := &ShareItem{Token: "tok1", ShareID: "sid1"}
	s.Set(item)

	if got := s.GetByShareID("sid1"); got != item {
		t.Fatal("GetByShareID returned wrong item")
	}
}

func TestStore_Delete_NonPersist(t *testing.T) {
	s := NewStore()
	item := &ShareItem{Token: "tok1", ShareID: "sid1", Persist: false}
	s.Set(item)
	s.Delete(item)

	if got := s.GetByShareID("sid1"); got != nil {
		t.Fatal("GetByShareID should return nil after delete")
	}
}

func TestStore_Delete_Persist(t *testing.T) {
	s := NewStore()
	ch := make(chan *FileRequest, 16)
	item := &ShareItem{
		Token:   "tok1",
		ShareID: "sid1",
		Persist: true,
		Conn:    nil, // would be a real conn in practice
		reqCh:   ch,
	}
	s.Set(item)
	s.Delete(item)

	got := s.GetByShareID("sid1")
	if got == nil {
		t.Fatal("GetByShareID should still return item for persist")
	}
	if got.Conn != nil {
		t.Fatal("Conn should be nil after persist delete")
	}
	if got.reqCh != nil {
		t.Fatal("reqCh should be nil after persist delete")
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	s := NewStore()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(3)
		id := string(rune('a' + i%26))
		go func() {
			defer wg.Done()
			s.Set(&ShareItem{Token: "t" + id, ShareID: "s" + id})
		}()
		go func() {
			defer wg.Done()
			s.GetByShareID("s" + id)
		}()
		go func() {
			defer wg.Done()
			item := s.GetByShareID("s" + id)
			if item != nil {
				s.Delete(item)
			}
		}()
	}
	wg.Wait()
}
