package transfer

import (
	"errors"
	"testing"
)

func TestResultStatusAggregation(t *testing.T) {
	peerA := Peer{IP: "192.168.1.10"}
	peerB := Peer{IP: "192.168.1.11"}
	cases := []struct {
		name string
		in   Result
		want Status
	}{
		{"none", Result{}, StatusFailed},
		{"all success", Result{Recipients: []RecipientResult{{Peer: peerA}, {Peer: peerB}}}, StatusCompleted},
		{"mixed", Result{Recipients: []RecipientResult{{Peer: peerA}, {Peer: peerB, Error: errors.New("boom")}}}, StatusPartialSuccess},
		{"all failed", Result{Recipients: []RecipientResult{{Peer: peerA, Error: errors.New("a")}, {Peer: peerB, Error: errors.New("b")}}}, StatusFailed},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.in.Status(); got != tc.want {
				t.Fatalf("Status() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestEventSinkSetsTimestamp(t *testing.T) {
	var got Event
	EventSink(func(event Event) { got = event }).Emit(Event{Kind: EventJobStarted})
	if got.OccurredAt.IsZero() {
		t.Fatalf("Emit should set OccurredAt")
	}
}

func TestRecorderCapturesEvents(t *testing.T) {
	recorder := &Recorder{}
	recorder.Sink().Emit(Event{Kind: EventItemStarted, ItemID: "a.txt"})
	if len(recorder.Events) != 1 || recorder.Events[0].ItemID != "a.txt" {
		t.Fatalf("events = %#v", recorder.Events)
	}
}
