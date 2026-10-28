package mailsend

import "time"

// Event is the sealed interface implemented by all send progress events.
type Event interface{ isEvent() }

// WorkerStarted is emitted when the send worker begins for an account.
type WorkerStarted struct{ Account string }

// WorkerStopped is emitted when the send worker exits. Err is nil on clean shutdown.
type WorkerStopped struct {
	Account string
	Err     error
}

// WorkerError is emitted on a non-fatal error (e.g. a store read failure that
// will be retried after backoff).
type WorkerError struct {
	Account string
	Err     error
}

// SendStarted is emitted just before transmitting an outbox item.
type SendStarted struct {
	Account string
	ItemID  int64
}

// SendSucceeded is emitted after an item is transmitted and deleted from the outbox.
type SendSucceeded struct {
	Account string
	ItemID  int64
}

// SendFailed is emitted when an item fails transmission (row is kept for retry).
type SendFailed struct {
	Account  string
	ItemID   int64
	Attempts int
	Err      error
}

// QueueDrained is emitted after a drain pass that moved at least one item.
type QueueDrained struct {
	Account string
	At      time.Time
	Sent    int
	Failed  int
}

func (WorkerStarted) isEvent() {}
func (WorkerStopped) isEvent() {}
func (WorkerError) isEvent()   {}
func (SendStarted) isEvent()   {}
func (SendSucceeded) isEvent() {}
func (SendFailed) isEvent()    {}
func (QueueDrained) isEvent()  {}
