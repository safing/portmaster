package log

import (
	"log/slog"
	"os"
	"runtime"

	"github.com/lmittmann/tint"
)

func setupSLog(level Severity) {
	// Set highest possible level, so it can be changed in runtime.
	handlerLogLevel := level.toSLogLevel()

	// Create handler depending on OS.
	var logHandler slog.Handler
	switch runtime.GOOS {
	case "windows":
		logHandler = tint.NewHandler(
			GlobalWriter,
			&tint.Options{
				AddSource:  true,
				Level:      handlerLogLevel,
				TimeFormat: timeFormat,
				NoColor:    !GlobalWriter.IsStdout(),
			},
		)
	case "linux":
		logHandler = tint.NewHandler(GlobalWriter, &tint.Options{
			AddSource:  true,
			Level:      handlerLogLevel,
			TimeFormat: timeFormat,
			NoColor:    !GlobalWriter.IsStdout(),
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
	// Set actual log level.
	slog.SetLogLoggerLevel(handlerLogLevel)
}
