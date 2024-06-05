package client

import (
	"fmt"
	"sync"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/log"
)

const (
	backOffTimer = 1 * time.Second

	offlineSignal uint8 = 0
	onlineSignal  uint8 = 1
)

// The Client enables easy interaction with the API.
type Client struct {
	sync.Mutex

	server string

	onlineSignal   chan struct{}
	offlineSignal  chan struct{}
	shutdownSignal chan struct{}
	lastSignal     uint8

	send   chan *Message
	resend chan *Message
	recv   chan *Message

	operations map[string]*Operation
	nextOpID   uint64

	lastError string
}

// NewClient returns a new Client.
func NewClient(server string) *Client {
	c := &Client{
		server:         server,
		onlineSignal:   make(chan struct{}),
		offlineSignal:  make(chan struct{}),
		shutdownSignal: make(chan struct{}),
		lastSignal:     offlineSignal,
		send:           make(chan *Message, 100),
		resend:         make(chan *Message, 1),
		recv:           make(chan *Message, 100),
		operations:     make(map[string]*Operation),
	}
	go c.handler()
	return c
}

// Connect connects to the API once.
func (c *Client) Connect() error {
	defer c.signalOffline()

	err := c.wsConnect()
	if err != nil && err.Error() != c.lastError {
		log.Errorf("client: error connecting to Portmaster: %s", err)
		c.lastError = err.Error()
	}
	return err
}

// StayConnected calls Connect again whenever the connection is lost.
func (c *Client) StayConnected() {
	log.Infof("client: connecting to Portmaster at %s", c.server)

	_ = c.Connect()
	for {
		select {
		case <-time.After(backOffTimer):
			log.Infof("client: reconnecting...")
			_ = c.Connect()
		case <-c.shutdownSignal:
			return
		}
	}
}

// Shutdown shuts the client down.
func (c *Client) Shutdown() {
	select {
	case <-c.shutdownSignal:
	default:
		close(c.shutdownSignal)
	}
}

func (c *Client) signalOnline() {
	c.Lock()
	defer c.Unlock()
	if c.lastSignal == offlineSignal {
		log.Infof("client: went online")
		c.offlineSignal = make(chan struct{})
		close(c.onlineSignal)
		c.lastSignal = onlineSignal

		// resend unsent request
		for _, op := range c.operations {
			if op.resuscitationEnabled.IsSet() && op.request.sent != nil && op.request.sent.SetToIf(true, false) {
				op.client.send <- op.request
				log.Infof("client: resuscitated %s %s %s", op.request.OpID, op.request.Type, op.request.Key)
			}
		}

	}
}

func (c *Client) signalOffline() {
	c.Lock()
	defer c.Unlock()
	if c.lastSignal == onlineSignal {
		log.Infof("client: went offline")
		c.onlineSignal = make(chan struct{})
		close(c.offlineSignal)
		c.lastSignal = offlineSignal

		// signal offline status to operations
		for _, op := range c.operations {
			op.handle(&Message{
				OpID: op.ID,
				Type: MsgOffline,
			})
		}

	}
}

// Online returns a closed channel read if the client is connected to the API.
func (c *Client) Online() <-chan struct{} {
	c.Lock()
	defer c.Unlock()
	return c.onlineSignal
}

// Offline returns a closed channel read if the client is not connected to the API.
func (c *Client) Offline() <-chan struct{} {
	c.Lock()
	defer c.Unlock()
	return c.offlineSignal
}

func (c *Client) handler() {
	for {
		select {

		case m := <-c.recv:

			if m == nil {
				return
			}

			c.Lock()
			op, ok := c.operations[m.OpID]
			c.Unlock()

			if ok {
				log.Tracef("client: [%s] received %s msg: %s", m.OpID, m.Type, m.Key)
				op.handle(m)
			} else {
				log.Tracef("client: received message for unknown operation %s", m.OpID)
			}

		case <-c.shutdownSignal:
			return

		}
	}
}

// Operation represents a single operation by a client.
type Operation struct {
	ID                   string
	request              *Message
	client               *Client
	handleFunc           func(*Message)
	handler              chan *Message
	resuscitationEnabled *abool.AtomicBool
}

func (op *Operation) handle(m *Message) {
	if op.handleFunc != nil {
		op.handleFunc(m)
	} else {
		select {
		case op.handler <- m:
		default:
			log.Warningf("client: handler channel of operation %s overflowed", op.ID)
		}
	}
}

// Cancel the operation.
func (op *Operation) Cancel() {
	op.client.Lock()
	defer op.client.Unlock()
	delete(op.client.operations, op.ID)
	close(op.handler)
}

// Send sends a request to the API.
func (op *Operation) Send(command, text string, data interface{}) {
	op.request = &Message{
		OpID:  op.ID,
		Type:  command,
		Key:   text,
		Value: data,
		sent:  abool.NewBool(false),
	}
	log.Tracef("client: [%s] sending %s msg: %s", op.request.OpID, op.request.Type, op.request.Key)
	op.client.send <- op.request
}

// EnableResuscitation will resend the request after reconnecting to the API.
func (op *Operation) EnableResuscitation() {
	op.resuscitationEnabled.Set()
}

// NewOperation returns a new operation.
func (c *Client) NewOperation(handleFunc func(*Message)) *Operation {
	c.Lock()
	defer c.Unlock()

	c.nextOpID++
	op := &Operation{
		ID:                   fmt.Sprintf("#%d", c.nextOpID),
		client:               c,
		handleFunc:           handleFunc,
		handler:              make(chan *Message, 100),
		resuscitationEnabled: abool.NewBool(false),
	}
	c.operations[op.ID] = op
	return op
}
