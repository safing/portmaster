use alloc::vec::Vec;
use wdk::filter_engine::callout::FilterType;
use wdk::{
    consts,
    filter_engine::{callout::Callout, layer::Layer},
};

use crate::{ale_callouts, packet_callouts, stream_callouts};

pub fn get_callout_vec() -> Vec<Callout> {
    alloc::vec![
        // -----------------------------------------
        // ALE Auth layers
        Callout::new(
            "AleLayerOutboundV4",
            "ALE layer for outbound connection for ipv4",
            0x58545073_f893_454c_bbea_a57bc964f46d,
            Layer::AleAuthConnectV4,
            consts::FWP_ACTION_CALLOUT_TERMINATING,
            FilterType::Resettable,
            ale_callouts::ale_layer_connect_v4,
        ),
        Callout::new(
            "AleLayerInboundV4",
            "ALE layer for inbound connections for ipv4",
            0xc6021395_0724_4e2c_ae20_3dde51fc3c68,
            Layer::AleAuthRecvAcceptV4,
            consts::FWP_ACTION_CALLOUT_TERMINATING,
            FilterType::Resettable,
            ale_callouts::ale_layer_accept_v4,
        ),
        Callout::new(
            "AleLayerOutboundV6",
            "ALE layer for outbound connections for ipv6",
            0x4bd2a080_2585_478d_977c_7f340c6bc3d4,
            Layer::AleAuthConnectV6,
            consts::FWP_ACTION_CALLOUT_TERMINATING,
            FilterType::Resettable,
            ale_callouts::ale_layer_connect_v6,
        ),
        Callout::new(
            "AleLayerInboundV6",
            "ALE layer for inbound connections for ipv6",
            0xd24480da_38fa_4099_9383_b5c83b69e4f2,
            Layer::AleAuthRecvAcceptV6,
            consts::FWP_ACTION_CALLOUT_TERMINATING,
            FilterType::Resettable,
            ale_callouts::ale_layer_accept_v6,
        ),
        // -----------------------------------------
        // ALE connection end layers
        Callout::new(
            "AleEndpointClosureV4",
            "ALE layer for indicating closing of connection for ipv4",
            0x58f02845_ace9_4455_ac80_8a84b86fe566,
            Layer::AleEndpointClosureV4,
            consts::FWP_ACTION_CALLOUT_INSPECTION,
            FilterType::NonResettable,
            ale_callouts::endpoint_closure_v4,
        ),
        Callout::new(
            "AleEndpointClosureV6",
            "ALE layer for indicating closing of connection for ipv6",
            0x2bc82359_9dc5_4315_9c93_c89467e283ce,
            Layer::AleEndpointClosureV6,
            consts::FWP_ACTION_CALLOUT_INSPECTION,
            FilterType::NonResettable,
            ale_callouts::endpoint_closure_v6,
        ),
        // -----------------------------------------
        // ALE resource assignment and release.
        // Callout::new(
        //     "AleResourceAssignmentV4",
        //     "Ipv4 Port assignment monitoring",
        //     0x6b9d1985_6f75_4d05_b9b5_1607e187906f,
        //     Layer::AleResourceAssignmentV4Discard,
        //     consts::FWP_ACTION_CALLOUT_INSPECTION,
        //     FilterType::NonResettable,
        //     ale_callouts::ale_resource_monitor,
        // ),
        Callout::new(
            "AleResourceReleaseV4",
            "Ipv4 Port release monitor",
            0x7b513bb3_a0be_4f77_a4bc_03c052abe8d7,
            Layer::AleResourceReleaseV4,
            consts::FWP_ACTION_CALLOUT_INSPECTION,
            FilterType::NonResettable,
            ale_callouts::ale_resource_monitor,
        ),
        // Callout::new(
        //     "AleResourceAssignmentV6",
        //     "Ipv4 Port assignment monitor",
        //     0xb0d02299_3d3e_437d_916a_f0e96a60cc18,
        //     Layer::AleResourceAssignmentV6Discard,
        //     consts::FWP_ACTION_CALLOUT_INSPECTION,
        //     FilterType::NonResettable,
        //     ale_callouts::ale_resource_monitor,
        // ),
        Callout::new(
            "AleResourceReleaseV6",
            "Ipv6 Port release monitor",
            0x6cf36e04_e656_42c3_8cac_a1ce05328bd1,
            Layer::AleResourceReleaseV6,
            consts::FWP_ACTION_CALLOUT_INSPECTION,
            FilterType::NonResettable,
            ale_callouts::ale_resource_monitor,
        ),
        // -----------------------------------------
        // Stream layer
        Callout::new(
            "StreamLayerV4",
            "Stream layer for ipv4",
            0xe2ca13bf_9710_4caa_a45c_e8c78b5ac780,
            Layer::StreamV4,
            consts::FWP_ACTION_CALLOUT_INSPECTION,
            FilterType::NonResettable,
            stream_callouts::stream_layer_tcp_v4,
        ),
        Callout::new(
            "StreamLayerV6",
            "Stream layer for ipv6",
            0x66c549b3_11e2_4b27_8f73_856e6fd82baa,
            Layer::StreamV6,
            consts::FWP_ACTION_CALLOUT_INSPECTION,
            FilterType::NonResettable,
            stream_callouts::stream_layer_tcp_v6,
        ),
        Callout::new(
            "DatagramDataLayerV4",
            "DatagramData layer for ipv4",
            0xe7eeeaba_168a_45bb_8747_e1a702feb2c5,
            Layer::DatagramDataV4,
            consts::FWP_ACTION_CALLOUT_INSPECTION,
            FilterType::NonResettable,
            stream_callouts::stream_layer_udp_v4,
        ),
        Callout::new(
            "DatagramDataLayerV6",
            "DatagramData layer for ipv4",
            0xb25862cd_f744_4452_b14a_d0c1e5a25b30,
            Layer::DatagramDataV6,
            consts::FWP_ACTION_CALLOUT_INSPECTION,
            FilterType::NonResettable,
            stream_callouts::stream_layer_udp_v6,
        ),
        // -----------------------------------------
        // Packet layers
        Callout::new(
            "IPPacketOutboundV4",
            "IP packet outbound network layer callout for Ipv4",
            0xf3183afe_dc35_49f1_8ea2_b16b5666dd36,
            Layer::OutboundIppacketV4,
            consts::FWP_ACTION_CALLOUT_TERMINATING,
            FilterType::NonResettable,
            packet_callouts::ip_packet_layer_outbound_v4,
        ),
        Callout::new(
            "IPPacketInboundV4",
            "IP packet inbound network layer callout for Ipv4",
            0xf0369374_203d_4bf0_83d2_b2ad3cc17a50,
            Layer::InboundIppacketV4,
            consts::FWP_ACTION_CALLOUT_TERMINATING,
            FilterType::NonResettable,
            packet_callouts::ip_packet_layer_inbound_v4,
        ),
        Callout::new(
            "IPPacketOutboundV6",
            "IP packet outbound network layer callout for Ipv6",
            0x91daf8bc_0908_4bf8_9f81_2c538ab8f25a,
            Layer::OutboundIppacketV6,
            consts::FWP_ACTION_CALLOUT_TERMINATING,
            FilterType::NonResettable,
            packet_callouts::ip_packet_layer_outbound_v6,
        ),
        Callout::new(
            "IPPacketInboundV6",
            "IP packet inbound network layer callout for Ipv6",
            0xfe9faf5f_ceb2_4cd9_9995_f2f2b4f5fcc0,
            Layer::InboundIppacketV6,
            consts::FWP_ACTION_CALLOUT_TERMINATING,
            FilterType::NonResettable,
            packet_callouts::ip_packet_layer_inbound_v6,
        )
    ]
}
