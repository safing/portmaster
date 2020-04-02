// +build root

package netenv

import "testing"

func TestGetApproximateInternetLocation(t *testing.T) {
	ip, err := GetApproximateInternetLocation()
	if err != nil {
		t.Errorf("GetApproximateInternetLocation failed: %s", err)
	}
	t.Logf("GetApproximateInternetLocation: %s", ip.String())
}
