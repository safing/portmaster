# There and back again: a packet's path

This document describes how packets move through the Windows kernel extension and where Portmaster decisions are applied.

## Entry layers

The first callout depends on packet direction and protocol:

- Outbound TCP enters `ALE_AUTH_CONNECT` before it reaches the outbound IP packet layer.
- Outbound UDP is registered at `ALE_AUTH_CONNECT` to capture its PID, but is authorized at the outbound IP packet layer so an ALE pend cannot corrupt the application's send result.
- An outbound TCP or UDP NBL injected by another WFP driver is also authorized at the IP packet layer. Its synthetic ALE flow can expose a System PID and one raw endpoint shared by many tuples, neither of which is valid application identity.
- Inbound traffic first reaches the inbound IP packet layer.
  - A new TCP or UDP tuple is permitted upward to `ALE_AUTH_RECV_ACCEPT`.
  - A cached live inbound tuple is also permitted while its verdict is still `Undecided` and its PID is unknown (`0`) or System (`4`).
  - Retained ended entries are never used as inbound packet policy: the tuple may already belong to a new flow that must receive fresh ALE attribution and authorization.
  - Once a verdict is cached or a concrete application PID is known, the packet layer handles it; received packets belonging to outbound connections remain on this path as before for temporary verdicts and reverse redirect rewriting.
- ICMP and other protocols are handled at the IP packet layer.

## ALE authorization

Two pairs of terminating, resettable callouts implement connection-level authorization:

- `ALE_AUTH_CONNECT_V4/V6` for outbound connections.
- `ALE_AUTH_RECV_ACCEPT_V4/V6` for inbound TCP accepts and the first inbound UDP packet from a unique remote tuple.

Both paths build the same connection key from WFP's fixed fields and use the process ID supplied in ALE metadata. This means an inbound connection is created with its owning PID instead of the PID 0 that the IP packet layer had to use.

### New connection

For a new TCP/UDP connection:

1. Build the connection key from ALE fields.
2. Save the classify operation and, when packet data exists, clone it.
   - At inbound receive/accept the NBL data offset is at the transport payload. The driver retreats by WFP's IP-header size plus transport-header size before cloning, so the clone starts at the IP header as required by both transport-receive and loopback network-send injection.
   - The event sent to Portmaster exposes the clone from the transport header and reports payload layer 4.
3. Pend an initial ALE operation with `FwpsPendOperation0`. A reauthorization cannot be pended, so that path blocks the current indication and later resets the resettable filters.
4. Add an `Undecided` cache entry before publishing the request to Portmaster.
5. Block and absorb the current indication while Portmaster decides. This is a hard WFP block: the callout clears `FWPS_RIGHT_ACTION_WRITE`, so a lower-priority filter cannot permit the original packet while its clone is pending.

If another indication arrives while the cache entry is still `Undecided`, it is saved under a separate packet ID and another event is queued. An outgoing indication can itself contain several `NET_BUFFER` packets; the packet cache splits that batch into one event and one verdict ID per packet before publishing it.

Self-injected packets normally bypass ALE authorization to prevent reinjection loops. One narrow exception preserves both application endpoints of loopback TCP and UDP traffic: an outbound packet-layer verdict can reinject the first packet before the server has reached `ALE_AUTH_RECV_ACCEPT`. That first inbound copy still follows the normal receive/accept path, so the listener PID, endpoint identity, and an independent server-side verdict are recorded; TCP also records its provisional child endpoint. Registration precedes reinjection of the verdict clone, so the next self-injected copy finds cached receive/accept state and is permitted without another request.

### Applying a verdict

