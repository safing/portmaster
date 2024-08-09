package terminal

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/structures/varint"
)

// FlowControl defines the flow control interface.
type FlowControl interface {
	Deliver(msg *Msg) *Error
	Receive() <-chan *Msg
	Send(msg *Msg, timeout time.Duration) *Error
	ReadyToSend() <-chan struct{}
	Flush(timeout time.Duration)
	StartWorkers(m *mgr.Manager, terminalName string)
	RecvQueueLen() int
	SendQueueLen() int
}

// FlowControlType represents a flow control type.
type FlowControlType uint8

// Flow Control Types.
const (
	FlowControlDefault FlowControlType = 0
	FlowControlDFQ     FlowControlType = 1
	FlowControlNone    FlowControlType = 2

	defaultFlowControl = FlowControlDFQ
)

// DefaultSize returns the default flow control size.
func (fct FlowControlType) DefaultSize() uint32 {
	if fct == FlowControlDefault {
		fct = defaultFlowControl
	}

	switch fct {
	case FlowControlDFQ:
		return 50000
	case FlowControlNone:
		return 10000
	case FlowControlDefault:
		fallthrough
	default:
		return 0
	}
}

// Flow Queue Configuration.
const (
	DefaultQueueSize        = 50000
	MaxQueueSize            = 1000000
	forceReportBelowPercent = 0.75
)

// DuplexFlowQueue is a duplex flow control mechanism using queues.
type DuplexFlowQueue struct {
	// ti is the Terminal that is using the DFQ.
	ctx context.Context

	// submitUpstream is used to submit messages to the upstream channel.
	submitUpstream func(msg *Msg, timeout time.Duration)

	// sendQueue holds the messages that are waiting to be sent.
	sendQueue chan *Msg
	// prioMsgs holds the number of messages to send with high priority.
	prioMsgs *int32
	// sendSpace indicates the amount free slots in the recvQueue on the other end.
	sendSpace *int32
	// readyToSend is used to notify sending components that there is free space.
	readyToSend chan struct{}
	// wakeSender is used to wake a sender in case the sendSpace was zero and the
	// sender is waiting for available space.
	wakeSender chan struct{}

	// recvQueue holds the messages that are waiting to be processed.
	recvQueue chan *Msg
	// reportedSpace indicates the amount of free slots that the other end knows
	// about.
	reportedSpace *int32
	// spaceReportLock locks the calculation of space to report.
	spaceReportLock sync.Mutex
	// forceSpaceReport forces the sender to send a space report.
	forceSpaceReport chan struct{}

	// flush is used to send a finish function to the handler, which will write
	// all pending messages and then call the received function.
	flush chan func()
}

// NewDuplexFlowQueue returns a new duplex flow queue.
func NewDuplexFlowQueue(
	ctx context.Context,
	queueSize uint32,
	submitUpstream func(msg *Msg, timeout time.Duration),
) *DuplexFlowQueue {
	dfq := &DuplexFlowQueue{
		ctx:              ctx,
		submitUpstream:   submitUpstream,
		sendQueue:        make(chan *Msg, queueSize),
		prioMsgs:         new(int32),
		sendSpace:        new(int32),
		readyToSend:      make(chan struct{}),
		wakeSender:       make(chan struct{}, 1),
		recvQueue:        make(chan *Msg, queueSize),
		reportedSpace:    new(int32),
		forceSpaceReport: make(chan struct{}, 1),
		flush:            make(chan func()),
	}
	atomic.StoreInt32(dfq.sendSpace, int32(queueSize))
	atomic.StoreInt32(dfq.reportedSpace, int32(queueSize))

	return dfq
}

// StartWorkers starts the necessary workers to operate the flow queue.
func (dfq *DuplexFlowQueue) StartWorkers(m *mgr.Manager, terminalName string) {
	m.Go(terminalName+" flow queue", dfq.FlowHandler)
}

// shouldReportRecvSpace returns whether the receive space should be reported.
func (dfq *DuplexFlowQueue) shouldReportRecvSpace() bool {
	return atomic.LoadInt32(dfq.reportedSpace) < int32(float32(cap(dfq.recvQueue))*forceReportBelowPercent)
}

// decrementReportedRecvSpace decreases the reported recv space by 1 and
// returns if the receive space should be reported.
func (dfq *DuplexFlowQueue) decrementReportedRecvSpace() (shouldReportRecvSpace bool) {
	return atomic.AddInt32(dfq.reportedSpace, -1) < int32(float32(cap(dfq.recvQueue))*forceReportBelowPercent)
}

// getSendSpace returns the current send space.
func (dfq *DuplexFlowQueue) getSendSpace() int32 {
	return atomic.LoadInt32(dfq.sendSpace)
}

