# There and back again, a packets tale.

An explanation on the complete path of the packet from entering to the exit of the kernel extension.

## Entry

The packet entry point depends on the packet and the internal windows filter state:   

- First packet of outbound connection -> AleAuthConnect Layer
- First packet of inbound connection -> InboundIppacket Layer

## ALE layer

Each defined ALE layer has a filter linked to it. This filter has a state.  
When a decision is made to block or permit a connection it will be saved to the filter state.
The only way to update the decision in a filter is to clear the whole state and apply the decision for the next packet of each connection.

### First packet

For outgoing connections this logic fallows:
  - Packet enters in one of the ALE layer
  - Packet is TCP or UDP
    1. Save and absorb packet.
    2. Send an event to Portmaster. 
    2. Create a cache entry.
  - If Packet is not TCP/UDP forward to packet layer

For incoming connection this logic fallow:
  - Packet enter in one of the Packet layer:
    1. Save packet and absorb.
    2. Send an event to Portmaster. 
    2. Create a cache entry if the protocol is TCP or UDP.
    3. Wait for Portmasters decision.


If more packets arrive before Portmaster returns a decision, packet will be absorbed and another event will be sent.
For Outgoing connection this will happen in ALE layer.
For Incoming connection this will happen in Packet layer. 

### Pormtaster returns a verdict for the connection

Connection cache will be updated and the packet will be injected.
The next steps depend of the direction of the packet and the verdict

* Permanent Verdict / Outgoing connection
  - Allow / Block / Drop directly in the ALE layer. For Block and Drop packet layer will not see the rest of the packet in the connection.
* Temporary Verdict / Outgoing connection
  - Always Allow - this connections are solely handled by the packet layer. (This is true only for outgoing connections) 

* Permanent or Temporary Verdict / Incoming connection
  - Allow / Block / Drop. Handled by the Packet layer

> There is no defined ALE layers for inbound connection. Inbound packets are handed compactly by the packet layer 

Fallowing specifics apply to the ALE layer:  
1. Connections with flag `reauthorize == false` are special. When the flag is `false` that means that a applications is calling a function `connect()` or `accept()` for a connection. This is a special case because we control the result of the function, telling the application that it's allowed or not allowed to continue with the connection. Since we are making request to Portmaster we need to take longer time. This is done with pending the packet. This allows the kernel extension to pause the event and continue when it has the verdict. See `ale_callouts.rs -> save_packet()` function.
2. If packet payload is present it is from the transport layer.


## Packet layer

The logic for the packet is split in two:

### TCP or UDP protocols

The packet layer will not process packets that miss a cache entry:  
- Incoming packet: it will forward it to the ALE layer.
- Outgoing packet: this is treated as invalid state since ALE should be the entry for the packets. If it happens the packet layer will create a request to Portmaster for it.

For packets with a cache entry:
- Permanent Verdict: apply the verdict.
- Redirect Verdict: copy the packet, modify and inject. Drop the original packet.
- Temporary verdict: send request to Portmaster.

After portmaster returns the verdict for the packet. If its allowed it will be modified (if needed) and injected everything else will be dropped.
The packet layer will permit all injected packets.

### Not TCP or UDP protocols -> ICMP, IGMP ...

Does packets are treated as with temporary verdict. There will be no cache entry for them.
Every packet will be send to Portmaster for a decision and re-injected if allowed.

## Connection Cache

It holds information for all TCP and UDP connections. Local and destination ip addresses and ports, verdict, protocol, process id
It also holds last active time and end time.  

Cache entry is removed automatically 1 minute after an end state has been set or after 10 minutes of inactivity.  

End stat is set by Endpoint layers or Resource release layers.