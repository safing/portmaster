//go:build windows
// +build windows

package kext

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
)

type readHelper struct {
	infoType    byte
	commandSize uint32
	readSize    int
	reader      io.Reader
}

func newReadHelper(r io.Reader) (*readHelper, error) {
	h := &readHelper{reader: r}
	if err := binary.Read(r, binary.LittleEndian, &h.infoType); err != nil {
		return nil, err
	}
	if err := binary.Read(r, binary.LittleEndian, &h.commandSize); err != nil {
		return nil, err
	}
	return h, nil
}

func (h *readHelper) Read(p []byte) (int, error) {
	n, err := h.reader.Read(p)
	h.readSize += n
	return n, err
}

func (h *readHelper) readData(data any) error {
	if err := binary.Read(h, binary.LittleEndian, data); err != nil {
		return errors.Join(ErrUnexpectedReadError, err)
	}
	if uint32(h.readSize) > h.commandSize {
		return ErrUnexpectedInfoSize
	}
	return nil
}

func (h *readHelper) readBytes(size uint32) ([]byte, error) {
	if uint32(h.readSize) >= h.commandSize {
		return nil, errors.Join(fmt.Errorf("read past end"), ErrUnexpectedReadError)
	}
	if size == 0 {
		size = h.commandSize - uint32(h.readSize)
	}
	if h.commandSize < uint32(h.readSize)+size {
		return nil, ErrUnexpectedInfoSize
	}
	buf := make([]byte, size)
	if err := binary.Read(h, binary.LittleEndian, buf); err != nil {
		return nil, errors.Join(ErrUnexpectedReadError, err)
	}
	return buf, nil
}

func (h *readHelper) readUntilEnd() {
	_, _ = h.readBytes(0)
}

// RecvInfo reads and parses an info packet from the driver
func RecvInfo(r io.Reader) (*Info, error) {
	h, err := newReadHelper(r)
	if err != nil {
		return nil, err
	}
	defer h.readUntilEnd()

	switch h.infoType {
	case InfoConnectionIpv4:
		return parseConnectionV4(h)
	case InfoConnectionIpv6:
		return parseConnectionV6(h)
	case InfoConnectionEndEventV4:
		return parseConnectionEndV4(h)
	case InfoConnectionEndEventV6:
		return parseConnectionEndV6(h)
	case InfoLogLine:
		return parseLogLine(h)
	case InfoBandwidthStatsV4, InfoBandwidthStatsV6:
		return nil, nil // Skip bandwidth stats
	case InfoRedirectionRequestV4:
		return parseRedirectionRequestV4(h)
	case InfoRedirectionRequestV6:
		return parseRedirectionRequestV6(h)
	}
	return nil, ErrUnknownInfoType
}

func parseConnectionV4(h *readHelper) (*Info, error) {
	var raw rawConnectionV4
	if err := h.readData(&raw); err != nil {
		return nil, fmt.Errorf("failed to parse ConnectionV4: %w", err)
	}
	var payload []byte
	var payloadSize uint32
	if err := h.readData(&payloadSize); err != nil {
		return nil, fmt.Errorf("failed to parse ConnectionV4 payload size: %w", err)
	}
	if payloadSize > 0 {
		var err error
		if payload, err = h.readBytes(payloadSize); err != nil {
			return nil, fmt.Errorf("failed to parse ConnectionV4 payload: %w", err)
		}
	}
	return &Info{Connection: &Connection{
		ID:           raw.ID,
		ProcessID:    raw.ProcessID,
		Direction:    raw.Direction,
		Protocol:     raw.Protocol,
		LocalIP:      net.IP(raw.LocalIP[:]),
		RemoteIP:     net.IP(raw.RemoteIP[:]),
		LocalPort:    raw.LocalPort,
		RemotePort:   raw.RemotePort,
		PayloadLayer: raw.PayloadLayer,
		Payload:      payload,
		IsIPv6:       false,
	}}, nil
}

