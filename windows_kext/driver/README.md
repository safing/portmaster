# Driver

This is the entry point of the Kernel extension.

## Quick overview

`entry.rs`:
This file contains the entry point and calling all the needed initialization code. 
- Setting up the driver object
- Allocating global state

`fn driver_entry()` -> entry pointer of the driver.

`device.rs`:  
Holds the global state of the driver.  
Initialization: Setting up global state, Filter engine and callouts.  

Portmaster communication:
The communication happens concurrently with the File read/write API.
That means when Pormtaster sends a command the kernel extension will start to process it and queue the result in the `IOQueue`.

`fn read()` -> called on read request from Portmaster  
- `IOQueue` holds all the events queued for Portmaster.

Blocks until there is a element that can be poped or shutdown request is sent from Portmaster.
If there is more then one event in the queue it will write as much as it can in the supplied buffer.

`fn write()` -> called on write request from Portmaster.  
Used when Portmaster wants to send a command to kernel extension.
Verdict Response, GetLogs ... (see `protocol` for list of all the commands)


## Callouts

`callouts.rs` -> defines the list of all used callouts in the kernel extension. 

ALE (Application Layer Enforcement)
https://learn.microsoft.com/en-us/windows/win32/fwp/application-layer-enforcement--ale-

### ALE Auth

Connection level filtering. It will make a decision based on the first packet of a connection. Works together with the packet layer to provide firewall functionality.
- **AleLayerOutboundV4**  
- **AleLayerInboundV4**  
- **AleLayerOutboundV6**  
- **AleLayerInboundV6**  


### ALE endpoint / resource assignment and release

Used to listen for event when connection has ended. Does no filtering.
- **AleEndpointClosureV4, AleEndpointClosureV6** - Triggered when connection to an endpoint has ended. Usually only TCP is triggered.  The triggered connection will be marked for deletion.

- **AleResourceAssignmentV4, AleResourceAssignmentV6** -> only for logging (not used)
- AleResourceReleaseV4, AleResourceReleaseV6 -> Triggered when port is release from an application. The triggered connection/s will be marked for deletion.

### Stream layer  

This layer works on the application OSI layer. Meaning that only the payload of the TCP/UDP connection will be available.
It is used for bandwidth monitoring. This functionality is completely separate from the rest of the system so it can be disabled or enabled without affect anything else. 

- **StreamLayerV4, StreamLayerV6** -> For TCP connections 
- **DatagramDataLayerV4, DatagramDataLayerV6** -> For UDP connections


### Packet layer

This layer handled each packet on the network OSI layer. Works together with ALE Auth layer to provide firewall functionality.
- **IPPacketOutboundV4, IPPacketOutboundV6** -> Triggered on every outbound packet.
- **IPPacketInboundV4, IPPacketInboundV6** -> Triggered on every inbound packet.
