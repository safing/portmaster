// Command rust-fixtures writes compatibility fixtures produced by the Go
// implementation. The Rust crates in crates/ compare their output against them.
package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage/sqlite"
	"github.com/safing/structures/dsd"
	"github.com/safing/structures/varint"
)

type fixtureFile struct {
	Cases []any `json:"cases"`
	// Records optionally describes the JSON payloads the cases were evaluated
	// against, keyed by full record key. Only used by query.json.
	Records map[string]fixtureRecord `json:"records,omitempty"`
}

var outDir string

func main() {
	flag.StringVar(&outDir, "out", "crates/fixtures/m0", "output directory")
	flag.Parse()

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fail(err)
	}
	genVarint()
	genMeta()
	genGoJSON()
	genDSD()
	genWrapper()
	genAPIRecord()
	genQuery()
	genGoStrconv()
	genSQLite()
}

// genGoStrconv records the exact output of the Go strconv/formatting helpers
// the query package relies on, so the Rust port can match error strings and
// float formatting byte for byte.
func genGoStrconv() {
	var cases []any

	// %g formatting of float64 values, as used by Query.Print.
	for _, f := range []float64{
		0, 1, -1, 1.5, -42.5, 0.1, 0.25, 120.413, 121, 100000, 999999.5,
		1e-04, 1e-05, 1234567, 1e06, 1.2345678901234567, -0.0001234,
		math.MaxFloat64, math.SmallestNonzeroFloat64,
		math.Inf(1), math.Inf(-1), math.NaN(),
	} {
		cases = append(cases, map[string]any{
			"kind": "format_g",
			"bits": fmt.Sprintf("%016x", math.Float64bits(f)),
			"out":  fmt.Sprintf("%g", f),
		})
	}

	// strconv.Quote as used in strconv error messages.
	for _, q := range []string{
		"abc", "", "a b", "a\"b", `a\b`, "a\tb", "a\nb", "\x00\x01\x7f",
		"ключ", "Ünïcode <&>", "a\xd1", "\xff", "🦀",
	} {
		cases = append(cases, map[string]any{
			"kind":   "quote",
			"in_hex": hx([]byte(q)),
			"out":    strconv.Quote(q),
		})
	}

	// Number parsing errors; "out" is empty when parsing succeeds.
	intInputs := []string{
		"1", "-1", "+1", "abc", "", "-", "+", " 1", "1 ", "1.5", "0x10",
		"1_0", "99999999999999999999", "-99999999999999999999", "ключ",
		"-9223372036854775808", "9223372036854775807",
	}
	for _, in := range intInputs {
		c := map[string]any{"kind": "parse_int", "in_hex": hx([]byte(in))}
		if _, err := strconv.ParseInt(in, 10, 64); err != nil {
			c["out"] = err.Error()
		}
		cases = append(cases, c)
	}
	floatInputs := []string{
		"1", "-1.5", "1e5", "abc", "", "1.2.3", "1e400", "NaN", "inf",
		"-Infinity", "0x10", "ключ",
	}
	for _, in := range floatInputs {
		c := map[string]any{"kind": "parse_float", "in_hex": hx([]byte(in))}
		if _, err := strconv.ParseFloat(in, 64); err != nil {
			c["out"] = err.Error()
		}
		cases = append(cases, c)
	}
	boolInputs := []string{
		"1", "t", "T", "TRUE", "true", "True", "0", "f", "F", "FALSE",
		"false", "False", "great", "", " true", "2",
	}
	for _, in := range boolInputs {
		c := map[string]any{"kind": "parse_bool", "in_hex": hx([]byte(in))}
		if _, err := strconv.ParseBool(in); err != nil {
			c["out"] = err.Error()
		} else {
			c["out"] = "ok"
		}
		cases = append(cases, c)
	}

	write("gostrconv.json", cases)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func write(name string, cases []any) {
	data, err := json.MarshalIndent(fixtureFile{Cases: cases}, "", "  ")
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, name), append(data, '\n'), 0o644); err != nil {
		fail(err)
	}
}

func writeRecords(name string, cases []any, records map[string]fixtureRecord) {
	data, err := json.MarshalIndent(fixtureFile{Cases: cases, Records: records}, "", "  ")
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, name), append(data, '\n'), 0o644); err != nil {
		fail(err)
	}
}

func hx(b []byte) string { return hex.EncodeToString(b) }

