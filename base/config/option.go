package config

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"sync"

	"github.com/mitchellh/copystructure"
	"github.com/tidwall/sjson"

	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/structures/dsd"
)

// OptionType defines the value type of an option.
type OptionType uint8

// Various attribute options. Use ExternalOptType for extended types in the frontend.
const (
	optTypeAny         OptionType = 0
	OptTypeString      OptionType = 1
	OptTypeStringArray OptionType = 2
	OptTypeInt         OptionType = 3
	OptTypeBool        OptionType = 4
)

func getTypeName(t OptionType) string {
	switch t {
	case optTypeAny:
		return "any"
	case OptTypeString:
		return "string"
	case OptTypeStringArray:
		return "[]string"
	case OptTypeInt:
		return "int"
	case OptTypeBool:
		return "bool"
	default:
		return "unknown"
	}
}

// PossibleValue defines a value that is possible for
// a configuration setting.
type PossibleValue struct {
	// Name is a human readable name of the option.
	Name string
	// Description is a human readable description of
	// this value.
	Description string
	// Value is the actual value of the option. The type
	// must match the option's value type.
	Value interface{}
}

// Annotations can be attached to configuration options to
// provide hints for user interfaces or other systems working
// or setting configuration options.
// Annotation keys should follow the below format to ensure
// future well-known annotation additions do not conflict
// with vendor/product/package specific annoations.
//
// Format: <vendor/package>:<scope>:<identifier> //.
type Annotations map[string]interface{}

// MigrationFunc is a function that migrates a config option value.
type MigrationFunc func(option *Option, value any) any

// Well known annotations defined by this package.
const (
	// DisplayHintAnnotation provides a hint for the user
	// interface on how to render an option.
	// The value of DisplayHintAnnotation is expected to
	// be a string. See DisplayHintXXXX constants below
	// for a list of well-known display hint annotations.
	DisplayHintAnnotation = "safing/portbase:ui:display-hint"
	// DisplayOrderAnnotation provides a hint for the user
	// interface in which order settings should be displayed.
	// The value of DisplayOrderAnnotations is expected to be
	// an number (int).
	DisplayOrderAnnotation = "safing/portbase:ui:order"
	// UnitAnnotations defines the SI unit of an option (if any).
	UnitAnnotation = "safing/portbase:ui:unit"
	// CategoryAnnotations can provide an additional category
	// to each settings. This category can be used by a user
	// interface to group certain options together.
	// User interfaces should treat a CategoryAnnotation, if
	// supported, with higher priority as a DisplayOrderAnnotation.
	CategoryAnnotation = "safing/portbase:ui:category"
	// SubsystemAnnotation can be used to mark an option as part
	// of a module subsystem.
	SubsystemAnnotation = "safing/portbase:module:subsystem"
	// StackableAnnotation can be set on configuration options that
	// stack on top of the default (or otherwise related) options.
	// The value of StackableAnnotaiton is expected to be a boolean but
	// may be extended to hold references to other options in the
	// future.
	StackableAnnotation = "safing/portbase:options:stackable"
	// RestartPendingAnnotation is automatically set on a configuration option
	// that requires a restart and has been changed.
	// The value must always be a boolean with value "true".
	RestartPendingAnnotation = "safing/portbase:options:restart-pending"
	// QuickSettingAnnotation can be used to add quick settings to
	// a configuration option. A quick setting can support the user
	// by switching between pre-configured values.
	// The type of a quick-setting annotation is []QuickSetting or QuickSetting.
	QuickSettingsAnnotation = "safing/portbase:ui:quick-setting"
	// RequiresAnnotation can be used to mark another option as a
	// requirement. The type of RequiresAnnotation is []ValueRequirement
	// or ValueRequirement.
	RequiresAnnotation = "safing/portbase:config:requires"
	// RequiresFeatureIDAnnotation can be used to mark a setting as only available
	// when the user has a certain feature ID in the subscription plan.
	// The type is []string or string.
	RequiresFeatureIDAnnotation = "safing/portmaster:ui:config:requires-feature"
	// SettablePerAppAnnotation can be used to mark a setting as settable per-app and
	// is a boolean.
	SettablePerAppAnnotation = "safing/portmaster:settable-per-app"
	// RequiresUIReloadAnnotation can be used to inform the UI that changing the value
	// of the annotated setting requires a full reload of the user interface.
	// The value of this annotation does not matter as the sole presence of
	// the annotation key is enough. Though, users are advised to set the value
	// of this annotation to true.
	RequiresUIReloadAnnotation = "safing/portmaster:ui:requires-reload"
)

