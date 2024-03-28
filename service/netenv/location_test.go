package netenv

import (
	"flag"
	"testing"
)

var privileged bool

func init() {
	flag.BoolVar(&privileged, "privileged", false, "run tests that require root/admin privileges")
}

func TestGetInternetLocation(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip()
	}
	if !privileged {
		t.Skip("skipping privileged test, active with -privileged argument")
	}

	loc, ok := GetInternetLocation()
	if !ok {
		t.Fatal("GetApproximateInternetLocation failed")
	}
	t.Logf("GetApproximateInternetLocation: %+v", loc)
}
