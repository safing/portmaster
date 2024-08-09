package api

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/service/mgr"
)

const (
	sessionCookieName = "Portmaster-API-Token"
	sessionCookieTTL  = 5 * time.Minute
)

var (
	apiKeys     = make(map[string]*AuthToken)
	apiKeysLock sync.Mutex

	authFnSet = abool.New()
	authFn    AuthenticatorFunc

	sessions     = make(map[string]*session)
	sessionsLock sync.Mutex

	// ErrAPIAccessDeniedMessage should be wrapped by errors returned by
	// AuthenticatorFunc in order to signify a blocked request, including a error
	// message for the user. This is an empty message on purpose, as to allow the
	// function to define the full text of the error shown to the user.
	ErrAPIAccessDeniedMessage = errors.New("")
)

// Permission defines an API requests permission.
type Permission int8

const (
	// NotFound declares that the operation does not exist.
	NotFound Permission = -2

	// Dynamic declares that the operation requires permission to be processed,
	// but anyone can execute the operation, as it reacts to permissions itself.
	Dynamic Permission = -1

	// NotSupported declares that the operation is not supported.
	NotSupported Permission = 0

	// PermitAnyone declares that anyone can execute the operation without any
	// authentication.
	PermitAnyone Permission = 1

	// PermitUser declares that the operation may be executed by authenticated
	// third party applications that are categorized as representing a simple
	// user and is limited in access.
	PermitUser Permission = 2

	// PermitAdmin declares that the operation may be executed by authenticated
	// third party applications that are categorized as representing an
	// administrator and has broad in access.
	PermitAdmin Permission = 3

	// PermitSelf declares that the operation may only be executed by the
	// software itself and its own (first party) components.
	PermitSelf Permission = 4
)

// AuthenticatorFunc is a function that can be set as the authenticator for the
// API endpoint. If none is set, all requests will have full access.
// The returned AuthToken represents the permissions that the request has.
type AuthenticatorFunc func(r *http.Request, s *http.Server) (*AuthToken, error)

// AuthToken represents either a set of required or granted permissions.
// All attributes must be set when the struct is built and must not be changed
// later. Functions may be called at any time.
// The Write permission implicitly also includes reading.
type AuthToken struct {
	Read       Permission
	Write      Permission
	ValidUntil *time.Time
}

type session struct {
	sync.Mutex

	token      *AuthToken
	validUntil time.Time
}

// Expired returns whether the session has expired.
func (sess *session) Expired() bool {
	sess.Lock()
	defer sess.Unlock()

	return time.Now().After(sess.validUntil)
}

// Refresh refreshes the validity of the session with the given TTL.
func (sess *session) Refresh(ttl time.Duration) {
	sess.Lock()
	defer sess.Unlock()

	sess.validUntil = time.Now().Add(ttl)
}

// AuthenticatedHandler defines the handler interface to specify custom
// permission for an API handler. The returned permission is the required
// permission for the request to proceed.
type AuthenticatedHandler interface {
	ReadPermission(r *http.Request) Permission
	WritePermission(r *http.Request) Permission
}

// SetAuthenticator sets an authenticator function for the API endpoint. If none is set, all requests will be permitted.
func SetAuthenticator(fn AuthenticatorFunc) error {
	if module.online.Load() {
		return ErrAuthenticationImmutable
	}

	if !authFnSet.SetToIf(false, true) {
		return ErrAuthenticationAlreadySet
	}

	authFn = fn
	return nil
}

