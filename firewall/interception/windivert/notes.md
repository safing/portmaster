# Notes

## Interception

- use windivert DLL
- cgo or loadDLL?

- netfilter exmaple: https://reqrypt.org/samples/netfilter.html
- v1.4 docs: https://reqrypt.org/windivert-doc.html#divert_recv_ex
- source: https://github.com/basil00/Divert

- other GO package wrapping this: https://github.com/clmul/go-windivert/blob/master/divert_windows.go

## Packet/Process Attribution

- use Iphlpapi.dll
  - GetExtendedTcpTable
  - GetOwnerModuleFromTcpEntry
  - GetExtendedUdpTable
  - GetOwnerModuleFromUdpEntry
  - for generic IP?

## Helpful resources

Calling Windows APIs
https://stackoverflow.com/questions/33709033/golang-how-can-i-call-win32-api-without-cgo#33709631

GetExtendedTcpTable (from Iphlpapi.dll)
https://msdn.microsoft.com/en-us/library/windows/desktop/aa365928(v=vs.85).aspx

GetUdpTable Example
https://stackoverflow.com/questions/49167311/how-to-convert-uintptr-to-go-struct
