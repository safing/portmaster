//go:build linux

package compat

import (
	"fmt"

	"github.com/coreos/go-iptables/iptables"
)

var (
	iptProtocols = []iptables.Protocol{
		iptables.ProtocolIPv4,
		iptables.ProtocolIPv6,
	}
	iptTables = []string{
		"filter",
		"nat",
		"mangle",
		"raw",
	}
)

// GetIPTablesChains returns the chain names currently in ip(6)tables.
func GetIPTablesChains() ([]string, error) {
	chains := make([]string, 0, 100)

	// Iterate over protocols.
	for _, protocol := range iptProtocols {
		if protocol == iptables.ProtocolIPv4 {
			chains = append(chains, "v4")
		} else {
			chains = append(chains, "v6")
		}

		// Get iptables access for protocol.
		tbls, err := iptables.NewWithProtocol(protocol)
		if err != nil {
			return nil, err
		}

		// Iterate over tables.
		for _, table := range iptTables {
			chains = append(chains, "  "+table)

			// Get chain names
			chainNames, err := tbls.ListChains(table)
			if err != nil {
				return nil, fmt.Errorf("failed to get chains of table %s: %w", table, err)
			}

			// Add chain names to list.
			for _, name := range chainNames {
				chains = append(chains, "    "+name)
			}
		}
	}

	return chains, nil
}
