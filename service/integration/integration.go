//go:build !windows
// +build !windows

package integration

type OSSpecific struct{}

// Initialize is empty on any OS different then Windows.
func (i *OSIntegration) Initialize() error {
	return nil
}

// CleanUp releases any resourses allocated during initializaion.
func (i *OSIntegration) CleanUp() error {
	return nil
}
