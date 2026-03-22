package client

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"mime"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/websocket"

	"liveshare/protocol"
)

type Client struct {
	Conn         *websocket.Conn
	ServerURL    string
	FilePath     string
	FileName     string
	FileSize     int64
	ShareID      string
	OneTime      bool
	NoCache      bool
	Persist      bool
	DirMode      bool
	DirList      bool
	MimeType     string
	ArchiveMode  string // "", "zip", "tar", "tgz"
	ArchivePaths []string
	dirRoot      *os.Root
}

func New(serverURL, filePath, displayName string, oneTime, noCache, persist bool, mimeType string) (*Client, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("stat file: %w", err)
	}

	conn, _, err := websocket.DefaultDialer.Dial(serverURL, nil)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}

	c := &Client{
		Conn:      conn,
		ServerURL: serverURL,
		FilePath:  filePath,
		FileName:  displayName,
		FileSize:  info.Size(),
		OneTime:   oneTime,
		NoCache:   noCache,
		Persist:   persist,
		MimeType:  mimeType,
	}

	if err := c.register(); err != nil {
		conn.Close()
		return nil, err
	}

	return c, nil
}

func NewArchive(serverURL string, paths []string, displayName, mode string, persist bool, mimeType string) (*Client, error) {
	for _, p := range paths {
		if _, err := os.Stat(p); err != nil {
			return nil, fmt.Errorf("stat %s: %w", p, err)
		}
	}

	conn, _, err := websocket.DefaultDialer.Dial(serverURL, nil)
	if err != nil {
		return nil, fmt.Errorf("dial: %w", err)
	}

	c := &Client{
		Conn:         conn,
		ServerURL:    serverURL,
		FileName:     displayName,
		FileSize:     0,
		NoCache:      true,
		Persist:      persist,
		MimeType:     mimeType,
		ArchiveMode:  mode,
		ArchivePaths: paths,
	}

	if err := c.register(); err != nil {
		conn.Close()
		return nil, err
	}

	return c, nil
}

func NewFolder(serverURL, dirPath, displayName string, dirList, persist bool) (*Client, error) {
	root, err := os.OpenRoot(dirPath)
	if err != nil {
		return nil, fmt.Errorf("open root %s: %w", dirPath, err)
	}

	conn, _, err := websocket.DefaultDialer.Dial(serverURL, nil)
	if err != nil {
		root.Close()
		return nil, fmt.Errorf("dial: %w", err)
	}

	c := &Client{
		Conn:      conn,
		ServerURL: serverURL,
		FilePath:  dirPath,
		FileName:  displayName,
		DirMode:   true,
		DirList:   dirList,
		NoCache:   true,
		Persist:   persist,
		dirRoot:   root,
	}

	if err := c.register(); err != nil {
		conn.Close()
		root.Close()
		return nil, err
	}

	return c, nil
}

func (c *Client) register() error {
	msg := protocol.Message{
		Type:     protocol.MsgRegister,
		Version:  protocol.Version,
		FileName: c.FileName,
		FileSize: c.FileSize,
		OneTime:  c.OneTime,
		NoCache:  c.NoCache,
		Persist:  c.Persist,
		DirMode:  c.DirMode,
		MimeType: c.MimeType,
	}
	if err := c.Conn.WriteJSON(msg); err != nil {
		return fmt.Errorf("send register: %w", err)
	}

	_, data, err := c.Conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read register response: %w", err)
	}

	var resp protocol.Message
	if err := json.Unmarshal(data, &resp); err != nil {
		return fmt.Errorf("parse register response: %w", err)
	}
	if resp.Type == protocol.MsgError {
		return fmt.Errorf("server error: %s", resp.Error)
	}
	if resp.Type != protocol.MsgRegistered {
		return fmt.Errorf("unexpected response type: %s", resp.Type)
	}

	c.ShareID = resp.ShareID
	slog.Info("registered", "file", c.FileName, "size", c.FileSize, "share_id", c.ShareID)
	return nil
}

func (c *Client) Run() error {
	defer c.Conn.Close()
	if c.dirRoot != nil {
		defer c.dirRoot.Close()
	}

	for {
		_, data, err := c.Conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read message: %w", err)
		}

		var msg protocol.Message
		if err := json.Unmarshal(data, &msg); err != nil {
			slog.Error("invalid message", "err", err)
			continue
		}

		switch msg.Type {
		case protocol.MsgFileRequest:
			var handleErr error
			if c.DirMode {
				handleErr = c.handleFolderRequest(msg)
			} else {
				switch c.ArchiveMode {
				case "zip":
					handleErr = c.handleArchiveRequest(msg, c.writeZipStream)
				case "tar":
					handleErr = c.handleArchiveRequest(msg, c.writeTarStream)
				case "tgz":
					handleErr = c.handleArchiveRequest(msg, c.writeTgzStream)
				default:
					handleErr = c.handleFileRequest(msg)
				}
			}
			if err := handleErr; err != nil {
				slog.Error("file request failed", "err", err, "request_id", msg.RequestID)
				errMsg := protocol.Message{
					Type:      protocol.MsgError,
					RequestID: msg.RequestID,
					Error:     err.Error(),
				}
				c.Conn.WriteJSON(errMsg)
			} else if c.OneTime {
				slog.Info("one-time share complete, disconnecting")
				return nil
			}
		default:
			slog.Warn("unexpected message type", "type", msg.Type)
		}
	}
}

