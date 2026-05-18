package log

import (
	"io"
	"log/slog"
	"os"
	"runtime"
	"sync"

	"github.com/lmittmann/tint"
	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
)

// slogLevel is the shared log level variable for the default slog logger.
// All loggers derived from slog.Default() (e.g. via .With()) share the same
// underlying handler and therefore respect changes to this variable immediately.
var (
	slogLevel     = new(slog.LevelVar)
	slogSetupOnce sync.Once
)

func setupSLog(level Severity) {
	// Update the shared level variable. All existing handlers and derived
	// loggers read from this pointer, so they pick up the change instantly.
	slogLevel.Set(level.toSLogLevel())

	// Create the handler and set slog.Default() exactly once, so that
	// managers created after startup always hold a logger whose underlying
	// handler is controlled by slogLevel.
	slogSetupOnce.Do(func() {
		var logHandler slog.Handler
		switch runtime.GOOS {
		case "windows":
			logHandler = tint.NewHandler(
				windowsColoring(GlobalWriter), // Enable coloring on Windows.
				&tint.Options{
					AddSource:  true,
					Level:      slogLevel,
					TimeFormat: timeFormat,
					NoColor:    !( /* Color: */ GlobalWriter.IsStdout() && isatty.IsTerminal(GlobalWriter.file.Fd())),
				},
			)

		case "linux":
			logHandler = tint.NewHandler(GlobalWriter, &tint.Options{
				AddSource:  true,
				Level:      slogLevel,
				TimeFormat: timeFormat,
				NoColor:    !( /* Color: */ GlobalWriter.IsStdout() && isatty.IsTerminal(GlobalWriter.file.Fd())),
			})

		default:
			logHandler = tint.NewHandler(os.Stdout, &tint.Options{
				AddSource:  true,
				Level:      slogLevel,
				TimeFormat: timeFormat,
				NoColor:    true,
			})
		}

		slog.SetDefault(slog.New(logHandler))
	})
}

func windowsColoring(lw *LogWriter) io.Writer {
	if lw.IsStdout() {
		return colorable.NewColorable(lw.file)
	}
	return lw
}
