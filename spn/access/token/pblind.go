package token

import (
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"fmt"
	"math"
	"math/big"
	mrand "math/rand"
	"sync"

	"github.com/mr-tron/base58"
	"github.com/rot256/pblind"

	"github.com/safing/structures/container"
	"github.com/safing/structures/dsd"
)

const pblindSecretSize = 32

// PBlindToken is token based on the pblind library.
type PBlindToken struct {
	Serial    int               `json:"N,omitempty"`
	Token     []byte            `json:"T,omitempty"`
	Signature *pblind.Signature `json:"S,omitempty"`
}

// Pack packs the token.
func (pbt *PBlindToken) Pack() ([]byte, error) {
	return dsd.Dump(pbt, dsd.CBOR)
}

// UnpackPBlindToken unpacks the token.
func UnpackPBlindToken(token []byte) (*PBlindToken, error) {
	t := &PBlindToken{}

	_, err := dsd.Load(token, t)
	if err != nil {
		return nil, err
	}

	return t, nil
}

// PBlindHandler is a handler for the pblind tokens.
type PBlindHandler struct {
	sync.Mutex
	opts *PBlindOptions

	publicKey  *pblind.PublicKey
	privateKey *pblind.SecretKey

	storageLock sync.Mutex
	Storage     []*PBlindToken

	// Client request state.
	requestStateLock sync.Mutex
	requestState     []RequestState
}

// PBlindOptions are options for the PBlindHandler.
type PBlindOptions struct {
	Zone                  string
	CurveName             string
	Curve                 elliptic.Curve
	PublicKey             string
	PrivateKey            string
	BatchSize             int
	UseSerials            bool
	RandomizeOrder        bool
	Fallback              bool
	SignalShouldRequest   func(Handler)
	DoubleSpendProtection func([]byte) error
}

// PBlindSignerState is a signer state.
type PBlindSignerState struct {
	signers []*pblind.StateSigner
}

// PBlindSetupResponse is a setup response.
type PBlindSetupResponse struct {
	Msgs []*pblind.Message1
}

// PBlindTokenRequest is a token request.
type PBlindTokenRequest struct {
	Msgs []*pblind.Message2
}

// IssuedPBlindTokens are issued pblind tokens.
type IssuedPBlindTokens struct {
	Msgs []*pblind.Message3
}

// RequestState is a request state.
type RequestState struct {
	Token []byte
	State *pblind.StateRequester
}

// NewPBlindHandler creates a new pblind handler.
func NewPBlindHandler(opts PBlindOptions) (*PBlindHandler, error) {
	pbh := &PBlindHandler{
		opts: &opts,
	}

	// Check curve, get from name.
	if opts.Curve == nil {
		switch opts.CurveName {
		case "P-256":
			opts.Curve = elliptic.P256()
		case "P-384":
			opts.Curve = elliptic.P384()
		case "P-521":
			opts.Curve = elliptic.P521()
		default:
			return nil, errors.New("no curve supplied")
		}
	} else if opts.CurveName != "" {
		return nil, errors.New("both curve and curve name supplied")
	}

	// Load keys.
	switch {
	case pbh.opts.PrivateKey != "":
		keyData, err := base58.Decode(pbh.opts.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("failed to decode private key: %w", err)
		}
		pivateKey := pblind.SecretKeyFromBytes(pbh.opts.Curve, keyData)
		pbh.privateKey = &pivateKey
		publicKey := pbh.privateKey.GetPublicKey()
		pbh.publicKey = &publicKey

		// Check public key if also provided.
		if pbh.opts.PublicKey != "" {
			if pbh.opts.PublicKey != base58.Encode(pbh.publicKey.Bytes()) {
				return nil, errors.New("private and public mismatch")
			}
		}

	case pbh.opts.PublicKey != "":
		keyData, err := base58.Decode(pbh.opts.PublicKey)
		if err != nil {
			return nil, fmt.Errorf("failed to decode public key: %w", err)
		}
		publicKey, err := pblind.PublicKeyFromBytes(pbh.opts.Curve, keyData)
		if err != nil {
			return nil, fmt.Errorf("failed to decode public key: %w", err)
		}
		pbh.publicKey = &publicKey

	default:
		return nil, errors.New("no key supplied")
	}

	return pbh, nil
}

