/*
 * windivert.h
 * (C) 2018, all rights reserved,
 *
 * This file is part of WinDivert.
 *
 * WinDivert is free software: you can redistribute it and/or modify it under
 * the terms of the GNU Lesser General Public License as published by the
 * Free Software Foundation, either version 3 of the License, or (at your
 * option) any later version.
 *
 * This program is distributed in the hope that it will be useful, but
 * WITHOUT ANY WARRANTY; without even the implied warranty of MERCHANTABILITY
 * or FITNESS FOR A PARTICULAR PURPOSE.  See the GNU Lesser General Public
 * License for more details.
 *
 * You should have received a copy of the GNU Lesser General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 * WinDivert is free software; you can redistribute it and/or modify it under
 * the terms of the GNU General Public License as published by the Free
 * Software Foundation; either version 2 of the License, or (at your option)
 * any later version.
 * 
 * This program is distributed in the hope that it will be useful, but
 * WITHOUT ANY WARRANTY; without even the implied warranty of MERCHANTABILITY
 * or FITNESS FOR A PARTICULAR PURPOSE.  See the GNU General Public License
 * for more details.
 * 
 * You should have received a copy of the GNU General Public License along
 * with this program; if not, write to the Free Software Foundation, Inc., 51
 * Franklin Street, Fifth Floor, Boston, MA 02110-1301, USA.
 */

#ifndef __WINDIVERT_H
#define __WINDIVERT_H

#ifndef WINDIVERT_KERNEL
#include <windows.h>
#endif      /* WINDIVERT_KERNEL */

#ifndef WINDIVERTEXPORT
#define WINDIVERTEXPORT     __declspec(dllimport)
#endif      /* WINDIVERTEXPORT */

#ifdef __MINGW32__
#define __in
#define __in_opt
#define __out
#define __out_opt
#define __inout
#define __inout_opt
#include <stdint.h>
#define INT8    int8_t
#define UINT8   uint8_t
#define INT16   int16_t
#define UINT16  uint16_t
#define INT32   int32_t
#define UINT32  uint32_t
#define INT64   int64_t
#define UINT64  uint64_t
#endif      /* __MINGW32__ */

