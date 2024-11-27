package log

import (
	"fmt"
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

	pkgLevelsActive = abool.NewBool(false)
	pkgLevels       = make(map[string]Severity)
	pkgLevelsLock   sync.Mutex

	logsWaiting     = make(chan struct{}, 1)
	logsWaitingFlag = abool.NewBool(false)

	shutdownFlag      = abool.NewBool(false)
	shutdownSignal    = make(chan struct{})
	shutdownWaitGroup sync.WaitGroup

	initializing  = abool.NewBool(false)
	started       = abool.NewBool(false)
	startedSignal = make(chan struct{})
)

// SetPkgLevels sets individual log levels for packages. Only effective after Start().
func SetPkgLevels(levels map[string]Severity) {
	pkgLevelsLock.Lock()
	pkgLevels = levels
	pkgLevelsLock.Unlock()
	pkgLevelsActive.Set()
}

// UnSetPkgLevels removes all individual log levels for packages.
func UnSetPkgLevels() {
	pkgLevelsActive.UnSet()
}

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
func Start() (err error) {
	if !initializing.SetToIf(false, true) {
		return nil
	}

	logBuffer = make(chan *logLine, 1024)

	if logLevelFlag != "" {
		initialLogLevel := ParseLevel(logLevelFlag)
		if initialLogLevel == 0 {
			fmt.Fprintf(os.Stderr, "log warning: invalid log level \"%s\", falling back to level info\n", logLevelFlag)
			initialLogLevel = InfoLevel
		}

		SetLogLevel(initialLogLevel)
	} else {
		// Setup slog here for the transition period.
		setupSLog(GetLogLevel())
	}

	// get and set file loglevels
	pkgLogLevels := pkgLogLevelsFlag
	if len(pkgLogLevels) > 0 {
		newPkgLevels := make(map[string]Severity)
		for _, pair := range strings.Split(pkgLogLevels, ",") {
			splitted := strings.Split(pair, "=")
			if len(splitted) != 2 {
				err = fmt.Errorf("log warning: invalid file log level \"%s\", ignoring", pair)
				fmt.Fprintf(os.Stderr, "%s\n", err.Error())
				break
			}
			fileLevel := ParseLevel(splitted[1])
			if fileLevel == 0 {
				err = fmt.Errorf("log warning: invalid file log level \"%s\", ignoring", pair)
				fmt.Fprintf(os.Stderr, "%s\n", err.Error())
				break
			}
			newPkgLevels[splitted[0]] = fileLevel
		}
		SetPkgLevels(newPkgLevels)
	}

	if !schedulingEnabled {
		close(writeTrigger)
	}
	startWriter()

	started.Set()
	close(startedSignal)

	return err
}

// Shutdown writes remaining log lines and then stops the log system.
func Shutdown() {
	if shutdownFlag.SetToIf(false, true) {
		close(shutdownSignal)
	}
	shutdownWaitGroup.Wait()
}
