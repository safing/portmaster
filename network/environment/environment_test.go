// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package environment

import "testing"

func TestEnvironment(t *testing.T) {

	connectivityTest := Connectivity()
	t.Logf("connectivity: %v", connectivityTest)

	nameserversTest, err := getNameserversFromResolvconf()
	if err != nil {
		t.Errorf("failed to get namerservers from resolvconf: %s", err)
	}
	t.Logf("nameservers from resolvconf: %v", nameserversTest)

	nameserversTest = Nameservers()
	t.Logf("nameservers: %v", nameserversTest)

	gatewaysTest := Gateways()
	t.Logf("gateways: %v", gatewaysTest)

}
