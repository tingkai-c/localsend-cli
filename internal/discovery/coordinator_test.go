package discovery

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/tingkai-c/localsend-cli/internal/models"
)

func TestDiscoveryCoordinatorStartsOnceAndFansOut(t *testing.T) {
	var udpCalls, httpCalls, startCalls atomic.Int32
	coord := newDiscoveryCoordinator(
		func(chan<- []models.SendModel) { udpCalls.Add(1) },
		func(chan<- []models.SendModel) { httpCalls.Add(1) },
		func() { startCalls.Add(1) },
	)

	first := make(chan []models.SendModel, 1)
	second := make(chan []models.SendModel, 1)

	coord.ListenAndStartBroadcasts(first)
	coord.ListenAndStartBroadcasts(second)
	unsubscribe := coord.Subscribe(make(chan []models.SendModel, 1))
	unsubscribe()
	coord.ListenAndStartBroadcasts(nil)

	waitForAtomicInt32(t, &udpCalls, 1)
	waitForAtomicInt32(t, &httpCalls, 1)
	waitForAtomicInt32(t, &startCalls, 1)

	want := []models.SendModel{{IP: "192.0.2.10", DeviceName: "phone"}}
	coord.updates <- want

	assertDeviceUpdate(t, first, want)
	assertDeviceUpdate(t, second, want)
}

func TestDiscoveryCoordinatorDoesNotBlockOnFullSubscriber(t *testing.T) {
	coord := newDiscoveryCoordinator(noopBroadcastListener, noopBroadcastListener, func() {})

	full := make(chan []models.SendModel, 1)
	full <- []models.SendModel{{IP: "192.0.2.1", DeviceName: "stale"}}
	ready := make(chan []models.SendModel, 1)

	coord.Subscribe(full)
	coord.Subscribe(ready)

	want := []models.SendModel{{IP: "192.0.2.11", DeviceName: "tablet"}}
	coord.updates <- want

	assertDeviceUpdate(t, ready, want)
}

func TestDiscoveryCoordinatorUnsubscribeStopsFanOut(t *testing.T) {
	coord := newDiscoveryCoordinator(noopBroadcastListener, noopBroadcastListener, func() {})
	updates := make(chan []models.SendModel, 1)

	unsubscribe := coord.Subscribe(updates)
	unsubscribe()
	unsubscribe()

	coord.updates <- []models.SendModel{{IP: "192.0.2.12", DeviceName: "laptop"}}

	select {
	case got := <-updates:
		t.Fatalf("received update after unsubscribe: %+v", got)
	case <-time.After(50 * time.Millisecond):
	}
}

func waitForAtomicInt32(t *testing.T, value *atomic.Int32, want int32) {
	t.Helper()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if got := value.Load(); got == want {
			return
		}
		time.Sleep(time.Millisecond)
	}

	t.Fatalf("timed out waiting for %d, got %d", want, value.Load())
}

func noopBroadcastListener(chan<- []models.SendModel) {}

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
	var udpCalls, httpCalls, startCalls atomic.Int32
	updates := make(chan []models.SendModel, 1)

	previous := defaultDiscoveryCoordinator
	defaultDiscoveryCoordinator = newDiscoveryCoordinator(
		func(chan<- []models.SendModel) {
			udpCalls.Add(1)
		},
		func(chan<- []models.SendModel) {
			httpCalls.Add(1)
		},
		func() {
			startCalls.Add(1)
		},
	)
	defer func() {
		defaultDiscoveryCoordinator = previous
	}()

	ListenAndStartBroadcasts(updates)
	ListenAndStartBroadcasts(nil)

	waitForAtomicInt32(t, &udpCalls, 1)
	waitForAtomicInt32(t, &httpCalls, 1)
	waitForAtomicInt32(t, &startCalls, 1)
}
