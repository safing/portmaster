package netenv

import "testing"

func TestEnvironment(t *testing.T) {
	t.Parallel()

	nameserversTest := Nameservers()
	t.Logf("nameservers: %+v", nameserversTest)

	gatewaysTest := Gateways()
	t.Logf("gateways: %+v", gatewaysTest)
}
