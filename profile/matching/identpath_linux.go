package matching

import (
	"fmt"
	"strings"

	"github.com/safing/portmaster/process"
	"github.com/safing/portmaster/profile"
)

// GetIdentificationPath returns the identifier for the given process (linux edition).
func GetIdentificationPath(p *process.Process) string {
	splittedPath := strings.Split(p.Path, "/")
	if len(splittedPath) > 3 {
		return fmt.Sprintf("%s%s", profile.IdentifierPrefix, strings.Join(splittedPath[len(splittedPath)-3:len(splittedPath)], "/"))
	}
	return fmt.Sprintf("%s%s", profile.IdentifierPrefix, p.Path)
}
