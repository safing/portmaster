package log

import (
	"fmt"
	"time"
)

var counter uint16

const (
	maxCount   uint16 = 999
	timeFormat string = "2006-01-02 15:04:05.000"
)

func (s Severity) String() string {
	switch s {
	case TraceLevel:
		return "TRC"
	case DebugLevel:
		return "DBG"
	case InfoLevel:
		return "INF"
	case WarningLevel:
		return "WRN"
	case ErrorLevel:
		return "ERR"
	case CriticalLevel:
		return "CRT"
	default:
		return "NON"
	}
}

func formatLine(line *logLine, duplicates uint64, useColor bool) string {
	var colorStart, colorEnd, colorDim, colorEndDim string
	if useColor {
		colorStart = line.level.color()
		colorEnd = endColor()
		colorDim = dimColor()
		colorEndDim = endDimColor()
	}

	counter++

	var fLine string
	if line.line == 0 {
		fLine = fmt.Sprintf(
			"%s%s%s %s%s%s %s? %s %03d%s%s %s",

			colorDim,
			line.timestamp.Format(timeFormat),
			colorEndDim,

			colorStart,
			line.level.String(),
			colorEnd,

			colorDim,

			rightArrow,

			counter,
			formatDuplicates(duplicates),
			colorEndDim,

			line.msg,
		)
	} else {
		fLen := len(line.file)
		fPartStart := fLen - 10
		if fPartStart < 0 {
			fPartStart = 0
		}
		fLine = fmt.Sprintf(
			"%s%s%s %s%s%s %s%s:%03d %s %03d%s%s %s",

			colorDim,
			line.timestamp.Format(timeFormat),
			colorEndDim,

			colorStart,
			line.level.String(),
			colorEnd,

			colorDim,
			line.file[fPartStart:],
			line.line,

			rightArrow,

			counter,
			formatDuplicates(duplicates),
			colorEndDim,

			line.msg,
		)
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
			fLine += fmt.Sprintf(
				"\n%s%23s%s %s%s%s %s%s:%03d %s%s     %s",
				colorDim,
				d,
				colorEndDim,

				colorStart,
				action.level.String(),
				colorEnd,

				colorDim,
				action.file[fPartStart:],
				action.line,

				rightArrow,
				colorEndDim,

				action.msg,
			)
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