func (c *Client) handleFileRequest(msg protocol.Message) error {
	f, err := os.Open(c.FilePath)
	if err != nil {
		return err
	}
	defer f.Close()

	if msg.Offset > 0 {
		if _, err := f.Seek(msg.Offset, io.SeekStart); err != nil {
			return err
		}
	}

	header := protocol.Message{
		Type:      protocol.MsgFileHeader,
		RequestID: msg.RequestID,
		FileName:  c.FileName,
		FileSize:  c.FileSize,
		MimeType:  c.MimeType,
	}
	if err := c.Conn.WriteJSON(header); err != nil {
		return err
	}
	slog.Info("streaming file", "file", c.FileName, "request_id", msg.RequestID)

	buf := make([]byte, 64*1024)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if writeErr := c.Conn.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
				return writeErr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	slog.Info("streaming complete", "file", c.FileName, "request_id", msg.RequestID)
	end := protocol.Message{
		Type:      protocol.MsgFileEnd,
		RequestID: msg.RequestID,
	}
	return c.Conn.WriteJSON(end)
}

func (c *Client) handleArchiveRequest(msg protocol.Message, streamFn func(pw *io.PipeWriter)) error {
	header := protocol.Message{
		Type:      protocol.MsgFileHeader,
		RequestID: msg.RequestID,
		FileName:  c.FileName,
		MimeType:  c.MimeType,
	}
	if err := c.Conn.WriteJSON(header); err != nil {
		return err
	}
	slog.Info("streaming archive", "file", c.FileName, "request_id", msg.RequestID)

	pr, pw := io.Pipe()
	go streamFn(pw)

	buf := make([]byte, 64*1024)
	for {
		n, err := pr.Read(buf)
		if n > 0 {
			if writeErr := c.Conn.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
				pr.Close()
				return writeErr
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}

	slog.Info("streaming complete", "file", c.FileName, "request_id", msg.RequestID)
	end := protocol.Message{
		Type:      protocol.MsgFileEnd,
		RequestID: msg.RequestID,
	}
	return c.Conn.WriteJSON(end)
}

func (c *Client) writeZipStream(pw *io.PipeWriter) {
	zw := zip.NewWriter(pw)
	var err error
	for _, p := range c.ArchivePaths {
		if err = addToZip(zw, p); err != nil {
			break
		}
	}
	zw.Close()
	pw.CloseWithError(err)
}

func (c *Client) writeTarStream(pw *io.PipeWriter) {
	tw := tar.NewWriter(pw)
	var err error
	for _, p := range c.ArchivePaths {
		if err = addToTar(tw, p); err != nil {
			break
		}
	}
	tw.Close()
	pw.CloseWithError(err)
}

func (c *Client) writeTgzStream(pw *io.PipeWriter) {
	gw := gzip.NewWriter(pw)
	tw := tar.NewWriter(gw)
	var err error
	for _, p := range c.ArchivePaths {
		if err = addToTar(tw, p); err != nil {
			break
		}
	}
	tw.Close()
	gw.Close()
	pw.CloseWithError(err)
}

func addToZip(zw *zip.Writer, rootPath string) error {
	info, err := os.Stat(rootPath)
	if err != nil {
		return err
	}

	if !info.IsDir() {
		w, err := zw.Create(filepath.Base(rootPath))
		if err != nil {
			return err
		}
		f, err := os.Open(rootPath)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	}

	base := filepath.Dir(rootPath)
	return filepath.Walk(rootPath, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		w, err := zw.Create(rel)
		if err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		_, err = io.Copy(w, f)
		return err
	})
}

func addToTar(tw *tar.Writer, rootPath string) error {
	info, err := os.Stat(rootPath)
	if err != nil {
		return err
	}

	if !info.IsDir() {
		return tarFile(tw, rootPath, filepath.Base(rootPath))
	}

	base := filepath.Dir(rootPath)
	return filepath.Walk(rootPath, func(path string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return err
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		return tarFile(tw, path, rel)
	})
}

