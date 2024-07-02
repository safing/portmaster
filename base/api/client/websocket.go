package client

import (
	"fmt"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/tevino/abool"

	"github.com/safing/portmaster/base/log"
)

type wsState struct {
	wsConn     *websocket.Conn
	wg         sync.WaitGroup
	failing    *abool.AtomicBool
	failSignal chan struct{}
}

func (c *Client) wsConnect() error {
	state := &wsState{
		failing:    abool.NewBool(false),
		failSignal: make(chan struct{}),
	}

	var err error
	state.wsConn, _, err = websocket.DefaultDialer.Dial(fmt.Sprintf("ws://%s/api/database/v1", c.server), nil)
	if err != nil {
		return err
	}

	c.signalOnline()

	state.wg.Add(2)
	go c.wsReader(state)
	go c.wsWriter(state)

	// wait for end of connection
	select {
	case <-state.failSignal:
	case <-c.shutdownSignal:
		state.Error("")
	}
	_ = state.wsConn.Close()
	state.wg.Wait()

	return nil
}

func (c *Client) wsReader(state *wsState) {
	defer state.wg.Done()
	for {
		_, data, err := state.wsConn.ReadMessage()
		log.Tracef("client: read message")
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				state.Error(fmt.Sprintf("client: read error: %s", err))
			} else {
				state.Error("client: connection closed by server")
			}
			return
		}
		log.Tracef("client: received message: %s", string(data))
		m, err := ParseMessage(data)
		if err != nil {
			log.Warningf("client: failed to parse message: %s", err)
		} else {
			select {
			case c.recv <- m:
			case <-state.failSignal:
				return
			}
		}
	}
}

func (c *Client) wsWriter(state *wsState) {
	defer state.wg.Done()
	for {
		select {
		case <-state.failSignal:
			return
		case m := <-c.resend:
			data, err := m.Pack()
			if err == nil {
				err = state.wsConn.WriteMessage(websocket.BinaryMessage, data)
			}
			if err != nil {
				state.Error(fmt.Sprintf("client: write error: %s", err))
				return
			}
			log.Tracef("client: sent message: %s", string(data))
			if m.sent != nil {
				m.sent.Set()
			}
		case m := <-c.send:
			data, err := m.Pack()
			if err == nil {
				err = state.wsConn.WriteMessage(websocket.BinaryMessage, data)
			}
			if err != nil {
				c.resend <- m
				state.Error(fmt.Sprintf("client: write error: %s", err))
				return
			}
			log.Tracef("client: sent message: %s", string(data))
			if m.sent != nil {
				m.sent.Set()
			}
		}
	}
}

func (state *wsState) Error(message string) {
	if state.failing.SetToIf(false, true) {
		close(state.failSignal)
		if message != "" {
			log.Warning(message)
		}
	}
}
