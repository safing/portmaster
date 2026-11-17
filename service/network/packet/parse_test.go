package packet

import (
	"testing"
)

// TestParseIgnoresSpareCapacity checks that parsing does not read past the end
// of the packet into the spare capacity of the given buffer.
func TestParseIgnoresSpareCapacity(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		layer3  bool
		proto   IPProtocol
		payload []byte
	}{
		{
			// IPv6 + VRRP (112) claiming one more IP address than present.
			name:    "IPv6 VRRP",
			layer3:  true,
			payload: []byte("a00000p00000000000000000000000000000000010000000"),
		},
		{
			// IPv4 + RDP (27) with a truncated header.
			name:    "IPv4 RDP",
			layer3:  true,
			payload: []byte("E00000\x00\x000\x1b00000000000100"),
		},
		{
			// TCP, too short for its header.
			name:    "layer 4 TCP",
			proto:   TCP,
			payload: []byte{0, 1, 0, 2, 0, 0, 0, 0, 0, 0, 0, 0, 0x50},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			parse := func(data []byte) error {
				base := &Base{}
				base.Info().Protocol = tc.proto
				if tc.layer3 {
					return ParseLayer3(data, base)
				}
				return ParseLayer4(data, base)
			}

			exact := make([]byte, len(tc.payload))
			copy(exact, tc.payload)
			exactErr := parse(exact)
			if exactErr == nil {
				t.Fatal("expected malformed packet to fail parsing")
			}

			// Same packet, in a larger buffer, as done by some interception
			// integrations.
			padded := make([]byte, len(tc.payload), len(tc.payload)+256)
			copy(padded, tc.payload)
			paddedErr := parse(padded)
			if paddedErr == nil {
				t.Fatalf("malformed packet parsed successfully with spare buffer capacity (without: %s)", exactErr)
			}
		})
	}
}
