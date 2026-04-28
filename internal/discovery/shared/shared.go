package shared

import (
	"sync"

	"github.com/meowrain/localsend-go/internal/config"
	"github.com/meowrain/localsend-go/internal/models"
	"github.com/meowrain/localsend-go/internal/utils"
)

// 全局设备记录哈希表和互斥锁,Message信息

var (
	DiscoveredDevices = make(map[string]models.BroadcastMessage)
	DevicesMutex      sync.RWMutex // 只保留一个互斥锁
)

// https://github.com/localsend/protocol?tab=readme-ov-file#71-device-type
//
// Fingerprint is populated at runtime from the SHA-256 of the TLS certificate
// the server presents — LocalSend clients pin peers by this value, so it must
// match the certificate actually served.
var Message = models.BroadcastMessage{
	Alias:       config.ConfigData.NameOfDevice,
	Version:     "2.0",
	DeviceModel: utils.CheckOSType(),
	DeviceType:  "headless",
	Port:        53317,
	Protocol:    "https",
	Download:    true,
	Announce:    true,
}
