package access

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/safing/portmaster/base/database"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/spn/access/account"
	"github.com/safing/portmaster/spn/access/token"
	"github.com/safing/structures/dsd"
)

// Client URLs.
const (
	AccountServer         = "https://api.account.safing.io"
	LoginPath             = "/api/v1/authenticate"
	UserProfilePath       = "/api/v1/user/profile"
	TokenRequestSetupPath = "/api/v1/token/request/setup" //nolint:gosec
	TokenRequestIssuePath = "/api/v1/token/request/issue" //nolint:gosec
	HealthCheckPath       = "/api/v1/health"

	defaultDataFormat     = dsd.CBOR
	defaultRequestTimeout = 30 * time.Second
)

var (
	accountClient     = &http.Client{}
	clientRequestLock sync.Mutex

	// EnableAfterLogin automatically enables the SPN subsystem/module after login.
	EnableAfterLogin = true
)

type clientRequestOptions struct {
	method               string
	url                  string
	send                 interface{}
	recv                 interface{}
	requestTimeout       time.Duration
	dataFormat           uint8
	setAuthToken         bool
	requireNextAuthToken bool
	logoutOnAuthError    bool
	requestSetupFunc     func(*http.Request) error
}

func makeClientRequest(opts *clientRequestOptions) (resp *http.Response, err error) {
	// Get request timeout.
	if opts.requestTimeout == 0 {
		opts.requestTimeout = defaultRequestTimeout
	}
	// Get context for request.
	var ctx context.Context
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(module.mgr.Ctx(), opts.requestTimeout)
	defer cancel()

	// Create new request.
	request, err := http.NewRequestWithContext(ctx, opts.method, opts.url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request structure: %w", err)
	}

	// Prepare body and content type.
	if opts.dataFormat == dsd.AUTO {
		opts.dataFormat = defaultDataFormat
	}
	if opts.send != nil {
		// Add data to body.
		err = dsd.DumpToHTTPRequest(request, opts.send, opts.dataFormat)
		if err != nil {
			return nil, fmt.Errorf("failed to add request body: %w", err)
		}
	} else {
		// Set requested HTTP response format.
		_, err = dsd.RequestHTTPResponseFormat(request, opts.dataFormat)
		if err != nil {
			return nil, fmt.Errorf("failed to set requested response format: %w", err)
		}
	}

	// Get auth token to apply to request.
	var authToken *AuthTokenRecord
	if opts.setAuthToken {
		authToken, err = GetAuthToken()
		if err != nil {
			return nil, ErrNotLoggedIn
		}
		authToken.Token.ApplyTo(request)
	}

	// Do any additional custom request setup.
	if opts.requestSetupFunc != nil {
		err = opts.requestSetupFunc(request)
		if err != nil {
			return nil, err
		}
	}

	// Make request.
	resp, err = accountClient.Do(request)
	if err != nil {
		updateUserWithFailedRequest(account.StatusConnectionError, false)
		tokenIssuerFailed()
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	log.Debugf("spn/access: request to %s returned %s", request.URL, resp.Status)
	defer func() {
		_ = resp.Body.Close()
	}()
	// Handle request error.
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated:
		// All good!

	case account.StatusInvalidAuth, account.StatusInvalidDevice:
		// Wrong username / password.
		updateUserWithFailedRequest(resp.StatusCode, true)
		return resp, ErrInvalidCredentials

	case account.StatusReachedDeviceLimit:
		// Device limit is reached.
		updateUserWithFailedRequest(resp.StatusCode, true)
		return resp, ErrDeviceLimitReached

	case account.StatusDeviceInactive:
		// Device is locked.
		updateUserWithFailedRequest(resp.StatusCode, true)
		return resp, ErrDeviceIsLocked

	default:
		updateUserWithFailedRequest(account.StatusUnknownError, false)
		tokenIssuerFailed()
		return resp, fmt.Errorf("unexpected reply: [%d] %s", resp.StatusCode, resp.Status)
	}

	// Save next auth token.
	if authToken != nil {
		err = authToken.Update(resp)
		if err != nil {
			if errors.Is(err, account.ErrMissingToken) {
				if opts.requireNextAuthToken {
					return resp, fmt.Errorf("failed to save next auth token: %w", err)
				}
			} else {
				return resp, fmt.Errorf("failed to save next auth token: %w", err)
			}
		}
	} else if opts.requireNextAuthToken {
		return resp, fmt.Errorf("failed to save next auth token: %w", account.ErrMissingToken)
	}

	// Load response data.
	if opts.recv != nil {
		_, err = dsd.LoadFromHTTPResponse(resp, opts.recv)
		if err != nil {
			return resp, fmt.Errorf("failed to parse response: %w", err)
		}
	}

	tokenIssuerIsFailing.UnSet()
	return resp, nil
}

