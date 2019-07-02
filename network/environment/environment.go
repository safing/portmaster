// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package environment

import (
	"bytes"
	"crypto/sha1"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/safing/portbase/log"
)

// TODO: find a good way to identify a network
// best options until now:
// MAC of gateway
// domain parameter of dhcp

// TODO: get dhcp servers on windows:
// windows: https://msdn.microsoft.com/en-us/library/windows/desktop/aa365917
// this info might already be included in the interfaces api provided by golang!

const (
	UNKNOWN uint8 = iota
	OFFLINE
	LIMITED // local network only
	PORTAL  // there seems to be an internet connection, but we are being intercepted
	ONLINE
)

const (
	connectivityRecheck = 2 * time.Second
	interfacesRecheck   = 2 * time.Second
	gatewaysRecheck     = 2 * time.Second
	nameserversRecheck  = 2 * time.Second
)

var (
	connectivity        uint8
	connectivityLock    sync.Mutex
	connectivityExpires = time.Now()

	// interfaces        = make(map[*net.IP]net.Flags)
	// interfacesLock    sync.Mutex
	// interfacesExpires = time.Now()

	gateways        = make([]*net.IP, 0)
	gatewaysLock    sync.Mutex
	gatewaysExpires = time.Now()

	nameservers        = make([]Nameserver, 0)
	nameserversLock    sync.Mutex
	nameserversExpires = time.Now()

	lastNetworkChange   *int64
	lastNetworkChecksum []byte
)

type Nameserver struct {
	IP     net.IP
	Search []string
}

func init() {
	lnc := int64(0)
	lastNetworkChange = &lnc
	go func() {
		time.Sleep(1 * time.Second)
		Connectivity()
	}()

	go monitorNetworkChanges()
}

// Connectivity returns the current state of connectivity to the network/Internet
func Connectivity() uint8 {
	// locking
	connectivityLock.Lock()
	defer connectivityLock.Unlock()
	// cache
	if connectivityExpires.After(time.Now()) {
		return connectivity
	}
	// logic
	// TODO: implement more methods
	status, err := getConnectivityStateFromDbus()
	if err != nil {
		log.Warningf("environment: could not get connectivity: %s", err)
		setConnectivity(UNKNOWN)
		return UNKNOWN
	}
	setConnectivity(status)
	return status
}

func setConnectivity(status uint8) {
	if connectivity != status {
		connectivity = status
		connectivityExpires = time.Now().Add(connectivityRecheck)

		var connectivityName string
		switch connectivity {
		case UNKNOWN:
			connectivityName = "unknown"
		case OFFLINE:
			connectivityName = "offline"
		case LIMITED:
			connectivityName = "limited"
		case PORTAL:
			connectivityName = "portal"
		case ONLINE:
			connectivityName = "online"
		default:
			connectivityName = "invalid"
		}
		log.Infof("environment: connectivity changed to %s", connectivityName)
	}
}

// ConnectionSucceeded should be called when a module was able to successfully connect to the internet (do not call too often)
func ConnectionSucceeded() {
	connectivityLock.Lock()
	defer connectivityLock.Unlock()
	setConnectivity(ONLINE)
}

func monitorNetworkChanges() {
	// TODO: make more elegant solution
	for {
		time.Sleep(2 * time.Second)
		hasher := sha1.New()
		interfaces, err := net.Interfaces()
		if err != nil {
			log.Warningf("environment: failed to get interfaces: %s", err)
			continue
		}
		for _, iface := range interfaces {
			io.WriteString(hasher, iface.Name)
			// log.Tracef("adding: %s", iface.Name)
			io.WriteString(hasher, iface.Flags.String())
			// log.Tracef("adding: %s", iface.Flags.String())
			addrs, err := iface.Addrs()
			if err != nil {
				log.Warningf("environment: failed to get addrs from interface %s: %s", iface.Name, err)
				continue
			}
			for _, addr := range addrs {
				io.WriteString(hasher, addr.String())
				// log.Tracef("adding: %s", addr.String())
			}
		}
		newChecksum := hasher.Sum(nil)
		if !bytes.Equal(lastNetworkChecksum, newChecksum) {
			if len(lastNetworkChecksum) == 0 {
				lastNetworkChecksum = newChecksum
				continue
			}
			lastNetworkChecksum = newChecksum
			atomic.StoreInt64(lastNetworkChange, time.Now().Unix())
			log.Info("environment: network changed")
			triggerNetworkChanged()
		}
	}
}
