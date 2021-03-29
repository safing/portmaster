package netenv

import (
	"flag"
	"testing"
)

var (
	privileged bool
)

func init() {
	flag.BoolVar(&privileged, "privileged", false, "run tests that require root/admin privileges")
}

func TestGetApproximateInternetLocation(t *testing.T) {
	if testing.Short() {
		t.Skip()
	}
	if !privileged {
		t.Skip("skipping privileged test, active with -privileged argument")
	}

	loc, err := GetInternetLocation()
	if err != nil {
		t.Fatalf("GetApproximateInternetLocation failed: %s", err)
	}
	t.Logf("GetApproximateInternetLocation: %+v", loc)
}
