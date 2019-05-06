package process

var (
	// UnknownProcess is used when a process cannot be found.
	UnknownProcess = &Process{
		UserID:    -1,
		UserName:  "Unknown",
		Pid:       -1,
		ParentPid: -1,
		Name:      "Unknown Processes",
	}

	// OSProcess is used to represent the Kernel.
	OSProcess = &Process{
		UserID:    0,
		UserName:  "Kernel",
		Pid:       0,
		ParentPid: 0,
		Name:      "Operating System",
	}
)

func init() {
	UnknownProcess.Save()
	OSProcess.Save()
}
