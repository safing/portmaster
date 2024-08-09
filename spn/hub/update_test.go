package hub

import (
	"fmt"
	"testing"

	"github.com/safing/jess"
	"github.com/safing/structures/dsd"
)

func TestHubUpdate(t *testing.T) {
	t.Parallel()

	// message signing

	testData := []byte{0}

	s1, err := jess.GenerateSignet("Ed25519", 0)
	if err != nil {
		t.Fatal(err)
	}
	err = s1.StoreKey()
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("s1: %+v\n", s1)

	s1e, err := s1.AsRecipient()
	if err != nil {
		t.Fatal(err)
	}
	err = s1e.StoreKey()
	if err != nil {
		t.Fatal(err)
	}
	s1e.ID = createHubID(s1e.Scheme, s1e.Key)
	s1.ID = s1e.ID

	t.Logf("generated hub ID: %s", s1.ID)

	env := jess.NewUnconfiguredEnvelope()
	env.SuiteID = jess.SuiteSignV1
	env.Senders = []*jess.Signet{s1}

	s, err := env.Correspondence(nil)
	if err != nil {
		t.Fatal(err)
	}
	letter, err := s.Close(testData)
	if err != nil {
		t.Fatal(err)
	}

	// smuggle the key
	letter.Keys = append(letter.Keys, &jess.Seal{
		Value: s1e.Key,
	})
	t.Logf("letter with smuggled key: %+v", letter)

	// pack
	data, err := letter.ToDSD(dsd.JSON)
	if err != nil {
		t.Fatal(err)
	}

	_, _, _, err = OpenHubMsg(nil, data, "test", true) //nolint:dogsled
	if err != nil {
		t.Fatal(err)
	}
}
