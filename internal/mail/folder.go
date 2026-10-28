package mail

import "time"

type Folder struct {
	ID            int64
	AccountID     int64
	Name          string
	Role          FolderRole
	UIDValidity   uint32
	HighestModSeq uint64
	UIDNext       uint32
	NumMessages   uint32
	HiddenSince   int64 // unix seconds; 0 means visible
	MailboxID     string
	LastSyncAt    time.Time
}
