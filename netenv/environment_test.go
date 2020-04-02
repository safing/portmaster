// +build linux

package netenv

import "testing"

func TestEnvironment(t *testing.T) {

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
