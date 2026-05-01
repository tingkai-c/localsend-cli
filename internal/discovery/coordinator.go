package discovery

import (
	"sync"

	"github.com/tingkai-c/localsend-cli/internal/models"
	"github.com/tingkai-c/localsend-cli/internal/utils/logger"
)

type discoveryStarter struct {
	listenUDP      func(chan<- []models.SendModel)
	listenHTTP     func(chan<- []models.SendModel)
	startBroadcast func()
}

type subscriber struct {
	updates  chan<- []models.SendModel
	onRemove func()
}

type coordinator struct {
	once sync.Once

	incoming chan []models.SendModel
	starter  discoveryStarter

	mu          sync.RWMutex
	nextID      int
	subscribers map[int]subscriber
}

var defaultCoordinator = newCoordinator(discoveryStarter{
	listenUDP: func(updates chan<- []models.SendModel) {
		go ListenForUDPBroadcasts(updates)
	},
	listenHTTP: func(updates chan<- []models.SendModel) {
		go ListenForHttpBroadCast(updates)
	},
	startBroadcast: func() {
		go StartUDPBroadcast()
	},
})

func newCoordinator(starter discoveryStarter) *coordinator {
	return &coordinator{
		incoming:    make(chan []models.SendModel, 16),
		starter:     starter,
		subscribers: make(map[int]subscriber),
	}
}

// Subscribe starts discovery if necessary and returns a channel that receives
// every discovered-device update broadcast by the shared discovery coordinator.
func Subscribe(buffer int) (<-chan []models.SendModel, func()) {
	return defaultCoordinator.Subscribe(buffer)
}

func (c *coordinator) Subscribe(buffer int) (<-chan []models.SendModel, func()) {
	if buffer < 0 {
		buffer = 0
	}

	updates := make(chan []models.SendModel, buffer)
	unsubscribe := c.addSubscriber(updates, func() { close(updates) })
	c.Start()

	return updates, unsubscribe
}

func (c *coordinator) ListenAndStartBroadcasts(updates chan<- []models.SendModel) {
	if updates != nil {
		c.addSubscriber(updates, nil)
	}
	c.Start()
}

func (c *coordinator) Start() {
	c.once.Do(func() {
		logger.Info("Listening for broadcasts...")
		go c.fanOut()
		c.starter.listenUDP(c.incoming)
		c.starter.listenHTTP(c.incoming)
		logger.Info("Start broadcasts...")
		c.starter.startBroadcast()
	})
}

func (c *coordinator) addSubscriber(updates chan<- []models.SendModel, onRemove func()) func() {
	c.mu.Lock()
	id := c.nextID
	c.nextID++
	c.subscribers[id] = subscriber{updates: updates, onRemove: onRemove}
	c.mu.Unlock()

	var once sync.Once
	return func() {
		once.Do(func() {
			c.mu.Lock()
			sub, ok := c.subscribers[id]
			if ok {
				delete(c.subscribers, id)
			}
			c.mu.Unlock()

			if ok && sub.onRemove != nil {
				sub.onRemove()
			}
		})
	}
}

func (c *coordinator) fanOut() {
	for devices := range c.incoming {
		c.mu.RLock()
		for _, sub := range c.subscribers {
			select {
			case sub.updates <- cloneDevices(devices):
			default:
				logger.Debug("Discovery subscriber channel is full, skipping update")
			}
		}
		c.mu.RUnlock()
	}
}

func cloneDevices(devices []models.SendModel) []models.SendModel {
	if len(devices) == 0 {
		return nil
	}
	cloned := make([]models.SendModel, len(devices))
	copy(cloned, devices)
	return cloned
}
