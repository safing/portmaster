package main

import (
	"github.com/safing/portmaster/base/api/client"
	"github.com/safing/portmaster/base/log"
)

func startShutdownEventListener() {
	shutdownNotifOp := apiClient.Sub("query runtime:modules/core/event/shutdown", handleShutdownEvent)
	shutdownNotifOp.EnableResuscitation()

	restartNotifOp := apiClient.Sub("query runtime:modules/core/event/restart", handleRestartEvent)
	restartNotifOp.EnableResuscitation()
}

func handleShutdownEvent(m *client.Message) {
	switch m.Type {
	case client.MsgOk, client.MsgUpdate, client.MsgNew:
		shuttingDown.Set()
		triggerTrayUpdate()

		log.Warningf("shutdown: received shutdown event, shutting down now")

		// wait for the API client connection to die
		<-apiClient.Offline()
		shuttingDown.UnSet()

		cancelMainCtx()

	case client.MsgWarning, client.MsgError:
		log.Errorf("shutdown: event subscription error: %s", string(m.RawValue))
	}
}

func handleRestartEvent(m *client.Message) {
	switch m.Type {
	case client.MsgOk, client.MsgUpdate, client.MsgNew:
		restarting.Set()
		triggerTrayUpdate()

		log.Warningf("restart: received restart event")

		// wait for the API client connection to die
		<-apiClient.Offline()
		restarting.UnSet()
		triggerTrayUpdate()
	case client.MsgWarning, client.MsgError:
		log.Errorf("shutdown: event subscription error: %s", string(m.RawValue))
	}
}