func (pbh *PBlindHandler) makeInfo(serial int) (*pblind.Info, error) {
	// Gather data for info.
	infoData := container.New()
	infoData.AppendAsBlock([]byte(pbh.opts.Zone))
	if pbh.opts.UseSerials {
		infoData.AppendInt(serial)
	}

	// Compress to point.
	info, err := pblind.CompressInfo(pbh.opts.Curve, infoData.CompileData())
	if err != nil {
		return nil, fmt.Errorf("failed to compress info: %w", err)
	}

	return &info, nil
}

// Zone returns the zone name.
func (pbh *PBlindHandler) Zone() string {
	return pbh.opts.Zone
}

// ShouldRequest returns whether the new tokens should be requested.
func (pbh *PBlindHandler) ShouldRequest() bool {
	pbh.storageLock.Lock()
	defer pbh.storageLock.Unlock()

	return pbh.shouldRequest()
}

func (pbh *PBlindHandler) shouldRequest() bool {
	// Return true if storage is at or below 10%.
	return len(pbh.Storage) == 0 || pbh.opts.BatchSize/len(pbh.Storage) > 10
}

// Amount returns the current amount of tokens in this handler.
func (pbh *PBlindHandler) Amount() int {
	pbh.storageLock.Lock()
	defer pbh.storageLock.Unlock()

	return len(pbh.Storage)
}

// IsFallback returns whether this handler should only be used as a fallback.
func (pbh *PBlindHandler) IsFallback() bool {
	return pbh.opts.Fallback
}

// CreateSetup sets up signers for a request.
func (pbh *PBlindHandler) CreateSetup() (state *PBlindSignerState, setupResponse *PBlindSetupResponse, err error) {
	state = &PBlindSignerState{
		signers: make([]*pblind.StateSigner, pbh.opts.BatchSize),
	}
	setupResponse = &PBlindSetupResponse{
		Msgs: make([]*pblind.Message1, pbh.opts.BatchSize),
	}

	// Go through the batch.
	for i := range pbh.opts.BatchSize {
		info, err := pbh.makeInfo(i + 1)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create info #%d: %w", i, err)
		}

		// Create signer.
		signer, err := pblind.CreateSigner(*pbh.privateKey, *info)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create signer #%d: %w", i, err)
		}
		state.signers[i] = signer

		// Create request setup.
		setupMsg, err := signer.CreateMessage1()
		if err != nil {
			return nil, nil, fmt.Errorf("failed to create setup msg #%d: %w", i, err)
		}
		setupResponse.Msgs[i] = &setupMsg
	}

	return state, setupResponse, nil
}

// CreateTokenRequest creates a token request to be sent to the token server.
func (pbh *PBlindHandler) CreateTokenRequest(requestSetup *PBlindSetupResponse) (request *PBlindTokenRequest, err error) {
	// Check request setup data.
	if len(requestSetup.Msgs) != pbh.opts.BatchSize {
		return nil, fmt.Errorf("invalid request setup msg count of %d", len(requestSetup.Msgs))
	}

	// Lock and reset the request state.
	pbh.requestStateLock.Lock()
	defer pbh.requestStateLock.Unlock()
	pbh.requestState = make([]RequestState, pbh.opts.BatchSize)
	request = &PBlindTokenRequest{
		Msgs: make([]*pblind.Message2, pbh.opts.BatchSize),
	}

	// Go through the batch.
	for i := range pbh.opts.BatchSize {
		// Check if we have setup data.
		if requestSetup.Msgs[i] == nil {
			return nil, fmt.Errorf("missing setup data #%d", i)
		}

		// Generate secret token.
		token := make([]byte, pblindSecretSize)
		n, err := rand.Read(token) //nolint:gosec // False positive - check the imports.
		if err != nil {
			return nil, fmt.Errorf("failed to get random token #%d: %w", i, err)
		}
		if n != pblindSecretSize {
			return nil, fmt.Errorf("failed to get full random token #%d: only got %d bytes", i, n)
		}
		pbh.requestState[i].Token = token

		// Create public metadata.
		info, err := pbh.makeInfo(i + 1)
		if err != nil {
			return nil, fmt.Errorf("failed to make token info #%d: %w", i, err)
		}

		// Create request and request state.
		requester, err := pblind.CreateRequester(*pbh.publicKey, *info, token)
		if err != nil {
			return nil, fmt.Errorf("failed to create request state #%d: %w", i, err)
		}
		pbh.requestState[i].State = requester

		err = requester.ProcessMessage1(*requestSetup.Msgs[i])
		if err != nil {
			return nil, fmt.Errorf("failed to process setup message #%d: %w", i, err)
		}

		// Create request message.
		requestMsg, err := requester.CreateMessage2()
		if err != nil {
			return nil, fmt.Errorf("failed to create request message #%d: %w", i, err)
		}
		request.Msgs[i] = &requestMsg
	}

	return request, nil
}

