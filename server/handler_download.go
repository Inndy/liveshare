package server

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
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

	cacheLen := int64(len(item.Cache))
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

	reqID := uuid.NewString()
	req := &FileRequest{
		RequestID: reqID,
		Offset:    cacheLen,
		Writer:    w,
		Done:      make(chan error, 1),
	}

	select {
	case item.reqCh <- req:
	case <-r.Context().Done():
		return
	}

	select {
	case err := <-req.Done:
		if err != nil {
			slog.Error("file transfer error", "err", err, "share_id", shareID)
		}
	case <-r.Context().Done():
		slog.Info("download cancelled by client", "share_id", shareID)
	}
}
