package process

import (
	"bytes"
	"context"
	"os"
	"strconv"

	"github.com/shirou/gopsutil/host"
	processInfo "github.com/shirou/gopsutil/process"
	"golang.org/x/sys/unix"
)

// fastCreateTime returns the creation time of the process with the given PID
// exactly as gopsutil's Process.CreateTime computes it, but much cheaper.
//
// gopsutil computes the creation time from the start time in /proc/<pid>/stat
// and the boot time, which it reads from the btime line of /proc/stat on every
// call. The kernel generates all of /proc/stat (including the counters of
// every CPU and interrupt) for each read, which makes up most of the cost of
// looking up the process of a connection. Here, the boot time is computed
// from the clocks the kernel derives btime from instead.
//
// If ok is false, the fast path cannot be used and the caller must use
// gopsutil.
func fastCreateTime(ctx context.Context, pid int32) (createdAt int64, ok bool) {
	// gopsutil reads from HOST_PROC, if set, and computes the boot time
	// differently in containers.
	if os.Getenv("HOST_PROC") != "" {
		return 0, false
	}
	system, role, err := host.VirtualizationWithContext(ctx) // Cached by gopsutil.
	if err != nil || role == "guest" && (system == "lxc" || system == "docker") {
		return 0, false
	}

	startTicks, ok := readStartTicks(pid)
	if !ok {
		return 0, false
	}
	bootTime, ok := bootTimeFromClocks()
	if !ok {
		return 0, false
	}

	// Same computation as gopsutil.
	ctime := (startTicks / uint64(processInfo.ClockTicks)) + bootTime //nolint:gosec // Same conversion as gopsutil.
	return int64(ctime * 1000), true                                  //nolint:gosec // Same conversion as gopsutil.
}

// readStartTicks returns the start time of the process in clock ticks after
// boot (field 22 of /proc/<pid>/stat).
func readStartTicks(pid int32) (uint64, bool) {
	contents, err := os.ReadFile("/proc/" + strconv.Itoa(int(pid)) + "/stat")
	if err != nil {
		return 0, false
	}

	// The process name (field 2) is in parentheses and may contain spaces and
	// parentheses: Continue after the last closing parenthesis, as gopsutil does.
	nameEnd := bytes.LastIndexByte(contents, ')')
	if nameEnd < 0 || nameEnd+2 > len(contents) {
		return 0, false
	}
	rest := contents[nameEnd+2:]

	// The rest starts with field 3: Find field 22.
	const startTimeIndex = 22 - 3
	var index int
	for {
		// Skip whitespace.
		for len(rest) > 0 && isStatSpace(rest[0]) {
			rest = rest[1:]
		}
		if len(rest) == 0 {
			return 0, false
		}
		// Find end of field.
		end := 0
		for end < len(rest) && !isStatSpace(rest[end]) {
			end++
		}
		if index == startTimeIndex {
			startTicks, err := strconv.ParseUint(string(rest[:end]), 10, 64)
			if err != nil {
				return 0, false
			}
			return startTicks, true
		}
		rest = rest[end:]
		index++
	}
}

func isStatSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\v', '\f', '\r':
		return true
	default:
		return false
	}
}

// bootTimeFromClocks returns the boot time in seconds since the epoch, like
// the btime line of /proc/stat.
//
// The kernel computes btime as the difference of CLOCK_REALTIME and
// CLOCK_BOOTTIME (in the time namespace of the reader), truncated to seconds.
// The two clocks cannot be read at the same instant, so the realtime clock is
// read between two reads of the boottime clock, which brackets the exact
// difference. If the bracket spans a full second, the result is ambiguous and
// ok is false.
func bootTimeFromClocks() (bootTime uint64, ok bool) {
	var boot1, realtime, boot2 unix.Timespec
	if unix.ClockGettime(unix.CLOCK_BOOTTIME, &boot1) != nil ||
		unix.ClockGettime(unix.CLOCK_REALTIME, &realtime) != nil ||
		unix.ClockGettime(unix.CLOCK_BOOTTIME, &boot2) != nil {
		return 0, false
	}

	upper := realtime.Nano() - boot1.Nano()
	lower := realtime.Nano() - boot2.Nano()
	if lower <= 0 || lower/1e9 != upper/1e9 {
		return 0, false
	}
	return uint64(lower / 1e9), true //nolint:gosec // Checked to be positive.
}
