package metrics

import (
	"flag"
	"os"
	"strings"

	"github.com/safing/portmaster/base/config"
)

// Configuration Keys.
var (
	CfgOptionInstanceKey   = "core/metrics/instance"
	instanceOption         config.StringOption
	cfgOptionInstanceOrder = 0

	CfgOptionCommentKey   = "core/metrics/comment"
	commentOption         config.StringOption
	cfgOptionCommentOrder = 0

	CfgOptionPushKey   = "core/metrics/push"
	pushOption         config.StringOption
	cfgOptionPushOrder = 0

	instanceFlag    string
	defaultInstance string
	commentFlag     string
	pushFlag        string
)

func init() {
	hostname, err := os.Hostname()
	if err == nil {
		hostname = strings.ReplaceAll(hostname, "-", "")
		if prometheusFormat.MatchString(hostname) {
			defaultInstance = hostname
		}
	}

	flag.StringVar(&instanceFlag, "metrics-instance", defaultInstance, "set the default metrics instance label for all metrics")
	flag.StringVar(&commentFlag, "metrics-comment", "", "set the default metrics comment label")
	flag.StringVar(&pushFlag, "push-metrics", "", "set default URL to push prometheus metrics to")
}

func prepConfig() error {
	err := config.Register(&config.Option{
		Name:            "Metrics Instance Name",
		Key:             CfgOptionInstanceKey,
		Description:     "Define the prometheus instance label for all exported metrics. Please note that changing the metrics instance name will reset persisted metrics.",
		Sensitive:       true,
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		DefaultValue:    instanceFlag,
		RequiresRestart: true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionInstanceOrder,
			config.CategoryAnnotation:     "Metrics",
		},
		ValidationRegex: "^(" + prometheusBaseFormt + ")?$",
	})
	if err != nil {
		return err
	}
	instanceOption = config.Concurrent.GetAsString(CfgOptionInstanceKey, instanceFlag)

	err = config.Register(&config.Option{
		Name:            "Metrics Comment Label",
		Key:             CfgOptionCommentKey,
		Description:     "Define a metrics comment label, which is added to the info metric.",
		Sensitive:       true,
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		DefaultValue:    commentFlag,
		RequiresRestart: true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionCommentOrder,
			config.CategoryAnnotation:     "Metrics",
		},
	})
	if err != nil {
		return err
	}
	commentOption = config.Concurrent.GetAsString(CfgOptionCommentKey, commentFlag)

	err = config.Register(&config.Option{
		Name:            "Push Prometheus Metrics",
		Key:             CfgOptionPushKey,
		Description:     "Push metrics to this URL in the prometheus format.",
		Sensitive:       true,
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		DefaultValue:    pushFlag,
		RequiresRestart: true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionPushOrder,
			config.CategoryAnnotation:     "Metrics",
		},
	})
	if err != nil {
		return err
	}
	pushOption = config.Concurrent.GetAsString(CfgOptionPushKey, pushFlag)

	return nil
}
