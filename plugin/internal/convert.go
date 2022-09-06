package internal

import (
	"fmt"

	"github.com/miekg/dns"
	"github.com/safing/portbase/config"
	"github.com/safing/portbase/notifications"
	"github.com/safing/portmaster/intel"
	"github.com/safing/portmaster/intel/geoip"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/netutils"
	"github.com/safing/portmaster/network/packet"
	"github.com/safing/portmaster/plugin/shared/proto"
)

func VerdictToNetwork(verdict proto.Verdict) network.Verdict {
	switch verdict {
	case proto.Verdict_VERDICT_ACCEPT:
		return network.VerdictAccept
	case proto.Verdict_VERDICT_BLOCK:
		return network.VerdictBlock
	case proto.Verdict_VERDICT_DROP:
		return network.VerdictDrop
	case proto.Verdict_VERDICT_FAILED:
		return network.VerdictFailed
	case proto.Verdict_VERDICT_REROUTE_TO_NS:
		return network.VerdictRerouteToNameserver
	case proto.Verdict_VERDICT_REROUTE_TO_TUNNEL:
		return network.VerdictRerouteToTunnel
	case proto.Verdict_VERDICT_UNDECIDED:
		return network.VerdictUndecided
	case proto.Verdict_VERDICT_UNDETERMINABLE:
		return network.VerdictUndeterminable

	default:
		return network.VerdictUndecided
	}
}