- `Accept` and `PermanentAccept` complete the ALE operation and reinject a saved packet when one exists.
- For ordinary inbound receive/accept, the same NBL is supplied to `FwpsCompleteOperation0` before `FwpsInjectTransportReceiveAsync0` is called. Inbound TCP and UDP loopback use `FwpsInjectNetworkSendAsync0` after the same ALE completion instead: Windows accepts the complete IP packet on the loopback send path but completes transport-receive injection of either protocol with `STATUS_DATA_NOT_ACCEPTED`; a TCP network-receive comparison fails with the same status. For TCP the rejected clone is the initial SYN; the sender retransmits it after approximately one second and the cached verdict permits that copy, masking the loss as connection latency. Network-send injection delivers the original SYN immediately. The matching injection handle is checked when either clone is indicated again; once any required server-side receive/accept authorization exists, self-injected copies are permitted without another request.
- A successful `FwpsInject*Async` return transfers packet ownership but reports only submission success. Every network and transport completion callback therefore checks the final NBL status; a negative status is copied with its exact injection path into the driver's nonpaged log ring before the callback releases the packet. `kext_monitor` receives that error through the existing log-event stream.
- Block/drop/failed verdicts complete an ALE pend without injecting the clone. Every block returned by a Portmaster callout is hard (`FWPS_RIGHT_ACTION_WRITE` is cleared), including silent `BLOCK + ABSORB` paths. Ordinary permits remain overridable; only the explicit Portmaster-owner bypass turns its permit into a hard permit.
- Each pending TCP/UDP packet carries the exact connection-cache instance ID that queued it. An outgoing WFP indication containing multiple `NET_BUFFER` packets is expanded under the same connection-liveness guard: every packet receives its own cache entry, userspace event, and verdict ID. Publication revalidates that instance and queues the packet and its event while holding the connection map's shared guard; native lifecycle end takes the exclusive guard. A request is therefore visible before its matching `END`, or rejected and completed as blocked if the connection ended first. A delayed verdict for a packet that still depends on its connection is discarded if that instance has ended, so tuple reuse cannot apply the old decision to a replacement connection. Once a lifecycle end is committed, pending packets that still depend on the live endpoint are removed and completed as blocked. An outbound UDP packet-layer clone is retained under its existing request ID but detached from the ended instance: the socket send completed before the network-layer packet was absorbed, so endpoint closure must not revoke that accepted datagram. A later accept verdict injects only the saved clone and cannot update an ended or replacement connection. This is per-packet lifetime handling; it neither coalesces userspace events nor serializes independent reinjections.
- TCP endpoint closure has an additional ordering requirement. If packet decisions for that flow are queued or currently being applied, the driver pends `ALE_ENDPOINT_CLOSURE` with `FwpsPendClassify0` instead of retiring their IDs. Requests published while the closure is waiting join the same set. On loopback, WFP reports both peers' IP packets as outbound, so the server-side closure also waits for the reversed client tuple; this keeps the endpoint alive while a peer FIN is reinjected and while the stack produces its final ACK. The last completed verdict claims the closure under the connection map's exclusive guard, marks only the exact instance ended, emits `END`, and calls `FwpsCompleteClassify0`. A concurrently reused tuple therefore cannot join the old closure or receive its verdict. This is lifecycle synchronization, not a retired-ID diagnostic exception: the affected IDs remain valid because their packets still have protocol work to perform.
- Completing or reinjecting a pended TCP operation can race with endpoint closure and produce a final reauthorization after its exact cache generation has ended. That indication belongs to the just-closed endpoint, but its optional endpoint metadata may already be incomplete. If the exact ended generation is still retained for late packets, the driver applies its saved verdict without registering or tracking another live generation; otherwise normal reauthorization handling is unchanged.
- Permanent cached verdicts are applied directly on later ALE classifications.
- Updating a cached verdict resets the resettable ALE filters so existing ALE-authorized flows are reauthorized against the new value.

### Flow-established attribution

The inspection callouts at `ALE_FLOW_ESTABLISHED_V4/V6` refresh the process ID of an existing TCP or UDP cache entry. TCP reaches this layer after its three-way handshake; UDP reaches it immediately after `ALE_AUTH_CONNECT` or `ALE_AUTH_RECV_ACCEPT` authorizes the first packet for a remote tuple. Required fixed fields are type-checked before the five-tuple is read, because the filters also see non-TCP/UDP protocols and generic flows that cannot be matched safely to an exact cache key.

For TCP, authorization records each new cache generation with the transport and parent endpoint handles supplied by WFP. For an accepted inbound connection, `ALE_AUTH_RECV_ACCEPT` can expose a provisional transport endpoint while `ALE_FLOW_ESTABLISHED` exposes the child endpoint later used by `ALE_ENDPOINT_CLOSURE`; the driver resolves that transition through `(tuple, parent endpoint, live instance_id)` and replaces the provisional handle. Endpoint closure therefore consumes the established handle and calls only the exact-instance end operation, so a delayed closure cannot end a replacement connection that reused the tuple. An authorization cannot create a new TCP cache generation without an endpoint handle because its later lifetime could not be correlated safely. Reauthorization of an already cached connection may omit endpoint metadata and reuses the saved identity.

For both TCP and UDP, outbound packet reinjection can produce an additional raw flow with the application's tuple and a different endpoint. The driver checks the NBL's injection state and skips any such `ALE_FLOW_ESTABLISHED` indication before endpoint or tuple resolution. Its own reinjection is a loop-prevention case; another driver's synthetic flow is skipped because a WinDivert-style injector can expose one raw endpoint for many unrelated tuples. Neither flow can therefore interfere with application attribution or lifecycle tracking. For UDP, the driver associates a context containing the remote tuple with the real WFP flow via `FwpsFlowAssociateContext`. WFP invokes `flowDeleteFn` when that peer flow is actually reclaimed, and the callback emits the connection-end event. Windows documents a 60-second default idle lifetime, but current Windows versions can defer idle reclamation substantially; the driver waits for the native lifecycle signal for every tracked connection. When WFP supplies a transport endpoint handle at authorization, the driver associates the exact cache instance with that handle. Flow establishment reuses and validates this identity and may promote an exact packet-layer fallback when it acquires a real endpoint or flow context. Windows emits `ALE_ENDPOINT_CLOSURE` once for the UDP socket, not once per remote peer, so socket closure ends both associated tuples and untracked fallbacks matching its local endpoint.