// IssueTokens sign the requested tokens.
func (pbh *PBlindHandler) IssueTokens(state *PBlindSignerState, request *PBlindTokenRequest) (response *IssuedPBlindTokens, err error) {
	// Check request data.
	if len(request.Msgs) != pbh.opts.BatchSize {
		return nil, fmt.Errorf("invalid request msg count of %d", len(request.Msgs))
	}
	if len(state.signers) != pbh.opts.BatchSize {
		return nil, fmt.Errorf("invalid request state count of %d", len(request.Msgs))
	}

	// Create response.
	response = &IssuedPBlindTokens{
		Msgs: make([]*pblind.Message3, pbh.opts.BatchSize),
	}

	// Go through the batch.
	for i := range pbh.opts.BatchSize {
		// Check if we have request data.
		if request.Msgs[i] == nil {
			return nil, fmt.Errorf("missing request data #%d", i)
		}

		// Process request msg.
		err = state.signers[i].ProcessMessage2(*request.Msgs[i])
		if err != nil {
			return nil, fmt.Errorf("failed to process request msg #%d: %w", i, err)
		}

		// Issue token.
		responseMsg, err := state.signers[i].CreateMessage3()
		if err != nil {
			return nil, fmt.Errorf("failed to issue token #%d: %w", i, err)
		}
		response.Msgs[i] = &responseMsg
	}

	return response, nil
}

// ProcessIssuedTokens processes the issued token from the server.
func (pbh *PBlindHandler) ProcessIssuedTokens(issuedTokens *IssuedPBlindTokens) error {
	// Check data.
	if len(issuedTokens.Msgs) != pbh.opts.BatchSize {
		return fmt.Errorf("invalid issued token count of %d", len(issuedTokens.Msgs))
	}

	// Step 1: Process issued tokens.

	// Lock and reset the request state.
	pbh.requestStateLock.Lock()
	defer pbh.requestStateLock.Unlock()
	defer func() {
		pbh.requestState = make([]RequestState, pbh.opts.BatchSize)
	}()
	finalizedTokens := make([]*PBlindToken, pbh.opts.BatchSize)

	// Go through the batch.
	for i := range pbh.opts.BatchSize {
		// Finalize token.
		err := pbh.requestState[i].State.ProcessMessage3(*issuedTokens.Msgs[i])
		if err != nil {
			return fmt.Errorf("failed to create final signature #%d: %w", i, err)
		}

		// Get and check final signature.
		signature, err := pbh.requestState[i].State.Signature()
		if err != nil {
			return fmt.Errorf("failed to create final signature #%d: %w", i, err)
		}
		info, err := pbh.makeInfo(i + 1)
		if err != nil {
			return fmt.Errorf("failed to make token info #%d: %w", i, err)
		}
		if !pbh.publicKey.Check(signature, *info, pbh.requestState[i].Token) {
			return fmt.Errorf("invalid signature on #%d", i)
		}

		// Save to temporary slice.
		newToken := &PBlindToken{
			Token:     pbh.requestState[i].Token,
			Signature: &signature,
		}
		if pbh.opts.UseSerials {
			newToken.Serial = i + 1
		}
		finalizedTokens[i] = newToken
	}

	// Step 2: Randomize received tokens

	if pbh.opts.RandomizeOrder {
		rInt, err := rand.Int(rand.Reader, big.NewInt(math.MaxInt64))
		if err != nil {
			return fmt.Errorf("failed to get seed for shuffle: %w", err)
		}
		mr := mrand.New(mrand.NewSource(rInt.Int64())) //nolint:gosec
		mr.Shuffle(len(finalizedTokens), func(i, j int) {
			finalizedTokens[i], finalizedTokens[j] = finalizedTokens[j], finalizedTokens[i]
		})
	}

	// Step 3: Add tokens to storage.

	// Wait for all processing to be complete, as using tokens from a faulty
	// batch can be dangerous, as the server could be doing this purposely to
	// create conditions that may benefit an attacker.

	pbh.storageLock.Lock()
	defer pbh.storageLock.Unlock()

	// Add finalized tokens to storage.
	pbh.Storage = append(pbh.Storage, finalizedTokens...)

	return nil
}

