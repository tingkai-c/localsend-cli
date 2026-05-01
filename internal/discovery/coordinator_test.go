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

	c := newCoordinator(discoveryStarter{
		listenUDP: func(chan<- []models.SendModel) {
			udpStarts.Add(1)
		},
		listenHTTP: func(chan<- []models.SendModel) {
			httpStarts.Add(1)
		},
		startBroadcast: func() {
			broadcastStarts.Add(1)
		},
	})

	first := make(chan []models.SendModel, 1)
	second := make(chan []models.SendModel, 1)

	c.ListenAndStartBroadcasts(first)
	c.ListenAndStartBroadcasts(second)
	_, unsubscribe := c.Subscribe(1)
	unsubscribe()

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
	c := newCoordinator(noopDiscoveryStarter())

	legacy := make(chan []models.SendModel, 1)
	c.ListenAndStartBroadcasts(legacy)

	subscribed, unsubscribe := c.Subscribe(1)
	defer unsubscribe()

	want := []models.SendModel{{IP: "192.0.2.10", DeviceName: "phone"}}
	c.incoming <- want

	assertDeviceUpdate(t, legacy, want)
	assertDeviceUpdate(t, subscribed, want)
}

func TestCoordinatorUnsubscribeStopsFanOut(t *testing.T) {
	c := newCoordinator(noopDiscoveryStarter())

	subscribed, unsubscribe := c.Subscribe(1)
	unsubscribe()

	_, ok := <-subscribed
	if ok {
		t.Fatal("subscribed channel is still open after unsubscribe")
	}

	c.incoming <- []models.SendModel{{IP: "192.0.2.11", DeviceName: "tablet"}}
}

func noopDiscoveryStarter() discoveryStarter {
	return discoveryStarter{
		listenUDP:      func(chan<- []models.SendModel) {},
		listenHTTP:     func(chan<- []models.SendModel) {},
		startBroadcast: func() {},
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