The cache's PID precedence rules ignore PID 0, prevent System (PID 4) from replacing a concrete application, and allow a concrete application PID to replace less reliable attribution. This repairs packet-layer fallback entries and refreshes the UDP owner without changing the cached verdict.

`ALE_FLOW_ESTABLISHED` is not emitted again for successful reauthorization, so this monitor cannot attribute a flow that was already active when the driver loaded.

`ALE_AUTH_RECV_ACCEPT` is a connection/remote-tuple authorization layer, not a per-packet layer. It attributes and performs the initial authorization of inbound TCP/UDP connections. The packet layer permits a missing tuple upward to that layer and keeps permitting a cached `Undecided` tuple only while its PID is unknown (`0`) or System (`4`). Once a verdict or concrete application PID is cached, subsequent packet indications are processed on the packet path instead of being permitted unconditionally.

## IP packet layer

The packet callouts still see every network-layer indication. They defer missing inbound TCP/UDP connections, plus undecided connections with PID 0 or 4, to ALE; all other cached states are processed on the packet path.

### TCP and UDP

- An inbound tuple with no live cache entry is permitted to `ALE_AUTH_RECV_ACCEPT`; all retained ended history is ignored so tuple reuse cannot inherit the previous flow's verdict.
- A cached live inbound connection is also permitted while its verdict is `Undecided` and its PID is `0` or `4`.
- Once a verdict or concrete application PID is cached, the existing packet behavior applies regardless of the cached connection direction:
  - permanent accept/block/drop is applied immediately;
  - temporary verdicts create a Portmaster request;
  - redirect verdicts clone, rewrite, recalculate checksums, inject the replacement, and absorb the original.
- An outbound NBL injected by another driver uses only live cache state. While the application connection exists, its PID and verdict remain authoritative. After that endpoint closes, retained history is deliberately ignored and the packet is sent as a stateless request attributed to the user-space injector through `PsGetCurrentProcessId`; no permanent verdict or endpoint identity is cached for a later tuple reuse.
- An outbound tuple unexpectedly missing from the cache uses the defensive packet-level fallback. Because the packet layer has no transport endpoint identity, this outbound entry is marked untracked: a later real endpoint/flow association promotes it, socket closure can end it by local endpoint, and periodic cleanup ends it after one inactive minute if no native identity ever appears.

### Other protocols

ICMP, IGMP, and protocols without TCP/UDP connection state are normally treated as temporary packet decisions. Each packet is cloned, sent to Portmaster, and reinjected only when allowed. ICMP echo requests use a short-lived identifier cache to associate replies with the sending process. ICMP Destination Unreachable / Port Unreachable packets (ICMPv4 type 3/code 3 and ICMPv6 type 1/code 4) are permitted directly in both directions because they have no user-space owner to query.

### Fragments and injected packets

Individual IP fragments are permitted until WFP presents the reassembled datagram, which has a complete transport header and can be keyed safely. Packets injected with this driver's own network or transport handle are detected and permitted to prevent reinjection loops; packets injected by another driver remain subject to policy as described above. The IP packet layer queries both driver handles and gives self-injection precedence across the pair: WFP reports an ALE clone injected through the transport handle as `InjectedByOther` when the same NBL is queried only against the network handle.

## Connection cache

The cache stores TCP and UDP keys, direction, process ID, verdict, redirect state, activity timestamps, and whether exact native lifecycle identity has been established. Native WFP flow deletion and endpoint closure mark tracked connections as ended and emit their lifecycle event. `ALE_RESOURCE_RELEASE` is intentionally not used for this state because it pairs with resource assignment rather than authorization. A closure with an unknown UDP endpoint handle additionally ends only untracked fallbacks matching its local endpoint; it cannot end a tracked replacement by tuple. Periodic cleanup ends an outbound untracked fallback after one inactive minute, providing a bound when WFP never supplied any endpoint or flow identity. Inbound fallbacks wait for socket closure because an already-authorized ALE flow must continue to find its cached policy. Cleanup removes ended entries after their one-minute late-packet grace period and shrinks a surviving per-port vector when its high-water capacity is at least four times the live set. During that interval the outbound packet path may use an ended entry for traffic already in flight, but the inbound path never does because the tuple may already identify a new flow awaiting ALE authorization. Tracked live TCP and UDP entries remain until a native lifecycle signal ends them, the cache is explicitly cleared, or the driver unloads.
