package mgr

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"

	"github.com/maruel/panicparse/v2/stack"
)

// WorkerInfoModule is used for interface checks on modules.
type WorkerInfoModule interface {
	WorkerInfo(s *stack.Snapshot) (*WorkerInfo, error)
}

func (m *Manager) registerWorker(w *WorkerCtx) {
	m.workersLock.Lock()
	defer m.workersLock.Unlock()

	// Iterate forwards over the ring buffer.
	end := (m.workersIndex - 1 + len(m.workers)) % len(m.workers)
	for {
		// Check if entry is available.
		if m.workers[m.workersIndex] == nil {
			m.workers[m.workersIndex] = w
			return
		}
		// Check if we checked the whole ring buffer.
		if m.workersIndex == end {
			break
		}
		// Go to next index.
		m.workersIndex = (m.workersIndex + 1) % len(m.workers)
	}

	// Increase ring buffer.
	newRingBuf := make([]*WorkerCtx, len(m.workers)*4)
	copy(newRingBuf, m.workers)
	// Add new entry.
	m.workersIndex = len(m.workers)
	newRingBuf[m.workersIndex] = w
	m.workersIndex++
	// Switch to new ring buffer.
	m.workers = newRingBuf
}

func (m *Manager) unregisterWorker(w *WorkerCtx) {
	m.workersLock.Lock()
	defer m.workersLock.Unlock()

	// Iterate backwards over the ring buffer.
	i := m.workersIndex
	end := (i + 1) % len(m.workers)
	for {
		// Check if entry is the one we want to remove.
		if m.workers[i] == w {
			m.workers[i] = nil
			return
		}
		// Check if we checked the whole ring buffer.
		if i == end {
			break
		}
		// Go to next index.
		i = (i - 1 + len(m.workers)) % len(m.workers)
	}
}

// WorkerInfo holds status information about a managers workers.
type WorkerInfo struct {
	Running int
	Waiting int

	Other   int
	Missing int

	Workers []*WorkerInfoDetail
}

// WorkerInfoDetail holds status information about a single worker.
type WorkerInfoDetail struct {
	Count       int
	State       string
	Mgr         string
	Name        string
	Func        string
	CurrentLine string
	ExtraInfo   string
}

// WorkerInfo returns status information for all running workers of this manager.
func (m *Manager) WorkerInfo(s *stack.Snapshot) (*WorkerInfo, error) {
	m.workersLock.Lock()
	defer m.workersLock.Unlock()

	var err error
	if s == nil {
		s, _, err = stack.ScanSnapshot(bytes.NewReader(fullStack()), io.Discard, stack.DefaultOpts())
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("get stack: %w", err)
		}
	}

	wi := &WorkerInfo{
		Workers: make([]*WorkerInfoDetail, 0, len(m.workers)),
	}

	// Go through all registered workers of manager.
	for _, w := range m.workers {
		// Ignore empty slots.
		if w == nil {
			continue
		}

		// Setup worker detail struct.
		wd := &WorkerInfoDetail{
			Count: 1,
			Mgr:   m.name,
		}
		if w.workerMgr != nil {
			wd.Name = w.workerMgr.name
			wd.Func = getFuncName(w.workerMgr.fn)
		} else {
			wd.Name = w.name
			wd.Func = getFuncName(w.workFunc)
		}

		// Search for stack of this worker.
	goroutines:
		for _, gr := range s.Goroutines {
			for _, call := range gr.Stack.Calls {
				// Check if the can find the worker function in a call stack.
				fullFuncName := call.Func.ImportPath + "." + call.Func.Name
				if fullFuncName == wd.Func {
					wd.State = gr.State

					// Find most useful line for where the goroutine currently is at.
					// Cut import path prefix to domain/user, eg. github.com/safing
					importPathPrefix := call.ImportPath
					splitted := strings.SplitN(importPathPrefix, "/", 3)
					if len(splitted) == 3 {
						importPathPrefix = splitted[0] + "/" + splitted[1] + "/"
					}
					// Find "last" call within that import path prefix.
					for _, call = range gr.Stack.Calls {
						if strings.HasPrefix(call.ImportPath, importPathPrefix) {
							wd.CurrentLine = call.ImportPath + "/" + call.SrcName + ":" + strconv.Itoa(call.Line)
							break
						}
					}
					// Fall back to last call if no better line was found.
					if wd.CurrentLine == "" {
						wd.CurrentLine = gr.Stack.Calls[0].ImportPath + "/" + gr.Stack.Calls[0].SrcName + ":" + strconv.Itoa(gr.Stack.Calls[0].Line)
					}

					// Add some extra info in some cases.
					if wd.State == "sleep" { //nolint:goconst
						wd.ExtraInfo = gr.SleepString()
					}

					break goroutines
				}
			}
		}

		// Summarize and add to list.
		switch wd.State {
		case "idle", "runnable", "running", "syscall",
			"waiting", "dead", "enqueue", "copystack":
			wi.Running++
		case "chan send", "chan receive", "select", "IO wait",
			"panicwait", "semacquire", "semarelease", "sleep",
			"sync.Mutex.Lock":
			wi.Waiting++
		case "":
			if w.workerMgr != nil {
				wi.Waiting++
				wd.State = "scheduled"
				wd.ExtraInfo = w.workerMgr.Status()
			} else {
				wi.Missing++
				wd.State = "missing"
			}
		default:
			wi.Other++
		}

		wi.Workers = append(wi.Workers, wd)
	}

	// Sort and return.
	wi.clean()
	return wi, nil
}