func genVarint() {
	var cases []any
	for _, n := range []uint8{0, 1, 127, 128, 200, 255} {
		cases = append(cases, map[string]any{"bits": 8, "value": n, "packed": hx(varint.Pack8(n))})
	}
	for _, n := range []uint16{0, 127, 128, 16383, 16384, 65535} {
		cases = append(cases, map[string]any{"bits": 16, "value": n, "packed": hx(varint.Pack16(n))})
	}
	for _, n := range []uint32{0, 127, 128, 2097151, 2097152, 4294967295} {
		cases = append(cases, map[string]any{"bits": 32, "value": n, "packed": hx(varint.Pack32(n))})
	}
	for _, n := range []uint64{0, 127, 128, 1 << 35, 1<<63 - 1, 18446744073709551615} {
		// Value as string: JSON numbers lose precision above 2^53.
		cases = append(cases, map[string]any{"bits": 64, "value": fmt.Sprint(n), "packed": hx(varint.Pack64(n))})
	}
	cases = append(cases, map[string]any{"bits": 0, "value": "prepend_length", "input": hx([]byte("hello")), "packed": hx(varint.PrependLength([]byte("hello")))})
	write("varint.json", cases)
}

func genMeta() {
	metas := []struct {
		name               string
		m                  record.Meta
		secret, crownjewel bool
	}{
		{"zero", record.Meta{}, false, false},
		{"typical", record.Meta{Created: 1700000000, Modified: 1700000100, Expires: 1700086400}, false, false},
		{"deleted", record.Meta{Created: 1, Modified: 2, Deleted: 3}, false, false},
		{"negative", record.Meta{Created: -1, Modified: -9223372036854775808, Expires: 9223372036854775807, Deleted: -86400}, false, false},
		{"secret", record.Meta{Created: 5}, true, false},
		{"crownjewel", record.Meta{Created: 5}, false, true},
		{"both", record.Meta{Created: 5}, true, true},
	}
	var cases []any
	for _, tc := range metas {
		m := tc.m
		if tc.secret {
			m.MakeSecret()
		}
		if tc.crownjewel {
			m.MakeCrownJewel()
		}
		gc, err := m.GenCodeMarshal(nil)
		if err != nil {
			fail(err)
		}
		dsdBytes, err := dsd.Dump(&m, dsd.GenCode)
		if err != nil {
			fail(err)
		}
		js, _ := json.Marshal(&m)
		cases = append(cases, map[string]any{
			"name": tc.name, "created": m.Created, "modified": m.Modified, "expires": m.Expires, "deleted": m.Deleted,
			"secret": tc.secret, "crownjewel": tc.crownjewel,
			"gencode": hx(gc), "dsd": hx(dsdBytes), "json": string(js),
		})
	}
	write("meta_gencode.json", cases)
}

var tricky = []string{
	"", "plain", "<script>&amp;</script>", "quote\" backslash\\ slash/", "tab\t nl\n cr\r",
	"\x00\x01\x1f\x7f", "unicode: ключ 🦀", "  ", "pipe|colon:space ",
}

func genGoJSON() {
	var cases []any
	for _, s := range tricky {
		b, _ := json.Marshal(s)
		cases = append(cases, map[string]any{"kind": "string", "input": hx([]byte(s)), "json": string(b)})
	}
	invalid := []byte{'a', 0xff, 'b', 0xc3}
	b, _ := json.Marshal(string(invalid))
	cases = append(cases, map[string]any{"kind": "string", "input": hx(invalid), "json": string(b)})

	m := map[string]any{"zeta": 1, "alpha": []string{"<a>", "b"}, "mid": map[string]any{"y": nil, "x": 1.5}, "empty": []string{}, "nil": []string(nil)}
	b, _ = json.Marshal(m)
	cases = append(cases, map[string]any{"kind": "map", "json": string(b)})
	bi, _ := json.MarshalIndent(m, "", "  ")
	cases = append(cases, map[string]any{"kind": "map_indent2", "json": string(bi)})
	write("gojson.json", cases)
}

type sample struct {
	record.Base
	sync.Mutex

	Name   string
	Count  int64
	Ratio  float64
	Tags   []string
	Nested map[string]string
	Flag   bool
}

func newSample() *sample {
	return &sample{Name: "Ünïcode <&>", Count: -42, Ratio: 0.25, Tags: []string{"a", "b|c"}, Nested: map[string]string{"z": "1", "a": "2"}, Flag: true}
}

func genDSD() {
	var cases []any
	s := newSample()
	for _, f := range []uint8{dsd.JSON, dsd.CBOR, dsd.MsgPack} {
		b, err := dsd.Dump(s, f)
		if err != nil {
			fail(err)
		}
		cases = append(cases, map[string]any{"format": f, "dump": hx(b)})
	}
	b, err := dsd.DumpAndCompress(s, dsd.JSON, dsd.GZIP)
	if err != nil {
		fail(err)
	}
	cases = append(cases, map[string]any{"format": dsd.GZIP, "inner": dsd.JSON, "dump": hx(b)})
	write("dsd.json", cases)
}

