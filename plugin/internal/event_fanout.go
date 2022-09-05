package internal

import (
	"context"
	"sync"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/plugin/shared/proto"
)

type (
	// EventFanout is used by the plugin system to allow certain internal
	// Portmaster events to be forwarded to plugins.
	EventFanout struct {
		nextSubscriptionId uint64

		m *modules.Module
		l sync.RWMutex

		registerConfigChangeOnce sync.Once
		configChangeListeners    map[uint64]configChangeListener
	}

	configChangeListener struct {
		pluginName string
		keys       []string
		send       chan<- *proto.WatchChangesResponse
	}
)

// NewEventFanout returns a new event fanout that is used to
// distribute certain internal Portmaster events to plugins.
func NewEventFanout(module *modules.Module) *EventFanout {
	return &EventFanout{
		m:                     module,
		configChangeListeners: make(map[uint64]configChangeListener),
	}
}

func (fanout *EventFanout) SubscribeConfigChanges(ctx context.Context, pluginName string, keys []string) <-chan *proto.WatchChangesResponse {
	fanout.setupConfigSubscription()

	fanout.l.Lock()
	defer fanout.l.Unlock()

	subscriptionID := fanout.nextSubscriptionId
	fanout.nextSubscriptionId++

	ch := make(chan *proto.WatchChangesResponse, 10)
	fanout.configChangeListeners[subscriptionID] = configChangeListener{
		pluginName: pluginName,
		send:       ch,
		keys:       keys,
	}

	go func() {
		<-ctx.Done()

		fanout.l.Lock()
		defer fanout.l.Unlock()

		defer close(ch)
		delete(fanout.configChangeListeners, subscriptionID)
	}()

	return ch
}

func (fanout *EventFanout) setupConfigSubscription() {
	fanout.registerConfigChangeOnce.Do(func() {
		fanout.m.RegisterEventHook(
			"config",
			"config change",
			"Broadcast config changes to plugins",
			func(ctx context.Context, i interface{}) error {
				// TODO(ppacher): right now we just always send the value of
				// the keys regardless if they changed or not.

				fanout.m.RLock()
				defer fanout.m.RUnlock()

				for _, listener := range fanout.configChangeListeners {
					for _, key := range listener.keys {
						value, err := GetConfigValueProto(key)
						if err != nil {
							log.Errorf("failed to get configuration option %s for plugin %s: %s", key, listener.pluginName, err)

							continue
						}

						select {
						case listener.send <- &proto.WatchChangesResponse{
							Key:   key,
							Value: value,
						}:
						default:
							log.Errorf("failed to send configuration value for %s to plugin %s", key, listener.pluginName)
						}
					}
				}

				return nil
			},
		)
	})
}
