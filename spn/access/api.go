package access

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/spn/access/account"
)

func registerAPIEndpoints() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        `spn/account/login`,
		Write:       api.PermitAdmin,
		WriteMethod: http.MethodPost,
		HandlerFunc: handleLogin,
		Name:        "SPN Login",
		Description: "Log into your SPN account.",
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        `spn/account/logout`,
		Write:       api.PermitAdmin,
		WriteMethod: http.MethodDelete,
		ActionFunc:  handleLogout,
		Name:        "SPN Logout",
		Description: "Logout from your SPN account.",
		Parameters: []api.Parameter{
			{
				Method:      http.MethodDelete,
				Field:       "purge",
				Value:       "",
				Description: "If set, account data is purged. Otherwise, the username and device ID are kept in order to log into the same device when logging in with the same user again.",
			},
		},
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        `spn/account/user/profile`,
		Read:        api.PermitUser,
		ReadMethod:  http.MethodGet,
		RecordFunc:  handleGetUserProfile,
		Name:        "SPN User Profile",
		Description: "Get the user profile of the logged in SPN account.",
		Parameters: []api.Parameter{
			{
				Method:      http.MethodGet,
				Field:       "refresh",
				Value:       "",
				Description: "If set, the user profile is freshly fetched from the account server.",
			},
		},
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:       `account/features`,
		Read:       api.PermitUser,
		ReadMethod: http.MethodGet,
		StructFunc: func(_ *api.Request) (i interface{}, err error) {
			return struct {
				Features []Feature
			}{
				Features: features,
			}, nil
		},
		Name:        "Get Account Features",
		Description: "Returns all account features.",
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:       `account/features/{id:[A-Za-z0-9_-]+}/icon`,
		Read:       api.PermitUser,
		ReadMethod: http.MethodGet,
		Name:       "Returns the image of the featuare",
		MimeType:   "image/svg+xml",
		DataFunc: func(ar *api.Request) (data []byte, err error) {
			featureID, ok := ar.URLVars["id"]
			if !ok {
				return nil, errors.New("invalid feature id")
			}

			for _, feature := range features {
				if feature.ID == featureID {
					return []byte(feature.icon), nil
				}
			}

			return nil, errors.New("feature id not found")
		},
	}); err != nil {
		return err
	}

	return nil
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	// Get username and password.
	username, password, ok := r.BasicAuth()
	// Request, if omitted.
	if !ok || username == "" || password == "" {
		w.Header().Set("WWW-Authenticate", "Basic realm=SPN Login")
		http.Error(w, "Login with your SPN account.", http.StatusUnauthorized)
		return
	}

	// Process login.
	user, code, err := Login(username, password)
	if err != nil {
		log.Warningf("spn/access: failed to login: %s", err)
		if code == 0 {
			http.Error(w, "Internal error: "+err.Error(), http.StatusInternalServerError)
		} else {
			http.Error(w, err.Error(), code)
		}
		return
	}

	// Return success.
	_, _ = w.Write([]byte(
		fmt.Sprintf("Now logged in as %s as device %s", user.Username, user.Device.Name),
	))
}

func handleLogout(ar *api.Request) (msg string, err error) {
	purge := ar.URL.Query().Get("purge") != ""
	err = Logout(false, purge)
	switch {
	case err != nil:
		log.Warningf("spn/access: failed to logout: %s", err)
		return "", err
	case purge:
		return "Logged out and user data purged.", nil
	default:
		return "Logged out.", nil
	}
}

func handleGetUserProfile(ar *api.Request) (r record.Record, err error) {
	// Check if we are already authenticated.
	user, err := GetUser()
	if err != nil || user.State == account.UserStateNone {
		return nil, api.ErrorWithStatus(
			ErrNotLoggedIn,
			account.StatusInvalidAuth,
		)
	}

	// Should we refresh the user profile?
	if ar.URL.Query().Get("refresh") != "" {
		user, _, err = UpdateUser()
		if err != nil {
			return nil, err
		}
	}

	return user, nil
}
