package terminal

import "fmt"

// CustomTerminalIDFormatting defines an interface for terminal to define their custom ID format.
type CustomTerminalIDFormatting interface {
	CustomIDFormat() string
}

// FmtID formats the terminal ID together with the parent's ID.
func (t *TerminalBase) FmtID() string {
	if t.ext != nil {
		if customFormatting, ok := t.ext.(CustomTerminalIDFormatting); ok {
			return customFormatting.CustomIDFormat()
		}
	}

	return fmtTerminalID(t.parentID, t.id)
}

func fmtTerminalID(craneID string, terminalID uint32) string {
	return fmt.Sprintf("%s#%d", craneID, terminalID)
}

func fmtOperationID(craneID string, terminalID, operationID uint32) string {
	return fmt.Sprintf("%s#%d>%d", craneID, terminalID, operationID)
}
