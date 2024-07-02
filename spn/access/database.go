package access

import (
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/spn/access/account"
)

const (
	userRecordKey           = "core:spn/account/user"
	authTokenRecordKey      = "core:spn/account/authtoken" //nolint:gosec // Not a credential.
	tokenStorageKeyTemplate = "core:spn/account/tokens/%s" //nolint:gosec // Not a credential.
)

var db = database.NewInterface(&database.Options{
	Local:    true,
	Internal: true,
})

// UserRecord holds a SPN user account.
type UserRecord struct {
	record.Base
	sync.Mutex

	*account.User

	LastNotifiedOfEnd *time.Time
	LoggedInAt        *time.Time
}

// MayUseSPN returns whether the user may currently use the SPN.
func (user *UserRecord) MayUseSPN() bool {
	// Shadow this function in order to allow calls on a nil user.
	if user == nil || user.User == nil {
		return false
	}
	return user.User.MayUseSPN()
}

// MayUsePrioritySupport returns whether the user may currently use the priority support.
func (user *UserRecord) MayUsePrioritySupport() bool {
	// Shadow this function in order to allow calls on a nil user.
	if user == nil || user.User == nil {
		return false
	}
	return user.User.MayUsePrioritySupport()
}

// MayUse returns whether the user may currently use the feature identified by
// the given feature ID.
// Leave feature ID empty to check without feature.
func (user *UserRecord) MayUse(featureID account.FeatureID) bool {
	// Shadow this function in order to allow calls on a nil user.
	if user == nil || user.User == nil {
		return false
	}
	return user.User.MayUse(featureID)
}

// AuthTokenRecord holds an authentication token.
type AuthTokenRecord struct {
	record.Base
	sync.Mutex

	Token *account.AuthToken
}

// GetToken returns the token from the record.
func (authToken *AuthTokenRecord) GetToken() *account.AuthToken {
	authToken.Lock()
	defer authToken.Unlock()

	return authToken.Token
}

// SaveNewAuthToken saves a new auth token to the database.
func SaveNewAuthToken(deviceID string, resp *http.Response) error {
	token, ok := account.GetNextTokenFromResponse(resp)
	if !ok {
		return account.ErrMissingToken
	}

	newAuthToken := &AuthTokenRecord{
		Token: &account.AuthToken{
			Device: deviceID,
			Token:  token,
		},
	}
	return newAuthToken.Save()
}

// Update updates an existing auth token with the next token from a response.
func (authToken *AuthTokenRecord) Update(resp *http.Response) error {
	token, ok := account.GetNextTokenFromResponse(resp)
	if !ok {
		return account.ErrMissingToken
	}

	// Update token with new account.AuthToken.
	func() {
		authToken.Lock()
		defer authToken.Unlock()

		authToken.Token = &account.AuthToken{
			Device: authToken.Token.Device,
			Token:  token,
		}
	}()

	return authToken.Save()
}

var (
	accountCacheLock sync.Mutex

	cachedUser    *UserRecord
	cachedUserSet bool

	cachedAuthToken *AuthTokenRecord
)

func clearUserCaches() {
	accountCacheLock.Lock()
	defer accountCacheLock.Unlock()

	cachedUser = nil
	cachedUserSet = false
	cachedAuthToken = nil
}

// GetUser returns the current user account.
// Returns nil when no user is logged in.
func GetUser() (*UserRecord, error) {
	// Check cache.
	accountCacheLock.Lock()
	defer accountCacheLock.Unlock()
	if cachedUserSet {
		if cachedUser == nil {
			return nil, ErrNotLoggedIn
		}
		return cachedUser, nil
	}

	// Load from disk.
	r, err := db.Get(userRecordKey)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			cachedUser = nil
			cachedUserSet = true
			return nil, ErrNotLoggedIn
		}
		return nil, err
	}

	// Unwrap record.
	if r.IsWrapped() {
		// only allocate a new struct, if we need it
		newUser := &UserRecord{}
		err = record.Unwrap(r, newUser)
		if err != nil {
			return nil, err
		}
		cachedUser = newUser
		cachedUserSet = true
		return cachedUser, nil
	}

	// Or adjust type.
	newUser, ok := r.(*UserRecord)
	if !ok {
		return nil, fmt.Errorf("record not of type *UserRecord, but %T", r)
	}
	cachedUser = newUser
	cachedUserSet = true
	return cachedUser, nil
}

// Save saves the User.
func (user *UserRecord) Save() error {
	// Update cache.
	accountCacheLock.Lock()
	defer accountCacheLock.Unlock()
	cachedUser = user
	cachedUserSet = true

	// Update view if unset.
	if user.View == nil {
		user.UpdateView(0)
	}

	// Set, check and update metadata.
	if !user.KeyIsSet() {
		user.SetKey(userRecordKey)
	}
	user.UpdateMeta()

	return db.Put(user)
}

// GetAuthToken returns the current auth token.
func GetAuthToken() (*AuthTokenRecord, error) {
	// Check cache.
	accountCacheLock.Lock()
	defer accountCacheLock.Unlock()
	if cachedAuthToken != nil {
		return cachedAuthToken, nil
	}

	// Load from disk.
	r, err := db.Get(authTokenRecordKey)
	if err != nil {
		return nil, err
	}

	// Unwrap record.
	if r.IsWrapped() {
		// only allocate a new struct, if we need it
		newAuthRecord := &AuthTokenRecord{}
		err = record.Unwrap(r, newAuthRecord)
		if err != nil {
			return nil, err
		}
		cachedAuthToken = newAuthRecord
		return newAuthRecord, nil
	}

	// Or adjust type.
	newAuthRecord, ok := r.(*AuthTokenRecord)
	if !ok {
		return nil, fmt.Errorf("record not of type *AuthTokenRecord, but %T", r)
	}
	cachedAuthToken = newAuthRecord
	return newAuthRecord, nil
}

// Save saves the auth token to the database.
func (authToken *AuthTokenRecord) Save() error {
	// Update cache.
	accountCacheLock.Lock()
	defer accountCacheLock.Unlock()
	cachedAuthToken = authToken

	// Set, check and update metadata.
	if !authToken.KeyIsSet() {
		authToken.SetKey(authTokenRecordKey)
	}
	authToken.UpdateMeta()
	authToken.Meta().MakeSecret()
	authToken.Meta().MakeCrownJewel()

	return db.Put(authToken)
}
