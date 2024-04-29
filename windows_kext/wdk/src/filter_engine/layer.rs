use core::fmt::Debug;

use windows_sys::{
    core::GUID,
    Win32::NetworkManagement::WindowsFilteringPlatform::{
        FWPM_LAYER_ALE_AUTH_CONNECT_V4, FWPM_LAYER_ALE_AUTH_CONNECT_V4_DISCARD,
        FWPM_LAYER_ALE_AUTH_CONNECT_V6, FWPM_LAYER_ALE_AUTH_CONNECT_V6_DISCARD,
        FWPM_LAYER_ALE_AUTH_LISTEN_V4, FWPM_LAYER_ALE_AUTH_LISTEN_V4_DISCARD,
        FWPM_LAYER_ALE_AUTH_LISTEN_V6, FWPM_LAYER_ALE_AUTH_LISTEN_V6_DISCARD,
        FWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V4, FWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V4_DISCARD,
        FWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V6, FWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V6_DISCARD,
        FWPM_LAYER_ALE_BIND_REDIRECT_V4, FWPM_LAYER_ALE_BIND_REDIRECT_V6,
        FWPM_LAYER_ALE_CONNECT_REDIRECT_V4, FWPM_LAYER_ALE_CONNECT_REDIRECT_V6,
        FWPM_LAYER_ALE_ENDPOINT_CLOSURE_V4, FWPM_LAYER_ALE_ENDPOINT_CLOSURE_V6,
        FWPM_LAYER_ALE_FLOW_ESTABLISHED_V4, FWPM_LAYER_ALE_FLOW_ESTABLISHED_V4_DISCARD,
        FWPM_LAYER_ALE_FLOW_ESTABLISHED_V6, FWPM_LAYER_ALE_FLOW_ESTABLISHED_V6_DISCARD,
        FWPM_LAYER_ALE_RESOURCE_ASSIGNMENT_V4, FWPM_LAYER_ALE_RESOURCE_ASSIGNMENT_V4_DISCARD,
        FWPM_LAYER_ALE_RESOURCE_ASSIGNMENT_V6, FWPM_LAYER_ALE_RESOURCE_ASSIGNMENT_V6_DISCARD,
        FWPM_LAYER_ALE_RESOURCE_RELEASE_V4, FWPM_LAYER_ALE_RESOURCE_RELEASE_V6,
        FWPM_LAYER_DATAGRAM_DATA_V4, FWPM_LAYER_DATAGRAM_DATA_V4_DISCARD,
        FWPM_LAYER_DATAGRAM_DATA_V6, FWPM_LAYER_DATAGRAM_DATA_V6_DISCARD,
        FWPM_LAYER_INBOUND_ICMP_ERROR_V4, FWPM_LAYER_INBOUND_ICMP_ERROR_V4_DISCARD,
        FWPM_LAYER_INBOUND_ICMP_ERROR_V6, FWPM_LAYER_INBOUND_ICMP_ERROR_V6_DISCARD,
        FWPM_LAYER_INBOUND_IPPACKET_V4, FWPM_LAYER_INBOUND_IPPACKET_V4_DISCARD,
        FWPM_LAYER_INBOUND_IPPACKET_V6, FWPM_LAYER_INBOUND_IPPACKET_V6_DISCARD,
        FWPM_LAYER_INBOUND_TRANSPORT_V4, FWPM_LAYER_INBOUND_TRANSPORT_V4_DISCARD,
        FWPM_LAYER_INBOUND_TRANSPORT_V6, FWPM_LAYER_INBOUND_TRANSPORT_V6_DISCARD,
        FWPM_LAYER_IPFORWARD_V4, FWPM_LAYER_IPFORWARD_V4_DISCARD, FWPM_LAYER_IPFORWARD_V6,
        FWPM_LAYER_IPFORWARD_V6_DISCARD, FWPM_LAYER_OUTBOUND_ICMP_ERROR_V4,
        FWPM_LAYER_OUTBOUND_ICMP_ERROR_V4_DISCARD, FWPM_LAYER_OUTBOUND_ICMP_ERROR_V6,
        FWPM_LAYER_OUTBOUND_ICMP_ERROR_V6_DISCARD, FWPM_LAYER_OUTBOUND_IPPACKET_V4,
        FWPM_LAYER_OUTBOUND_IPPACKET_V4_DISCARD, FWPM_LAYER_OUTBOUND_IPPACKET_V6,
        FWPM_LAYER_OUTBOUND_IPPACKET_V6_DISCARD, FWPM_LAYER_OUTBOUND_TRANSPORT_V4,
        FWPM_LAYER_OUTBOUND_TRANSPORT_V4_DISCARD, FWPM_LAYER_OUTBOUND_TRANSPORT_V6,
        FWPM_LAYER_OUTBOUND_TRANSPORT_V6_DISCARD, FWPM_LAYER_STREAM_V4,
        FWPM_LAYER_STREAM_V4_DISCARD, FWPM_LAYER_STREAM_V6, FWPM_LAYER_STREAM_V6_DISCARD,
    },
};

#[repr(C)]
pub(crate) struct Value {
    pub(crate) value_type: ValueType,
    pub(crate) value: ValueData,
}

#[repr(C)]
pub(crate) struct IncomingValues {
    pub(crate) layer_id: u16,
    pub(crate) value_count: u32,
    pub(crate) incoming_value_array: *const Value,
}

#[repr(C)]
pub(crate) union ValueData {
    pub(crate) uint8: u8,
    pub(crate) uint16: u16,
    pub(crate) uint32: u32,
    pub(crate) uint64: *const u64,
    pub(crate) byte_array16: *const [u8; 16],
    // TODO: add the rest of possible values.
}

