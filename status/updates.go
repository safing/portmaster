package status

// Update status options
const (
	UpdateStatusCurrentStable = "stable"
	UpdateStatusCurrentBeta   = "beta"
	UpdateStatusAvailable     = "available" // restart or reboot required
	UpdateStatusFailed        = "failed"    // check logs
)

// SetUpdateStatus updates the system status with a new update status.
func SetUpdateStatus(newStatus string) {
	status.Lock()
	status.UpdateStatus = newStatus
	status.Unlock()

	go status.Save()
}
