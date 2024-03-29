package captain

import (
	"net"
	"sync"
)

var (
	exceptionLock sync.Mutex
	exceptIPv4    net.IP
	exceptIPv6    net.IP
)

func setExceptions(ipv4, ipv6 net.IP) {
	exceptionLock.Lock()
	defer exceptionLock.Unlock()

	exceptIPv4 = ipv4
	exceptIPv6 = ipv6
}

// IsExcepted checks if the given IP is currently excepted from the SPN.
func IsExcepted(ip net.IP) bool {
	exceptionLock.Lock()
	defer exceptionLock.Unlock()

	return ip.Equal(exceptIPv4) || ip.Equal(exceptIPv6)
}
