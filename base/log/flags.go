package log

import "flag"

var (
	logLevelFlag     string
	pkgLogLevelsFlag string
)

func init() {
	flag.StringVar(&logLevelFlag, "log", "", "set log level to [trace|debug|info|warning|error|critical]")
	flag.StringVar(&pkgLogLevelsFlag, "plog", "", "set log level of packages: database=trace,notifications=debug")
}
