package ebpf

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/cilium/ebpf/link"
	"github.com/cilium/ebpf/ringbuf"
	"github.com/cilium/ebpf/rlimit"
	"github.com/hashicorp/go-multierror"
	"golang.org/x/sys/unix"

	"github.com/safing/portmaster/base/log"
)

//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -Werror" bpf ../programs/exec.c

// These constants are defined in `bpf/handler.c` and must be kept in sync.
const (
	arglen  = 32
	argsize = 1024
)

var errTracerClosed = errors.New("tracer is closed")

// event contains details about each exec call, sent from the eBPF program to
// userspace through a perf ring buffer. This type must be kept in sync with
// `event_t` in `bpf/handler.c`.
type event struct {
	// Details about the process being launched.
	Filename [argsize]byte
	Argv     [arglen][argsize]byte
	Argc     uint32
	UID      uint32
	GID      uint32
	PID      uint32

	// Name of the calling process.
	Comm [argsize]byte
}

// Event contains data about each exec event with many fields for easy
// filtering and logging.
type Event struct {
	Filename string `json:"filename"`
	// Argv contains the raw argv supplied to the process, including argv[0]
	// (which is equal to `filepath.Base(e.Filename)` in most circumstances).
	Argv []string `json:"argv"`
	// Truncated is true if we were unable to read all process arguments into
	// Argv because there were more than ARGLEN arguments.
	Truncated bool `json:"truncated"`

	// These values are of the new process. Keep in mind that the exec call may
	// fail and the PID will be released in such a case.
	PID uint32 `json:"pid"`
	UID uint32 `json:"uid"`
	GID uint32 `json:"gid"`

	// Comm is the "name" of the parent process, usually the filename of the
	// executable (but not always).
	Comm string `json:"comm"`
}

// Tracer is the exec tracer itself.
// It must be closed after use.
type Tracer struct {
	objs bpfObjects
	tp   link.Link
	rb   *ringbuf.Reader

	closeLock sync.Mutex
	closed    chan struct{}
}

// New instantiates all of the BPF objects into the running kernel, starts
// tracing, and returns the created Tracer. After calling this successfully, the
// caller should immediately attach a for loop running `h.Read()`.
//
// The returned Tracer MUST be closed when not needed anymore otherwise kernel
// resources may be leaked.
func New() (*Tracer, error) {
	t := &Tracer{
		tp: nil,
		rb: nil,

		closeLock: sync.Mutex{},
		closed:    make(chan struct{}),
	}

	if err := loadBpfObjects(&t.objs, nil); err != nil {
		return nil, fmt.Errorf("ebpf: failed to load ebpf object: %w", err)
	}

	if err := t.start(); err != nil {
		// Best effort.
		_ = t.Close()
		return nil, fmt.Errorf("start tracer: %w", err)
	}

	// It could be very bad if someone forgot to close this, so we'll try to
	// detect when it doesn't get closed and log a warning.
	stack := debug.Stack()
	runtime.SetFinalizer(t, func(t *Tracer) {
		err := t.Close()
		if errors.Is(err, errTracerClosed) {
			return
		}

		log.Infof("tracer was finalized but was not closed, created at: %s", stack)
		log.Infof("tracers must be closed when finished with to avoid leaked kernel resources")
		if err != nil {
			log.Errorf("closing tracer failed: %+v", err)
		}
	})

	return t, nil
}

// start loads the eBPF programs and maps into the kernel and starts them.
// You should immediately attach a for loop running `h.Read()` after calling
// this successfully.
func (t *Tracer) start() error {
	// If we don't startup successfully, we need to make sure all of the
	// stuff is cleaned up properly or we'll be leaking kernel resources.
	ok := false
	defer func() {
		if !ok {
			// Best effort.
			_ = t.Close()
		}
	}()

	// Allow the current process to lock memory for eBPF resources. This
	// does nothing on 5.11+ kernels which don't need this.
	err := rlimit.RemoveMemlock()
	if err != nil {
		return fmt.Errorf("remove memlock: %w", err)
	}

	// Attach the eBPF program to the `sys_enter_execve` tracepoint, which
	// is triggered at the beginning of each `execve()` syscall.
	t.tp, err = link.Tracepoint("syscalls", "sys_enter_execve", t.objs.EnterExecve, nil)
	if err != nil {
		return fmt.Errorf("open tracepoint: %w", err)
	}

	// Create the reader for the event ringbuf.
	t.rb, err = ringbuf.NewReader(t.objs.PmExecMap)
	if err != nil {
		return fmt.Errorf("open ringbuf reader: %w", err)
	}

	ok = true
	return nil
}

// Read reads an event from the eBPF program via the ringbuf, parses it and
// returns it. If the *tracer is closed during the blocked call, and error that
// wraps io.EOF will be returned.
func (t *Tracer) Read() (*Event, error) {
	rb := t.rb
	if rb == nil {
		return nil, errors.New("ringbuf reader is not initialized, tracer may not be open or may have been closed")
	}

	record, err := rb.Read()
	if err != nil {
		if errors.Is(err, ringbuf.ErrClosed) {
			return nil, fmt.Errorf("tracer closed: %w", io.EOF)
		}

		return nil, fmt.Errorf("read from ringbuf: %w", err)
	}

	// Parse the ringbuf event entry into an event structure.
	var rawEvent event
	err = binary.Read(bytes.NewBuffer(record.RawSample), binary.NativeEndian, &rawEvent)
	if err != nil {
		return nil, fmt.Errorf("parse raw ringbuf entry into event struct: %w", err)
	}

	ev := &Event{
		Filename:  unix.ByteSliceToString(rawEvent.Filename[:]),
		Argv:      []string{}, // populated below
		Truncated: rawEvent.Argc == arglen+1,
		PID:       rawEvent.PID,
		UID:       rawEvent.UID,
		GID:       rawEvent.GID,
		Comm:      unix.ByteSliceToString(rawEvent.Comm[:]),
	}

	// Copy only the args we're allowed to read from the array. If we read more
	// than rawEvent.Argc, we could be copying non-zeroed memory.
	argc := int(rawEvent.Argc)
	if argc > arglen {
		argc = arglen
	}
	for i := range argc {
		str := unix.ByteSliceToString(rawEvent.Argv[i][:])
		if strings.TrimSpace(str) != "" {
			ev.Argv = append(ev.Argv, str)
		}
	}

	return ev, nil
}

// Close gracefully closes and frees all resources associated with the eBPF
// tracepoints, maps and other resources. Any blocked `Read()` operations will
// return an error that wraps `io.EOF`.
func (t *Tracer) Close() error {
	t.closeLock.Lock()
	defer t.closeLock.Unlock()
	select {
	case <-t.closed:
		return errTracerClosed
	default:
	}
	close(t.closed)
	runtime.SetFinalizer(t, nil)

	// Close everything started in h.Start() in reverse order.
	var merr error
	if t.rb != nil {
		err := t.rb.Close()
		if err != nil {
			merr = multierror.Append(merr, fmt.Errorf("close ringbuf reader: %w", err))
		}
	}
	if t.tp != nil {
		err := t.tp.Close()
		if err != nil {
			merr = multierror.Append(merr, fmt.Errorf("close tracepoint: %w", err))
		}
	}
	err := t.objs.Close()
	if err != nil {
		merr = multierror.Append(merr, fmt.Errorf("close eBPF objects: %w", err))
	}

	return merr
}
