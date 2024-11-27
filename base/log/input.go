package log

import (
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

var (
	warnLogLines = new(uint64)
	errLogLines  = new(uint64)
	critLogLines = new(uint64)
)

func log(level Severity, msg string, tracer *ContextTracer) {
	if !started.IsSet() {
		// a bit resource intense, but keeps logs before logging started.
		// TODO: create option to disable logging
		go func() {
			<-startedSignal
			log(level, msg, tracer)
		}()
		return
	}

	// get time
	now := time.Now()

	// get file and line
	_, file, line, ok := runtime.Caller(2)
	if !ok {
		file = ""
		line = 0
	} else {
		if len(file) > 3 {
			file = file[:len(file)-3]
		} else {
			file = ""
		}
	}

	// check if level is enabled for file or generally
	if pkgLevelsActive.IsSet() {
		pathSegments := strings.Split(file, "/")
		if len(pathSegments) < 2 {
			// file too short for package levels
			return
		}
		pkgLevelsLock.Lock()
		severity, ok := pkgLevels[pathSegments[len(pathSegments)-2]]
		pkgLevelsLock.Unlock()
		if ok {
			if level < severity {
				return
			}
		} else {
			// no package level set, check against global level
			if uint32(level) < atomic.LoadUint32(logLevel) {
				return
			}
		}
	} else if uint32(level) < atomic.LoadUint32(logLevel) {
		// no package levels set, check against global level
		return
	}

	// create log object
	log := &logLine{
		msg:       msg,
		tracer:    tracer,
		level:     level,
		timestamp: now,
		file:      file,
		line:      line,
	}

	// send log to processing
	select {
	case logBuffer <- log:
	default:
	forceEmptyingLoop:
		// force empty buffer until we can send to it
		for {
			select {
			case forceEmptyingOfBuffer <- struct{}{}:
			case logBuffer <- log:
				break forceEmptyingLoop
			}
		}
	}

	// wake up writer if necessary
	if logsWaitingFlag.SetToIf(false, true) {
		select {
		case logsWaiting <- struct{}{}:
		default:
		}
	}
}

func fastcheck(level Severity) bool {
	if pkgLevelsActive.IsSet() {
		return true
	}
	if uint32(level) >= atomic.LoadUint32(logLevel) {
		return true
	}
	return false
}

// Trace is used to log tiny steps. Log traces to context if you can!
func Trace(msg string) {
	if fastcheck(TraceLevel) {
		log(TraceLevel, msg, nil)
	}
}

// Tracef is used to log tiny steps. Log traces to context if you can!
func Tracef(format string, things ...interface{}) {
	if fastcheck(TraceLevel) {
		log(TraceLevel, fmt.Sprintf(format, things...), nil)
	}
}

// Debug is used to log minor errors or unexpected events. These occurrences are usually not worth mentioning in itself, but they might hint at a bigger problem.
func Debug(msg string) {
	if fastcheck(DebugLevel) {
		log(DebugLevel, msg, nil)
	}
}

// Debugf is used to log minor errors or unexpected events. These occurrences are usually not worth mentioning in itself, but they might hint at a bigger problem.
func Debugf(format string, things ...interface{}) {
	if fastcheck(DebugLevel) {
		log(DebugLevel, fmt.Sprintf(format, things...), nil)
	}
}

// Info is used to log mildly significant events. Should be used to inform about somewhat bigger or user affecting events that happen.
func Info(msg string) {
	if fastcheck(InfoLevel) {
		log(InfoLevel, msg, nil)
	}
}

// Infof is used to log mildly significant events. Should be used to inform about somewhat bigger or user affecting events that happen.
func Infof(format string, things ...interface{}) {
	if fastcheck(InfoLevel) {
		log(InfoLevel, fmt.Sprintf(format, things...), nil)
	}
}

// Warning is used to log (potentially) bad events, but nothing broke (even a little) and there is no need to panic yet.
func Warning(msg string) {
	atomic.AddUint64(warnLogLines, 1)
	if fastcheck(WarningLevel) {
		log(WarningLevel, msg, nil)
	}
}

// Warningf is used to log (potentially) bad events, but nothing broke (even a little) and there is no need to panic yet.
func Warningf(format string, things ...interface{}) {
	atomic.AddUint64(warnLogLines, 1)
	if fastcheck(WarningLevel) {
		log(WarningLevel, fmt.Sprintf(format, things...), nil)
	}
}

// Error is used to log errors that break or impair functionality. The task/process may have to be aborted and tried again later. The system is still operational. Maybe User/Admin should be informed.
func Error(msg string) {
	atomic.AddUint64(errLogLines, 1)
	if fastcheck(ErrorLevel) {
		log(ErrorLevel, msg, nil)
	}
}

// Errorf is used to log errors that break or impair functionality. The task/process may have to be aborted and tried again later. The system is still operational.
func Errorf(format string, things ...interface{}) {
	atomic.AddUint64(errLogLines, 1)
	if fastcheck(ErrorLevel) {
		log(ErrorLevel, fmt.Sprintf(format, things...), nil)
	}
}

// Critical is used to log events that completely break the system. Operation cannot continue. User/Admin must be informed.
func Critical(msg string) {
	atomic.AddUint64(critLogLines, 1)
	if fastcheck(CriticalLevel) {
		log(CriticalLevel, msg, nil)
	}
}

// Criticalf is used to log events that completely break the system. Operation cannot continue. User/Admin must be informed.
func Criticalf(format string, things ...interface{}) {
	atomic.AddUint64(critLogLines, 1)
	if fastcheck(CriticalLevel) {
		log(CriticalLevel, fmt.Sprintf(format, things...), nil)
	}
}

// TotalWarningLogLines returns the total amount of warning log lines since
// start of the program.
func TotalWarningLogLines() uint64 {
	return atomic.LoadUint64(warnLogLines)
}

// TotalErrorLogLines returns the total amount of error log lines since start
// of the program.
func TotalErrorLogLines() uint64 {
	return atomic.LoadUint64(errLogLines)
}

// TotalCriticalLogLines returns the total amount of critical log lines since
// start of the program.
func TotalCriticalLogLines() uint64 {
	return atomic.LoadUint64(critLogLines)
}