#[repr(C)]
#[derive(Copy, Clone, Debug)]
pub enum ValueType {
    FwpEmpty = 0,
    FwpUint8 = 1,
    FwpUint16 = 2,
    FwpUint32 = 3,
    FwpUint64 = 4,
    FwpInt8 = 5,
    FwpInt16 = 6,
    FwpInt32 = 7,
    FwpInt64 = 8,
    FwpFloat = 9,
    FwpDouble = 10,
    FwpByteArray16Type = 11,
    FwpByteBlobType = 12,
    FwpSid = 13,
    FwpSecurityDescriptorType = 14,
    FwpTokenInformationType = 15,
    FwpTokenAccessInformationType = 16,
    FwpUnicodeStringType = 17,
    FwpByteArray6Type = 18,
    FwpSingleDataTypeMax = 0xff,
    FwpV4AddrMask = 0xff + 1,
    FwpV6AddrMask = 0xff + 2,
    FwpRangeType = 0xff + 3,
    FwpDataTypeMax = 0xff + 4,
}

#[derive(Copy, Clone, Debug)]
pub enum Layer {
    InboundIppacketV4,
    InboundIppacketV4Discard,
    InboundIppacketV6,
    InboundIppacketV6Discard,
    OutboundIppacketV4,
    OutboundIppacketV4Discard,
    OutboundIppacketV6,
    OutboundIppacketV6Discard,
    IpforwardV4,
    IpforwardV4Discard,
    IpforwardV6,
    IpforwardV6Discard,
    InboundTransportV4,
    InboundTransportV4Discard,
    InboundTransportV6,
    InboundTransportV6Discard,
    OutboundTransportV4,
    OutboundTransportV4Discard,
    OutboundTransportV6,
    OutboundTransportV6Discard,
    StreamV4,
    StreamV4Discard,
    StreamV6,
    StreamV6Discard,
    DatagramDataV4,
    DatagramDataV4Discard,
    DatagramDataV6,
    DatagramDataV6Discard,
    InboundIcmpErrorV4,
    InboundIcmpErrorV4Discard,
    InboundIcmpErrorV6,
    InboundIcmpErrorV6Discard,
    OutboundIcmpErrorV4,
    OutboundIcmpErrorV4Discard,
    OutboundIcmpErrorV6,
    OutboundIcmpErrorV6Discard,
    AleResourceAssignmentV4,
    AleResourceAssignmentV4Discard,
    AleResourceAssignmentV6,
    AleResourceAssignmentV6Discard,
    AleAuthListenV4,
    AleAuthListenV4Discard,
    AleAuthListenV6,
    AleAuthListenV6Discard,
    AleAuthRecvAcceptV4,
    AleAuthRecvAcceptV4Discard,
    AleAuthRecvAcceptV6,
    AleAuthRecvAcceptV6Discard,
    AleAuthConnectV4,
    AleAuthConnectV4Discard,
    AleAuthConnectV6,
    AleAuthConnectV6Discard,
    AleFlowEstablishedV4,
    AleFlowEstablishedV4Discard,
    AleFlowEstablishedV6,
    AleFlowEstablishedV6Discard,
    AleConnectRedirectV4,
    AleConnectRedirectV6,
    AleBindRedirectV4,
    AleBindRedirectV6,
    AleResourceReleaseV4,
    AleResourceReleaseV6,
    AleEndpointClosureV4,
    AleEndpointClosureV6,
}

