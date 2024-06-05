package osdetail

import (
	"bytes"
	"errors"
	"os/exec"
	"strings"
)

// RunCmd runs the given command and run error checks on the output.
func RunCmd(command ...string) (output []byte, err error) {
	// Create command to execute.
	var cmd *exec.Cmd
	switch len(command) {
	case 0:
		return nil, errors.New("no command supplied")
	case 1:
		cmd = exec.Command(command[0])
	default:
		cmd = exec.Command(command[0], command[1:]...)
	}

	// Create and assign output buffers.
	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	// Run command and collect output.
	err = cmd.Run()
	stdout, stderr := stdoutBuf.Bytes(), stderrBuf.Bytes()
	if err != nil {
		return nil, err
	}
	// Command might not return an error, but just write to stdout instead.
	if len(stderr) > 0 {
		return nil, errors.New(strings.SplitN(string(stderr), "\n", 2)[0])
	}

	// Debugging output:
	// fmt.Printf("command stdout: %s\n", stdout)
	// fmt.Printf("command stderr: %s\n", stderr)

	// Finalize stdout.
	cleanedOutput := bytes.TrimSpace(stdout)
	if len(cleanedOutput) == 0 {
		return nil, ErrEmptyOutput
	}

	return cleanedOutput, nil
}