// decrementSendSpace decreases the send space by 1 and returns it.
func (dfq *DuplexFlowQueue) decrementSendSpace() int32 {
	return atomic.AddInt32(dfq.sendSpace, -1)
}

func (dfq *DuplexFlowQueue) addToSendSpace(n int32) {
	// Add new space to send space and check if it was zero.
	atomic.AddInt32(dfq.sendSpace, n)
	// Wake the sender in case it is waiting.
	select {
	case dfq.wakeSender <- struct{}{}:
	default:
	}
}

// reportableRecvSpace returns how much free space can be reported to the other
// end. The returned number must be communicated to the other end and must not
// be ignored.
func (dfq *DuplexFlowQueue) reportableRecvSpace() int32 {
	// Changes to the recvQueue during calculation are no problem.
	// We don't want to report space twice though!
	dfq.spaceReportLock.Lock()
	defer dfq.spaceReportLock.Unlock()

	// Calculate reportable receive space and add it to the reported space.
	reportedSpace := atomic.LoadInt32(dfq.reportedSpace)
	toReport := int32(cap(dfq.recvQueue)-len(dfq.recvQueue)) - reportedSpace

	// Never report values below zero.
	// This can happen, as dfq.reportedSpace is decreased after a container is
	// submitted to dfq.recvQueue by dfq.Deliver(). This race condition can only
	// lower the space to report, not increase it. A simple check here solved
	// this problem and keeps performance high.
	// Also, don't report values of 1, as the benefit is minimal and this might
	// be commonly triggered due to the buffer of the force report channel.
	if toReport <= 1 {
		return 0
	}

	// Add space to report to dfq.reportedSpace and return it.
	atomic.AddInt32(dfq.reportedSpace, toReport)
	return toReport
}

// FlowHandler handles all flow queue internals and must be started as a worker
// in the module where it is used.
func (dfq *DuplexFlowQueue) FlowHandler(_ *mgr.WorkerCtx) error {
	// The upstreamSender is started by the terminal module, but is tied to the
	// flow owner instead. Make sure that the flow owner's module depends on the
	// terminal module so that it is shut down earlier.

	var sendSpaceDepleted bool
	var flushFinished func()

	// Drain all queues when shutting down.
	defer func() {
		for {
			select {
			case msg := <-dfq.sendQueue:
				msg.Finish()
			case msg := <-dfq.recvQueue:
				msg.Finish()
			default:
				return
			}
		}
	}()

sending:
	for {
		// If the send queue is depleted, wait to be woken.
		if sendSpaceDepleted {
			select {
			case <-dfq.wakeSender:
				if dfq.getSendSpace() > 0 {
					sendSpaceDepleted = false
				} else {
					continue sending
				}

			case <-dfq.forceSpaceReport:
				// Forced reporting of space.
				// We do not need to check if there is enough sending space, as there is
				// no data included.
				spaceToReport := dfq.reportableRecvSpace()
				if spaceToReport > 0 {
					msg := NewMsg(varint.Pack64(uint64(spaceToReport)))
					dfq.submitUpstream(msg, 0)
				}
				continue sending

			case <-dfq.ctx.Done():
				return nil
			}
		}

		// Get message from send queue.

		select {
		case dfq.readyToSend <- struct{}{}:
			// Notify that we are ready to send.

		case msg := <-dfq.sendQueue:
			// Send message from queue.

			// If nil, the queue is being shut down.
			if msg == nil {
				return nil
			}

			// Check if we are handling a high priority message or waiting for one.
			// Mark any msgs as high priority, when there is one in the pipeline.
			remainingPrioMsgs := atomic.AddInt32(dfq.prioMsgs, -1)
			switch {
			case remainingPrioMsgs >= 0:
				msg.Unit.MakeHighPriority()
			case remainingPrioMsgs < -30_000:
				// Prevent wrap to positive.
				// Compatible with int16 or bigger.
				atomic.StoreInt32(dfq.prioMsgs, 0)
			}

			// Wait for processing slot.
			msg.Unit.WaitForSlot()

			// Prepend available receiving space.
			msg.Data.Prepend(varint.Pack64(uint64(dfq.reportableRecvSpace())))

			// Submit for sending upstream.
			dfq.submitUpstream(msg, 0)
			// Decrease the send space and set flag if depleted.
			if dfq.decrementSendSpace() <= 0 {
				sendSpaceDepleted = true
			}

			// Check if the send queue is empty now and signal flushers.
			if flushFinished != nil && len(dfq.sendQueue) == 0 {
				flushFinished()
				flushFinished = nil
			}

		case <-dfq.forceSpaceReport:
			// Forced reporting of space.
			// We do not need to check if there is enough sending space, as there is
			// no data included.
			spaceToReport := dfq.reportableRecvSpace()
			if spaceToReport > 0 {
				msg := NewMsg(varint.Pack64(uint64(spaceToReport)))
				dfq.submitUpstream(msg, 0)
			}

		case newFlushFinishedFn := <-dfq.flush:
			// Signal immediately if send queue is empty.
			if len(dfq.sendQueue) == 0 {
				newFlushFinishedFn()
			} else {
				// If there already is a flush finished function, stack them.
				if flushFinished != nil {
					stackedFlushFinishFn := flushFinished
					flushFinished = func() {
						stackedFlushFinishFn()
						newFlushFinishedFn()
					}
				} else {
					flushFinished = newFlushFinishedFn
				}
			}

		case <-dfq.ctx.Done():
			return nil
		}
	}
}

