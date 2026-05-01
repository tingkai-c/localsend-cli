package discovery

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/tingkai-c/localsend-cli/internal/models"
)

func TestDiscoveryCoordinatorStartsOnceAndFansOut(t *testing.T) {
	var udpCalls, httpCalls, startCalls int32
	release := make(chan struct{})

	coord := newDiscoveryCoordinator(
		func(updates chan<- []models.SendModel) {
			atomic.AddInt32(&udpCalls, 1)
			<-release
			updates <- []models.SendModel{{IP: "192.168.1.10", DeviceName: "Device A"}}
		},
		func(chan<- []models.SendModel) {
			atomic.AddInt32(&httpCalls, 1)
		},
		func() {
			atomic.AddInt32(&startCalls, 1)
		},
	)

	first := make(chan []models.SendModel, 1)
	second := make(chan []models.SendModel, 1)

	coord.Subscribe(first)
	coord.Subscribe(second)

	waitForInt32(t, &udpCalls, 1)
	waitForInt32(t, &httpCalls, 1)
	waitForInt32(t, &startCalls, 1)

	close(release)

	want := []models.SendModel{{IP: "192.168.1.10", DeviceName: "Device A"}}
	assertDeviceUpdate(t, first, want)
	assertDeviceUpdate(t, second, want)
}

func TestDiscoveryCoordinatorAllowsNilSubscriber(t *testing.T) {
	var udpCalls, httpCalls, startCalls int32

	coord := newDiscoveryCoordinator(
		func(chan<- []models.SendModel) {
			atomic.AddInt32(&udpCalls, 1)
		},
		func(chan<- []models.SendModel) {
			atomic.AddInt32(&httpCalls, 1)
		},
		func() {
			atomic.AddInt32(&startCalls, 1)
		},
	)

	coord.Subscribe(nil)

	waitForInt32(t, &udpCalls, 1)
	waitForInt32(t, &httpCalls, 1)
	waitForInt32(t, &startCalls, 1)
}

func waitForInt32(t *testing.T, v *int32, want int32) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(v) == want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %d, got %d", want, atomic.LoadInt32(v))
}

func assertDeviceUpdate(t *testing.T, ch <-chan []models.SendModel, want []models.SendModel) {
	t.Helper()

	select {
	case got := <-ch:
		if len(got) != len(want) {
			t.Fatalf("unexpected update length: got %d want %d", len(got), len(want))
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("unexpected update at %d: got %+v want %+v", i, got[i], want[i])
			}
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for fan-out update")
	}
}

func TestListenAndStartBroadcastsAlias(t *testing.T) {
	var udpCalls, httpCalls, startCalls int32
	updates := make(chan []models.SendModel, 1)

	previous := defaultDiscoveryCoordinator
	defaultDiscoveryCoordinator = newDiscoveryCoordinator(
		func(chan<- []models.SendModel) {
			atomic.AddInt32(&udpCalls, 1)
		},
		func(chan<- []models.SendModel) {
			atomic.AddInt32(&httpCalls, 1)
		},
		func() {
			atomic.AddInt32(&startCalls, 1)
		},
	)
	defer func() {
		defaultDiscoveryCoordinator = previous
	}()

	ListenAndStartBroadcasts(updates)
	ListenAndStartBroadcasts(nil)

	waitForInt32(t, &udpCalls, 1)
	waitForInt32(t, &httpCalls, 1)
	waitForInt32(t, &startCalls, 1)
}