// QuickSettingsAction defines the action of a quick setting.
type QuickSettingsAction string

const (
	// QuickReplace replaces the current setting with the one from
	// the quick setting.
	QuickReplace = QuickSettingsAction("replace")
	// QuickMergeTop merges the value of the quick setting with the
	// already configured one adding new values on the top. Merging
	// is only supported for OptTypeStringArray.
	QuickMergeTop = QuickSettingsAction("merge-top")
	// QuickMergeBottom merges the value of the quick setting with the
	// already configured one adding new values at the bottom. Merging
	// is only supported for OptTypeStringArray.
	QuickMergeBottom = QuickSettingsAction("merge-bottom")
)

// QuickSetting defines a quick setting for a configuration option and
// should be used together with the QuickSettingsAnnotation.
type QuickSetting struct {
	// Name is the name of the quick setting.
	Name string

	// Value is the value that the quick-setting configures. It must match
	// the expected value type of the annotated option.
	Value interface{}

	// Action defines the action of the quick setting.
	Action QuickSettingsAction
}

// ValueRequirement defines a requirement on another configuration option.
type ValueRequirement struct {
	// Key is the key of the configuration option that is required.
	Key string

	// Value that is required.
	Value interface{}
}

// Values for the DisplayHintAnnotation.
const (
	// DisplayHintOneOf is used to mark an option
	// as a "select"-style option. That is, only one of
	// the supported values may be set. This option makes
	// only sense together with the PossibleValues property
	// of Option.
	DisplayHintOneOf = "one-of"
	// DisplayHintOrdered is used to mark a list option as ordered.
	// That is, the order of items is important and a user interface
	// is encouraged to provide the user with re-ordering support
	// (like drag'n'drop).
	DisplayHintOrdered = "ordered"
	// DisplayHintFilePicker is used to mark the option as being a file, which
	// should give the option to use a file picker to select a local file from disk.
	DisplayHintFilePicker = "file-picker"
)

// Option describes a configuration option.
type Option struct {
	sync.Mutex
	// Name holds the name of the configuration options.
	// It should be human readable and is mainly used for
	// presentation purposes.
	// Name is considered immutable after the option has
	// been created.
	Name string
	// Key holds the database path for the option. It should
	// follow the path format `category/sub/key`.
	// Key is considered immutable after the option has
	// been created.
	Key string
	// Description holds a human readable description of the
	// option and what is does. The description should be short.
	// Use the Help property for a longer support text.
	// Description is considered immutable after the option has
	// been created.
	Description string
	// Help may hold a long version of the description providing
	// assistance with the configuration option.
	// Help is considered immutable after the option has
	// been created.
	Help string
	// Sensitive signifies that the configuration values may contain sensitive
	// content, such as authentication keys.
	Sensitive bool
	// OptType defines the type of the option.
	// OptType is considered immutable after the option has
	// been created.
	OptType OptionType
	// ExpertiseLevel can be used to set the required expertise
	// level for the option to be displayed to a user.
	// ExpertiseLevel is considered immutable after the option has
	// been created.
	ExpertiseLevel ExpertiseLevel
	// ReleaseLevel is used to mark the stability of the option.
	// ReleaseLevel is considered immutable after the option has
	// been created.
	ReleaseLevel ReleaseLevel
	// RequiresRestart should be set to true if a modification of
	// the options value requires a restart of the whole application
	// to take effect.
	// RequiresRestart is considered immutable after the option has
	// been created.
	RequiresRestart bool
	// DefaultValue holds the default value of the option. Note that
	// this value can be overwritten during runtime (see activeDefaultValue
	// and activeFallbackValue).
	// DefaultValue is considered immutable after the option has
	// been created.
	DefaultValue interface{}
	// ValidationRegex may contain a regular expression used to validate
	// the value of option. If the option type is set to OptTypeStringArray
	// the validation regex is applied to all entries of the string slice.
	// Note that it is recommended to keep the validation regex simple so
	// it can also be used in other languages (mainly JavaScript) to provide
	// a better user-experience by pre-validating the expression.
	// ValidationRegex is considered immutable after the option has
	// been created.
	ValidationRegex string
	// ValidationFunc may contain a function to validate more complex values.
	// The error is returned beyond the scope of this package and may be
	// displayed to a user.
	ValidationFunc func(value interface{}) error `json:"-"`
	// PossibleValues may be set to a slice of values that are allowed
	// for this configuration setting. Note that PossibleValues makes most
	// sense when ExternalOptType is set to HintOneOf
	// PossibleValues is considered immutable after the option has
	// been created.
	PossibleValues []PossibleValue `json:",omitempty"`
	// Annotations adds additional annotations to the configuration options.
	// See documentation of Annotations for more information.
	// Annotations is considered mutable and setting/reading annotation keys
	// must be performed while the option is locked.
	Annotations Annotations
	// Migrations holds migration functions that are given the raw option value
	// before any validation is run. The returned value is then used.
	Migrations []MigrationFunc `json:"-"`

	activeValue         *valueCache // runtime value (loaded from config file or set by user)
	activeDefaultValue  *valueCache // runtime default value (may be set internally)
	activeFallbackValue *valueCache // default value from option registration
	compiledRegex       *regexp.Regexp
}

