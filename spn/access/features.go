package access

import "github.com/safing/portmaster/spn/access/account"

// Feature describes a notable part of the program.
type Feature struct {
	Name              string
	ID                string
	RequiredFeatureID account.FeatureID
	ConfigKey         string
	ConfigScope       string
	InPackage         *Package
	Comment           string
	Beta              bool
	ComingSoon        bool
	icon              string
}

// Package combines a set of features.
type Package struct {
	Name     string
	HexColor string
	InfoURL  string
}

var (
	infoURL     = "https://safing.io/pricing/"
	packageFree = &Package{
		Name:     "Free",
		HexColor: "#ffffff",
		InfoURL:  infoURL,
	}
	packagePlus = &Package{
		Name:     "Plus",
		HexColor: "#2fcfae",
		InfoURL:  infoURL,
	}
	packagePro = &Package{
		Name:     "Pro",
		HexColor: "#029ad0",
		InfoURL:  infoURL,
	}
	features = []Feature{
		{
			Name:        "Secure DNS",
			ID:          "dns",
			ConfigScope: "dns/",
			InPackage:   packageFree,
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
			InPackage:   packageFree,
			icon: `
<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
  <path stroke-linecap="round" stroke-linejoin="round" d="M3.98 8.223A10.477 10.477 0 001.934 12C3.226 16.338 7.244 19.5 12 19.5c.993 0 1.953-.138 2.863-.395M6.228 6.228A10.45 10.45 0 0112 4.5c4.756 0 8.773 3.162 10.065 7.498a10.523 10.523 0 01-4.293 5.774M6.228 6.228L3 3m3.228 3.228l3.65 3.65m7.894 7.894L21 21m-3.228-3.228l-3.65-3.65m0 0a3 3 0 10-4.243-4.243m4.242 4.242L9.88 9.88" />
</svg>
			`,
		},
		{
			Name:              "Network History",
			ID:                string(account.FeatureHistory),
			RequiredFeatureID: account.FeatureHistory,
			ConfigKey:         "history/enable",
			ConfigScope:       "history/",
			InPackage:         packagePlus,
			icon: `
<svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
  <path stroke-linecap="round" stroke-linejoin="round"
    d="M12 6.042A8.967 8.967 0 006 3.75c-1.052 0-2.062.18-3 .512v14.25A8.987 8.987 0 016 18c2.305 0 4.408.867 6 2.292m0-14.25a8.966 8.966 0 016-2.292c1.052 0 2.062.18 3 .512v14.25A8.987 8.987 0 0018 18a8.967 8.967 0 00-6 2.292m0-14.25v14.25" />
</svg>	
			`,
		},
		{
			Name:              "Bandwidth Visibility",
			ID:                string(account.FeatureBWVis),
			RequiredFeatureID: account.FeatureBWVis,
			InPackage:         packagePlus,
			Beta:              true,
			icon: `
    <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
      <path stroke-linecap="round" stroke-linejoin="round"
        d="M3 13.125C3 12.504 3.504 12 4.125 12h2.25c.621 0 1.125.504 1.125 1.125v6.75C7.5 20.496 6.996 21 6.375 21h-2.25A1.125 1.125 0 013 19.875v-6.75zM9.75 8.625c0-.621.504-1.125 1.125-1.125h2.25c.621 0 1.125.504 1.125 1.125v11.25c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 01-1.125-1.125V8.625zM16.5 4.125c0-.621.504-1.125 1.125-1.125h2.25C20.496 3 21 3.504 21 4.125v15.75c0 .621-.504 1.125-1.125 1.125h-2.25a1.125 1.125 0 01-1.125-1.125V4.125z" />
    </svg>
			`,
		},
		{
			Name:              "Safing Support",
			ID:                string(account.FeatureSafingSupport),
			RequiredFeatureID: account.FeatureSafingSupport,
			InPackage:         packagePlus,
			icon: `
    <svg xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24" stroke-width="1.5" stroke="currentColor">
      <path stroke-linecap="round" stroke-linejoin="round"
        d="M15.75 6a3.75 3.75 0 11-7.5 0 3.75 3.75 0 017.5 0zM4.501 20.118a7.5 7.5 0 0114.998 0A17.933 17.933 0 0112 21.75c-2.676 0-5.216-.584-7.499-1.632z" />
    </svg>	
			`,
		},
		{
			Name:              "Safing Privacy Network",
			ID:                string(account.FeatureSPN),
			RequiredFeatureID: account.FeatureSPN,
			ConfigKey:         "spn/enable",
			ConfigScope:       "spn/",
			InPackage:         packagePro,
			icon: `
    <svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" stroke="currentColor" class="text-green-300">
      <g fill="none" stroke-linecap="round" stroke-linejoin="round" stroke-width="1.5">
        <path
          d="M6.488 15.581c.782.781.782 2.048 0 2.829-.782.781-2.049.781-2.83 0-.782-.781-.782-2.048 0-2.829.781-.781 2.048-.781 2.83 0M13.415 3.586c.782.781.782 2.048 0 2.829-.782.781-2.049.781-2.83 0-.782-.781-.782-2.048 0-2.829.781-.781 2.049-.781 2.83 0M20.343 15.58c.782.781.782 2.048 0 2.829-.782.781-2.049.781-2.83 0-.782-.781-.782-2.048 0-2.829.781-.781 2.048-.781 2.83 0">
        </path>
        <path
          d="M17.721 18.581C16.269 20.071 14.246 21 12 21c-1.146 0-2.231-.246-3.215-.68M4.293 15.152c-.56-1.999-.352-4.21.769-6.151.574-.995 1.334-1.814 2.205-2.449M13.975 5.254c2.017.512 3.834 1.799 4.957 3.743.569.985.899 2.041 1.018 3.103">
        </path>
      </g>
    </svg>
			`,
		},
	}
)
