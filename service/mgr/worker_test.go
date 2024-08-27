package mgr

import (
	"fmt"
	"testing"
	"time"
)

func TestWorkerInfo(t *testing.T) { //nolint:paralleltest
	mgr := New("test")
	mgr.Go("test func one", testFunc1)
	mgr.Go("test func two", testFunc2)
	mgr.Go("test func three", testFunc3)
	defer mgr.Cancel()

	time.Sleep(100 * time.Millisecond)

	info, err := mgr.WorkerInfo(nil)
	if err != nil {
		t.Fatal(err)
	}
	if info.Waiting != 3 {
		t.Errorf("expected three waiting workers")
	}

	fmt.Printf("%+v\n", info)
}

func testFunc1(ctx *WorkerCtx) error {
	select {
	case <-time.After(1 * time.Second):
	case <-ctx.Done():
	}
	return nil
}

func testFunc2(ctx *WorkerCtx) error {
	select {
	case <-time.After(1 * time.Second):
	case <-ctx.Done():
	}
	return nil
}

func testFunc3(ctx *WorkerCtx) error {
	select {
	case <-time.After(1 * time.Second):
	case <-ctx.Done():
	}
	return nil
}
