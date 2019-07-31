package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/safing/portbase/container"
	"github.com/safing/portbase/database/record"
	"github.com/safing/portbase/formats/dsd"
	"github.com/safing/portbase/info"
	"github.com/safing/portmaster/updates"
	"github.com/spf13/cobra"
)

func initializeLogFile(logFilePath string, identifier string, updateFile *updates.File) *os.File {
	logFile, err := os.OpenFile(logFilePath, os.O_RDWR|os.O_CREATE, 0444)
	if err != nil {
		log.Printf("failed to create log file %s: %s\n", logFilePath, err)
		return nil
	}

	// create header, so that the portmaster can view log files as a database
	meta := record.Meta{}
	meta.Update()
	meta.SetAbsoluteExpiry(time.Now().Add(720 * time.Hour).Unix()) // one month

	// manually marshal
	// version
	c := container.New([]byte{1})
	// meta
	metaSection, err := dsd.Dump(meta, dsd.JSON)
	if err != nil {
		log.Printf("failed to serialize header for log file %s: %s\n", logFilePath, err)
		finalizeLogFile(logFile, logFilePath)
		return nil
	}
	c.AppendAsBlock(metaSection)
	// log file data type (string) and newline for better manual viewing
	c.Append([]byte("S\n"))
	c.Append([]byte(fmt.Sprintf("executing %s version %s on %s %s\n", identifier, updateFile.Version(), runtime.GOOS, runtime.GOARCH)))

	_, err = logFile.Write(c.CompileData())
	if err != nil {
		log.Printf("failed to write header for log file %s: %s\n", logFilePath, err)
		finalizeLogFile(logFile, logFilePath)
		return nil
	}

	return logFile
}

func finalizeLogFile(logFile *os.File, logFilePath string) {
	err := logFile.Close()
	if err != nil {
		log.Printf("failed to close log file %s: %s\n", logFilePath, err)
	}

	// check file size
	stat, err := os.Stat(logFilePath)
	if err == nil {
		// delete if file is smaller than
		if stat.Size() < 200 { // header + info is about 150 bytes
			err := os.Remove(logFilePath)
			if err != nil {
				log.Printf("failed to delete empty log file %s: %s\n", logFilePath, err)
			}
		}
	}
}

func initControlLogFile() *os.File {
	// check logging dir
	logFileBasePath := filepath.Join(logsRoot.Path, "fstree", "control")
	err := logsRoot.EnsureAbsPath(logFileBasePath)
	if err != nil {
		log.Printf("failed to check/create log file folder %s: %s\n", logFileBasePath, err)
	}

	// open log file
	logFilePath := filepath.Join(logFileBasePath, fmt.Sprintf("%s.log", time.Now().UTC().Format("2006-02-01-15-04-05")))
	return initializeLogFile(logFilePath, "control/portmaster-control", updates.NewFile("", info.Version(), false))
}

func logControlError(cErr error) {
	// check if error present
	if cErr == nil {
		return
	}

	// check logging dir
	logFileBasePath := filepath.Join(logsRoot.Path, "fstree", "control")
	err := logsRoot.EnsureAbsPath(logFileBasePath)
	if err != nil {
		log.Printf("failed to check/create log file folder %s: %s\n", logFileBasePath, err)
	}

	// open log file
	logFilePath := filepath.Join(logFileBasePath, fmt.Sprintf("%s.error.log", time.Now().UTC().Format("2006-02-01-15-04-05")))
	errorFile := initializeLogFile(logFilePath, "control/portmaster-control", updates.NewFile("", info.Version(), false))
	if errorFile == nil {
		return
	}

	// write error and close
	fmt.Fprintln(errorFile, cErr.Error())
	errorFile.Close()
}

func logControlStack() {
	// check logging dir
	logFileBasePath := filepath.Join(logsRoot.Path, "fstree", "control")
	err := logsRoot.EnsureAbsPath(logFileBasePath)
	if err != nil {
		log.Printf("failed to check/create log file folder %s: %s\n", logFileBasePath, err)
	}

	// open log file
	logFilePath := filepath.Join(logFileBasePath, fmt.Sprintf("%s.stack.log", time.Now().UTC().Format("2006-02-01-15-04-05")))
	errorFile := initializeLogFile(logFilePath, "control/portmaster-control", updates.NewFile("", info.Version(), false))
	if errorFile == nil {
		return
	}

	// write error and close
	pprof.Lookup("goroutine").WriteTo(errorFile, 1)
	errorFile.Close()
}

func runAndLogControlError(wrappedFunc func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		err := wrappedFunc(cmd, args)
		if err != nil {
			logControlError(err)
		}
		return err
	}
}
