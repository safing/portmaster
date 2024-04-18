package access

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portbase/config"
	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portmaster/spn/access/account"
	"github.com/safing/portmaster/spn/access/token"
	"github.com/safing/portmaster/spn/conf"
)

var (
	module *modules.Module

	accountUpdateTask *modules.Task

	tokenIssuerIsFailing     = abool.New()
	tokenIssuerRetryDuration = 10 * time.Minute

	// AccountUpdateEvent is fired when the account has changed in any way.
	AccountUpdateEvent = "account update"
)

// Errors.
var (
	ErrDeviceIsLocked       = errors.New("device is locked")
	ErrDeviceLimitReached   = errors.New("device limit reached")
	ErrFallbackNotAvailable = errors.New("fallback tokens not available, token issuer is online")
	ErrInvalidCredentials   = errors.New("invalid credentials")
	ErrMayNotUseSPN         = errors.New("may not use SPN")
	ErrNotLoggedIn          = errors.New("not logged in")
)

func init() {
	module = modules.Register("access", prep, start, stop, "terminal")
}

func prep() error {
	module.RegisterEvent(AccountUpdateEvent, true)

	// Register API handlers.
	if conf.Client() {
		err := registerAPIEndpoints()
		if err != nil {
			return err
		}
	}

	return nil
}

func start() error {
	// Initialize zones.
	if err := InitializeZones(); err != nil {
		return err
	}

	if conf.Client() {
		// Load tokens from database.
		loadTokens()

		// Register new task.
		accountUpdateTask = module.NewTask(
			"update account",
			UpdateAccount,
		).Repeat(24 * time.Hour).Schedule(time.Now().Add(1 * time.Minute))
	}

	return nil
}

func stop() error {
	if conf.Client() {
		// Stop account update task.
		accountUpdateTask.Cancel()
		accountUpdateTask = nil

		// Store tokens to database.
		storeTokens()
	}

	// Reset zones.
	token.ResetRegistry()

	return nil
}

// UpdateAccount updates the user account and fetches new tokens, if needed.
func UpdateAccount(_ context.Context, task *modules.Task) error {
	// Retry sooner if the token issuer is failing.
	defer func() {
		if tokenIssuerIsFailing.IsSet() && task != nil {
			task.Schedule(time.Now().Add(tokenIssuerRetryDuration))
		}
	}()

	// Get current user.
	u, err := GetUser()
	if err == nil {
		// Do not update if we just updated.
		if time.Since(time.Unix(u.Meta().Modified, 0)) < 2*time.Minute {
			return nil
		}
	}

	u, _, err = UpdateUser()
	if err != nil {
		return fmt.Errorf("failed to update user profile: %w", err)
	}

	err = UpdateTokens()
	if err != nil {
		return fmt.Errorf("failed to get tokens: %w", err)
	}

	// Schedule next check.
	switch {
	case u == nil: // No user.
	case u.Subscription == nil: // No subscription.
	case u.Subscription.EndsAt == nil: // Subscription not active

	case time.Until(*u.Subscription.EndsAt) < 24*time.Hour &&
		time.Since(*u.Subscription.EndsAt) < 24*time.Hour:
		// Update account every hour 24h hours before and after the subscription ends.
		task.Schedule(time.Now().Add(time.Hour))

	case u.Subscription.NextBillingDate == nil: // No auto-subscription.

	case time.Until(*u.Subscription.NextBillingDate) < 24*time.Hour &&
		time.Since(*u.Subscription.NextBillingDate) < 24*time.Hour:
		// Update account every hour 24h hours before and after the next billing date.
		task.Schedule(time.Now().Add(time.Hour))
	}

	return nil
}

func enableSPN() {
	err := config.SetConfigOption("spn/enable", true)
	if err != nil {
		log.Warningf("spn/access: failed to enable the SPN during login: %s", err)
	}
}

func disableSPN() {
	err := config.SetConfigOption("spn/enable", false)
	if err != nil {
		log.Warningf("spn/access: failed to disable the SPN during logout: %s", err)
	}
}

// TokenIssuerIsFailing returns whether token issuing is currently failing.
func TokenIssuerIsFailing() bool {
	return tokenIssuerIsFailing.IsSet()
}

func tokenIssuerFailed() {
	if !tokenIssuerIsFailing.SetToIf(false, true) {
		return
	}
	if !module.Online() {
		return
	}

	accountUpdateTask.Schedule(time.Now().Add(tokenIssuerRetryDuration))
}

// IsLoggedIn returns whether a User is currently logged in.
func (user *UserRecord) IsLoggedIn() bool {
	user.Lock()
	defer user.Unlock()

	switch user.State {
	case account.UserStateNone, account.UserStateLoggedOut:
		return false
	default:
		return true
	}
}

// MayUseTheSPN returns whether the currently logged in User may use the SPN.
func (user *UserRecord) MayUseTheSPN() bool {
	user.Lock()
	defer user.Unlock()

	return user.User.MayUseSPN()
}
