package log

import (
	"fmt"
	"testing"
	"time"
)

func init() {
	err := Start()
	if err != nil {
		panic(fmt.Sprintf("start failed: %s", err))
	}
}

func TestLogging(t *testing.T) {
	t.Parallel()

	// skip
	if testing.Short() {
		t.Skip()
	}

	// set levels (static random)
	SetLogLevel(WarningLevel)
	SetLogLevel(InfoLevel)
	SetLogLevel(ErrorLevel)
	SetLogLevel(DebugLevel)
	SetLogLevel(CriticalLevel)
	SetLogLevel(TraceLevel)

	// log
	Trace("Trace")
	Debug("Debug")
	Info("Info")
	Warning("Warning")
	Error("Error")
	Critical("Critical")

	// logf
	Tracef("Trace %s", "f")
	Debugf("Debug %s", "f")
	Infof("Info %s", "f")
	Warningf("Warning %s", "f")
	Errorf("Error %s", "f")
	Criticalf("Critical %s", "f")

	// play with levels
	SetLogLevel(CriticalLevel)
	Warning("Warning")
	SetLogLevel(TraceLevel)

	// log invalid level
	log(0xFF, "msg", nil)

	// wait logs to be written
	time.Sleep(1 * time.Millisecond)

	// just for show
	UnSetPkgLevels()

	// do not really shut down, we may need logging for other tests
	// ShutdownLogging()
}
