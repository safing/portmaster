package token

import (
	"testing"

	"github.com/safing/portmaster/base/rng"
)

func TestToken(t *testing.T) {
	t.Parallel()

	randomData, err := rng.Bytes(32)
	if err != nil {
		t.Fatal(err)
	}

	c := &Token{
		Zone: "test",
		Data: randomData,
	}

	s := c.String()
	_, err = ParseToken(s)
	if err != nil {
		t.Fatal(err)
	}

	r := c.Raw()
	_, err = ParseRawToken(r)
	if err != nil {
		t.Fatal(err)
	}
}
