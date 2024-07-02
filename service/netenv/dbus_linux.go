//go:build !server

package netenv

import (
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/godbus/dbus/v5"

	"github.com/safing/portmaster/base/log"
)

var (
	dbusConn     *dbus.Conn
	dbusConnLock sync.Mutex
)

func getNameserversFromDbus() ([]Nameserver, error) { //nolint:gocognit // TODO
	// cmdline tool for exploring: gdbus introspect --system --dest org.freedesktop.NetworkManager --object-path /org/freedesktop/NetworkManager

	var ns []Nameserver
	var err error

	dbusConnLock.Lock()
	defer dbusConnLock.Unlock()

	if dbusConn == nil {
		dbusConn, err = dbus.SystemBus()
	}
	if err != nil {
		return nil, err
	}

	primaryConnectionVariant, err := getNetworkManagerProperty(dbusConn, dbus.ObjectPath("/org/freedesktop/NetworkManager"), "org.freedesktop.NetworkManager.PrimaryConnection")
	if err != nil {
		return nil, fmt.Errorf("dbus: failed to access NetworkManager.PrimaryConnection: %w", err)
	}
	primaryConnection, ok := primaryConnectionVariant.Value().(dbus.ObjectPath)
	if !ok {
		return nil, errors.New("dbus: could not assert type of /org/freedesktop/NetworkManager:org.freedesktop.NetworkManager.PrimaryConnection")
	}

	activeConnectionsVariant, err := getNetworkManagerProperty(dbusConn, dbus.ObjectPath("/org/freedesktop/NetworkManager"), "org.freedesktop.NetworkManager.ActiveConnections")
	if err != nil {
		return nil, fmt.Errorf("dbus: failed to access NetworkManager.ActiveConnections: %w", err)
	}
	activeConnections, ok := activeConnectionsVariant.Value().([]dbus.ObjectPath)
	if !ok {
		return nil, errors.New("dbus: could not assert type of /org/freedesktop/NetworkManager:org.freedesktop.NetworkManager.ActiveConnections")
	}

	sortedConnections := []dbus.ObjectPath{primaryConnection}
	for _, activeConnection := range activeConnections {
		if !objectPathInSlice(activeConnection, sortedConnections) {
			sortedConnections = append(sortedConnections, activeConnection)
		}
	}

	for _, activeConnection := range sortedConnections {
		newNameservers, err := dbusGetInterfaceNameservers(dbusConn, activeConnection, 4)
		if err != nil {
			log.Warningf("failed to get nameserver: %s", err)
		} else {
			ns = append(ns, newNameservers...)
		}

		newNameservers, err = dbusGetInterfaceNameservers(dbusConn, activeConnection, 6)
		if err != nil {
			log.Warningf("failed to get nameserver: %s", err)
		} else {
			ns = append(ns, newNameservers...)
		}
	}

	return ns, nil
}

