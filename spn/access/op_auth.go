package access

import (
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/spn/access/token"
	"github.com/safing/portmaster/spn/terminal"
	"github.com/safing/structures/container"
)

// OpTypeAccessCodeAuth is the type ID of the auth operation.
const OpTypeAccessCodeAuth = "auth"

func init() {
	terminal.RegisterOpType(terminal.OperationFactory{
		Type:  OpTypeAccessCodeAuth,
		Start: checkAccessCode,
	})
}

// AuthorizeOp is used to authorize a session.
type AuthorizeOp struct {
	terminal.OneOffOperationBase
}

// Type returns the type ID.
func (op *AuthorizeOp) Type() string {
	return OpTypeAccessCodeAuth
}

// AuthorizeToTerminal starts an authorization operation.
func AuthorizeToTerminal(t terminal.Terminal) (*AuthorizeOp, *terminal.Error) {
	op := &AuthorizeOp{}
	op.Init()

	newToken, err := GetToken(ExpandAndConnectZones)
	if err != nil {
		return nil, terminal.ErrInternalError.With("failed to get access token: %w", err)
	}

	tErr := t.StartOperation(op, container.New(newToken.Raw()), 10*time.Second)
	if tErr != nil {
		return nil, terminal.ErrInternalError.With("failed to init auth op: %w", tErr)
	}

	return op, nil
}

func checkAccessCode(t terminal.Terminal, opID uint32, initData *container.Container) (terminal.Operation, *terminal.Error) {
	// Parse provided access token.
	receivedToken, err := token.ParseRawToken(initData.CompileData())
	if err != nil {
		return nil, terminal.ErrMalformedData.With("failed to parse access token: %w", err)
	}

	// Check if token is valid.
	granted, err := VerifyToken(receivedToken)
	if err != nil {
		return nil, terminal.ErrPermissionDenied.With("invalid access token: %w", err)
	}

	// Get the authorizing terminal for applying the granted permission.
	authTerm, ok := t.(terminal.AuthorizingTerminal)
	if !ok {
		return nil, terminal.ErrIncorrectUsage.With("terminal does not handle authorization")
	}

	// Grant permissions.
	authTerm.GrantPermission(granted)
	log.Debugf("spn/access: granted %s permissions via %s zone", t.FmtID(), receivedToken.Zone)

	// End successfully.
	return nil, terminal.ErrExplicitAck
}
