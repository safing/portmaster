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
)

func init() {
	UnknownProcess.Save()
}
