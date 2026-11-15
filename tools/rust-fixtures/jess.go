// Fixtures for the Rust port of the jess JSON signature verification.
//
// Signatures are produced with the real jess library and a freshly generated
// Ed25519 signet; the canonical content is produced with the same gjson, sjson
// and pretty calls that filesig uses internally.
package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/pretty"
	"github.com/tidwall/sjson"

	"github.com/safing/jess"
	"github.com/safing/jess/filesig"
)

// jessSplit mirrors the unexported filesig.jsonSplit.
func jessSplit(data []byte) (content []byte, checksums, signatures []string, err error) {
	if !gjson.ValidBytes(data) {
		return nil, nil, nil, errors.New("invalid json")
	}
	content = data
	for _, key := range []string{filesig.JSONChecksumKey, filesig.JSONSignatureKey} {
		result := gjson.GetBytes(content, key)
		if !result.Exists() {
			continue
		}
		var list []string
		if result.IsArray() {
			for _, r := range result.Array() {
				if r.Type == gjson.String {
					list = append(list, r.String())
				}
			}
		} else if result.Type == gjson.String {
			list = []string{result.String()}
		}
		if key == filesig.JSONChecksumKey {
			checksums = list
		} else {
			signatures = list
		}
		content, err = sjson.DeleteBytes(content, key)
		if err != nil {
			return nil, nil, nil, err
		}
	}
	content = pretty.PrettyOptions(content, &pretty.Options{
		Width: 200, Prefix: "", Indent: " ", SortKeys: true,
	})
	return content, checksums, signatures, nil
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func newSigner(suite, id string) (*jess.Envelope, *jess.Signet, string) {
	signet, err := jess.GenerateSignet("Ed25519", 128)
	if err != nil {
		fail(err)
	}
	signet.ID = id
	signet.Info = &jess.SignetInfo{Name: id}
	exported, err := signet.Export(false)
	if err != nil {
		fail(err)
	}
	env := jess.NewUnconfiguredEnvelope()
	env.SuiteID = suite
	env.Senders = []*jess.Signet{signet}
	return env, signet, exported
}

func genJess() {
	signStore := jess.NewMemTrustStore()

	envA, sigA, exportA := newSigner(jess.SuiteSignV1, "test-key-a")
	envB, sigB, exportB := newSigner(jess.SuiteSignV1, "test-key-b")
	envF, sigF, exportF := newSigner(jess.SuiteSignFileV1, "test-key-file")
	envV2, sigV2, exportV2 := newSigner(jess.SuiteSignV2, "test-key-v2")
	// A key that is signing-capable but never part of the trust set.
	envU, sigU, _ := newSigner(jess.SuiteSignV1, "test-key-unknown")
	for _, s := range []*jess.Signet{sigA, sigB, sigF, sigV2, sigU} {
		if err := signStore.StoreSignet(s); err != nil {
			fail(err)
		}
	}

	index := []byte(`{
  "zeta": [1, 2.50, -3e2, "ü <>& é 🦀", true, null],
  "Name": "Portmaster Intel",
  "Version": "v1.2.3",
  "Nested": {"b": {"y": 1, "x": [ ], "w": {}}, "a": "x\"y\\z\/", "é": 1},
  "Artifacts": [
    {"Filename": "base.dsdl", "SHA256": "aa", "URLs": ["https://example.com/a?x=1&y=2"]},
    {"URLs": [], "Filename": "geoipv4.mmdb.gz", "SHA256": "bb"}
  ],
  "Long": [1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33,34,35,36,37,38,39,40,41,42,43,44,45,46,47,48,49,50,51,52,53,54,55,56,57,58,59,60],
  "Published": "2023-11-14T22:13:20Z"
}`)

	sign := func(data []byte, env *jess.Envelope) []byte {
		out, err := filesig.AddJSONSignature(data, env, signStore)
		if err != nil {
			fail(err)
		}
		return out
	}

	signedA := sign(index, envA)
	signedB := sign(index, envB)
	signedAB := sign(signedA, envB)
	signedF := sign(index, envF)
	signedV2 := sign(index, envV2)
	signedU := sign(index, envU)

	withChecksum, err := filesig.AddJSONChecksum(index)
	if err != nil {
		fail(err)
	}
	// Checksum and signature together.
	signedAndSummed, err := filesig.AddJSONChecksum(signedA)
	if err != nil {
		fail(err)
	}

	tamperedContent := []byte(string(signedA))
	tamperedContent = []byte(replaceOnce(string(tamperedContent), "v1.2.3", "v1.2.4"))
	tamperedSig := tamperSignature(signedA)
	tamperedChecksum := []byte(replaceOnce(string(withChecksum), "v1.2.3", "v9.9.9"))
	reordered := []byte(`{"Version":"v1.2.3","Name":"Portmaster Intel"}`)
	reorderedSigned := sign(reordered, envA)
	reorderedSwapped, err := sjson.SetBytes(
		[]byte(`{"Name": "Portmaster Intel",   "Version": "v1.2.3"}`),
		filesig.JSONSignatureKey, gjson.GetBytes(reorderedSigned, filesig.JSONSignatureKey).String())
	if err != nil {
		fail(err)
	}

	type sigCase struct {
		Name       string   `json:"name"`
		Input      string   `json:"input"`
		Content    string   `json:"content"`
		SplitError string   `json:"split_error"`
		Checksums  []string `json:"checksums"`
		Signatures []string `json:"signatures"`
		TrustSet   string   `json:"trust_set"`
		SigError   string   `json:"sig_error"`
		SumError   string   `json:"sum_error"`
	}

	trustSets := map[string]*jess.MemTrustStore{}
	mk := func(name string, signets ...*jess.Signet) {
		ts := jess.NewMemTrustStore()
		for _, s := range signets {
			pub, err := s.AsRecipient()
			if err != nil {
				fail(err)
			}
			if err := ts.StoreSignet(pub); err != nil {
				fail(err)
			}
		}
		trustSets[name] = ts
	}
	mk("a", sigA)
	mk("ab", sigA, sigB)
	mk("b", sigB)
	mk("file", sigF)
	mk("v2", sigV2)
	mk("empty")

	var cases []sigCase
	add := func(name string, input []byte, trust string) {
		c := sigCase{Name: name, Input: string(input), TrustSet: trust}
		content, sums, sigs, err := jessSplit(input)
		c.SplitError = errString(err)
		c.Content = string(content)
		c.Checksums = sums
		c.Signatures = sigs
		c.SigError = errString(filesig.VerifyJSONSignature(input, trustSets[trust]))
		c.SumError = errString(filesig.VerifyJSONChecksum(input))
		cases = append(cases, c)
	}

	add("valid", signedA, "a")
	add("valid-both-trusted", signedA, "ab")
	add("valid-second-key", signedB, "ab")
	add("two-signatures", signedAB, "ab")
	add("two-signatures-one-untrusted", signedAB, "a")
	add("signfile-suite", signedF, "file")
	add("sign-v2-suite", signedV2, "v2")
	add("unknown-signer", signedU, "a")
	add("wrong-trust-set", signedA, "b")
	add("empty-trust-set", signedA, "empty")
	add("tampered-content", tamperedContent, "a")
	add("tampered-signature", tamperedSig, "a")
	add("unsigned", index, "a")
	add("checksum-only", withChecksum, "a")
	add("checksum-tampered", tamperedChecksum, "a")
	add("checksum-and-signature", signedAndSummed, "a")
	add("whitespace-independent", reorderedSwapped, "a")
	add("invalid-json", []byte(`{"a": 1,}`), "a")
	add("not-an-object", []byte(`[1, 2, {"b": 1, "a": 2}]`), "a")
	add("signature-not-base64", []byte(`{"a": 1, "_jess-signature": "!!!"}`), "a")
	add("signature-not-cbor", []byte(`{"a": 1, "_jess-signature": "AAAA"}`), "a")
	add("signature-number", []byte(`{"a": 1, "_jess-signature": 5}`), "a")
	add("checksum-bad-label", []byte(`{"a": 1, "_jess-checksum": "abc"}`), "a")
	add("duplicate-meta-key", []byte(`{"a": 1, "_jess-checksum": "x", "_jess-checksum": "y"}`), "a")
	// gjson matches meta keys after unescaping, so a JSON-escaped key still
	// counts as the signature/checksum key.
	add("escaped-signature-key", []byte(replaceOnce(string(signedA),
		`"_jess-signature"`, `"\u005fjess-signature"`)), "a")
	add("escaped-checksum-key", []byte(replaceOnce(string(withChecksum),
		`"_jess-checksum"`, `"\u005fjess-checksum"`)), "a")
	add("escaped-signature-key-tampered", []byte(replaceOnce(string(tamperedContent),
		`"_jess-signature"`, `"\u005fjess-signature"`)), "a")

	// Pretty-printer only cases, including odd but valid JSON.
	prettyInputs := []string{
		`{}`, `[]`, `""`, `0`, `-1.5e3`, `true`, `null`,
		`{"b":1,"a":2}`,
		`{"a":{"d":1,"c":[3,2,1]},"B":"x"}`,
		`{"k":1,"k":0,"k":"s","k":null,"k":true,"k":[1],"k":{"z":1,"y":2}}`,
		`{"a":2,"a":1,"b":3}`,
		`{"s":"é","é":1,"e":2,"z":"\ud800","y":"\udc00x"}`,
		`{"n":10,"m":9,"l":-1,"k":1e2,"j":1E-2,"i":2.0,"h":2}`,
		`{"o":{"b":1,"a":2},"n":[{"b":1,"a":2},[1,2,{"b":1}]],"m":"x"}`,
		`[[1,2,3],[],{},[[]],[{"a":1}],"str",12]`,
		"  \n\t{ \"a\" : [ 1 ,\n 2 ] , \"b\" :\r\n{ } }  ",
		`[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33,34,35,36,37,38,39,40,41,42,43,44,45,46,47,48,49,50,51,52,53,54,55,56,57,58,59,60,61,62,63,64,65,66,67,68,69,70]`,
		`{"x":[1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20,21,22,23,24,25,26,27,28,29,30,31,32,33,34,35,36,37,38,39,40,41,42,43,44,45,46,47,48,49,50,51,52,53,54,55,56,57,58,59,60]}`,
		`{"x":[["aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"],["cccccccccccccccccccccccccccccccccccc","dddddddddddddddddddddddddddddddddddddddddd"]]}`,
		`{"x":[{"a":1},2]}`,
		`{"1e400":1e400,"a":-1e400,"b":1e-400}`,
		`"a\"b\\"`,
		`{"z":"</script>&<>","y":"  "}`,
	}
	var prettyCases []map[string]any
	for _, in := range prettyInputs {
		out := pretty.PrettyOptions([]byte(in), &pretty.Options{Width: 200, Prefix: "", Indent: " ", SortKeys: true})
		prettyCases = append(prettyCases, map[string]any{
			"input":  in,
			"valid":  gjson.Valid(in),
			"output": string(out),
		})
	}
	// Validity only.
	validInputs := []string{
		``, ` `, `{`, `}`, `{"a"}`, `{"a":}`, `{"a":1,}`, `[1,]`, `[,1]`, `[1 2]`, `01`, `-`, `1.`, `1e`, `.5`,
		`+1`, `NaN`, `tru`, `nul`, `"a`, "\"a\nb\"", `"\x"`, `"\u12"`, `{"a":1}{"b":2}`, `{"a":1} x`,
		"\ufeff{}", "\v{}", `{a:1}`, `{'a':1}`, `[1,2`, `{"a":[1,{"b":[]}]}`, "\t\r\n 1 \t\r\n", `-0`, `1E+5`, `0.5e-3`,
	}
	var validCases []map[string]any
	for _, in := range validInputs {
		validCases = append(validCases, map[string]any{"input": in, "valid": gjson.Valid(in)})
	}

	writeObject("jess.json", map[string]any{
		"signing_keys": map[string]string{
			"a": exportA, "b": exportB, "file": exportF, "v2": exportV2,
		},
		"cases":        cases,
		"pretty_cases": prettyCases,
		"valid_cases":  validCases,
	})
}

func replaceOnce(s, old, repl string) string {
	i := strings.Index(s, old)
	if i < 0 {
		fail(fmt.Errorf("replaceOnce: %q not found", old))
	}
	return s[:i] + repl + s[i+len(old):]
}

// tamperSignature flips one bit in the middle of the encoded signature.
func tamperSignature(data []byte) []byte {
	res := gjson.GetBytes(data, filesig.JSONSignatureKey)
	sig := []byte(res.String())
	pos := len(sig) - 8
	if sig[pos] == 'A' {
		sig[pos] = 'B'
	} else {
		sig[pos] = 'A'
	}
	out, err := sjson.SetBytes(data, filesig.JSONSignatureKey, string(sig))
	if err != nil {
		fail(err)
	}
	return out
}