func updateUserWithFailedRequest(statusCode int, disableSubscription bool) {
	// Get user from database.
	user, err := GetUser()
	if err != nil {
		if !errors.Is(err, ErrNotLoggedIn) {
			log.Warningf("spn/access: failed to get user to update with failed request: %s", err)
		}
		return
	}

	func() {
		user.Lock()
		defer user.Unlock()

		// Ignore update if user state is undefined or logged out.
		if user.State == "" || user.State == account.UserStateLoggedOut {
			return
		}

		// Disable the subscription if desired.
		if disableSubscription && user.Subscription != nil {
			user.Subscription.EndsAt = nil
		}

		// Update view with the status code and save user.
		user.UpdateView(statusCode)
	}()

	err = user.Save()
	if err != nil {
		log.Warningf("spn/access: failed to save user after update with failed request: %s", err)
	}
}

// Login logs the user into the SPN account with the given username and password.
func Login(username, password string) (user *UserRecord, code int, err error) {
	clientRequestLock.Lock()
	defer clientRequestLock.Unlock()

	// Trigger account update when done.
	defer module.EventAccountUpdate.Submit(struct{}{})

	// Get previous user.
	previousUser, err := GetUser()
	if err != nil {
		if !errors.Is(err, ErrNotLoggedIn) {
			log.Warningf("spn/access: failed to get previous for re-login: %s", err)
		}
		previousUser = nil
	}

	// Create request options.
	userAccount := &account.User{}
	requestOptions := &clientRequestOptions{
		method:     http.MethodPost,
		url:        AccountServer + LoginPath,
		recv:       userAccount,
		dataFormat: dsd.JSON,
		requestSetupFunc: func(request *http.Request) error {
			// Add username and password.
			request.SetBasicAuth(username, password)

			// Try to reuse the device ID, if the username matches the previous user.
			if previousUser != nil && username == previousUser.Username {
				request.Header.Set(account.AuthHeaderDevice, previousUser.Device.ID)
			}

			return nil
		},
	}

	// Make request.
	resp, err := makeClientRequest(requestOptions) //nolint:bodyclose // Body is closed in function.
	if err != nil {
		if resp != nil && resp.StatusCode == account.StatusInvalidDevice {
			// Try again without the previous device ID.
			previousUser = nil
			log.Info("spn/access: retrying log in without re-using previous device ID")
			resp, err = makeClientRequest(requestOptions) //nolint:bodyclose // Body is closed in function.
		}
		if err != nil {
			if resp != nil {
				return nil, resp.StatusCode, err
			}
			return nil, 0, err
		}
	}

	// Save new user.
	now := time.Now()
	user = &UserRecord{
		User:       userAccount,
		LoggedInAt: &now,
	}

	user.UpdateView(0)
	err = user.Save()
	if err != nil {
		return user, resp.StatusCode, fmt.Errorf("failed to save new user profile: %w", err)
	}

	// Save initial auth token.
	err = SaveNewAuthToken(user.Device.ID, resp)
	if err != nil {
		return user, resp.StatusCode, fmt.Errorf("failed to save initial auth token: %w", err)
	}

	// Enable the SPN right after login.
	if user.MayUseSPN() && EnableAfterLogin {
		enableSPN()
	}

	log.Infof("spn/access: logged in as %q on device %q", user.Username, user.Device.Name)
	return user, resp.StatusCode, nil
}

