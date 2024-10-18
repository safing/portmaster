# Protocol

Defines protocol that communicates with `kextinterface` / Portmaster.

The crate implements simple binary protocol. The communications is designed to be concurrent stream of packets.
Input and output work independent of each other.
 - Pormtaster can read multiple info packets from the queue with single read request.
 - Portmaster can write one command packet to the kernel extension with single write request.

## Info: Kext -> Portmaster

Info is a packet that sends information/events from the kernel extension to portmaster.
For example: `new connection`, `end of connection`, `bandwidth stats` ... check `info.rs` for full list.

The Info packet contains a header that is 5 bytes
```
0                   1                   2                   3                   4
0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|   Info Type   |                            Length                             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```
> Note that one tick mark represents one bit position.

The header is followed by the info data.


## Command: Portmaster -> Kext

Command is a packet that portmaster sends to the kernel extension.
For example: `verdict response`, `shutdown`, `get logs` ... check `command.rs` for full list.

The header of the command packet is 1 byte
```
0 1 2 3 4 5 6 7
+-+-+-+-+-+-+-+-+
| Command Type  |
+-+-+-+-+-+-+-+-+
```
> Note that one tick mark represents one bit position.

Rest of the packet will be the payload of the command (some commands don't contain payload just the command type).

