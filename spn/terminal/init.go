package terminal

import (
	"context"

	"github.com/safing/jess"
	"github.com/safing/portmaster/spn/cabin"
	"github.com/safing/portmaster/spn/hub"
	"github.com/safing/structures/container"
	"github.com/safing/structures/dsd"
	"github.com/safing/structures/varint"
)

/*

Terminal Init Message Format:

- Version [varint]
- Data Block [bytes; not blocked]
	- TerminalOpts as DSD

*/

const (
	minSupportedTerminalVersion = 1
	maxSupportedTerminalVersion = 1
)

// TerminalOpts holds configuration for the terminal.
type TerminalOpts struct { //nolint:golint,maligned // TODO: Rename.
	Version uint8  `json:"-"`
	Encrypt bool   `json:"e,omitempty"`
	Padding uint16 `json:"p,omitempty"`

	FlowControl     FlowControlType `json:"fc,omitempty"`
	FlowControlSize uint32          `json:"qs,omitempty"` // Previously was "QueueSize".

	UsePriorityDataMsgs bool `json:"pr,omitempty"`
}

// ParseTerminalOpts parses terminal options from the container and checks if
// they are valid.
func ParseTerminalOpts(c *container.Container) (*TerminalOpts, *Error) {
	// Parse and check version.
	version, err := c.GetNextN8()
	if err != nil {
		return nil, ErrMalformedData.With("failed to parse version: %w", err)
	}
	if version < minSupportedTerminalVersion || version > maxSupportedTerminalVersion {
		return nil, ErrUnsupportedVersion.With("requested terminal version %d", version)
	}

	// Parse init message.
	initMsg := &TerminalOpts{}
	_, err = dsd.Load(c.CompileData(), initMsg)
	if err != nil {
		return nil, ErrMalformedData.With("failed to parse init message: %w", err)
	}
	initMsg.Version = version

	// Check if options are valid.
	tErr := initMsg.Check(false)
	if tErr != nil {
		return nil, tErr
	}

	return initMsg, nil
}

// Pack serialized the terminal options and checks if they are valid.
func (opts *TerminalOpts) Pack() (*container.Container, *Error) {
	// Check if options are valid.
	tErr := opts.Check(true)
	if tErr != nil {
		return nil, tErr
	}

	// Pack init message.
	optsData, err := dsd.Dump(opts, dsd.CBOR)
	if err != nil {
		return nil, ErrInternalError.With("failed to pack init message: %w", err)
	}

	// Compile init message.
	return container.New(
		varint.Pack8(opts.Version),
		optsData,
	), nil
}

// Check checks if terminal options are valid.
func (opts *TerminalOpts) Check(useDefaultsForRequired bool) *Error {
	// Version is required - use default when permitted.
	if opts.Version == 0 && useDefaultsForRequired {
		opts.Version = 1
	}
	if opts.Version < minSupportedTerminalVersion || opts.Version > maxSupportedTerminalVersion {
		return ErrInvalidOptions.With("unsupported terminal version %d", opts.Version)
	}

	// FlowControl is optional.
	switch opts.FlowControl {
	case FlowControlDefault:
		// Set to default flow control.
		opts.FlowControl = defaultFlowControl
	case FlowControlNone, FlowControlDFQ:
		// Ok.
	default:
		return ErrInvalidOptions.With("unknown flow control type: %d", opts.FlowControl)
	}

	// FlowControlSize is required as it needs to be same on both sides.
	// Use default when permitted.
	if opts.FlowControlSize == 0 && useDefaultsForRequired {
		opts.FlowControlSize = opts.FlowControl.DefaultSize()
	}
	if opts.FlowControlSize <= 0 || opts.FlowControlSize > MaxQueueSize {
		return ErrInvalidOptions.With("invalid flow control size of %d", opts.FlowControlSize)
	}

	return nil
}

// NewLocalBaseTerminal creates a new local terminal base for use with inheriting terminals.
func NewLocalBaseTerminal(
	ctx context.Context,
	id uint32,
	parentID string,
	remoteHub *hub.Hub,
	initMsg *TerminalOpts,
	upstream Upstream,
) (
	t *TerminalBase,
	initData *container.Container,
	err *Error,
) {
	// Pack, check and add defaults to init message.
	initData, err = initMsg.Pack()
	if err != nil {
		return nil, nil, err
	}

	// Create baseline.
	t, err = createTerminalBase(ctx, id, parentID, false, initMsg, upstream)
	if err != nil {
		return nil, nil, err
	}

	// Setup encryption if enabled.
	if remoteHub != nil {
		initMsg.Encrypt = true

		// Select signet (public key) of remote Hub to use.
		s := remoteHub.SelectSignet()
		if s == nil {
			return nil, nil, ErrHubNotReady.With("failed to select signet of remote hub")
		}

		// Create new session.
		env := jess.NewUnconfiguredEnvelope()
		env.SuiteID = jess.SuiteWireV1
		env.Recipients = []*jess.Signet{s}
		jession, err := env.WireCorrespondence(nil)
		if err != nil {
			return nil, nil, ErrIntegrity.With("failed to initialize encryption: %w", err)
		}
		t.jession = jession

		// Encryption is ready for sending.
		close(t.encryptionReady)
	}

	return t, initData, nil
}

// NewRemoteBaseTerminal creates a new remote terminal base for use with inheriting terminals.
func NewRemoteBaseTerminal(
	ctx context.Context,
	id uint32,
	parentID string,
	identity *cabin.Identity,
	initData *container.Container,
	upstream Upstream,
) (
	t *TerminalBase,
	initMsg *TerminalOpts,
	err *Error,
) {
	// Parse init message.
	initMsg, err = ParseTerminalOpts(initData)
	if err != nil {
		return nil, nil, err
	}

	// Create baseline.
	t, err = createTerminalBase(ctx, id, parentID, true, initMsg, upstream)
	if err != nil {
		return nil, nil, err
	}

	// Setup encryption if enabled.
	if initMsg.Encrypt {
		if identity == nil {
			return nil, nil, ErrInternalError.With("missing identity for setting up incoming encryption")
		}
		t.identity = identity
	}

	return t, initMsg, nil
}
