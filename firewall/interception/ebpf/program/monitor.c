#include "vmlinux-x86.h"
#include "bpf/bpf_helpers.h"
#include "bpf/bpf_tracing.h"

typedef unsigned char u8;
typedef short int s16;
typedef short unsigned int u16;
typedef int s32;
typedef unsigned int u32;
typedef long long int s64;
typedef long long unsigned int u64;
typedef u16 le16;
typedef u16 be16;
typedef u32 be32;
typedef u64 be64;
typedef u32 wsum;


#define AF_INET 2
#define AF_INET6 10
#define TASK_COMM_LEN 16

char __license[] SEC("license") = "Dual MIT/GPL";

struct
{
	__uint(type, BPF_MAP_TYPE_RINGBUF);
	__uint(max_entries, 1 << 24);
} events SEC(".maps");

/**
 * The sample submitted to userspace over a ring buffer.
 * Emit struct event's type info into the ELF's BTF so bpf2go
 * can generate a Go type from it.
 */
struct event {
	be32 saddr[4];
	be32 daddr[4];
	u16 sport;
	u16 dport;
	u32 pid;
	u8 ipVersion;
};
struct event *unused __attribute__((unused));

SEC("fexit/tcp_v4_connect")
int BPF_PROG(tcp_v4_connect, struct sock *sk) {
	if (sk->__sk_common.skc_family != AF_INET) {
		return 0;
	}

	struct tcp_sock *ts = bpf_skc_to_tcp_sock(sk);
	if (!ts) {
		return 0;
	}

	struct event *tcp_info;
	tcp_info = bpf_ringbuf_reserve(&events, sizeof(struct event), 0);
	if (!tcp_info) {
		return 0;
	}

	tcp_info->pid = __builtin_bswap32((u32)bpf_get_current_pid_tgid());
	tcp_info->dport = sk->__sk_common.skc_dport;
	tcp_info->sport = sk->__sk_common.skc_num;
	tcp_info->saddr[0] = __builtin_bswap32(sk->__sk_common.skc_rcv_saddr);
	tcp_info->daddr[0] = __builtin_bswap32(sk->__sk_common.skc_daddr);
	tcp_info->ipVersion = 4;

	bpf_ringbuf_submit(tcp_info, 0);
	return 0;
};

SEC("fexit/tcp_v6_connect")
int BPF_PROG(tcp_v6_connect, struct sock *sk) {
	if (sk->__sk_common.skc_family != AF_INET6) {
		return 0;
	}

	struct tcp_sock *ts = bpf_skc_to_tcp_sock(sk);
	if (!ts) {
		return 0;
	}

	struct event *tcp_info;
	tcp_info = bpf_ringbuf_reserve(&events, sizeof(struct event), 0);
	if (!tcp_info) {
		return 0;
	}

	tcp_info->pid = __builtin_bswap32((u32)bpf_get_current_pid_tgid());
	for(int i = 0; i < 4; i++) {
		tcp_info->saddr[i] = __builtin_bswap32(sk->__sk_common.skc_v6_rcv_saddr.in6_u.u6_addr32[i]);
	}
	for(int i = 0; i < 4; i++) {
		tcp_info->daddr[i] = __builtin_bswap32(sk->__sk_common.skc_v6_daddr.in6_u.u6_addr32[i]);
	}
	tcp_info->dport = sk->__sk_common.skc_dport;
	tcp_info->sport = sk->__sk_common.skc_num;
	tcp_info->ipVersion = 6;

	bpf_ringbuf_submit(tcp_info, 0);
	return 0;
};

// SEC("fentry/udp_sendmsg")