package crew

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/network/netutils"
	"github.com/safing/portmaster/service/network/packet"
	"github.com/safing/portmaster/spn/conf"
	"github.com/safing/portmaster/spn/terminal"
	"github.com/safing/structures/container"
	"github.com/safing/structures/dsd"
)

// ConnectOpType is the type ID for the connection operation.
const ConnectOpType string = "connect"

var activeConnectOps = new(int64)

// ConnectOp is used to connect data tunnels to servers on the Internet.
type ConnectOp struct {
	terminal.OperationBase

	// Flow Control
	dfq *terminal.DuplexFlowQueue

	// Context and shutdown handling
	// ctx is the context of the Terminal.
	ctx context.Context
	// cancelCtx cancels ctx.
	cancelCtx context.CancelFunc
	// doneWriting signals that the writer has finished writing.
	doneWriting chan struct{}

	// Metrics
	incomingTraffic atomic.Uint64
	outgoingTraffic atomic.Uint64
	started         time.Time

	// Connection
	t       terminal.Terminal
	conn    net.Conn
	request *ConnectRequest
	entry   bool
	tunnel  *Tunnel
}

// Type returns the type ID.
func (op *ConnectOp) Type() string {
	return ConnectOpType
}

// Ctx returns the operation context.
func (op *ConnectOp) Ctx() context.Context {
	return op.ctx
}

// ConnectRequest holds all the information necessary for a connect operation.
type ConnectRequest struct {
	Domain              string            `json:"d,omitempty"`
	IP                  net.IP            `json:"ip,omitempty"`
	UsePriorityDataMsgs bool              `json:"pr,omitempty"`
	Protocol            packet.IPProtocol `json:"p,omitempty"`
	Port                uint16            `json:"po,omitempty"`
	QueueSize           uint32            `json:"qs,omitempty"`
}

// DialNetwork returns the address of the connect request.
func (r *ConnectRequest) DialNetwork() string {
	if ip4 := r.IP.To4(); ip4 != nil {
		switch r.Protocol { //nolint:exhaustive // Only looking for supported protocols.
		case packet.TCP:
			return "tcp4"
		case packet.UDP:
			return "udp4"
		}
	} else {
		switch r.Protocol { //nolint:exhaustive // Only looking for supported protocols.
		case packet.TCP:
			return "tcp6"
		case packet.UDP:
			return "udp6"
		}
	}

	return ""
}

// Address returns the address of the connext request.
func (r *ConnectRequest) Address() string {
	return net.JoinHostPort(r.IP.String(), strconv.Itoa(int(r.Port)))
}

func (r *ConnectRequest) String() string {
	if r.Domain != "" {
		return fmt.Sprintf("%s (%s %s)", r.Domain, r.Protocol, r.Address())
	}
	return fmt.Sprintf("%s %s", r.Protocol, r.Address())
}

func init() {
	terminal.RegisterOpType(terminal.OperationFactory{
		Type:     ConnectOpType,
		Requires: terminal.MayConnect,
		Start:    startConnectOp,
	})
}

// NewConnectOp starts a new connect operation.
func NewConnectOp(tunnel *Tunnel) (*ConnectOp, *terminal.Error) {
	// Submit metrics.
	connectOpCnt.Inc()

	// Create request.
	request := &ConnectRequest{
		Domain:              tunnel.connInfo.Entity.Domain,
		IP:                  tunnel.connInfo.Entity.IP,
		Protocol:            packet.IPProtocol(tunnel.connInfo.Entity.Protocol),
		Port:                tunnel.connInfo.Entity.Port,
		UsePriorityDataMsgs: terminal.UsePriorityDataMsgs,
	}

	// Set defaults.
	if request.QueueSize == 0 {
		request.QueueSize = terminal.DefaultQueueSize
	}

	// Create new op.
	op := &ConnectOp{
		doneWriting: make(chan struct{}),
		t:           tunnel.dstTerminal,
		conn:        tunnel.conn,
		request:     request,
		entry:       true,
		tunnel:      tunnel,
	}
	op.ctx, op.cancelCtx = context.WithCancel(module.mgr.Ctx())
	op.dfq = terminal.NewDuplexFlowQueue(op.Ctx(), request.QueueSize, op.submitUpstream)

	// Prepare init msg.
	data, err := dsd.Dump(request, dsd.CBOR)
	if err != nil {
		return nil, terminal.ErrInternalError.With("failed to pack connect request: %w", err)
	}

	// Initialize.
	tErr := op.t.StartOperation(op, container.New(data), 5*time.Second)
	if tErr != nil {
		return nil, tErr
	}

	// Setup metrics.
	op.started = time.Now()

	module.mgr.Go("connect op conn reader", op.connReader)
	module.mgr.Go("connect op conn writer", op.connWriter)
	module.mgr.Go("connect op flow handler", op.dfq.FlowHandler)

	log.Infof("spn/crew: connected to %s via %s", request, tunnel.dstPin.Hub)
	return op, nil
}

