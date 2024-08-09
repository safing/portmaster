package log

import (
	"github.com/safing/portmaster/base/utils/osdetail"
)

const (
	rightArrow = ">"
	leftArrow  = "<"
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

	// colorBlack   = "\033[30m"
	// colorGreen   = "\033[32m"
	// colorWhite   = "\033[37m"
)

var (
	colorsSupported bool
)

func init() {
	colorsSupported = osdetail.EnableColorSupport()
}

func (s Severity) color() string {
	if colorsSupported {
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
		default:
			return ""
		}
	}
	return ""
}

func endColor() string {
	if colorsSupported {
		return "\033[0m"
	}
	return ""
}

func blueColor() string {
	if colorsSupported {
		return colorBlue
	}
	return ""
}

func dimColor() string {
	if colorsSupported {
		return colorDim
	}
	return ""
}

func endDimColor() string {
	if colorsSupported {
		return colorEndDim
	}
	return ""
}