// Logout logs the user out of the SPN account.
// Specify "shallow" to keep user data in order to display data in the
// UI - preferably when logged out be the server.
// Specify "purge" in order to fully delete all user account data, even
// the device ID so that logging in again will create a new device.
func Logout(shallow, purge bool) error {
	clientRequestLock.Lock()
	defer clientRequestLock.Unlock()

	// Trigger account update when done.
	defer module.EventAccountUpdate.Submit(struct{}{})

	// Clear caches.
	clearUserCaches()

	// Clear tokens.
	clearTokens()

	// Delete auth token.
	err := db.Delete(authTokenRecordKey)
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return fmt.Errorf("failed to delete auth token: %w", err)
	}

	// Delete all user data if purging.
	if purge {
		err := db.Delete(userRecordKey)
		if err != nil && !errors.Is(err, database.ErrNotFound) {
			return fmt.Errorf("failed to delete user: %w", err)
		}

		// Disable SPN when the user logs out directly.
		disableSPN()

		log.Info("spn/access: logged out and purged data")
		return nil
	}

	// Else, just update the user.
	user, err := GetUser()
	if err != nil {
		if errors.Is(err, ErrNotLoggedIn) {
			return nil
		}
		return fmt.Errorf("failed to load user for logout: %w", err)
	}

	func() {
		user.Lock()
		defer user.Unlock()

		if shallow {
			// Shallow logout: User stays logged in the UI to display status when
			// logged out from the Portmaster or Customer Hub.
			user.User.State = account.UserStateLoggedOut
		} else {
			// Proper logout: User is logged out from UI.
			// Reset all user data, except for username and device ID in order to log
			// into the same device again.
			user.User = &account.User{
				Username: user.Username,
				Device: &account.Device{
					ID: user.Device.ID,
				},
			}
			user.LoggedInAt = &time.Time{}
		}
		user.UpdateView(0)
	}()
	err = user.Save()
	if err != nil {
		return fmt.Errorf("failed to save user for logout: %w", err)
	}

	if shallow {
		log.Info("spn/access: logged out shallow")
	} else {
		log.Info("spn/access: logged out")

		// Disable SPN when the user logs out directly.
		disableSPN()
	}

	return nil
}

// UpdateUser fetches the current user information from the server.
func UpdateUser() (user *UserRecord, statusCode int, err error) {
	clientRequestLock.Lock()
	defer clientRequestLock.Unlock()

	// Trigger account update when done.
	defer module.EventAccountUpdate.Submit(struct{}{})

	// Create request options.
	userData := &account.User{}
	requestOptions := &clientRequestOptions{
		method:               http.MethodGet,
		url:                  AccountServer + UserProfilePath,
		recv:                 userData,
		dataFormat:           dsd.JSON,
		setAuthToken:         true,
		requireNextAuthToken: true,
		logoutOnAuthError:    true,
	}

	// Make request.
	resp, err := makeClientRequest(requestOptions) //nolint:bodyclose // Body is closed in function.
	if err != nil {
		if resp != nil {
			return nil, resp.StatusCode, err
		}
		return nil, 0, err
	}

	// Save to previous user, if exists.
	previousUser, err := GetUser()
	if err == nil {
		func() {
			previousUser.Lock()
			defer previousUser.Unlock()
			previousUser.User = userData
			previousUser.UpdateView(resp.StatusCode)
		}()
		err := previousUser.Save()
		if err != nil {
			log.Warningf("spn/access: failed to save updated user profile: %s", err)
		}

		// Notify user of nearing end of package.
		notifyOfPackageEnd(previousUser)

		log.Infof("spn/access: got user profile, updated existing")
		return previousUser, resp.StatusCode, nil
	}

	// Else, save as new user.
	now := time.Now()
	newUser := &UserRecord{
		User:       userData,
		LoggedInAt: &now,
	}
	newUser.UpdateView(resp.StatusCode)
	err = newUser.Save()
	if err != nil {
		log.Warningf("spn/access: failed to save new user profile: %s", err)
	}

	// Notify user of nearing end of package.
	notifyOfPackageEnd(newUser)

	log.Infof("spn/access: got user profile, saved as new")
	return newUser, resp.StatusCode, nil
}

