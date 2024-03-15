package hub

import "github.com/safing/jess"

// SingleTrustStore is a simple truststore that always returns the same Signet.
type SingleTrustStore struct {
	Signet *jess.Signet
}

// GetSignet implements the truststore interface.
func (ts *SingleTrustStore) GetSignet(id string, recipient bool) (*jess.Signet, error) {
	if ts.Signet.ID != id || recipient != ts.Signet.Public {
		return nil, jess.ErrSignetNotFound
	}

	return ts.Signet, nil
}
