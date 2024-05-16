package kextinterface

import (
	"encoding/binary"
	"errors"
	"io"
)

const (
	InfoLogLine              = 0
	InfoConnectionIpv4       = 1
	InfoConnectionIpv6       = 2
	InfoConnectionEndEventV4 = 3
	InfoConnectionEndEventV6 = 4
	InfoBandwidthStatsV4     = 5
	InfoBandwidthStatsV6     = 6
)

var (
	ErrUnknownInfoType     = errors.New("unknown info type")
	ErrUnexpectedReadError = errors.New("unexpected read error")
)

type connectionV4Internal struct {
	ID           uint64
	ProcessID    uint64
	Direction    byte
	Protocol     byte
	LocalIP      [4]byte
	RemoteIP     [4]byte
	LocalPort    uint16
	RemotePort   uint16
	PayloadLayer uint8
}

type ConnectionV4 struct {
	connectionV4Internal
	Payload []byte
}

func (c *ConnectionV4) Compare(other *ConnectionV4) bool {
	return c.ID == other.ID &&
		c.ProcessID == other.ProcessID &&
		c.Direction == other.Direction &&
		c.Protocol == other.Protocol &&
		c.LocalIP == other.LocalIP &&
		c.RemoteIP == other.RemoteIP &&
		c.LocalPort == other.LocalPort &&
		c.RemotePort == other.RemotePort
}

type connectionV6Internal struct {
	ID           uint64
	ProcessID    uint64
	Direction    byte
	Protocol     byte
	LocalIP      [16]byte
	RemoteIP     [16]byte
	LocalPort    uint16
	RemotePort   uint16
	PayloadLayer uint8
}

type ConnectionV6 struct {
	connectionV6Internal
	Payload []byte
}

func (c ConnectionV6) Compare(other *ConnectionV6) bool {
	return c.ID == other.ID &&
		c.ProcessID == other.ProcessID &&
		c.Direction == other.Direction &&
		c.Protocol == other.Protocol &&
		c.LocalIP == other.LocalIP &&
		c.RemoteIP == other.RemoteIP &&
		c.LocalPort == other.LocalPort &&
		c.RemotePort == other.RemotePort
}

type ConnectionEndV4 struct {
	ProcessID  uint64
	Direction  byte
	Protocol   byte
	LocalIP    [4]byte
	RemoteIP   [4]byte
	LocalPort  uint16
	RemotePort uint16
}

type ConnectionEndV6 struct {
	ProcessID  uint64
	Direction  byte
	Protocol   byte
	LocalIP    [16]byte
	RemoteIP   [16]byte
	LocalPort  uint16
	RemotePort uint16
}

type LogLine struct {
	Severity byte
	Line     string
}

type BandwidthValueV4 struct {
	LocalIP          [4]byte
	LocalPort        uint16
	RemoteIP         [4]byte
	RemotePort       uint16
	TransmittedBytes uint64
	ReceivedBytes    uint64
}

type BandwidthValueV6 struct {
	LocalIP          [16]byte
	LocalPort        uint16
	RemoteIP         [16]byte
	RemotePort       uint16
	TransmittedBytes uint64
	ReceivedBytes    uint64
}

type BandwidthStatsArray struct {
	Protocol uint8
	ValuesV4 []BandwidthValueV4
	ValuesV6 []BandwidthValueV6
}

type Info struct {
	ConnectionV4    *ConnectionV4
	ConnectionV6    *ConnectionV6
	ConnectionEndV4 *ConnectionEndV4
	ConnectionEndV6 *ConnectionEndV6
	LogLine         *LogLine
	BandwidthStats  *BandwidthStatsArray
}

