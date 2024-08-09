package broadcasts

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/accessor"
)

func registerAPIEndpoints() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        `broadcasts/matching-data`,
		Read:        api.PermitAdmin,
		StructFunc:  handleMatchingData,
		Name:        "Get Broadcast Notifications Matching Data",
		Description: "Returns the data used by the broadcast notifications to match the instance.",
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        `broadcasts/reset-state`,
		Write:       api.PermitAdmin,
		WriteMethod: http.MethodPost,
		ActionFunc:  handleResetState,
		Name:        "Resets the Broadcast Notification States",
		Description: "Delete the cache of Broadcast Notifications, making them appear again.",
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        `broadcasts/simulate`,
		Write:       api.PermitAdmin,
		WriteMethod: http.MethodPost,
		ActionFunc:  handleSimulate,
		Name:        "Simulate Broadcast Notifications",
		Description: "Test broadcast notifications by sending a valid source file in the body.",
		Parameters: []api.Parameter{
			{
				Method:      http.MethodPost,
				Field:       "state",
				Value:       "true",
				Description: "Check against state when deciding to display a broadcast notification. Acknowledgements are always saved.",
			},
		},
	}); err != nil {
		return err
	}

	return nil
}

func handleMatchingData(ar *api.Request) (i interface{}, err error) {
	return collectData(), nil
}

func handleResetState(ar *api.Request) (msg string, err error) {
	err = db.Delete(broadcastStatesDBKey)
	if err != nil {
		return "", err
	}
	return "Reset complete.", nil
}

func handleSimulate(ar *api.Request) (msg string, err error) {
	// Parse broadcast notification data.
	broadcasts, err := parseBroadcastSource(ar.InputData)
	if err != nil {
		return "", fmt.Errorf("failed to parse broadcast notifications update: %w", err)
	}

	// Get and marshal matching data.
	matchingData := collectData()
	matchingJSON, err := json.Marshal(matchingData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal broadcast notifications matching data: %w", err)
	}
	matchingDataAccessor := accessor.NewJSONBytesAccessor(&matchingJSON)

	var bss *BroadcastStates
	if ar.URL.Query().Get("state") == "true" {
		// Get broadcast notification states.
		bss, err = getBroadcastStates()
		if err != nil {
			if !errors.Is(err, database.ErrNotFound) {
				return "", fmt.Errorf("failed to get broadcast notifications states: %w", err)
			}
			bss = newBroadcastStates()
		}
	}

	// Go through all broadcast nofications and check if they match.
	var results []string
	for _, bn := range broadcasts.Notifications {
		err := handleBroadcast(bn, matchingDataAccessor, bss)
		switch {
		case err == nil:
			results = append(results, fmt.Sprintf("%30s: displayed", bn.id))
		case errors.Is(err, ErrSkip):
			results = append(results, fmt.Sprintf("%30s: %s", bn.id, err))
		default:
			results = append(results, fmt.Sprintf("FAILED %23s: %s", bn.id, err))
		}
	}

	return strings.Join(results, "\n"), nil
}
