package log

import (
	"log/slog"
	"os"
	"runtime"

	"github.com/lmittmann/tint"
	"github.com/mattn/go-colorable"
	"github.com/mattn/go-isatty"
)

func setupSLog(logLevel Severity) {
	// Convert to slog level.
	var level slog.Level
	switch logLevel {
	case TraceLevel:
		level = slog.LevelDebug
	case DebugLevel:
		level = slog.LevelDebug
	case InfoLevel:
		level = slog.LevelInfo
	case WarningLevel:
		level = slog.LevelWarn
	case ErrorLevel:
		level = slog.LevelError
	case CriticalLevel:
		level = slog.LevelError
	}

	// Setup logging.
	// Define output.
	logOutput := os.Stdout
	// Create handler depending on OS.
	var logHandler slog.Handler
	switch runtime.GOOS {
	case "windows":
		logHandler = tint.NewHandler(
			colorable.NewColorable(logOutput),
			&tint.Options{
				AddSource:  true,
				Level:      level,
				TimeFormat: timeFormat,
			},
		)
	case "linux":
		logHandler = tint.NewHandler(logOutput, &tint.Options{
			AddSource:  true,
			Level:      level,
			TimeFormat: timeFormat,
			NoColor:    !isatty.IsTerminal(logOutput.Fd()),
		})
	default:
		logHandler = tint.NewHandler(os.Stdout, &tint.Options{
			AddSource:  true,
			Level:      level,
			TimeFormat: timeFormat,
			NoColor:    true,
		})
	}

	// Set as default logger.
	slog.SetDefault(slog.New(logHandler))
	slog.SetLogLoggerLevel(level)
}
