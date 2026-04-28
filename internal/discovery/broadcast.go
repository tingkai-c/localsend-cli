package discovery

import (
	"time"

	"github.com/tingkai-c/localsend-tui/internal/utils/logger"

	"github.com/tingkai-c/localsend-tui/internal/models"
)

const (
	multicastIP   = "224.0.0.167"
	broadcastPort = 53317
	httpTimeout   = 2 * time.Second
	scanInterval  = 2 * time.Second
	deviceTTL     = 200 * time.Second // 设备的生存时间
)

func ListenAndStartBroadcasts(updates chan<- []models.SendModel) {
	logger.Info("Listening for broadcasts...")
	go ListenForUDPBroadcasts(updates)
	go ListenForHttpBroadCast(updates)
	logger.Info("Start broadcasts...")
	go StartUDPBroadcast()
}
