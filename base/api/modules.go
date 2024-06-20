package api

import (
	"time"
)

// ModuleHandler specifies the interface for API endpoints that are bound to a module.
// type ModuleHandler interface {
// 	BelongsTo() *modules.Module
// }

const (
	moduleCheckMaxWait      = 10 * time.Second
	moduleCheckTickDuration = 500 * time.Millisecond
)

// moduleIsReady checks if the given module is online and http requests can be
// sent its way. If the module is not online already, it will wait for a short
// duration for it to come online.
// func moduleIsReady(m *modules.Module) (ok bool) {
// 	// Check if we are given a module.
// 	if m == nil {
// 		// If no module is given, we assume that the handler has not been linked to
// 		// a module, and we can safely continue with the request.
// 		return true
// 	}

// 	// Check if the module is online.
// 	if m.Online() {
// 		return true
// 	}

// 	// Check if the module will come online.
// 	if m.OnlineSoon() {
// 		var i time.Duration
// 		for i = 0; i < moduleCheckMaxWait; i += moduleCheckTickDuration {
// 			// Wait a little.
// 			time.Sleep(moduleCheckTickDuration)
// 			// Check if module is now online.
// 			if m.Online() {
// 				return true
// 			}
// 		}
// 	}

// 	return false
// }
