package proto

import (
	"fmt"

	"github.com/safing/portbase/config"
	"github.com/safing/portbase/notifications"
	"github.com/safing/portmaster/intel"
	"github.com/safing/portmaster/intel/geoip"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/network/packet"
)

func VerdictToNetwork(verdict Verdict) network.Verdict {
	switch verdict {
	case Verdict_VERDICT_ACCEPT:
		return network.VerdictAccept
	case Verdict_VERDICT_BLOCK:
		return network.VerdictBlock
	case Verdict_VERDICT_DROP:
		return network.VerdictDrop
	case Verdict_VERDICT_FAILED:
		return network.VerdictFailed
	case Verdict_VERDICT_REROUTE_TO_NS:
		return network.VerdictRerouteToNameserver
	case Verdict_VERDICT_REROUTE_TO_TUNNEL:
		return network.VerdictRerouteToTunnel
	case Verdict_VERDICT_UNDECIDED:
		return network.VerdictUndecided
	case Verdict_VERDICT_UNDETERMINABLE:
		return network.VerdictUndeterminable

	default:
		return network.VerdictUndecided
	}
}

func ConnectionFromNetwork(conn *network.Connection) *Connection {
	if conn == nil {
		return nil
	}

	protoConn := &Connection{
		Id:         conn.ID,
		Type:       ConnTypeFromNetwork(conn.Type),
		External:   conn.External,
		IpVersion:  IPVersionFromNetwork(conn.IPVersion),
		Inbound:    conn.Inbound,
		IpProtocol: int32(conn.IPProtocol),
		LocalIp:    conn.LocalIP.String(),
		LocalPort:  int32(conn.LocalPort),
		Entity:     IntelEntityFromNetwork(conn.Entity),
		Started:    uint64(conn.Started),
		Tunneled:   conn.Tunneled,
		Process:    ProcessFromNetwork(conn.ProcessContext),
		Verdict:    VerdictFromNetwork(conn.Verdict),
	}

	return protoConn
}

func VerdictFromNetwork(verdict network.Verdict) Verdict {
	switch verdict {
	case network.VerdictAccept:
		return Verdict_VERDICT_ACCEPT
	case network.VerdictBlock:
		return Verdict_VERDICT_BLOCK
	case network.VerdictDrop:
		return Verdict_VERDICT_DROP
	case network.VerdictFailed:
		return Verdict_VERDICT_FAILED
	case network.VerdictRerouteToNameserver:
		return Verdict_VERDICT_REROUTE_TO_NS
	case network.VerdictRerouteToTunnel:
		return Verdict_VERDICT_REROUTE_TO_TUNNEL
	case network.VerdictUndeterminable:
		return Verdict_VERDICT_UNDETERMINABLE
	case network.VerdictUndecided:
		fallthrough
	default:
		return Verdict_VERDICT_UNDECIDED
	}
}

func ConnTypeFromNetwork(connType network.ConnectionType) ConnectionType {
	switch connType {
	case network.IPConnection:
		return ConnectionType_CONNECTION_TYPE_IP

	case network.DNSRequest:
		return ConnectionType_CONNECTION_TYPE_DNS

	default:
		return ConnectionType_CONNECTION_TYPE_UNKNOWN
	}
}

func IPVersionFromNetwork(ipVersion packet.IPVersion) IPVersion {
	switch ipVersion {
	case packet.IPv4:
		return IPVersion_IP_VERSION_4
	case packet.IPv6:
		return IPVersion_IP_VERSION_6
	default:
		return IPVersion_IP_VERSION_UNKNOWN
	}
}

func IntelEntityFromNetwork(entity *intel.Entity) *IntelEntity {
	if entity == nil {
		return nil
	}

	return &IntelEntity{
		Protocol:      int32(entity.Protocol),
		Port:          int32(entity.Port),
		Domain:        entity.Domain,
		Cnames:        entity.CNAME,
		ReverseDomain: entity.ReverseDomain,
		Ip:            entity.IP.String(),
		Scope:         IPScopeFromNetwork(entity.IPScope),
		Country:       entity.Country,
		Asn:           int32(entity.ASN),
		AsOwner:       entity.ASOrg,
		Coordinates:   CoordinatesFromNetwork(entity.Coordinates),
	}
}

func IPScopeFromNetwork(scope netutils.IPScope) IPScope {
	switch scope {
	case netutils.Undefined:
		return IPScope_IP_SCOPE_UNKNOWN
	case netutils.HostLocal:
		return IPScope_IP_SCOPE_HOST_LOCAL
	case netutils.LinkLocal:
		return IPScope_IP_SCOPE_LINK_LOCAL
	case netutils.SiteLocal:
		return IPScope_IP_SCOPE_SITE_LOCAL
	case netutils.Global:
		return IPScope_IP_SCOPE_GLOBAL
	case netutils.GlobalMulticast:
		return IPScope_IP_SCOPE_GLOBAL_MULTICAST
	case netutils.LocalMulticast:
		return IPScope_IP_SCOPE_LOCAL_MULTICAST

	default:
		return IPScope_IP_SCOPE_UNKNOWN
	}
}

func ProcessFromNetwork(process network.ProcessContext) *ProcessContext {
	return &ProcessContext{
		Name:        process.ProfileName,
		Profile:     process.Profile,
		BinaryPath:  process.BinaryPath,
		CommandLine: process.CmdLine,
		ProcessId:   int64(process.PID),
		Source:      process.Source,
	}
}