func tarFile(tw *tar.Writer, filePath, name string) error {
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return err
	}

	hdr, err := tar.FileInfoHeader(info, "")
	if err != nil {
		return err
	}
	hdr.Name = name

	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = io.Copy(tw, f)
	return err
}

func (c *Client) handleFolderRequest(msg protocol.Message) error {
	reqPath := msg.FilePath
	if reqPath == "" || reqPath == "/" {
		return c.serveDirPath(msg, ".")
	}

	reqPath = strings.TrimPrefix(reqPath, "/")

	if strings.HasSuffix(reqPath, "/") {
		return c.serveDirPath(msg, reqPath)
	}

	f, err := c.dirRoot.Open(reqPath)
	if err != nil {
		if c.isNotExist(err) {
			return c.sendError(msg, "not found: "+reqPath)
		}
		return c.sendError(msg, err.Error())
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return c.sendError(msg, err.Error())
	}

	if info.IsDir() {
		f.Close()
		return c.serveDirPath(msg, reqPath+"/")
	}

	mimeType := mime.TypeByExtension(filepath.Ext(reqPath))
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	header := protocol.Message{
		Type:      protocol.MsgFileHeader,
		RequestID: msg.RequestID,
		FileName:  filepath.Base(reqPath),
		FileSize:  info.Size(),
		MimeType:  mimeType,
	}
	if err := c.Conn.WriteJSON(header); err != nil {
		return err
	}
	slog.Info("streaming file", "path", reqPath, "request_id", msg.RequestID)

	buf := make([]byte, 64*1024)
	for {
		n, readErr := f.Read(buf)
		if n > 0 {
			if writeErr := c.Conn.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
				return writeErr
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}

	return c.Conn.WriteJSON(protocol.Message{Type: protocol.MsgFileEnd, RequestID: msg.RequestID})
}

func (c *Client) serveDirPath(msg protocol.Message, dirPath string) error {
	trimmed := strings.TrimSuffix(dirPath, "/")
	if trimmed == "" {
		trimmed = "."
	}

	indexPath := trimmed + "/index.html"
	if trimmed == "." {
		indexPath = "index.html"
	}
	if f, err := c.dirRoot.Open(indexPath); err == nil {
		f.Close()
		msg.FilePath = indexPath
		return c.handleFolderRequest(msg)
	}

	if !c.DirList {
		return c.sendError(msg, "not found: "+dirPath)
	}

	return c.sendDirListing(msg, trimmed)
}

func (c *Client) sendDirListing(msg protocol.Message, dirPath string) error {
	f, err := c.dirRoot.Open(dirPath)
	if err != nil {
		return c.sendError(msg, err.Error())
	}
	defer f.Close()

	entries, err := f.ReadDir(-1)
	if err != nil {
		return c.sendError(msg, err.Error())
	}

	var buf strings.Builder
	buf.WriteString("<!DOCTYPE html><html><head><meta charset=\"utf-8\"><title>Index of /")
	buf.WriteString(html.EscapeString(dirPath))
	buf.WriteString("</title></head><body><h1>Index of /")
	buf.WriteString(html.EscapeString(dirPath))
	buf.WriteString("</h1><ul>")

	if dirPath != "." {
		buf.WriteString(`<li><a href="../">../</a></li>`)
	}

	for _, e := range entries {
		name := e.Name()
		displayName := name
		if e.IsDir() {
			displayName += "/"
			name += "/"
		}
		buf.WriteString(`<li><a href="`)
		buf.WriteString(html.EscapeString(name))
		buf.WriteString(`">`)
		buf.WriteString(html.EscapeString(displayName))
		buf.WriteString("</a></li>")
	}
	buf.WriteString("</ul></body></html>")

	body := buf.String()
	header := protocol.Message{
		Type:      protocol.MsgFileHeader,
		RequestID: msg.RequestID,
		FileName:  "index.html",
		FileSize:  int64(len(body)),
		MimeType:  "text/html; charset=utf-8",
	}
	if err := c.Conn.WriteJSON(header); err != nil {
		return err
	}

	if err := c.Conn.WriteMessage(websocket.BinaryMessage, []byte(body)); err != nil {
		return err
	}

	return c.Conn.WriteJSON(protocol.Message{Type: protocol.MsgFileEnd, RequestID: msg.RequestID})
}

func (c *Client) sendError(msg protocol.Message, errText string) error {
	return c.Conn.WriteJSON(protocol.Message{
		Type:      protocol.MsgError,
		RequestID: msg.RequestID,
		Error:     errText,
	})
}

func (c *Client) isNotExist(err error) bool {
	return os.IsNotExist(err)
}

func (c *Client) Reconnect() error {
	conn, _, err := websocket.DefaultDialer.Dial(c.ServerURL, nil)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	c.Conn = conn
	return c.register()
}
