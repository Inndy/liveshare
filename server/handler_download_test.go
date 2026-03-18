package server

import (
	"net/http/httptest"
	"testing"
)

func TestSetDownloadHeaders_MimeInline(t *testing.T) {
	w := httptest.NewRecorder()
	item := &ShareItem{FileName: "test.html"}
	setDownloadHeaders(w, item, "text/html", false)

	if got := w.Header().Get("Content-Type"); got != "text/html" {
		t.Fatalf("Content-Type = %q, want text/html", got)
	}
	if got := w.Header().Get("Content-Disposition"); got != "inline" {
		t.Fatalf("Content-Disposition = %q, want inline", got)
	}
}

func TestSetDownloadHeaders_MimeForceDownload(t *testing.T) {
	w := httptest.NewRecorder()
	item := &ShareItem{FileName: "test.html"}
	setDownloadHeaders(w, item, "text/html", true)

	if got := w.Header().Get("Content-Type"); got != "text/html" {
		t.Fatalf("Content-Type = %q, want text/html", got)
	}
	if got := w.Header().Get("Content-Disposition"); got != `attachment; filename="test.html"` {
		t.Fatalf("Content-Disposition = %q, want attachment", got)
	}
}

func TestSetDownloadHeaders_NoMime(t *testing.T) {
	w := httptest.NewRecorder()
	item := &ShareItem{FileName: "data.bin"}
	setDownloadHeaders(w, item, "", false)

	if got := w.Header().Get("Content-Type"); got != "application/octet-stream" {
		t.Fatalf("Content-Type = %q, want application/octet-stream", got)
	}
	if got := w.Header().Get("Content-Disposition"); got != `attachment; filename="data.bin"` {
		t.Fatalf("Content-Disposition = %q, want attachment", got)
	}
}

func TestSetDownloadHeaders_ContentLength(t *testing.T) {
	w := httptest.NewRecorder()
	item := &ShareItem{FileName: "f.bin", FileSize: 1234}
	setDownloadHeaders(w, item, "", false)
	if got := w.Header().Get("Content-Length"); got != "1234" {
		t.Fatalf("Content-Length = %q, want 1234", got)
	}

	w2 := httptest.NewRecorder()
	item2 := &ShareItem{FileName: "f.bin", FileSize: 1234, DirMode: true}
	setDownloadHeaders(w2, item2, "", false)
	if got := w2.Header().Get("Content-Length"); got != "" {
		t.Fatalf("Content-Length should be empty for DirMode, got %q", got)
	}
}

func TestDeferredHeaderWriter_ResolveMimeType(t *testing.T) {
	tests := []struct {
		name     string
		reqMime  string
		itemMime string
		want     string
	}{
		{"req takes priority", "text/css", "text/html", "text/css"},
		{"falls back to item", "", "text/html", "text/html"},
		{"both empty", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dw := &deferredHeaderWriter{
				req:  &FileRequest{MimeType: tt.reqMime},
				item: &ShareItem{MimeType: tt.itemMime},
			}
			if got := dw.resolveMimeType(); got != tt.want {
				t.Fatalf("resolveMimeType() = %q, want %q", got, tt.want)
			}
		})
	}
}