// GetToken returns a token.
func (pbh *PBlindHandler) GetToken() (token *Token, err error) {
	pbh.storageLock.Lock()
	defer pbh.storageLock.Unlock()

	// Check if we have supply.
	if len(pbh.Storage) == 0 {
		return nil, ErrEmpty
	}

	// Pack token.
	data, err := pbh.Storage[0].Pack()
	if err != nil {
		return nil, fmt.Errorf("failed to pack token: %w", err)
	}

	// Shift to next token.
	pbh.Storage = pbh.Storage[1:]

	// Check if we should signal that we should request tokens.
	if pbh.opts.SignalShouldRequest != nil && pbh.shouldRequest() {
		pbh.opts.SignalShouldRequest(pbh)
	}

	return &Token{
		Zone: pbh.opts.Zone,
		Data: data,
	}, nil
}

// Verify verifies the given token.
func (pbh *PBlindHandler) Verify(token *Token) error {
	// Check if zone matches.
	if token.Zone != pbh.opts.Zone {
		return ErrZoneMismatch
	}

	// Unpack token.
	t, err := UnpackPBlindToken(token.Data)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrTokenMalformed, err)
	}

	// Check if serial is valid.
	switch {
	case pbh.opts.UseSerials && t.Serial > 0 && t.Serial <= pbh.opts.BatchSize:
		// Using serials in accepted range.
	case !pbh.opts.UseSerials && t.Serial == 0:
		// Not using serials and serial is zero.
	default:
		return fmt.Errorf("%w: invalid serial", ErrTokenMalformed)
	}

	// Build info for checking signature.
	info, err := pbh.makeInfo(t.Serial)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrTokenMalformed, err)
	}

	// Check signature.
	if !pbh.publicKey.Check(*t.Signature, *info, t.Token) {
		return ErrTokenInvalid
	}

	// Check for double spending.
	if pbh.opts.DoubleSpendProtection != nil {
		if err := pbh.opts.DoubleSpendProtection(t.Token); err != nil {
			return fmt.Errorf("%w: %w", ErrTokenUsed, err)
		}
	}

	return nil
}

// PBlindStorage is a storage for pblind tokens.
type PBlindStorage struct {
	Storage []*PBlindToken
}

// Save serializes and returns the current tokens.
func (pbh *PBlindHandler) Save() ([]byte, error) {
	pbh.storageLock.Lock()
	defer pbh.storageLock.Unlock()

	if len(pbh.Storage) == 0 {
		return nil, ErrEmpty
	}

	s := &PBlindStorage{
		Storage: pbh.Storage,
	}

	return dsd.Dump(s, dsd.CBOR)
}

// Load loads the given tokens into the handler.
func (pbh *PBlindHandler) Load(data []byte) error {
	pbh.storageLock.Lock()
	defer pbh.storageLock.Unlock()

	s := &PBlindStorage{}
	_, err := dsd.Load(data, s)
	if err != nil {
		return err
	}

	// Check signatures on load.
	for _, t := range s.Storage {
		// Build info for checking signature.
		info, err := pbh.makeInfo(t.Serial)
		if err != nil {
			return err
		}

		// Check signature.
		if !pbh.publicKey.Check(*t.Signature, *info, t.Token) {
			return ErrTokenInvalid
		}
	}

	pbh.Storage = s.Storage
	return nil
}

// Clear clears all the tokens in the handler.
func (pbh *PBlindHandler) Clear() {
	pbh.storageLock.Lock()
	defer pbh.storageLock.Unlock()

	pbh.Storage = nil
}
