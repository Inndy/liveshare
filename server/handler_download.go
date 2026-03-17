package server

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
)

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	shareID := r.PathValue("id")

	item := s.Store.GetByShareID(shareID)
	if item == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, item.FileName))
	if item.FileSize > 0 {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", item.FileSize))
	}
	w.Header().Set("Content-Type", "application/octet-stream")

	var cacheLen int64
	if !item.NoCache {
		cacheLen = int64(len(item.Cache))
		if cacheLen > 0 {
			if _, err := w.Write(item.Cache); err != nil {
				slog.Error("write cache failed", "err", err)
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
		}

		if item.FileSize > 0 && cacheLen >= item.FileSize {
			return
		}
	}

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()

	var reqId [8]byte

	// safe to ignore error since go1.20. read.Read never fail
	rand.Read(reqId[:])
	req := &FileRequest{
		RequestID: fmt.Sprintf("%x", reqId),
		Offset:    cacheLen,
		Writer:    w,
		Done:      make(chan error, 1),
		Ctx:       ctx,
	}

	select {
	case item.reqCh <- req:
	case <-ctx.Done():
		return
	}

	err := <-req.Done
	if err != nil {
		slog.Error("file transfer error", "err", err, "share_id", shareID)
	}
}
