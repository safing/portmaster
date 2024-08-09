package docks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/tevino/abool"

	"github.com/safing/jess"
	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/spn/cabin"
	"github.com/safing/portmaster/spn/hub"
	"github.com/safing/portmaster/spn/ships"
	"github.com/safing/portmaster/spn/terminal"
	"github.com/safing/structures/container"
	"github.com/safing/structures/varint"
)

const (
	// QOTD holds the quote of the day to return on idling unused connections.
	QOTD = "Privacy is not an option, and it shouldn't be the price we accept for just getting on the Internet.\nGary Kovacs\n"

	// maxUnloadSize defines the maximum size of a message to unload.
	maxUnloadSize            = 16384
	maxSegmentLength         = 16384
	maxCraneStoppingDuration = 6 * time.Hour
	maxCraneStopDuration     = 10 * time.Second
)

var (
	// optimalMinLoadSize defines minimum for Crane.targetLoadSize.
	optimalMinLoadSize = 3072 // Targeting around 4096.

	// loadingMaxWaitDuration is the maximum time a crane will wait for
	// additional data to send.
	loadingMaxWaitDuration = 5 * time.Millisecond
)

// Errors.
var (
	ErrDone = errors.New("crane is done")
)

// Crane is the primary duplexer and connection manager.
type Crane struct {
	// ID is the ID of the Crane.
	ID string
	// opts holds options.
	opts terminal.TerminalOpts

	// ctx is the context of the Terminal.
	ctx context.Context
	// cancelCtx cancels ctx.
	cancelCtx context.CancelFunc
	// stopping indicates if the Crane will be stopped soon. The Crane may still
	// be used until stopped, but must not be advertised anymore.
	stopping *abool.AtomicBool
	// stopped indicates if the Crane has been stopped. Whoever stopped the Crane
	// already took care of notifying everyone, so a silent fail is normally the
	// best response.
	stopped *abool.AtomicBool
	// authenticated indicates if there is has been any successful authentication.
	authenticated *abool.AtomicBool

	// ConnectedHub is the identity of the remote Hub.
	ConnectedHub *hub.Hub
	// NetState holds the network optimization state.
	// It must always be set and the reference must not be changed.
	// Access to fields within are coordinated by itself.
	NetState *NetworkOptimizationState
	// identity is identity of this instance and is usually only populated on a server.
	identity *cabin.Identity

	// jession is the jess session used for encryption.
	jession *jess.Session
	// jessionLock locks jession.
	jessionLock sync.Mutex

	// Controller is the Crane's Controller Terminal.
	Controller *CraneControllerTerminal

	// ship represents the underlying physical connection.
	ship ships.Ship
	// unloading moves containers from the ship to the crane.
	unloading chan *container.Container
	// loading moves containers from the crane to the ship.
	loading chan *container.Container
	// terminalMsgs holds containers from terminals waiting to be laoded.
	terminalMsgs chan *terminal.Msg
	// controllerMsgs holds important containers from terminals waiting to be laoded.
	controllerMsgs chan *terminal.Msg

	// terminals holds all the connected terminals.
	terminals map[uint32]terminal.Terminal
	// terminalsLock locks terminals.
	terminalsLock sync.Mutex
	// nextTerminalID holds the next terminal ID.
	nextTerminalID uint32

	// targetLoadSize defines the optimal loading size.
	targetLoadSize int
}

