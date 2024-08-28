package service

import (
	"testing"
	"time"

	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/mgr"
)

func TestDebug(t *testing.T) {
	t.Parallel()

	// Create test instance with at least one worker.
	i := &Instance{}
	n, err := notifications.New(i)
	if err != nil {
		t.Fatal(err)
	}
	i.serviceGroup = mgr.NewGroup(n)
	i.SpnGroup = mgr.NewExtendedGroup()
	err = i.Start()
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)

	info, err := i.GetWorkerInfo()
	if err != nil {
		t.Fatal(err)
	}
	t.Log(info)
}
