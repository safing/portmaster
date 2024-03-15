package main

import (
	"bytes"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/spn/hub"
	"github.com/safing/portmaster/spn/navigator"
)

var observedTestChange = &observedChange{
	Title: "Hub Changed: fogos (8uLe-zUkC)",
	Summary: `ConnectedTo.ZwqBAzGqifBAFKFW1GQijNM18pi7BnWH34GyKBF7KB5fC5.HubID removed ZwqBAzGqifBAFKFW1GQijNM18pi7BnWH34GyKBF7KB5fC5
	ConnectedTo.ZwqBAzGqifBAFKFW1GQijNM18pi7BnWH34GyKBF7KB5fC5.Capacity removed 3403661
	ConnectedTo.ZwqBAzGqifBAFKFW1GQijNM18pi7BnWH34GyKBF7KB5fC5.Latency removed 252.350006ms`,
	UpdatedPin: &navigator.PinExport{
		ID:        "Zwtb8EKMatnMRkW1VaLh8CPV3QswD9iuRU4Sda8uLezUkC",
		Name:      "fogos",
		Map:       "main",
		FirstSeen: time.Now(),
		EntityV4: &intel.Entity{
			IP:      net.IPv4(138, 201, 140, 70),
			IPScope: netutils.Global,
			Country: "DE",
			ASN:     24940,
			ASOrg:   "Hetzner Online GmbH",
		},
		States:        []string{"HasRequiredInfo", "Reachable", "Active", "Trusted"},
		VerifiedOwner: "Safing",
		HopDistance:   3,
		SessionActive: false,
		Info: &hub.Announcement{
			ID:             "Zwtb8EKMatnMRkW1VaLh8CPV3QswD9iuRU4Sda8uLezUkC",
			Timestamp:      1677682008,
			Name:           "fogos",
			Group:          "Safing",
			ContactAddress: "abuse@safing.io",
			ContactService: "email",
			Hosters:        []string{"Hetzner"},
			Datacenter:     "DE-Hetzner-FSN",
			IPv4:           net.IPv4(138, 201, 140, 70),
			IPv6:           net.ParseIP("2a01:4f8:172:3753::2"),
			Transports:     []string{"tcp:17", "tcp:17017"},
			Entry:          []string{},
			Exit:           []string{"- * TCP/25"},
		},
		Status: &hub.Status{
			Timestamp: 1694180778,
			Version:   "0.6.19 ",
		},
	},
	UpdateTime: time.Now(),
}

func TestNotificationTemplate(t *testing.T) {
	t.Parallel()

	fmt.Println("==========\nFound templates:")
	for _, tpl := range templates.Templates() {
		fmt.Println(tpl.Name())
	}
	fmt.Println("")

	fmt.Println("\n\n==========\nMatrix template:")
	matrixOutput := &bytes.Buffer{}
	err := templates.ExecuteTemplate(matrixOutput, "matrix-notification", observedTestChange)
	if err != nil {
		t.Errorf("failed to render matrix template: %s", err)
	}
	fmt.Println(matrixOutput.String())

	fmt.Println("\n\n==========\nDiscord template:")
	discordOutput := &bytes.Buffer{}
	err = templates.ExecuteTemplate(discordOutput, "discord-notification", observedTestChange)
	if err != nil {
		t.Errorf("failed to render discord template: %s", err)
	}
	fmt.Println(discordOutput.String())
}