impl Layer {
    pub fn get_guid(&self) -> GUID {
        match self {
            Layer::InboundIppacketV4 => FWPM_LAYER_INBOUND_IPPACKET_V4,
            Layer::InboundIppacketV4Discard => FWPM_LAYER_INBOUND_IPPACKET_V4_DISCARD,
            Layer::InboundIppacketV6 => FWPM_LAYER_INBOUND_IPPACKET_V6,
            Layer::InboundIppacketV6Discard => FWPM_LAYER_INBOUND_IPPACKET_V6_DISCARD,
            Layer::OutboundIppacketV4 => FWPM_LAYER_OUTBOUND_IPPACKET_V4,
            Layer::OutboundIppacketV4Discard => FWPM_LAYER_OUTBOUND_IPPACKET_V4_DISCARD,
            Layer::OutboundIppacketV6 => FWPM_LAYER_OUTBOUND_IPPACKET_V6,
            Layer::OutboundIppacketV6Discard => FWPM_LAYER_OUTBOUND_IPPACKET_V6_DISCARD,
            Layer::IpforwardV4 => FWPM_LAYER_IPFORWARD_V4,
            Layer::IpforwardV4Discard => FWPM_LAYER_IPFORWARD_V4_DISCARD,
            Layer::IpforwardV6 => FWPM_LAYER_IPFORWARD_V6,
            Layer::IpforwardV6Discard => FWPM_LAYER_IPFORWARD_V6_DISCARD,
            Layer::InboundTransportV4 => FWPM_LAYER_INBOUND_TRANSPORT_V4,
            Layer::InboundTransportV4Discard => FWPM_LAYER_INBOUND_TRANSPORT_V4_DISCARD,
            Layer::InboundTransportV6 => FWPM_LAYER_INBOUND_TRANSPORT_V6,
            Layer::InboundTransportV6Discard => FWPM_LAYER_INBOUND_TRANSPORT_V6_DISCARD,
            Layer::OutboundTransportV4 => FWPM_LAYER_OUTBOUND_TRANSPORT_V4,
            Layer::OutboundTransportV4Discard => FWPM_LAYER_OUTBOUND_TRANSPORT_V4_DISCARD,
            Layer::OutboundTransportV6 => FWPM_LAYER_OUTBOUND_TRANSPORT_V6,
            Layer::OutboundTransportV6Discard => FWPM_LAYER_OUTBOUND_TRANSPORT_V6_DISCARD,
            Layer::StreamV4 => FWPM_LAYER_STREAM_V4,
            Layer::StreamV4Discard => FWPM_LAYER_STREAM_V4_DISCARD,
            Layer::StreamV6 => FWPM_LAYER_STREAM_V6,
            Layer::StreamV6Discard => FWPM_LAYER_STREAM_V6_DISCARD,
            Layer::DatagramDataV4 => FWPM_LAYER_DATAGRAM_DATA_V4,
            Layer::DatagramDataV4Discard => FWPM_LAYER_DATAGRAM_DATA_V4_DISCARD,
            Layer::DatagramDataV6 => FWPM_LAYER_DATAGRAM_DATA_V6,
            Layer::DatagramDataV6Discard => FWPM_LAYER_DATAGRAM_DATA_V6_DISCARD,
            Layer::InboundIcmpErrorV4 => FWPM_LAYER_INBOUND_ICMP_ERROR_V4,
            Layer::InboundIcmpErrorV4Discard => FWPM_LAYER_INBOUND_ICMP_ERROR_V4_DISCARD,
            Layer::InboundIcmpErrorV6 => FWPM_LAYER_INBOUND_ICMP_ERROR_V6,
            Layer::InboundIcmpErrorV6Discard => FWPM_LAYER_INBOUND_ICMP_ERROR_V6_DISCARD,
            Layer::OutboundIcmpErrorV4 => FWPM_LAYER_OUTBOUND_ICMP_ERROR_V4,
            Layer::OutboundIcmpErrorV4Discard => FWPM_LAYER_OUTBOUND_ICMP_ERROR_V4_DISCARD,
            Layer::OutboundIcmpErrorV6 => FWPM_LAYER_OUTBOUND_ICMP_ERROR_V6,
            Layer::OutboundIcmpErrorV6Discard => FWPM_LAYER_OUTBOUND_ICMP_ERROR_V6_DISCARD,
            Layer::AleResourceAssignmentV4 => FWPM_LAYER_ALE_RESOURCE_ASSIGNMENT_V4,
            Layer::AleResourceAssignmentV4Discard => FWPM_LAYER_ALE_RESOURCE_ASSIGNMENT_V4_DISCARD,
            Layer::AleResourceAssignmentV6 => FWPM_LAYER_ALE_RESOURCE_ASSIGNMENT_V6,
            Layer::AleResourceAssignmentV6Discard => FWPM_LAYER_ALE_RESOURCE_ASSIGNMENT_V6_DISCARD,
            Layer::AleAuthListenV4 => FWPM_LAYER_ALE_AUTH_LISTEN_V4,
            Layer::AleAuthListenV4Discard => FWPM_LAYER_ALE_AUTH_LISTEN_V4_DISCARD,
            Layer::AleAuthListenV6 => FWPM_LAYER_ALE_AUTH_LISTEN_V6,
            Layer::AleAuthListenV6Discard => FWPM_LAYER_ALE_AUTH_LISTEN_V6_DISCARD,
            Layer::AleAuthRecvAcceptV4 => FWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V4,
            Layer::AleAuthRecvAcceptV4Discard => FWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V4_DISCARD,
            Layer::AleAuthRecvAcceptV6 => FWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V6,
            Layer::AleAuthRecvAcceptV6Discard => FWPM_LAYER_ALE_AUTH_RECV_ACCEPT_V6_DISCARD,
            Layer::AleAuthConnectV4 => FWPM_LAYER_ALE_AUTH_CONNECT_V4,
            Layer::AleAuthConnectV4Discard => FWPM_LAYER_ALE_AUTH_CONNECT_V4_DISCARD,
            Layer::AleAuthConnectV6 => FWPM_LAYER_ALE_AUTH_CONNECT_V6,
            Layer::AleAuthConnectV6Discard => FWPM_LAYER_ALE_AUTH_CONNECT_V6_DISCARD,
            Layer::AleFlowEstablishedV4 => FWPM_LAYER_ALE_FLOW_ESTABLISHED_V4,
            Layer::AleFlowEstablishedV4Discard => FWPM_LAYER_ALE_FLOW_ESTABLISHED_V4_DISCARD,
            Layer::AleFlowEstablishedV6 => FWPM_LAYER_ALE_FLOW_ESTABLISHED_V6,
            Layer::AleFlowEstablishedV6Discard => FWPM_LAYER_ALE_FLOW_ESTABLISHED_V6_DISCARD,
            Layer::AleConnectRedirectV4 => FWPM_LAYER_ALE_CONNECT_REDIRECT_V4,
            Layer::AleConnectRedirectV6 => FWPM_LAYER_ALE_CONNECT_REDIRECT_V6,
            Layer::AleBindRedirectV4 => FWPM_LAYER_ALE_BIND_REDIRECT_V4,
            Layer::AleBindRedirectV6 => FWPM_LAYER_ALE_BIND_REDIRECT_V6,
            Layer::AleResourceReleaseV4 => FWPM_LAYER_ALE_RESOURCE_RELEASE_V4,
            Layer::AleResourceReleaseV6 => FWPM_LAYER_ALE_RESOURCE_RELEASE_V6,
            Layer::AleEndpointClosureV4 => FWPM_LAYER_ALE_ENDPOINT_CLOSURE_V4,
            Layer::AleEndpointClosureV6 => FWPM_LAYER_ALE_ENDPOINT_CLOSURE_V6,
        }
    }
}