// NewCrane returns a new crane.
func NewCrane(ship ships.Ship, connectedHub *hub.Hub, id *cabin.Identity) (*Crane, error) {
	// Cranes always run in module context.
	ctx, cancelCtx := context.WithCancel(module.mgr.Ctx())

	newCrane := &Crane{
		ctx:           ctx,
		cancelCtx:     cancelCtx,
		stopping:      abool.NewBool(false),
		stopped:       abool.NewBool(false),
		authenticated: abool.NewBool(false),

		ConnectedHub: connectedHub,
		NetState:     newNetworkOptimizationState(),
		identity:     id,

		ship:           ship,
		unloading:      make(chan *container.Container),
		loading:        make(chan *container.Container, 100),
		terminalMsgs:   make(chan *terminal.Msg, 100),
		controllerMsgs: make(chan *terminal.Msg, 100),

		terminals: make(map[uint32]terminal.Terminal),
	}
	err := registerCrane(newCrane)
	if err != nil {
		return nil, fmt.Errorf("failed to register crane: %w", err)
	}

	// Shift next terminal IDs on the server.
	if !ship.IsMine() {
		newCrane.nextTerminalID += 4
	}

	// Calculate target load size.
	loadSize := ship.LoadSize()
	if loadSize <= 0 {
		loadSize = ships.BaseMTU
	}
	newCrane.targetLoadSize = loadSize
	for newCrane.targetLoadSize < optimalMinLoadSize {
		newCrane.targetLoadSize += loadSize
	}
	// Subtract overhead needed for encryption.
	newCrane.targetLoadSize -= 25 // Manually tested for jess.SuiteWireV1
	// Subtract space needed for length encoding the final chunk.
	newCrane.targetLoadSize -= varint.EncodedSize(uint64(newCrane.targetLoadSize))

	return newCrane, nil
}

// IsMine returns whether the crane was started on this side.
func (crane *Crane) IsMine() bool {
	return crane.ship.IsMine()
}

// Public returns whether the crane has been published.
func (crane *Crane) Public() bool {
	return crane.ship.Public()
}

// IsStopping returns whether the crane is stopping.
func (crane *Crane) IsStopping() bool {
	return crane.stopping.IsSet()
}

// MarkStoppingRequested marks the crane as stopping requested.
func (crane *Crane) MarkStoppingRequested() {
	crane.NetState.lock.Lock()
	defer crane.NetState.lock.Unlock()

	if !crane.NetState.stoppingRequested {
		crane.NetState.stoppingRequested = true
		crane.startSyncStateOp()
	}
}

// MarkStopping marks the crane as stopping.
func (crane *Crane) MarkStopping() (stopping bool) {
	// Can only stop owned cranes.
	if !crane.IsMine() {
		return false
	}

	if !crane.stopping.SetToIf(false, true) {
		return false
	}

	crane.NetState.lock.Lock()
	defer crane.NetState.lock.Unlock()
	crane.NetState.markedStoppingAt = time.Now()

	crane.startSyncStateOp()
	return true
}

// AbortStopping aborts the stopping.
func (crane *Crane) AbortStopping() (aborted bool) {
	aborted = crane.stopping.SetToIf(true, false)

	crane.NetState.lock.Lock()
	defer crane.NetState.lock.Unlock()

	abortedStoppingRequest := crane.NetState.stoppingRequested
	crane.NetState.stoppingRequested = false
	crane.NetState.markedStoppingAt = time.Time{}

	// Sync if any state changed.
	if aborted || abortedStoppingRequest {
		crane.startSyncStateOp()
	}

	return aborted
}

// Authenticated returns whether the other side of the crane has authenticated
// itself with an access code.
func (crane *Crane) Authenticated() bool {
	return crane.authenticated.IsSet()
}

// Publish publishes the connection as a lane.
func (crane *Crane) Publish() error {
	// Check if crane is connected.
	if crane.ConnectedHub == nil {
		return fmt.Errorf("spn/docks: %s: cannot publish without defined connected hub", crane)
	}

	// Submit metrics.
	if !crane.Public() {
		newPublicCranes.Inc()
	}

	// Mark crane as public.
	maskedID := crane.ship.MaskAddress(crane.ship.RemoteAddr())
	crane.ship.MarkPublic()

	// Assign crane to make it available to others.
	AssignCrane(crane.ConnectedHub.ID, crane)

	log.Infof("spn/docks: %s (was %s) is now public", crane, maskedID)
	return nil
}

