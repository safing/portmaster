// Compatibility fixtures for the Rust port of base/rng.
//
// The Fortuna generator is made deterministic by calling the exported
// Seed(int64) method (which discards the OS-derived initial state), so the
// Rust crate can replay the exact sequence byte for byte.
package main

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"

	"github.com/aead/serpent"
	"github.com/seehuhn/fortuna"
	"github.com/seehuhn/sha256d"
)

type fortunaCase struct {
	Kind    string   `json:"kind"`
	Cipher  string   `json:"cipher"`
	Seed    int64    `json:"seed"`
	Reseeds []string `json:"reseeds"`
	Lengths []int    `json:"lengths"`
	OutHex  string   `json:"out_hex"`
}

func genRng() {
	var cases []any

	// SHA-256d (used by Fortuna for reseeding).
	for _, in := range [][]byte{
		[]byte("abc"),
		{},
		[]byte("portmaster"),
		make([]byte, 64),
	} {
		h := sha256d.New()
		_, _ = h.Write(in)
		cases = append(cases, map[string]any{
			"kind":    "sha256d",
			"in_hex":  hx(in),
			"out_hex": hx(h.Sum(nil)),
		})
	}

	// Fortuna output for both selectable ciphers, with intermediate reseeds.
	for _, cipherName := range []string{"aes", "serpent"} {
		var newCipher func([]byte) (cipher.Block, error) = aes.NewCipher
		if cipherName == "serpent" {
			newCipher = serpent.NewCipher
		}
		for _, tc := range []struct {
			seed    int64
			reseeds [][]byte
			lengths []int
		}{
			{seed: 0, lengths: []int{0, 1, 16, 17, 100, 4096}},
			{seed: 1, reseeds: [][]byte{{1, 2, 3, 4}, {5}}, lengths: []int{32, 100, 1024}},
			{seed: 1 << 62, reseeds: [][]byte{[]byte("entropy")}, lengths: []int{64, 333}},
		} {
			rng := fortuna.NewGenerator(newCipher)
			rng.Seed(tc.seed)
			var out []byte
			for _, r := range tc.reseeds {
				rng.Reseed(r)
			}
			for _, n := range tc.lengths {
				out = append(out, rng.PseudoRandomData(uint(n))...)
			}
			reseedHex := make([]string, 0, len(tc.reseeds))
			for _, r := range tc.reseeds {
				reseedHex = append(reseedHex, hx(r))
			}
			cases = append(cases, fortunaCase{
				Kind:    "fortuna",
				Cipher:  cipherName,
				Seed:    tc.seed,
				Reseeds: reseedHex,
				Lengths: tc.lengths,
				OutHex:  hx(out),
			})
		}
	}

	// Serpent block cipher vectors (128/192/256-bit keys).
	for _, tc := range []struct{ key, pt string }{
		{"80000000000000000000000000000000", "00000000000000000000000000000000"},
		{"0101010101010101010101010101010101010101010101010101010101010101", "01010101010101010101010101010101"},
	} {
		key := fromHexRng(tc.key)
		c, err := serpent.NewCipher(key)
		if err != nil {
			fail(err)
		}
		pt := fromHexRng(tc.pt)
		ct := make([]byte, 16)
		c.Encrypt(ct, pt)
		cases = append(cases, map[string]any{
			"kind":   "serpent",
			"key_hex": hx(key),
			"pt_hex":  hx(pt),
			"ct_hex":  hx(ct),
		})
	}

	write("rng.json", cases)
}

func fromHexRng(s string) []byte {
	b, err := hex.DecodeString(s)
	if err != nil {
		fail(err)
	}
	return b
}
