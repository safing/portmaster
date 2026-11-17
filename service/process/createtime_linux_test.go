package process

import (
	"bufio"
	"context"
	"os"
	"strconv"
	"strings"
	"testing"

	processInfo "github.com/shirou/gopsutil/process"
)

func TestBootTimeFromClocks(t *testing.T) {
	t.Parallel()

	f, err := os.Open("/proc/stat")
	if err != nil {
		t.Skip(err)
	}
	defer func() { _ = f.Close() }()
	var btime uint64
	scanner := bufio.NewScanner(f)
	scanner.Buffer(nil, 1<<20)
	for scanner.Scan() {
		if value, ok := strings.CutPrefix(scanner.Text(), "btime "); ok {
			btime, err = strconv.ParseUint(value, 10, 64)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	if btime == 0 {
		t.Skip("no btime in /proc/stat")
	}

	bootTime, ok := bootTimeFromClocks()
	if !ok {
		// Only fails if the bracket spans a second boundary, which is very
		// unlikely: Try again.
		bootTime, ok = bootTimeFromClocks()
	}
	if !ok {
		t.Fatal("failed to get boot time from clocks")
	}
	if bootTime != btime {
		t.Fatalf("boot time %d differs from btime %d", bootTime, btime)
	}
}

func TestFastCreateTime(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Skip(err)
	}
	var checked int
	for _, entry := range entries {
		pid, err := strconv.ParseInt(entry.Name(), 10, 32)
		if err != nil {
			continue
		}
		pInfo, err := processInfo.NewProcessWithContext(ctx, int32(pid))
		if err != nil {
			continue // Exited.
		}
		want, err := pInfo.CreateTimeWithContext(ctx)
		if err != nil {
			continue
		}
		got, ok := fastCreateTime(ctx, int32(pid))
		if !ok {
			_, err = os.Stat("/proc/" + entry.Name())
			if err != nil {
				continue // Exited.
			}
			t.Fatalf("fast create time failed for pid %d", pid)
		}
		if got != want {
			t.Fatalf("create time of pid %d: got %d, want %d", pid, got, want)
		}
		checked++
	}
	if checked == 0 {
		t.Skip("no processes checked")
	}
	t.Logf("checked %d processes", checked)
}

func TestReadStartTicks(t *testing.T) {
	t.Parallel()

	if _, ok := readStartTicks(-1); ok {
		t.Fatal("expected failure for invalid pid")
	}
	ownPID := int32(os.Getpid()) //nolint:gosec // PIDs fit into int32.
	if _, ok := readStartTicks(ownPID); !ok {
		t.Fatal("failed to read start ticks of own process")
	}
}