// Format formats the worker information as a readable table.
func (wi *WorkerInfo) Format() string {
	buf := bytes.NewBuffer(nil)

	// Add summary.
	buf.WriteString(fmt.Sprintf(
		"%d Workers: %d running, %d waiting\n\n",
		len(wi.Workers),
		wi.Running,
		wi.Waiting,
	))

	// Build table.
	tabWriter := tabwriter.NewWriter(buf, 4, 4, 3, ' ', 0)
	_, _ = fmt.Fprintf(tabWriter, "#\tState\tModule\tName\tWorker Func\tCurrent Line\tExtra Info\n")

	for _, wd := range wi.Workers {
		_, _ = fmt.Fprintf(tabWriter,
			"%d\t%s\t%s\t%s\t%s\t%s\t%s\n",
			wd.Count,
			wd.State,
			wd.Mgr,
			wd.Name,
			wd.Func,
			wd.CurrentLine,
			wd.ExtraInfo,
		)
	}
	_ = tabWriter.Flush()

	return buf.String()
}

func getFuncName(fn func(w *WorkerCtx) error) string {
	name := runtime.FuncForPC(reflect.ValueOf(fn).Pointer()).Name()
	return strings.TrimSuffix(name, "-fm")
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

// MergeWorkerInfo merges multiple worker infos into one.
func MergeWorkerInfo(infos ...*WorkerInfo) *WorkerInfo {
	// Calculate total registered workers.
	var totalWorkers int
	for _, status := range infos {
		totalWorkers += len(status.Workers)
	}

	// Merge all worker infos.
	wi := &WorkerInfo{
		Workers: make([]*WorkerInfoDetail, 0, totalWorkers),
	}
	for _, info := range infos {
		wi.Running += info.Running
		wi.Waiting += info.Waiting
		wi.Other += info.Other
		wi.Missing += info.Missing
		wi.Workers = append(wi.Workers, info.Workers...)
	}

	// Sort and return.
	wi.clean()
	return wi
}

func (wi *WorkerInfo) clean() {
	// Check if there is anything to do.
	if len(wi.Workers) <= 1 {
		return
	}

	// Sort for deduplication.
	slices.SortFunc(wi.Workers, sortWorkerInfoDetail)

	// Count duplicate worker details.
	current := wi.Workers[0]
	for i := 1; i < len(wi.Workers); i++ {
		if workerDetailsAreEqual(current, wi.Workers[i]) {
			current.Count++
		} else {
			current = wi.Workers[i]
		}
	}
	// Deduplicate worker details.
	wi.Workers = slices.CompactFunc(wi.Workers, workerDetailsAreEqual)

	// Sort for presentation.
	slices.SortFunc(wi.Workers, sortWorkerInfoDetailByCount)
}

// sortWorkerInfoDetail is a sort function to sort worker info details by their content.
func sortWorkerInfoDetail(a, b *WorkerInfoDetail) int {
	switch {
	case a.State != b.State:
		return strings.Compare(a.State, b.State)
	case a.Mgr != b.Mgr:
		return strings.Compare(a.Mgr, b.Mgr)
	case a.Name != b.Name:
		return strings.Compare(a.Name, b.Name)
	case a.Func != b.Func:
		return strings.Compare(a.Func, b.Func)
	case a.CurrentLine != b.CurrentLine:
		return strings.Compare(a.CurrentLine, b.CurrentLine)
	case a.ExtraInfo != b.ExtraInfo:
		return strings.Compare(a.ExtraInfo, b.ExtraInfo)
	case a.Count != b.Count:
		return b.Count - a.Count
	default:
		return 0
	}
}

// sortWorkerInfoDetailByCount is a sort function to sort worker info details by their count and then by content.
func sortWorkerInfoDetailByCount(a, b *WorkerInfoDetail) int {
	stateA, stateB := goroutineStateOrder(a.State), goroutineStateOrder(b.State)
	switch {
	case stateA != stateB:
		return stateA - stateB
	case a.State != b.State:
		return strings.Compare(a.State, b.State)
	case a.Count != b.Count:
		return b.Count - a.Count
	case a.Mgr != b.Mgr:
		return strings.Compare(a.Mgr, b.Mgr)
	case a.Name != b.Name:
		return strings.Compare(a.Name, b.Name)
	case a.Func != b.Func:
		return strings.Compare(a.Func, b.Func)
	case a.CurrentLine != b.CurrentLine:
		return strings.Compare(a.CurrentLine, b.CurrentLine)
	case a.ExtraInfo != b.ExtraInfo:
		return strings.Compare(a.ExtraInfo, b.ExtraInfo)
	default:
		return 0
	}
}

// workerDetailsAreEqual is a deduplication function for worker details.
func workerDetailsAreEqual(a, b *WorkerInfoDetail) bool {
	switch {
	case a.State != b.State:
		return false
	case a.Mgr != b.Mgr:
		return false
	case a.Name != b.Name:
		return false
	case a.Func != b.Func:
		return false
	case a.CurrentLine != b.CurrentLine:
		return false
	case a.ExtraInfo != b.ExtraInfo:
		return false
	default:
		return true
	}
}

//nolint:goconst
func goroutineStateOrder(state string) int {
	switch state {
	case "runnable", "running", "syscall":
		return 0 // Active.
	case "idle", "waiting", "dead", "enqueue", "copystack":
		return 1 // Active-ish.
	case "semacquire", "semarelease", "sleep", "panicwait", "sync.Mutex.Lock":
		return 2 // Bad (practice) blocking.
	case "chan send", "chan receive", "select":
		return 3 // Potentially bad (practice), but normal blocking.
	case "IO wait":
		return 4 // Normal blocking.
	case "scheduled":
		return 5 // Not running.
	case "missing", "":
		return 6 // Warning of undetected workers.
	default:
		return 9
	}
}
