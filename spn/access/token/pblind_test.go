package token

import (
	"crypto/elliptic"
	"encoding/asn1"
	"testing"
	"time"

	"github.com/rot256/pblind"
)

const PBlindTestZone = "test-pblind"

func init() {
	// Combined testing config.

	h, err := NewPBlindHandler(PBlindOptions{
		Zone:           PBlindTestZone,
		Curve:          elliptic.P256(),
		PrivateKey:     "HbwGtLsqek1Fdwuz1MhNQfiY7tj9EpWHeMWHPZ9c6KYY",
		UseSerials:     true,
		BatchSize:      1000,
		RandomizeOrder: true,
	})
	if err != nil {
		panic(err)
	}

	err = RegisterPBlindHandler(h)
	if err != nil {
		panic(err)
	}
}

func TestPBlind(t *testing.T) {
	t.Parallel()

	opts := &PBlindOptions{
		Zone:           PBlindTestZone,
		Curve:          elliptic.P256(),
		UseSerials:     true,
		BatchSize:      1000,
		RandomizeOrder: true,
	}

	// Issuer
	opts.PrivateKey = "HbwGtLsqek1Fdwuz1MhNQfiY7tj9EpWHeMWHPZ9c6KYY"
	issuer, err := NewPBlindHandler(*opts)
	if err != nil {
		t.Fatal(err)
	}

	// Client
	opts.PrivateKey = ""
	opts.PublicKey = "285oMDh3w5mxyFgpmmURifKfhkcqwwsdnePpPZ6Nqm8cc"
	client, err := NewPBlindHandler(*opts)
	if err != nil {
		t.Fatal(err)
	}

	// Verifier
	verifier, err := NewPBlindHandler(*opts)
	if err != nil {
		t.Fatal(err)
	}

	// Play through the whole use case.

	signerState, setupResponse, err := issuer.CreateSetup()
	if err != nil {
		t.Fatal(err)
	}

	request, err := client.CreateTokenRequest(setupResponse)
	if err != nil {
		t.Fatal(err)
	}

	issuedTokens, err := issuer.IssueTokens(signerState, request)
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

func TestPBlindLibrary(t *testing.T) {
	t.Parallel()

	// generate a key-pair

	curve := elliptic.P256()

	sk, _ := pblind.NewSecretKey(curve)
	pk := sk.GetPublicKey()

	msgStr := []byte("128b_accesstoken")
	infoStr := []byte("v=1 serial=12345")
	info, err := pblind.CompressInfo(curve, infoStr)
	if err != nil {
		t.Fatal(err)
	}

	totalStart := time.Now()
	batchSize := 1000

	signers := make([]*pblind.StateSigner, batchSize)
	requesters := make([]*pblind.StateRequester, batchSize)
	toServer := make([][]byte, batchSize)
	toClient := make([][]byte, batchSize)

	// Create signers and prep requests.
	start := time.Now()
	for i := range batchSize {
		signer, err := pblind.CreateSigner(sk, info)
		if err != nil {
			t.Fatal(err)
		}
		signers[i] = signer

		msg1S, err := signer.CreateMessage1()
		if err != nil {
			t.Fatal(err)
		}
		ser1S, err := asn1.Marshal(msg1S)
		if err != nil {
			t.Fatal(err)
		}
		toClient[i] = ser1S
	}
	t.Logf("created %d signers and request preps in %s", batchSize, time.Since(start))
	t.Logf("sending %d bytes to client", lenOfByteSlices(toClient))

	// Create requesters and create requests.
	start = time.Now()
	for i := range batchSize {
		requester, err := pblind.CreateRequester(pk, info, msgStr)
		if err != nil {
			t.Fatal(err)
		}
		requesters[i] = requester

		var msg1R pblind.Message1
		_, err = asn1.Unmarshal(toClient[i], &msg1R)
		if err != nil {
			t.Fatal(err)
		}
		err = requester.ProcessMessage1(msg1R)
		if err != nil {
			t.Fatal(err)
		}

		msg2R, err := requester.CreateMessage2()
		if err != nil {
			t.Fatal(err)
		}
		ser2R, err := asn1.Marshal(msg2R)
		if err != nil {
			t.Fatal(err)
		}
		toServer[i] = ser2R
	}
	t.Logf("created %d requesters and requests in %s", batchSize, time.Since(start))
	t.Logf("sending %d bytes to server", lenOfByteSlices(toServer))

	// Sign requests
	start = time.Now()
	for i := range batchSize {
		var msg2S pblind.Message2
		_, err = asn1.Unmarshal(toServer[i], &msg2S)
		if err != nil {
			t.Fatal(err)
		}
		err = signers[i].ProcessMessage2(msg2S)
		if err != nil {
			t.Fatal(err)
		}

		msg3S, err := signers[i].CreateMessage3()
		if err != nil {
			t.Fatal(err)
		}
		ser3S, err := asn1.Marshal(msg3S)
		if err != nil {
			t.Fatal(err)
		}
		toClient[i] = ser3S
	}
	t.Logf("signed %d requests in %s", batchSize, time.Since(start))
	t.Logf("sending %d bytes to client", lenOfByteSlices(toClient))

	// Verify signed requests
	start = time.Now()
	for i := range batchSize {
		var msg3R pblind.Message3
		_, err := asn1.Unmarshal(toClient[i], &msg3R)
		if err != nil {
			t.Fatal(err)
		}
		err = requesters[i].ProcessMessage3(msg3R)
		if err != nil {
			t.Fatal(err)
		}
		signature, err := requesters[i].Signature()
		if err != nil {
			t.Fatal(err)
		}
		sig, err := asn1.Marshal(signature)
		if err != nil {
			t.Fatal(err)
		}
		toServer[i] = sig

		// check signature
		if !pk.Check(signature, info, msgStr) {
			t.Fatal("signature invalid")
		}
	}
	t.Logf("finalized and verified %d signed tokens in %s", batchSize, time.Since(start))
	t.Logf("stored %d signed tokens in %d bytes", batchSize, lenOfByteSlices(toServer))

	// Verify on server
	start = time.Now()
	for i := range batchSize {
		var sig pblind.Signature
		_, err := asn1.Unmarshal(toServer[i], &sig)
		if err != nil {
			t.Fatal(err)
		}

		// check signature
		if !pk.Check(sig, info, msgStr) {
			t.Fatal("signature invalid")
		}
	}
	t.Logf("verified %d signed tokens in %s", batchSize, time.Since(start))

	t.Logf("process complete")
	t.Logf("simulated the whole process for %d tokens in %s", batchSize, time.Since(totalStart))
}

func lenOfByteSlices(v [][]byte) (length int) {
	for _, s := range v {
		length += len(s)
	}
	return
}
