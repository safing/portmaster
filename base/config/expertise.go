package config

import (
	"sync/atomic"

	"github.com/tevino/abool"
)

// ExpertiseLevel allows to group settings by user expertise.
// It's useful if complex or technical settings should be hidden
// from the average user while still allowing experts and developers
// to change deep configuration settings.
type ExpertiseLevel uint8

// Expertise Level constants.
const (
	ExpertiseLevelUser      ExpertiseLevel = 0
	ExpertiseLevelExpert    ExpertiseLevel = 1
	ExpertiseLevelDeveloper ExpertiseLevel = 2

	ExpertiseLevelNameUser      = "user"
	ExpertiseLevelNameExpert    = "expert"
	ExpertiseLevelNameDeveloper = "developer"

	expertiseLevelKey = "core/expertiseLevel"
)

var (
	expertiseLevelOption     *Option
	expertiseLevel           = new(int32)
	expertiseLevelOptionFlag = abool.New()
)

func init() {
	registerExpertiseLevelOption()
}

func registerExpertiseLevelOption() {
	expertiseLevelOption = &Option{
		Name:           "界面模式",
		Key:            expertiseLevelKey,
		Description:    "控制默认显示的设置和信息的数量。隐藏的设置仍然有效。可以在右上角临时更改。",
		OptType:        OptTypeString,
		ExpertiseLevel: ExpertiseLevelUser,
		ReleaseLevel:   ReleaseLevelStable,
		DefaultValue:   ExpertiseLevelNameUser,
		Annotations: Annotations{
			DisplayOrderAnnotation: -16,
			DisplayHintAnnotation:  DisplayHintOneOf,
			CategoryAnnotation:     "用户界面",
		},
		PossibleValues: []PossibleValue{
			{
				Name:        "简单界面",
				Value:       ExpertiseLevelNameUser,
				Description: "隐藏复杂的设置和信息。",
			},
			{
				Name:        "高级界面",
				Value:       ExpertiseLevelNameExpert,
				Description: "显示技术细节。",
			},
			{
				Name:        "开发者界面",
				Value:       ExpertiseLevelNameDeveloper,
				Description: "开发者模式。请谨慎操作！",
			},
		},
	}

	err := Register(expertiseLevelOption)
	if err != nil {
		panic(err)
	}

	expertiseLevelOptionFlag.Set()
}

func updateExpertiseLevel() {
	// get value
	value := expertiseLevelOption.activeFallbackValue
	if expertiseLevelOption.activeValue != nil {
		value = expertiseLevelOption.activeValue
	}
	if expertiseLevelOption.activeDefaultValue != nil {
		value = expertiseLevelOption.activeDefaultValue
	}
	// set atomic value
	switch value.stringVal {
	case ExpertiseLevelNameUser:
		atomic.StoreInt32(expertiseLevel, int32(ExpertiseLevelUser))
	case ExpertiseLevelNameExpert:
		atomic.StoreInt32(expertiseLevel, int32(ExpertiseLevelExpert))
	case ExpertiseLevelNameDeveloper:
		atomic.StoreInt32(expertiseLevel, int32(ExpertiseLevelDeveloper))
	default:
		atomic.StoreInt32(expertiseLevel, int32(ExpertiseLevelUser))
	}
}

// GetExpertiseLevel returns the current active expertise level.
func GetExpertiseLevel() uint8 {
	return uint8(atomic.LoadInt32(expertiseLevel))
}
