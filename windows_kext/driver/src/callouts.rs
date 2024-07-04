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
            "Portmaster ALE Outbound IPv4",
            "Portmaster uses this layer to block/permit outgoing ipv4 connections",
            0x58545073_f893_454c_bbea_a57bc964f46d,
            Layer::AleAuthConnectV4,
            consts::FWP_ACTION_CALLOUT_TERMINATING,
            FilterType::Resettable,
            ale_callouts::ale_layer_connect_v4,
        ),
        Callout::new(
            "Portmaster ALE Outbound IPv6",
            "Portmaster uses this layer to block/permit outgoing ipv6 connections",
            0x4bd2a080_2585_478d_977c_7f340c6bc3d4,
            Layer::AleAuthConnectV6,
            consts::FWP_ACTION_CALLOUT_TERMINATING,
            FilterType::Resettable,
            ale_callouts::ale_layer_connect_v6,
        ),
        // -----------------------------------------
        // ALE connection end layers
        Callout::new(
            "Portmaster Endpoint Closure IPv4",
            "Portmaster uses this layer to detect when a IPv4 connection has ended",
            0x58f02845_ace9_4455_ac80_8a84b86fe566,
            Layer::AleEndpointClosureV4,
            consts::FWP_ACTION_CALLOUT_INSPECTION,
            FilterType::NonResettable,
            ale_callouts::endpoint_closure_v4,
        ),
        Callout::new(
            "Portmaster Endpoint Closure IPv6",
            "Portmaster uses this layer to detect when a IPv6 connection has ended",
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
            "Portmaster resource release IPv4",
            "Portmaster uses this layer to detect when a IPv4 port has been released",
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
            "Portmaster resource release IPv6",
            "Portmaster uses this layer to detect when a IPv6 port has been released",
            0x6cf36e04_e656_42c3_8cac_a1ce05328bd1,
            Layer::AleResourceReleaseV6,
            consts::FWP_ACTION_CALLOUT_INSPECTION,
            FilterType::NonResettable,
            ale_callouts::ale_resource_monitor,
        ),
        // -----------------------------------------
        // Stream layer
        Callout::new(
            "Portmaster Stream IPv4",
            "Portmaster uses this layer for bandwidth statistics of IPv4 TCP connections",
            0xe2ca13bf_9710_4caa_a45c_e8c78b5ac780,
            Layer::StreamV4,
            consts::FWP_ACTION_CALLOUT_INSPECTION,
            FilterType::NonResettable,
            stream_callouts::stream_layer_tcp_v4,
        ),
        Callout::new(
            "Portmaster Stream IPv6",
            "Portmaster uses this layer for bandwidth statistics of IPv6 TCP connections",
            0x66c549b3_11e2_4b27_8f73_856e6fd82baa,
            Layer::StreamV6,
            consts::FWP_ACTION_CALLOUT_INSPECTION,
            FilterType::NonResettable,
            stream_callouts::stream_layer_tcp_v6,
        ),
        Callout::new(
            "Portmaster Datagram IPv4",
            "Portmaster uses this layer for bandwidth statistics of IPv4 UDP connections",
            0xe7eeeaba_168a_45bb_8747_e1a702feb2c5,
            Layer::DatagramDataV4,
            consts::FWP_ACTION_CALLOUT_INSPECTION,
            FilterType::NonResettable,
            stream_callouts::stream_layer_udp_v4,
        ),
        Callout::new(
            "Portmaster Datagram IPv6",
            "Portmaster uses this layer for bandwidth statistics of IPv6 UDP connections",
            0xb25862cd_f744_4452_b14a_d0c1e5a25b30,
            Layer::DatagramDataV6,
            consts::FWP_ACTION_CALLOUT_INSPECTION,
            FilterType::NonResettable,
            stream_callouts::stream_layer_udp_v6,
        ),
        // -----------------------------------------
        // Packet layers
        Callout::new(
            "Portmaster Packet Outbound IPv4",
            "Portmaster uses this layer to redirect/block/permit outgoing ipv4 packets",
            0xf3183afe_dc35_49f1_8ea2_b16b5666dd36,
            Layer::OutboundIppacketV4,
            consts::FWP_ACTION_CALLOUT_TERMINATING,
            FilterType::NonResettable,
            packet_callouts::ip_packet_layer_outbound_v4,
        ),
        Callout::new(
            "Portmaster Packet Inbound IPv4",
            "Portmaster uses this layer to redirect/block/permit inbound ipv4 packets",
            0xf0369374_203d_4bf0_83d2_b2ad3cc17a50,
            Layer::InboundIppacketV4,
            consts::FWP_ACTION_CALLOUT_TERMINATING,
            FilterType::NonResettable,
            packet_callouts::ip_packet_layer_inbound_v4,
        ),
        Callout::new(
            "Portmaster Packet Outbound IPv6",
            "Portmaster uses this layer to redirect/block/permit outgoing ipv6 packets",
            0x91daf8bc_0908_4bf8_9f81_2c538ab8f25a,
            Layer::OutboundIppacketV6,
            consts::FWP_ACTION_CALLOUT_TERMINATING,
            FilterType::NonResettable,
            packet_callouts::ip_packet_layer_outbound_v6,
        ),
        Callout::new(
            "Portmaster Packet Inbound IPv6",
            "Portmaster uses this layer to redirect/block/permit inbound ipv6 packets",
            0xfe9faf5f_ceb2_4cd9_9995_f2f2b4f5fcc0,
            Layer::InboundIppacketV6,
            consts::FWP_ACTION_CALLOUT_TERMINATING,
            FilterType::NonResettable,
            packet_callouts::ip_packet_layer_inbound_v6,
        )
    ]
}
