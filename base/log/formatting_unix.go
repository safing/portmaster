//go:build !windows

package log

const (
	rightArrow = "▶"
	leftArrow  = "◀"
)

const (
	colorRed     = "\033[31m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"

	// Saved for later:
	// colorBlack   = "\033[30m" //.
	// colorGreen   = "\033[32m" //.
	// colorWhite   = "\033[37m" //.
)

func (s Severity) color() string {
	switch s {
	case DebugLevel:
		return colorCyan
	case InfoLevel:
		return colorBlue
	case WarningLevel:
		return colorYellow
	case ErrorLevel:
		return colorRed
	case CriticalLevel:
		return colorMagenta
	case TraceLevel:
		return ""
	default:
		return ""
	}
}

func endColor() string {
	return "\033[0m"
}
