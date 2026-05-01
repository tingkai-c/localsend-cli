package discovery

import (
	"sync"

	"github.com/tingkai-c/localsend-cli/internal/models"
)

type broadcastListener func(chan<- []models.SendModel)

type discoveryCoordinator struct {
	once      sync.Once
	mu        sync.RWMutex
	subs      []chan<- []models.SendModel
	updates   chan []models.SendModel
	listenUDP broadcastListener
	listenHTTP broadcastListener
	startUDP  func()
}

func newDiscoveryCoordinator(listenUDP, listenHTTP broadcastListener, startUDP func()) *discoveryCoordinator {
	return &discoveryCoordinator{
		listenUDP:  listenUDP,
		listenHTTP:  listenHTTP,
		startUDP:    startUDP,
		subs:        make([]chan<- []models.SendModel, 0),
		updates:     make(chan []models.SendModel),
	}
}

var defaultDiscoveryCoordinator = newDiscoveryCoordinator(
	ListenForUDPBroadcasts,
	ListenForHttpBroadCast,
	StartUDPBroadcast,
)

func Subscribe(updates chan<- []models.SendModel) {
	defaultDiscoveryCoordinator.Subscribe(updates)
}

func (c *discoveryCoordinator) Subscribe(updates chan<- []models.SendModel) {
	c.mu.Lock()
	c.subs = append(c.subs, updates)
	c.mu.Unlock()
	c.start()
}

func (c *discoveryCoordinator) start() {
	c.once.Do(func() {
		go c.dispatch()
		go c.listenUDP(c.updates)
		go c.listenHTTP(c.updates)
		go c.startUDP()
	})
}

func (c *discoveryCoordinator) dispatch() {
	for devices := range c.updates {
		snapshot := append([]models.SendModel(nil), devices...)

		c.mu.RLock()
		subscribers := append([]chan<- []models.SendModel(nil), c.subs...)
		c.mu.RUnlock()

		for _, sub := range subscribers {
			sub <- snapshot
		}
	}
}
