package nameserver

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"
	"sync/atomic"

	"github.com/miekg/dns"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/compat"
	"github.com/safing/portmaster/service/firewall"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
)

type NameServer struct {
	mgr      *mgr.Manager
	instance instance

	states *mgr.StateMgr
}

func (ns *NameServer) Manager() *mgr.Manager {
	return ns.mgr
}

func (ns *NameServer) States() *mgr.StateMgr {
	return ns.states
}

func (ns *NameServer) Start() error {
	return start()
}

func (ns *NameServer) Stop() error {
	return stop()
}

var (
	stopListeners     bool
	stopListener1     func() error
	stopListener2     func() error
	stopListenersLock sync.Mutex

	eventIDConflictingService = "nameserver:conflicting-service"
	eventIDListenerFailed     = "nameserver:listener-failed"
)

func prep() error {
	return registerConfig()
}

func start() error {
	if err := registerMetrics(); err != nil {
		return err
	}

	// Get listen addresses.
	ip1, ip2, port, err := getListenAddresses(nameserverAddressConfig())
	if err != nil {
		return fmt.Errorf("failed to parse nameserver listen address: %w", err)
	}

	// Tell the compat module where we are listening.
	compat.SetNameserverListenIP(ip1)

	// Get own hostname.
	hostname, err = os.Hostname()
	if err != nil {
		log.Warningf("nameserver: failed to get hostname: %s", err)
	}
	hostname += "."

	// Start listener(s).
	if ip2 == nil {
		// Start a single listener.
		startListener(ip1, port, true)

		// Set nameserver matcher in firewall to fast-track dns queries.
		if ip1.Equal(net.IPv4zero) || ip1.Equal(net.IPv6zero) {
			// Fast track dns queries destined for any of the local IPs.
			return firewall.SetNameserverIPMatcher(func(ip net.IP) bool {
				dstIsMe, err := netenv.IsMyIP(ip)
				if err != nil {
					log.Warningf("nameserver: failed to check if IP %s is local: %s", ip, err)
				}
				return dstIsMe
			})
		}
		return firewall.SetNameserverIPMatcher(func(ip net.IP) bool {
			return ip.Equal(ip1)
		})
	}

	// Dual listener.
	startListener(ip1, port, true)
	startListener(ip2, port, false)

	// Fast track dns queries destined for one of the listener IPs.
	return firewall.SetNameserverIPMatcher(func(ip net.IP) bool {
		return ip.Equal(ip1) || ip.Equal(ip2)
	})
}

func startListener(ip net.IP, port uint16, first bool) {
	// Start DNS server as service worker.
	module.mgr.Go("dns resolver", func(ctx *mgr.WorkerCtx) error {
		// Create DNS server.
		dnsServer := &dns.Server{
			Addr: net.JoinHostPort(
				ip.String(),
				strconv.Itoa(int(port)),
			),
			Net:     "udp",
			Handler: dns.HandlerFunc(handleRequestAsWorker),
		}

		// Register stop function.
		func() {
			stopListenersLock.Lock()
			defer stopListenersLock.Unlock()

			// Check if we should stop
			if stopListeners {
				_ = dnsServer.Shutdown()
				dnsServer = nil
				return
			}

			// Register stop function.
			if first {
				stopListener1 = dnsServer.Shutdown
			} else {
				stopListener2 = dnsServer.Shutdown
			}
		}()

		// Check if we should stop.
		if dnsServer == nil {
			return nil
		}

		// Resolve generic listener error, if primary listener.
		if first {
			module.states.Remove(eventIDListenerFailed)
		}

		// Start listening.
		log.Infof("nameserver: starting to listen on %s", dnsServer.Addr)
		err := dnsServer.ListenAndServe()
		if err != nil {
			// Stop worker without error if we are shutting down.
			if module.mgr.IsDone() {
				return nil
			}
			log.Warningf("nameserver: failed to listen on %s: %s", dnsServer.Addr, err)
			handleListenError(err, ip, port, first)
		}
		return err
	})
}

