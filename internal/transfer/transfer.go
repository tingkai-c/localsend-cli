// Package transfer defines UI-neutral transfer jobs, recipients, events, and
// aggregate results. Protocol adapters emit these events; CLI and TUI adapters
// decide how to render them.
package transfer

import (
	"time"
)

type Peer struct {
	IP          string
	Alias       string
	Fingerprint string
}

type Item struct {
	ID       string
	Name     string
	Path     string
	Size     int64
	FileType string
}

type Job struct {
	ID         string
	Items      []Item
	Recipients []Peer
	StartedAt  time.Time
}

type EventKind string

const (
	EventJobStarted        EventKind = "job_started"
	EventRecipientPrepared EventKind = "recipient_prepared"
	EventItemStarted       EventKind = "item_started"
	EventBytesTransferred  EventKind = "bytes_transferred"
	EventItemCompleted     EventKind = "item_completed"
	EventRecipientComplete EventKind = "recipient_complete"
	EventRecipientFailed   EventKind = "recipient_failed"
	EventCanceled          EventKind = "canceled"
)

type Event struct {
	Kind        EventKind
	JobID       string
	Peer        Peer
	ItemID      string
	Bytes       int64
	TotalBytes  int64
	OccurredAt  time.Time
	Err         error
	Description string
}

type EventSink func(Event)

func (s EventSink) Emit(event Event) {
	if s == nil {
		return
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	s(event)
}

type Status string

const (
	StatusPending        Status = "pending"
	StatusCompleted      Status = "completed"
	StatusPartialSuccess Status = "partial_success"
	StatusFailed         Status = "failed"
	StatusCanceled       Status = "canceled"
)

type RecipientResult struct {
	Peer  Peer
	Items []Item
	Error error
}

func (r RecipientResult) Status() Status {
	if r.Error != nil {
		return StatusFailed
	}
	return StatusCompleted
}

type Result struct {
	JobID      string
	StartedAt  time.Time
	FinishedAt time.Time
	Recipients []RecipientResult
}

func (r Result) Status() Status {
	if len(r.Recipients) == 0 {
		return StatusFailed
	}
	successes := 0
	failures := 0
	for _, recipient := range r.Recipients {
		if recipient.Error != nil {
			failures++
		} else {
			successes++
		}
	}
	switch {
	case successes == len(r.Recipients):
		return StatusCompleted
	case failures == len(r.Recipients):
		return StatusFailed
	default:
		return StatusPartialSuccess
	}
}

func (r Result) SuccessfulRecipients() int {
	count := 0
	for _, recipient := range r.Recipients {
		if recipient.Error == nil {
			count++
		}
	}
	return count
}

// Recorder is a tiny in-memory event consumer useful for tests and for TUI
// adapters that need a reducer-friendly state map.
type Recorder struct {
	Events []Event
}

func (r *Recorder) Sink() EventSink {
	return func(event Event) {
		r.Events = append(r.Events, event)
	}
}