#[repr(usize)]
pub enum FieldsInboundIppacketV4 {
    IpLocalAddress,
    IpRemoteAddress,
    IpLocalAddressType,
    IpLocalInterface,
    InterfaceIndex,
    SubInterfaceIndex,
    Flags,
    InterfaceType,
    TunnelType,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsInboundIppacketV6 {
    IpLocalAddress,
    IpRemoteAddress,
    IpLocalAddressType,
    IpLocalInterface,
    InterfaceIndex,
    SubInterfaceIndex,
    Flags,
    InterfaceType,
    TunnelType,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsOutboundIppacketV4 {
    IpLocalAddress,
    IpLocalAddressType,
    IpRemoteAddress,
    IpLocalInterface,
    InterfaceIndex,
    SubInterfaceIndex,
    Flags,
    InterfaceType,
    TunnelType,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsOutboundIppacketV6 {
    IpLocalAddress,
    IpLocalAddressType,
    IpRemoteAddress,
    IpLocalInterface,
    InterfaceIndex,
    SubInterfaceIndex,
    Flags,
    InterfaceType,
    TunnelType,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsIpforwardV4 {
    IpSourceAddress,
    IpDestinationAddress,
    IpDestinationAddressType,
    IpLocalInterface,
    IpForwardInterface,
    SourceInterfaceIndex,
    SourceSubInterfaceIndex,
    DestinationInterfaceIndex,
    DestinationSubInterfaceIndex,
    Flags,
    IpPhysicalArrivalInterface,
    ArrivalInterfaceProfileId,
    IpPhysicalNexthopInterface,
    NexthopInterfaceProfileId,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsIpforwardV6 {
    IpSourceAddress,
    IpDestinationAddress,
    IpDestinationAddressType,
    IpLocalInterface,
    IpForwardInterface,
    SourceInterfaceIndex,
    SourceSubInterfaceIndex,
    DestinationInterfaceIndex,
    DestinationSubInterfaceIndex,
    Flags,
    IpPhysicalArrivalInterface,
    ArrivalInterfaceProfileId,
    IpPhysicalNexthopInterface,
    NexthopInterfaceProfileId,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsInboundTransportV4 {
    IpProtocol,
    IpLocalAddress,
    IpRemoteAddress,
    IpLocalAddressType,
    IpLocalPort,
    IpRemotePort,
    IpLocalInterface,
    InterfaceIndex,
    SubInterfaceIndex,
    Flags,
    InterfaceType,
    TunnelType,
    ProfileId,
    IpsecSecurityRealmId,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsInboundTransportFas {
    FieldInboundTransportFastMax,
}

#[repr(usize)]
pub enum FieldsOutboundTransportFas {
    FieldOutboundTransportFastMax,
}

#[repr(usize)]
pub enum FieldsInboundTransportV6 {
    IpProtocol,
    IpLocalAddress,
    IpRemoteAddress,
    IpLocalAddressType,
    IpLocalPort,
    IpRemotePort,
    IpLocalInterface,
    InterfaceIndex,
    SubInterfaceIndex,
    Flags,
    InterfaceType,
    TunnelType,
    ProfileId,
    IpsecSecurityRealmId,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsOutboundTransportV4 {
    IpProtocol,
    IpLocalAddress,
    IpLocalAddressType,
    IpRemoteAddress,
    IpLocalPort,
    IpRemotePort,
    IpLocalInterface,
    InterfaceIndex,
    SubInterfaceIndex,
    IpDestinationAddressType,
    Flags,
    InterfaceType,
    TunnelType,
    ProfileId,
    IpsecSecurityRealmId,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsOutboundTransportV6 {
    IpProtocol,
    IpLocalAddress,
    IpLocalAddressType,
    IpRemoteAddress,
    IpLocalPort,
    IpRemotePort,
    IpLocalInterface,
    InterfaceIndex,
    SubInterfaceIndex,
    IpDestinationAddressType,
    Flags,
    InterfaceType,
    TunnelType,
    ProfileId,
    IpsecSecurityRealmId,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsStreamV4 {
    IpLocalAddress,
    IpLocalAddressType,
    IpRemoteAddress,
    IpLocalPort,
    IpRemotePort,
    Direction,
    Flags,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsStreamV6 {
    IpLocalAddress,
    IpLocalAddressType,
    IpRemoteAddress,
    IpLocalPort,
    IpRemotePort,
    Direction,
    Flags,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsDatagramDataV4 {
    IpProtocol,
    IpLocalAddress,
    IpRemoteAddress,
    IpLocalAddressType,
    IpLocalPort,
    IpRemotePort,
    IpLocalInterface,
    InterfaceIndex,
    SubInterfaceIndex,
    Direction,
    Flags,
    InterfaceType,
    TunnelType,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsDatagramDataV6 {
    IpProtocol,
    IpLocalAddress,
    IpRemoteAddress,
    IpLocalAddressType,
    IpLocalPort,
    IpRemotePort,
    IpLocalInterface,
    InterfaceIndex,
    SubInterfaceIndex,
    Direction,
    Flags,
    InterfaceType,
    TunnelType,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsStreamPacketV4 {
    IpLocalAddress,
    IpRemoteAddress,
    IpLocalPort,
    IpRemotePort,
    IpLocalInterface,
    InterfaceIndex,
    SubInterfaceIndex,
    Direction,
    Flags,
    InterfaceType,
    TunnelType,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsStreamPacketV6 {
    IpLocalAddress,
    IpRemoteAddress,
    IpLocalPort,
    IpRemotePort,
    IpLocalInterface,
    InterfaceIndex,
    SubInterfaceIndex,
    Direction,
    Flags,
    InterfaceType,
    TunnelType,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsInboundIcmpErrorV4 {
    EmbeddedProtocol,
    IpLocalAddress,
    IpRemoteAddress,
    EmbeddedRemoteAddress,
    EmbeddedLocalAddressType,
    EmbeddedLocalPort,
    EmbeddedRemotePort,
    IpLocalInterface,
    IcmpType,
    IcmpCode,
    InterfaceIndex,    // of local/delivery interface
    SubInterfaceIndex, // of arrival interface
    InterfaceType,     // of local/delivery interface
    TunnelType,        // of local/delivery interface
    IpArrivalInterface,
    ArrivalInterfaceIndex,
    ArrivalInterfaceType,
    ArrivalTunnelType,
    Flags,
    ArrivalInterfaceProfileId,
    InterfaceQuarantineEpoch,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsInboundIcmpErrorV6 {
    EmbeddedProtocol,
    IpLocalAddress,
    IpRemoteAddress,
    EmbeddedRemoteAddress,
    EmbeddedLocalAddressType,
    EmbeddedLocalPort,
    EmbeddedRemotePort,
    IpLocalInterface,
    IcmpType,
    IcmpCode,
    InterfaceIndex,    // of local/delivery interface
    SubInterfaceIndex, // of arrival interface
    InterfaceType,     // of local/delivery interface
    TunnelType,        // of local/delivery interface
    IpArrivalInterface,
    ArrivalInterfaceIndex,
    ArrivalInterfaceType,
    ArrivalTunnelType,
    Flags,
    ArrivalInterfaceProfileId,
    InterfaceQuarantineEpoch,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsOutboundIcmpErrorV4 {
    IpLocalAddress,
    IpRemoteAddress,
    IpLocalAddressType,
    IpLocalInterface,
    IcmpType,
    IcmpCode,
    InterfaceIndex,
    SubInterfaceIndex,
    InterfaceType,
    TunnelType,
    Flags,
    NexthopInterfaceProfileId,
    InterfaceQuarantineEpoch,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsOutboundIcmpErrorV6 {
    IpLocalAddress,
    IpRemoteAddress,
    IpLocalAddressType,
    IpLocalInterface,
    IpLocalPort,
    IpRemotePort,
    InterfaceIndex,
    SubInterfaceIndex,
    InterfaceType,
    TunnelType,
    Flags,
    NexthopInterfaceProfileId,
    InterfaceQuarantineEpoch,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsAleResourceAssignmentV4 {
    AleAppId,
    AleUserId,
    IpLocalAddress,
    IpLocalAddressType,
    IpLocalPort,
    IpProtocol,
    AlePromiscuousMode,
    IpLocalInterface,
    Flags,
    InterfaceType,
    TunnelType,
    LocalInterfaceProfileId,
    SioFirewallSocketProperty,
    AlePackageId,
    AleSecurityAttributeFqbnValue,
    CompartmentId,
    //
    // These reserved fields MUST be in this order. DO NOT change their order
    //
    Reserved0,
    Reserved1,
    Max,
}

#[repr(usize)]
pub enum FieldsAleResourceAssignmentV6 {
    AleAppId,
    AleUserId,
    IpLocalAddress,
    IpLocalAddressType,
    IpLocalPort,
    IpProtocol,
    AlePromiscuousMode,
    IpLocalInterface,
    Flags,
    InterfaceType,
    TunnelType,
    LocalInterfaceProfileId,
    SioFirewallSocketProperty,
    AlePackageId,
    AleSecurityAttributeFqbnValue,
    CompartmentId,
    //
    // These reserved fields MUST be in this order. DO NOT change their order
    //
    Reserved0,
    Reserved1,
    Max,
}

#[repr(usize)]
pub enum FieldsAleResourceReleaseV4 {
    AleAppId,
    AleUserId,
    IpLocalAddress,
    IpLocalAddressType,
    IpLocalPort,
    IpProtocol,
    IpLocalInterface,
    Flags,
    AlePackageId,
    AleSecurityAttributeFqbnValue,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsAleResourceReleaseV6 {
    AleAppId,
    AleUserId,
    IpLocalAddress,
    IpLocalAddressType,
    IpLocalPort,
    IpProtocol,
    IpLocalInterface,
    Flags,
    AlePackageId,
    AleSecurityAttributeFqbnValue,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsAleEndpointClosureV4 {
    AleAppId,
    AleUserId,
    IpLocalAddress,
    IpLocalAddressType,
    IpLocalPort,
    IpProtocol,
    IpRemoteAddress,
    IpRemotePort,
    IpLocalInterface,
    Flags,
    AlePackageId,
    AleSecurityAttributeFqbnValue,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsAleEndpointClosureV6 {
    AleAppId,
    AleUserId,
    IpLocalAddress,
    IpLocalAddressType,
    IpLocalPort,
    IpProtocol,
    IpRemoteAddress,
    IpRemotePort,
    IpLocalInterface,
    Flags,
    AlePackageId,
    AleSecurityAttributeFqbnValue,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsAleAuthListenV4 {
    AleAppId,
    AleUserId,
    IpLocalAddress,
    IpLocalAddressType,
    IpLocalPort,
    IpLocalInterface,
    Flags,
    InterfaceType,
    TunnelType,
    LocalInterfaceProfileId,
    SioFirewallSocketProperty,
    AlePackageId,
    AleSecurityAttributeFqbnValue,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsAleAuthListenV6 {
    AleAppId,
    AleUserId,
    IpLocalAddress,
    IpLocalAddressType,
    IpLocalPort,
    IpLocalInterface,
    Flags,
    InterfaceType,
    TunnelType,
    LocalInterfaceProfileId,
    SioFirewallSocketProperty,
    AlePackageId,
    AleSecurityAttributeFqbnValue,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsAleAuthRecvAcceptV4 {
    AleAppId,
    AleUserId,
    IpLocalAddress,
    IpLocalAddressType,
    IpLocalPort,
    IpProtocol,
    IpRemoteAddress,
    IpRemotePort,
    AleRemoteUserId,
    AleRemoteMachineId,
    IpLocalInterface,
    Flags,
    SioFirewallSystemPort,
    NapContext,
    InterfaceType,     // of local/delivery interface
    TunnelType,        // of local/delivery interface
    InterfaceIndex,    // of local/delivery interface
    SubInterfaceIndex, // of arrival interface
    IpArrivalInterface,
    ArrivalInterfaceType,
    ArrivalTunnelType,
    ArrivalInterfaceIndex,
    NexthopSubInterfaceIndex,
    IpNexthopInterface,
    NexthopInterfaceType,
    NexthopTunnelType,
    NexthopInterfaceIndex,
    OriginalProfileId,
    CurrentProfileId,
    ReauthorizeReason,
    OriginalIcmpType,
    InterfaceQuarantineEpoch,
    AlePackageId,
    AleSecurityAttributeFqbnValue,
    CompartmentId,
    //
    // These reserved fields MUST be in this order. DO NOT change their order
    //
    Reserved0,
    Reserved1,
    Reserved2,
    Reserved3,
    Max,
}

#[repr(usize)]
pub enum FieldsAleAuthRecvAcceptV6 {
    AleAppId,
    AleUserId,
    IpLocalAddress,
    IpLocalAddressType,
    IpLocalPort,
    IpProtocol,
    IpRemoteAddress,
    IpRemotePort,
    AleRemoteUserId,
    AleRemoteMachineId,
    IpLocalInterface,
    Flags,
    SioFirewallSystemPort,
    NapContext,
    InterfaceType,     // of local/delivery interface
    TunnelType,        // of local/delivery interface
    InterfaceIndex,    // of local/delivery interface
    SubInterfaceIndex, // of arrival interface
    IpArrivalInterface,
    ArrivalInterfaceType,
    ArrivalTunnelType,
    ArrivalInterfaceIndex,
    NexthopSubInterfaceIndex,
    IpNexthopInterface,
    NexthopInterfaceType,
    NexthopTunnelType,
    NexthopInterfaceIndex,
    OriginalProfileId,
    CurrentProfileId,
    ReauthorizeReason,
    OriginalIcmpType,
    InterfaceQuarantineEpoch,
    AlePackageId,
    AleSecurityAttributeFqbnValue,
    CompartmentId,
    //
    // These reserved fields MUST be in this order. DO NOT change their order
    //
    Reserved0,
    Reserved1,
    Reserved2,
    Reserved3,
    Max,
}

#[repr(usize)]
pub enum FieldsAleBindRedirectV4 {
    AleAppId,
    AleUserId,
    IpLocalAddress,
    IpLocalAddressType,
    IpLocalPort,
    IpProtocol,
    Flags,
    AlePackageId,
    AleSecurityAttributeFqbnValue,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsAleBindRedirectV6 {
    AleAppId,
    AleUserId,
    IpLocalAddress,
    IpLocalAddressType,
    IpLocalPort,
    IpProtocol,
    Flags,
    AlePackageId,
    AleSecurityAttributeFqbnValue,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsAleConnectRedirectV4 {
    AleAppId,
    AleUserId,
    IpLocalAddress,
    IpLocalAddressType,
    IpLocalPort,
    IpProtocol,
    IpRemoteAddress,
    IpDestinationAddressType,
    IpRemotePort,
    Flags,
    AleOriginalAppId,
    AlePackageId,
    AleSecurityAttributeFqbnValue,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsAleConnectRedirectV6 {
    AleAppId,
    AleUserId,
    IpLocalAddress,
    IpLocalAddressType,
    IpLocalPort,
    IpProtocol,
    IpRemoteAddress,
    IpDestinationAddressType,
    IpRemotePort,
    Flags,
    AleOriginalAppId,
    AlePackageId,
    AleSecurityAttributeFqbnValue,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsAleAuthConnectV4 {
    AleAppId,
    AleUserId,
    IpLocalAddress,
    IpLocalAddressType,
    IpLocalPort,
    IpProtocol,
    IpRemoteAddress,
    IpRemotePort,
    AleRemoteUserId,
    AleRemoteMachineId,
    IpDestinationAddressType,
    IpLocalInterface,
    Flags,
    InterfaceType,
    TunnelType,
    InterfaceIndex,
    SubInterfaceIndex,
    IpArrivalInterface,
    ArrivalInterfaceType,
    ArrivalTunnelType,
    ArrivalInterfaceIndex,
    NexthopSubInterfaceIndex,
    IpNexthopInterface,
    NexthopInterfaceType,
    NexthopTunnelType,
    NexthopInterfaceIndex,
    OriginalProfileId,
    CurrentProfileId,
    ReauthorizeReason,
    PeerName,
    OriginalIcmpType,
    InterfaceQuarantineEpoch,
    AleOriginalAppId,
    AlePackageId,
    AleSecurityAttributeFqbnValue,
    AleEffectiveName,
    CompartmentId,
    //
    // These reserved fields MUST be in this order. DO NOT change their order
    //
    Reserved0,
    Reserved1,
    Reserved2,
    Reserved3,
    Max,
}

#[repr(usize)]
pub enum FieldsAleAuthConnectV6 {
    AleAppId,
    AleUserId,
    IpLocalAddress,
    IpLocalAddressType,
    IpLocalPort,
    IpProtocol,
    IpRemoteAddress,
    IpRemotePort,
    AleRemoteUserId,
    AleRemoteMachineId,
    IpDestinationAddressType,
    IpLocalInterface,
    Flags,
    InterfaceType,
    TunnelType,
    InterfaceIndex,
    SubInterfaceIndex,
    IpArrivalInterface,
    ArrivalInterfaceType,
    ArrivalTunnelType,
    ArrivalInterfaceIndex,
    NexthopSubInterfaceIndex,
    IpNexthopInterface,
    NexthopInterfaceType,
    NexthopTunnelType,
    NexthopInterfaceIndex,
    OriginalProfileId,
    CurrentProfileId,
    ReauthorizeReason,
    PeerName,
    OriginalIcmpType,
    InterfaceQuarantineEpoch,
    AleOriginalAppId,
    AlePackageId,
    AleSecurityAttributeFqbnValue,
    AleEffectiveName,
    CompartmentId,
    //
    // These reserved fields MUST be in this order. DO NOT change their order
    //
    Reserved0,
    Reserved1,
    Reserved2,
    Reserved3,
    Max,
}

#[repr(usize)]
pub enum FieldsAleFlowEstablishedV4 {
    AleAppId,
    AleUserId,
    IpLocalAddress,
    IpLocalAddressType,
    IpLocalPort,
    IpProtocol,
    IpRemoteAddress,
    IpRemotePort,
    AleRemoteUserId,
    AleRemoteMachineId,
    IpDestinationAddressType,
    IpLocalInterface,
    Direction,
    InterfaceType,
    TunnelType,
    Flags,
    AleOriginalAppId,
    AlePackageId,
    AleSecurityAttributeFqbnValue,
    CompartmentId,
    //
    // These reserved fields MUST be in this order. DO NOT change their order
    //
    Reserved0,
    Reserved1,
    Reserved2,
    Reserved3,
    Max,
}

#[repr(usize)]
pub enum FieldsAleFlowEstablishedV6 {
    AleAppId,
    AleUserId,
    IpLocalAddress,
    IpLocalAddressType,
    IpLocalPort,
    IpProtocol,
    IpRemoteAddress,
    IpRemotePort,
    AleRemoteUserId,
    AleRemoteMachineId,
    IpDestinationAddressType,
    IpLocalInterface,
    Direction,
    InterfaceType,
    TunnelType,
    Flags,
    AleOriginalAppId,
    AlePackageId,
    AleSecurityAttributeFqbnValue,
    CompartmentId,
    //
    // These reserved fields MUST be in this order. DO NOT change their order
    //
    Reserved0,
    Reserved1,
    Reserved2,
    Reserved3,
    Max,
}

#[repr(usize)]
pub enum FieldsNameResolutionCacheV4 {
    AleUserId,
    AleAppId,
    IpRemoteAddress,
    PeerName,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsNameResolutionCacheV6 {
    AleUserId,
    AleAppId,
    IpRemoteAddress,
    PeerName,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsInboundMacFrameEthernet {
    InterfaceMacAddress,
    MacLocalAddress,
    MacRemoteAddress,
    MacLocalAddressType,
    MacRemoteAddressType,
    EtherType,
    VlanId,
    Interface,
    InterfaceIndex,
    NdisPort,
    L2Flags,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsOutboundMacFrameEthernet {
    InterfaceMacAddress,
    MacLocalAddress,
    MacRemoteAddress,
    MacLocalAddressType,
    MacRemoteAddressType,
    EtherType,
    VlanId,
    Interface,
    InterfaceIndex,
    NdisPort,
    L2Flags,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsInboundMacFrameNative {
    NdisMediaType,
    NdisPhysicalMediaType,
    Interface,
    InterfaceType,
    InterfaceIndex,
    NdisPort,
    L2Flags,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsInboundMacFrameNativeFast {
    FastMax,
}

#[repr(usize)]
pub enum FieldsOutboundMacFrameNative {
    NdisMediaType,
    NdisPhysicalMediaType,
    Interface,
    InterfaceType,
    InterfaceIndex,
    NdisPort,
    L2Flags,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsOutboundMacFrameNativeFast {
    Max,
}

#[repr(usize)]
pub enum FieldsIngressVswitchEthernet {
    MacSourceAddress,
    MacSourceAddressType,
    MacDestinationAddress,
    MacDestinationAddressType,
    EtherType,
    VlanId,
    VswitchTenantNetworkId,
    VswitchId,
    VswitchNetworkType,
    VswitchSourceInterfaceId,
    VswitchSourceInterfaceType,
    VswitchSourceVmId,
    L2Flags,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsEgressVswitchEthernet {
    MacSourceAddress,
    MacSourceAddressType,
    MacDestinationAddress,
    MacDestinationAddressType,
    EtherType,
    VlanId,
    VswitchTenantNetworkId,
    VswitchId,
    VswitchNetworkType,
    VswitchSourceInterfaceId,
    VswitchSourceInterfaceType,
    VswitchSourceVmId,
    VswitchDestinationInterfaceId,
    VswitchDestinationInterfaceType,
    VswitchDestinationVmId,
    L2Flags,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsIngressVswitchTransportV4 {
    IpSourceAddress,
    IpDestinationAddress,
    IpProtocol,
    IpSourcePort,
    IpDestinationPort,
    VlanId,
    VswitchTenantNetworkId,
    VswitchId,
    VswitchNetworkType,
    VswitchSourceInterfaceId,
    VswitchSourceInterfaceType,
    VswitchSourceVmId,
    L2Flags,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsIngressVswitchTransportV6 {
    IpSourceAddress,
    IpDestinationAddress,
    IpProtocol,
    IpSourcePort,
    IpDestinationPort,
    VlanId,
    VswitchTenantNetworkId,
    VswitchId,
    VswitchNetworkType,
    VswitchSourceInterfaceId,
    VswitchSourceInterfaceType,
    VswitchSourceVmId,
    L2Flags,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsEgressVswitchTransportV4 {
    IpSourceAddress,
    IpDestinationAddress,
    IpProtocol,
    IpSourcePort,
    IpDestinationPort,
    VlanId,
    VswitchTenantNetworkId,
    VswitchId,
    VswitchNetworkType,
    VswitchSourceInterfaceId,
    VswitchSourceInterfaceType,
    VswitchSourceVmId,
    VswitchDestinationInterfaceId,
    VswitchDestinationInterfaceType,
    VswitchDestinationVmId,
    L2Flags,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsEgressVswitchTransportV6 {
    IpSourceAddress,
    IpDestinationAddress,
    IpProtocol,
    IpSourcePort,
    IpDestinationPort,
    VlanId,
    VswitchTenantNetworkId,
    VswitchId,
    VswitchNetworkType,
    VswitchSourceInterfaceId,
    VswitchSourceInterfaceType,
    VswitchSourceVmId,
    VswitchDestinationInterfaceId,
    VswitchDestinationInterfaceType,
    VswitchDestinationVmId,
    L2Flags,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsIpsecKmDemuxV4 {
    IpLocalAddress,
    IpRemoteAddress,
    QmMode,
    IpLocalInterface,
    CurrentProfileId,
    IpsecSecurityRealmId,
    Max,
}

#[repr(usize)]
pub enum FieldsIpsecKmDemuxV6 {
    IpLocalAddress,
    IpRemoteAddress,
    QmMode,
    IpLocalInterface,
    CurrentProfileId,
    IpsecSecurityRealmId,
    Max,
}

#[repr(usize)]
pub enum FieldsIpsecV4 {
    IpProtocol,
    IpLocalAddress,
    IpRemoteAddress,
    IpLocalPort,
    IpRemotePort,
    IpLocalInterface,
    ProfileId,
    IpsecSecurityRealmId,
    Max,
}

#[repr(usize)]
pub enum FieldsIpsecV6 {
    IpProtocol,
    IpLocalAddress,
    IpRemoteAddress,
    IpLocalPort,
    IpRemotePort,
    IpLocalInterface,
    ProfileId,
    IpsecSecurityRealmId,
    Max,
}

#[repr(usize)]
pub enum FieldsIkeextV4 {
    IpLocalAddress,
    IpRemoteAddress,
    IpLocalInterface,
    ProfileId,
    IpsecSecurityRealmId,
    Max,
}

#[repr(usize)]
pub enum FieldsIkeextV6 {
    IpLocalAddress,
    IpRemoteAddress,
    IpLocalInterface,
    ProfileId,
    IpsecSecurityRealmId,
    Max,
}

#[repr(usize)]
pub enum FieldsRpcUm {
    RemoteUserToken,
    AuthLevel,
    AuthType,
    DcomAppId,
    IfFlag,
    IfUuid,
    IfVersion,
    ImageName,
    LocalAddrV4,
    LocalAddrV6,
    LocalPort,
    Max,
    Pipe,
    Protocol,
    RemoteAddrV4,
    RemoteAddrV6,
    SecEncryptAlgorithm,
    SecKeySize,
}

#[repr(usize)]
pub enum FieldsRpcEpmap {
    RemoteUserToken,
    IfUuid,
    IfVersion,
    Protocol,
    AuthType,
    AuthLevel,
    SecEncryptAlgorithm,
    SecKeySize,
    LocalAddrV4,
    LocalAddrV6,
    LocalPort,
    Pipe,
    RemoteAddrV4,
    RemoteAddrV6,
    Max,
}

#[repr(usize)]
pub enum FieldsRpcEpAdd {
    ProcessWithRpcIfUuid,
    Protocol,
    EpValue,
    EpFlags,
    Max,
}

#[repr(usize)]
pub enum FieldsRpcProxyConn {
    ClientToken,
    ServerName,
    ServerPort,
    ProxyAuthType,
    ClientCertKeyLength,
    ClientCertOid,
    Max,
}

#[repr(usize)]
pub enum FieldsRpcProxyIf {
    ClientToken,
    IfUuid,
    IfVersion,
    ServerName,
    ServerPort,
    ProxyAuthType,
    ClientCertKeyLength,
    ClientCertOid,
    Max,
}

#[repr(usize)]
pub enum FieldsKmAuthorization {
    RemoteId,
    AuthenticationType,
    KmType,
    Direction,
    KmMode,
    IpsecPolicyKey,
    NapContext,
    Max,
}

#[repr(usize)]
pub enum FieldsInboundReserved2 {
    Reserved0,
    Reserved1,
    Reserved2,
    Reserved3,
    Reserved4,
    Reserved5,
    Reserved6,
    Reserved7,
    Reserved8,
    Reserved9,
    Reserved10,
    Reserved11,
    Reserved12,
    Reserved13,
    Reserved14,
    Reserved15,
    Max,
}

#[repr(usize)]
pub enum FieldsOutboundNetworkConnectionPolicyV4 {
    AleAppId,
    AleUserId,
    IpLocalAddress,
    IpLocalAddressType,
    IpLocalPort,
    IpProtocol,
    IpRemoteAddress,
    IpDestinationAddressType,
    IpRemotePort,
    Flags,
    AleOriginalAppId,
    AlePackageId,
    AleSecurityAttributeFqbnValue,
    CompartmentId,
    Max,
}

#[repr(usize)]
pub enum FieldsOutboundNetworkConnectionPolicyV6 {
    AleAppId,
    AleUserId,
    IpLocalAddress,
    IpLocalAddressType,
    IpLocalPort,
    IpProtocol,
    IpRemoteAddress,
    IpDestinationAddressType,
    IpRemotePort,
    Flags,
    AleOriginalAppId,
    AlePackageId,
    AleSecurityAttributeFqbnValue,
    CompartmentId,
    Max,
}
