package protocol

const Version = 1

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
	Version   int     `json:"v,omitempty"`
	RequestID string  `json:"request_id,omitempty"`
	ShareID   string  `json:"share_id,omitempty"`
	FileName  string  `json:"file_name,omitempty"`
	FileSize  int64   `json:"file_size,omitempty"`
	Offset    int64   `json:"offset,omitempty"`
	OneTime   bool    `json:"one_time,omitempty"`
	NoCache   bool    `json:"no_cache,omitempty"`
	Persist   bool    `json:"persist,omitempty"`
	DirMode   bool    `json:"dir_mode,omitempty"`
	MimeType  string  `json:"mime_type,omitempty"`
	FilePath  string  `json:"file_path,omitempty"`
	Error     string  `json:"error,omitempty"`
}