func dbusGetInterfaceNameservers(dbusConn *dbus.Conn, interfaceObject dbus.ObjectPath, ipVersion uint8) ([]Nameserver, error) {
	ipConfigPropertyKey := fmt.Sprintf("org.freedesktop.NetworkManager.Connection.Active.Ip%dConfig", ipVersion)
	nameserversIPsPropertyKey := fmt.Sprintf("org.freedesktop.NetworkManager.IP%dConfig.Nameservers", ipVersion)
	nameserversDomainsPropertyKey := fmt.Sprintf("org.freedesktop.NetworkManager.IP%dConfig.Domains", ipVersion)
	nameserversSearchesPropertyKey := fmt.Sprintf("org.freedesktop.NetworkManager.IP%dConfig.Searches", ipVersion)

	// Get Interface Configuration.
	ipConfigVariant, err := getNetworkManagerProperty(dbusConn, interfaceObject, ipConfigPropertyKey)
	if err != nil {
		return nil, fmt.Errorf("failed to access %s:%s: %w", interfaceObject, ipConfigPropertyKey, err)
	}
	ipConfig, ok := ipConfigVariant.Value().(dbus.ObjectPath)
	if !ok {
		return nil, fmt.Errorf("could not assert type of %s:%s", interfaceObject, ipConfigPropertyKey)
	}

	// Check if interface is active in the selected IP version
	if !ipConfig.IsValid() || ipConfig == "/" {
		return nil, nil
	}

	// Get Nameserver IPs
	nameserverIPsVariant, err := getNetworkManagerProperty(dbusConn, ipConfig, nameserversIPsPropertyKey)
	if err != nil {
		return nil, fmt.Errorf("failed to access %s:%s: %w", ipConfig, nameserversIPsPropertyKey, err)
	}
	var nameserverIPs []net.IP
	switch ipVersion {
	case 4:
		nameserverIP4s, ok := nameserverIPsVariant.Value().([]uint32)
		if !ok {
			return nil, fmt.Errorf("could not assert type of %s:%s", ipConfig, nameserversIPsPropertyKey)
		}
		for _, ip := range nameserverIP4s {
			a := uint8(ip / 16777216)
			b := uint8((ip % 16777216) / 65536)
			c := uint8((ip % 65536) / 256)
			d := uint8(ip % 256)
			nameserverIPs = append(nameserverIPs, net.IPv4(d, c, b, a))
		}
	case 6:
		nameserverIP6s, ok := nameserverIPsVariant.Value().([][]byte)
		if !ok {
			return nil, fmt.Errorf("could not assert type of %s:%s", ipConfig, nameserversIPsPropertyKey)
		}
		for _, ip := range nameserverIP6s {
			if len(ip) != 16 {
				return nil, fmt.Errorf("query returned IPv6 address with invalid length: %q", ip)
			}
			nameserverIPs = append(nameserverIPs, net.IP(ip))
		}
	}

	// Get Nameserver Domains
	nameserverDomainsVariant, err := getNetworkManagerProperty(dbusConn, ipConfig, nameserversDomainsPropertyKey)
	if err != nil {
		return nil, fmt.Errorf("failed to access %s:%s: %w", ipConfig, nameserversDomainsPropertyKey, err)
	}
	nameserverDomains, ok := nameserverDomainsVariant.Value().([]string)
	if !ok {
		return nil, fmt.Errorf("could not assert type of %s:%s", ipConfig, nameserversDomainsPropertyKey)
	}

	// Get Nameserver Searches
	nameserverSearchesVariant, err := getNetworkManagerProperty(dbusConn, ipConfig, nameserversSearchesPropertyKey)
	if err != nil {
		return nil, fmt.Errorf("failed to access %s:%s: %w", ipConfig, nameserversSearchesPropertyKey, err)
	}
	nameserverSearches, ok := nameserverSearchesVariant.Value().([]string)
	if !ok {
		return nil, fmt.Errorf("could not assert type of %s:%s", ipConfig, nameserversSearchesPropertyKey)
	}

	ns := make([]Nameserver, 0, len(nameserverIPs))
	searchDomains := append(nameserverDomains, nameserverSearches...) //nolint:gocritic
	for _, nameserverIP := range nameserverIPs {
		ns = append(ns, Nameserver{
			IP:     nameserverIP,
			Search: searchDomains,
		})
	}

	return ns, nil
}

func getConnectivityStateFromDbus() (OnlineStatus, error) {
	var err error

	dbusConnLock.Lock()
	defer dbusConnLock.Unlock()

	if dbusConn == nil {
		dbusConn, err = dbus.SystemBus()
	}
	if err != nil {
		return 0, err
	}

	connectivityStateVariant, err := getNetworkManagerProperty(dbusConn, dbus.ObjectPath("/org/freedesktop/NetworkManager"), "org.freedesktop.NetworkManager.Connectivity")
	if err != nil {
		return 0, err
	}
	connectivityState, ok := connectivityStateVariant.Value().(uint32)
	if !ok {
		return 0, errors.New("dbus: could not assert type of /org/freedesktop/NetworkManager:org.freedesktop.NetworkManager.Connectivity")
	}

	// NMConnectivityState
	// NM_CONNECTIVITY_UNKNOWN	= 0 Network connectivity is unknown.
	// NM_CONNECTIVITY_NONE = 1 The host is not connected to any network.
	// NM_CONNECTIVITY_PORTAL = 2 The host is behind a captive portal and cannot reach the full Internet.
	// NM_CONNECTIVITY_LIMITED = 3 The host is connected to a network, but does not appear to be able to reach the full Internet.
	// NM_CONNECTIVITY_FULL = 4 The host is connected to a network, and appears to be able to reach the full Internet.

	switch connectivityState {
	case 0:
		return StatusUnknown, nil
	case 1:
		return StatusOffline, nil
	case 2:
		return StatusPortal, nil
	case 3:
		return StatusLimited, nil
	case 4:
		return StatusOnline, nil
	}

	return StatusUnknown, nil
}

func getNetworkManagerProperty(conn *dbus.Conn, objectPath dbus.ObjectPath, property string) (dbus.Variant, error) {
	object := conn.Object("org.freedesktop.NetworkManager", objectPath)
	return object.GetProperty(property)
}

func objectPathInSlice(a dbus.ObjectPath, list []dbus.ObjectPath) bool {
	for _, b := range list {
		if string(b) == string(a) {
			return true
		}
	}
	return false
}
