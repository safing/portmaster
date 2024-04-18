package token

import (
	"crypto/rand"
	"errors"
	"fmt"

	"github.com/mr-tron/base58"
)

const sessionIDSize = 32

// RequestHandlingState is a request handling state.
type RequestHandlingState struct {
	SessionID string
	PBlind    map[string]*PBlindSignerState
}

// SetupRequest is a setup request.
type SetupRequest struct {
	PBlind map[string]struct{} `json:"PB,omitempty"`
}

// SetupResponse is a setup response.
type SetupResponse struct {
	SessionID string                          `json:"ID,omitempty"`
	PBlind    map[string]*PBlindSetupResponse `json:"PB,omitempty"`
}

// TokenRequest is a token request.
type TokenRequest struct { //nolint:golint // Be explicit.
	SessionID string                           `json:"ID,omitempty"`
	PBlind    map[string]*PBlindTokenRequest   `json:"PB,omitempty"`
	Scramble  map[string]*ScrambleTokenRequest `json:"S,omitempty"`
}

// IssuedTokens are issued tokens.
type IssuedTokens struct {
	PBlind   map[string]*IssuedPBlindTokens   `json:"PB,omitempty"`
	Scramble map[string]*IssuedScrambleTokens `json:"SC,omitempty"`
}

// CreateSetupRequest creates a combined setup request for all registered tokens, if needed.
func CreateSetupRequest() (request *SetupRequest, setupRequired bool) {
	registryLock.RLock()
	defer registryLock.RUnlock()

	request = &SetupRequest{
		PBlind: make(map[string]struct{}, len(pblindRegistry)),
	}

	// Go through handlers and create request setups.
	for _, pblindHandler := range pblindRegistry {
		// Check if we need to request with this handler.
		if pblindHandler.ShouldRequest() {
			request.PBlind[pblindHandler.Zone()] = struct{}{}
			setupRequired = true
		}
	}

	return
}

// HandleSetupRequest handles a setup request for all registered tokens.
func HandleSetupRequest(request *SetupRequest) (*RequestHandlingState, *SetupResponse, error) {
	registryLock.RLock()
	defer registryLock.RUnlock()

	// Generate session token.
	randomID := make([]byte, sessionIDSize)
	n, err := rand.Read(randomID)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate session ID: %w", err)
	}
	if n != sessionIDSize {
		return nil, nil, fmt.Errorf("failed to get full session ID: only got %d bytes", n)
	}
	sessionID := base58.Encode(randomID)

	// Create state and response.
	state := &RequestHandlingState{
		SessionID: sessionID,
		PBlind:    make(map[string]*PBlindSignerState, len(pblindRegistry)),
	}
	setup := &SetupResponse{
		SessionID: sessionID,
		PBlind:    make(map[string]*PBlindSetupResponse, len(pblindRegistry)),
	}

	// Go through handlers and create setups.
	for _, pblindHandler := range pblindRegistry {
		// Check if we have a request for this handler.
		_, ok := request.PBlind[pblindHandler.Zone()]
		if !ok {
			continue
		}

		plindState, pblindSetup, err := pblindHandler.CreateSetup()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create setup for %s: %w", pblindHandler.Zone(), err)
		}

		state.PBlind[pblindHandler.Zone()] = plindState
		setup.PBlind[pblindHandler.Zone()] = pblindSetup
	}

	return state, setup, nil
}

