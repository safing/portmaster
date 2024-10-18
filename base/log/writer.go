package log

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// GlobalWriter is the global log writer.
var GlobalWriter *LogWriter = nil

type LogWriter struct {
	writeLock sync.Mutex
	isStdout  bool
	file      *os.File
}

// NewStdoutWriter creates a new log writer thet will write to the stdout.
func NewStdoutWriter() *LogWriter {
	return &LogWriter{
		file:     os.Stdout,
		isStdout: true,
	}
}

// NewFileWriter creates a new log writer that will write to a file. The file path will be <dir>/2006-01-02-15-04-05.log (with current date and time)
func NewFileWriter(dir string) (*LogWriter, error) {
	_ = os.MkdirAll(dir, 0o777)
	logFile := fmt.Sprintf("%s.log", time.Now().UTC().Format("2006-01-02-15-04-05"))
	file, err := os.Create(filepath.Join(dir, logFile))
	if err != nil {
		return nil, err
	}
	return &LogWriter{
		file:     file,
		isStdout: false,
	}, nil
}

// Write writes the buffer to the writer.
func (l *LogWriter) Write(buf []byte) (int, error) {
	if l == nil {
		return 0, fmt.Errorf("log writer not initialized")
	}
	// No need to lock in stdout context.
	if !l.isStdout {
		l.writeLock.Lock()
		defer l.writeLock.Unlock()
	}

	return l.file.Write(buf)
}

// WriteMessage writes the message to the writer.
func (l *LogWriter) WriteMessage(msg Message, duplicates uint64) {
	if l == nil {
		return
	}
	// No need to lock in stdout context.
	if !l.isStdout {
		l.writeLock.Lock()
		defer l.writeLock.Unlock()
	}
	fmt.Fprintln(l.file, formatLine(msg.(*logLine), duplicates, l.isStdout))
}

// IsStdout returns true if writer was initialized with stdout
func (l *LogWriter) IsStdout() bool {
	return l != nil && l.isStdout
}

// Close closes the writer.
func (l *LogWriter) Close() {
	if l != nil && !l.isStdout {
		_ = l.file.Close()
	}
}

// CleanOldLogs clean all logs in dir that are older then threshold.
func CleanOldLogs(dir string, threshold time.Duration) error {
	files, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read dir: %w", err)
	}

	for _, f := range files {
		if f.IsDir() {
			continue
		}
		logDateStr := strings.TrimSuffix(f.Name(), ".log")
		logDate, err := time.Parse("2006-01-02-15-04-05", logDateStr)
		if err != nil {
			continue
		}

		if logDate.Add(threshold).Before(time.Now()) {
			_ = os.Remove(filepath.Join(dir, f.Name()))
		}
	}
	return nil
}
