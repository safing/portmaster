package token

import (
	"testing"

	"github.com/safing/jess/lhash"
)

const ScrambleTestZone = "test-scramble"

func init() {
	// Combined testing config.

	h, err := NewScrambleHandler(ScrambleOptions{
		Zone:          ScrambleTestZone,
		Algorithm:     lhash.SHA2_256,
		InitialTokens: []string{"2VqJ8BvDew1tUpytZhR7tuvq7ToPpW3tQtHvu3veE3iW"},
	})
	if err != nil {
		panic(err)
	}

	err = RegisterScrambleHandler(h)
	if err != nil {
		panic(err)
	}
}

func TestScramble(t *testing.T) {
	t.Parallel()

	opts := &ScrambleOptions{
		Zone:      ScrambleTestZone,
		Algorithm: lhash.SHA2_256,
	}

	// Issuer
	opts.InitialTokens = []string{"2VqJ8BvDew1tUpytZhR7tuvq7ToPpW3tQtHvu3veE3iW"}
	issuer, err := NewScrambleHandler(*opts)
	if err != nil {
		t.Fatal(err)
	}

	// Client
	opts.InitialTokens = nil
	opts.InitialVerifiers = []string{"Cy9tz37Xq9NiXGDRU9yicjGU62GjXskE9KqUmuoddSxaE3"}
	client, err := NewScrambleHandler(*opts)
	if err != nil {
		t.Fatal(err)
	}

	// Verifier
	verifier, err := NewScrambleHandler(*opts)
	if err != nil {
		t.Fatal(err)
	}

	// Play through the whole use case.

	request := client.CreateTokenRequest()
	if err != nil {
		t.Fatal(err)
	}

	issuedTokens, err := issuer.IssueTokens(request)
	if err != nil {
		t.Fatal(err)
	}

	err = client.ProcessIssuedTokens(issuedTokens)
	if err != nil {
		t.Fatal(err)
	}

	token, err := client.GetToken()
	if err != nil {
		t.Fatal(err)
	}

	err = verifier.Verify(token)
	if err != nil {
		t.Fatal(err)
	}
}