func handleListenError(err error, ip net.IP, port uint16, primaryListener bool) {
	var n *notifications.Notification

	// Create suffix for secondary listener
	var secondaryEventIDSuffix string
	if !primaryListener {
		secondaryEventIDSuffix = "-secondary"
	}

	// Find a conflicting service.
	cfProcess := findConflictingProcess(ip, port)
	if cfProcess != nil {
		// Report the conflicting process.

		// Build conflicting process description.
		var cfDescription string
		cfName, err := cfProcess.Name()
		if err == nil && cfName != "" {
			cfDescription = cfName
		}
		cfExe, err := cfProcess.Exe()
		if err == nil && cfDescription != "" {
			if cfDescription != "" {
				cfDescription += " (" + cfExe + ")"
			} else {
				cfDescription = cfName
			}
		}

		// Notify user about conflicting service.
		n = notifications.Notify(&notifications.Notification{
			EventID: eventIDConflictingService + secondaryEventIDSuffix,
			Type:    notifications.Error,
			Title:   "Conflicting DNS Software",
			Message: "Restart Portmaster after you have deactivated or properly configured the conflicting software: " +
				cfDescription,
			ShowOnSystem: true,
			AvailableActions: []*notifications.Action{
				{
					Text:    "Open Docs",
					Type:    notifications.ActionTypeOpenURL,
					Payload: "https://docs.safing.io/portmaster/install/status/software-compatibility",
				},
			},
		})
	} else {
		// If no conflict is found, report the error directly.
		n = notifications.Notify(&notifications.Notification{
			EventID: eventIDListenerFailed + secondaryEventIDSuffix,
			Type:    notifications.Error,
			Title:   "Secure DNS Error",
			Message: fmt.Sprintf(
				"The internal DNS server failed. Restart Portmaster to try again. Error: %s",
				err,
			),
			ShowOnSystem: true,
		})
	}

	// Attach error to module, if primary listener.
	if primaryListener {
		n.SyncWithState(module.states)
	}
}

func stop() error {
	stopListenersLock.Lock()
	defer stopListenersLock.Unlock()

	// Stop listeners.
	stopListeners = true
	if stopListener1 != nil {
		if err := stopListener1(); err != nil {
			log.Warningf("nameserver: failed to stop listener1: %s", err)
		}
	}
	if stopListener2 != nil {
		if err := stopListener2(); err != nil {
			log.Warningf("nameserver: failed to stop listener2: %s", err)
		}
	}

	return nil
}

func getListenAddresses(listenAddress string) (ip1, ip2 net.IP, port uint16, err error) {
	// Split host and port.
	ipString, portString, err := net.SplitHostPort(listenAddress)
	if err != nil {
		return nil, nil, 0, fmt.Errorf(
			"failed to parse address %s: %w",
			listenAddress,
			err,
		)
	}

	// Parse the IP address. If the want to listen on localhost, we need to
	// listen separately for IPv4 and IPv6.
	if ipString == "localhost" {
		ip1 = net.IPv4(127, 0, 0, 17)
		if netenv.IPv6Enabled() {
			ip2 = net.IPv6loopback
		} else {
			log.Warningf("nameserver: no IPv6 stack detected, disabling IPv6 nameserver listener")
		}
	} else {
		ip1 = net.ParseIP(ipString)
		if ip1 == nil {
			return nil, nil, 0, fmt.Errorf(
				"failed to parse IP %s from %s",
				ipString,
				listenAddress,
			)
		}
	}

	// Parse the port.
	port64, err := strconv.ParseUint(portString, 10, 16)
	if err != nil {
		return nil, nil, 0, fmt.Errorf(
			"failed to parse port %s from %s: %w",
			portString,
			listenAddress,
			err,
		)
	}

	return ip1, ip2, uint16(port64), nil
}

var (
	module     *NameServer
	shimLoaded atomic.Bool
)

// New returns a new NameServer module.
func New(instance instance) (*NameServer, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}
	m := mgr.New("NameServer")
	module = &NameServer{
		mgr:      m,
		instance: instance,

		states: mgr.NewStateMgr(m),
	}
	if err := prep(); err != nil {
		return nil, err
	}

	return module, nil
}

type instance interface{}
