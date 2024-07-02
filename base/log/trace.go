package log

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ContextTracerKey is the key used for the context key/value storage.
type ContextTracerKey struct{}

// ContextTracer is attached to a context in order bind logs to a context.
type ContextTracer struct {
	sync.Mutex
	logs []*logLine
}

var key = ContextTracerKey{}

// AddTracer adds a ContextTracer to the returned Context. Will return a nil ContextTracer if logging level is not set to trace. Will return a nil ContextTracer if one already exists. Will return a nil ContextTracer in case of an error. Will return a nil context if nil.
func AddTracer(ctx context.Context) (context.Context, *ContextTracer) {
	if ctx != nil && fastcheck(TraceLevel) {
		// check pkg levels
		if pkgLevelsActive.IsSet() {
			// get file
			_, file, _, ok := runtime.Caller(1)
			if !ok {
				// cannot get file, ignore
				return ctx, nil
			}

			pathSegments := strings.Split(file, "/")
			if len(pathSegments) < 2 {
				// file too short for package levels
				return ctx, nil
			}
			pkgLevelsLock.Lock()
			severity, ok := pkgLevels[pathSegments[len(pathSegments)-2]]
			pkgLevelsLock.Unlock()
			if ok {
				// check against package level
				if TraceLevel < severity {
					return ctx, nil
				}
			} else {
				// no package level set, check against global level
				if uint32(TraceLevel) < atomic.LoadUint32(logLevel) {
					return ctx, nil
				}
			}
		} else if uint32(TraceLevel) < atomic.LoadUint32(logLevel) {
			// no package levels set, check against global level
			return ctx, nil
		}

		// check for existing tracer
		_, ok := ctx.Value(key).(*ContextTracer)
		if !ok {
			// add and return new tracer
			tracer := &ContextTracer{}
			return context.WithValue(ctx, key, tracer), tracer
		}
	}
	return ctx, nil
}

// Tracer returns the ContextTracer previously added to the given Context.
func Tracer(ctx context.Context) *ContextTracer {
	if ctx != nil {
		tracer, ok := ctx.Value(key).(*ContextTracer)
		if ok {
			return tracer
		}
	}
	return nil
}

// Submit collected logs on the context for further processing/outputting. Does nothing if called on a nil ContextTracer.
func (tracer *ContextTracer) Submit() {
	if tracer == nil {
		return
	}

	if !started.IsSet() {
		// a bit resource intense, but keeps logs before logging started.
		// TODO: create option to disable logging
		go func() {
			<-startedSignal
			tracer.Submit()
		}()
		return
	}

	if len(tracer.logs) == 0 {
		return
	}

	// extract last line as main line
	mainLine := tracer.logs[len(tracer.logs)-1]
	tracer.logs = tracer.logs[:len(tracer.logs)-1]

	// create log object
	log := &logLine{
		msg:       mainLine.msg,
		tracer:    tracer,
		level:     mainLine.level,
		timestamp: mainLine.timestamp,
		file:      mainLine.file,
		line:      mainLine.line,
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
		logsWaiting <- struct{}{}
	}
}

func (tracer *ContextTracer) log(level Severity, msg string) {
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

	tracer.Lock()
	defer tracer.Unlock()
	tracer.logs = append(tracer.logs, &logLine{
		timestamp: time.Now(),
		level:     level,
		msg:       msg,
		file:      file,
		line:      line,
	})
}

// Trace is used to log tiny steps. Log traces to context if you can!
func (tracer *ContextTracer) Trace(msg string) {
	switch {
	case tracer != nil:
		tracer.log(TraceLevel, msg)
	case fastcheck(TraceLevel):
		log(TraceLevel, msg, nil)
	}
}

// Tracef is used to log tiny steps. Log traces to context if you can!
func (tracer *ContextTracer) Tracef(format string, things ...interface{}) {
	switch {
	case tracer != nil:
		tracer.log(TraceLevel, fmt.Sprintf(format, things...))
	case fastcheck(TraceLevel):
		log(TraceLevel, fmt.Sprintf(format, things...), nil)
	}
}

