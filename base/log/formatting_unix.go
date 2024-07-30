//go:build !windows

package log

const (
	rightArrow = "▶"
	leftArrow  = "◀"
)

const (
	colorDim     = "\033[2m"
	colorEndDim  = "\033[22m"
	colorRed     = "\033[91m"
	colorYellow  = "\033[93m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
	colorGreen   = "\033[92m"

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
		return colorGreen
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

func blueColor() string {
	return colorBlue
}

func dimColor() string {
	return colorDim
}

func endDimColor() string {
	return colorEndDim
}
