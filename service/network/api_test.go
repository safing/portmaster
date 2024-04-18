package network

import (
	"fmt"
	"net"
	"testing"

	"github.com/safing/portmaster/service/intel"
)

func TestDebugInfoLineFormatting(t *testing.T) {
	t.Parallel()

	for _, conn := range connectionTestData {
		fmt.Println(conn.debugInfoLine())
	}
}

func TestDebugInfoFormatting(t *testing.T) {
	t.Parallel()

	fmt.Println(buildNetworkDebugInfoData(connectionTestData))
}

var connectionTestData = []*Connection{
	{
		ID:         "17-255.255.255.255-29810-192.168.0.23-40672",
		Scope:      "IL",
		IPVersion:  4,
		Inbound:    true,
		IPProtocol: 17,
		LocalIP:    net.ParseIP("255.255.255.255"),
		LocalPort:  29810,
		Entity: &intel.Entity{
			Protocol:      17,
			Port:          40672,
			Domain:        "",
			ReverseDomain: "",
			IP:            net.ParseIP("192.168.0.23"),
			Country:       "",
			ASN:           0,
		},
		Verdict: 2,
		Reason: Reason{
			Msg:       "incoming connection blocked by default",
			OptionKey: "filter/serviceEndpoints",
			Profile:   "",
		},
		Started:          1614010349,
		Ended:            1614010350,
		VerdictPermanent: true,
		Inspecting:       false,
		Tunneled:         false,
		Encrypted:        false,
		ProcessContext: ProcessContext{
			ProcessName: "Unidentified Processes",
			ProfileName: "Unidentified Processes",
			BinaryPath:  "",
			PID:         -1,
			Profile:     "_unidentified",
			Source:      "local",
		},
		Internal:               false,
		ProfileRevisionCounter: 1,
	},
	{
		ID:         "6-192.168.0.176-55216-13.32.6.15-80",
		Scope:      "PI",
		IPVersion:  4,
		Inbound:    false,
		IPProtocol: 6,
		LocalIP:    net.ParseIP("192.168.0.176"),
		LocalPort:  55216,
		Entity: &intel.Entity{
			Protocol:      6,
			Port:          80,
			Domain:        "",
			ReverseDomain: "",
			IP:            net.ParseIP("13.32.6.15"),
			Country:       "DE",
			ASN:           16509,
		},
		Verdict: 2,
		Reason: Reason{
			Msg:       "default permit",
			OptionKey: "filter/defaultAction",
			Profile:   "",
		},
		Started:          1614010475,
		Ended:            1614010565,
		VerdictPermanent: true,
		Inspecting:       false,
		Tunneled:         false,
		Encrypted:        false,
		ProcessContext: ProcessContext{
			ProcessName: "NetworkManager",
			ProfileName: "Network Manager",
			BinaryPath:  "/usr/sbin/NetworkManager",
			PID:         1273,
			Profile:     "3a9b0eb5-c7fe-4bc7-9b93-a90f4ff84b5b",
			Source:      "local",
		},
		Internal:               true,
		ProfileRevisionCounter: 1,
	},
	{
		ID:         "6-192.168.0.176-49982-142.250.74.211-443",
		Scope:      "pkg.go.dev.",
		IPVersion:  4,
		Inbound:    false,
		IPProtocol: 6,
		LocalIP:    net.ParseIP("192.168.0.176"),
		LocalPort:  49982,
		Entity: &intel.Entity{
			Protocol:      6,
			Port:          443,
			Domain:        "pkg.go.dev.",
			ReverseDomain: "",
			CNAME: []string{
				"ghs.googlehosted.com.",
			},
			IP:      net.ParseIP("142.250.74.211"),
			Country: "US",
			ASN:     15169,
		},
		Verdict: 2,
		Reason: Reason{
			Msg:       "default permit",
			OptionKey: "filter/defaultAction",
			Profile:   "",
		},
		Started:          1614010415,
		Ended:            1614010745,
		VerdictPermanent: true,
		Inspecting:       false,
		Tunneled:         false,
		Encrypted:        false,
		ProcessContext: ProcessContext{
			ProcessName: "firefox",
			ProfileName: "Firefox",
			BinaryPath:  "/usr/bin/firefox",
			PID:         5710,
			Profile:     "74b30392-9e4d-4157-83a9-fffafd3e2bde",
			Source:      "local",
		},
		Internal:               false,
		ProfileRevisionCounter: 1,
	},
}
