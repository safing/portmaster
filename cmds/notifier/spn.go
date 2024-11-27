package main

import (
	"sync"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/api/client"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/structures/dsd"
)

const (
	spnModuleKey = "config:spn/enable"
	spnStatusKey = "runtime:spn/status"
)

var (
	spnEnabled = abool.New()

	spnStatusCache     *SPNStatus
	spnStatusCacheLock sync.Mutex
)

// SPNStatus holds SPN status information.
type SPNStatus struct {
	Status             string
	HomeHubID          string
	HomeHubName        string
	ConnectedIP        string
	ConnectedTransport string
	ConnectedSince     *time.Time
}

// GetSPNStatus returns the SPN status.
func GetSPNStatus() *SPNStatus {
	spnStatusCacheLock.Lock()
	defer spnStatusCacheLock.Unlock()

	return spnStatusCache
}

func updateSPNStatus(s *SPNStatus) {
	spnStatusCacheLock.Lock()
	defer spnStatusCacheLock.Unlock()

	spnStatusCache = s
}

func spnStatusClient() {
	moduleQueryOp := apiClient.Qsub(query+spnModuleKey, handleSPNModuleUpdate)
	moduleQueryOp.EnableResuscitation()

	statusQueryOp := apiClient.Qsub(query+spnStatusKey, handleSPNStatusUpdate)
	statusQueryOp.EnableResuscitation()
}

func handleSPNModuleUpdate(m *client.Message) {
	switch m.Type {
	case client.MsgOk, client.MsgUpdate, client.MsgNew:
		var cfg struct {
			Value bool `json:"Value"`
		}
		_, err := dsd.Load(m.RawValue, &cfg)
		if err != nil {
			log.Warningf("config: failed to parse config: %s", err)
			return
		}
		log.Infof("config: received update to SPN module: enabled=%v", cfg.Value)

		spnEnabled.SetTo(cfg.Value)
		triggerTrayUpdate()

	default:
	}
}

func handleSPNStatusUpdate(m *client.Message) {
	switch m.Type {
	case client.MsgOk, client.MsgUpdate, client.MsgNew:
		newStatus := &SPNStatus{}
		_, err := dsd.Load(m.RawValue, newStatus)
		if err != nil {
			log.Warningf("config: failed to parse config: %s", err)
			return
		}
		log.Infof("config: received update to SPN status: %+v", newStatus)

		updateSPNStatus(newStatus)
		triggerTrayUpdate()

	default:
	}
}

func ToggleSPN() {
	var cfg struct {
		Value bool `json:"Value"`
	}
	cfg.Value = !spnEnabled.IsSet()

	apiClient.Update(spnModuleKey, &cfg, nil)
}
