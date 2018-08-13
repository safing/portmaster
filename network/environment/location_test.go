// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

// +build root

package environment

import "testing"

func TestGetApproximateInternetLocation(t *testing.T) {
	ip, err := GetApproximateInternetLocation()
	if err != nil {
		t.Errorf("GetApproximateInternetLocation failed: %s", err)
	}
	t.Logf("GetApproximateInternetLocation: %s", ip.String())
}
