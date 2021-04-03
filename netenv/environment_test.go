package netenv

import "testing"

func TestEnvironment(t *testing.T) {
	nameserversTest := Nameservers()
	t.Logf("nameservers: %+v", nameserversTest)

	gatewaysTest := Gateways()
	t.Logf("gateways: %+v", gatewaysTest)
}
