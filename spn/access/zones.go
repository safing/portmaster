package access

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/tevino/abool"

	"github.com/safing/jess/lhash"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/spn/access/token"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/terminal"
)

var (
	// ExpandAndConnectZones are the zones that grant access to the expand and
	// connect operations.
	ExpandAndConnectZones = []string{"pblind1", "alpha2", "fallback1"}

	zonePermissions = map[string]terminal.Permission{
		"pblind1":   terminal.AddPermissions(terminal.MayExpand, terminal.MayConnect),
		"alpha2":    terminal.AddPermissions(terminal.MayExpand, terminal.MayConnect),
		"fallback1": terminal.AddPermissions(terminal.MayExpand, terminal.MayConnect),
	}
	persistentZones = ExpandAndConnectZones

	enableTestMode = abool.New()
)

// EnableTestMode enables the test mode, leading the access module to only
// register a test zone.
// This should not be used to test the access module itself.
func EnableTestMode() {
	enableTestMode.Set()
}

// InitializeZones initialized the permission zones.
// It initializes the test zones, if EnableTestMode was called before.
// Must only be called once.
func InitializeZones() error {
	// Check if we are testing.
	if enableTestMode.IsSet() {
		return initializeTestZone()
	}

	// Special client zone config.
	var requestSignalHandler func(token.Handler)
	if conf.Integrated() {
		requestSignalHandler = shouldRequestTokensHandler
	}

	// Register pblind1 as the first primary zone.
	ph, err := token.NewPBlindHandler(token.PBlindOptions{
		Zone:                "pblind1",
		CurveName:           "P-256",
		PublicKey:           "eXoJXzXbM66UEsM2eVi9HwyBPLMfVnNrC7gNrsfMUJDs",
		UseSerials:          true,
		BatchSize:           1000,
		RandomizeOrder:      true,
		SignalShouldRequest: requestSignalHandler,
	})
	if err != nil {
		return fmt.Errorf("failed to create pblind1 token handler: %w", err)
	}
	err = token.RegisterPBlindHandler(ph)
	if err != nil {
		return fmt.Errorf("failed to register pblind1 token handler: %w", err)
	}

	// Register fallback1 zone as fallback when the issuer is not available.
	sh, err := token.NewScrambleHandler(token.ScrambleOptions{
		Zone:             "fallback1",
		Algorithm:        lhash.BLAKE2b_256,
		InitialVerifiers: []string{"ZwkQoaAttVBMURzeLzNXokFBMAMUUwECfM1iHojcVKBmjk"},
		Fallback:         true,
	})
	if err != nil {
		return fmt.Errorf("failed to create fallback1 token handler: %w", err)
	}
	err = token.RegisterScrambleHandler(sh)
	if err != nil {
		return fmt.Errorf("failed to register fallback1 token handler: %w", err)
	}

	// Register alpha2 zone for transition phase.
	sh, err = token.NewScrambleHandler(token.ScrambleOptions{
		Zone:             "alpha2",
		Algorithm:        lhash.BLAKE2b_256,
		InitialVerifiers: []string{"ZwojEvXZmAv7SZdNe7m94Xzu7F9J8vULqKf7QYtoTpN2tH"},
	})
	if err != nil {
		return fmt.Errorf("failed to create alpha2 token handler: %w", err)
	}
	err = token.RegisterScrambleHandler(sh)
	if err != nil {
		return fmt.Errorf("failed to register alpha2 token handler: %w", err)
	}

	return nil
}

