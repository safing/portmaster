package terminal

import (
	"github.com/safing/structures/container"
	"github.com/safing/structures/varint"
)

/*
Terminal and Operation Message Format:

- Length [varint]
	- If Length is 0, the remainder of given data is padding.
- IDType [varint]
	- Type [uses least two significant bits]
		- One of Init, Data, Stop
	- ID [uses all other bits]
		- The ID is currently not adapted in order to make reading raw message
			easier. This means that IDs are currently always a multiple of 4.
- Data [bytes; format depends on msg type]
	- MsgTypeInit:
		- Data [bytes]
	- MsgTypeData:
		- AddAvailableSpace [varint, if Flow Queue is used]
		- (Encrypted) Data [bytes]
	- MsgTypeStop:
		- Error Code [varint]
*/

// MsgType is the message type for both terminals and operations.
type MsgType uint8

const (
	// MsgTypeInit is used to establish a new terminal or run a new operation.
	MsgTypeInit MsgType = 1

	// MsgTypeData is used to send data to a terminal or operation.
	MsgTypeData MsgType = 2

	// MsgTypePriorityData is used to send prioritized data to a terminal or operation.
	MsgTypePriorityData MsgType = 0

	// MsgTypeStop is used to abandon a terminal or end an operation, with an optional error.
	MsgTypeStop MsgType = 3
)

// AddIDType prepends the ID and Type header to the message.
func AddIDType(c *container.Container, id uint32, msgType MsgType) {
	c.Prepend(varint.Pack32(id | uint32(msgType)))
}

// MakeMsg prepends the message header (Length and ID+Type) to the data.
func MakeMsg(c *container.Container, id uint32, msgType MsgType) {
	AddIDType(c, id, msgType)
	c.PrependLength()
}

// ParseIDType parses the combined message ID and type.
func ParseIDType(c *container.Container) (id uint32, msgType MsgType, err error) {
	idType, err := c.GetNextN32()
	if err != nil {
		return 0, 0, err
	}

	msgType = MsgType(idType % 4)
	return idType - uint32(msgType), msgType, nil
}