#ifdef __cplusplus
extern "C" {
#endif

/****************************************************************************/
/* WINDIVERT API                                                            */
/****************************************************************************/

/*
 * Divert address.
 */
typedef struct
{
    INT64  Timestamp;                   /* Packet's timestamp. */
    UINT32 IfIdx;                       /* Packet's interface index. */
    UINT32 SubIfIdx;                    /* Packet's sub-interface index. */
    UINT8  Direction:1;                 /* Packet's direction. */
    UINT8  Loopback:1;                  /* Packet is loopback? */
    UINT8  Impostor:1;                  /* Packet is impostor? */
    UINT8  PseudoIPChecksum:1;          /* Packet has pseudo IPv4 checksum? */
    UINT8  PseudoTCPChecksum:1;         /* Packet has pseudo TCP checksum? */
    UINT8  PseudoUDPChecksum:1;         /* Packet has pseudo UDP checksum? */
    UINT8  Reserved:2;
} WINDIVERT_ADDRESS, *PWINDIVERT_ADDRESS;

#define WINDIVERT_DIRECTION_OUTBOUND    0
#define WINDIVERT_DIRECTION_INBOUND     1

/*
 * Divert layers.
 */
typedef enum
{
    WINDIVERT_LAYER_NETWORK = 0,        /* Network layer. */
    WINDIVERT_LAYER_NETWORK_FORWARD = 1 /* Network layer (forwarded packets) */
} WINDIVERT_LAYER, *PWINDIVERT_LAYER;

/*
 * Divert flags.
 */
#define WINDIVERT_FLAG_SNIFF            1
#define WINDIVERT_FLAG_DROP             2
#define WINDIVERT_FLAG_DEBUG            4

/*
 * Divert parameters.
 */
typedef enum
{
    WINDIVERT_PARAM_QUEUE_LEN  = 0,     /* Packet queue length. */
    WINDIVERT_PARAM_QUEUE_TIME = 1,     /* Packet queue time. */
    WINDIVERT_PARAM_QUEUE_SIZE = 2      /* Packet queue size. */
} WINDIVERT_PARAM, *PWINDIVERT_PARAM;
#define WINDIVERT_PARAM_MAX             WINDIVERT_PARAM_QUEUE_SIZE

#ifndef WINDIVERT_KERNEL

/*
 * Open a WinDivert handle.
 */
extern WINDIVERTEXPORT HANDLE WinDivertOpen(
    __in        const char *filter,
    __in        WINDIVERT_LAYER layer,
    __in        INT16 priority,
    __in        UINT64 flags);

/*
 * Receive (read) a packet from a WinDivert handle.
 */
extern WINDIVERTEXPORT BOOL WinDivertRecv(
    __in        HANDLE handle,
    __out       PVOID pPacket,
    __in        UINT packetLen,
    __out_opt   PWINDIVERT_ADDRESS pAddr,
    __out_opt   UINT *readLen);

/*
 * Receive (read) a packet from a WinDivert handle.
 */
extern WINDIVERTEXPORT BOOL WinDivertRecvEx(
    __in        HANDLE handle,
    __out       PVOID pPacket,
    __in        UINT packetLen,
    __in        UINT64 flags,
    __out_opt   PWINDIVERT_ADDRESS pAddr,
    __out_opt   UINT *readLen,
    __inout_opt LPOVERLAPPED lpOverlapped);

/*
 * Send (write/inject) a packet to a WinDivert handle.
 */
extern WINDIVERTEXPORT BOOL WinDivertSend(
    __in        HANDLE handle,
    __in        PVOID pPacket,
    __in        UINT packetLen,
    __in        PWINDIVERT_ADDRESS pAddr,
    __out_opt   UINT *writeLen);

/*
 * Send (write/inject) a packet to a WinDivert handle.
 */
extern WINDIVERTEXPORT BOOL WinDivertSendEx(
    __in        HANDLE handle,
    __in        PVOID pPacket,
    __in        UINT packetLen,
    __in        UINT64 flags,
    __in        PWINDIVERT_ADDRESS pAddr,
    __out_opt   UINT *writeLen,
    __inout_opt LPOVERLAPPED lpOverlapped);

/*
 * Close a WinDivert handle.
 */
extern WINDIVERTEXPORT BOOL WinDivertClose(
    __in        HANDLE handle);

/*
 * Set a WinDivert handle parameter.
 */
extern WINDIVERTEXPORT BOOL WinDivertSetParam(
    __in        HANDLE handle,
    __in        WINDIVERT_PARAM param,
    __in        UINT64 value);

/*
 * Get a WinDivert handle parameter.
 */
extern WINDIVERTEXPORT BOOL WinDivertGetParam(
    __in        HANDLE handle,
    __in        WINDIVERT_PARAM param,
    __out       UINT64 *pValue);

#endif      /* WINDIVERT_KERNEL */

/****************************************************************************/
/* WINDIVERT HELPER API                                                     */
/****************************************************************************/

/*
 * IPv4/IPv6/ICMP/ICMPv6/TCP/UDP header definitions.
 */
typedef struct
{
    UINT8  HdrLength:4;
    UINT8  Version:4;
    UINT8  TOS;
    UINT16 Length;
    UINT16 Id;
    UINT16 FragOff0;
    UINT8  TTL;
    UINT8  Protocol;
    UINT16 Checksum;
    UINT32 SrcAddr;
    UINT32 DstAddr;
} WINDIVERT_IPHDR, *PWINDIVERT_IPHDR;

#define WINDIVERT_IPHDR_GET_FRAGOFF(hdr)                    \
    (((hdr)->FragOff0) & 0xFF1F)
#define WINDIVERT_IPHDR_GET_MF(hdr)                         \
    ((((hdr)->FragOff0) & 0x0020) != 0)
#define WINDIVERT_IPHDR_GET_DF(hdr)                         \
    ((((hdr)->FragOff0) & 0x0040) != 0)
#define WINDIVERT_IPHDR_GET_RESERVED(hdr)                   \
    ((((hdr)->FragOff0) & 0x0080) != 0)

#define WINDIVERT_IPHDR_SET_FRAGOFF(hdr, val)               \
    do                                                      \
    {                                                       \
        (hdr)->FragOff0 = (((hdr)->FragOff0) & 0x00E0) |    \
            ((val) & 0xFF1F);                               \
    }                                                       \
    while (FALSE)
#define WINDIVERT_IPHDR_SET_MF(hdr, val)                    \
    do                                                      \
    {                                                       \
        (hdr)->FragOff0 = (((hdr)->FragOff0) & 0xFFDF) |    \
            (((val) & 0x0001) << 5);                        \
    }                                                       \
    while (FALSE)
#define WINDIVERT_IPHDR_SET_DF(hdr, val)                    \
    do                                                      \
    {                                                       \
        (hdr)->FragOff0 = (((hdr)->FragOff0) & 0xFFBF) |    \
            (((val) & 0x0001) << 6);                        \
    }                                                       \
    while (FALSE)
#define WINDIVERT_IPHDR_SET_RESERVED(hdr, val)              \
    do                                                      \
    {                                                       \
        (hdr)->FragOff0 = (((hdr)->FragOff0) & 0xFF7F) |    \
            (((val) & 0x0001) << 7);                        \
    }                                                       \
    while (FALSE)

typedef struct
{
    UINT8  TrafficClass0:4;
    UINT8  Version:4;
    UINT8  FlowLabel0:4;
    UINT8  TrafficClass1:4;
    UINT16 FlowLabel1;
    UINT16 Length;
    UINT8  NextHdr;
    UINT8  HopLimit;
    UINT32 SrcAddr[4];
    UINT32 DstAddr[4];
} WINDIVERT_IPV6HDR, *PWINDIVERT_IPV6HDR;

#define WINDIVERT_IPV6HDR_GET_TRAFFICCLASS(hdr)             \
    ((((hdr)->TrafficClass0) << 4) | ((hdr)->TrafficClass1))
#define WINDIVERT_IPV6HDR_GET_FLOWLABEL(hdr)                \
    ((((UINT32)(hdr)->FlowLabel0) << 16) | ((UINT32)(hdr)->FlowLabel1))

#define WINDIVERT_IPV6HDR_SET_TRAFFICCLASS(hdr, val)        \
    do                                                      \
    {                                                       \
        (hdr)->TrafficClass0 = ((UINT8)(val) >> 4);         \
        (hdr)->TrafficClass1 = (UINT8)(val);                \
    }                                                       \
    while (FALSE)
#define WINDIVERT_IPV6HDR_SET_FLOWLABEL(hdr, val)           \
    do                                                      \
    {                                                       \
        (hdr)->FlowLabel0 = (UINT8)((val) >> 16);           \
        (hdr)->FlowLabel1 = (UINT16)(val);                  \
    }                                                       \
    while (FALSE)

typedef struct
{
    UINT8  Type;
    UINT8  Code;
    UINT16 Checksum;
    UINT32 Body;
} WINDIVERT_ICMPHDR, *PWINDIVERT_ICMPHDR;

typedef struct
{
    UINT8  Type;
    UINT8  Code;
    UINT16 Checksum;
    UINT32 Body;
} WINDIVERT_ICMPV6HDR, *PWINDIVERT_ICMPV6HDR;

typedef struct
{
    UINT16 SrcPort;
    UINT16 DstPort;
    UINT32 SeqNum;
    UINT32 AckNum;
    UINT16 Reserved1:4;
    UINT16 HdrLength:4;
    UINT16 Fin:1;
    UINT16 Syn:1;
    UINT16 Rst:1;
    UINT16 Psh:1;
    UINT16 Ack:1;
    UINT16 Urg:1;
    UINT16 Reserved2:2;
    UINT16 Window;
    UINT16 Checksum;
    UINT16 UrgPtr;
} WINDIVERT_TCPHDR, *PWINDIVERT_TCPHDR;

typedef struct
{
    UINT16 SrcPort;
    UINT16 DstPort;
    UINT16 Length;
    UINT16 Checksum;
} WINDIVERT_UDPHDR, *PWINDIVERT_UDPHDR;

#ifndef WINDIVERT_KERNEL

/*
 * Flags for WinDivertHelperCalcChecksums()
 */
#define WINDIVERT_HELPER_NO_IP_CHECKSUM                     1
#define WINDIVERT_HELPER_NO_ICMP_CHECKSUM                   2
#define WINDIVERT_HELPER_NO_ICMPV6_CHECKSUM                 4
#define WINDIVERT_HELPER_NO_TCP_CHECKSUM                    8
#define WINDIVERT_HELPER_NO_UDP_CHECKSUM                    16

/*
 * Parse IPv4/IPv6/ICMP/ICMPv6/TCP/UDP headers from a raw packet.
 */
extern WINDIVERTEXPORT BOOL WinDivertHelperParsePacket(
    __in        PVOID pPacket,
    __in        UINT packetLen,
    __out_opt   PWINDIVERT_IPHDR *ppIpHdr,
    __out_opt   PWINDIVERT_IPV6HDR *ppIpv6Hdr,
    __out_opt   PWINDIVERT_ICMPHDR *ppIcmpHdr,
    __out_opt   PWINDIVERT_ICMPV6HDR *ppIcmpv6Hdr,
    __out_opt   PWINDIVERT_TCPHDR *ppTcpHdr,
    __out_opt   PWINDIVERT_UDPHDR *ppUdpHdr,
    __out_opt   PVOID *ppData,
    __out_opt   UINT *pDataLen);

/*
 * Parse an IPv4 address.
 */
extern WINDIVERTEXPORT BOOL WinDivertHelperParseIPv4Address(
    __in        const char *addrStr,
    __out_opt   UINT32 *pAddr);

/*
 * Parse an IPv6 address.
 */
extern WINDIVERTEXPORT BOOL WinDivertHelperParseIPv6Address(
    __in        const char *addrStr,
    __out_opt   UINT32 *pAddr);

/*
 * Calculate IPv4/IPv6/ICMP/ICMPv6/TCP/UDP checksums.
 */
extern WINDIVERTEXPORT UINT WinDivertHelperCalcChecksums(
    __inout     PVOID pPacket, 
    __in        UINT packetLen,
    __in_opt    PWINDIVERT_ADDRESS pAddr,
    __in        UINT64 flags);

/*
 * Check the given filter string.
 */
extern WINDIVERTEXPORT BOOL WinDivertHelperCheckFilter(
    __in        const char *filter,
    __in        WINDIVERT_LAYER layer,
    __out_opt   const char **errorStr,
    __out_opt   UINT *errorPos);

/*
 * Evaluate the given filter string.
 */
extern WINDIVERTEXPORT BOOL WinDivertHelperEvalFilter(
    __in        const char *filter,
    __in        WINDIVERT_LAYER layer,
    __in        PVOID pPacket,
    __in        UINT packetLen,
    __in        PWINDIVERT_ADDRESS pAddr);

#endif      /* WINDIVERT_KERNEL */

#ifdef __cplusplus
}
#endif

#endif      /* __WINDIVERT_H */
