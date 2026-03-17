package protocol

type MsgType string

const (
	MsgRegister    MsgType = "register"
	MsgRegistered  MsgType = "registered"
	MsgFileRequest MsgType = "file_request"
	MsgFileHeader  MsgType = "file_header"
	MsgFileEnd     MsgType = "file_end"
	MsgError       MsgType = "error"
)

type Message struct {
	Type      MsgType `json:"type"`
	RequestID string  `json:"request_id,omitempty"`
	ShareID   string  `json:"share_id,omitempty"`
	FileName  string  `json:"file_name,omitempty"`
	FileSize  int64   `json:"file_size,omitempty"`
	Offset    int64   `json:"offset,omitempty"`
	OneTime   bool    `json:"one_time,omitempty"`
	Error     string  `json:"error,omitempty"`
}