// LocalAddr returns ship's local address.
func (crane *Crane) LocalAddr() net.Addr {
	return crane.ship.LocalAddr()
}

// RemoteAddr returns ship's local address.
func (crane *Crane) RemoteAddr() net.Addr {
	return crane.ship.RemoteAddr()
}

// Transport returns ship's transport.
func (crane *Crane) Transport() *hub.Transport {
	return crane.ship.Transport()
}

func (crane *Crane) getNextTerminalID() uint32 {
	crane.terminalsLock.Lock()
	defer crane.terminalsLock.Unlock()

	for {
		// Bump to next ID.
		crane.nextTerminalID += 8

		// Check if it's free.
		_, ok := crane.terminals[crane.nextTerminalID]
		if !ok {
			return crane.nextTerminalID
		}
	}
}

func (crane *Crane) terminalCount() int {
	crane.terminalsLock.Lock()
	defer crane.terminalsLock.Unlock()

	return len(crane.terminals)
}

func (crane *Crane) getTerminal(id uint32) (t terminal.Terminal, ok bool) {
	crane.terminalsLock.Lock()
	defer crane.terminalsLock.Unlock()

	t, ok = crane.terminals[id]
	return
}

func (crane *Crane) setTerminal(t terminal.Terminal) {
	crane.terminalsLock.Lock()
	defer crane.terminalsLock.Unlock()

	crane.terminals[t.ID()] = t
}

func (crane *Crane) deleteTerminal(id uint32) (t terminal.Terminal, ok bool) {
	crane.terminalsLock.Lock()
	defer crane.terminalsLock.Unlock()

	t, ok = crane.terminals[id]
	if ok {
		delete(crane.terminals, id)
		return t, true
	}
	return nil, false
}

// AbandonTerminal abandons the terminal with the given ID.
func (crane *Crane) AbandonTerminal(id uint32, err *terminal.Error) {
	// Get active terminal.
	t, ok := crane.deleteTerminal(id)
	if ok {
		// If the terminal was registered, abandon it.

		// Log reason the terminal is ending. Override stopping error with nil.
		switch {
		case err == nil || err.IsOK():
			log.Debugf("spn/docks: %T %s is being abandoned", t, t.FmtID())
		case err.Is(terminal.ErrStopping):
			err = nil
			log.Debugf("spn/docks: %T %s is being abandoned by peer", t, t.FmtID())
		case err.Is(terminal.ErrNoActivity):
			err = nil
			log.Debugf("spn/docks: %T %s is being abandoned due to no activity", t, t.FmtID())
		default:
			log.Warningf("spn/docks: %T %s: %s", t, t.FmtID(), err)
		}

		// Call the terminal's abandon function.
		t.Abandon(err)
	} else { //nolint:gocritic
		// When a crane terminal is abandoned, it calls crane.AbandonTerminal when
		// finished. This time, the terminal won't be in the registry anymore and
		// it finished shutting down, so we can now check if the crane needs to be
		// stopped.

		// If the crane is stopping, check if we can stop.
		// We can stop when all terminals are abandoned or after a timeout.
		// FYI: The crane controller will always take up one slot.
		if crane.stopping.IsSet() &&
			crane.terminalCount() <= 1 {
			// Stop the crane in worker, so the caller can do some work.
			module.mgr.Go("retire crane", func(_ *mgr.WorkerCtx) error {
				// Let enough time for the last errors to be sent, as terminals are abandoned in a goroutine.
				time.Sleep(3 * time.Second)
				crane.Stop(nil)
				return nil
			})
		}
	}
}

func (crane *Crane) sendImportantTerminalMsg(msg *terminal.Msg, timeout time.Duration) *terminal.Error {
	select {
	case crane.controllerMsgs <- msg:
		return nil
	case <-crane.ctx.Done():
		msg.Finish()
		return terminal.ErrCanceled
	}
}

