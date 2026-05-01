package discovery

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/tingkai-c/localsend-cli/internal/models"
)

func TestCoordinatorStartsDiscoveryOnlyOnce(t *testing.T) {
	var udpStarts atomic.Int32
	var httpStarts atomic.Int32
	var broadcastStarts atomic.Int32

	c := &discoveryCoordinator{
		subs:       make([]chan<- []models.SendModel, 0),
		updates:    make(chan []models.SendModel),
		listenUDP:  func(chan<- []models.SendModel) { udpStarts.Add(1) },
		listenHTTP: func(chan<- []models.SendModel) { httpStarts.Add(1) },
		startUDP:   func() { broadcastStarts.Add(1) },
	}

	first := make(chan []models.SendModel, 1)
	second := make(chan []models.SendModel, 1)

	c.Subscribe(first)
	c.Subscribe(second)

	waitForStarts := func() {
		deadline := time.Now().Add(200 * time.Millisecond)
		for time.Now().Before(deadline) {
			if udpStarts.Load() == 1 && httpStarts.Load() == 1 && broadcastStarts.Load() == 1 {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	waitForStarts()

	if got := udpStarts.Load(); got != 1 {
		t.Fatalf("udp listener starts = %d, want 1", got)
	}
	if got := httpStarts.Load(); got != 1 {
		t.Fatalf("http listener starts = %d, want 1", got)
	}
	if got := broadcastStarts.Load(); got != 1 {
		t.Fatalf("udp broadcast starts = %d, want 1", got)
	}
}

func TestCoordinatorFansOutDiscoveryUpdates(t *testing.T) {
	c := &discoveryCoordinator{
		subs:    make([]chan<- []models.SendModel, 0),
		updates: make(chan []models.SendModel),
		listenUDP: func(chan<- []models.SendModel) {
		},
		listenHTTP: func(chan<- []models.SendModel) {
		},
		startUDP: func() {
		},
	}

	legacy := make(chan []models.SendModel, 1)
	c.Subscribe(legacy)

	want := []models.SendModel{{IP: "192.0.2.10", DeviceName: "phone"}}
	c.updates <- want

	assertDeviceUpdate(t, legacy, want)
}

func TestCoordinatorUnsubscribeStopsFanOut(t *testing.T) {
	c := &discoveryCoordinator{
		subs:    make([]chan<- []models.SendModel, 0),
		updates: make(chan []models.SendModel),
		listenUDP: func(chan<- []models.SendModel) {
		},
		listenHTTP: func(chan<- []models.SendModel) {
		},
		startUDP: func() {
		},
	}

	sub := make(chan []models.SendModel, 1)
	c.Subscribe(sub)

	c.mu.Lock()
	if len(c.subs) == 0 {
		c.mu.Unlock()
		t.Fatal("subscription did not register")
	}
	c.subs = c.subs[:0]
	c.mu.Unlock()

	select {
	case c.updates <- []models.SendModel{{IP: "192.0.2.11", DeviceName: "tablet"}}:
	default:
	}

	select {
	case got := <-sub:
		t.Fatalf("expected no fanout after unsubscribe, got %+v", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func assertDeviceUpdate(t *testing.T, updates <-chan []models.SendModel, want []models.SendModel) {
	t.Helper()

	select {
	case got := <-updates:
		if len(got) != len(want) {
			t.Fatalf("update len = %d, want %d", len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("update[%d] = %+v, want %+v", i, got[i], want[i])
			}
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for discovery update")
	}
}

func TestListenAndStartBroadcastsAlias(t *testing.T) {
	updates := make(chan []models.SendModel, 1)

	done := make(chan struct{})
	go func() {
		ListenAndStartBroadcasts(updates)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("ListenAndStartBroadcasts did not return")
	}
}
