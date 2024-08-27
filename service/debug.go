package service

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"runtime"

	"github.com/maruel/panicparse/v2/stack"

	"github.com/safing/portmaster/base/utils/debug"
	"github.com/safing/portmaster/service/mgr"
)

// GetWorkerInfo returns the worker info of all running workers.
func (i *Instance) GetWorkerInfo() (*mgr.WorkerInfo, error) {
	snapshot, _, err := stack.ScanSnapshot(bytes.NewReader(fullStack()), io.Discard, stack.DefaultOpts())
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("get stack: %w", err)
	}

	infos := make([]*mgr.WorkerInfo, 0, 32)
	for _, m := range i.serviceGroup.Modules() {
		wi, _ := m.Manager().WorkerInfo(snapshot) // Does not fail when we provide a snapshot.
		infos = append(infos, wi)
	}
	for _, m := range i.SpnGroup.Modules() {
		wi, _ := m.Manager().WorkerInfo(snapshot) // Does not fail when we provide a snapshot.
		infos = append(infos, wi)
	}

	return mgr.MergeWorkerInfo(infos...), nil
}

// AddWorkerInfoToDebugInfo adds the worker info of all running workers to the debug info.
func (i *Instance) AddWorkerInfoToDebugInfo(di *debug.Info) {
	info, err := i.GetWorkerInfo()
	if err != nil {
		di.AddSection(
			"Worker Status Failed",
			debug.UseCodeSection,
			err.Error(),
		)
		return
	}

	di.AddSection(
		fmt.Sprintf("Worker Status: %d/%d (%d?)", info.Running, len(info.Workers), info.Missing+info.Other),
		debug.UseCodeSection,
		info.Format(),
	)
}

func fullStack() []byte {
	buf := make([]byte, 8096)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return buf[:n]
		}
		buf = make([]byte, 2*len(buf))
	}
}
