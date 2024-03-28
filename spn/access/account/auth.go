package account

import (
	"errors"
	"net/http"
)

// Authentication Headers.
const (
	AuthHeaderDevice              = "Device-17"
	AuthHeaderToken               = "Token-17"
	AuthHeaderNextToken           = "Next-Token-17"
	AuthHeaderNextTokenDeprecated = "Next_token_17"
)

// Errors.
var (
	ErrMissingDeviceID = errors.New("missing device ID")
	ErrMissingToken    = errors.New("missing token")
)

// AuthToken holds an authentication token.
type AuthToken struct {
	Device string
	Token  string
}

// GetAuthTokenFromRequest extracts an authentication token from a request.
func GetAuthTokenFromRequest(request *http.Request) (*AuthToken, error) {
	device := request.Header.Get(AuthHeaderDevice)
	if device == "" {
		return nil, ErrMissingDeviceID
	}
	token := request.Header.Get(AuthHeaderToken)
	if token == "" {
		return nil, ErrMissingToken
	}

	return &AuthToken{
		Device: device,
		Token:  token,
	}, nil
}

// ApplyTo applies the authentication token to a request.
func (at *AuthToken) ApplyTo(request *http.Request) {
	request.Header.Set(AuthHeaderDevice, at.Device)
	request.Header.Set(AuthHeaderToken, at.Token)
}

// GetNextTokenFromResponse extracts an authentication token from a response.
func GetNextTokenFromResponse(resp *http.Response) (token string, ok bool) {
	token = resp.Header.Get(AuthHeaderNextToken)
	if token == "" {
		// TODO: Remove when fixed on server.
		token = resp.Header.Get(AuthHeaderNextTokenDeprecated)
	}

	return token, token != ""
}

// ApplyNextTokenToResponse applies the next authentication token to a response.
func ApplyNextTokenToResponse(w http.ResponseWriter, token string) {
	w.Header().Set(AuthHeaderNextToken, token)
}
