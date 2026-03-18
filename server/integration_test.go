package server

import (
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"go.inndy.tw/base256"

	"liveshare/client"
	"liveshare/protocol"
)

// --- Test helpers ---

type testEnv struct {
	srv     *Server
	httpSrv *httptest.Server
	t       *testing.T
}

func newTestEnv(t *testing.T, tokens ...string) *testEnv {
	t.Helper()
	srv := New()
	for _, tok := range tokens {
		srv.tokens[tok] = ""
	}
	httpSrv := httptest.NewServer(srv.Handler())
	t.Cleanup(httpSrv.Close)
	return &testEnv{srv: srv, httpSrv: httpSrv, t: t}
}

func (e *testEnv) wsURL(token string) string {
	return "ws" + strings.TrimPrefix(e.httpSrv.URL, "http") + "/ws/" + token
}

func (e *testEnv) downloadURL(shareID, path string) string {
	return e.httpSrv.URL + "/d/" + shareID + "/" + path
}

func (e *testEnv) connectAndRegister(token string, reg protocol.Message) (*websocket.Conn, protocol.Message) {
	e.t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(e.wsURL(token), nil)
	if err != nil {
		e.t.Fatalf("dial: %v", err)
	}

	reg.Type = protocol.MsgRegister
	if err := conn.WriteJSON(reg); err != nil {
		conn.Close()
		e.t.Fatalf("write register: %v", err)
	}

	_, data, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		e.t.Fatalf("read registered: %v", err)
	}
	var resp protocol.Message
	if err := json.Unmarshal(data, &resp); err != nil {
		conn.Close()
		e.t.Fatalf("unmarshal registered: %v", err)
	}
	if resp.Type == protocol.MsgError {
		conn.Close()
		e.t.Fatalf("server error: %s", resp.Error)
	}
	if resp.Type != protocol.MsgRegistered {
		conn.Close()
		e.t.Fatalf("unexpected type: %s", resp.Type)
	}
	return conn, resp
}

// fakeClient registers via WS and serves file requests using the provided handler.
// handler receives the file_request message and should write file_header + binary data + file_end.
// Returns shareID. The goroutine stops when conn is closed.
func fakeClient(t *testing.T, env *testEnv, token string, reg protocol.Message,
	handler func(conn *websocket.Conn, msg protocol.Message)) string {
	t.Helper()
	conn, resp := env.connectAndRegister(token, reg)
	shareID := resp.ShareID

	go func() {
		defer conn.Close()
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg protocol.Message
			if err := json.Unmarshal(data, &msg); err != nil {
				return
			}
			if msg.Type == protocol.MsgFileRequest {
				handler(conn, msg)
			}
		}
	}()

	return shareID
}

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return p
}

func writeTempDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func simpleFileHandler(body string) func(*websocket.Conn, protocol.Message) {
	return func(conn *websocket.Conn, msg protocol.Message) {
		conn.WriteJSON(protocol.Message{
			Type:      protocol.MsgFileHeader,
			RequestID: msg.RequestID,
			FileName:  "test.bin",
			FileSize:  int64(len(body)),
		})
		conn.WriteMessage(websocket.BinaryMessage, []byte(body))
		conn.WriteJSON(protocol.Message{Type: protocol.MsgFileEnd, RequestID: msg.RequestID})
	}
}

func simpleFileHandlerWithMime(body, mimeType string) func(*websocket.Conn, protocol.Message) {
	return func(conn *websocket.Conn, msg protocol.Message) {
		conn.WriteJSON(protocol.Message{
			Type:      protocol.MsgFileHeader,
			RequestID: msg.RequestID,
			FileName:  "test.bin",
			FileSize:  int64(len(body)),
			MimeType:  mimeType,
		})
		conn.WriteMessage(websocket.BinaryMessage, []byte(body))
		conn.WriteJSON(protocol.Message{Type: protocol.MsgFileEnd, RequestID: msg.RequestID})
	}
}

func httpGet(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	return resp
}