// Debug is used to log minor errors or unexpected events. These occurrences are usually not worth mentioning in itself, but they might hint at a bigger problem.
func (tracer *ContextTracer) Debug(msg string) {
	switch {
	case tracer != nil:
		tracer.log(DebugLevel, msg)
	case fastcheck(DebugLevel):
		log(DebugLevel, msg, nil)
	}
}

// Debugf is used to log minor errors or unexpected events. These occurrences are usually not worth mentioning in itself, but they might hint at a bigger problem.
func (tracer *ContextTracer) Debugf(format string, things ...interface{}) {
	switch {
	case tracer != nil:
		tracer.log(DebugLevel, fmt.Sprintf(format, things...))
	case fastcheck(DebugLevel):
		log(DebugLevel, fmt.Sprintf(format, things...), nil)
	}
}

// Info is used to log mildly significant events. Should be used to inform about somewhat bigger or user affecting events that happen.
func (tracer *ContextTracer) Info(msg string) {
	switch {
	case tracer != nil:
		tracer.log(InfoLevel, msg)
	case fastcheck(InfoLevel):
		log(InfoLevel, msg, nil)
	}
}

// Infof is used to log mildly significant events. Should be used to inform about somewhat bigger or user affecting events that happen.
func (tracer *ContextTracer) Infof(format string, things ...interface{}) {
	switch {
	case tracer != nil:
		tracer.log(InfoLevel, fmt.Sprintf(format, things...))
	case fastcheck(InfoLevel):
		log(InfoLevel, fmt.Sprintf(format, things...), nil)
	}
}

// Warning is used to log (potentially) bad events, but nothing broke (even a little) and there is no need to panic yet.
func (tracer *ContextTracer) Warning(msg string) {
	switch {
	case tracer != nil:
		tracer.log(WarningLevel, msg)
	case fastcheck(WarningLevel):
		log(WarningLevel, msg, nil)
	}
}

// Warningf is used to log (potentially) bad events, but nothing broke (even a little) and there is no need to panic yet.
func (tracer *ContextTracer) Warningf(format string, things ...interface{}) {
	switch {
	case tracer != nil:
		tracer.log(WarningLevel, fmt.Sprintf(format, things...))
	case fastcheck(WarningLevel):
		log(WarningLevel, fmt.Sprintf(format, things...), nil)
	}
}

// Error is used to log errors that break or impair functionality. The task/process may have to be aborted and tried again later. The system is still operational. Maybe User/Admin should be informed.
func (tracer *ContextTracer) Error(msg string) {
	switch {
	case tracer != nil:
		tracer.log(ErrorLevel, msg)
	case fastcheck(ErrorLevel):
		log(ErrorLevel, msg, nil)
	}
}

// Errorf is used to log errors that break or impair functionality. The task/process may have to be aborted and tried again later. The system is still operational.
func (tracer *ContextTracer) Errorf(format string, things ...interface{}) {
	switch {
	case tracer != nil:
		tracer.log(ErrorLevel, fmt.Sprintf(format, things...))
	case fastcheck(ErrorLevel):
		log(ErrorLevel, fmt.Sprintf(format, things...), nil)
	}
}

// Critical is used to log events that completely break the system. Operation connot continue. User/Admin must be informed.
func (tracer *ContextTracer) Critical(msg string) {
	switch {
	case tracer != nil:
		tracer.log(CriticalLevel, msg)
	case fastcheck(CriticalLevel):
		log(CriticalLevel, msg, nil)
	}
}

// Criticalf is used to log events that completely break the system. Operation connot continue. User/Admin must be informed.
func (tracer *ContextTracer) Criticalf(format string, things ...interface{}) {
	switch {
	case tracer != nil:
		tracer.log(CriticalLevel, fmt.Sprintf(format, things...))
	case fastcheck(CriticalLevel):
		log(CriticalLevel, fmt.Sprintf(format, things...), nil)
	}
}
