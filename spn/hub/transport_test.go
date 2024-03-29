package hub

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func parseT(t *testing.T, definition string) *Transport {
	t.Helper()

	tr, err := ParseTransport(definition)
	if err != nil {
		t.Fatal(err)
		return nil
	}
	return tr
}

func parseTError(definition string) error {
	_, err := ParseTransport(definition)
	return err
}

func TestTransportParsing(t *testing.T) {
	t.Parallel()

	// test parsing

	assert.Equal(t, &Transport{
		Protocol: "spn",
		Port:     17,
	}, parseT(t, "spn:17"), "should match")

	assert.Equal(t, &Transport{
		Protocol: "smtp",
		Port:     25,
	}, parseT(t, "smtp:25"), "should match")

	assert.Equal(t, &Transport{
		Protocol: "smtp",
		Port:     25,
	}, parseT(t, "smtp://:25"), "should match")

	assert.Equal(t, &Transport{
		Protocol: "smtp",
		Port:     587,
	}, parseT(t, "smtp:587"), "should match")

	assert.Equal(t, &Transport{
		Protocol: "imap",
		Port:     143,
	}, parseT(t, "imap:143"), "should match")

	assert.Equal(t, &Transport{
		Protocol: "http",
		Port:     80,
	}, parseT(t, "http:80"), "should match")

	assert.Equal(t, &Transport{
		Protocol: "http",
		Domain:   "example.com",
		Port:     80,
	}, parseT(t, "http://example.com:80"), "should match")

	assert.Equal(t, &Transport{
		Protocol: "https",
		Port:     443,
	}, parseT(t, "https:443"), "should match")

	assert.Equal(t, &Transport{
		Protocol: "ws",
		Port:     80,
	}, parseT(t, "ws:80"), "should match")

	assert.Equal(t, &Transport{
		Protocol: "wss",
		Domain:   "example.com",
		Port:     443,
		Path:     "/spn",
	}, parseT(t, "wss://example.com:443/spn"), "should match")

	assert.Equal(t, &Transport{
		Protocol: "http",
		Domain:   "example.com",
		Port:     80,
	}, parseT(t, "http://example.com:80"), "should match")

	assert.Equal(t, &Transport{
		Protocol: "http",
		Domain:   "example.com",
		Port:     80,
		Path:     "/test%20test",
	}, parseT(t, "http://example.com:80/test test"), "should match")

	assert.Equal(t, &Transport{
		Protocol: "http",
		Domain:   "example.com",
		Port:     80,
		Path:     "/test%20test",
	}, parseT(t, "http://example.com:80/test%20test"), "should match")

	assert.Equal(t, &Transport{
		Protocol: "http",
		Domain:   "example.com",
		Port:     80,
		Path:     "/test?key=value",
	}, parseT(t, "http://example.com:80/test?key=value"), "should match")

	// test parsing and formatting

	assert.Equal(t, "spn:17",
		parseT(t, "spn:17").String(), "should match")
	assert.Equal(t, "smtp:25",
		parseT(t, "smtp:25").String(), "should match")
	assert.Equal(t, "smtp:25",
		parseT(t, "smtp://:25").String(), "should match")
	assert.Equal(t, "smtp:587",
		parseT(t, "smtp:587").String(), "should match")
	assert.Equal(t, "imap:143",
		parseT(t, "imap:143").String(), "should match")
	assert.Equal(t, "http:80",
		parseT(t, "http:80").String(), "should match")
	assert.Equal(t, "http://example.com:80",
		parseT(t, "http://example.com:80").String(), "should match")
	assert.Equal(t, "https:443",
		parseT(t, "https:443").String(), "should match")
	assert.Equal(t, "ws:80",
		parseT(t, "ws:80").String(), "should match")
	assert.Equal(t, "wss://example.com:443/spn",
		parseT(t, "wss://example.com:443/spn").String(), "should match")
	assert.Equal(t, "http://example.com:80",
		parseT(t, "http://example.com:80").String(), "should match")
	assert.Equal(t, "http://example.com:80/test%20test",
		parseT(t, "http://example.com:80/test test").String(), "should match")
	assert.Equal(t, "http://example.com:80/test%20test",
		parseT(t, "http://example.com:80/test%20test").String(), "should match")
	assert.Equal(t, "http://example.com:80/test?key=value",
		parseT(t, "http://example.com:80/test?key=value").String(), "should match")

	// test invalid

	require.Error(t, parseTError("spn"), "should fail")
	require.Error(t, parseTError("spn:"), "should fail")
	require.Error(t, parseTError("spn:0"), "should fail")
	require.Error(t, parseTError("spn:65536"), "should fail")
}
