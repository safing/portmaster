package osdetail

import (
	"bytes"
	"errors"
)

// RunPowershellCmd runs a powershell command and returns its output.
func RunPowershellCmd(script string) (output []byte, err error) {
	// Create command to execute.
	return RunCmd(
		"powershell.exe",
		"-ExecutionPolicy", "Bypass",
		"-NoProfile",
		"-NonInteractive",
		"[System.Console]::OutputEncoding = [System.Text.Encoding]::UTF8\n"+script,
	)
}

const outputSeparator = "pwzzhtuvpwdgozhzbnjj"

// RunTerminalCmd runs a Windows cmd command and returns its output.
// It sets the output of the cmd to UTF-8 in order to avoid encoding errors.
func RunTerminalCmd(command ...string) (output []byte, err error) {
	output, err = RunCmd(append([]string{
		"cmd.exe",
		"/c",
		"chcp",  // Set output encoding...
		"65001", // ...to UTF-8.
		"&",
		"echo",
		outputSeparator,
		"&",
	},
		command...,
	)...)
	if err != nil {
		return nil, err
	}

	// Find correct start of output and shift start.
	index := bytes.IndexAny(output, outputSeparator+"\r\n")
	if index < 0 {
		return nil, errors.New("failed to post-process output: could not find output separator")
	}
	output = output[index+len(outputSeparator)+2:]

	return output, nil
}
