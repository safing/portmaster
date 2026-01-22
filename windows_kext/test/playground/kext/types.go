//go:build windows
// +build windows

package kext

import "net"

// Verdict command structure
type Verdict struct {
	Command uint8
	ID      uint64
	Verdict uint8
}

// UpdateV4 command structure
type UpdateV4 struct {
	Command       uint8
	Protocol      uint8
	LocalAddress  [4]byte
	LocalPort     uint16
	RemoteAddress [4]byte
	RemotePort    uint16
	Verdict       uint8
}

// UpdateV6 command structure
type UpdateV6 struct {
	Command       uint8
	Protocol      uint8
	LocalAddress  [16]byte
	LocalPort     uint16
	RemoteAddress [16]byte
	RemotePort    uint16
	Verdict       uint8
}

// Connection is a unified interface for V4/V6 connections
type Connection struct {
	ID           uint64
	ProcessID    uint64
	Direction    byte
	Protocol     byte
	LocalIP      net.IP
	RemoteIP     net.IP
	LocalPort    uint16
	RemotePort   uint16
	PayloadLayer uint8
	Payload      []byte
	IsIPv6       bool
}

// ConnectionEnd is a unified interface for V4/V6 connection end events
type ConnectionEnd struct {
	ProcessID  uint64
	Direction  byte
	Protocol   byte
	LocalIP    net.IP
	RemoteIP   net.IP
	LocalPort  uint16
	RemotePort uint16
	IsIPv6     bool
}

// RedirectionRequest is a unified interface for V4/V6 redirect requests
type RedirectionRequest struct {
	ID         uint64
	ProcessID  uint64
	Direction  byte
	Protocol   byte
	LocalIP    net.IP
	RemoteIP   net.IP
	LocalPort  uint16
	RemotePort uint16
	IsIPv6     bool
}

// LogLine received from driver
type LogLine struct {
	Severity byte
	Line     string
}

// RedirectV4 command structure - response to RedirectionRequestV4
type RedirectV4 struct {
	Command      uint8
	ID           uint64
	Redirect     uint8   // 0 = no redirect (permit), 1 = redirect to LocalAddress
	LocalAddress [4]byte // Local interface IP to redirect to (when Redirect = 1)
}

// RedirectV6 command structure - response to RedirectionRequestV6
type RedirectV6 struct {
	Command      uint8
	ID           uint64
	Redirect     uint8    // 0 = no redirect (permit), 1 = redirect to LocalAddress
	LocalAddress [16]byte // Local interface IP to redirect to (when Redirect = 1)
}

// Info represents a parsed info packet from driver
type Info struct {
	Connection         *Connection
	ConnectionEnd      *ConnectionEnd
	LogLine            *LogLine
	RedirectionRequest *RedirectionRequest
}

// Raw structs for binary parsing (fixed-size arrays for binary.Read)
type rawConnectionV4 struct {
	ID           uint64
	ProcessID    uint64
	Direction    byte
	Protocol     byte
	LocalIP      [4]byte
	RemoteIP     [4]byte
	LocalPort    uint16
	RemotePort   uint16
	PayloadLayer byte
}

type rawConnectionV6 struct {
	ID           uint64
	ProcessID    uint64
	Direction    byte
	Protocol     byte
	LocalIP      [16]byte
	RemoteIP     [16]byte
	LocalPort    uint16
	RemotePort   uint16
	PayloadLayer byte
}

type rawConnectionEndV4 struct {
	ProcessID  uint64
	Direction  byte
	Protocol   byte
	LocalIP    [4]byte
	RemoteIP   [4]byte
	LocalPort  uint16
	RemotePort uint16
}

type rawConnectionEndV6 struct {
	ProcessID  uint64
	Direction  byte
	Protocol   byte
	LocalIP    [16]byte
	RemoteIP   [16]byte
	LocalPort  uint16
	RemotePort uint16
}

type rawRedirectionRequestV4 struct {
	ID         uint64
	ProcessID  uint64
	Direction  byte
	Protocol   byte
	LocalIP    [4]byte
	RemoteIP   [4]byte
	LocalPort  uint16
	RemotePort uint16
}

type rawRedirectionRequestV6 struct {
	ID         uint64
	ProcessID  uint64
	Direction  byte
	Protocol   byte
	LocalIP    [16]byte
	RemoteIP   [16]byte
	LocalPort  uint16
	RemotePort uint16
}
