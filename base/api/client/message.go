package client

import (
	"bytes"
	"errors"

	"github.com/tevino/abool"

	"github.com/safing/structures/container"
	"github.com/safing/structures/dsd"
)

// ErrMalformedMessage is returned when a malformed message was encountered.
var ErrMalformedMessage = errors.New("malformed message")

// Message is an API message.
type Message struct {
	OpID     string
	Type     string
	Key      string
	RawValue []byte
	Value    interface{}
	sent     *abool.AtomicBool
}

// ParseMessage parses the given raw data and returns a Message.
func ParseMessage(data []byte) (*Message, error) {
	parts := bytes.SplitN(data, apiSeperatorBytes, 4)
	if len(parts) < 2 {
		return nil, ErrMalformedMessage
	}

	m := &Message{
		OpID: string(parts[0]),
		Type: string(parts[1]),
	}

	switch m.Type {
	case MsgOk, MsgUpdate, MsgNew:
		// parse key and data
		//    127|ok|<key>|<data>
		//    127|upd|<key>|<data>
		//    127|new|<key>|<data>
		if len(parts) != 4 {
			return nil, ErrMalformedMessage
		}
		m.Key = string(parts[2])
		m.RawValue = parts[3]
	case MsgDelete:
		// parse key
		//    127|del|<key>
		if len(parts) != 3 {
			return nil, ErrMalformedMessage
		}
		m.Key = string(parts[2])
	case MsgWarning, MsgError:
		// parse message
		//    127|error|<message>
		//    127|warning|<message> // error with single record, operation continues
		if len(parts) != 3 {
			return nil, ErrMalformedMessage
		}
		m.Key = string(parts[2])
	case MsgDone, MsgSuccess:
		// nothing more to do
		//    127|success
		//    127|done
	}

	return m, nil
}

// Pack serializes a message into a []byte slice.
func (m *Message) Pack() ([]byte, error) {
	c := container.New([]byte(m.OpID), apiSeperatorBytes, []byte(m.Type))

	if m.Key != "" {
		c.Append(apiSeperatorBytes)
		c.Append([]byte(m.Key))
		if len(m.RawValue) > 0 {
			c.Append(apiSeperatorBytes)
			c.Append(m.RawValue)
		} else if m.Value != nil {
			var err error
			m.RawValue, err = dsd.Dump(m.Value, dsd.JSON)
			if err != nil {
				return nil, err
			}
			c.Append(apiSeperatorBytes)
			c.Append(m.RawValue)
		}
	}

	return c.CompileData(), nil
}
