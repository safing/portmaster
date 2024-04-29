package kext_interface

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

var ErrorUnknownInfoType = errors.New("unknown info type")

type connectionV4Internal struct {
	Id           uint64
	ProcessId    uint64
	Direction    byte
	Protocol     byte
	LocalIp      [4]byte
	RemoteIp     [4]byte
	LocalPort    uint16
	RemotePort   uint16
	PayloadLayer uint8
}

type ConnectionV4 struct {
	connectionV4Internal
	Payload []byte
}

func (c *ConnectionV4) Compare(other *ConnectionV4) bool {
	return c.Id == other.Id &&
		c.ProcessId == other.ProcessId &&
		c.Direction == other.Direction &&
		c.Protocol == other.Protocol &&
		c.LocalIp == other.LocalIp &&
		c.RemoteIp == other.RemoteIp &&
		c.LocalPort == other.LocalPort &&
		c.RemotePort == other.RemotePort
}

type connectionV6Internal struct {
	Id           uint64
	ProcessId    uint64
	Direction    byte
	Protocol     byte
	LocalIp      [16]byte
	RemoteIp     [16]byte
	LocalPort    uint16
	RemotePort   uint16
	PayloadLayer uint8
}

type ConnectionV6 struct {
	connectionV6Internal
	Payload []byte
}

func (c ConnectionV6) Compare(other *ConnectionV6) bool {
	return c.Id == other.Id &&
		c.ProcessId == other.ProcessId &&
		c.Direction == other.Direction &&
		c.Protocol == other.Protocol &&
		c.LocalIp == other.LocalIp &&
		c.RemoteIp == other.RemoteIp &&
		c.LocalPort == other.LocalPort &&
		c.RemotePort == other.RemotePort
}

type ConnectionEndV4 struct {
	ProcessId  uint64
	Direction  byte
	Protocol   byte
	LocalIp    [4]byte
	RemoteIp   [4]byte
	LocalPort  uint16
	RemotePort uint16
}

type ConnectionEndV6 struct {
	ProcessId  uint64
	Direction  byte
	Protocol   byte
	LocalIp    [16]byte
	RemoteIp   [16]byte
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

	// Read data
	switch infoType {
	case InfoConnectionIpv4:
		{
			var fixedSizeValues connectionV4Internal
			err = binary.Read(reader, binary.LittleEndian, &fixedSizeValues)
			if err != nil {
				return nil, err
			}
			// Read size of payload
			var size uint32
			err = binary.Read(reader, binary.LittleEndian, &size)
			if err != nil {
				return nil, err
			}
			newInfo := ConnectionV4{connectionV4Internal: fixedSizeValues, Payload: make([]byte, size)}
			err = binary.Read(reader, binary.LittleEndian, &newInfo.Payload)
			return &Info{ConnectionV4: &newInfo}, nil
		}
	case InfoConnectionIpv6:
		{
			var fixedSizeValues connectionV6Internal
			err = binary.Read(reader, binary.LittleEndian, &fixedSizeValues)
			if err != nil {
				return nil, err
			}
			// Read size of payload
			var size uint32
			err = binary.Read(reader, binary.LittleEndian, &size)
			if err != nil {
				return nil, err
			}
			newInfo := ConnectionV6{connectionV6Internal: fixedSizeValues, Payload: make([]byte, size)}
			err = binary.Read(reader, binary.LittleEndian, &newInfo.Payload)
			return &Info{ConnectionV6: &newInfo}, nil
		}
	case InfoConnectionEndEventV4:
		{
			var new ConnectionEndV4
			err = binary.Read(reader, binary.LittleEndian, &new)
			if err != nil {
				return nil, err
			}
			return &Info{ConnectionEndV4: &new}, nil
		}
	case InfoConnectionEndEventV6:
		{
			var new ConnectionEndV6
			err = binary.Read(reader, binary.LittleEndian, &new)
			if err != nil {
				return nil, err
			}
			return &Info{ConnectionEndV6: &new}, nil
		}
	case InfoLogLine:
		{
			var logLine = LogLine{}
			// Read severity
			err = binary.Read(reader, binary.LittleEndian, &logLine.Severity)
			if err != nil {
				return nil, err
			}
			// Read string
			var line = make([]byte, size-1) // -1 for the severity enum.
			err = binary.Read(reader, binary.LittleEndian, &line)
			logLine.Line = string(line)
			return &Info{LogLine: &logLine}, nil
		}
	case InfoBandwidthStatsV4:
		{
			// Read Protocol
			var protocol uint8
			err = binary.Read(reader, binary.LittleEndian, &protocol)
			if err != nil {
				return nil, err
			}
			// Read size of array
			var size uint32
			err = binary.Read(reader, binary.LittleEndian, &size)
			if err != nil {
				return nil, err
			}
			// Read array
			var stats_array = make([]BandwidthValueV4, size)
			for i := 0; i < int(size); i++ {
				binary.Read(reader, binary.LittleEndian, &stats_array[i])
			}

			return &Info{BandwidthStats: &BandwidthStatsArray{Protocol: protocol, ValuesV4: stats_array}}, nil
		}
	case InfoBandwidthStatsV6:
		{
			// Read Protocol
			var protocol uint8
			err = binary.Read(reader, binary.LittleEndian, &protocol)
			if err != nil {
				return nil, err
			}
			// Read size of array
			var size uint32
			err = binary.Read(reader, binary.LittleEndian, &size)
			if err != nil {
				return nil, err
			}
			// Read array
			var stats_array = make([]BandwidthValueV6, size)
			for i := 0; i < int(size); i++ {
				binary.Read(reader, binary.LittleEndian, &stats_array[i])
			}

			return &Info{BandwidthStats: &BandwidthStatsArray{Protocol: protocol, ValuesV6: stats_array}}, nil
		}
	}

	unknownData := make([]byte, size)
	reader.Read(unknownData)
	return nil, ErrorUnknownInfoType
}
