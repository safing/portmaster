#include "vmlinux-x86.h"
#include "bpf/bpf_helpers.h"
#include "bpf/bpf_tracing.h"
#include "bpf/bpf_core_read.h"

#define AF_INET 2
#define AF_INET6 10

#define PROTOCOL_TCP 6
#define PROTOCOL_UDP 17

char __license[] SEC("license") = "GPL";

struct sk_key {
	u32 src_ip[4];
	u32 dst_ip[4];
	u16 src_port;
	u16 dst_port;
	u8 protocol;
	u8 ipv6;
};

struct sk_info {
	u64 rx;
	u64 tx;
	u64 reported;
};

// Max number of connections that will be kept. Increse the number if it's not enough.
#define SOCKOPS_MAP_SIZE 5000
struct {
	__uint(type, BPF_MAP_TYPE_LRU_HASH);
	__uint(max_entries, SOCKOPS_MAP_SIZE);
	__type(key, struct sk_key);
	__type(value, struct sk_info);
} pm_bandwidth_map SEC(".maps");

SEC("sockops")
int socket_operations(struct bpf_sock_ops *skops) {
	switch (skops->op) {
	case BPF_SOCK_OPS_TCP_CONNECT_CB: // Outgoing connections
		// Set flag so any modification on the socket, will trigger this function.
		bpf_sock_ops_cb_flags_set(skops, BPF_SOCK_OPS_ALL_CB_FLAGS);
		return 0;
	case BPF_SOCK_OPS_TCP_LISTEN_CB: // Listening ports
		bpf_sock_ops_cb_flags_set(skops, BPF_SOCK_OPS_ALL_CB_FLAGS);
		// No rx tx data for this socket object.
		return 0;
	case BPF_SOCK_OPS_PASSIVE_ESTABLISHED_CB: // Incoming connections
		// Set flag so any modification on the socket, will trigger this function.
		bpf_sock_ops_cb_flags_set(skops, BPF_SOCK_OPS_ALL_CB_FLAGS);
		return 0;
	default:
		break;
	}

	struct bpf_sock *sk = skops->sk;
	if (sk == NULL) {
		return 0;
	}

	struct sk_key key = {0};
	key.protocol = PROTOCOL_TCP;
	if(sk->family == AF_INET) {
		// Generate key for IPv4
		key.src_ip[0] = sk->src_ip4;
		key.src_port = sk->src_port;
		key.dst_ip[0] = sk->dst_ip4;
		key.dst_port = __builtin_bswap16(sk->dst_port);
		key.ipv6 = 0;

		struct sk_info newInfo = {0};
		newInfo.rx = skops->bytes_received;
		newInfo.tx = skops->bytes_acked;

		bpf_map_update_elem(&pm_bandwidth_map, &key, &newInfo, BPF_ANY);
	} else if(sk->family == AF_INET6){
		// Generate key for IPv6
		key.src_ip[0] = sk->src_ip6[0];
		key.src_ip[1] = sk->src_ip6[1];
		key.src_ip[2] = sk->src_ip6[2];
		key.src_ip[3] = sk->src_ip6[3];
		key.src_port = sk->src_port;

		key.dst_ip[0] = sk->dst_ip6[0];
		key.dst_ip[1] = sk->dst_ip6[1];
		key.dst_ip[2] = sk->dst_ip6[2];
		key.dst_ip[3] = sk->dst_ip6[3];
		key.dst_port = __builtin_bswap16(sk->dst_port);

		key.ipv6 = 1;

		struct sk_info newInfo = {0};
		newInfo.rx = skops->bytes_received;
		newInfo.tx = skops->bytes_acked;

		bpf_map_update_elem(&pm_bandwidth_map, &key, &newInfo, BPF_ANY);
	}

	return 0;
}

// udp_sendmsg hookes to the respective kernel function and saves the bandwidth data
SEC("fentry/udp_sendmsg")
int BPF_PROG(udp_sendmsg, struct sock *sk, struct msghdr *msg, size_t len) {
	struct sock_common *skc = &sk->__sk_common;

	// Create a key for the map and set all the nececery information.
	struct sk_key key = {0};
	key.protocol = PROTOCOL_UDP;
	key.src_ip[0] = skc->skc_rcv_saddr;
	key.dst_ip[0] = skc->skc_daddr;
	key.src_port = skc->skc_num;
	key.dst_port = __builtin_bswap16(skc->skc_dport);
	key.ipv6 = 0;

	// Update the map with the new information
	struct sk_info *info = bpf_map_lookup_elem(&pm_bandwidth_map, &key);
	if (info != NULL) {
		__sync_fetch_and_add(&info->tx, len); // TODO: Use atomic instead.
		__sync_fetch_and_and(&info->reported, 0); // TODO: Use atomic instead.
	} else {
		struct sk_info newInfo = {0};

		newInfo.tx = len;
		bpf_map_update_elem(&pm_bandwidth_map, &key, &newInfo, BPF_ANY);
	}

	return 0;
};

