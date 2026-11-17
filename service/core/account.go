package core

import (
	"errors"
	"net/http"
	"sync"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/runtime"
)

const userRecordKey = "core:spn/account/user"

// UserProfileRecord holds the local user account profile to expose enabled features
// (such as bandwidth visibility and history) to the UI without requiring connection
// to external account servers.
type UserProfileRecord struct {
	record.Base
	sync.Mutex

	Username    string           `json:"username"`
	State       string           `json:"state"`
	CurrentPlan *UserPlan        `json:"current_plan"`
	View        *UserProfileView `json:"view,omitempty"`
}

// UserPlan describes the active feature plan for the UI.
type UserPlan struct {
	Name       string   `json:"name"`
	FeatureIDs []string `json:"feature_ids"`
}

// UserProfileView controls display of account buttons in the UI.
type UserProfileView struct {
	Message           string `json:"Message"`
	ShowAccountData   bool   `json:"ShowAccountData"`
	ShowAccountButton bool   `json:"ShowAccountButton"`
	ShowLoginButton   bool   `json:"ShowLoginButton"`
	ShowRefreshButton bool   `json:"ShowRefreshButton"`
	ShowLogoutButton  bool   `json:"ShowLogoutButton"`
}

// AccountFeature describes a feature for the UI.
type AccountFeature struct {
	Name              string          `json:"Name"`
	ID                string          `json:"ID"`
	RequiredFeatureID string          `json:"RequiredFeatureID"`
	ConfigKey         string          `json:"ConfigKey,omitempty"`
	ConfigScope       string          `json:"ConfigScope,omitempty"`
	InPackage         *AccountPackage `json:"InPackage,omitempty"`
	Comment           string          `json:"Comment,omitempty"`
	Beta              bool            `json:"Beta,omitempty"`
	ComingSoon        bool            `json:"ComingSoon,omitempty"`
	icon              string
}

// AccountPackage combines a set of features.
type AccountPackage struct {
	Name     string `json:"Name"`
	HexColor string `json:"HexColor"`
	InfoURL  string `json:"InfoURL"`
}

var (
	pkgFree = &AccountPackage{
		Name:     "Free",
		HexColor: "#ffffff",
	}
	pkgPlus = &AccountPackage{
		Name:     "Plus",
		HexColor: "#2fcfae",
	}

	accountFeatures = []AccountFeature{
		{
			Name:        "Secure DNS",
			ID:          "dns",
			ConfigScope: "dns/",
			InPackage:   pkgFree,
			icon: `
			    <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
    			  <path stroke-linecap="round" stroke-linejoin="round"
    			    d="M12 21a9.004 9.004 0 008.716-6.747M12 21a9.004 9.004 0 01-8.716-6.747M12 21c2.485 0 4.5-4.03 4.5-9S14.485 3 12 3m0 18c-2.485 0-4.5-4.03-4.5-9S9.515 3 12 3m0 0a8.997 8.997 0 017.843 4.582M12 3a8.997 8.997 0 00-7.843 4.582m15.686 0A11.953 11.953 0 0112 10.5c-2.998 0-5.74-1.1-7.843-2.918m15.686 0A8.959 8.959 0 0121 12c0 .778-.099 1.533-.284 2.253m0 0A17.919 17.919 0 0112 16.5c-3.162 0-6.133-.815-8.716-2.247m0 0A9.015 9.015 0 013 12c0-1.605.42-3.113 1.157-4.418" />
    			</svg>
			`,
		},
		{
			Name:        "Privacy Filter",
			ID:          "filter",
			ConfigScope: "filter/",
			InPackage:   pkgFree,
			icon: `
<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
  <path stroke-linecap="round" stroke-linejoin="round" d="M3.98 8.223A10.477 10.477 0 001.934 12C3.226 16.338 7.244 19.5 12 19.5c.993 0 1.953-.138 2.863-.395M6.228 6.228A10.45 10.45 0 0112 4.5c4.756 0 8.773 3.162 10.065 7.498a10.523 10.523 0 01-4.293 5.774M6.228 6.228L3 3m3.228 3.228l3.65 3.65m7.894 7.894L21 21m-3.228-3.228l-3.65-3.65m0 0a3 3 0 10-4.243-4.243m4.242 4.242L9.88 9.88" />
</svg>
			`,
		},
		{
			Name:              "Network History",
			ID:                "history",
			RequiredFeatureID: "history",
			ConfigKey:         "history/enable",
			ConfigScope:       "history/",
			InPackage:         pkgPlus,
			icon: `
<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
  <path stroke-linecap="round" stroke-linejoin="round"
    d="M12 6.042A8.967 8.967 0 006 3.75c-1.052 0-2.062.18-3 .512v14.25A8.987 8.987 0 016 18c2.305 0 4.408.867 6 2.292m0-14.25a8.966 8.966 0 016-2.292c1.052 0 2.062.18 3 .512v14.25A8.987 8.987 0 0018 18a8.967 8.967 0 00-6 2.292m0-14.25v14.25" />
</svg>
			`,
		},
		{
			Name:              "Bandwidth Visibility",
			ID:                "bw-vis",
			RequiredFeatureID: "bw-vis",
			InPackage:         pkgPlus,
			Beta:              true,
			icon: `
    <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
      <path stroke-linecap="round" stroke-linejoin="round"
        d="M3 13.125C3 12.504 3.504 12 4.125 12h2.25c.621 0 1.125.504 1.125 1.125v6.75C7.5 20.496 6.996 21 6.375 21h-2.25A1.125 1.125 0 013 19.875v-6.75zM9.75 8.625c0-.621.504-1.125 1.125-1.125h2.25c.621 0 1.125.504 1.125 1.125v11.25c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 01-1.125-1.125V8.625zM16.5 4.125c0-.621.504-1.125 1.125-1.125h2.25C20.496 3 21 3.504 21 4.125v15.75c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 01-1.125-1.125V4.125z" />
    </svg>
			`,
		},
		{
			Name:        "Split Tunneling",
			ID:          "splittun",
			ConfigKey:   "splittun/enable",
			ConfigScope: "splittun/",
			InPackage:   pkgFree,
			icon: `
	<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" stroke="currentColor" fill="none">
	<g stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5">    
		<path d="M8.5 12h3" />    
		<path d="M11 12c4-2 6-4 6.5-4.5" /> <path d="M11 12c4 2 6 4 6.5 4.5" />		
		<circle cx="19" cy="6" r="2" /> <circle cx="19" cy="18" r="2" /> <circle cx="4" cy="12" r="2" />
	</g>
	</svg>
			`,
		},
	}
)