func startConnectOp(t terminal.Terminal, opID uint32, data *container.Container) (terminal.Operation, *terminal.Error) {
	// Check if we are running a public hub.
	if !conf.PublicHub() {
		return nil, terminal.ErrPermissionDenied.With("connecting is only allowed on public hubs")
	}

	// Parse connect request.
	request := &ConnectRequest{}
	_, err := dsd.Load(data.CompileData(), request)
	if err != nil {
		connectOpCntError.Inc() // More like a protocol/system error than a bad request.
		return nil, terminal.ErrMalformedData.With("failed to parse connect request: %w", err)
	}
	if request.QueueSize == 0 || request.QueueSize > terminal.MaxQueueSize {
		connectOpCntError.Inc() // More like a protocol/system error than a bad request.
		return nil, terminal.ErrInvalidOptions.With("invalid queue size of %d", request.QueueSize)
	}

	// Check if IP seems valid.
	if len(request.IP) != net.IPv4len && len(request.IP) != net.IPv6len {
		connectOpCntError.Inc() // More like a protocol/system error than a bad request.
		return nil, terminal.ErrInvalidOptions.With("ip address is not valid")
	}

	// Create and initialize operation.
	op := &ConnectOp{
		doneWriting: make(chan struct{}),
		t:           t,
		request:     request,
	}
	op.InitOperationBase(t, opID)
	op.ctx, op.cancelCtx = context.WithCancel(t.Ctx())
	op.dfq = terminal.NewDuplexFlowQueue(op.Ctx(), request.QueueSize, op.submitUpstream)

	// Start worker to complete setting up the connection.
	module.mgr.Go("connect op setup", op.handleSetup)

	return op, nil
}

func (op *ConnectOp) handleSetup(_ *mgr.WorkerCtx) error {
	// Get terminal session for rate limiting.
	var session *terminal.Session
	if sessionTerm, ok := op.t.(terminal.SessionTerminal); ok {
		session = sessionTerm.GetSession()
	} else {
		connectOpCntError.Inc()
		log.Errorf("spn/crew: %T is not a session terminal, aborting op %s#%d", op.t, op.t.FmtID(), op.ID())
		op.Stop(op, terminal.ErrInternalError.With("no session available"))
		return nil
	}

	// Limit concurrency of connecting.
	cancelErr := session.LimitConcurrency(op.Ctx(), func() {
		op.setup(session)
	})

	// If context was canceled, stop operation.
	if cancelErr != nil {
		connectOpCntCanceled.Inc()
		op.Stop(op, terminal.ErrCanceled.With(cancelErr.Error()))
	}

	// Do not return a worker error.
	return nil
}

func (op *ConnectOp) setup(session *terminal.Session) {
	// Rate limit before connecting.
	if tErr := session.RateLimit(); tErr != nil {
		// Add rate limit info to error.
		if tErr.Is(terminal.ErrRateLimited) {
			connectOpCntRateLimited.Inc()
			op.Stop(op, tErr.With(session.RateLimitInfo()))
			return
		}

		connectOpCntError.Inc()
		op.Stop(op, tErr)
		return
	}

	// Check if connection target is in global scope.
	ipScope := netutils.GetIPScope(op.request.IP)
	if ipScope != netutils.Global {
		session.ReportSuspiciousActivity(terminal.SusFactorQuiteUnusual)
		connectOpCntBadRequest.Inc()
		op.Stop(op, terminal.ErrPermissionDenied.With("denied request to connect to non-global IP %s", op.request.IP))
		return
	}

	// Check exit policy.
	if tErr := checkExitPolicy(op.request); tErr != nil {
		session.ReportSuspiciousActivity(terminal.SusFactorQuiteUnusual)
		connectOpCntBadRequest.Inc()
		op.Stop(op, tErr)
		return
	}

	// Check one last time before connecting if operation was not canceled.
	if op.Ctx().Err() != nil {
		op.Stop(op, terminal.ErrCanceled.With(op.Ctx().Err().Error()))
		connectOpCntCanceled.Inc()
		return
	}

	// Connect to destination.
	dialNet := op.request.DialNetwork()
	if dialNet == "" {
		session.ReportSuspiciousActivity(terminal.SusFactorCommon)
		connectOpCntBadRequest.Inc()
		op.Stop(op, terminal.ErrIncorrectUsage.With("protocol %s is not supported", op.request.Protocol))
		return
	}
	dialer := &net.Dialer{
		Timeout:       10 * time.Second,
		LocalAddr:     conf.GetBindAddr(dialNet),
		FallbackDelay: -1, // Disables Fast Fallback from IPv6 to IPv4.
		KeepAlive:     -1, // Disable keep-alive.
	}
	conn, err := dialer.DialContext(op.Ctx(), dialNet, op.request.Address())
	if err != nil {
		// Connection errors are common, but still a bit suspicious.
		var netError net.Error
		switch {
		case errors.As(err, &netError) && netError.Timeout():
			session.ReportSuspiciousActivity(terminal.SusFactorCommon)
			connectOpCntFailed.Inc()
		case errors.Is(err, context.Canceled):
			session.ReportSuspiciousActivity(terminal.SusFactorCommon)
			connectOpCntCanceled.Inc()
		default:
			session.ReportSuspiciousActivity(terminal.SusFactorWeirdButOK)
			connectOpCntFailed.Inc()
		}

		op.Stop(op, terminal.ErrConnectionError.With("failed to connect to %s: %w", op.request, err))
		return
	}
	op.conn = conn

	// Start worker.
	module.mgr.Go("connect op conn reader", op.connReader)
	module.mgr.Go("connect op conn writer", op.connWriter)
	module.mgr.Go("connect op flow handler", op.dfq.FlowHandler)

	connectOpCntConnected.Inc()
	log.Infof("spn/crew: connected op %s#%d to %s", op.t.FmtID(), op.ID(), op.request)
}

