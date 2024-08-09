package token

import (
	"testing"
	"time"

	"github.com/safing/structures/dsd"
)

func TestFull(t *testing.T) {
	t.Parallel()

	testStart := time.Now()

	// Roundtrip 1

	start := time.Now()
	setupRequest, setupRequired := CreateSetupRequest()
	if !setupRequired {
		t.Fatal("setup should be required")
	}
	setupRequestData, err := dsd.Dump(setupRequest, dsd.CBOR)
	if err != nil {
		t.Fatal(err)
	}
	setupRequest = nil // nolint:ineffassign,wastedassign // Just to be sure.
	t.Logf("setupRequest: %s, %d bytes", time.Since(start), len(setupRequestData))

	start = time.Now()
	loadedSetupRequest := &SetupRequest{}
	_, err = dsd.Load(setupRequestData, loadedSetupRequest)
	if err != nil {
		t.Fatal(err)
	}
	serverState, setupResponse, err := HandleSetupRequest(loadedSetupRequest)
	if err != nil {
		t.Fatal(err)
	}
	setupResponseData, err := dsd.Dump(setupResponse, dsd.CBOR)
	if err != nil {
		t.Fatal(err)
	}
	setupResponse = nil // nolint:ineffassign,wastedassign // Just to be sure.
	t.Logf("setupResponse: %s, %d bytes", time.Since(start), len(setupResponseData))

	// Roundtrip 2

	start = time.Now()
	loadedSetupResponse := &SetupResponse{}
	_, err = dsd.Load(setupResponseData, loadedSetupResponse)
	if err != nil {
		t.Fatal(err)
	}
	request, requestRequired, err := CreateTokenRequest(loadedSetupResponse)
	if err != nil {
		t.Fatal(err)
	}
	if !requestRequired {
		t.Fatal("request should be required")
	}
	requestData, err := dsd.Dump(request, dsd.CBOR)
	if err != nil {
		t.Fatal(err)
	}
	request = nil // nolint:ineffassign,wastedassign // Just to be sure.
	t.Logf("request: %s, %d bytes", time.Since(start), len(requestData))

	start = time.Now()
	loadedRequest := &TokenRequest{}
	_, err = dsd.Load(requestData, loadedRequest)
	if err != nil {
		t.Fatal(err)
	}
	response, err := IssueTokens(serverState, loadedRequest)
	if err != nil {
		t.Fatal(err)
	}
	responseData, err := dsd.Dump(response, dsd.CBOR)
	if err != nil {
		t.Fatal(err)
	}
	response = nil // nolint:ineffassign,wastedassign // Just to be sure.
	t.Logf("response: %s, %d bytes", time.Since(start), len(responseData))

	start = time.Now()
	loadedResponse := &IssuedTokens{}
	_, err = dsd.Load(responseData, loadedResponse)
	if err != nil {
		t.Fatal(err)
	}
	err = ProcessIssuedTokens(loadedResponse)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("processing: %s", time.Since(start))

	// Token Usage

	for _, testZone := range []string{
		PBlindTestZone,
		ScrambleTestZone,
	} {
		start = time.Now()

		token, err := GetToken(testZone)
		if err != nil {
			t.Fatal(err)
		}
		tokenData := token.Raw()
		token = nil // nolint:wastedassign // Just to be sure.

		loadedToken, err := ParseRawToken(tokenData)
		if err != nil {
			t.Fatal(err)
		}
		err = VerifyToken(loadedToken)
		if err != nil {
			t.Fatal(err)
		}

		t.Logf("using %s token: %s", testZone, time.Since(start))
	}

	t.Logf("full simulation took %s", time.Since(testStart))
}