func ConnectionFromNetwork(conn *network.Connection) *proto.Connection {
	if conn == nil {
		return nil
	}

	protoConn := &proto.Connection{
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

func VerdictFromNetwork(verdict network.Verdict) proto.Verdict {
	switch verdict {
	case network.VerdictAccept:
		return proto.Verdict_VERDICT_ACCEPT
	case network.VerdictBlock:
		return proto.Verdict_VERDICT_BLOCK
	case network.VerdictDrop:
		return proto.Verdict_VERDICT_DROP
	case network.VerdictFailed:
		return proto.Verdict_VERDICT_FAILED
	case network.VerdictRerouteToNameserver:
		return proto.Verdict_VERDICT_REROUTE_TO_NS
	case network.VerdictRerouteToTunnel:
		return proto.Verdict_VERDICT_REROUTE_TO_TUNNEL
	case network.VerdictUndeterminable:
		return proto.Verdict_VERDICT_UNDETERMINABLE
	case network.VerdictUndecided:
		fallthrough
	default:
		return proto.Verdict_VERDICT_UNDECIDED
	}
}

func ConnTypeFromNetwork(connType network.ConnectionType) proto.ConnectionType {
	switch connType {
	case network.IPConnection:
		return proto.ConnectionType_CONNECTION_TYPE_IP

	case network.DNSRequest:
		return proto.ConnectionType_CONNECTION_TYPE_DNS

	default:
		return proto.ConnectionType_CONNECTION_TYPE_UNKNOWN
	}
}

func IPVersionFromNetwork(ipVersion packet.IPVersion) proto.IPVersion {
	switch ipVersion {
	case packet.IPv4:
		return proto.IPVersion_IP_VERSION_4
	case packet.IPv6:
		return proto.IPVersion_IP_VERSION_6
	default:
		return proto.IPVersion_IP_VERSION_UNKNOWN
	}
}

func IntelEntityFromNetwork(entity *intel.Entity) *proto.IntelEntity {
	if entity == nil {
		return nil
	}

	return &proto.IntelEntity{
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

func IPScopeFromNetwork(scope netutils.IPScope) proto.IPScope {
	switch scope {
	case netutils.Undefined:
		return proto.IPScope_IP_SCOPE_UNKNOWN
	case netutils.HostLocal:
		return proto.IPScope_IP_SCOPE_HOST_LOCAL
	case netutils.LinkLocal:
		return proto.IPScope_IP_SCOPE_LINK_LOCAL
	case netutils.SiteLocal:
		return proto.IPScope_IP_SCOPE_SITE_LOCAL
	case netutils.Global:
		return proto.IPScope_IP_SCOPE_GLOBAL
	case netutils.GlobalMulticast:
		return proto.IPScope_IP_SCOPE_GLOBAL_MULTICAST
	case netutils.LocalMulticast:
		return proto.IPScope_IP_SCOPE_LOCAL_MULTICAST

	default:
		return proto.IPScope_IP_SCOPE_UNKNOWN
	}
}

func ProcessFromNetwork(process network.ProcessContext) *proto.ProcessContext {
	return &proto.ProcessContext{
		Name:        process.ProfileName,
		Profile:     process.Profile,
		BinaryPath:  process.BinaryPath,
		CommandLine: process.CmdLine,
		ProcessId:   int64(process.PID),
		Source:      process.Source,
	}
}

func CoordinatesFromNetwork(coord *geoip.Coordinates) *proto.Coordinates {
	if coord == nil {
		return nil
	}

	return &proto.Coordinates{
		AccuracyRadius: int32(coord.AccuracyRadius),
		Latitude:       float32(coord.Latitude),
		Longitude:      float32(coord.Longitude),
	}
}

func OptionTypeToConfig(optType proto.OptionType) config.OptionType {
	switch optType {
	case proto.OptionType_OPTION_TYPE_BOOL:
		return config.OptTypeBool
	case proto.OptionType_OPTION_TYPE_INT:
		return config.OptTypeInt
	case proto.OptionType_OPTION_TYPE_STRING:
		return config.OptTypeString
	case proto.OptionType_OPTION_TYPE_STRING_ARRAY:
		return config.OptTypeStringArray
	case proto.OptionType_OPTION_TYPE_ANY:
		fallthrough
	default:
		return config.OptionType(0)
	}
}

func UnwrapConfigValue(value *proto.Value, valueType config.OptionType) (any, error) {
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

func WrapConfigValue(x interface{}, valueType config.OptionType) (*proto.Value, error) {
	if x == nil {
		return &proto.Value{}, nil
	}

	switch valueType {
	case config.OptTypeBool:
		return &proto.Value{
			Bool: x.(bool),
		}, nil
	case config.OptTypeInt:
		return &proto.Value{
			Int: int64(x.(int)),
		}, nil
	case config.OptTypeString:
		return &proto.Value{
			String_: x.(string),
		}, nil
	case config.OptTypeStringArray:
		return &proto.Value{
			StringArray: x.([]string),
		}, nil
	}

	return nil, fmt.Errorf("unsupported option type")
}

func NotificationFromProto(notif *proto.Notification) *notifications.Notification {
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

func NotificationActionTypeFromProto(action *proto.NotificationAction) *notifications.Action {
	res := &notifications.Action{
		ID:   action.GetId(),
		Text: action.GetText(),
	}

	switch payload := action.GetActionType().(type) {
	case *proto.NotificationAction_InjectEventId:
		res.Payload = payload.InjectEventId

	case *proto.NotificationAction_OpenPage:
		res.Payload = payload.OpenPage

	case *proto.NotificationAction_OpenProfile:
		res.Payload = payload.OpenProfile

	case *proto.NotificationAction_OpenSetting:
		res.Payload = notifications.ActionTypeOpenSettingPayload{
			Key:     payload.OpenSetting.Key,
			Profile: payload.OpenSetting.Profile,
		}

	case *proto.NotificationAction_OpenUrl:
		res.Payload = payload.OpenUrl

	case *proto.NotificationAction_Webhook:
		res.Type = notifications.ActionTypeWebhook
		webhook := notifications.ActionTypeWebhookPayload{
			Method:  payload.Webhook.Method,
			URL:     payload.Webhook.Url,
			Payload: payload.Webhook.Payload,
		}

		switch payload.Webhook.ResultAction {
		case proto.WebhookResultAction_WEBHOOK_RESULT_ACTION_DISPLAY:
			webhook.ResultAction = "display"
		case proto.WebhookResultAction_WEBHOOK_RESULT_ACTION_IGNORE:
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

func NotificationStateFromProto(state proto.NotificationState) notifications.State {
	switch state {
	case proto.NotificationState_NOTIFICATION_STATE_ACTIVE:
		return notifications.Active
	case proto.NotificationState_NOTIFICATION_STATE_EXECUTED:
		return notifications.Executed
	case proto.NotificationState_NOTIFICATION_STATE_RESPONDED:
		return notifications.Responded
	case proto.NotificationState_NOTIFICATION_STATE_UNKNOWN:
		fallthrough
	default:
		return ""
	}
}

func NotificationTypeFromProto(nType proto.NotificationType) notifications.Type {
	switch nType {
	case proto.NotificationType_NOTIFICATION_TYPE_ERROR:
		return notifications.Error
	case proto.NotificationType_NOTIFICATION_TYPE_WARNING:
		return notifications.Warning
	case proto.NotificationType_NOTIFICATION_TYPE_PROMPT:
		return notifications.Prompt
	case proto.NotificationType_NOTIFICATION_TYPE_INFO:
		fallthrough
	default:
		return notifications.Info
	}
}

func DNSQuestionToProto(msg dns.Question) *proto.DNSQuestion {
	return &proto.DNSQuestion{
		Name:  msg.Name,
		Type:  uint32(msg.Qtype),
		Class: uint32(msg.Qclass),
	}
}

func DNSRRFromProto(rr *proto.DNSRR) (dns.RR, error) {
	hdr := dns.RR_Header{
		Name:   rr.GetName(),
		Rrtype: uint16(rr.GetType()),
		Class:  uint16(rr.GetClass()),
		Ttl:    rr.GetTtl(),
	}

	switch uint16(rr.Type) {
	case dns.TypeA:
		return &dns.A{
			Hdr: hdr,
			A:   rr.Data,
		}, nil
	case dns.TypeAAAA:
		return &dns.AAAA{
			Hdr:  hdr,
			AAAA: rr.Data,
		}, nil
	case dns.TypeANY:
		return &dns.ANY{
			Hdr: hdr,
		}, nil
	case dns.TypeTXT:
		return &dns.TXT{
			Hdr: hdr,
			Txt: []string{string(rr.Data)},
		}, nil
	case dns.TypeCNAME:
		return &dns.CNAME{
			Hdr:    hdr,
			Target: string(rr.Data),
		}, nil
	}

	return nil, fmt.Errorf("unsupported DNS resource record %d", rr.Type)
}