func (op *ConnectOp) submitUpstream(msg *terminal.Msg, timeout time.Duration) {
	err := op.Send(msg, timeout)
	if err != nil {
		msg.Finish()
		op.Stop(op, err.Wrap("failed to send data (op) read from %s", op.connectedType()))
	}
}

const (
	readBufSize = 1500

	// High priority up to first 10MB.
	highPrioThreshold = 10_000_000

	// Rate limit to 128 Mbit/s after 1GB traffic.
	// Do NOT use time.Sleep per packet, as it is very inaccurate and will sleep a lot longer than desired.
	rateLimitThreshold = 1_000_000_000
	rateLimitMaxMbit   = 128
)

func (op *ConnectOp) connReader(_ *mgr.WorkerCtx) error {
	// Metrics setup and submitting.
	atomic.AddInt64(activeConnectOps, 1)
	defer func() {
		atomic.AddInt64(activeConnectOps, -1)
		connectOpDurationHistogram.UpdateDuration(op.started)
		connectOpIncomingDataHistogram.Update(float64(op.incomingTraffic.Load()))
	}()

	rateLimiter := terminal.NewRateLimiter(rateLimitMaxMbit)

	for {
		// Read from connection.
		buf := make([]byte, readBufSize)
		n, err := op.conn.Read(buf)
		if err != nil {
			if errors.Is(err, io.EOF) {
				op.Stop(op, terminal.ErrStopping.With("connection to %s was closed on read", op.connectedType()))
			} else {
				op.Stop(op, terminal.ErrConnectionError.With("failed to read from %s: %w", op.connectedType(), err))
			}
			return nil
		}
		if n == 0 {
			log.Tracef("spn/crew: connect op %s>%d read 0 bytes from %s", op.t.FmtID(), op.ID(), op.connectedType())
			continue
		}

		// Submit metrics.
		connectOpIncomingBytes.Add(n)
		inBytes := op.incomingTraffic.Add(uint64(n))

		// Rate limit if over threshold.
		if inBytes > rateLimitThreshold {
			rateLimiter.Limit(uint64(n))
		}

		// Create message from data.
		msg := op.NewMsg(buf[:n])

		// Define priority and possibly wait for slot.
		switch {
		case inBytes > highPrioThreshold:
			msg.Unit.WaitForSlot()
		case op.request.UsePriorityDataMsgs:
			msg.Unit.MakeHighPriority()
		}

		// Send packet.
		tErr := op.dfq.Send(
			msg,
			30*time.Second,
		)
		if tErr != nil {
			msg.Finish()
			op.Stop(op, tErr.Wrap("failed to send data (dfq) from %s", op.connectedType()))
			return nil
		}
	}
}

// Deliver delivers a messages to the operation.
func (op *ConnectOp) Deliver(msg *terminal.Msg) *terminal.Error {
	return op.dfq.Deliver(msg)
}

