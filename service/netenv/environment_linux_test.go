package netenv

import "testing"

func TestLinuxEnvironment(t *testing.T) {
	t.Parallel()

	nameserversTest, err := getNameserversFromResolvconf()
	if err != nil {
		t.Errorf("failed to get namerservers from resolvconf: %s", err)
	}
	t.Logf("nameservers from resolvconf: %+v", nameserversTest)
}