func authenticateRequest(w http.ResponseWriter, r *http.Request, targetHandler http.Handler, readMethod bool) *AuthToken {
	tracer := log.Tracer(r.Context())

	// Get required permission for target handler.
	requiredPermission := PermitSelf
	if authdHandler, ok := targetHandler.(AuthenticatedHandler); ok {
		if readMethod {
			requiredPermission = authdHandler.ReadPermission(r)
		} else {
			requiredPermission = authdHandler.WritePermission(r)
		}
	}

	// Check if we need to do any authentication at all.
	switch requiredPermission { //nolint:exhaustive
	case NotFound:
		// Not found.
		tracer.Debug("api: no API endpoint registered for this path")
		http.Error(w, "Not found.", http.StatusNotFound)
		return nil
	case NotSupported:
		// A read or write permission can be marked as not supported.
		tracer.Trace("api: authenticated handler reported: not supported")
		http.Error(w, "Method not allowed.", http.StatusMethodNotAllowed)
		return nil
	case PermitAnyone:
		// Don't process permissions, as we don't need them.
		tracer.Tracef("api: granted %s access to public handler", r.RemoteAddr)
		return &AuthToken{
			Read:  PermitAnyone,
			Write: PermitAnyone,
		}
	case Dynamic:
		// Continue processing permissions, but treat as PermitAnyone.
		requiredPermission = PermitAnyone
	}

	// The required permission must match the request permission values after
	// handling the specials.
	if requiredPermission < PermitAnyone || requiredPermission > PermitSelf {
		tracer.Warningf(
			"api: handler returned invalid permission: %s (%d)",
			requiredPermission,
			requiredPermission,
		)
		http.Error(w, "Internal server error during authentication.", http.StatusInternalServerError)
		return nil
	}

	// Authenticate request.
	token, handled := checkAuth(w, r, requiredPermission > PermitAnyone)
	switch {
	case handled:
		return nil
	case token == nil:
		// Use default permissions.
		token = &AuthToken{
			Read:  PermitAnyone,
			Write: PermitAnyone,
		}
	}

	// Get effective permission for request.
	var requestPermission Permission
	if readMethod {
		requestPermission = token.Read
	} else {
		requestPermission = token.Write
	}

	// Check for valid request permission.
	if requestPermission < PermitAnyone || requestPermission > PermitSelf {
		tracer.Warningf(
			"api: authenticator returned invalid permission: %s (%d)",
			requestPermission,
			requestPermission,
		)
		http.Error(w, "Internal server error during authentication.", http.StatusInternalServerError)
		return nil
	}

	// Check permission.
	if requestPermission < requiredPermission {
		// If the token is strictly public, return an authentication request.
		if token.Read == PermitAnyone && token.Write == PermitAnyone {
			w.Header().Set(
				"WWW-Authenticate",
				`Bearer realm="Portmaster API" domain="/"`,
			)
			http.Error(w, "Authorization required.", http.StatusUnauthorized)
			return nil
		}

		// Otherwise just inform of insufficient permissions.
		http.Error(w, "Insufficient permissions.", http.StatusForbidden)
		return nil
	}

	tracer.Tracef("api: granted %s access to protected handler", r.RemoteAddr)

	// Make a copy of the AuthToken in order mitigate the handler poisoning the
	// token, as changes would apply to future requests.
	return &AuthToken{
		Read:  token.Read,
		Write: token.Write,
	}
}

func checkAuth(w http.ResponseWriter, r *http.Request, authRequired bool) (token *AuthToken, handled bool) {
	// Return highest possible permissions in dev mode.
	if devMode() {
		return &AuthToken{
			Read:  PermitSelf,
			Write: PermitSelf,
		}, false
	}

	// Database Bridge Access.
	if r.RemoteAddr == endpointBridgeRemoteAddress {
		return &AuthToken{
			Read:  dbCompatibilityPermission,
			Write: dbCompatibilityPermission,
		}, false
	}

	// Check for valid API key.
	token = checkAPIKey(r)
	if token != nil {
		return token, false
	}

	// Check for valid session cookie.
	token = checkSessionCookie(r)
	if token != nil {
		return token, false
	}

	// Check if an external authentication method is available.
	if !authFnSet.IsSet() {
		return nil, false
	}

	// Authenticate externally.
	token, err := authFn(r, server)
	if err != nil {
		// Check if the authentication process failed internally.
		if !errors.Is(err, ErrAPIAccessDeniedMessage) {
			log.Tracer(r.Context()).Errorf("api: authenticator failed: %s", err)
			http.Error(w, "Internal server error during authentication.", http.StatusInternalServerError)
			return nil, true
		}

		// Return authentication failure message if authentication is required.
		if authRequired {
			log.Tracer(r.Context()).Warningf("api: denying api access from %s", r.RemoteAddr)
			http.Error(w, err.Error(), http.StatusForbidden)
			return nil, true
		}

		return nil, false
	}

	// Abort if no token is returned.
	if token == nil {
		return nil, false
	}

	// Create session cookie for authenticated request.
	err = createSession(w, r, token)
	if err != nil {
		log.Tracer(r.Context()).Warningf("api: failed to create session: %s", err)
	}
	return token, false
}

