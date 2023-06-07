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

char __license[] SEC("license") = "GPL";

// Ring buffer for all connection events
struct {
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 1 << 24);
} events SEC(".maps");

// Event struct that will be sent to Go on each new connection. (The name should be the same as the go generate command)
struct Event {
	u32 saddr[4];
	u32 daddr[4];
	u16 sport;
	u16 dport;
	u32 pid;
	u8 ipVersion;
	u8 protocol;
};
struct Event *unused __attribute__((unused));

// Fexit of tcp_v4_connect will be executed when equivalent kernel function returns.
// In the kernel function all IPs and ports are set and then tcp_connect is called. tcp_v4_connect -> tcp_connect -> [this-function]
SEC("fexit/tcp_v4_connect")
int BPF_PROG(tcp_v4_connect, struct sock *sk) {
	// Ignore everything else then IPv4
	if (sk->__sk_common.skc_family != AF_INET) {
		return 0;
	}

	// Make sure it's tcp sock
	struct tcp_sock *ts = bpf_skc_to_tcp_sock(sk);
	if (!ts) {
		return 0;
	}

	// Alloc space for the event
	struct Event *tcp_info;
	tcp_info = bpf_ringbuf_reserve(&events, sizeof(struct Event), 0);
	if (!tcp_info) {
		return 0;
	}

  // Read PID
	tcp_info->pid = __builtin_bswap32((u32)bpf_get_current_pid_tgid());

	// Set src and dist ports
	tcp_info->dport = sk->__sk_common.skc_dport;
	tcp_info->sport = sk->__sk_common.skc_num;

	// Set src and dist IPs
	tcp_info->saddr[0] = __builtin_bswap32(sk->__sk_common.skc_rcv_saddr);
	tcp_info->daddr[0] = __builtin_bswap32(sk->__sk_common.skc_daddr);

	// Set IP version
	tcp_info->ipVersion = 4;

	// Set protocol
	tcp_info->protocol = TCP;

  // Send event
	bpf_ringbuf_submit(tcp_info, 0);
	return 0;
};

// Fexit(function exit) of tcp_v6_connect will be executed when equivalent kernel function returns.
// In the kernel function all IPs and ports are set and then tcp_connect is called. tcp_v6_connect -> tcp_connect -> [this-function]
SEC("fexit/tcp_v6_connect")
int BPF_PROG(tcp_v6_connect, struct sock *sk) {
	// Ignore everything else then IPv6
	if (sk->__sk_common.skc_family != AF_INET6) {
		return 0;
	}

	// Make sure its a tcp6 sock
	struct tcp6_sock *ts = bpf_skc_to_tcp6_sock(sk);
	if (!ts) {
		return 0;
	}

	// Alloc space for the event
	struct Event *tcp_info;
	tcp_info = bpf_ringbuf_reserve(&events, sizeof(struct Event), 0);
	if (!tcp_info) {
		return 0;
	}

	// Read PID
	tcp_info->pid = __builtin_bswap32((u32)bpf_get_current_pid_tgid());

	// Set src and dist ports
	tcp_info->dport = sk->__sk_common.skc_dport;
	tcp_info->sport = sk->__sk_common.skc_num;

	// Set src and dist IPs
	for(int i = 0; i < 4; i++) {
		tcp_info->saddr[i] = __builtin_bswap32(sk->__sk_common.skc_v6_rcv_saddr.in6_u.u6_addr32[i]);
	}
	for(int i = 0; i < 4; i++) {
		tcp_info->daddr[i] = __builtin_bswap32(sk->__sk_common.skc_v6_daddr.in6_u.u6_addr32[i]);
	}

	// Set IP version
	tcp_info->ipVersion = 6;

	// Set protocol
	tcp_info->protocol = TCP;

  // Send event
	bpf_ringbuf_submit(tcp_info, 0);
	return 0;
};

// Fentry(function enter) of udp_sendmsg will be executed before equivalent kernel function is called.
// [this-function] -> udp_sendmsg
SEC("fentry/udp_sendmsg")
int BPF_PROG(udp_sendmsg, struct sock *sk) {
	// Ignore everything else then IPv4
	if (sk->__sk_common.skc_family != AF_INET) {
		return 0;
	}

  // Allocate space for the event.
	struct Event *udp_info;
	udp_info = bpf_ringbuf_reserve(&events, sizeof(struct Event), 0);
	if (!udp_info) {
		return 0;
	}

	// Read PID
	udp_info->pid = __builtin_bswap32((u32)bpf_get_current_pid_tgid());

	// Set src and dist ports
	udp_info->dport = sk->__sk_common.skc_dport;
	udp_info->sport = sk->__sk_common.skc_num;

	// Set src and dist IPs
	udp_info->saddr[0] = __builtin_bswap32(sk->__sk_common.skc_rcv_saddr);
	udp_info->daddr[0] = __builtin_bswap32(sk->__sk_common.skc_daddr);

	// Set IP version
	udp_info->ipVersion = 4;

	// Set protocol. No way to detect udplite for ipv4
	udp_info->protocol = UDP;

  // Send event
	bpf_ringbuf_submit(udp_info, 0);
	return 0;
}

// Fentry(function enter) of udpv6_sendmsg will be executed before equivalent kernel function is called.
// [this-function] -> udpv6_sendmsg
SEC("fentry/udpv6_sendmsg")
int BPF_PROG(udpv6_sendmsg, struct sock *sk) {
	// Ignore everything else then IPv6
	if (sk->__sk_common.skc_family != AF_INET6) {
		return 0;
	}

  // Make sure its udp6 socket
	struct udp6_sock *us = bpf_skc_to_udp6_sock(sk);
	if (!us) {
		return 0;
	}

  // Allocate space for the event.
	struct Event *udp_info;
	udp_info = bpf_ringbuf_reserve(&events, sizeof(struct Event), 0);
	if (!udp_info) {
		return 0;
	}

  // Read PID
	udp_info->pid = __builtin_bswap32((u32)bpf_get_current_pid_tgid());

	// Set src and dist ports
	udp_info->dport = sk->__sk_common.skc_dport;
	udp_info->sport = sk->__sk_common.skc_num;

	// Set src and dist IPs
	for(int i = 0; i < 4; i++) {
		udp_info->saddr[i] = __builtin_bswap32(sk->__sk_common.skc_v6_rcv_saddr.in6_u.u6_addr32[i]);
	}
	for(int i = 0; i < 4; i++) {
		udp_info->daddr[i] = __builtin_bswap32(sk->__sk_common.skc_v6_daddr.in6_u.u6_addr32[i]);
	}

	// IP version
	udp_info->ipVersion = 6;

	// Set protocol for UDPLite
	if(us->udp.pcflag == 0) {
		udp_info->protocol = UDP;
	} else {
		udp_info->protocol = UDPLite;
	}

  // Send event
	bpf_ringbuf_submit(udp_info, 0);
	return 0;
}