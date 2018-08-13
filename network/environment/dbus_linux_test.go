// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package environment

import "testing"

func TestDbus(t *testing.T) {
	nameservers, err := getNameserversFromDbus()
	if err != nil {
		t.Errorf("getNameserversFromDbus failed: %s", err)
	}
	t.Logf("getNameserversFromDbus: %v", nameservers)

	connectivityState, err := getConnectivityStateFromDbus()
	if err != nil {
		t.Errorf("getConnectivityStateFromDbus failed: %s", err)
	}
	t.Logf("getConnectivityStateFromDbus: %v", connectivityState)
}
