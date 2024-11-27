package log

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tevino/abool"
)

// concept
/*
- Logging function:
  - check if file-based levelling enabled
    - if yes, check if level is active on this file
  - check if level is active
  - send data to backend via big buffered channel
- Backend:
  - wait until there is time for writing logs
  - write logs
  - configurable if logged to folder (buffer + rollingFileAppender) and/or console
  - console: log everything above INFO to stderr
- Channel overbuffering protection:
  - if buffer is full, trigger write
- Anti-Importing-Loop:
  - everything imports logging
  - logging is configured by main module and is supplied access to configuration and taskmanager
*/

// Severity describes a log level.
type Severity uint32

func (s Severity) toSLogLevel() slog.Level {
	// Convert to slog level.
	switch s {
	case TraceLevel:
		return slog.LevelDebug
	case DebugLevel:
		return slog.LevelDebug
	case InfoLevel:
		return slog.LevelInfo
	case WarningLevel:
		return slog.LevelWarn
	case ErrorLevel:
		return slog.LevelError
	case CriticalLevel:
		return slog.LevelError
	}
	// Failed to convert, return default log level
	return slog.LevelWarn
}

// Message describes a log level message and is implemented
// by logLine.
type Message interface {
	Text() string
	Severity() Severity
	Time() time.Time
	File() string
	LineNumber() int
}

type logLine struct {
	msg       string
	tracer    *ContextTracer
	level     Severity
	timestamp time.Time
	file      string
	line      int
}

func (ll *logLine) Text() string {
	return ll.msg
}

func (ll *logLine) Severity() Severity {
	return ll.level
}

func (ll *logLine) Time() time.Time {
	return ll.timestamp
}

func (ll *logLine) File() string {
	return ll.file
}

func (ll *logLine) LineNumber() int {
	return ll.line
}

func (ll *logLine) Equal(ol *logLine) bool {
	switch {
	case ll.msg != ol.msg:
		return false
	case ll.tracer != nil || ol.tracer != nil:
		return false
	case ll.file != ol.file:
		return false
	case ll.line != ol.line:
		return false
	case ll.level != ol.level:
		return false
	}
	return true
}

// Log Levels.
const (
	TraceLevel    Severity = 1
	DebugLevel    Severity = 2
	InfoLevel     Severity = 3
	WarningLevel  Severity = 4
	ErrorLevel    Severity = 5
	CriticalLevel Severity = 6
)

var (
	logBuffer             chan *logLine
	forceEmptyingOfBuffer = make(chan struct{})

	logLevelInt = uint32(InfoLevel)
	logLevel    = &logLevelInt

	logsWaiting     = make(chan struct{}, 1)
	logsWaitingFlag = abool.NewBool(false)

	shutdownFlag      = abool.NewBool(false)
	shutdownSignal    = make(chan struct{})
	shutdownWaitGroup sync.WaitGroup

	initializing  = abool.NewBool(false)
	started       = abool.NewBool(false)
	startedSignal = make(chan struct{})
)

// GetLogLevel returns the current log level.
func GetLogLevel() Severity {
	return Severity(atomic.LoadUint32(logLevel))
}

// SetLogLevel sets a new log level. Only effective after Start().
func SetLogLevel(level Severity) {
	atomic.StoreUint32(logLevel, uint32(level))

	// Setup slog here for the transition period.
	setupSLog(level)
}

// Name returns the name of the log level.
func (s Severity) Name() string {
	switch s {
	case TraceLevel:
		return "trace"
	case DebugLevel:
		return "debug"
	case InfoLevel:
		return "info"
	case WarningLevel:
		return "warning"
	case ErrorLevel:
		return "error"
	case CriticalLevel:
		return "critical"
	default:
		return "none"
	}
}

// ParseLevel returns the level severity of a log level name.
func ParseLevel(level string) Severity {
	switch strings.ToLower(level) {
	case "trace":
		return 1
	case "debug":
		return 2
	case "info":
		return 3
	case "warning":
		return 4
	case "error":
		return 5
	case "critical":
		return 6
	}
	return 0
}

// Start starts the logging system. Must be called in order to see logs.
func Start(level string, logToStdout bool, logDir string) (err error) {
	if !initializing.SetToIf(false, true) {
		return nil
	}

	// Parse log level argument.
	initialLogLevel := InfoLevel
	if level != "" {
		initialLogLevel = ParseLevel(level)
		if initialLogLevel == 0 {
			fmt.Fprintf(os.Stderr, "log warning: invalid log level %q, falling back to level info\n", level)
			initialLogLevel = InfoLevel
		}
	}

	// Setup writer.
	if logToStdout {
		GlobalWriter = NewStdoutWriter()
	} else {
		// Create file log writer.
		var err error
		GlobalWriter, err = NewFileWriter(logDir)
		if err != nil {
			return fmt.Errorf("failed to initialize log file: %w", err)
		}
	}

	// Init logging systems.
	SetLogLevel(initialLogLevel)
	logBuffer = make(chan *logLine, 1024)

	if !schedulingEnabled {
		close(writeTrigger)
	}
	startWriter()

	started.Set()
	close(startedSignal)

	// Delete all logs older than one month.
	if !logToStdout {
		err = CleanOldLogs(logDir, 30*24*time.Hour)
		if err != nil {
			Errorf("log: failed to clean old log files: %s", err)
		}
	}

	return err
}

// Shutdown writes remaining log lines and then stops the log system.
func Shutdown() {
	if shutdownFlag.SetToIf(false, true) {
		close(shutdownSignal)
	}
	shutdownWaitGroup.Wait()
	GlobalWriter.Close()
}
