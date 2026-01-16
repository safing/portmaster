package logger

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Logger handles logging to console and/or file
type Logger struct {
	mu            sync.Mutex
	console       io.Writer
	file          *os.File
	toConsole     bool
	toFile        bool
	prefix        string
	consoleFilter func(string) bool
}

// Config for logger creation
type Config struct {
	ToConsole bool
	ToFile    bool
	FilePath  string
	Prefix    string
	Truncate  bool // If true, truncate file on open; if false, append
}

// New creates a new logger with the given configuration
func New(cfg Config) (*Logger, error) {
	l := &Logger{
		toConsole: cfg.ToConsole,
		toFile:    cfg.ToFile,
		prefix:    cfg.Prefix,
		console:   os.Stdout,
	}

	if cfg.ToFile && cfg.FilePath != "" {
		flags := os.O_CREATE | os.O_WRONLY
		if cfg.Truncate {
			flags |= os.O_TRUNC
		} else {
			flags |= os.O_APPEND
		}
		f, err := os.OpenFile(cfg.FilePath, flags, 0644)
		if err != nil {
			return nil, fmt.Errorf("failed to open log file: %w", err)
		}
		l.file = f
	}

	return l, nil
}

// Close closes the log file if open
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file != nil {
		return l.file.Close()
	}
	return nil
}

// SetConsoleOutput enables/disables console output
func (l *Logger) SetConsoleOutput(enabled bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.toConsole = enabled
}

// IsConsoleEnabled returns whether console output is enabled
func (l *Logger) IsConsoleEnabled() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.toConsole
}

// SetConsoleFilter sets a filter function for console output
// The filter receives the formatted message and returns true if it should be logged to console
func (l *Logger) SetConsoleFilter(filter func(string) bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.consoleFilter = filter
}

// EnableFileOutput enables file output, creating/opening the file if needed
func (l *Logger) EnableFileOutput(filePath string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.toFile && l.file != nil {
		return nil // Already enabled
	}

	if filePath == "" {
		return fmt.Errorf("file path is required")
	}

	f, err := os.OpenFile(filePath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}

	l.file = f
	l.toFile = true
	return nil
}

// DisableFileOutput disables file output and closes the file
func (l *Logger) DisableFileOutput() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.toFile || l.file == nil {
		return nil // Already disabled
	}

	err := l.file.Close()
	l.file = nil
	l.toFile = false
	return err
}

func (l *Logger) log(level, format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	prefix := ""
	if l.prefix != "" {
		prefix = "[" + l.prefix + "] "
	}
	msg := fmt.Sprintf("%s %s%s: %s\n", timestamp, prefix, level, fmt.Sprintf(format, args...))

	if l.toConsole && l.console != nil {
		// Apply filter if set
		if l.consoleFilter == nil || l.consoleFilter(msg) {
			_, _ = l.console.Write([]byte(msg))
		}
	}
	if l.toFile && l.file != nil {
		_, _ = l.file.Write([]byte(msg))
	}
}

// Info logs an info message
func (l *Logger) Info(format string, args ...any) {
	l.log("INFO", format, args...)
}

// Warn logs a warning message
func (l *Logger) Warn(format string, args ...any) {
	l.log("WARN", format, args...)
}

// Error logs an error message
func (l *Logger) Error(format string, args ...any) {
	l.log("ERROR", format, args...)
}

// Debug logs a debug message
func (l *Logger) Debug(format string, args ...any) {
	l.log("DEBUG", format, args...)
}

// Raw writes a raw message without timestamp/level
func (l *Logger) Raw(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	msg := fmt.Sprintf(format, args...)
	if l.toConsole && l.console != nil {
		_, _ = l.console.Write([]byte(msg))
	}
	if l.toFile && l.file != nil {
		_, _ = l.file.Write([]byte(msg))
	}
}

// Printf writes a formatted message
func (l *Logger) Printf(format string, args ...any) {
	l.Raw(format+"\n", args...)
}