func checkAPIKey(r *http.Request) *AuthToken {
	// Get API key from request.
	key := r.Header.Get("Authorization")
	if key == "" {
		return nil
	}

	// Parse API key.
	switch {
	case strings.HasPrefix(key, "Bearer "):
		key = strings.TrimPrefix(key, "Bearer ")
	case strings.HasPrefix(key, "Basic "):
		user, pass, _ := r.BasicAuth()
		key = user + pass
	default:
		log.Tracer(r.Context()).Tracef(
			"api: provided api key type %s is unsupported", strings.Split(key, " ")[0],
		)
		return nil
	}

	apiKeysLock.Lock()
	defer apiKeysLock.Unlock()

	// Check if the provided API key exists.
	token, ok := apiKeys[key]
	if !ok {
		log.Tracer(r.Context()).Tracef(
			"api: provided api key %s... is unknown", key[:4],
		)
		return nil
	}

	// Abort if the token is expired.
	if token.ValidUntil != nil && time.Now().After(*token.ValidUntil) {
		log.Tracer(r.Context()).Warningf("api: denying api access from %s using expired token", r.RemoteAddr)
		return nil
	}

	return token
}

func updateAPIKeys() {
	apiKeysLock.Lock()
	defer apiKeysLock.Unlock()

	log.Debug("api: importing possibly updated API keys from config")

	// Delete current keys.
	for k := range apiKeys {
		delete(apiKeys, k)
	}

	// whether or not we found expired API keys that should be removed
	// from the setting
	hasExpiredKeys := false

	// a list of valid API keys. Used when hasExpiredKeys is set to true.
	// in that case we'll update the setting to only contain validAPIKeys
	validAPIKeys := []string{}

	// Parse new keys.
	for _, key := range configuredAPIKeys() {
		u, err := url.Parse(key)
		if err != nil {
			log.Errorf("api: failed to parse configured API key %s: %s", key, err)

			continue
		}

		if u.Path == "" {
			log.Errorf("api: malformed API key %s: missing path section", key)

			continue
		}

		// Create token with default permissions.
		token := &AuthToken{
			Read:  PermitAnyone,
			Write: PermitAnyone,
		}

		// Update with configured permissions.
		q := u.Query()
		// Parse read permission.
		readPermission, err := parseAPIPermission(q.Get("read"))
		if err != nil {
			log.Errorf("api: invalid API key %s: %s", key, err)
			continue
		}
		token.Read = readPermission
		// Parse write permission.
		writePermission, err := parseAPIPermission(q.Get("write"))
		if err != nil {
			log.Errorf("api: invalid API key %s: %s", key, err)
			continue
		}
		token.Write = writePermission

		expireStr := q.Get("expires")
		if expireStr != "" {
			validUntil, err := time.Parse(time.RFC3339, expireStr)
			if err != nil {
				log.Errorf("api: invalid API key %s: %s", key, err)
				continue
			}

			// continue to the next token if this one is already invalid
			if time.Now().After(validUntil) {
				// mark the key as expired so we'll remove it from the setting afterwards
				hasExpiredKeys = true

				continue
			}

			token.ValidUntil = &validUntil
		}

		// Save token.
		apiKeys[u.Path] = token
		validAPIKeys = append(validAPIKeys, key)
	}

	if hasExpiredKeys {
		module.mgr.Go("api key cleanup", func(ctx *mgr.WorkerCtx) error {
			if err := config.SetConfigOption(CfgAPIKeys, validAPIKeys); err != nil {
				log.Errorf("api: failed to remove expired API keys: %s", err)
			} else {
				log.Infof("api: removed expired API keys from %s", CfgAPIKeys)
			}

			return nil
		})
	}
}

