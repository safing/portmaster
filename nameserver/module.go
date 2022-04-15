package nameserver

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"sync"

	"github.com/miekg/dns"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/modules/subsystems"
	"github.com/safing/portmaster/compat"
	"github.com/safing/portmaster/firewall"
	"github.com/safing/portmaster/netenv"
)

var (
	module *modules.Module

	stopListeners     bool
	stopListener1     func() error
	stopListener2     func() error
	stopListenersLock sync.Mutex
)

func init() {
	module = modules.Register("nameserver", prep, start, stop, "core", "resolver")
	subsystems.Register(
		"dns",
		"Secure DNS",
		"DNS resolver with scoping and DNS-over-TLS",
		module,
		"config:dns/",
		nil,
	)
}

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
	module.StartServiceWorker("dns resolver", 0, func(ctx context.Context) error {
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

		// Start listening.
		log.Infof("nameserver: starting to listen on %s", dnsServer.Addr)
		err := dnsServer.ListenAndServe()
		if err != nil {
			// check if we are shutting down
			if module.IsStopping() {
				return nil
			}
			// is something blocking our port?
			checkErr := checkForConflictingService(ip, port)
			if checkErr != nil {
				return checkErr
			}
		}
		return err
	})
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
		ip2 = net.IPv6loopback
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
