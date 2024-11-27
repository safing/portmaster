package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/info"
	"github.com/safing/structures/container"
	"github.com/safing/structures/dsd"
)

func initializeLogFile(logFilePath string, identifier string, version string) *os.File {
	logFile, err := os.OpenFile(logFilePath, os.O_RDWR|os.O_CREATE, 0o0440) //nolint:gosec // As desired.
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
		finalizeLogFile(logFile)
		return nil
	}
	c.AppendAsBlock(metaSection)
	// log file data type (string) and newline for better manual viewing
	c.Append([]byte("S\n"))
	c.Append([]byte(fmt.Sprintf("executing %s version %s on %s %s\n", identifier, version, runtime.GOOS, runtime.GOARCH)))

	_, err = logFile.Write(c.CompileData())
	if err != nil {
		log.Printf("failed to write header for log file %s: %s\n", logFilePath, err)
		finalizeLogFile(logFile)
		return nil
	}

	return logFile
}

func finalizeLogFile(logFile *os.File) {
	logFilePath := logFile.Name()

	err := logFile.Close()
	if err != nil {
		log.Printf("failed to close log file %s: %s\n", logFilePath, err)
	}

	// check file size
	stat, err := os.Stat(logFilePath)
	if err != nil {
		return
	}

	// delete if file is smaller than
	if stat.Size() >= 200 { // header + info is about 150 bytes
		return
	}

	if err := os.Remove(logFilePath); err != nil {
		log.Printf("failed to delete empty log file %s: %s\n", logFilePath, err)
	}
}

func getLogFile(options *Options, version, ext string) *os.File {
	// check logging dir
	logFileBasePath := filepath.Join(logsRoot.Path, options.ShortIdentifier)
	err := logsRoot.EnsureAbsPath(logFileBasePath)
	if err != nil {
		log.Printf("failed to check/create log file folder %s: %s\n", logFileBasePath, err)
	}

	// open log file
	logFilePath := filepath.Join(logFileBasePath, fmt.Sprintf("%s%s", time.Now().UTC().Format("2006-01-02-15-04-05"), ext))
	return initializeLogFile(logFilePath, options.Identifier, version)
}

func getPmStartLogFile(ext string) *os.File {
	return getLogFile(&Options{
		ShortIdentifier: "start",
		Identifier:      "start/portmaster-start",
	}, info.Version(), ext)
}

//nolint:unused // false positive on linux, currently used by windows only. TODO: move to a _windows file.
func logControlError(cErr error) {
	// check if error present
	if cErr == nil {
		return
	}

	errorFile := getPmStartLogFile(".error.log")
	if errorFile == nil {
		return
	}
	defer func() {
		_ = errorFile.Close()
	}()

	fmt.Fprintln(errorFile, cErr.Error())
}

//nolint:deadcode,unused // false positive on linux, currently used by windows only. TODO: move to a _windows file.
func runAndLogControlError(wrappedFunc func(cmd *cobra.Command, args []string) error) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		err := wrappedFunc(cmd, args)
		if err != nil {
			logControlError(err)
		}
		return err
	}
}
