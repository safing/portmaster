package api

func registerModulesEndpoints() error {
	// TODO(vladimir): do we need this?
	// if err := RegisterEndpoint(Endpoint{
	// 	Path:        "modules/status",
	// 	Read:        PermitUser,
	// 	StructFunc:  getStatusfunc,
	// 	Name:        "Get Module Status",
	// 	Description: "Returns status information of all modules.",
	// }); err != nil {
	// 	return err
	// }

	// TODO(vladimir): do we need this?
	// if err := RegisterEndpoint(Endpoint{
	// 	Path:        "modules/{moduleName:.+}/trigger/{eventName:.+}",
	// 	Write:       PermitSelf,
	// 	ActionFunc:  triggerEvent,
	// 	Name:        "Trigger Event",
	// 	Description: "Triggers an event of an internal module.",
	// }); err != nil {
	// 	return err
	// }

	return nil
}

// func getStatusfunc(ar *Request) (i interface{}, err error) {
// 	status := modules.GetStatus()
// 	if status == nil {
// 		return nil, errors.New("modules not yet initialized")
// 	}
// 	return status, nil
// }

// func triggerEvent(ar *Request) (msg string, err error) {
// 	// Get parameters.
// 	moduleName := ar.URLVars["moduleName"]
// 	eventName := ar.URLVars["eventName"]
// 	if moduleName == "" || eventName == "" {
// 		return "", errors.New("invalid parameters")
// 	}

// 	// Inject event.
// 	if err := module.InjectEvent("api event injection", moduleName, eventName, nil); err != nil {
// 		return "", fmt.Errorf("failed to inject event: %w", err)
// 	}

// 	return "event successfully injected", nil
// }
