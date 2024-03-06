#include "vmlinux-x86.h"
#include "bpf/bpf_helpers.h"
#include "bpf/bpf_tracing.h"

// IP Version
#define AF_INET 2
#define AF_INET6 10

// Protocols
#define TCP     6
#define UDP     17
#define UDPLite 136

#define OUTBOUND 0
#define INBOUND 1

char __license[] SEC("license") = "GPL";

// Ring buffer for all connection events
struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 1 << 24);
} pm_connection_events SEC(".maps");

// Event struct that will be sent to Go on each new connection. (The name should be the same as the go generate command)
struct Event {
	u32 saddr[4];
	u32 daddr[4];
	u16 sport;
	u16 dport;
	u32 pid;
	u8 ipVersion;
	u8 protocol;
	u8 direction;
};
struct Event *unused __attribute__((unused));

// Fentry of tcp_connect will be executed when equivalent kernel function is called.
// In the kernel all IP address and ports should be set before tcp_connect is called. [this-function] -> tcp_connect 
SEC("fentry/tcp_connect")
int BPF_PROG(tcp_connect, struct sock *sk) {
	// Alloc space for the event
	struct Event *tcp_info;
	tcp_info = bpf_ringbuf_reserve(&pm_connection_events, sizeof(struct Event), 0);
	if (!tcp_info) {
		return 0;
	}

	// Read PID (Careful: This is the Thread Group ID in kernel speak!)
	tcp_info->pid = __builtin_bswap32((u32)(bpf_get_current_pid_tgid() >> 32));

	// Set protocol
	tcp_info->protocol = TCP;

	// Set direction
	tcp_info->direction = OUTBOUND;

	// Set src and dist ports
	tcp_info->sport = __builtin_bswap16(sk->__sk_common.skc_num);
	tcp_info->dport = sk->__sk_common.skc_dport;

	// Set src and dist IPs
	if (sk->__sk_common.skc_family == AF_INET) {
		tcp_info->saddr[0] = __builtin_bswap32(sk->__sk_common.skc_rcv_saddr);
		tcp_info->daddr[0] = __builtin_bswap32(sk->__sk_common.skc_daddr);
		// Set IP version
		tcp_info->ipVersion = 4;
	} else if (sk->__sk_common.skc_family == AF_INET6) {
		for(int i = 0; i < 4; i++) {
			tcp_info->saddr[i] = __builtin_bswap32(sk->__sk_common.skc_v6_rcv_saddr.in6_u.u6_addr32[i]);
		}
		for(int i = 0; i < 4; i++) {
			tcp_info->daddr[i] = __builtin_bswap32(sk->__sk_common.skc_v6_daddr.in6_u.u6_addr32[i]);
		}
		// Set IP version
		tcp_info->ipVersion = 6;
	}

	// Send event
	bpf_ringbuf_submit(tcp_info, 0);
	return 0;
};

// Fexit(function exit) of udp_v4_connect will be executed after the ip4_datagram_connect kernel function is called.
// ip4_datagram_connect -> udp_v4_connect
SEC("fexit/ip4_datagram_connect")
int BPF_PROG(udp_v4_connect, struct sock *sk) {
	// Ignore everything else then IPv4
	if (sk->__sk_common.skc_family != AF_INET) {
		return 0;
	}

	// ip4_datagram_connect return error
	if (sk->__sk_common.skc_dport == 0) {
		return 0;
	}

	// Allocate space for the event.
	struct Event *udp_info;
	udp_info = bpf_ringbuf_reserve(&pm_connection_events, sizeof(struct Event), 0);
	if (!udp_info) {
		return 0;
	}

	// Read PID (Careful: This is the Thread Group ID in kernel speak!)
	udp_info->pid = __builtin_bswap32((u32)(bpf_get_current_pid_tgid() >> 32));

	// Set src and dst ports
	udp_info->sport = __builtin_bswap16(sk->__sk_common.skc_num);
	udp_info->dport = sk->__sk_common.skc_dport;

	// Set src and dst IPs
	udp_info->saddr[0] = __builtin_bswap32(sk->__sk_common.skc_rcv_saddr);
	udp_info->daddr[0] = __builtin_bswap32(sk->__sk_common.skc_daddr);

	// Set IP version
	udp_info->ipVersion = 4;

	// Set protocol
	if(sk->sk_protocol == IPPROTO_UDPLITE) {
		udp_info->protocol = UDPLite;
	} else {
		udp_info->protocol = UDP;
	}

	// Send event
	bpf_ringbuf_submit(udp_info, 0);
	return 0;
}

// Fentry(function enter) of udp_v6_connect will be executed after the ip6_datagram_connect kernel function is called.
// ip6_datagram_connect -> udp_v6_connect
SEC("fexit/ip6_datagram_connect")
int BPF_PROG(udp_v6_connect, struct sock *sk) {
	// Ignore everything else then IPv6
	if (sk->__sk_common.skc_family != AF_INET6) {
		return 0;
	}

	// ip6_datagram_connect return error
	if (sk->__sk_common.skc_dport == 0) {
		return 0;
	}

	// Make sure its udp6 socket
	struct udp6_sock *us = bpf_skc_to_udp6_sock(sk);
	if (!us) {
		return 0;
	}

	// Allocate space for the event.
	struct Event *udp_info;
	udp_info = bpf_ringbuf_reserve(&pm_connection_events, sizeof(struct Event), 0);
	if (!udp_info) {
		return 0;
	}

	// Read PID (Careful: This is the Thread Group ID in kernel speak!)
	udp_info->pid = __builtin_bswap32((u32)(bpf_get_current_pid_tgid() >> 32));

	// Set src and dst ports
	udp_info->sport = __builtin_bswap16(sk->__sk_common.skc_num);
	udp_info->dport = sk->__sk_common.skc_dport;

	// Set src and dst IPs
	for(int i = 0; i < 4; i++) {
		udp_info->saddr[i] = __builtin_bswap32(sk->__sk_common.skc_v6_rcv_saddr.in6_u.u6_addr32[i]);
	}
	for(int i = 0; i < 4; i++) {
		udp_info->daddr[i] = __builtin_bswap32(sk->__sk_common.skc_v6_daddr.in6_u.u6_addr32[i]);
	}

	// IP version
	udp_info->ipVersion = 6;

	// Set protocol
	if(sk->sk_protocol == IPPROTO_UDPLITE) {
		udp_info->protocol = UDPLite;
	} else {
		udp_info->protocol = UDP;
	}

	// Send event
	bpf_ringbuf_submit(udp_info, 0);
	return 0;
}