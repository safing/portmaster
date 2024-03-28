package hub

import (
	"fmt"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckStringFormat(t *testing.T) {
	t.Parallel()

	testSet := map[string]bool{
		// Printable ASCII (character code 32-127)
		" ": true, "!": true, `"`: false, "#": true, "$": false, "%": false, "&": false, "'": false,
		"(": true, ")": true, "*": true, "+": true, ",": true, "-": true, ".": true, "/": true,
		"0": true, "1": true, "2": true, "3": true, "4": true, "5": true, "6": true, "7": true,
		"8": true, "9": true, ":": true, ";": false, "<": false, "=": true, ">": false, "?": true,
		"@": true, "A": true, "B": true, "C": true, "D": true, "E": true, "F": true, "G": true,
		"H": true, "I": true, "J": true, "K": true, "L": true, "M": true, "N": true, "O": true,
		"P": true, "Q": true, "R": true, "S": true, "T": true, "U": true, "V": true, "W": true,
		"X": true, "Y": true, "Z": true, "[": true, `\`: false, "]": true, "^": true, "_": true,
		"`": false, "a": true, "b": true, "c": true, "d": true, "e": true, "f": true, "g": true,
		"h": true, "i": true, "j": true, "k": true, "l": true, "m": true, "n": true, "o": true,
		"p": true, "q": true, "r": true, "s": true, "t": true, "u": true, "v": true, "w": true,
		"x": true, "y": true, "z": true, "{": true, "|": true, "}": true, "~": true,
		// Not testing for DELETE character.

		// Extended ASCII (character code 128-255)
		"€": false, "‚": false, "ƒ": false, "„": false, "…": false, "†": false, "‡": false, "ˆ": false,
		"‰": false, "Š": true, "‹": false, "Œ": true, "Ž": true, "‘": false, "’": false, "“": false,
		"”": false, "•": false, "–": false, "—": false, "˜": false, "™": false, "š": true, "›": false,
		"œ": true, "ž": true, "Ÿ": true, "¡": true, "¢": false, "£": false, "¤": false, "¥": false,
		"¦": false, "§": false, "¨": false, "©": false, "ª": false, "«": false, "¬": false, "®": false,
		"¯": false, "°": false, "±": false, "²": false, "³": false, "´": false, "µ": false, "¶": false,
		"·": false, "¸": false, "¹": false, "º": false, "»": false, "¼": false, "½": false, "¾": false,
		"¿": true, "À": true, "Á": true, "Â": true, "Ã": true, "Ä": true, "Å": true, "Æ": true,
		"Ç": true, "È": true, "É": true, "Ê": true, "Ë": true, "Ì": true, "Í": true, "Î": true,
		"Ï": true, "Ð": true, "Ñ": true, "Ò": true, "Ó": true, "Ô": true, "Õ": true, "Ö": true,
		"×": false, "Ø": true, "Ù": true, "Ú": true, "Û": true, "Ü": true, "Ý": true, "Þ": true,
		"ß": true, "à": true, "á": true, "â": true, "ã": true, "ä": true, "å": true, "æ": true,
		"ç": true, "è": true, "é": true, "ê": true, "ë": true, "ì": true, "í": true, "î": true,
		"ï": true, "ð": true, "ñ": true, "ò": true, "ó": true, "ô": true, "õ": true, "ö": true,
		"÷": false, "ø": true, "ù": true, "ú": true, "û": true, "ü": true, "ý": true, "þ": true,
		"ÿ": true,
	}

	for testCharacter, isPermitted := range testSet {
		if isPermitted {
			require.NoError(t, checkStringFormat(fmt.Sprintf("test character %q", testCharacter), testCharacter, 3))
		} else {
			require.Error(t, checkStringFormat(fmt.Sprintf("test character %q", testCharacter), testCharacter, 3))
		}
	}
}

func TestCheckIPFormat(t *testing.T) {
	t.Parallel()

	// IPv4
	require.NoError(t, checkIPFormat("test IP 1.1.1.1", net.IPv4(1, 1, 1, 1)))
	require.NoError(t, checkIPFormat("test IP 192.168.1.1", net.IPv4(192, 168, 1, 1)))
	require.Error(t, checkIPFormat("test IP 255.0.0.1", net.IPv4(255, 0, 0, 1)))

	// IPv6
	require.NoError(t, checkIPFormat("test IP ::1", net.ParseIP("::1")))
	require.NoError(t, checkIPFormat("test IP 2606:4700:4700::1111", net.ParseIP("2606:4700:4700::1111")))

	// Invalid
	require.Error(t, checkIPFormat("test IP with length 3", net.IP([]byte{0, 0, 0})))
	require.Error(t, checkIPFormat("test IP with length 5", net.IP([]byte{0, 0, 0, 0, 0})))
	require.Error(t, checkIPFormat(
		"test IP with length 15",
		net.IP([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}),
	))
	require.Error(t, checkIPFormat(
		"test IP with length 17",
		net.IP([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}),
	))
}
