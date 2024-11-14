package log

import (
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	"github.com/safing/portmaster/base/info"
)

// Adapter is used to write logs.
type Adapter interface {
	// Write is called for each log message.
	WriteMessage(msg Message, duplicates uint64)
}

var (
	schedulingEnabled = false
	writeTrigger      = make(chan struct{})
)

// EnableScheduling enables external scheduling of the logger. This will require to manually trigger writes via TriggerWrite whenever logs should be written. Please note that full buffers will also trigger writing. Must be called before Start() to have an effect.
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

func startWriter() {
	if GlobalWriter.isStdout {
		fmt.Fprintf(GlobalWriter,
			"%s%s%s %sBOF %s%s\n",

			dimColor(),
			time.Now().Format(timeFormat),
			endDimColor(),

			blueColor(),
			rightArrow,
			endColor(),
		)
	} else {
		fmt.Fprintf(GlobalWriter,
			"%s BOF %s\n",
			time.Now().Format(timeFormat),
			rightArrow,
		)
	}
	writeVersion()

	shutdownWaitGroup.Add(1)
	go writerManager()
}

func writeVersion() {
	if GlobalWriter.isStdout {
		fmt.Fprintf(GlobalWriter, "%s%s%s running %s%s%s\n",
			dimColor(),
			time.Now().Format(timeFormat),
			endDimColor(),

			blueColor(),
			info.CondensedVersion(),
			endColor())
	} else {
		fmt.Fprintf(GlobalWriter, "%s running %s\n", time.Now().Format(timeFormat), info.CondensedVersion())
	}
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

func writer() error {
	var err error
	defer func() {
		// recover from panic
		panicVal := recover()
		if panicVal != nil {
			_, err = fmt.Fprintf(GlobalWriter, "%s", panicVal)

			// write stack to stderr
			fmt.Fprintf(
				GlobalWriter,
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
			return err
		}

		// wait for timeslot to log
		select {
		case <-writeTrigger: // normal process
		case <-forceEmptyingOfBuffer: // log buffer is full!
		case <-shutdownSignal: // shutting down
			finalizeWriting()
			return err
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
				GlobalWriter.WriteMessage(currentLine, duplicates)
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
			GlobalWriter.WriteMessage(currentLine, duplicates)
			// add to unexpected logs
			addUnexpectedLogs(currentLine)
		}

		// back down a little
		select {
		case <-time.After(10 * time.Millisecond):
		case <-shutdownSignal:
			finalizeWriting()
			return err
		}

	}
}

func finalizeWriting() {
	for {
		select {
		case line := <-logBuffer:
			GlobalWriter.WriteMessage(line, 0)
		case <-time.After(10 * time.Millisecond):
			if GlobalWriter.isStdout {
				fmt.Fprintf(GlobalWriter,
					"%s%s%s %sEOF %s%s\n",

					dimColor(),
					time.Now().Format(timeFormat),
					endDimColor(),

					blueColor(),
					leftArrow,
					endColor(),
				)
			} else {
				fmt.Fprintf(GlobalWriter,
					"%s EOF %s\n",
					time.Now().Format(timeFormat),
					leftArrow,
				)
			}
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