// AddAnnotation adds the annotation key to option if it's not already set.
func (option *Option) AddAnnotation(key string, value interface{}) {
	option.Lock()
	defer option.Unlock()

	if option.Annotations == nil {
		option.Annotations = make(Annotations)
	}

	if _, ok := option.Annotations[key]; ok {
		return
	}
	option.Annotations[key] = value
}

// SetAnnotation sets the value of the annotation key overwritting an
// existing value if required.
func (option *Option) SetAnnotation(key string, value interface{}) {
	option.Lock()
	defer option.Unlock()

	option.setAnnotation(key, value)
}

// setAnnotation sets the value of the annotation key overwritting an
// existing value if required. Does not lock the Option.
func (option *Option) setAnnotation(key string, value interface{}) {
	if option.Annotations == nil {
		option.Annotations = make(Annotations)
	}
	option.Annotations[key] = value
}

// GetAnnotation returns the value of the annotation key.
func (option *Option) GetAnnotation(key string) (interface{}, bool) {
	option.Lock()
	defer option.Unlock()

	if option.Annotations == nil {
		return nil, false
	}
	val, ok := option.Annotations[key]
	return val, ok
}

// AnnotationEquals returns whether the annotation of the given key matches the
// given value.
func (option *Option) AnnotationEquals(key string, value any) bool {
	option.Lock()
	defer option.Unlock()

	if option.Annotations == nil {
		return false
	}
	setValue, ok := option.Annotations[key]
	if !ok {
		return false
	}
	return reflect.DeepEqual(value, setValue)
}

// copyOrNil returns a copy of the option, or nil if copying failed.
func (option *Option) copyOrNil() *Option {
	copied, err := copystructure.Copy(option)
	if err != nil {
		return nil
	}
	return copied.(*Option) //nolint:forcetypeassert
}

// IsSetByUser returns whether the option has been set by the user.
func (option *Option) IsSetByUser() bool {
	option.Lock()
	defer option.Unlock()

	return option.activeValue != nil
}

// UserValue returns the value set by the user or nil if the value has not
// been changed from the default.
func (option *Option) UserValue() any {
	option.Lock()
	defer option.Unlock()

	if option.activeValue == nil {
		return nil
	}
	return option.activeValue.getData(option)
}

// ValidateValue checks if the given value is valid for the option.
func (option *Option) ValidateValue(value any) error {
	option.Lock()
	defer option.Unlock()

	value = migrateValue(option, value)
	if _, err := validateValue(option, value); err != nil {
		return err
	}
	return nil
}

// Export expors an option to a Record.
func (option *Option) Export() (record.Record, error) {
	option.Lock()
	defer option.Unlock()

	return option.export()
}

func (option *Option) export() (record.Record, error) {
	data, err := json.Marshal(option)
	if err != nil {
		return nil, err
	}

	if option.activeValue != nil {
		data, err = sjson.SetBytes(data, "Value", option.activeValue.getData(option))
		if err != nil {
			return nil, err
		}
	}

	if option.activeDefaultValue != nil {
		data, err = sjson.SetBytes(data, "DefaultValue", option.activeDefaultValue.getData(option))
		if err != nil {
			return nil, err
		}
	}

	r, err := record.NewWrapper(fmt.Sprintf("config:%s", option.Key), nil, dsd.JSON, data)
	if err != nil {
		return nil, err
	}
	r.SetMeta(&record.Meta{})

	return r, nil
}

type sortByKey []*Option

func (opts sortByKey) Len() int           { return len(opts) }
func (opts sortByKey) Less(i, j int) bool { return opts[i].Key < opts[j].Key }
func (opts sortByKey) Swap(i, j int)      { opts[i], opts[j] = opts[j], opts[i] }