// Flush waits for all waiting data to be sent.
func (dfq *DuplexFlowQueue) Flush(timeout time.Duration) {
	// Create channel and function for notifying.
	wait := make(chan struct{})
	finished := func() {
		close(wait)
	}
	// Request flush and return when stopping.
	select {
	case dfq.flush <- finished:
	case <-dfq.ctx.Done():
		return
	case <-TimedOut(timeout):
		return
	}
	// Wait for flush to finish and return when stopping.
	select {
	case <-wait:
	case <-dfq.ctx.Done():
	case <-TimedOut(timeout):
	}
}

var ready = make(chan struct{})

func init() {
	close(ready)
}

// ReadyToSend returns a channel that can be read when data can be sent.
func (dfq *DuplexFlowQueue) ReadyToSend() <-chan struct{} {
	if atomic.LoadInt32(dfq.sendSpace) > 0 {
		return ready
	}
	return dfq.readyToSend
}

// Send adds the given container to the send queue.
func (dfq *DuplexFlowQueue) Send(msg *Msg, timeout time.Duration) *Error {
	select {
	case dfq.sendQueue <- msg:
		if msg.Unit.IsHighPriority() {
			// Reset prioMsgs to the current queue size, so that all waiting and the
			// message we just added are all handled as high priority.
			atomic.StoreInt32(dfq.prioMsgs, int32(len(dfq.sendQueue)))
		}
		return nil

	case <-TimedOut(timeout):
		msg.Finish()
		return ErrTimeout

	case <-dfq.ctx.Done():
		msg.Finish()
		return ErrStopping
	}
}

// Receive receives a container from the recv queue.
func (dfq *DuplexFlowQueue) Receive() <-chan *Msg {
	// If the reported recv space is nearing its end, force a report.
	if dfq.shouldReportRecvSpace() {
		select {
		case dfq.forceSpaceReport <- struct{}{}:
		default:
		}
	}

	return dfq.recvQueue
}

// Deliver submits a container for receiving from upstream.
func (dfq *DuplexFlowQueue) Deliver(msg *Msg) *Error {
	// Ignore nil containers.
	if msg == nil || msg.Data == nil {
		msg.Finish()
		return ErrMalformedData.With("no data")
	}

	// Get and add new reported space.
	addSpace, err := msg.Data.GetNextN16()
	if err != nil {
		msg.Finish()
		return ErrMalformedData.With("failed to parse reported space: %w", err)
	}
	if addSpace > 0 {
		dfq.addToSendSpace(int32(addSpace))
	}
	// Abort processing if the container only contained a space update.
	if !msg.Data.HoldsData() {
		msg.Finish()
		return nil
	}

	select {
	case dfq.recvQueue <- msg:

		// If the recv queue accepted the Container, decrement the recv space.
		shouldReportRecvSpace := dfq.decrementReportedRecvSpace()
		// If the reported recv space is nearing its end, force a report, if the
		// sender worker is idle.
		if shouldReportRecvSpace {
			select {
			case dfq.forceSpaceReport <- struct{}{}:
			default:
			}
		}

		return nil
	default:
		// If the recv queue is full, return an error.
		// The whole point of the flow queue is to guarantee that this never happens.
		msg.Finish()
		return ErrQueueOverflow
	}
}

// FlowStats returns a k=v formatted string of internal stats.
func (dfq *DuplexFlowQueue) FlowStats() string {
	return fmt.Sprintf(
		"sq=%d rq=%d sends=%d reps=%d",
		len(dfq.sendQueue),
		len(dfq.recvQueue),
		atomic.LoadInt32(dfq.sendSpace),
		atomic.LoadInt32(dfq.reportedSpace),
	)
}

// RecvQueueLen returns the current length of the receive queue.
func (dfq *DuplexFlowQueue) RecvQueueLen() int {
	return len(dfq.recvQueue)
}

// SendQueueLen returns the current length of the send queue.
func (dfq *DuplexFlowQueue) SendQueueLen() int {
	return len(dfq.sendQueue)
}
