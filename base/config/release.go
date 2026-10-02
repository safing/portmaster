package config

import (
	"sync/atomic"

	"github.com/tevino/abool"
)

// ReleaseLevel is used to define the maturity of a
// configuration setting.
type ReleaseLevel uint8

// Release Level constants.
const (
	ReleaseLevelStable       ReleaseLevel = 0
	ReleaseLevelBeta         ReleaseLevel = 1
	ReleaseLevelExperimental ReleaseLevel = 2

	ReleaseLevelNameStable       = "stable"
	ReleaseLevelNameBeta         = "beta"
	ReleaseLevelNameExperimental = "experimental"

	releaseLevelKey = "core/releaseLevel"
)

var (
	releaseLevel           = new(int32)
	releaseLevelOption     *Option
	releaseLevelOptionFlag = abool.New()
)

func init() {
	registerReleaseLevelOption()
}

func registerReleaseLevelOption() {
	releaseLevelOption = &Option{
		Name:           "功能稳定性",
		Key:            releaseLevelKey,
		Description:    `可能导致问题。决定是否要尝试不稳定的功能。“测试”功能已经过 Safing 团队的初步测试，而“实验”功能则非常不成熟。禁用“测试”或“实验”后，其设置将恢复为默认值。`,
		OptType:        OptTypeString,
		ExpertiseLevel: ExpertiseLevelDeveloper,
		ReleaseLevel:   ReleaseLevelStable,
		DefaultValue:   ReleaseLevelNameStable,
		Annotations: Annotations{
			DisplayOrderAnnotation: -8,
			DisplayHintAnnotation:  DisplayHintOneOf,
			CategoryAnnotation:     "更新",
		},
		PossibleValues: []PossibleValue{
			{
				Name:        "稳定",
				Value:       ReleaseLevelNameStable,
				Description: "仅显示稳定功能。",
			},
			{
				Name:        "测试",
				Value:       ReleaseLevelNameBeta,
				Description: "显示稳定和测试功能。",
			},
			{
				Name:        "实验",
				Value:       ReleaseLevelNameExperimental,
				Description: "显示所有功能",
			},
		},
	}

	err := Register(releaseLevelOption)
	if err != nil {
		panic(err)
	}

	releaseLevelOptionFlag.Set()
}

func updateReleaseLevel() {
	// get value
	value := releaseLevelOption.activeFallbackValue
	if releaseLevelOption.activeValue != nil {
		value = releaseLevelOption.activeValue
	}
	if releaseLevelOption.activeDefaultValue != nil {
		value = releaseLevelOption.activeDefaultValue
	}
	// set atomic value
	switch value.stringVal {
	case ReleaseLevelNameStable:
		atomic.StoreInt32(releaseLevel, int32(ReleaseLevelStable))
	case ReleaseLevelNameBeta:
		atomic.StoreInt32(releaseLevel, int32(ReleaseLevelBeta))
	case ReleaseLevelNameExperimental:
		atomic.StoreInt32(releaseLevel, int32(ReleaseLevelExperimental))
	default:
		atomic.StoreInt32(releaseLevel, int32(ReleaseLevelStable))
	}
}

func getReleaseLevel() ReleaseLevel {
	return ReleaseLevel(atomic.LoadInt32(releaseLevel))
}