func getLocalUserProfile() *UserProfileRecord {
	return &UserProfileRecord{
		Username: "local",
		State:    "approved",
		CurrentPlan: &UserPlan{
			Name: "Local",
			FeatureIDs: []string{
				"bw-vis",
				"history",
				"spn",
				"xray",
				"splittun",
			},
		},
		View: &UserProfileView{
			Message:           "",
			ShowAccountData:   false,
			ShowAccountButton: false,
			ShowLoginButton:   false,
			ShowRefreshButton: false,
			ShowLogoutButton:  false,
		},
	}
}

func registerAccountAPIEndpoints() error {
	if err := api.RegisterEndpoint(api.Endpoint{
		Path:        `spn/account/user/profile`,
		Read:        api.PermitUser,
		ReadMethod:  http.MethodGet,
		StructFunc: func(_ *api.Request) (interface{}, error) {
			return getLocalUserProfile(), nil
		},
		Name:        "SPN User Profile",
		Description: "Get the user profile of the local account.",
	}); err != nil {
		return err
	}

	if err := api.RegisterEndpoint(api.Endpoint{
		Path:       `account/features`,
		Read:       api.PermitUser,
		ReadMethod: http.MethodGet,
		StructFunc: func(_ *api.Request) (interface{}, error) {
			return struct {
				Features []AccountFeature `json:"Features"`
			}{
				Features: accountFeatures,
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
		Name:       "Returns the image of the feature",
		MimeType:   "image/svg+xml",
		DataFunc: func(ar *api.Request) ([]byte, error) {
			featureID, ok := ar.URLVars["id"]
			if !ok {
				return nil, errors.New("invalid feature id")
			}

			for _, feature := range accountFeatures {
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

// SPNStatusRecord provides a stub for runtime:spn/status so UI and tray
// subscriptions receive a valid "disabled" status instead of failing.
type SPNStatusRecord struct {
	record.Base
	sync.Mutex
	Status string `json:"Status"`
}

func initSPNStatus() {
	rec := &SPNStatusRecord{
		Status: "connected",
	}
	rec.SetKey("runtime:spn/status")
	rec.UpdateMeta()

	provider := runtime.SimpleValueGetterFunc(func(_ string) ([]record.Record, error) {
		return []record.Record{rec}, nil
	})
	if _, err := runtime.Register("spn/status", provider); err != nil {
		log.Warningf("core: failed to register runtime:spn/status: %s", err)
	}
}

func initUserProfile() {
	user := getLocalUserProfile()
	user.SetKey(userRecordKey)
	user.UpdateMeta()

	if err := db.Put(user); err != nil {
		log.Warningf("core: failed to save local user profile: %s", err)
	}

	initSPNStatus()
}