func sampleRecords() []record.Record {
	var out []record.Record

	s := newSample()
	s.SetKey("core:test/sample/1")
	s.SetMeta(&record.Meta{Created: 1700000000, Modified: 1700000500, Expires: 0})
	out = append(out, s)

	u := newSample()
	u.Name = "ключ"
	u.SetKey("core:test/unicode/ключ 🦀|x")
	u.SetMeta(&record.Meta{Created: 1, Modified: 2, Expires: 1900000000})
	out = append(out, u)

	cb, err := dsd.Dump(newSample(), dsd.CBOR)
	if err != nil {
		fail(err)
	}
	w, err := record.NewWrapper("core:test/cbor/1", &record.Meta{Created: 10, Modified: 11}, dsd.CBOR, cb[1:])
	if err != nil {
		fail(err)
	}
	out = append(out, w)

	d := newSample()
	d.SetKey("core:test/deleted/1")
	dm := &record.Meta{Created: 1, Modified: 5}
	dm.Delete()
	dm.Deleted = 6
	d.SetMeta(dm)
	out = append(out, d)

	sec := newSample()
	sec.SetKey("core:test/secret/1")
	sm := &record.Meta{Created: 3, Modified: 3}
	sm.MakeSecret()
	sm.MakeCrownJewel()
	sec.SetMeta(sm)
	out = append(out, sec)

	exp := newSample()
	exp.SetKey("core:test/expired/1")
	exp.SetMeta(&record.Meta{Created: 1, Modified: 1, Expires: 2})
	out = append(out, exp)
	return out
}

func genWrapper() {
	var cases []any
	for _, r := range sampleRecords() {
		mr, err := r.MarshalRecord(r)
		if err != nil {
			fail(err)
		}
		w, err := record.NewRawWrapper(r.DatabaseName(), r.DatabaseKey(), mr)
		if err != nil {
			fail(err)
		}
		cases = append(cases, map[string]any{
			"key": r.Key(), "db": r.DatabaseName(), "db_key": r.DatabaseKey(),
			"marshal_record": hx(mr), "parsed_format": w.Format, "parsed_data": hx(w.Data),
			"meta": w.Meta(), "deleted": w.Meta().IsDeleted(),
		})
	}
	write("wrapper.json", cases)
}

func genAPIRecord() {
	var cases []any
	for _, r := range sampleRecords() {
		withID, err1 := api.MarshalRecord(r, true)
		without, err2 := api.MarshalRecord(r, false)
		c := map[string]any{"key": r.Key()}
		if err1 != nil || err2 != nil {
			c["error"] = fmt.Sprint(err1, err2)
		} else {
			c["with_dsd_id"] = hx(withID)
			c["without_dsd_id"] = string(without)
		}
		cases = append(cases, c)
	}
	write("api_record.json", cases)
}

var queries = []string{
	"query core:",
	"query core:test/",
	"query core:test/ where Name == ключ",
	`query core:test/ where Name == Ünïcode\ <&>`,
	"query core:test/ where Count > -50",
	"query core:test/ where Count >= -42 and Count <= -42",
	"query core:test/ where Count < 0 or Flag is false",
	"query core:test/ where not Flag is true",
	"query core:test/ where Ratio f== 0.25",
	"query core:test/ where Ratio f> 0.1",
	"query core:test/ where Ratio f< 0.1",
	"query core:test/ where Name sameas ключ",
	"query core:test/ where Name s== ключ",
	"query core:test/ where Name contains code",
	"query core:test/ where Name co code",
	"query core:test/ where Name startswith Ü",
	"query core:test/ where Name sw Ü",
	"query core:test/ where Name endswith >",
	"query core:test/ where Name ew >",
	"query core:test/ where Name in ключ,foo",
	"query core:test/ where Tags in a,b",
	`query core:test/ where Name matches ^Ü.*>$`,
	"query core:test/ where Name re ^k",
	"query core:test/ where Nested.a exists",
	"query core:test/ where Nested.q ex",
	"query core:test/ where Tags.0 == a",
	`query core:test/ where Tags.1 == b|c`,
	"query core:test/ where (Count > 0 or Flag is true) and Name exists",
	"query core:test/ where Count > 0 or (Flag is true and Ratio f>= 0.25)",
	"query core:test/ orderby Name",
	"query core:test/ limit 10",
	"query core:test/ limit 10 offset 5",
	"query core:test/ where Flag is true orderby Count limit 1 offset 0",
	"query  core:test/  where   Count   ==   -42",
	// Invalid queries.
	"",
	"query",
	"quer core:",
	"query core:test/ where",
	"query core:test/ where Name",
	"query core:test/ where Name ===",
	"query core:test/ where Count > abc",
	"query core:test/ where (Count > 1",
	"query core:test/ where Count > 1)",
	"query core:test/ limit x",
	"query core:test/ where Count > 1 and or Flag is true",
	`query core:test/ where Name == "quoted value"`,
}

