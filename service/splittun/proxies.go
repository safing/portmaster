package splittun

import (
	"context"
	"fmt"
	"net"
	"sync"

	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/service/splittun/proxy"
)

var (
	proxiesLocker sync.RWMutex
	manager       *mgr.Manager
	tcp4Proxy     *proxy.TCPProxy
	tcp6Proxy     *proxy.TCPProxy
	udp4Proxy     *proxy.UDPProxy
	udp6Proxy     *proxy.UDPProxy
)

type proxiedEgressFinder interface {
	FindProxiedEgressConnection(destIP net.IP, destPort uint16) []*proxy.ConnContext
}

func IsProxiedConnectionInfo(connInfo *network.Connection) bool {
	if connInfo == nil || connInfo.Entity == nil || connInfo.LocalIP == nil || connInfo.Entity.IP == nil {
		return false
	}

	proxiesLocker.RLock()
	var finder proxiedEgressFinder

	switch connInfo.IPProtocol {
	case packet.TCP:
		switch connInfo.IPVersion {
		case packet.IPv4:
			finder = tcp4Proxy
		case packet.IPv6:
			finder = tcp6Proxy
		}
	case packet.UDP:
		switch connInfo.IPVersion {
		case packet.IPv4:
			finder = udp4Proxy
		case packet.IPv6:
			finder = udp6Proxy
		}
	}

	if finder == nil {
		proxiesLocker.RUnlock()
		return false
	}

	// TODO: The current FindProxiedEgressConnection path allocates a slice on each lookup in cache.go.
	//		 Consider adding a HasProxiedEgressConnection boolean method in the cache/proxy layer
	//		 to avoid allocating a result slice when only existence is needed. This can reduce GC pressure under load.
	isProxied := len(finder.FindProxiedEgressConnection(connInfo.Entity.IP, connInfo.Entity.Port)) > 0
	proxiesLocker.RUnlock()
	return isProxied
}

func startProxies(mgr *mgr.Manager) error {
	var (
		tcp4 *proxy.TCPProxy
		tcp6 *proxy.TCPProxy
		udp4 *proxy.UDPProxy
		udp6 *proxy.UDPProxy
		err  error
	)

	_ = stopProxies()

	// Ensure any partially-started proxies are shut down if we return an error.
	var startErr error
	defer func() {
		if startErr != nil {
			ctx := mgr.Ctx()
			if tcp4 != nil {
				tcp4.Shutdown(ctx)
			}
			if udp4 != nil {
				udp4.Shutdown(ctx)
			}
			if tcp6 != nil {
				tcp6.Shutdown(ctx)
			}
			if udp6 != nil {
				udp6.Shutdown(ctx)
			}
		}
	}()

	tcp4, err = proxy.NewTCPProxy(fmt.Sprintf("0.0.0.0:%d", SplitTunPort), "tcp4", proxyDecider, &proxyLogger{prefix: "tcp4", mgr: mgr})
	if err != nil {
		startErr = fmt.Errorf("failed to start TCPv4 proxy: %w", err)
		return startErr
	}
	udp4, err = proxy.NewUDPProxy(fmt.Sprintf("0.0.0.0:%d", SplitTunPort), "udp4", proxyDecider, &proxyLogger{prefix: "udp4", mgr: mgr})
	if err != nil {
		startErr = fmt.Errorf("failed to start UDPv4 proxy: %w", err)
		return startErr
	}

	if netenv.IPv6Enabled() {
		tcp6, err = proxy.NewTCPProxy(fmt.Sprintf("[::]:%d", SplitTunPort), "tcp6", proxyDecider, &proxyLogger{prefix: "tcp6", mgr: mgr})
		if err != nil {
			startErr = fmt.Errorf("failed to start TCPv6 proxy: %w", err)
			return startErr
		}
		udp6, err = proxy.NewUDPProxy(fmt.Sprintf("[::]:%d", SplitTunPort), "udp6", proxyDecider, &proxyLogger{prefix: "udp6", mgr: mgr})
		if err != nil {
			startErr = fmt.Errorf("failed to start UDPv6 proxy: %w", err)
			return startErr
		}
	}

	proxiesLocker.Lock()
	manager = mgr
	tcp4Proxy = tcp4
	tcp6Proxy = tcp6
	udp4Proxy = udp4
	udp6Proxy = udp6
	proxiesLocker.Unlock()

	return nil
}

func stopProxies() error {
	proxiesLocker.Lock()
	mgr := manager
	tcp4 := tcp4Proxy
	tcp6 := tcp6Proxy
	udp4 := udp4Proxy
	udp6 := udp6Proxy
	tcp4Proxy = nil
	tcp6Proxy = nil
	udp4Proxy = nil
	udp6Proxy = nil
	proxiesLocker.Unlock()

	var ctx context.Context
	if mgr != nil {
		ctx = mgr.Ctx()
	} else {
		ctx = context.Background()
	}

	if tcp4 != nil {
		tcp4.Shutdown(ctx)
	}
	if tcp6 != nil {
		tcp6.Shutdown(ctx)
	}
	if udp4 != nil {
		udp4.Shutdown(ctx)
	}
	if udp6 != nil {
		udp6.Shutdown(ctx)
	}

	return nil
}

// PROXY LOGGER WRAPPER
type proxyLogger struct {
	prefix string
	mgr    *mgr.Manager
}

func (l proxyLogger) Debugf(format string, args ...interface{}) {
	l.mgr.Debug(l.getLogLine(format, args...))
}
func (l proxyLogger) Warnf(format string, args ...interface{}) {
	l.mgr.Warn(l.getLogLine(format, args...))
}
func (l proxyLogger) Infof(format string, args ...interface{}) {
	l.mgr.Info(l.getLogLine(format, args...))
}
func (l proxyLogger) Errorf(format string, args ...interface{}) {
	l.mgr.Error(l.getLogLine(format, args...))
}
func (l proxyLogger) getLogLine(format string, args ...interface{}) string {
	return fmt.Sprintf("%s: "+format, append([]interface{}{l.prefix}, args...)...)
}
