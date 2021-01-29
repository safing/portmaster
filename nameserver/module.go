package nameserver

import (
	"context"
	"fmt"
	"net"
	"strconv"

	"github.com/safing/portbase/log"
	"github.com/safing/portbase/modules"
	"github.com/safing/portbase/modules/subsystems"
	"github.com/safing/portmaster/firewall"
	"github.com/safing/portmaster/netenv"

	"github.com/miekg/dns"
)

var (
	module       *modules.Module
	stopListener func() error
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
	err := registerConfig()
	if err != nil {
		return err
	}

	return registerMetrics()
}

func start() error {
	logFlagOverrides()

	ip1, ip2, port, err := getListenAddresses(nameserverAddressConfig())
	if err != nil {
		return fmt.Errorf("failed to parse nameserver listen address: %w", err)
	}

	// Start listener(s).
	if ip2 == nil {
		// Start a single listener.
		dnsServer := startListener(ip1, port)
		stopListener = dnsServer.Shutdown

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
		} else {
			return firewall.SetNameserverIPMatcher(func(ip net.IP) bool {
				return ip.Equal(ip1)
			})
		}

	} else {
		// Dual listener.
		dnsServer1 := startListener(ip1, port)
		dnsServer2 := startListener(ip2, port)
		stopListener = func() error {
			// Shutdown both listeners.
			err1 := dnsServer1.Shutdown()
			err2 := dnsServer2.Shutdown()
			// Return first error.
			if err1 != nil {
				return err1
			}
			return err2
		}

		// Fast track dns queries destined for one of the listener IPs.
		return firewall.SetNameserverIPMatcher(func(ip net.IP) bool {
			return ip.Equal(ip1) || ip.Equal(ip2)
		})
	}
}

func startListener(ip net.IP, port uint16) *dns.Server {
	// Create DNS server.
	dnsServer := &dns.Server{
		Addr: net.JoinHostPort(
			ip.String(),
			strconv.Itoa(int(port)),
		),
		Net: "udp",
	}
	dns.HandleFunc(".", handleRequestAsWorker)

	// Start DNS server as service worker.
	log.Infof("nameserver: starting to listen on %s", dnsServer.Addr)
	module.StartServiceWorker("dns resolver", 0, func(ctx context.Context) error {
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

	return dnsServer
}

func stop() error {
	if stopListener != nil {
		return stopListener()
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
