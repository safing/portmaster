// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

// +build !linux

package environment

func getNameserversFromDbus() ([]Nameserver, error) {
	var nameservers []Nameserver
	return nameservers, nil
}

func getConnectivityStateFromDbus() (uint8, error) {
	return UNKNOWN, nil
}