func initializeTestZone() error {
	// Safeguard checks if we should really enable the test zone.
	if !strings.HasSuffix(os.Args[0], ".test") {
		return errors.New("tried to enable test mode, but no test binary was detected")
	}
	if token.RegistrySize() > 0 {
		return fmt.Errorf("tried to enable test zone, but %d handlers are already registered", token.RegistrySize())
	}

	// Reset zones.
	token.ResetRegistry()

	// Set eligible zones.
	ExpandAndConnectZones = []string{"unittest"}
	zonePermissions = map[string]terminal.Permission{
		"unittest": terminal.AddPermissions(terminal.MayExpand, terminal.MayConnect),
	}

	// Register unittest zone as for testing.
	sh, err := token.NewScrambleHandler(token.ScrambleOptions{
		Zone:             "unittest",
		Algorithm:        lhash.BLAKE2b_256,
		InitialTokens:    []string{"6jFqLA93uSLL52utGKrvctG3ZfopSQ8WFqjsRK1c2Svt"},
		InitialVerifiers: []string{"ZwoEoL59sr81s7WnF2vydGzjeejE3u8CqVafig1NTQzUr7"},
	})
	if err != nil {
		return fmt.Errorf("failed to create unittest token handler: %w", err)
	}
	err = token.RegisterScrambleHandler(sh)
	if err != nil {
		return fmt.Errorf("failed to register unittest token handler: %w", err)
	}

	return nil
}

func shouldRequestTokensHandler(_ token.Handler) {
	// Run the account update task as now.
	module.updateAccountWorkerMgr.Go()
}

// GetTokenAmount returns the amount of tokens for the given zones.
func GetTokenAmount(zones []string) (regular, fallback int) {
handlerLoop:
	for _, zone := range zones {
		// Get handler and check if it should be used.
		handler, ok := token.GetHandler(zone)
		if !ok {
			log.Warningf("spn/access: use of non-registered zone %q", zone)
			continue handlerLoop
		}

		if handler.IsFallback() {
			fallback += handler.Amount()
		} else {
			regular += handler.Amount()
		}
	}

	return
}

// ShouldRequest returns whether tokens should be requested for the given zones.
func ShouldRequest(zones []string) (shouldRequest bool) {
handlerLoop:
	for _, zone := range zones {
		// Get handler and check if it should be used.
		handler, ok := token.GetHandler(zone)
		if !ok {
			log.Warningf("spn/access: use of non-registered zone %q", zone)
			continue handlerLoop
		}

		// Go through all handlers every time as this will be the case anyway most
		// of the time and will help us better catch zone misconfiguration.
		if handler.ShouldRequest() {
			shouldRequest = true
		}
	}

	return shouldRequest
}

// GetToken returns a token of one of the given zones.
func GetToken(zones []string) (t *token.Token, err error) {
handlerSelection:
	for _, zone := range zones {
		// Get handler and check if it should be used.
		handler, ok := token.GetHandler(zone)
		switch {
		case !ok:
			log.Warningf("spn/access: use of non-registered zone %q", zone)
			continue handlerSelection
		case handler.IsFallback() && !TokenIssuerIsFailing():
			// Skip fallback zone if everything works.
			continue handlerSelection
		}

		// Get token from handler.
		t, err = token.GetToken(zone)
		if err == nil {
			return t, nil
		}
	}

	// Return existing error, if exists.
	if err != nil {
		return nil, err
	}
	return nil, token.ErrEmpty
}

// VerifyRawToken verifies a raw token.
func VerifyRawToken(data []byte) (granted terminal.Permission, err error) {
	t, err := token.ParseRawToken(data)
	if err != nil {
		return 0, fmt.Errorf("failed to parse token: %w", err)
	}

	return VerifyToken(t)
}

// VerifyToken verifies a token.
func VerifyToken(t *token.Token) (granted terminal.Permission, err error) {
	handler, ok := token.GetHandler(t.Zone)
	if !ok {
		return terminal.NoPermission, token.ErrZoneUnknown
	}

	// Check if the token is a fallback token.
	if handler.IsFallback() && !healthCheck() {
		return terminal.NoPermission, ErrFallbackNotAvailable
	}

	// Verify token.
	err = handler.Verify(t)
	if err != nil {
		return 0, fmt.Errorf("failed to verify token: %w", err)
	}

	// Return permission of zone.
	granted, ok = zonePermissions[t.Zone]
	if !ok {
		return terminal.NoPermission, nil
	}
	return granted, nil
}