// UpdateTokens fetches more tokens for handlers that need it.
func UpdateTokens() error {
	clientRequestLock.Lock()
	defer clientRequestLock.Unlock()

	// Check if the user may request tokens.
	user, err := GetUser()
	if err != nil {
		return fmt.Errorf("failed to get user: %w", err)
	}
	if !user.MayUseTheSPN() {
		return ErrMayNotUseSPN
	}

	// Create setup request, return if not required.
	setupRequest, setupRequired := token.CreateSetupRequest()
	var setupResponse *token.SetupResponse
	if setupRequired {
		// Request setup data.
		setupResponse = &token.SetupResponse{}
		_, err := makeClientRequest(&clientRequestOptions{ //nolint:bodyclose // Body is closed in function.
			method:            http.MethodPost,
			url:               AccountServer + TokenRequestSetupPath,
			send:              setupRequest,
			recv:              setupResponse,
			dataFormat:        dsd.MsgPack,
			setAuthToken:      true,
			logoutOnAuthError: true,
		})
		if err != nil {
			return fmt.Errorf("failed to request setup data: %w", err)
		}
	}

	// Create request for issuing new tokens.
	tokenRequest, requestRequired, err := token.CreateTokenRequest(setupResponse)
	if err != nil {
		return fmt.Errorf("failed to create token request: %w", err)
	}
	if !requestRequired {
		return nil
	}

	// Request issuing new tokens.
	issuedTokens := &token.IssuedTokens{}
	_, err = makeClientRequest(&clientRequestOptions{ //nolint:bodyclose // Body is closed in function.
		method:            http.MethodPost,
		url:               AccountServer + TokenRequestIssuePath,
		send:              tokenRequest,
		recv:              issuedTokens,
		dataFormat:        dsd.MsgPack,
		setAuthToken:      true,
		logoutOnAuthError: true,
	})
	if err != nil {
		return fmt.Errorf("failed to request tokens: %w", err)
	}

	// Save tokens to handlers.
	err = token.ProcessIssuedTokens(issuedTokens)
	if err != nil {
		return fmt.Errorf("failed to process issued tokens: %w", err)
	}

	// Log new status.
	regular, fallback := GetTokenAmount(ExpandAndConnectZones)
	log.Infof(
		"spn/access: got new tokens, now at %d regular and %d fallback tokens for expand and connect",
		regular,
		fallback,
	)

	return nil
}

var (
	lastHealthCheckExpires          time.Time
	lastHealthCheckLock             sync.Mutex
	lastHealthCheckValidityDuration = 30 * time.Second
)

func healthCheck() (ok bool) {
	lastHealthCheckLock.Lock()
	defer lastHealthCheckLock.Unlock()

	// Return current value if recently checked.
	if time.Now().Before(lastHealthCheckExpires) {
		return tokenIssuerIsFailing.IsNotSet()
	}

	// Check health.
	_, err := makeClientRequest(&clientRequestOptions{ //nolint:bodyclose // Body is closed in function.
		method: http.MethodGet,
		url:    AccountServer + HealthCheckPath,
	})
	if err != nil {
		log.Warningf("spn/access: token issuer health check failed: %s", err)
	}
	// Update health check expiry.
	lastHealthCheckExpires = time.Now().Add(lastHealthCheckValidityDuration)

	return tokenIssuerIsFailing.IsNotSet()
}
