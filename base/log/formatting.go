package log

import (
	"fmt"
	"time"
)

var counter uint16

const (
	maxCount   uint16 = 999
	timeFormat string = "060102 15:04:05.000"
)

func (s Severity) String() string {
	switch s {
	case TraceLevel:
		return "TRAC"
	case DebugLevel:
		return "DEBU"
	case InfoLevel:
		return "INFO"
	case WarningLevel:
		return "WARN"
	case ErrorLevel:
		return "ERRO"
	case CriticalLevel:
		return "CRIT"
	default:
		return "NONE"
	}
}

func formatLine(line *logLine, duplicates uint64, useColor bool) string {
	colorStart := ""
	colorEnd := ""
	if useColor {
		colorStart = line.level.color()
		colorEnd = endColor()
	}

	counter++

	var fLine string
	if line.line == 0 {
		fLine = fmt.Sprintf("%s%s ? %s %s %03d%s%s %s", colorStart, line.timestamp.Format(timeFormat), rightArrow, line.level.String(), counter, formatDuplicates(duplicates), colorEnd, line.msg)
	} else {
		fLen := len(line.file)
		fPartStart := fLen - 10
		if fPartStart < 0 {
			fPartStart = 0
		}
		fLine = fmt.Sprintf("%s%s %s:%03d %s %s %03d%s%s %s", colorStart, line.timestamp.Format(timeFormat), line.file[fPartStart:], line.line, rightArrow, line.level.String(), counter, formatDuplicates(duplicates), colorEnd, line.msg)
	}

	if line.tracer != nil {
		// append full trace time
		if len(line.tracer.logs) > 0 {
			fLine += fmt.Sprintf(" Σ=%s", line.timestamp.Sub(line.tracer.logs[0].timestamp))
		}

		// append all trace actions
		var d time.Duration
		for i, action := range line.tracer.logs {
			// set color
			if useColor {
				colorStart = action.level.color()
			}
			// set filename length
			fLen := len(action.file)
			fPartStart := fLen - 10
			if fPartStart < 0 {
				fPartStart = 0
			}
			// format
			if i == len(line.tracer.logs)-1 { // last
				d = line.timestamp.Sub(action.timestamp)
			} else {
				d = line.tracer.logs[i+1].timestamp.Sub(action.timestamp)
			}
			fLine += fmt.Sprintf("\n%s%19s %s:%03d %s %s%s     %s", colorStart, d, action.file[fPartStart:], action.line, rightArrow, action.level.String(), colorEnd, action.msg)
		}
	}

	if counter >= maxCount {
		counter = 0
	}

	return fLine
}

func formatDuplicates(duplicates uint64) string {
	if duplicates == 0 {
		return ""
	}
	return fmt.Sprintf(" [%dx]", duplicates+1)
}