func RecvInfo(reader io.Reader) (*Info, error) {
	var infoType byte
	err := binary.Read(reader, binary.LittleEndian, &infoType)
	if err != nil {
		return nil, err
	}

	// Read size of data
	var size uint32
	err = binary.Read(reader, binary.LittleEndian, &size)
	if err != nil {
		return nil, err
	}

	// Read data
	switch infoType {
	case InfoConnectionIpv4:
		{
			var fixedSizeValues connectionV4Internal
			err = binary.Read(reader, binary.LittleEndian, &fixedSizeValues)
			if err != nil {
				return nil, errors.Join(ErrUnexpectedReadError, err)
			}
			// Read size of payload
			var size uint32
			err = binary.Read(reader, binary.LittleEndian, &size)
			if err != nil {
				return nil, errors.Join(ErrUnexpectedReadError, err)
			}
			newInfo := ConnectionV4{connectionV4Internal: fixedSizeValues, Payload: make([]byte, size)}
			err = binary.Read(reader, binary.LittleEndian, &newInfo.Payload)
			if err != nil {
				return nil, errors.Join(ErrUnexpectedReadError, err)
			}
			return &Info{ConnectionV4: &newInfo}, nil
		}
	case InfoConnectionIpv6:
		{
			var fixedSizeValues connectionV6Internal
			err = binary.Read(reader, binary.LittleEndian, &fixedSizeValues)
			if err != nil {
				return nil, errors.Join(ErrUnexpectedReadError, err)
			}
			// Read size of payload
			var size uint32
			err = binary.Read(reader, binary.LittleEndian, &size)
			if err != nil {
				return nil, errors.Join(ErrUnexpectedReadError, err)
			}
			newInfo := ConnectionV6{connectionV6Internal: fixedSizeValues, Payload: make([]byte, size)}
			err = binary.Read(reader, binary.LittleEndian, &newInfo.Payload)
			if err != nil {
				return nil, errors.Join(ErrUnexpectedReadError, err)
			}
			return &Info{ConnectionV6: &newInfo}, nil
		}
	case InfoConnectionEndEventV4:
		{
			var connectionEnd ConnectionEndV4
			err = binary.Read(reader, binary.LittleEndian, &connectionEnd)
			if err != nil {
				return nil, errors.Join(ErrUnexpectedReadError, err)
			}
			return &Info{ConnectionEndV4: &connectionEnd}, nil
		}
	case InfoConnectionEndEventV6:
		{
			var connectionEnd ConnectionEndV6
			err = binary.Read(reader, binary.LittleEndian, &connectionEnd)
			if err != nil {
				return nil, errors.Join(ErrUnexpectedReadError, err)
			}
			return &Info{ConnectionEndV6: &connectionEnd}, nil
		}
	case InfoLogLine:
		{
			logLine := LogLine{}
			// Read severity
			err = binary.Read(reader, binary.LittleEndian, &logLine.Severity)
			if err != nil {
				return nil, errors.Join(ErrUnexpectedReadError, err)
			}
			// Read string
			line := make([]byte, size-1) // -1 for the severity enum.
			err = binary.Read(reader, binary.LittleEndian, &line)
			if err != nil {
				return nil, errors.Join(ErrUnexpectedReadError, err)
			}
			logLine.Line = string(line)
			return &Info{LogLine: &logLine}, nil
		}
	case InfoBandwidthStatsV4:
		{
			// Read Protocol
			var protocol uint8
			err = binary.Read(reader, binary.LittleEndian, &protocol)
			if err != nil {
				return nil, errors.Join(ErrUnexpectedReadError, err)
			}
			// Read size of array
			var size uint32
			err = binary.Read(reader, binary.LittleEndian, &size)
			if err != nil {
				return nil, errors.Join(ErrUnexpectedReadError, err)
			}
			// Read array
			statsArray := make([]BandwidthValueV4, size)
			for i := range int(size) {
				err = binary.Read(reader, binary.LittleEndian, &statsArray[i])
				if err != nil {
					return nil, errors.Join(ErrUnexpectedReadError, err)
				}
			}

			return &Info{BandwidthStats: &BandwidthStatsArray{Protocol: protocol, ValuesV4: statsArray}}, nil
		}
	case InfoBandwidthStatsV6:
		{
			// Read Protocol
			var protocol uint8
			err = binary.Read(reader, binary.LittleEndian, &protocol)
			if err != nil {
				return nil, errors.Join(ErrUnexpectedReadError, err)
			}
			// Read size of array
			var size uint32
			err = binary.Read(reader, binary.LittleEndian, &size)
			if err != nil {
				return nil, errors.Join(ErrUnexpectedReadError, err)
			}
			// Read array
			statsArray := make([]BandwidthValueV6, size)
			for i := range int(size) {
				err = binary.Read(reader, binary.LittleEndian, &statsArray[i])
				if err != nil {
					return nil, errors.Join(ErrUnexpectedReadError, err)
				}
			}

			return &Info{BandwidthStats: &BandwidthStatsArray{Protocol: protocol, ValuesV6: statsArray}}, nil
		}
	}

	// Command not recognized, read until the end of command and return.
	// During normal operation this should not happen.
	unknownData := make([]byte, size)
	_, _ = reader.Read(unknownData)

	return nil, ErrUnknownInfoType
}