// Send is used by others to send a message through the crane.
func (crane *Crane) Send(msg *terminal.Msg, timeout time.Duration) *terminal.Error {
	select {
	case crane.terminalMsgs <- msg:
		return nil
	case <-crane.ctx.Done():
		msg.Finish()
		return terminal.ErrCanceled
	}
}

func (crane *Crane) encrypt(shipment *container.Container) (encrypted *container.Container, err error) {
	// Skip if encryption is not enabled.
	if crane.jession == nil {
		return shipment, nil
	}

	crane.jessionLock.Lock()
	defer crane.jessionLock.Unlock()

	letter, err := crane.jession.Close(shipment.CompileData())
	if err != nil {
		return nil, err
	}

	encrypted, err = letter.ToWire()
	if err != nil {
		return nil, fmt.Errorf("failed to pack letter: %w", err)
	}

	return encrypted, nil
}

func (crane *Crane) decrypt(shipment *container.Container) (decrypted *container.Container, err error) {
	// Skip if encryption is not enabled.
	if crane.jession == nil {
		return shipment, nil
	}

	crane.jessionLock.Lock()
	defer crane.jessionLock.Unlock()

	letter, err := jess.LetterFromWire(shipment)
	if err != nil {
		return nil, fmt.Errorf("failed to parse letter: %w", err)
	}

	decryptedData, err := crane.jession.Open(letter)
	if err != nil {
		return nil, err
	}

	return container.New(decryptedData), nil
}

func (crane *Crane) unloader(workerCtx *mgr.WorkerCtx) error {
	// Unclean shutdown safeguard.
	defer crane.Stop(terminal.ErrUnknownError.With("unloader died"))

	for {
		// Get first couple bytes to get the packet length.
		// 2 bytes are enough to encode 65535.
		// On the other hand, packets can be only 2 bytes small.
		lenBuf := make([]byte, 2)
		err := crane.unloadUntilFull(lenBuf)
		if err != nil {
			if errors.Is(err, io.EOF) {
				crane.Stop(terminal.ErrStopping.With("connection closed"))
			} else {
				crane.Stop(terminal.ErrInternalError.With("failed to unload: %w", err))
			}
			return nil
		}

		// Unpack length.
		containerLen, n, err := varint.Unpack64(lenBuf)
		if err != nil {
			crane.Stop(terminal.ErrMalformedData.With("failed to get container length: %w", err))
			return nil
		}
		switch {
		case containerLen <= 0:
			crane.Stop(terminal.ErrMalformedData.With("received empty container with length %d", containerLen))
			return nil
		case containerLen > maxUnloadSize:
			crane.Stop(terminal.ErrMalformedData.With("received oversized container with length %d", containerLen))
			return nil
		}

		// Build shipment.
		var shipmentBuf []byte
		leftovers := len(lenBuf) - n

		if leftovers == int(containerLen) {
			// We already have all the shipment data.
			shipmentBuf = lenBuf[n:]
		} else {
			// Create a shipment buffer, copy leftovers and read the rest from the connection.
			shipmentBuf = make([]byte, containerLen)
			if leftovers > 0 {
				copy(shipmentBuf, lenBuf[n:])
			}

			// Read remaining shipment.
			err = crane.unloadUntilFull(shipmentBuf[leftovers:])
			if err != nil {
				crane.Stop(terminal.ErrInternalError.With("failed to unload: %w", err))
				return nil
			}
		}

		// Submit to handler.
		select {
		case <-crane.ctx.Done():
			crane.Stop(nil)
			return nil
		case crane.unloading <- container.New(shipmentBuf):
		}
	}
}

func (crane *Crane) unloadUntilFull(buf []byte) error {
	var bytesRead int
	for {
		// Get shipment from ship.
		n, err := crane.ship.UnloadTo(buf[bytesRead:])
		if err != nil {
			return err
		}
		if n == 0 {
			log.Tracef("spn/docks: %s unloaded 0 bytes", crane)
		}
		bytesRead += n

		// Return if buffer has been fully filled.
		if bytesRead == len(buf) {
			// Submit metrics.
			crane.submitCraneTrafficStats(bytesRead)
			crane.NetState.ReportTraffic(uint64(bytesRead), true)

			return nil
		}
	}
}

