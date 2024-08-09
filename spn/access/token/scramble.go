package token

import (
	"fmt"
	"sync"

	"github.com/mr-tron/base58"

	"github.com/safing/jess/lhash"
	"github.com/safing/structures/dsd"
)

const (
	scrambleSecretSize = 32
)

// ScrambleToken is token based on hashing.
type ScrambleToken struct {
	Token []byte
}

// Pack packs the token.
func (pbt *ScrambleToken) Pack() ([]byte, error) {
	return pbt.Token, nil
}

// UnpackScrambleToken unpacks the token.
func UnpackScrambleToken(token []byte) (*ScrambleToken, error) {
	return &ScrambleToken{Token: token}, nil
}

// ScrambleHandler is a handler for the scramble tokens.
type ScrambleHandler struct {
	sync.Mutex
	opts *ScrambleOptions

	storageLock sync.Mutex
	Storage     []*ScrambleToken

	verifiersLock sync.RWMutex
	verifiers     map[string]*ScrambleToken
}

// ScrambleOptions are options for the ScrambleHandler.
type ScrambleOptions struct {
	Zone             string
	Algorithm        lhash.Algorithm
	InitialTokens    []string
	InitialVerifiers []string
	Fallback         bool
}

// ScrambleTokenRequest is a token request.
type ScrambleTokenRequest struct{}

// IssuedScrambleTokens are issued scrambled tokens.
type IssuedScrambleTokens struct {
	Tokens []*ScrambleToken
}

// NewScrambleHandler creates a new scramble handler.
func NewScrambleHandler(opts ScrambleOptions) (*ScrambleHandler, error) {
	sh := &ScrambleHandler{
		opts:      &opts,
		verifiers: make(map[string]*ScrambleToken, len(opts.InitialTokens)+len(opts.InitialVerifiers)),
	}

	// Add initial tokens.
	sh.Storage = make([]*ScrambleToken, len(opts.InitialTokens))
	for i, token := range opts.InitialTokens {
		// Add to storage.
		tokenData, err := base58.Decode(token)
		if err != nil {
			return nil, fmt.Errorf("failed to decode initial token %q: %w", token, err)
		}
		sh.Storage[i] = &ScrambleToken{
			Token: tokenData,
		}

		// Add to verifiers.
		scrambledToken := lhash.Digest(sh.opts.Algorithm, tokenData).Bytes()
		sh.verifiers[string(scrambledToken)] = sh.Storage[i]
	}

	// Add initial verifiers.
	for _, verifier := range opts.InitialVerifiers {
		verifierData, err := base58.Decode(verifier)
		if err != nil {
			return nil, fmt.Errorf("failed to decode verifier %q: %w", verifier, err)
		}
		sh.verifiers[string(verifierData)] = &ScrambleToken{}
	}

	return sh, nil
}

// Zone returns the zone name.
func (sh *ScrambleHandler) Zone() string {
	return sh.opts.Zone
}

// ShouldRequest returns whether the new tokens should be requested.
func (sh *ScrambleHandler) ShouldRequest() bool {
	sh.storageLock.Lock()
	defer sh.storageLock.Unlock()

	return len(sh.Storage) == 0
}

// Amount returns the current amount of tokens in this handler.
func (sh *ScrambleHandler) Amount() int {
	sh.storageLock.Lock()
	defer sh.storageLock.Unlock()

	return len(sh.Storage)
}

// IsFallback returns whether this handler should only be used as a fallback.
func (sh *ScrambleHandler) IsFallback() bool {
	return sh.opts.Fallback
}

// CreateTokenRequest creates a token request to be sent to the token server.
func (sh *ScrambleHandler) CreateTokenRequest() (request *ScrambleTokenRequest) {
	return &ScrambleTokenRequest{}
}

// IssueTokens sign the requested tokens.
func (sh *ScrambleHandler) IssueTokens(request *ScrambleTokenRequest) (response *IssuedScrambleTokens, err error) {
	// Copy the storage.
	tokens := make([]*ScrambleToken, len(sh.Storage))
	copy(tokens, sh.Storage)

	return &IssuedScrambleTokens{
		Tokens: tokens,
	}, nil
}

// ProcessIssuedTokens processes the issued token from the server.
func (sh *ScrambleHandler) ProcessIssuedTokens(issuedTokens *IssuedScrambleTokens) error {
	sh.verifiersLock.RLock()
	defer sh.verifiersLock.RUnlock()

	// Validate tokens.
	for i, newToken := range issuedTokens.Tokens {
		// Scramle token.
		scrambledToken := lhash.Digest(sh.opts.Algorithm, newToken.Token).Bytes()

		// Check if token is valid.
		_, ok := sh.verifiers[string(scrambledToken)]
		if !ok {
			return fmt.Errorf("invalid token on #%d", i)
		}
	}

	// Copy to storage.
	sh.Storage = issuedTokens.Tokens

	return nil
}

// Verify verifies the given token.
func (sh *ScrambleHandler) Verify(token *Token) error {
	if token.Zone != sh.opts.Zone {
		return ErrZoneMismatch
	}

	// Hash the data.
	scrambledToken := lhash.Digest(sh.opts.Algorithm, token.Data).Bytes()

	sh.verifiersLock.RLock()
	defer sh.verifiersLock.RUnlock()

	// Check if token is valid.
	_, ok := sh.verifiers[string(scrambledToken)]
	if !ok {
		return ErrTokenInvalid
	}

	return nil
}

// GetToken returns a token.
func (sh *ScrambleHandler) GetToken() (*Token, error) {
	sh.storageLock.Lock()
	defer sh.storageLock.Unlock()

	if len(sh.Storage) == 0 {
		return nil, ErrEmpty
	}

	return &Token{
		Zone: sh.opts.Zone,
		Data: sh.Storage[0].Token,
	}, nil
}

// ScrambleStorage is a storage for scramble tokens.
type ScrambleStorage struct {
	Storage []*ScrambleToken
}

// Save serializes and returns the current tokens.
func (sh *ScrambleHandler) Save() ([]byte, error) {
	sh.storageLock.Lock()
	defer sh.storageLock.Unlock()

	if len(sh.Storage) == 0 {
		return nil, ErrEmpty
	}

	s := &ScrambleStorage{
		Storage: sh.Storage,
	}

	return dsd.Dump(s, dsd.CBOR)
}

// Load loads the given tokens into the handler.
func (sh *ScrambleHandler) Load(data []byte) error {
	sh.storageLock.Lock()
	defer sh.storageLock.Unlock()

	s := &ScrambleStorage{}
	_, err := dsd.Load(data, s)
	if err != nil {
		return err
	}

	sh.Storage = s.Storage
	return nil
}

// Clear clears all the tokens in the handler.
func (sh *ScrambleHandler) Clear() {
	sh.storageLock.Lock()
	defer sh.storageLock.Unlock()

	sh.Storage = nil
}
