package internal

import (
	"fmt"

	"github.com/safing/portbase/config"
	"github.com/safing/portmaster/plugin/shared/proto"
)

func GetConfigValueProto(key string) (*proto.Value, error) {
	opt, err := config.GetOption(key)
	if err != nil {
		return nil, err
	}

	var value *proto.Value

	switch opt.OptType {
	case config.OptTypeBool:
		value = &proto.Value{
			Bool: config.Concurrent.GetAsBool(opt.Key, false)(),
		}

	case config.OptTypeInt:
		value = &proto.Value{
			Int: config.Concurrent.GetAsInt(opt.Key, 0)(),
		}

	case config.OptTypeString:
		value = &proto.Value{
			String_: config.Concurrent.GetAsString(opt.Key, "")(),
		}

	case config.OptTypeStringArray:
		value = &proto.Value{
			StringArray: config.Concurrent.GetAsStringArray(opt.Key, []string{})(),
		}

	default:
		return nil, fmt.Errorf("unsupported option type %d", opt.OptType)
	}

	return value, nil
}
