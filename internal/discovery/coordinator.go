package discovery

import (
	"sync"

	"github.com/tingkai-c/localsend-cli/internal/models"
	"github.com/tingkai-c/localsend-cli/internal/utils/logger"
)

type broadcastListener func(chan<- []models.SendModel)

type discoveryCoordinator struct {
	once sync.Once

	updates    chan []models.SendModel
	listenUDP  broadcastListener
	listenHTTP broadcastListener
	startUDP   func()

	mu          sync.RWMutex
	nextID      int
	subscribers map[int]chan<- []models.SendModel
}

func newDiscoveryCoordinator(listenUDP, listenHTTP broadcastListener, startUDP func()) *discoveryCoordinator {
	return &discoveryCoordinator{
		updates:     make(chan []models.SendModel, 16),
		listenUDP:   listenUDP,
		listenHTTP:  listenHTTP,
		startUDP:    startUDP,
		subscribers: make(map[int]chan<- []models.SendModel),
	}
}

var defaultDiscoveryCoordinator = newDiscoveryCoordinator(
	ListenForUDPBroadcasts,
	ListenForHttpBroadCast,
	StartUDPBroadcast,
)

// Subscribe registers a listener for discovery updates and starts the shared
// discovery coordinator. The returned function removes the listener.
func Subscribe(updates chan<- []models.SendModel) func() {
	return defaultDiscoveryCoordinator.Subscribe(updates)
}

func (c *discoveryCoordinator) ListenAndStartBroadcasts(updates chan<- []models.SendModel) func() {
	return c.Subscribe(updates)
}

func (c *discoveryCoordinator) Subscribe(updates chan<- []models.SendModel) func() {
	unsubscribe := func() {}
	if updates != nil {
		unsubscribe = c.addSubscriber(updates)
	}
	c.start()
	return unsubscribe
}

func (c *discoveryCoordinator) start() {
	c.once.Do(func() {
		logger.Info("Listening for broadcasts...")
		go c.dispatch()
		go c.listenUDP(c.updates)
		go c.listenHTTP(c.updates)
		logger.Info("Start broadcasts...")
		go c.startUDP()
	})
}

func (c *discoveryCoordinator) addSubscriber(updates chan<- []models.SendModel) func() {
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	c.subscribers[id] = updates
	c.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			delete(c.subscribers, id)
			c.mu.Unlock()
		})
	}
}

func (c *discoveryCoordinator) dispatch() {
	for devices := range c.updates {
		c.mu.RLock()
		for _, sub := range c.subscribers {
			select {
			case sub <- cloneSendModels(devices):
			default:
				logger.Debug("Discovery subscriber channel is full, skipping update")
			}
		}
		c.mu.RUnlock()
	}
}

func cloneSendModels(devices []models.SendModel) []models.SendModel {
	if len(devices) == 0 {
		return nil
	}
	cloned := make([]models.SendModel, len(devices))
	copy(cloned, devices)
	return cloned
}