func checkSessionCookie(r *http.Request) *AuthToken {
	// Get session cookie from request.
	c, err := r.Cookie(sessionCookieName)
	if err != nil {
		return nil
	}

	// Check if session cookie is registered.
	sessionsLock.Lock()
	sess, ok := sessions[c.Value]
	sessionsLock.Unlock()
	if !ok {
		log.Tracer(r.Context()).Tracef("api: provided session cookie %s is unknown", c.Value)
		return nil
	}

	// Check if session is still valid.
	if sess.Expired() {
		log.Tracer(r.Context()).Tracef("api: provided session cookie %s has expired", c.Value)
		return nil
	}

	// Refresh session and return.
	sess.Refresh(sessionCookieTTL)
	log.Tracer(r.Context()).Tracef("api: session cookie %s is valid, refreshing", c.Value)
	return sess.token
}

func createSession(w http.ResponseWriter, r *http.Request, token *AuthToken) error {
	// Generate new session key.
	secret, err := rng.Bytes(32) // 256 bit
	if err != nil {
		return err
	}
	sessionKey := base64.RawURLEncoding.EncodeToString(secret)

	// Set token cookie in response.
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionKey,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})

	// Create session.
	sess := &session{
		token: token,
	}
	sess.Refresh(sessionCookieTTL)

	// Save session.
	sessionsLock.Lock()
	defer sessionsLock.Unlock()
	sessions[sessionKey] = sess
	log.Tracer(r.Context()).Debug("api: issued session cookie")

	return nil
}

func cleanSessions(_ *mgr.WorkerCtx) error {
	sessionsLock.Lock()
	defer sessionsLock.Unlock()

	for sessionKey, sess := range sessions {
		if sess.Expired() {
			delete(sessions, sessionKey)
		}
	}

	return nil
}

func deleteSession(sessionKey string) {
	sessionsLock.Lock()
	defer sessionsLock.Unlock()

	delete(sessions, sessionKey)
}

func getEffectiveMethod(r *http.Request) (eMethod string, readMethod bool, ok bool) {
	method := r.Method

	// Get CORS request method if OPTIONS request.
	if r.Method == http.MethodOptions {
		method = r.Header.Get("Access-Control-Request-Method")
		if method == "" {
			return "", false, false
		}
	}

	switch method {
	case http.MethodGet, http.MethodHead:
		return http.MethodGet, true, true
	case http.MethodPost, http.MethodPut, http.MethodDelete:
		return method, false, true
	default:
		return "", false, false
	}
}

func parseAPIPermission(s string) (Permission, error) {
	switch strings.ToLower(s) {
	case "", "anyone":
		return PermitAnyone, nil
	case "user":
		return PermitUser, nil
	case "admin":
		return PermitAdmin, nil
	default:
		return PermitAnyone, fmt.Errorf("invalid permission: %s", s)
	}
}

func (p Permission) String() string {
	switch p {
	case NotSupported:
		return "NotSupported"
	case Dynamic:
		return "Dynamic"
	case PermitAnyone:
		return "PermitAnyone"
	case PermitUser:
		return "PermitUser"
	case PermitAdmin:
		return "PermitAdmin"
	case PermitSelf:
		return "PermitSelf"
	case NotFound:
		return "NotFound"
	default:
		return "Unknown"
	}
}

// Role returns a string representation of the permission role.
func (p Permission) Role() string {
	switch p {
	case PermitAnyone:
		return "Anyone"
	case PermitUser:
		return "User"
	case PermitAdmin:
		return "Admin"
	case PermitSelf:
		return "Self"
	case Dynamic, NotFound, NotSupported:
		return "Invalid"
	default:
		return "Invalid"
	}
}
