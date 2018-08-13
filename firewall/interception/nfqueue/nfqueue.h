#pragma once
// #define _BSD_SOURCE
// #define __BSD_SOURCE

// #define __FAVOR_BSD // Just Using _BSD_SOURCE didn't work on my system for some reason
// #define __USE_BSD
#include <stdlib.h>
// #include <sys/socket.h>
// #include <netinet/in.h>
#include <arpa/inet.h>
#include <linux/ip.h>
#include <linux/tcp.h>
#include <linux/udp.h>
#include <linux/ipv6.h>
// #include <linux/netfilter.h>
#include <libnetfilter_queue/libnetfilter_queue.h>

// extern int nfq_callback(uint8_t version, uint8_t protocol, unsigned char *saddr, unsigned char *daddr,
// 						uint16_t sport, uint16_t dport, unsigned char * extra, void* data);

int nfqueue_cb_new(struct nfq_q_handle *qh, struct nfgenmsg *nfmsg, struct nfq_data *nfa, void *data);
void loop_for_packets(struct nfq_handle *h);

static inline struct nfq_q_handle * create_queue(struct nfq_handle *h, uint16_t qid) {
  //we use this because it's more convient to pass the callback in C
  // FIXME: check malloc success
  uint16_t *data = malloc(sizeof(uint16_t));
  *data = qid;
  return nfq_create_queue(h, qid, &nfqueue_cb_new, (void*)data);
}