// fixtureRecord is a record payload as loaded from storage.
type fixtureRecord struct {
	Format uint8  `json:"format"`
	Data   string `json:"data"`
}

// sampleJSONRecords mirrors sampleRecords, but wraps the payloads as JSON
// records — the shape the database hands to queries after loading them from
// storage. The CBOR record keeps its format, which has no JSON accessor.
func sampleJSONRecords() ([]record.Record, map[string]fixtureRecord) {
	var out []record.Record
	records := map[string]fixtureRecord{}

	add := func(key string, meta *record.Meta, mutate func(*sample)) {
		s := newSample()
		if mutate != nil {
			mutate(s)
		}
		dumped, err := dsd.Dump(s, dsd.JSON)
		if err != nil {
			fail(err)
		}
		payload := dumped[1:]
		w, err := record.NewWrapper(key, meta, dsd.JSON, payload)
		if err != nil {
			fail(err)
		}
		out = append(out, w)
		records[key] = fixtureRecord{Format: dsd.JSON, Data: hx(payload)}
	}

	add("core:test/sample/1", &record.Meta{Created: 1700000000, Modified: 1700000500}, nil)
	add("core:test/unicode/ключ 🦀|x", &record.Meta{Created: 1, Modified: 2, Expires: 1900000000},
		func(s *sample) { s.Name = "ключ" })

	// A CBOR record has no JSON accessor. Its content is irrelevant for the
	// match matrix, and Go's CBOR dump is not deterministic (map ordering), so
	// use a fixed empty CBOR map to keep the fixture reproducible.
	cborPayload := []byte{0xa0}
	w, err := record.NewWrapper("core:test/cbor/1", &record.Meta{Created: 10, Modified: 11}, dsd.CBOR, cborPayload)
	if err != nil {
		fail(err)
	}
	out = append(out, w)
	records["core:test/cbor/1"] = fixtureRecord{Format: dsd.CBOR, Data: hx(cborPayload)}

	deletedMeta := &record.Meta{Created: 1, Modified: 5}
	deletedMeta.Delete()
	deletedMeta.Deleted = 6
	add("core:test/deleted/1", deletedMeta, nil)

	secretMeta := &record.Meta{Created: 3, Modified: 3}
	secretMeta.MakeSecret()
	secretMeta.MakeCrownJewel()
	add("core:test/secret/1", secretMeta, nil)

	add("core:test/expired/1", &record.Meta{Created: 1, Modified: 1, Expires: 2}, nil)

	return out, records
}

func genQuery() {
	recs, records := sampleJSONRecords()
	var cases []any
	for _, qs := range queries {
		c := map[string]any{"query": qs}
		q, err := query.ParseQuery(qs)
		if err != nil {
			c["error"] = err.Error()
			cases = append(cases, c)
			continue
		}
		c["print"] = q.Print()
		c["db"] = q.DatabaseName()
		c["prefix"] = q.DatabaseKeyPrefix()
		matches := map[string]bool{}
		for _, r := range recs {
			matches[r.Key()] = q.MatchesKey(r.DatabaseKey()) && q.MatchesRecord(r)
		}
		c["matches"] = matches
		cases = append(cases, c)
	}
	writeRecords("query.json", cases, records)
}

func genSQLite() {
	dir := filepath.Join(outDir, "core_sample")
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fail(err)
	}
	db, err := sqlite.NewSQLite("core", dir)
	if err != nil {
		fail(err)
	}
	var cases []any
	for _, r := range sampleRecords() {
		if _, err := db.Put(r); err != nil {
			fail(err)
		}
		mr, _ := r.MarshalRecord(r)
		cases = append(cases, map[string]any{"key": r.Key(), "marshal_record": hx(mr)})
	}
	if err := db.Shutdown(); err != nil {
		fail(err)
	}
	// Fold WAL into the main file so the fixture is a single file.
	_ = os.Remove(filepath.Join(dir, "db.sqlite-wal"))
	_ = os.Remove(filepath.Join(dir, "db.sqlite-shm"))
	write("sqlite_records.json", cases)
}
