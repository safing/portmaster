package log

import (
	"io"
	"log/slog"
	"os"
	"runtime"

	"github.com/lmittmann/tint"
	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
)

func setupSLog(level Severity) {
	// TODO: Changes in the log level are not yet reflected onto the slog handlers in the modules.

	// Set highest possible level, so it can be changed in runtime.
	handlerLogLevel := level.toSLogLevel()

	// Create handler depending on OS.
	var logHandler slog.Handler
	switch runtime.GOOS {
	case "windows":
		logHandler = tint.NewHandler(
			windowsColoring(GlobalWriter), // Enable coloring on Windows.
			&tint.Options{
				AddSource:  true,
				Level:      handlerLogLevel,
				TimeFormat: timeFormat,
				NoColor:    !( /* Color: */ GlobalWriter.IsStdout() && isatty.IsTerminal(GlobalWriter.file.Fd())),
			},
		)

	case "linux":
		logHandler = tint.NewHandler(GlobalWriter, &tint.Options{
			AddSource:  true,
			Level:      handlerLogLevel,
			TimeFormat: timeFormat,
			NoColor:    !( /* Color: */ GlobalWriter.IsStdout() && isatty.IsTerminal(GlobalWriter.file.Fd())),
		})

	default:
		logHandler = tint.NewHandler(os.Stdout, &tint.Options{
			AddSource:  true,
			Level:      handlerLogLevel,
			TimeFormat: timeFormat,
			NoColor:    true,
		})
	}

	// Set as default logger.
	slog.SetDefault(slog.New(logHandler))
}

func windowsColoring(lw *LogWriter) io.Writer {
	if lw.IsStdout() {
		return colorable.NewColorable(lw.file)
	}
	return lw
}
