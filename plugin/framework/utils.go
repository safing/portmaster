package framework

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/safing/portmaster/plugin/shared/decider"
	"github.com/safing/portmaster/plugin/shared/proto"
	"github.com/safing/portmaster/plugin/shared/reporter"
)

type (
	// DeciderFunc is a utility type to implement a decider.Decider using
	// a function only.
	//
	// It implements decider.Decider.
	DeciderFunc func(context.Context, *proto.Connection) (proto.Verdict, string, error)

	// ReporterFunc is a utility type to implement a reporter.Reporter using
	// a function only.
	//
	// It implements reporter.Reporter.
	ReporterFunc func(context.Context, *proto.Connection) error
)

var (
	getExecPathOnce        sync.Once
	executablePath         string
	resolvedExecutablePath string
	getExecError           error
)

// DecideOnConnection passes through to fn and implements decider.Decider.
func (fn DeciderFunc) DecideOnConnection(ctx context.Context, conn *proto.Connection) (proto.Verdict, string, error) {
	return fn(ctx, conn)
}

// ReportConnection passes through to fn and implements reporter.Reporter.
func (fn ReporterFunc) ReportConnection(ctx context.Context, conn *proto.Connection) error {
	return fn(ctx, conn)
}

// ChainDeciders is a utility method to register more than on decider in a plugin.
// It executes the deciders one after another and returns the first error encountered
// or the first verdict that is not VERDICT_UNDECIDED, VERDICT_UNDERTERMINABLE or VERDICT_FAILED.
//
// If a decider returns a nil error but VERDICT_FAILED ChainDeciders will still return a non-nil
// error.
func ChainDeciders(deciders ...decider.Decider) DeciderFunc {
	return func(ctx context.Context, c *proto.Connection) (proto.Verdict, string, error) {
		for idx, d := range deciders {
			verdict, reason, err := d.DecideOnConnection(ctx, c)
			if err != nil {
				return verdict, reason, err
			}

			switch verdict {
			case proto.Verdict_VERDICT_UNDECIDED,
				proto.Verdict_VERDICT_UNDETERMINABLE:
				continue

			case proto.Verdict_VERDICT_FAILED:
				return verdict, reason, fmt.Errorf("chained decider at index %d return VERDICT_FAILED", idx)

			default:
				return verdict, reason, nil
			}
		}

		return proto.Verdict_VERDICT_UNDECIDED, "", nil
	}
}

// ChainDeciderFunc is like ChainDeciders but accepts DeciderFunc instead of decider.Decider.
// This is mainly for convenience to avoid casting to DeciderFunc for each parameter passed
// to ChainDecider.
func ChainDeciderFunc(fns ...DeciderFunc) DeciderFunc {
	deciders := make([]decider.Decider, len(fns))
	for idx, fn := range fns {
		deciders[idx] = fn
	}

	return ChainDeciders(deciders...)
}

// AllowPluginConnections returns a decider function that will allow outgoing and
// incoming connections to the plugin itself.
// This is mainly used in combination with ChainDecider or ChainDeciderFunc.
//
//	framework.RegisterDecider(framework.ChainDeciderFunc(
//		AllowPluginConnections(),
//		yourDeciderFunc,
//	))
//
func AllowPluginConnections() DeciderFunc {
	getExecPathOnce.Do(func() {
		executablePath, getExecError = os.Executable()
		if getExecError != nil {
			return
		}

		resolvedExecutablePath, getExecError = filepath.EvalSymlinks(executablePath)
	})

	return func(ctx context.Context, c *proto.Connection) (proto.Verdict, string, error) {
		binary := c.GetProcess().GetBinaryPath()

		if getExecError != nil {
			return proto.Verdict_VERDICT_UNDECIDED, "", fmt.Errorf("failed to get executable path: %s", getExecError)
		}

		if binary == resolvedExecutablePath || binary == executablePath {
			return proto.Verdict_VERDICT_ACCEPT, "own plugin connections are allowed", nil
		}

		return proto.Verdict_VERDICT_UNDECIDED, "", nil
	}
}

var (
	_ decider.Decider   = new(DeciderFunc)
	_ reporter.Reporter = new(ReporterFunc)
)