func (crane *Crane) handler(workerCtx *mgr.WorkerCtx) error {
	var partialShipment *container.Container
	var segmentLength uint32

	// Unclean shutdown safeguard.
	defer crane.Stop(terminal.ErrUnknownError.With("handler died"))

handling:
	for {
		select {
		case <-crane.ctx.Done():
			crane.Stop(nil)
			return nil

		case shipment := <-crane.unloading:
			// log.Debugf("spn/crane %s: before decrypt: %v ... %v", crane.ID, c.CompileData()[:10], c.CompileData()[c.Length()-10:])

			// Decrypt shipment.
			shipment, err := crane.decrypt(shipment)
			if err != nil {
				crane.Stop(terminal.ErrIntegrity.With("failed to decrypt: %w", err))
				return nil
			}

			// Process all segments/containers of the shipment.
			for shipment.HoldsData() {
				if partialShipment != nil {
					// Continue processing partial segment.
					// Append new shipment to previous partial segment.
					partialShipment.AppendContainer(shipment)
					shipment, partialShipment = partialShipment, nil
				}

				// Get next segment length.
				if segmentLength == 0 {
					segmentLength, err = shipment.GetNextN32()
					if err != nil {
						if errors.Is(err, varint.ErrBufTooSmall) {
							// Continue handling when there is not yet enough data.
							partialShipment = shipment
							segmentLength = 0
							continue handling
						}

						crane.Stop(terminal.ErrMalformedData.With("failed to get segment length: %w", err))
						return nil
					}

					if segmentLength == 0 {
						// Remainder is padding.
						continue handling
					}

					// Check if the segment is within the boundary.
					if segmentLength > maxSegmentLength {
						crane.Stop(terminal.ErrMalformedData.With("received oversized segment with length %d", segmentLength))
						return nil
					}
				}

				// Check if we have enough data for the segment.
				if uint32(shipment.Length()) < segmentLength {
					partialShipment = shipment
					continue handling
				}

				// Get segment from shipment.
				segment, err := shipment.GetAsContainer(int(segmentLength))
				if err != nil {
					crane.Stop(terminal.ErrMalformedData.With("failed to get segment: %w", err))
					return nil
				}
				segmentLength = 0

				// Get terminal ID and message type of segment.
				terminalID, terminalMsgType, err := terminal.ParseIDType(segment)
				if err != nil {
					crane.Stop(terminal.ErrMalformedData.With("failed to get terminal ID and msg type: %w", err))
					return nil
				}

				switch terminalMsgType {
				case terminal.MsgTypeInit:
					crane.establishTerminal(terminalID, segment)

				case terminal.MsgTypeData, terminal.MsgTypePriorityData:
					// Get terminal and let it further handle the message.
					t, ok := crane.getTerminal(terminalID)
					if ok {
						// Create msg and set priority.
						msg := terminal.NewEmptyMsg()
						msg.FlowID = terminalID
						msg.Type = terminalMsgType
						msg.Data = segment
						if msg.Type == terminal.MsgTypePriorityData {
							msg.Unit.MakeHighPriority()
						}
						// Deliver to terminal.
						deliveryErr := t.Deliver(msg)
						if deliveryErr != nil {
							msg.Finish()
							// This is a hot path. Start a worker for abandoning the terminal.
							module.mgr.Go("end terminal", func(_ *mgr.WorkerCtx) error {
								crane.AbandonTerminal(t.ID(), deliveryErr.Wrap("failed to deliver data"))
								return nil
							})
						}
					} else {
						log.Tracef("spn/docks: %s received msg for unknown terminal %d", crane, terminalID)
					}

				case terminal.MsgTypeStop:
					// Parse error.
					receivedErr, err := terminal.ParseExternalError(segment.CompileData())
					if err != nil {
						log.Warningf("spn/docks: %s failed to parse abandon error: %s", crane, err)
						receivedErr = terminal.ErrUnknownError.AsExternal()
					}
					// This is a hot path. Start a worker for abandoning the terminal.
					module.mgr.Go("end terminal", func(_ *mgr.WorkerCtx) error {
						crane.AbandonTerminal(terminalID, receivedErr)
						return nil
					})
				}
			}
		}
	}
}