// CreateTokenRequest creates a token request for all registered tokens.
func CreateTokenRequest(setup *SetupResponse) (request *TokenRequest, requestRequired bool, err error) {
	registryLock.RLock()
	defer registryLock.RUnlock()

	// Check setup data.
	if setup != nil && setup.SessionID == "" {
		return nil, false, errors.New("setup data is missing a session ID")
	}

	// Create token request.
	request = &TokenRequest{
		PBlind:   make(map[string]*PBlindTokenRequest, len(pblindRegistry)),
		Scramble: make(map[string]*ScrambleTokenRequest, len(scrambleRegistry)),
	}
	if setup != nil {
		request.SessionID = setup.SessionID
	}

	// Go through handlers and create requests.
	if setup != nil {
		for _, pblindHandler := range pblindRegistry {
			// Check if we have setup data for this handler.
			pblindSetup, ok := setup.PBlind[pblindHandler.Zone()]
			if !ok {
				// TODO: Abort if we should have received request data.
				continue
			}

			// Create request.
			pblindRequest, err := pblindHandler.CreateTokenRequest(pblindSetup)
			if err != nil {
				return nil, false, fmt.Errorf("failed to create token request for %s: %w", pblindHandler.Zone(), err)
			}

			requestRequired = true
			request.PBlind[pblindHandler.Zone()] = pblindRequest
		}
	}
	for _, scrambleHandler := range scrambleRegistry {
		// Check if we need to request with this handler.
		if scrambleHandler.ShouldRequest() {
			requestRequired = true
			request.Scramble[scrambleHandler.Zone()] = scrambleHandler.CreateTokenRequest()
		}
	}

	return request, requestRequired, nil
}

// IssueTokens issues tokens for all registered tokens.
func IssueTokens(state *RequestHandlingState, request *TokenRequest) (response *IssuedTokens, err error) {
	registryLock.RLock()
	defer registryLock.RUnlock()

	// Create token response.
	response = &IssuedTokens{
		PBlind:   make(map[string]*IssuedPBlindTokens, len(pblindRegistry)),
		Scramble: make(map[string]*IssuedScrambleTokens, len(scrambleRegistry)),
	}

	// Go through handlers and create requests.
	for _, pblindHandler := range pblindRegistry {
		// Check if we have all the data for issuing.
		pblindState, ok := state.PBlind[pblindHandler.Zone()]
		if !ok {
			continue
		}
		pblindRequest, ok := request.PBlind[pblindHandler.Zone()]
		if !ok {
			continue
		}

		// Issue tokens.
		pblindTokens, err := pblindHandler.IssueTokens(pblindState, pblindRequest)
		if err != nil {
			return nil, fmt.Errorf("failed to issue tokens for %s: %w", pblindHandler.Zone(), err)
		}

		response.PBlind[pblindHandler.Zone()] = pblindTokens
	}
	for _, scrambleHandler := range scrambleRegistry {
		// Check if we have all the data for issuing.
		scrambleRequest, ok := request.Scramble[scrambleHandler.Zone()]
		if !ok {
			continue
		}

		// Issue tokens.
		scrambleTokens, err := scrambleHandler.IssueTokens(scrambleRequest)
		if err != nil {
			return nil, fmt.Errorf("failed to issue tokens for %s: %w", scrambleHandler.Zone(), err)
		}

		response.Scramble[scrambleHandler.Zone()] = scrambleTokens
	}

	return response, nil
}

// ProcessIssuedTokens processes issued tokens for all registered tokens.
func ProcessIssuedTokens(response *IssuedTokens) error {
	registryLock.RLock()
	defer registryLock.RUnlock()

	// Go through handlers and create requests.
	for _, pblindHandler := range pblindRegistry {
		// Check if we received tokens.
		pblindResponse, ok := response.PBlind[pblindHandler.Zone()]
		if !ok {
			continue
		}

		// Process issued tokens.
		err := pblindHandler.ProcessIssuedTokens(pblindResponse)
		if err != nil {
			return fmt.Errorf("failed to process issued tokens for %s: %w", pblindHandler.Zone(), err)
		}
	}
	for _, scrambleHandler := range scrambleRegistry {
		// Check if we received tokens.
		scrambleResponse, ok := response.Scramble[scrambleHandler.Zone()]
		if !ok {
			continue
		}

		// Process issued tokens.
		err := scrambleHandler.ProcessIssuedTokens(scrambleResponse)
		if err != nil {
			return fmt.Errorf("failed to process issued tokens for %s: %w", scrambleHandler.Zone(), err)
		}
	}

	return nil
}