func (op *ConnectOp) connWriter(_ *mgr.WorkerCtx) error {
	// Metrics submitting.
	defer func() {
		connectOpOutgoingDataHistogram.Update(float64(op.outgoingTraffic.Load()))
	}()

	defer func() {
		// Signal that we are done with writing.
		close(op.doneWriting)
		// Close connection.
		_ = op.conn.Close()
	}()

	var msg *terminal.Msg
	defer msg.Finish()

	rateLimiter := terminal.NewRateLimiter(rateLimitMaxMbit)

writing:
	for {
		msg.Finish()

		select {
		case msg = <-op.dfq.Receive():
		case <-op.ctx.Done():
			op.Stop(op, terminal.ErrCanceled)
			return nil
		default:
			// Handle all data before also listening for the context cancel.
			// This ensures all data is written properly before stopping.
			select {
			case msg = <-op.dfq.Receive():
			case op.doneWriting <- struct{}{}:
				op.Stop(op, terminal.ErrStopping)
				return nil
			case <-op.ctx.Done():
				op.Stop(op, terminal.ErrCanceled)
				return nil
			}
		}

		// TODO: Instead of compiling data here again, can we send it as in the container?
		data := msg.Data.CompileData()
		if len(data) == 0 {
			continue writing
		}

		// Submit metrics.
		connectOpOutgoingBytes.Add(len(data))
		out := op.outgoingTraffic.Add(uint64(len(data)))

		// Rate limit if over threshold.
		if out > rateLimitThreshold {
			rateLimiter.Limit(uint64(len(data)))
		}

		// Special handling after first data was received on client.
		if op.entry &&
			out == uint64(len(data)) {
			// Report time taken to receive first byte.
			connectOpTTFBDurationHistogram.UpdateDuration(op.started)

			// If not stickied yet, stick destination to Hub.
			if !op.tunnel.stickied {
				op.tunnel.stickDestinationToHub()
			}
		}

		// Send all given data.
		for {
			n, err := op.conn.Write(data)
			switch {
			case err != nil:
				if errors.Is(err, io.EOF) {
					op.Stop(op, terminal.ErrStopping.With("connection to %s was closed on write", op.connectedType()))
				} else {
					op.Stop(op, terminal.ErrConnectionError.With("failed to send to %s: %w", op.connectedType(), err))
				}
				return nil
			case n == 0:
				op.Stop(op, terminal.ErrConnectionError.With("sent 0 bytes to %s", op.connectedType()))
				return nil
			case n < len(data):
				// If not all data was sent, try again.
				log.Debugf("spn/crew: %s#%d only sent %d/%d bytes to %s", op.t.FmtID(), op.ID(), n, len(data), op.connectedType())
				data = data[n:]
			default:
				continue writing
			}
		}
	}
}

func (op *ConnectOp) connectedType() string {
	if op.entry {
		return "origin"
	}
	return "destination"
}

// HandleStop gives the operation the ability to cleanly shut down.
// The returned error is the error to send to the other side.
// Should never be called directly. Call Stop() instead.
func (op *ConnectOp) HandleStop(err *terminal.Error) (errorToSend *terminal.Error) {
	if err.IsError() {
		reportConnectError(err)
	}

	// If the connection has sent or received any data so far, finish the data
	// flows as it makes sense.
	if op.incomingTraffic.Load() > 0 || op.outgoingTraffic.Load() > 0 {
		// If the op was ended locally, send all data before closing.
		// If the op was ended remotely, don't bother sending remaining data.
		if !err.IsExternal() {
			// Flushing could mean sending a full buffer of 50000 packets.
			op.dfq.Flush(5 * time.Minute)
		}

		// If the op was ended remotely, write all remaining received data.
		// If the op was ended locally, don't bother writing remaining data.
		if err.IsExternal() {
			select {
			case <-op.doneWriting:
			default:
				select {
				case <-op.doneWriting:
				case <-time.After(5 * time.Second):
				}
			}
		}
	}

	// Cancel workers.
	op.cancelCtx()

	// Special client-side handling.
	if op.entry {
		// Mark the connection as failed if there was an error and no data was sent to the app yet.
		if err.IsError() && op.outgoingTraffic.Load() == 0 {
			// Set connection to failed and save it to propagate the update.
			c := op.tunnel.connInfo
			func() {
				c.Lock()
				defer c.Unlock()

				if err.IsExternal() {
					c.Failed(fmt.Sprintf(
						"the exit node reported an error: %s", err,
					), "")
				} else {
					c.Failed(fmt.Sprintf(
						"connection failed locally: %s", err,
					), "")
				}

				c.Save()
			}()
		}

		// Avoid connecting to the destination via this Hub if:
		// - The error is external - ie. from the server.
		// - The error is a connection error.
		// - No data was received.
		// This indicates that there is some network level issue that we can
		// possibly work around by using another exit node.
		if err.IsError() && err.IsExternal() &&
			err.Is(terminal.ErrConnectionError) &&
			op.outgoingTraffic.Load() == 0 {
			op.tunnel.avoidDestinationHub()
		}

		// Don't leak local errors to the server.
		if !err.IsExternal() {
			// Change error that is reported.
			return terminal.ErrStopping
		}
	}

	return err
}
