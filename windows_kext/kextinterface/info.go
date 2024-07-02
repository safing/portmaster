package kextinterface

import (
	"encoding/binary"
	"errors"
	"fmt"
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
	ErrUnexpectedInfoSize  = errors.New("unexpected info size")
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

type readHelper struct {
	infoType    byte
	commandSize uint32

	readSize int

	reader io.Reader
}

func newReadHelper(reader io.Reader) (*readHelper, error) {
	helper := &readHelper{reader: reader}

	err := binary.Read(reader, binary.LittleEndian, &helper.infoType)
	if err != nil {
		return nil, err
	}

	err = binary.Read(reader, binary.LittleEndian, &helper.commandSize)
	if err != nil {
		return nil, err
	}

	return helper, nil
}

func (r *readHelper) ReadData(data any) error {
	err := binary.Read(r, binary.LittleEndian, data)
	if err != nil {
		return errors.Join(ErrUnexpectedReadError, err)
	}

	if err := r.checkOverRead(); err != nil {
		return err
	}

	return nil
}

// Passing size = 0 will read the rest of the command.
func (r *readHelper) ReadBytes(size uint32) ([]byte, error) {
	if uint32(r.readSize) >= r.commandSize {
		return nil, errors.Join(fmt.Errorf("cannot read more bytes than the command size: %d >= %d", r.readSize, r.commandSize), ErrUnexpectedReadError)
	}

	if size == 0 {
		size = r.commandSize - uint32(r.readSize)
	}

	if r.commandSize < uint32(r.readSize)+size {
		return nil, ErrUnexpectedInfoSize
	}

	bytes := make([]byte, size)
	err := binary.Read(r, binary.LittleEndian, bytes)
	if err != nil {
		return nil, errors.Join(ErrUnexpectedReadError, err)
	}

	return bytes, nil
}

func (r *readHelper) ReadUntilTheEnd() {
	_, _ = r.ReadBytes(0)
}

func (r *readHelper) checkOverRead() error {
	if uint32(r.readSize) > r.commandSize {
		return ErrUnexpectedInfoSize
	}

	return nil
}

func (r *readHelper) Read(p []byte) (n int, err error) {
	n, err = r.reader.Read(p)
	r.readSize += n
	return
}

func RecvInfo(reader io.Reader) (*Info, error) {
	helper, err := newReadHelper(reader)
	if err != nil {
		return nil, err
	}

	// Make sure the whole command is read before return.
	defer helper.ReadUntilTheEnd()

	// Read data
	switch helper.infoType {
	case InfoConnectionIpv4:
		{
			parseError := fmt.Errorf("failed to parse InfoConnectionIpv4")
			newInfo := ConnectionV4{}
			var fixedSizeValues connectionV4Internal
			// Read fixed size values.
			err = helper.ReadData(&fixedSizeValues)
			if err != nil {
				return nil, errors.Join(parseError, err, fmt.Errorf("fixed"))
			}
			newInfo.connectionV4Internal = fixedSizeValues
			// Read size of payload.
			var payloadSize uint32
			err = helper.ReadData(&payloadSize)
			if err != nil {
				return nil, errors.Join(parseError, err, fmt.Errorf("payloadsize"))
			}

			// Check if there is payload.
			if payloadSize > 0 {
				// Read payload.
				newInfo.Payload, err = helper.ReadBytes(payloadSize)
				if err != nil {
					return nil, errors.Join(parseError, err, fmt.Errorf("payload"))
				}
			}
			return &Info{ConnectionV4: &newInfo}, nil
		}
	case InfoConnectionIpv6:
		{
			parseError := fmt.Errorf("failed to parse InfoConnectionIpv6")
			newInfo := ConnectionV6{}

			// Read fixed size values.
			err = helper.ReadData(&newInfo.connectionV6Internal)
			if err != nil {
				return nil, errors.Join(parseError, err)
			}

			// Read size of payload.
			var payloadSize uint32
			err = helper.ReadData(&payloadSize)
			if err != nil {
				return nil, errors.Join(parseError, err)
			}

			// Check if there is payload.
			if payloadSize > 0 {
				// Read payload.
				newInfo.Payload, err = helper.ReadBytes(payloadSize)
				if err != nil {
					return nil, errors.Join(parseError, err)
				}
			}

			return &Info{ConnectionV6: &newInfo}, nil
		}
	case InfoConnectionEndEventV4:
		{
			parseError := fmt.Errorf("failed to parse InfoConnectionEndEventV4")
			var connectionEnd ConnectionEndV4

			// Read fixed size values.
			err = helper.ReadData(&connectionEnd)
			if err != nil {
				return nil, errors.Join(parseError, err)
			}
			return &Info{ConnectionEndV4: &connectionEnd}, nil
		}
	case InfoConnectionEndEventV6:
		{
			parseError := fmt.Errorf("failed to parse InfoConnectionEndEventV6")
			var connectionEnd ConnectionEndV6

			// Read fixed size values.
			err = helper.ReadData(&connectionEnd)
			if err != nil {
				return nil, errors.Join(parseError, err)
			}
			return &Info{ConnectionEndV6: &connectionEnd}, nil
		}
	case InfoLogLine:
		{
			parseError := fmt.Errorf("failed to parse InfoLogLine")
			logLine := LogLine{}
			// Read severity
			err = helper.ReadData(&logLine.Severity)
			if err != nil {
				return nil, errors.Join(parseError, err)
			}
			// Read string
			bytes, err := helper.ReadBytes(0)
			if err != nil {
				return nil, errors.Join(parseError, err)
			}
			logLine.Line = string(bytes)
			return &Info{LogLine: &logLine}, nil
		}
	case InfoBandwidthStatsV4:
		{
			parseError := fmt.Errorf("failed to parse InfoBandwidthStatsV4")
			// Read Protocol
			var protocol uint8
			err = helper.ReadData(&protocol)
			if err != nil {
				return nil, errors.Join(parseError, err)
			}
			// Read size of array
			var size uint32
			err = helper.ReadData(&size)
			if err != nil {
				return nil, errors.Join(parseError, err)
			}
			// Read array
			statsArray := make([]BandwidthValueV4, size)
			for i := range int(size) {
				err = helper.ReadData(&statsArray[i])
				if err != nil {
					return nil, errors.Join(parseError, err)
				}
			}

			return &Info{BandwidthStats: &BandwidthStatsArray{Protocol: protocol, ValuesV4: statsArray}}, nil
		}
	case InfoBandwidthStatsV6:
		{
			parseError := fmt.Errorf("failed to parse InfoBandwidthStatsV6")
			// Read Protocol
			var protocol uint8
			err = helper.ReadData(&protocol)
			if err != nil {
				return nil, errors.Join(parseError, err)
			}
			// Read size of array
			var size uint32
			err = helper.ReadData(&size)
			if err != nil {
				return nil, errors.Join(parseError, err)
			}
			// Read array
			statsArray := make([]BandwidthValueV6, size)
			for i := range int(size) {
				err = helper.ReadData(&statsArray[i])
				if err != nil {
					return nil, errors.Join(parseError, err)
				}
			}

			return &Info{BandwidthStats: &BandwidthStatsArray{Protocol: protocol, ValuesV6: statsArray}}, nil
		}
	}

	return nil, ErrUnknownInfoType
}
