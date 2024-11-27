package log

import (
	"fmt"
	"os"
	"runtime/debug"
	"sync"
	"time"
)

type (
	// Adapter is used to write logs.
	Adapter interface {
		// Write is called for each log message.
		Write(msg Message, duplicates uint64)
	}

	// AdapterFunc is a convenience type for implementing
	// Adapter.
	AdapterFunc func(msg Message, duplicates uint64)

	// FormatFunc formats msg into a string.
	FormatFunc func(msg Message, duplicates uint64) string

	// SimpleFileAdapter implements Adapter and writes all
	// messages to File.
	SimpleFileAdapter struct {
		Format FormatFunc
		File   *os.File
	}
)

var (
	// StdoutAdapter is a simple file adapter that writes
	// all logs to os.Stdout using a predefined format.
	StdoutAdapter = &SimpleFileAdapter{
		File:   os.Stdout,
		Format: defaultColorFormater,
	}

	// StderrAdapter is a simple file adapter that writes
	// all logs to os.Stdout using a predefined format.
	StderrAdapter = &SimpleFileAdapter{
		File:   os.Stderr,
		Format: defaultColorFormater,
	}
)

var (
	adapter Adapter = StdoutAdapter

	schedulingEnabled = false
	writeTrigger      = make(chan struct{})
)

// SetAdapter configures the logging adapter to use.
// This must be called before the log package is initialized.
func SetAdapter(a Adapter) {
	if initializing.IsSet() || a == nil {
		return
	}

	adapter = a
}

// Write implements Adapter and calls fn.
func (fn AdapterFunc) Write(msg Message, duplicates uint64) {
	fn(msg, duplicates)
}

// Write implements Adapter and writes msg the underlying file.
func (fileAdapter *SimpleFileAdapter) Write(msg Message, duplicates uint64) {
	fmt.Fprintln(fileAdapter.File, fileAdapter.Format(msg, duplicates))
}

// EnableScheduling enables external scheduling of the logger. This will require to manually trigger writes via TriggerWrite whenevery logs should be written. Please note that full buffers will also trigger writing. Must be called before Start() to have an effect.
func EnableScheduling() {
	if !initializing.IsSet() {
		schedulingEnabled = true
	}
}

// TriggerWriter triggers log output writing.
func TriggerWriter() {
	if started.IsSet() && schedulingEnabled {
		select {
		case writeTrigger <- struct{}{}:
		default:
		}
	}
}

// TriggerWriterChannel returns the channel to trigger log writing. Returned channel will close if EnableScheduling() is not called correctly.
func TriggerWriterChannel() chan struct{} {
	return writeTrigger
}

func defaultColorFormater(line Message, duplicates uint64) string {
	return formatLine(line.(*logLine), duplicates, true) //nolint:forcetypeassert // TODO: improve
}

func startWriter() {
	fmt.Printf(
		"%s%s%s %sBOF %s%s\n",

		dimColor(),
		time.Now().Format(timeFormat),
		endDimColor(),

		blueColor(),
		rightArrow,
		endColor(),
	)

	shutdownWaitGroup.Add(1)
	go writerManager()
}

func writerManager() {
	defer shutdownWaitGroup.Done()

	for {
		err := writer()
		if err != nil {
			Errorf("log: writer failed: %s", err)
		} else {
			return
		}
	}
}

