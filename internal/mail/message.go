package mail

import "time"

type Flag uint8

const (
	FlagSeen Flag = 1 << iota
	FlagFlagged
	FlagAnswered
	FlagDraft
	FlagDeleted
)

type Address struct {
	Name  string
	Email string
}

type Attachment struct {
	ID        int64
	MessageID int64
	Filename  string
	MIMEType  string
	SizeBytes int64
	SHA256    string
	ContentID string // bare Content-ID, angle brackets stripped; empty for non-inline parts
	DiskPath  string // relative to attachments dir
}

type Message struct {
	ID           int64
	FolderID     int64
	UID          uint32
	MessageID    string // RFC 5322 Message-ID
	InReplyTo    string
	References   []string
	ThreadRoot   string // Message-ID of thread root
	Subject      string
	From         Address
	To           []Address
	CC           []Address
	BCC          []Address
	DateSent     time.Time
	DateReceived time.Time
	Flags        Flag
	SizeBytes    int64
	BodyText     string
	BodyHTML     string
	HasAttach    bool
	RawPath      string // optional spilled .eml path
	Attachments  []Attachment
}

func (m *Message) IsRead() bool    { return m.Flags&FlagSeen != 0 }
func (m *Message) IsFlagged() bool { return m.Flags&FlagFlagged != 0 }