func (crane *Crane) loader(workerCtx *mgr.WorkerCtx) (err error) {
	shipment := container.New()
	var partialShipment *container.Container
	var loadingTimer *time.Timer

	// Unclean shutdown safeguard.
	defer crane.Stop(terminal.ErrUnknownError.With("loader died"))

	// Return the loading wait channel if waiting.
	loadNow := func() <-chan time.Time {
		if loadingTimer != nil {
			return loadingTimer.C
		}
		return nil
	}

	// Make sure any received message is finished
	var msg, firstMsg *terminal.Msg
	defer msg.Finish()
	defer firstMsg.Finish()

	for {
		// Reset first message in shipment.
		firstMsg.Finish()
		firstMsg = nil

	fillingShipment:
		for shipment.Length() < crane.targetLoadSize {
			// Gather segments until shipment is filled.

			// Prioritize messages from the controller.
			select {
			case msg = <-crane.controllerMsgs:
			case <-crane.ctx.Done():
				crane.Stop(nil)
				return nil

			default:
				// Then listen for all.
				select {
				case msg = <-crane.controllerMsgs:
				case msg = <-crane.terminalMsgs:
				case <-loadNow():
					break fillingShipment
				case <-crane.ctx.Done():
					crane.Stop(nil)
					return nil
				}
			}

			// Debug unit leaks.
			msg.Debug()

			// Handle new message.
			if msg != nil {
				// Pack msg and add to segment.
				msg.Pack()
				newSegment := msg.Data

				// Check if this is the first message.
				// This is the only message where we wait for a slot.
				if firstMsg == nil {
					firstMsg = msg
					firstMsg.Unit.WaitForSlot()
				} else {
					msg.Finish()
				}

				// Check length.
				if newSegment.Length() > maxSegmentLength {
					log.Warningf("spn/docks: %s ignored oversized segment with length %d", crane, newSegment.Length())
					continue fillingShipment
				}

				// Append to shipment.
				shipment.AppendContainer(newSegment)

				// Set loading max wait timer on first segment.
				if loadingTimer == nil {
					loadingTimer = time.NewTimer(loadingMaxWaitDuration)
				}

			} else if crane.stopped.IsSet() {
				// If there is no new segment, this might have been triggered by a
				// closed channel. Check if the crane is still active.
				return nil
			}
		}

	sendingShipment:
		for {
			// Check if we are over the target load size and split the shipment.
			if shipment.Length() > crane.targetLoadSize {
				partialShipment, err = shipment.GetAsContainer(crane.targetLoadSize)
				if err != nil {
					crane.Stop(terminal.ErrInternalError.With("failed to split segment: %w", err))
					return nil
				}
				shipment, partialShipment = partialShipment, shipment
			}

			// Load shipment.
			err = crane.load(shipment)
			if err != nil {
				crane.Stop(terminal.ErrShipSunk.With("failed to load shipment: %w", err))
				return nil
			}

			// Reset loading timer.
			loadingTimer = nil

			// Continue loading with partial shipment, or a new one.
			if partialShipment != nil {
				// Continue loading with a partial previous shipment.
				shipment, partialShipment = partialShipment, nil

				// If shipment is not big enough to send immediately, wait for more data.
				if shipment.Length() < crane.targetLoadSize {
					loadingTimer = time.NewTimer(loadingMaxWaitDuration)
					break sendingShipment
				}

			} else {
				// Continue loading with new shipment.
				shipment = container.New()
				break sendingShipment
			}
		}
	}
}

