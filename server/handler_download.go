package server

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
)

type deferredHeaderWriter struct {
	http.ResponseWriter
	req         *FileRequest
	item        *ShareItem
	forceDl     bool
	wroteHeader bool
}

func (dw *deferredHeaderWriter) Write(p []byte) (int, error) {
	if !dw.wroteHeader {
		dw.wroteHeader = true
		setDownloadHeaders(dw.ResponseWriter, dw.item, dw.resolveMimeType(), dw.forceDl)
	}
	return dw.ResponseWriter.Write(p)
}

func (dw *deferredHeaderWriter) resolveMimeType() string {
	if dw.req.MimeType != "" {
		return dw.req.MimeType
	}
	return dw.item.MimeType
}

func setDownloadHeaders(w http.ResponseWriter, item *ShareItem, mimeType string, forceDl bool) {
	if mimeType != "" && !forceDl {
		w.Header().Set("Content-Type", mimeType)
		w.Header().Set("Content-Disposition", "inline")
	} else {
		if mimeType != "" {
			w.Header().Set("Content-Type", mimeType)
		} else {
			w.Header().Set("Content-Type", "application/octet-stream")
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, item.FileName))
	}
	if item.FileSize > 0 && !item.DirMode {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", item.FileSize))
	}
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	shareID := r.PathValue("id")
	reqPath := r.PathValue("path")

	item := s.Store.GetByShareID(shareID)
	if item == nil || item.Conn == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	forceDl := r.URL.Query().Get("dl") == "1"

	var cacheLen int64
	if !item.NoCache && !item.DirMode {
		item.cacheMu.RLock()
		cache := item.Cache
		item.cacheMu.RUnlock()
		cacheLen = int64(len(cache))
		if cacheLen > 0 {
			setDownloadHeaders(w, item, item.MimeType, forceDl)
			if _, err := w.Write(cache); err != nil {
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
	rand.Read(reqId[:])
	req := &FileRequest{
		RequestID: fmt.Sprintf("%x", reqId),
		Offset:    cacheLen,
		FilePath:  reqPath,
		Writer:    w,
		Done:      make(chan error, 1),
		Ctx:       ctx,
	}

	dw := &deferredHeaderWriter{
		ResponseWriter: w,
		req:            req,
		item:           item,
		forceDl:        forceDl,
	}
	req.Writer = dw

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
