package cabin

import (
	"crypto/subtle"
	"errors"
	"fmt"

	"github.com/safing/jess"
	"github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/spn/hub"
	"github.com/safing/structures/dsd"
)

var (
	verificationChallengeSize    = 32
	verificationChallengeMinSize = 16
	verificationSigningSuite     = jess.SuiteSignV1
	verificationRequirements     = jess.NewRequirements().
					Remove(jess.Confidentiality).
					Remove(jess.Integrity).
					Remove(jess.RecipientAuthentication)
)

// Verification is used to verify certain aspects of another Hub.
type Verification struct {
	// Challenge is a random value chosen by the client.
	Challenge []byte `json:"c"`
	// Purpose defines the purpose of the verification. Protects against using verification for other purposes.
	Purpose string `json:"p"`
	// ClientReference is an optional field for exchanging metadata about the client. Protects against forwarding/relay attacks.
	ClientReference string `json:"cr"`
	// ServerReference is an optional field for exchanging metadata about the server. Protects against forwarding/relay attacks.
	ServerReference string `json:"sr"`
}

// CreateVerificationRequest creates a new verification request with the given
// purpose and references.
func CreateVerificationRequest(purpose, clientReference, serverReference string) (v *Verification, request []byte, err error) {
	// Generate random challenge.
	challenge, err := rng.Bytes(verificationChallengeSize)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate challenge: %w", err)
	}

	// Create verification object.
	v = &Verification{
		Purpose:         purpose,
		ClientReference: clientReference,
		Challenge:       challenge,
	}

	// Serialize verification.
	request, err = dsd.Dump(v, dsd.JSON)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to serialize verification request: %w", err)
	}

	// The server reference is not sent to the server, but needs to be supplied
	// by the server.
	v.ServerReference = serverReference

	return v, request, nil
}

// SignVerificationRequest sign a verification request.
// The purpose and references must match the request, else the verification
// will fail.
func (id *Identity) SignVerificationRequest(request []byte, purpose, clientReference, serverReference string) (response []byte, err error) {
	// Parse request.
	v := new(Verification)
	_, err = dsd.Load(request, v)
	if err != nil {
		return nil, fmt.Errorf("failed to parse request: %w", err)
	}

	// Validate request.
	if len(v.Challenge) < verificationChallengeMinSize {
		return nil, errors.New("challenge too small")
	}
	if v.Purpose != purpose {
		return nil, errors.New("purpose mismatch")
	}
	if v.ClientReference != clientReference {
		return nil, errors.New("client reference mismatch")
	}

	// Assign server reference and serialize.
	v.ServerReference = serverReference
	dataToSign, err := dsd.Dump(v, dsd.JSON)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize verification response: %w", err)
	}

	// Sign response.
	e := jess.NewUnconfiguredEnvelope()
	e.SuiteID = verificationSigningSuite
	e.Senders = []*jess.Signet{id.Signet}
	jession, err := e.Correspondence(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to setup signer: %w", err)
	}
	letter, err := jession.Close(dataToSign)
	if err != nil {
		return nil, fmt.Errorf("failed to sign: %w", err)
	}

	// Serialize and return.
	signedResponse, err := letter.ToDSD(dsd.JSON)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize letter: %w", err)
	}

	return signedResponse, nil
}

// Verify verifies the verification response and checks if everything is valid.
func (v *Verification) Verify(response []byte, h *hub.Hub) error {
	// Parse response.
	letter, err := jess.LetterFromDSD(response)
	if err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	// Verify response.
	responseData, err := letter.Open(
		verificationRequirements,
		&hub.SingleTrustStore{
			Signet: h.PublicKey,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to verify response: %w", err)
	}

	// Parse verified response.
	responseV := new(Verification)
	_, err = dsd.Load(responseData, responseV)
	if err != nil {
		return fmt.Errorf("failed to parse verified response: %w", err)
	}

	// Validate request.
	if subtle.ConstantTimeCompare(v.Challenge, responseV.Challenge) != 1 {
		return errors.New("challenge mismatch")
	}
	if subtle.ConstantTimeCompare([]byte(v.Purpose), []byte(responseV.Purpose)) != 1 {
		return errors.New("purpose mismatch")
	}
	if subtle.ConstantTimeCompare([]byte(v.ClientReference), []byte(responseV.ClientReference)) != 1 {
		return errors.New("client reference mismatch")
	}
	if subtle.ConstantTimeCompare([]byte(v.ServerReference), []byte(responseV.ServerReference)) != 1 {
		return errors.New("server reference mismatch")
	}

	return nil
}
