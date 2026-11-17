// Copyright (c) 2026
// SPDX-License-Identifier: GPL-3.0-or-later

// This file preserves the minimal public surface of the former Safing account
// backend so that the remaining SPN packages still compile. The SPN module is
// never started in portmasterpro. Every symbol here is local, side-effect free
// and performs no network, token, account, persistence or telemetry I/O.

package access

import (
	"errors"
	"time"

	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/spn/access/token"
	"github.com/safing/portmaster/spn/terminal"
)

// ErrNotLoggedIn is retained for source compatibility only. No account login
// exists in portmasterpro; callers must not treat it as a recoverable login flow.
var ErrNotLoggedIn = errors.New("account authentication has been removed from portmasterpro")

// ErrAccessRemoved is returned by every former SPN token or authorization path.
var ErrAccessRemoved = errors.New("SPN access tokens have been removed from portmasterpro")

// ExpandAndConnectZones is kept as an empty zone list: no access zones exist.
var ExpandAndConnectZones = []string{}

// UserRecord is a deliberately empty compatibility record. Account data is
// never stored, loaded or transmitted by portmasterpro. All methods are
// nil-safe and report that no account is available.
type UserRecord struct {
	record.Base

	// View is always nil; it only exists for source compatibility.
	View *UserView

	LastNotifiedOfEnd *time.Time
	LoggedInAt        *time.Time
}

// UserView is an empty compatibility type for the removed account view.
type UserView struct {
	Message string
}

// IsLoggedIn always reports false.
func (user *UserRecord) IsLoggedIn() bool { return false }

// MayUse always reports false: there are no account-gated features left.
func (user *UserRecord) MayUse(_ string) bool { return false }

// MayUseSPN always reports false.
func (user *UserRecord) MayUseSPN() bool { return false }

// MayUseTheSPN always reports false.
func (user *UserRecord) MayUseTheSPN() bool { return false }

// MayUsePrioritySupport always reports false.
func (user *UserRecord) MayUsePrioritySupport() bool { return false }

// GetUser always reports that accounts are unavailable.
func GetUser() (*UserRecord, error) { return nil, ErrNotLoggedIn }

// Login never performs authentication or network I/O.
func Login(_, _ string) (user *UserRecord, code int, err error) {
	return nil, 0, ErrNotLoggedIn
}

// Logout is a no-op because nothing is ever logged in.
func Logout(_, _ bool) error { return nil }

// UpdateUser never contacts an account server.
func UpdateUser() (user *UserRecord, statusCode int, err error) {
	return nil, 0, ErrNotLoggedIn
}

// UpdateTokens never requests or stores tokens.
func UpdateTokens() error { return ErrAccessRemoved }

// InitializeZones is a no-op because account-associated SPN zones are removed.
func InitializeZones() error { return nil }

// EnableTestMode is a no-op kept for the SPN test suites.
func EnableTestMode() {}

// ShouldRequest always reports false: tokens are never requested.
func ShouldRequest(_ []string) bool { return false }

// GetTokenAmount always reports zero tokens.
func GetTokenAmount(_ []string) (regular, fallback int) { return 0, 0 }

// GetToken never returns a token.
func GetToken(_ []string) (*token.Token, error) { return nil, ErrAccessRemoved }

// TokenIssuerIsFailing always reports false; there is no token issuer.
func TokenIssuerIsFailing() bool { return false }

// AuthorizeOp is kept for type compatibility with the SPN crew and captain
// packages. It is never started.
type AuthorizeOp struct {
	terminal.OneOffOperationBase
}

// OpTypeAccessCodeAuth is the former type ID of the auth operation. The
// operation type is intentionally not registered with the terminal package.
const OpTypeAccessCodeAuth = "auth"

// Type returns the type ID.
func (op *AuthorizeOp) Type() string { return OpTypeAccessCodeAuth }

// AuthorizeToTerminal always fails locally: no access token can be presented.
func AuthorizeToTerminal(_ terminal.Terminal) (*AuthorizeOp, *terminal.Error) {
	return nil, terminal.ErrPermissionDenied.With("%w", ErrAccessRemoved)
}
