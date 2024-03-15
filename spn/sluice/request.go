package sluice

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/safing/portmaster/service/network"
	"github.com/safing/portmaster/service/network/packet"
)

const (
	defaultSluiceTTL = 30 * time.Second
)

var (
	// ErrUnsupported is returned when a protocol is not supported.
	ErrUnsupported = errors.New("unsupported protocol")

	// ErrSluiceOffline is returned when the sluice for a network is offline.
	ErrSluiceOffline = errors.New("is offline")
)

// Request holds request data for a sluice entry.
type Request struct {
	ConnInfo   *network.Connection
	CallbackFn RequestCallbackFunc
	Expires    time.Time
}

// RequestCallbackFunc is called for taking a over handling connection that arrived at the sluice.
type RequestCallbackFunc func(connInfo *network.Connection, conn net.Conn)

// AwaitRequest pre-registers a connection at the sluice for initializing it when it arrives.
func AwaitRequest(connInfo *network.Connection, callbackFn RequestCallbackFunc) error {
	network := getNetworkFromConnInfo(connInfo)
	if network == "" {
		return ErrUnsupported
	}

	sluice, ok := getSluice(network)
	if !ok {
		return fmt.Errorf("sluice for network %s %w", network, ErrSluiceOffline)
	}

	return sluice.AwaitRequest(&Request{
		ConnInfo:   connInfo,
		CallbackFn: callbackFn,
		Expires:    time.Now().Add(defaultSluiceTTL),
	})
}

func getNetworkFromConnInfo(connInfo *network.Connection) string {
	var network string

	// protocol
	switch connInfo.IPProtocol { //nolint:exhaustive // Looking for specific values.
	case packet.TCP:
		network = "tcp"
	case packet.UDP:
		network = "udp"
	default:
		return ""
	}

	// IP version
	switch connInfo.IPVersion {
	case packet.IPv4:
		network += "4"
	case packet.IPv6:
		network += "6"
	default:
		return ""
	}

	return network
}