func parseConnectionV6(h *readHelper) (*Info, error) {
	var raw rawConnectionV6
	if err := h.readData(&raw); err != nil {
		return nil, fmt.Errorf("failed to parse ConnectionV6: %w", err)
	}
	var payload []byte
	var payloadSize uint32
	if err := h.readData(&payloadSize); err != nil {
		return nil, fmt.Errorf("failed to parse ConnectionV6 payload size: %w", err)
	}
	if payloadSize > 0 {
		var err error
		if payload, err = h.readBytes(payloadSize); err != nil {
			return nil, fmt.Errorf("failed to parse ConnectionV6 payload: %w", err)
		}
	}
	return &Info{Connection: &Connection{
		ID:           raw.ID,
		ProcessID:    raw.ProcessID,
		Direction:    raw.Direction,
		Protocol:     raw.Protocol,
		LocalIP:      net.IP(raw.LocalIP[:]),
		RemoteIP:     net.IP(raw.RemoteIP[:]),
		LocalPort:    raw.LocalPort,
		RemotePort:   raw.RemotePort,
		PayloadLayer: raw.PayloadLayer,
		Payload:      payload,
		IsIPv6:       true,
	}}, nil
}

func parseConnectionEndV4(h *readHelper) (*Info, error) {
	var raw rawConnectionEndV4
	if err := h.readData(&raw); err != nil {
		return nil, fmt.Errorf("failed to parse ConnectionEndV4: %w", err)
	}
	return &Info{ConnectionEnd: &ConnectionEnd{
		ProcessID:  raw.ProcessID,
		Direction:  raw.Direction,
		Protocol:   raw.Protocol,
		LocalIP:    net.IP(raw.LocalIP[:]),
		RemoteIP:   net.IP(raw.RemoteIP[:]),
		LocalPort:  raw.LocalPort,
		RemotePort: raw.RemotePort,
		IsIPv6:     false,
	}}, nil
}

func parseConnectionEndV6(h *readHelper) (*Info, error) {
	var raw rawConnectionEndV6
	if err := h.readData(&raw); err != nil {
		return nil, fmt.Errorf("failed to parse ConnectionEndV6: %w", err)
	}
	return &Info{ConnectionEnd: &ConnectionEnd{
		ProcessID:  raw.ProcessID,
		Direction:  raw.Direction,
		Protocol:   raw.Protocol,
		LocalIP:    net.IP(raw.LocalIP[:]),
		RemoteIP:   net.IP(raw.RemoteIP[:]),
		LocalPort:  raw.LocalPort,
		RemotePort: raw.RemotePort,
		IsIPv6:     true,
	}}, nil
}

func parseLogLine(h *readHelper) (*Info, error) {
	var severity byte
	if err := h.readData(&severity); err != nil {
		return nil, fmt.Errorf("failed to parse LogLine severity: %w", err)
	}
	lineBytes, err := h.readBytes(0)
	if err != nil {
		return nil, fmt.Errorf("failed to parse LogLine text: %w", err)
	}
	return &Info{LogLine: &LogLine{Severity: severity, Line: string(lineBytes)}}, nil
}

func parseRedirectionRequestV4(h *readHelper) (*Info, error) {
	var raw rawRedirectionRequestV4
	if err := h.readData(&raw); err != nil {
		return nil, fmt.Errorf("failed to parse RedirectionRequestV4: %w", err)
	}
	return &Info{RedirectionRequest: &RedirectionRequest{
		ID:         raw.ID,
		ProcessID:  raw.ProcessID,
		Direction:  raw.Direction,
		Protocol:   raw.Protocol,
		LocalIP:    net.IP(raw.LocalIP[:]),
		RemoteIP:   net.IP(raw.RemoteIP[:]),
		LocalPort:  raw.LocalPort,
		RemotePort: raw.RemotePort,
		IsIPv6:     false,
	}}, nil
}

func parseRedirectionRequestV6(h *readHelper) (*Info, error) {
	var raw rawRedirectionRequestV6
	if err := h.readData(&raw); err != nil {
		return nil, fmt.Errorf("failed to parse RedirectionRequestV6: %w", err)
	}
	return &Info{RedirectionRequest: &RedirectionRequest{
		ID:         raw.ID,
		ProcessID:  raw.ProcessID,
		Direction:  raw.Direction,
		Protocol:   raw.Protocol,
		LocalIP:    net.IP(raw.LocalIP[:]),
		RemoteIP:   net.IP(raw.RemoteIP[:]),
		LocalPort:  raw.LocalPort,
		RemotePort: raw.RemotePort,
		IsIPv6:     true,
	}}, nil
}