// defer should be able to edit the err. So naked return is required.
// nolint:golint,nakedret
func writer() (err error) {
	defer func() {
		// recover from panic
		panicVal := recover()
		if panicVal != nil {
			err = fmt.Errorf("%s", panicVal)

			// write stack to stderr
			fmt.Fprintf(
				os.Stderr,
				`===== Error Report =====
Message: %s
StackTrace:

%s
===== End of Report =====
`,
				err,
				string(debug.Stack()),
			)
		}
	}()

	var currentLine *logLine
	var duplicates uint64

	for {
		// reset
		currentLine = nil
		duplicates = 0

		// wait until logs need to be processed
		select {
		case <-logsWaiting: // normal process
			logsWaitingFlag.UnSet()
		case <-forceEmptyingOfBuffer: // log buffer is full!
		case <-shutdownSignal: // shutting down
			finalizeWriting()
			return
		}

		// wait for timeslot to log
		select {
		case <-writeTrigger: // normal process
		case <-forceEmptyingOfBuffer: // log buffer is full!
		case <-shutdownSignal: // shutting down
			finalizeWriting()
			return
		}

		// write all the logs!
	writeLoop:
		for {
			select {
			case nextLine := <-logBuffer:
				// first line we process, just assign to currentLine
				if currentLine == nil {
					currentLine = nextLine
					continue writeLoop
				}

				// we now have currentLine and nextLine

				// if currentLine and nextLine are equal, do not print, just increase counter and continue
				if nextLine.Equal(currentLine) {
					duplicates++
					continue writeLoop
				}

				// if currentLine and line are _not_ equal, output currentLine
				adapter.Write(currentLine, duplicates)
				// add to unexpected logs
				addUnexpectedLogs(currentLine)
				// reset duplicate counter
				duplicates = 0
				// set new currentLine
				currentLine = nextLine
			default:
				break writeLoop
			}
		}

		// write final line
		if currentLine != nil {
			adapter.Write(currentLine, duplicates)
			// add to unexpected logs
			addUnexpectedLogs(currentLine)
		}

		// back down a little
		select {
		case <-time.After(10 * time.Millisecond):
		case <-shutdownSignal:
			finalizeWriting()
			return
		}

	}
}

func finalizeWriting() {
	for {
		select {
		case line := <-logBuffer:
			adapter.Write(line, 0)
		case <-time.After(10 * time.Millisecond):
			fmt.Printf(
				"%s%s%s %sEOF %s%s\n",

				dimColor(),
				time.Now().Format(timeFormat),
				endDimColor(),

				blueColor(),
				leftArrow,
				endColor(),
			)
			return
		}
	}
}

// Last Unexpected Logs

var (
	lastUnexpectedLogs      [10]string
	lastUnexpectedLogsIndex int
	lastUnexpectedLogsLock  sync.Mutex
)

func addUnexpectedLogs(line *logLine) {
	// Add main line.
	if line.level >= WarningLevel {
		addUnexpectedLogLine(line)
		return
	}

	// Check for unexpected lines in the tracer.
	if line.tracer != nil {
		for _, traceLine := range line.tracer.logs {
			if traceLine.level >= WarningLevel {
				// Add full trace.
				addUnexpectedLogLine(line)
				return
			}
		}
	}
}

func addUnexpectedLogLine(line *logLine) {
	lastUnexpectedLogsLock.Lock()
	defer lastUnexpectedLogsLock.Unlock()

	// Format line and add to logs.
	lastUnexpectedLogs[lastUnexpectedLogsIndex] = formatLine(line, 0, false)

	// Increase index and wrap back to start.
	lastUnexpectedLogsIndex = (lastUnexpectedLogsIndex + 1) % len(lastUnexpectedLogs)
}

// GetLastUnexpectedLogs returns the last 10 log lines of level Warning an up.
func GetLastUnexpectedLogs() []string {
	lastUnexpectedLogsLock.Lock()
	defer lastUnexpectedLogsLock.Unlock()

	// Make a copy and return.
	logsLen := len(lastUnexpectedLogs)
	start := lastUnexpectedLogsIndex
	logsCopy := make([]string, 0, logsLen)
	// Loop from mid-to-mid.
	for i := start; i < start+logsLen; i++ {
		if lastUnexpectedLogs[i%logsLen] != "" {
			logsCopy = append(logsCopy, lastUnexpectedLogs[i%logsLen])
		}
	}

	return logsCopy
}
