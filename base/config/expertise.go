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
		Name:           "UI Mode",
		Key:            expertiseLevelKey,
		Description:    "Control the default amount of settings and information shown. Hidden settings are still in effect. Can be changed temporarily in the top right corner.",
		OptType:        OptTypeString,
		ExpertiseLevel: ExpertiseLevelUser,
		ReleaseLevel:   ReleaseLevelStable,
		DefaultValue:   ExpertiseLevelNameUser,
		Annotations: Annotations{
			DisplayOrderAnnotation: -16,
			DisplayHintAnnotation:  DisplayHintOneOf,
			CategoryAnnotation:     "User Interface",
		},
		PossibleValues: []PossibleValue{
			{
				Name:        "Simple Interface",
				Value:       ExpertiseLevelNameUser,
				Description: "Hide complex settings and information.",
			},
			{
				Name:        "Advanced Interface",
				Value:       ExpertiseLevelNameExpert,
				Description: "Show technical details.",
			},
			{
				Name:        "Developer Interface",
				Value:       ExpertiseLevelNameDeveloper,
				Description: "Developer mode. Please be careful!",
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