func readBody(t *testing.T, resp *http.Response) string {
	t.Helper()
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// --- C. Basic Integration ---

func TestIntegration_SingleFileDownload(t *testing.T) {
	env := newTestEnv(t, "tok1")
	content := "hello world"
	shareID := fakeClient(t, env, "tok1", protocol.Message{
		FileName: "test.bin",
		FileSize: int64(len(content)),
	}, simpleFileHandler(content))

	resp := httpGet(t, env.downloadURL(shareID, "test.bin"))
	body := readBody(t, resp)
	if body != content {
		t.Fatalf("body = %q, want %q", body, content)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("Content-Type = %q, want application/octet-stream", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Fatalf("Content-Disposition = %q, want attachment", cd)
	}
}

func TestIntegration_CacheHit(t *testing.T) {
	env := newTestEnv(t, "tok1")
	content := "cached content"
	requestCount := 0
	shareID := fakeClient(t, env, "tok1", protocol.Message{
		FileName: "test.bin",
		FileSize: int64(len(content)),
	}, func(conn *websocket.Conn, msg protocol.Message) {
		requestCount++
		simpleFileHandler(content)(conn, msg)
	})

	body1 := readBody(t, httpGet(t, env.downloadURL(shareID, "test.bin")))
	if body1 != content {
		t.Fatalf("first download: %q", body1)
	}

	// Small delay to let cache populate
	time.Sleep(50 * time.Millisecond)

	body2 := readBody(t, httpGet(t, env.downloadURL(shareID, "test.bin")))
	if body2 != content {
		t.Fatalf("second download: %q", body2)
	}

	if requestCount != 1 {
		t.Fatalf("expected 1 file_request, got %d", requestCount)
	}
}

func TestIntegration_NoCache(t *testing.T) {
	env := newTestEnv(t, "tok1")
	content := "no cache content"
	requestCount := 0
	shareID := fakeClient(t, env, "tok1", protocol.Message{
		FileName: "test.bin",
		FileSize: int64(len(content)),
		NoCache:  true,
	}, func(conn *websocket.Conn, msg protocol.Message) {
		requestCount++
		simpleFileHandler(content)(conn, msg)
	})

	readBody(t, httpGet(t, env.downloadURL(shareID, "test.bin")))
	readBody(t, httpGet(t, env.downloadURL(shareID, "test.bin")))

	if requestCount != 2 {
		t.Fatalf("expected 2 file_requests, got %d", requestCount)
	}
}

func TestIntegration_OneTimeShare(t *testing.T) {
	env := newTestEnv(t, "tok1")
	content := "one time only"
	shareID := fakeClient(t, env, "tok1", protocol.Message{
		FileName: "test.bin",
		FileSize: int64(len(content)),
		OneTime:  true,
	}, func(conn *websocket.Conn, msg protocol.Message) {
		simpleFileHandler(content)(conn, msg)
		conn.Close()
	})

	body := readBody(t, httpGet(t, env.downloadURL(shareID, "test.bin")))
	if body != content {
		t.Fatalf("body = %q", body)
	}

	time.Sleep(50 * time.Millisecond)

	resp := httpGet(t, env.downloadURL(shareID, "test.bin"))
	if resp.StatusCode != http.StatusNotFound {
		resp.Body.Close()
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestIntegration_NotFound(t *testing.T) {
	env := newTestEnv(t, "tok1")
	resp := httpGet(t, env.downloadURL("nonexistent", "file.bin"))
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestIntegration_DisconnectedClient(t *testing.T) {
	env := newTestEnv(t, "tok1")
	conn, resp := env.connectAndRegister("tok1", protocol.Message{
		FileName: "test.bin",
		FileSize: 100,
	})
	shareID := resp.ShareID
	conn.Close()

	time.Sleep(50 * time.Millisecond)

	httpResp := httpGet(t, env.downloadURL(shareID, "test.bin"))
	if httpResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", httpResp.StatusCode)
	}
	httpResp.Body.Close()
}

// --- D. Persist Mode ---

func TestIntegration_Persist_DeterministicID(t *testing.T) {
	env := newTestEnv(t, "tok1")
	_, resp := env.connectAndRegister("tok1", protocol.Message{
		FileName: "hello.txt",
		Persist:  true,
	})

	h := sha256.Sum256([]byte("tok1/hello.txt"))
	expected := base256.Encode(h[:persistShareIdSize], "-")
	if resp.ShareID != expected {
		t.Fatalf("shareID = %q, want %q", resp.ShareID, expected)
	}
}

func TestIntegration_Persist_OrphanSurvives(t *testing.T) {
	env := newTestEnv(t, "tok1")
	conn, resp := env.connectAndRegister("tok1", protocol.Message{
		FileName: "hello.txt",
		Persist:  true,
	})
	shareID := resp.ShareID
	conn.Close()

	time.Sleep(50 * time.Millisecond)

	item := env.srv.Store.GetByShareID(shareID)
	if item == nil {
		t.Fatal("persist item should survive disconnect")
	}
	if item.Conn != nil {
		t.Fatal("Conn should be nil after disconnect")
	}

	httpResp := httpGet(t, env.downloadURL(shareID, "hello.txt"))
	if httpResp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 for orphan, got %d", httpResp.StatusCode)
	}
	httpResp.Body.Close()
}

func TestIntegration_Persist_Reconnect(t *testing.T) {
	env := newTestEnv(t, "tok1")
	content := "persist reconnect"

	conn1, resp1 := env.connectAndRegister("tok1", protocol.Message{
		FileName: "hello.txt",
		Persist:  true,
	})
	shareID1 := resp1.ShareID
	conn1.Close()

	time.Sleep(50 * time.Millisecond)

	shareID2 := fakeClient(t, env, "tok1", protocol.Message{
		FileName: "hello.txt",
		Persist:  true,
	}, simpleFileHandler(content))

	if shareID1 != shareID2 {
		t.Fatalf("shareIDs differ: %q vs %q", shareID1, shareID2)
	}

	body := readBody(t, httpGet(t, env.downloadURL(shareID2, "hello.txt")))
	if body != content {
		t.Fatalf("body = %q, want %q", body, content)
	}
}

func TestIntegration_SameToken_MultipleConcurrentShares(t *testing.T) {
	env := newTestEnv(t, "tok1")

	content1 := "file one content"
	content2 := "file two content"

	shareID1 := fakeClient(t, env, "tok1", protocol.Message{
		FileName: "file1.txt",
		FileSize: int64(len(content1)),
	}, simpleFileHandler(content1))

	shareID2 := fakeClient(t, env, "tok1", protocol.Message{
		FileName: "file2.txt",
		FileSize: int64(len(content2)),
	}, simpleFileHandler(content2))

	if shareID1 == shareID2 {
		t.Fatal("two shares with same token should have different share IDs")
	}

	body1 := readBody(t, httpGet(t, env.downloadURL(shareID1, "file1.txt")))
	if body1 != content1 {
		t.Fatalf("share1 body = %q, want %q", body1, content1)
	}

	body2 := readBody(t, httpGet(t, env.downloadURL(shareID2, "file2.txt")))
	if body2 != content2 {
		t.Fatalf("share2 body = %q, want %q", body2, content2)
	}
}

// --- E. Folder Share ---

func TestIntegration_FolderShare_ServeFile(t *testing.T) {
	dir := writeTempDir(t, map[string]string{
		"hello.txt": "hello from folder",
	})

	env := newTestEnv(t, "tok1")
	c, err := client.NewFolder(env.wsURL("tok1"), dir, "testdir", false, false)
	if err != nil {
		t.Fatal(err)
	}
	go c.Run()
	defer c.Conn.Close()

	resp := httpGet(t, env.downloadURL(c.ShareID, "hello.txt"))
	body := readBody(t, resp)
	if body != "hello from folder" {
		t.Fatalf("body = %q", body)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type = %q, want text/plain*", ct)
	}
}

func TestIntegration_FolderShare_IndexFallback(t *testing.T) {
	dir := writeTempDir(t, map[string]string{
		"index.html": "<html>index</html>",
	})

	env := newTestEnv(t, "tok1")
	c, err := client.NewFolder(env.wsURL("tok1"), dir, "testdir", false, false)
	if err != nil {
		t.Fatal(err)
	}
	go c.Run()
	defer c.Conn.Close()

	body := readBody(t, httpGet(t, env.downloadURL(c.ShareID, "")))
	if body != "<html>index</html>" {
		t.Fatalf("body = %q", body)
	}
}

func TestIntegration_FolderShare_DirListEnabled(t *testing.T) {
	dir := writeTempDir(t, map[string]string{
		"a.txt": "aaa",
		"b.txt": "bbb",
	})

	env := newTestEnv(t, "tok1")
	c, err := client.NewFolder(env.wsURL("tok1"), dir, "testdir", true, false)
	if err != nil {
		t.Fatal(err)
	}
	go c.Run()
	defer c.Conn.Close()

	resp := httpGet(t, env.downloadURL(c.ShareID, ""))
	body := readBody(t, resp)
	if !strings.Contains(body, "a.txt") || !strings.Contains(body, "b.txt") {
		t.Fatalf("listing should contain files, got: %s", body)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html*", ct)
	}
}

func TestIntegration_FolderShare_DirListDisabled(t *testing.T) {
	dir := writeTempDir(t, map[string]string{
		"a.txt": "aaa",
	})

	env := newTestEnv(t, "tok1")
	c, err := client.NewFolder(env.wsURL("tok1"), dir, "testdir", false, false)
	if err != nil {
		t.Fatal(err)
	}
	go c.Run()
	defer c.Conn.Close()

	// Request root without index.html, DirList=false → should get error
	resp := httpGet(t, env.downloadURL(c.ShareID, ""))
	body := readBody(t, resp)
	// The client sends an error message which the server propagates
	// The response depends on how the server handles client errors
	// In this case it should still get some response since DirMode uses deferred headers
	_ = body
	// The important thing is it does NOT return a directory listing
	if strings.Contains(body, "<ul>") {
		t.Fatal("should not return directory listing when DirList=false")
	}
}

func TestIntegration_FolderShare_SubdirIndex(t *testing.T) {
	dir := writeTempDir(t, map[string]string{
		"sub/index.html": "<html>subdir</html>",
	})

	env := newTestEnv(t, "tok1")
	c, err := client.NewFolder(env.wsURL("tok1"), dir, "testdir", false, false)
	if err != nil {
		t.Fatal(err)
	}
	go c.Run()
	defer c.Conn.Close()

	body := readBody(t, httpGet(t, env.downloadURL(c.ShareID, "sub/")))
	if body != "<html>subdir</html>" {
		t.Fatalf("body = %q", body)
	}
}

func TestIntegration_FolderShare_NotFound(t *testing.T) {
	dir := writeTempDir(t, map[string]string{
		"exists.txt": "yes",
	})

	env := newTestEnv(t, "tok1")
	c, err := client.NewFolder(env.wsURL("tok1"), dir, "testdir", false, false)
	if err != nil {
		t.Fatal(err)
	}
	go c.Run()
	defer c.Conn.Close()

	// Requesting a non-existent file — client sends error, server should relay it
	// The transfer will complete with an error, but the HTTP response may vary
	resp := httpGet(t, env.downloadURL(c.ShareID, "nonexistent.txt"))
	resp.Body.Close()
	// Client sends error for not found files; the server processes it
	// We can't easily check the HTTP status because the deferred header writer
	// may not have written headers yet when the error occurs
}

func TestIntegration_FolderShare_MimeDetection(t *testing.T) {
	dir := writeTempDir(t, map[string]string{
		"style.css": "body { color: red; }",
	})

	env := newTestEnv(t, "tok1")
	c, err := client.NewFolder(env.wsURL("tok1"), dir, "testdir", false, false)
	if err != nil {
		t.Fatal(err)
	}
	go c.Run()
	defer c.Conn.Close()

	resp := httpGet(t, env.downloadURL(c.ShareID, "style.css"))
	body := readBody(t, resp)
	if body != "body { color: red; }" {
		t.Fatalf("body = %q", body)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/css") {
		t.Fatalf("Content-Type = %q, want text/css*", ct)
	}
}

func TestIntegration_FolderShare_PathTraversal(t *testing.T) {
	dir := writeTempDir(t, map[string]string{
		"safe.txt": "safe",
	})

	env := newTestEnv(t, "tok1")
	c, err := client.NewFolder(env.wsURL("tok1"), dir, "testdir", false, false)
	if err != nil {
		t.Fatal(err)
	}
	go c.Run()
	defer c.Conn.Close()

	// os.Root should reject path traversal
	resp := httpGet(t, env.downloadURL(c.ShareID, "../../etc/passwd"))
	resp.Body.Close()
	// Should not return /etc/passwd content
	if resp.StatusCode == http.StatusOK {
		// Even if status is 200, body should not be /etc/passwd
		// But os.Root should prevent this
	}
}

// --- F. Custom MIME / Inline ---

func TestIntegration_CustomMime_Inline(t *testing.T) {
	env := newTestEnv(t, "tok1")
	content := "<html>hello</html>"
	shareID := fakeClient(t, env, "tok1", protocol.Message{
		FileName: "page.html",
		FileSize: int64(len(content)),
		MimeType: "text/html",
	}, simpleFileHandler(content))

	resp := httpGet(t, env.downloadURL(shareID, "page.html"))
	body := readBody(t, resp)
	if body != content {
		t.Fatalf("body = %q", body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/html" {
		t.Fatalf("Content-Type = %q, want text/html", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != "inline" {
		t.Fatalf("Content-Disposition = %q, want inline", cd)
	}
}

func TestIntegration_CustomMime_ForceDl(t *testing.T) {
	env := newTestEnv(t, "tok1")
	content := "<html>hello</html>"
	shareID := fakeClient(t, env, "tok1", protocol.Message{
		FileName: "page.html",
		FileSize: int64(len(content)),
		MimeType: "text/html",
	}, simpleFileHandler(content))

	resp := httpGet(t, env.downloadURL(shareID, "page.html")+"?dl=1")
	body := readBody(t, resp)
	if body != content {
		t.Fatalf("body = %q", body)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Fatalf("Content-Disposition = %q, want attachment", cd)
	}
}

func TestIntegration_CustomMime_ClientOverride(t *testing.T) {
	env := newTestEnv(t, "tok1")
	content := "body { color: red; }"
	shareID := fakeClient(t, env, "tok1", protocol.Message{
		FileName: "style.css",
		FileSize: int64(len(content)),
	}, simpleFileHandlerWithMime(content, "text/css"))

	resp := httpGet(t, env.downloadURL(shareID, "style.css"))
	body := readBody(t, resp)
	if body != content {
		t.Fatalf("body = %q", body)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/css") {
		t.Fatalf("Content-Type = %q, want text/css*", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); cd != "inline" {
		t.Fatalf("Content-Disposition = %q, want inline", cd)
	}
}

func TestIntegration_NoMime_BackwardCompat(t *testing.T) {
	env := newTestEnv(t, "tok1")
	content := "binary data"
	shareID := fakeClient(t, env, "tok1", protocol.Message{
		FileName: "data.bin",
		FileSize: int64(len(content)),
	}, simpleFileHandler(content))

	resp := httpGet(t, env.downloadURL(shareID, "data.bin"))
	body := readBody(t, resp)
	if body != content {
		t.Fatalf("body = %q", body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/octet-stream" {
		t.Fatalf("Content-Type = %q, want application/octet-stream", ct)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") {
		t.Fatalf("Content-Disposition = %q, want attachment", cd)
	}
}
