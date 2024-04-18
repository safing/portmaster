package account

import (
	"time"

	"golang.org/x/exp/slices"
)

// User, Subscription and Charge states.
const (
	// UserStateNone is only used within Portmaster for saving information for
	// logging into the same device.
	UserStateNone      = ""
	UserStateFresh     = "fresh"
	UserStateQueued    = "queued"
	UserStateApproved  = "approved"
	UserStateSuspended = "suspended"
	UserStateLoggedOut = "loggedout" // Portmaster only.

	SubscriptionStateManual    = "manual"    // Manual renewal.
	SubscriptionStateActive    = "active"    // Automatic renewal.
	SubscriptionStateCancelled = "cancelled" // Automatic, but canceled.

	ChargeStatePending   = "pending"
	ChargeStateCompleted = "completed"
	ChargeStateDead      = "dead"
)

// Agent and Hub return statuses.
const (
	// StatusInvalidAuth [401 Unauthorized] is returned when the credentials are
	// invalid or the user was logged out.
	StatusInvalidAuth = 401
	// StatusNoAccess [403 Forbidden] is returned when the user does not have
	// an active subscription or the subscription does not include the required
	// feature for the request.
	StatusNoAccess = 403
	// StatusInvalidDevice [410 Gone] is returned when the device trying to
	// log into does not exist.
	StatusInvalidDevice = 410
	// StatusReachedDeviceLimit [409 Conflict] is returned when the device limit is reached.
	StatusReachedDeviceLimit = 409
	// StatusDeviceInactive [423 Locked] is returned when the device is locked.
	StatusDeviceInactive = 423
	// StatusNotLoggedIn [412 Precondition] is returned by the Portmaster, if an action required to be logged in, but the user is not logged in.
	StatusNotLoggedIn = 412

	// StatusUnknownError is a special status code that signifies an unknown or
	// unexpected error by the API.
	StatusUnknownError = -1
	// StatusConnectionError is a special status code that signifies a
	// connection error.
	StatusConnectionError = -2
)

// User describes an SPN user account.
type User struct {
	Username     string        `json:"username"`
	State        string        `json:"state"`
	Balance      int           `json:"balance"`
	Device       *Device       `json:"device"`
	Subscription *Subscription `json:"subscription"`
	CurrentPlan  *Plan         `json:"current_plan"`
	NextPlan     *Plan         `json:"next_plan"`
	View         *View         `json:"view"`
}

// MayUseSPN returns whether the user may currently use the SPN.
func (u *User) MayUseSPN() bool {
	return u.MayUse(FeatureSPN)
}

// MayUsePrioritySupport returns whether the user may currently use the priority support.
func (u *User) MayUsePrioritySupport() bool {
	return u.MayUse(FeatureSafingSupport)
}

// MayUse returns whether the user may currently use the feature identified by
// the given feature ID.
// Leave feature ID empty to check without feature.
func (u *User) MayUse(featureID FeatureID) bool {
	switch {
	case u == nil:
		// We need a user, obviously.
	case u.State != UserStateApproved:
		// Only approved users may use the SPN.
	case u.Subscription == nil:
		// Need a subscription.
	case u.Subscription.EndsAt == nil:
	case time.Now().After(*u.Subscription.EndsAt):
		// Subscription needs to be active.
	case u.CurrentPlan == nil:
		// Need a plan / package.
	case featureID != "" &&
		!slices.Contains(u.CurrentPlan.FeatureIDs, featureID):
		// Required feature ID must be in plan / package feature IDs.
	default:
		// All checks passed!
		return true
	}
	return false
}

// Device describes a device of an SPN user.
type Device struct {
	Name string `json:"name"`
	ID   string `json:"id"`
}

// Subscription describes an SPN subscription.
type Subscription struct {
	EndsAt          *time.Time `json:"ends_at"`
	State           string     `json:"state"`
	NextBillingDate *time.Time `json:"next_billing_date"`
	PaymentProvider string     `json:"payment_provider"`
}

// FeatureID defines a feature that requires a plan/subscription.
type FeatureID string

// A list of all supported features.
const (
	FeatureSPN           = FeatureID("spn")
	FeatureSafingSupport = FeatureID("support")
	FeatureHistory       = FeatureID("history")
	FeatureBWVis         = FeatureID("bw-vis")
	FeatureVPNCompat     = FeatureID("vpn-compat")
)

// Plan describes an SPN subscription plan.
type Plan struct {
	Name       string      `json:"name"`
	Amount     int         `json:"amount"`
	Months     int         `json:"months"`
	Renewable  bool        `json:"renewable"`
	FeatureIDs []FeatureID `json:"feature_ids"`
}