func CoordinatesFromNetwork(coord *geoip.Coordinates) *Coordinates {
	if coord == nil {
		return nil
	}

	return &Coordinates{
		AccuracyRadius: int32(coord.AccuracyRadius),
		Latitude:       float32(coord.Latitude),
		Longitude:      float32(coord.Longitude),
	}
}

func OptionTypeToConfig(optType OptionType) config.OptionType {
	switch optType {
	case OptionType_OPTION_TYPE_BOOL:
		return config.OptTypeBool
	case OptionType_OPTION_TYPE_INT:
		return config.OptTypeInt
	case OptionType_OPTION_TYPE_STRING:
		return config.OptTypeString
	case OptionType_OPTION_TYPE_STRING_ARRAY:
		return config.OptTypeStringArray
	case OptionType_OPTION_TYPE_ANY:
		fallthrough
	default:
		return config.OptionType(0)
	}
}

func UnwrapConfigValue(value *Value, valueType config.OptionType) (any, error) {
	if value == nil {
		return nil, fmt.Errorf("nil value")
	}

	switch valueType {
	case config.OptTypeBool:
		return value.GetBool(), nil
	case config.OptTypeInt:
		return value.GetInt(), nil
	case config.OptTypeString:
		return value.GetString_(), nil
	case config.OptTypeStringArray:
		return value.GetStringArray(), nil
	}

	return nil, fmt.Errorf("unsupported option type %d", valueType)
}

func WrapConfigValue(x interface{}, valueType config.OptionType) (*Value, error) {
	if x == nil {
		return &Value{}, nil
	}

	switch valueType {
	case config.OptTypeBool:
		return &Value{
			Bool: x.(bool),
		}, nil
	case config.OptTypeInt:
		return &Value{
			Int: int64(x.(int)),
		}, nil
	case config.OptTypeString:
		return &Value{
			String_: x.(string),
		}, nil
	case config.OptTypeStringArray:
		return &Value{
			StringArray: x.([]string),
		}, nil
	}

	return nil, fmt.Errorf("unsupported option type")
}

func NotificationFromProto(notif *Notification) *notifications.Notification {
	if notif == nil {
		return nil
	}

	actions := make([]*notifications.Action, len(notif.GetActions()))
	for idx, protoAction := range notif.GetActions() {
		actions[idx] = NotificationActionTypeFromProto(protoAction)
	}

	return &notifications.Notification{
		EventID:          notif.GetEventId(),
		GUID:             notif.GetGuid(),
		Type:             NotificationTypeFromProto(notif.GetType()),
		State:            NotificationStateFromProto(notif.GetState()),
		AvailableActions: actions,
		Title:            notif.GetTitle(),
		Category:         notif.GetCategory(),
		Message:          notif.GetMessage(),
		ShowOnSystem:     notif.GetShowOnSystem(),
		Expires:          notif.GetExpires(),
		SelectedActionID: "",
	}
}

func NotificationActionTypeFromProto(action *NotificationAction) *notifications.Action {
	res := &notifications.Action{
		ID:   action.GetId(),
		Text: action.GetText(),
	}

	switch payload := action.GetActionType().(type) {
	case *NotificationAction_InjectEventId:
		res.Payload = payload.InjectEventId

	case *NotificationAction_OpenPage:
		res.Payload = payload.OpenPage

	case *NotificationAction_OpenProfile:
		res.Payload = payload.OpenProfile

	case *NotificationAction_OpenSetting:
		res.Payload = notifications.ActionTypeOpenSettingPayload{
			Key:     payload.OpenSetting.Key,
			Profile: payload.OpenSetting.Profile,
		}

	case *NotificationAction_OpenUrl:
		res.Payload = payload.OpenUrl

	case *NotificationAction_Webhook:
		res.Type = notifications.ActionTypeWebhook
		webhook := notifications.ActionTypeWebhookPayload{
			Method:  payload.Webhook.Method,
			URL:     payload.Webhook.Url,
			Payload: payload.Webhook.Payload,
		}

		switch payload.Webhook.ResultAction {
		case WebhookResultAction_WEBHOOK_RESULT_ACTION_DISPLAY:
			webhook.ResultAction = "display"
		case WebhookResultAction_WEBHOOK_RESULT_ACTION_IGNORE:
			webhook.ResultAction = "ignore"
		default:
			webhook.ResultAction = "ignore"
		}

		res.Payload = webhook

	default:
		res.Type = notifications.ActionTypeNone
		res.Payload = nil

	}

	return res
}

func NotificationStateFromProto(state NotificationState) notifications.State {
	switch state {
	case NotificationState_NOTIFICATION_STATE_ACTIVE:
		return notifications.Active
	case NotificationState_NOTIFICATION_STATE_EXECUTED:
		return notifications.Executed
	case NotificationState_NOTIFICATION_STATE_RESPONDED:
		return notifications.Responded
	case NotificationState_NOTIFICATION_STATE_UNKNOWN:
		fallthrough
	default:
		return ""
	}
}

func NotificationTypeFromProto(nType NotificationType) notifications.Type {
	switch nType {
	case NotificationType_NOTIFICATION_TYPE_ERROR:
		return notifications.Error
	case NotificationType_NOTIFICATION_TYPE_WARNING:
		return notifications.Warning
	case NotificationType_NOTIFICATION_TYPE_PROMPT:
		return notifications.Prompt
	case NotificationType_NOTIFICATION_TYPE_INFO:
		fallthrough
	default:
		return notifications.Info
	}
}
