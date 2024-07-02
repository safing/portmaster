package base

import (
	"flag"
	"fmt"
	"os"
	"runtime/pprof"

	"github.com/safing/portmaster/service/mgr"
)

var cpuProfile string

func init() {
	flag.StringVar(&cpuProfile, "cpuprofile", "", "write cpu profile to `file`")
}

func startProfiling() {
	if cpuProfile != "" {
		module.mgr.Go("cpu profiler", cpuProfiler)
	}
}

func cpuProfiler(ctx *mgr.WorkerCtx) error {
	f, err := os.Create(cpuProfile)
	if err != nil {
		return fmt.Errorf("could not create CPU profile: %w", err)
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		return fmt.Errorf("could not start CPU profile: %w", err)
	}

	// wait for shutdown
	<-ctx.Done()

	pprof.StopCPUProfile()
	err = f.Close()
	if err != nil {
		return fmt.Errorf("failed to close CPU profile file: %w", err)
	}
	return nil
}
