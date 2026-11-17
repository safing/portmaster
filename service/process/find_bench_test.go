package process

import (
	"context"
	"os"
	"testing"

	"github.com/safing/portmaster/service/profile"
)

// BenchmarkGetProcessWithProfile measures the process attribution of a new
// connection of a process that is already known and has a profile, which is
// the case for almost all connections.
func BenchmarkGetProcessWithProfile(b *testing.B) {
	enableProcessDetection = func() bool { return true }
	ctx := context.Background()
	pid := os.Getpid()

	// Load the process once, as the first connection of a process does.
	proc, err := GetOrFindProcess(ctx, pid)
	if err != nil {
		b.Fatal(err)
	}
	proc.Lock()
	proc.setProfile("local/bench", &profile.LayeredProfile{})
	proc.Unlock()

	b.ReportAllocs()
	for b.Loop() {
		p, err := GetProcessWithProfile(ctx, pid)
		if err != nil {
			b.Fatal(err)
		}
		if p != proc {
			b.Fatal("unexpected process")
		}
	}
}
