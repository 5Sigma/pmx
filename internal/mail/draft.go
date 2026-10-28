package mail

import "time"

type Draft struct {
	ID        int64
	AccountID int64
	Subject   string
	To        []Address
	CC        []Address
	BCC       []Address
	BodyText  string
	UpdatedAt time.Time
}

type OutboxItem struct {
	ID        int64
	AccountID int64
	RawEML    []byte
	QueuedAt  time.Time
	Attempts  int
	LastError string
}