func (crane *Crane) load(c *container.Container) error {
	// Add Padding if needed.
	if crane.opts.Padding > 0 {
		paddingNeeded := int(crane.opts.Padding) -
			((c.Length() + varint.EncodedSize(uint64(c.Length()))) % int(crane.opts.Padding))
		// As the length changes slightly with the padding, we should avoid loading
		// lengths around the varint size hops:
		// - 128
		// - 16384
		// - 2097152
		// - 268435456

		// Pad to target load size at maximum.
		maxPadding := crane.targetLoadSize - c.Length()
		if paddingNeeded > maxPadding {
			paddingNeeded = maxPadding
		}

		if paddingNeeded > 0 {
			// Add padding indicator.
			c.Append([]byte{0})
			paddingNeeded--

			// Add needed padding data.
			if paddingNeeded > 0 {
				padding, err := rng.Bytes(paddingNeeded)
				if err != nil {
					log.Debugf("spn/docks: %s failed to get random padding data, using zeros instead", crane)
					padding = make([]byte, paddingNeeded)
				}
				c.Append(padding)
			}
		}
	}

	// Encrypt shipment.
	c, err := crane.encrypt(c)
	if err != nil {
		return fmt.Errorf("failed to encrypt: %w", err)
	}

	// Finalize data.
	c.PrependLength()
	readyToSend := c.CompileData()

	// Submit metrics.
	crane.submitCraneTrafficStats(len(readyToSend))
	crane.NetState.ReportTraffic(uint64(len(readyToSend)), false)

	// Load onto ship.
	err = crane.ship.Load(readyToSend)
	if err != nil {
		return fmt.Errorf("failed to load ship: %w", err)
	}

	return nil
}

// Stop stops the crane.
func (crane *Crane) Stop(err *terminal.Error) {
	if !crane.stopped.SetToIf(false, true) {
		return
	}

	// Log error message.
	if err != nil {
		if err.IsOK() {
			log.Infof("spn/docks: %s is done", crane)
		} else {
			log.Warningf("spn/docks: %s is stopping: %s", crane, err)
		}
	}

	// Unregister crane.
	unregisterCrane(crane)

	// Stop all terminals.
	for _, t := range crane.allTerms() {
		t.Abandon(err) // Async!
	}

	// Stop controller.
	if crane.Controller != nil {
		crane.Controller.Abandon(err) // Async!
	}

	// Wait shortly for all terminals to finish abandoning.
	waitStep := 50 * time.Millisecond
	for i := time.Duration(0); i < maxCraneStopDuration; i += waitStep {
		// Check if all terminals are done.
		if crane.terminalCount() == 0 {
			break
		}

		time.Sleep(waitStep)
	}

	// Close connection.
	crane.ship.Sink()

	// Cancel crane context.
	crane.cancelCtx()

	// Notify about change.
	crane.NotifyUpdate()
}

func (crane *Crane) allTerms() []terminal.Terminal {
	crane.terminalsLock.Lock()
	defer crane.terminalsLock.Unlock()

	terms := make([]terminal.Terminal, 0, len(crane.terminals))
	for _, term := range crane.terminals {
		terms = append(terms, term)
	}

	return terms
}

func (crane *Crane) String() string {
	remoteAddr := crane.ship.RemoteAddr()
	switch {
	case remoteAddr == nil:
		return fmt.Sprintf("crane %s", crane.ID)
	case crane.ship.IsMine():
		return fmt.Sprintf("crane %s to %s", crane.ID, crane.ship.MaskAddress(crane.ship.RemoteAddr()))
	default:
		return fmt.Sprintf("crane %s from %s", crane.ID, crane.ship.MaskAddress(crane.ship.RemoteAddr()))
	}
}

// Stopped returns whether the crane has stopped.
func (crane *Crane) Stopped() bool {
	return crane.stopped.IsSet()
}
