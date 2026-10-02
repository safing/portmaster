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
		Name:            "指标实例名称",
		Key:             CfgOptionInstanceKey,
		Description:     "为所有导出的指标定义 Prometheus 实例标签。请注意，更改指标实例名称将重置已持久化的指标。",
		Sensitive:       true,
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		DefaultValue:    instanceFlag,
		RequiresRestart: true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionInstanceOrder,
			config.CategoryAnnotation:     "指标",
		},
		ValidationRegex: "^(" + prometheusBaseFormt + ")?$",
	})
	if err != nil {
		return err
	}
	instanceOption = config.Concurrent.GetAsString(CfgOptionInstanceKey, instanceFlag)

	err = config.Register(&config.Option{
		Name:            "指标注释标签",
		Key:             CfgOptionCommentKey,
		Description:     "定义一个指标注释标签，它将被添加到 info 指标中。",
		Sensitive:       true,
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		DefaultValue:    commentFlag,
		RequiresRestart: true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionCommentOrder,
			config.CategoryAnnotation:     "指标",
		},
	})
	if err != nil {
		return err
	}
	commentOption = config.Concurrent.GetAsString(CfgOptionCommentKey, commentFlag)

	err = config.Register(&config.Option{
		Name:            "推送 Prometheus 指标",
		Key:             CfgOptionPushKey,
		Description:     "以 Prometheus 格式将指标推送到此 URL。",
		Sensitive:       true,
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		DefaultValue:    pushFlag,
		RequiresRestart: true,
		Annotations: config.Annotations{
			config.DisplayOrderAnnotation: cfgOptionPushOrder,
			config.CategoryAnnotation:     "指标",
		},
	})
	if err != nil {
		return err
	}
	pushOption = config.Concurrent.GetAsString(CfgOptionPushKey, pushFlag)

	return nil
}
