//go:build !windows
// +build !windows

package integration

type OSSpecific struct{}

func (i *OSIntegration) Initialize() error {
	return nil
}
