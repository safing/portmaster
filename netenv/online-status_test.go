package netenv

import (
	"context"
	"testing"
)

func TestCheckOnlineStatus(t *testing.T) {
	checkOnlineStatus(context.Background())
	t.Logf("online status: %s", GetOnlineStatus())
	t.Logf("captive portal: %+v", GetCaptivePortal())
}