// udp_recvmsg hookes to the respective kernel function and saves the bandwidth data
SEC("fentry/udp_recvmsg")
int BPF_PROG(udp_recvmsg, struct sock *sk, struct msghdr *msg, size_t len, int flags, int *addr_len) {
	struct sock_common *skc = &sk->__sk_common;

	// Create a key for the map and set all the nececery information.
	struct sk_key key = {0};
	key.protocol = PROTOCOL_UDP;
	key.src_ip[0] = skc->skc_rcv_saddr;
	key.dst_ip[0] = skc->skc_daddr;
	key.src_port = skc->skc_num;
	key.dst_port = __builtin_bswap16(skc->skc_dport);
	key.ipv6 = 0;

	// Update the map with the new information
	struct sk_info *info = bpf_map_lookup_elem(&pm_bandwidth_map, &key);
	if (info != NULL) {
		__sync_fetch_and_add(&info->rx, len); // TODO: Use atomic instead.
		__sync_fetch_and_and(&info->reported, 0); // TODO: Use atomic instead.
	} else {
		struct sk_info newInfo = {0};

		newInfo.rx = len;
		bpf_map_update_elem(&pm_bandwidth_map, &key, &newInfo, BPF_ANY);
	}

	return 0;
};

// udpv6_sendmsg hookes to the respective kernel function and saves the bandwidth data
SEC("fentry/udpv6_sendmsg")
int BPF_PROG(udpv6_sendmsg, struct sock *sk, struct msghdr *msg, size_t len) {
	struct sock_common *skc = &sk->__sk_common;

	// Create a key for the map and set all the nececery information.
	struct sk_key key = {0};
	key.protocol = PROTOCOL_UDP;
	for (int i = 0; i < 4; i++) {
		key.src_ip[i] = skc->skc_v6_rcv_saddr.in6_u.u6_addr32[i];
		key.dst_ip[i] = skc->skc_v6_rcv_saddr.in6_u.u6_addr32[i];
	}
	key.src_port = skc->skc_num;
	key.dst_port = __builtin_bswap16(skc->skc_dport);
	key.ipv6 = 1;

	// Update the map with the new information
	struct sk_info *info = bpf_map_lookup_elem(&pm_bandwidth_map, &key);
	if (info != NULL) {
		__sync_fetch_and_add(&info->tx, len); // TODO: Use atomic instead.
		__sync_fetch_and_and(&info->reported, 0); // TODO: Use atomic instead.
	} else {
		struct sk_info newInfo = {0};
		newInfo.tx = len;
		bpf_map_update_elem(&pm_bandwidth_map, &key, &newInfo, BPF_ANY);
	}

	return 0;
}

// udpv6_recvmsg hookes to the respective kernel function and saves the bandwidth data
SEC("fentry/udpv6_recvmsg")
int BPF_PROG(udpv6_recvmsg, struct sock *sk, struct msghdr *msg, size_t len, int flags, int *addr_len) {
	struct sock_common *skc = &sk->__sk_common;

	// Create a key for the map and set all the nececery information.
	struct sk_key key = {0};
	key.protocol = PROTOCOL_UDP;
	for (int i = 0; i < 4; i++) {
		key.src_ip[i] = skc->skc_v6_rcv_saddr.in6_u.u6_addr32[i];
		key.dst_ip[i] = skc->skc_v6_rcv_saddr.in6_u.u6_addr32[i];
	}
	key.src_port = skc->skc_num;
	key.dst_port = __builtin_bswap16(skc->skc_dport);
	key.ipv6 = 1;

	// Update the map with the new information
	struct sk_info *info = bpf_map_lookup_elem(&pm_bandwidth_map, &key);
	if (info != NULL) {
		__sync_fetch_and_add(&info->rx, len); // TODO: Use atomic instead.
		__sync_fetch_and_and(&info->reported, 0); // TODO: Use atomic instead.
	} else {
		struct sk_info newInfo = {0};
		newInfo.rx = len;
		bpf_map_update_elem(&pm_bandwidth_map, &key, &newInfo, BPF_ANY);
	}

	return 0;
}