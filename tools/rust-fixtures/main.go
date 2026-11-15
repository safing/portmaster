// Command rust-fixtures writes compatibility fixtures produced by the Go
// implementation. The Rust crates in crates/ compare their output against them.
package main

import (
	"bytes"
	"compress/gzip"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/safing/portmaster/base/api"
	"github.com/safing/portmaster/base/config"
	"github.com/safing/portmaster/base/database/query"
	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/portmaster/base/database/storage/sqlite"
	"github.com/safing/portmaster/base/notifications"
	"github.com/safing/portmaster/service/intel"
	"github.com/safing/portmaster/service/mgr"
	"github.com/safing/portmaster/service/netenv"
	"github.com/safing/portmaster/service/network/proc"
	"github.com/safing/portmaster/service/network/socket"
	"github.com/safing/portmaster/service/process"
	"github.com/safing/portmaster/service/profile"
	"github.com/safing/portmaster/service/profile/binmeta"
	"github.com/safing/portmaster/service/status"
	"github.com/safing/structures/dsd"
	"github.com/safing/structures/varint"
	"github.com/tannerryan/ring"
	"golang.org/x/net/publicsuffix"
)

type fixtureFile struct {
	Cases []any `json:"cases"`
	// Records optionally describes the JSON payloads the cases were evaluated
	// against, keyed by full record key. Only used by query.json.
	Records map[string]fixtureRecord `json:"records,omitempty"`
}

var (
	outDir    string
	verifyDir string
)

func main() {
	flag.StringVar(&outDir, "out", "crates/fixtures/m0", "output directory")
	flag.StringVar(&verifyDir, "verify", "", "verify a Rust-written sqlite location and print its contents as JSON")
	flag.Parse()

	if verifyDir != "" {
		verifyDatabase(verifyDir)
		return
	}

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
	genConfig()
	genProfileOptions()
	genNetState()
	genProcess()
	genNetwork()
	genProfile()
	genNotifications()
	genStatus()
	genIntel()
	genGeoIP()
	genUpdates()
	genEndpoints()
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

// verifyDatabase opens a database location written by the Rust implementation
// with the Go storage, then prints the query result keys, the raw table rows and
// the migration bookkeeping as JSON.
func verifyDatabase(location string) {
	db, err := sqlite.NewSQLite("core", location)
	if err != nil {
		fail(err)
	}

	// Reading through the Go storage proves that records decode and match.
	it, err := db.Query(query.New("core:"), true, true)
	if err != nil {
		fail(err)
	}
	queryKeys := []string{}
	for r := range it.Next {
		queryKeys = append(queryKeys, r.Key())
	}
	if err := it.Err(); err != nil {
		fail(err)
	}
	if err := db.Shutdown(); err != nil {
		fail(err)
	}

	// Dump the raw table for byte-level comparison.
	raw, err := sql.Open("sqlite", filepath.Join(location, "db.sqlite"))
	if err != nil {
		fail(err)
	}
	defer func() {
		if err := raw.Close(); err != nil {
			fail(err)
		}
	}()

	type rawRecord struct {
		Key        string `json:"key"`
		Format     int64  `json:"format"`
		Value      string `json:"value"`
		Created    int64  `json:"created"`
		Modified   int64  `json:"modified"`
		Expires    int64  `json:"expires"`
		Deleted    int64  `json:"deleted"`
		Secret     bool   `json:"secret"`
		Crownjewel bool   `json:"crownjewel"`
	}
	rows, err := raw.Query(`SELECT key, COALESCE(format, -1), COALESCE(hex(value), ''),
		created, modified, expires, deleted, secret, crownjewel FROM records ORDER BY key`)
	if err != nil {
		fail(err)
	}
	records := []rawRecord{}
	for rows.Next() {
		var r rawRecord
		if err := rows.Scan(&r.Key, &r.Format, &r.Value, &r.Created, &r.Modified,
			&r.Expires, &r.Deleted, &r.Secret, &r.Crownjewel); err != nil {
			fail(err)
		}
		records = append(records, r)
	}
	if err := rows.Err(); err != nil {
		fail(err)
	}

	type migration struct {
		ID        string `json:"id"`
		AppliedAt string `json:"applied_at"`
	}
	rows, err = raw.Query("SELECT id, CAST(applied_at AS TEXT) FROM gorp_migrations ORDER BY id")
	if err != nil {
		fail(err)
	}
	migrations := []migration{}
	for rows.Next() {
		var m migration
		if err := rows.Scan(&m.ID, &m.AppliedAt); err != nil {
			fail(err)
		}
		migrations = append(migrations, m)
	}
	if err := rows.Err(); err != nil {
		fail(err)
	}

	out, err := json.MarshalIndent(map[string]any{
		"query_keys": queryKeys,
		"records":    records,
		"migrations": migrations,
	}, "", "  ")
	if err != nil {
		fail(err)
	}
	fmt.Println(string(out))
}

// fakeInstance implements the config module's instance interface for fixture
// generation; no data directory is needed because saving is disabled.
type fakeInstance struct{}

func (fakeInstance) DataDir() string                  { return "" }
func (fakeInstance) SetCmdLineOperation(func() error) {}

// genConfig records the config file written by the Go implementation and the
// exported options of every type.
func genConfig() {
	if _, err := config.New(fakeInstance{}); err != nil {
		fail(err)
	}

	register := func(option *config.Option) {
		if err := config.Register(option); err != nil {
			fail(err)
		}
	}
	register(&config.Option{
		Name:            "String Option",
		Key:             "test/string",
		Description:     "A string option.",
		Help:            "Some help text.",
		Sensitive:       true,
		OptType:         config.OptTypeString,
		ExpertiseLevel:  config.ExpertiseLevelExpert,
		ReleaseLevel:    config.ReleaseLevelStable,
		DefaultValue:    "default",
		ValidationRegex: "^(default|banana|water)$",
		PossibleValues: []config.PossibleValue{
			{Name: "Default", Value: "default", Description: "The default."},
			{Name: "Banana", Value: "banana", Description: "A fruit."},
			{Name: "Water", Value: "water", Description: "A drink."},
		},
		Annotations: config.Annotations{
			config.DisplayHintAnnotation:  config.DisplayHintOneOf,
			config.DisplayOrderAnnotation: 10,
			config.CategoryAnnotation:     "Test",
		},
	})
	register(&config.Option{
		Name:           "String Array Option",
		Key:            "test/stringArray",
		Description:    "A string array option.",
		OptType:        config.OptTypeStringArray,
		ExpertiseLevel: config.ExpertiseLevelUser,
		ReleaseLevel:   config.ReleaseLevelBeta,
		DefaultValue:   []string{"default"},
	})
	register(&config.Option{
		Name:           "Int Option",
		Key:            "test/int",
		Description:    "An int option.",
		OptType:        config.OptTypeInt,
		ExpertiseLevel: config.ExpertiseLevelUser,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   7,
	})
	register(&config.Option{
		Name:           "Bool Option",
		Key:            "test/bool",
		Description:    "A bool option.",
		OptType:        config.OptTypeBool,
		ExpertiseLevel: config.ExpertiseLevelUser,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   false,
	})
	register(&config.Option{
		Name:           "Default Option",
		Key:            "test/withDefault",
		Description:    "An option with an internal default.",
		OptType:        config.OptTypeString,
		ExpertiseLevel: config.ExpertiseLevelUser,
		ReleaseLevel:   config.ReleaseLevelStable,
		DefaultValue:   "fallback",
	})

	set := func(key string, value any) {
		if err := config.SetConfigOption(key, value); err != nil {
			fail(err)
		}
	}
	set("test/string", "banana")
	set("test/stringArray", []string{"x<&>", "y"})
	set("test/int", 42)
	set("test/bool", true)
	if err := config.SetDefaultConfigOption("test/withDefault", "internal"); err != nil {
		fail(err)
	}

	file, err := config.MapToJSON(config.GetActiveConfigValues())
	if err != nil {
		fail(err)
	}
	flattened, err := config.JSONToMap(file)
	if err != nil {
		fail(err)
	}

	exports := map[string]string{}
	for _, key := range []string{
		"test/string", "test/stringArray", "test/int", "test/bool", "test/withDefault",
	} {
		option, err := config.GetOption(key)
		if err != nil {
			fail(err)
		}
		r, err := option.Export()
		if err != nil {
			fail(err)
		}
		wrapper, ok := r.(*record.Wrapper)
		if !ok {
			fail(errors.New("export is not a wrapper"))
		}
		exports[key] = string(wrapper.Data)
	}

	writeObject("config.json", map[string]any{
		"file":        string(file),
		"flattened":   flattened,
		"exports":     exports,
		"definitions": config.ExportOptions(),
	})
}

func writeObject(name string, object any) {
	data, err := json.MarshalIndent(object, "", "  ")
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, name), append(data, '\n'), 0o644); err != nil {
		fail(err)
	}
}

// genNetState records the kernel socket tables and how the Go parser reads
// them, plus a verified socket-inode to PID mapping.
func genNetState() {
	type connFixture struct {
		LocalIP    string `json:"local_ip"`
		LocalPort  uint16 `json:"local_port"`
		RemoteIP   string `json:"remote_ip"`
		RemotePort uint16 `json:"remote_port"`
		UID        int    `json:"uid"`
		Inode      int    `json:"inode"`
	}
	type bindFixture struct {
		LocalIP   string `json:"local_ip"`
		LocalPort uint16 `json:"local_port"`
		UID       int    `json:"uid"`
		Inode     int    `json:"inode"`
	}
	type tableFixture struct {
		Stack       string        `json:"stack"`
		Raw         string        `json:"raw"`
		Error       string        `json:"error,omitempty"`
		Connections []connFixture `json:"connections"`
		Binds       []bindFixture `json:"binds"`
	}

	readRaw := func(path string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		return string(data)
	}
	tableCase := func(stack, path string, conns []*socket.ConnectionInfo,
		binds []*socket.BindInfo, err error) tableFixture {
		fixture := tableFixture{
			Stack:       stack,
			Raw:         readRaw(path),
			Connections: []connFixture{},
			Binds:       []bindFixture{},
		}
		if err != nil {
			fixture.Error = err.Error()
			return fixture
		}
		for _, conn := range conns {
			fixture.Connections = append(fixture.Connections, connFixture{
				LocalIP: conn.Local.IP.String(), LocalPort: conn.Local.Port,
				RemoteIP: conn.Remote.IP.String(), RemotePort: conn.Remote.Port,
				UID: conn.UID, Inode: conn.Inode,
			})
		}
		for _, bind := range binds {
			fixture.Binds = append(fixture.Binds, bindFixture{
				LocalIP: bind.Local.IP.String(), LocalPort: bind.Local.Port,
				UID: bind.UID, Inode: bind.Inode,
			})
		}
		return fixture
	}

	var tables []tableFixture
	conns, binds, err := proc.GetTCP4Table()
	tables = append(tables, tableCase("tcp4", "/proc/net/tcp", conns, binds, err))
	conns, binds, err = proc.GetTCP6Table()
	tables = append(tables, tableCase("tcp6", "/proc/net/tcp6", conns, binds, err))
	binds, err = proc.GetUDP4Table()
	tables = append(tables, tableCase("udp4", "/proc/net/udp", nil, binds, err))
	binds, err = proc.GetUDP6Table()
	tables = append(tables, tableCase("udp6", "/proc/net/udp6", nil, binds, err))

	// Open a listener and let the Go implementation find our own PID for it.
	var lookup map[string]any
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		fail(err)
	}
	port := uint16(listener.Addr().(*net.TCPAddr).Port)
	if _, listeners, err := proc.GetTCP4Table(); err == nil {
		for _, bind := range listeners {
			if bind.Local.Port != port || !bind.Local.IP.Equal(net.IPv4(127, 0, 0, 1)) {
				continue
			}
			info := &socket.BindInfo{
				Local: bind.Local,
				UID:   os.Getuid(),
				Inode: bind.Inode,
				PID:   socket.UndefinedProcessID,
			}
			pid := proc.GetPID(info)
			if pid != os.Getpid() {
				fail(errors.New("proc.GetPID did not find the generating process"))
			}
			lookup = map[string]any{
				"uid":   os.Getuid(),
				"inode": bind.Inode,
				"pid":   pid,
			}
			break
		}
	}
	_ = listener.Close()
	if lookup == nil {
		fail(errors.New("could not verify the socket inode to pid lookup"))
	}

	// The fixture is committed, so no addresses, ports, UIDs, PIDs or inodes of
	// this machine may end up in the repository. The mapping is applied to the
	// raw tables and to the parsed expectations alike, which keeps them
	// consistent: the Rust parser must produce the mapped expectations from the
	// mapped raw tables.
	sanitizer := newNetSanitizer()
	for i := range tables {
		tables[i].Raw = sanitizer.sanitizeRaw(tables[i].Raw)
		conns := make([]connFixture, len(tables[i].Connections))
		for j, conn := range tables[i].Connections {
			conn.LocalIP = sanitizer.ip(conn.LocalIP)
			conn.LocalPort = sanitizer.port(conn.LocalPort)
			conn.RemoteIP = sanitizer.ip(conn.RemoteIP)
			conn.RemotePort = sanitizer.port(conn.RemotePort)
			conn.UID = sanitizer.uid(conn.UID)
			conn.Inode = sanitizer.inode(conn.Inode)
			conns[j] = conn
		}
		tables[i].Connections = conns
		binds := make([]bindFixture, len(tables[i].Binds))
		for j, bind := range tables[i].Binds {
			bind.LocalIP = sanitizer.ip(bind.LocalIP)
			bind.LocalPort = sanitizer.port(bind.LocalPort)
			bind.UID = sanitizer.uid(bind.UID)
			bind.Inode = sanitizer.inode(bind.Inode)
			binds[j] = bind
		}
		tables[i].Binds = binds
	}
	lookup["uid"] = sanitizer.uid(lookup["uid"].(int))
	lookup["inode"] = sanitizer.inode(lookup["inode"].(int))
	lookup["pid"] = 4242

	writeObject("netstate.json", map[string]any{
		"tables":     tables,
		"pid_lookup": lookup,
	})
}

// netSanitizer maps machine-specific values to documentation addresses and
// stable fake numbers.
type netSanitizer struct {
	ips      map[string]string
	ports    map[uint16]uint16
	inodes   map[int]int
	nextIP   int
	nextPort uint16
	nextInoe int
}

func newNetSanitizer() *netSanitizer {
	return &netSanitizer{
		ips:    make(map[string]string),
		ports:  make(map[uint16]uint16),
		inodes: make(map[int]int),
	}
}

func (s *netSanitizer) ip(ip string) string {
	if mapped, ok := s.ips[ip]; ok {
		return mapped
	}

	var mapped string
	if strings.Contains(ip, ":") {
		mapped = fmt.Sprintf("2001:db8::%x", s.nextIP+1)
	} else {
		s.nextIP++
		index := s.nextIP
		switch {
		case index <= 250:
			mapped = fmt.Sprintf("192.0.2.%d", index)
		case index <= 500:
			mapped = fmt.Sprintf("198.51.100.%d", index-250)
		default:
			mapped = fmt.Sprintf("203.0.113.%d", (index-500)%250+1)
		}
	}
	s.ips[ip] = mapped
	return mapped
}

func (s *netSanitizer) port(port uint16) uint16 {
	if mapped, ok := s.ports[port]; ok {
		return mapped
	}
	s.nextPort++
	mapped := 40000 + s.nextPort
	s.ports[port] = mapped
	return mapped
}

func (s *netSanitizer) uid(uid int) int {
	// One fake user for everything; UIDs are not needed for parsing.
	_ = uid
	return 1000
}

func (s *netSanitizer) inode(inode int) int {
	if mapped, ok := s.inodes[inode]; ok {
		return mapped
	}
	s.nextInoe++
	mapped := 200000 + s.nextInoe
	s.inodes[inode] = mapped
	return mapped
}

// sanitizeRaw rewrites the address, port, uid and inode columns of a table.
func (s *netSanitizer) sanitizeRaw(raw string) string {
	lines := strings.Split(raw, "\n")
	var out strings.Builder
	for i, line := range lines {
		if i == 0 {
			// Header row.
			out.WriteString(line)
			out.WriteString("\n")
			continue
		}
		if strings.TrimSpace(line) == "" {
			continue
		}

		tokens := strings.Fields(line)
		if len(tokens) < 10 {
			fail(fmt.Errorf("netstate: unsupported table line: %q", line))
		}
		tokens[1] = s.sanitizeAddress(tokens[1])
		tokens[2] = s.sanitizeAddress(tokens[2])
		uid, err := strconv.Atoi(tokens[7])
		if err != nil {
			fail(fmt.Errorf("netstate: invalid uid %q", tokens[7]))
		}
		tokens[7] = strconv.Itoa(s.uid(uid))
		inode, err := strconv.Atoi(tokens[9])
		if err != nil {
			fail(fmt.Errorf("netstate: invalid inode %q", tokens[9]))
		}
		tokens[9] = strconv.Itoa(s.inode(inode))

		out.WriteString(strings.Join(tokens, " "))
		out.WriteString("\n")
	}
	return out.String()
}

func (s *netSanitizer) sanitizeAddress(address string) string {
	ipHex, portHex, ok := strings.Cut(address, ":")
	if !ok {
		fail(fmt.Errorf("netstate: invalid address %q", address))
	}
	port, err := strconv.ParseUint(portHex, 16, 16)
	if err != nil {
		fail(fmt.Errorf("netstate: invalid port %q", portHex))
	}
	ip, err := decodeNetStateIP(ipHex)
	if err != nil {
		fail(err)
	}
	return encodeNetStateIP(s.ip(ip)) + ":" + fmt.Sprintf("%04X", s.port(uint16(port)))
}

// decodeNetStateIP decodes the kernel's hex address format.
func decodeNetStateIP(data string) (string, error) {
	decoded, err := hex.DecodeString(data)
	if err != nil {
		return "", err
	}
	switch len(decoded) {
	case 4:
		return net.IPv4(decoded[3], decoded[2], decoded[1], decoded[0]).String(), nil
	case 16:
		for i := 0; i < 16; i += 4 {
			decoded[i], decoded[i+1], decoded[i+2], decoded[i+3] = decoded[i+3], decoded[i+2], decoded[i+1], decoded[i]
		}
		return net.IP(decoded).String(), nil
	default:
		return "", fmt.Errorf("netstate: invalid address length %d", len(decoded))
	}
}

func encodeNetStateIP(ip string) string {
	parsed := net.ParseIP(ip)
	if ip4 := parsed.To4(); ip4 != nil {
		return hex.EncodeToString([]byte{ip4[3], ip4[2], ip4[1], ip4[0]})
	}
	decoded := parsed.To16()
	for i := 0; i < 16; i += 4 {
		decoded[i], decoded[i+1], decoded[i+2], decoded[i+3] = decoded[i+3], decoded[i+2], decoded[i+1], decoded[i]
	}
	return hex.EncodeToString(decoded)
}

// genProcess records the JSON form of a process, so the Rust port keeps the
// same field names and order.
func genProcess() {
	proc := &process.Process{
		Name:            "test-process",
		UserID:          1000,
		UserName:        "testuser",
		UserHome:        "/home/test",
		Pid:             4242,
		CreatedAt:       1_700_000_000_000,
		ParentPid:       1000,
		ParentCreatedAt: 1_600_000_000_000,
		LeaderPid:       1000,
		Path:            "/tmp/.mount_App/AppRun",
		ExecName:        "AppRun",
		Cwd:             "/home/test",
		CmdLine:         "AppRun --flag",
		FirstArg:        "AppRun",
		Env: map[string]string{
			"HOME": "/home/test",
			"PATH": "/usr/bin",
		},
		Tags: []profile.Tag{
			{Key: "app-image-path", Value: "/tmp/.mount_App/AppRun"},
			{Key: "custom", Value: "value"},
		},
		MatchingPath:     "/tmp/.mount_App/AppRun",
		PrimaryProfileID: "_unidentified",
		FirstSeen:        1_700_000_100,
		LastSeen:         1_700_000_200,
		Error:            "test error",
		ExecHashes: map[string]string{
			"sha256": "abc123",
		},
	}

	dumped, err := dsd.Dump(proc, dsd.JSON)
	if err != nil {
		fail(err)
	}

	writeObject("process.json", map[string]any{
		"process": string(dumped[1:]),
	})
}

// profileMatchingData implements profile.MatchingData for the fixtures.
type profileMatchingData struct {
	TagList          []profile.Tag
	EnvMap           map[string]string
	ProcessPath      string
	ProcessMatchPath string
	ProcessCmdline   string
}

func (md *profileMatchingData) Tags() []profile.Tag    { return md.TagList }
func (md *profileMatchingData) Env() map[string]string { return md.EnvMap }
func (md *profileMatchingData) Path() string           { return md.ProcessPath }
func (md *profileMatchingData) MatchingPath() string   { return md.ProcessMatchPath }
func (md *profileMatchingData) Cmdline() string        { return md.ProcessCmdline }

// genProfile records the profile JSON, fingerprint matching scores and derived
// profile IDs of the Go implementation.
func genProfile() {
	// Profile JSON, including icons, fingerprints and a nested config.
	prof := &profile.Profile{
		ID:                 "test-profile-id",
		Source:             profile.SourceLocal,
		Name:               "Test Profile",
		Description:        "A profile for fixtures.",
		Warning:            "Be careful.",
		WarningLastUpdated: time.Unix(1_700_000_200, 0).UTC(),
		Homepage:           "https://example.com",
		Icon:               "/tmp/icon.png",
		IconType:           binmeta.IconTypeFile,
		Icons: []binmeta.Icon{
			{Type: binmeta.IconTypeAPI, Value: "/api/v1/icons/1", Source: binmeta.IconSourceCore},
			{Type: binmeta.IconTypeFile, Value: "/tmp/icon.png", Source: binmeta.IconSourceUser},
		},
		LinkedPath:          "/usr/bin/test",
		PresentationPath:    "/usr/bin/test",
		UsePresentationPath: true,
		Fingerprints: []profile.Fingerprint{
			{Type: profile.FingerprintTypePathID, Operation: profile.FingerprintOperationEqualsID, Value: "/usr/bin/test"},
			{Type: profile.FingerprintTypeTagID, Key: "app-image-path", Operation: profile.FingerprintOperationPrefixID, Value: "/tmp/.mount_", MergedFrom: "local/other"},
		},
		Config: map[string]interface{}{
			"filter": map[string]interface{}{
				"defaultAction": "block",
				"endpoints":     []interface{}{"+ 1.1.1.1:443", "- 10.0.0.0/8"},
				"lists":         []interface{}{"list-a"},
			},
		},
		LastEdited: 1_700_000_100,
		Created:    1_700_000_000,
		Internal:   false,
	}
	dumped, err := dsd.Dump(prof, dsd.JSON)
	if err != nil {
		fail(err)
	}

	// Fingerprint matching cases.
	type matchCase struct {
		Name         string                `json:"name"`
		Fingerprints []profile.Fingerprint `json:"fingerprints"`
		LinkedPath   string                `json:"linked_path,omitempty"`
		Tags         []profile.Tag         `json:"tags,omitempty"`
		Env          map[string]string     `json:"env,omitempty"`
		Path         string                `json:"path,omitempty"`
		MatchingPath string                `json:"matching_path,omitempty"`
		Cmdline      string                `json:"cmdline,omitempty"`
		Score        int                   `json:"score"`
		Error        string                `json:"error,omitempty"`
	}
	fp := func(typ, key, op, value string) profile.Fingerprint {
		return profile.Fingerprint{Type: typ, Key: key, Operation: op, Value: value}
	}
	matchCases := []matchCase{
		{
			Name:         "tag-equals",
			Fingerprints: []profile.Fingerprint{fp("tag", "tag-a", "equals", "value")},
			Tags:         []profile.Tag{{Key: "tag-a", Value: "value"}},
		},
		{
			Name:         "tag-prefix",
			Fingerprints: []profile.Fingerprint{fp("tag", "key", "prefix", "pre")},
			Tags:         []profile.Tag{{Key: "key", Value: "prefix-123"}},
		},
		{
			Name:         "tag-regex",
			Fingerprints: []profile.Fingerprint{fp("tag", "k", "regex", "[0-9]+")},
			Tags:         []profile.Tag{{Key: "k", Value: "abc12345"}},
		},
		{
			Name:         "tag-key-mismatch",
			Fingerprints: []profile.Fingerprint{fp("tag", "other", "equals", "value")},
			Tags:         []profile.Tag{{Key: "tag-a", Value: "value"}},
			Path:         "/usr/bin/no-match",
		},
		{
			Name:         "cmdline-prefix",
			Fingerprints: []profile.Fingerprint{fp("cmdline", "", "prefix", "/usr/bin/python")},
			Cmdline:      "/usr/bin/python script.py",
		},
		{
			Name:         "cmdline-regex",
			Fingerprints: []profile.Fingerprint{fp("cmdline", "", "regex", "script\\.py")},
			Cmdline:      "/usr/bin/python script.py",
		},
		{
			Name:         "env-equals",
			Fingerprints: []profile.Fingerprint{fp("env", "APP_ID", "equals", "test")},
			Env:          map[string]string{"APP_ID": "test"},
		},
		{
			Name:         "matching-path-equals",
			Fingerprints: []profile.Fingerprint{fp("path", "", "equals", "/opt/app/bin/app")},
			Path:         "/opt/app/bin/python3",
			MatchingPath: "/opt/app/bin/app",
		},
		{
			Name:         "path-prefix",
			Fingerprints: []profile.Fingerprint{fp("path", "", "prefix", "/usr/bin/")},
			Path:         "/usr/bin/curl",
		},
		{
			Name:         "no-match",
			Fingerprints: []profile.Fingerprint{fp("path", "", "equals", "/nope")},
			Path:         "/usr/bin/curl",
		},
		{
			Name:         "regex-empty-match",
			Fingerprints: []profile.Fingerprint{fp("path", "", "regex", "a*")},
			Path:         "bcd",
		},
		{
			Name:       "linked-path-fallback",
			LinkedPath: "/usr/bin/legacy",
			Path:       "/usr/bin/legacy",
		},
		{
			Name:         "invalid-regex",
			Fingerprints: []profile.Fingerprint{fp("path", "", "regex", "[")},
			Path:         "/usr/bin/curl",
		},
	}
	for i := range matchCases {
		mc := &matchCases[i]
		parsed, parseErr := profile.ParseFingerprints(mc.Fingerprints, mc.LinkedPath)
		if parseErr != nil {
			mc.Error = parseErr.Error()
		}
		if parsed != nil {
			mc.Score = profile.MatchFingerprints(parsed, &profileMatchingData{
				TagList:          mc.Tags,
				EnvMap:           mc.Env,
				ProcessPath:      mc.Path,
				ProcessMatchPath: mc.MatchingPath,
				ProcessCmdline:   mc.Cmdline,
			})
		}
	}

	// Derived profile IDs, including duplicates and unsorted input.
	type idCase struct {
		Name         string                `json:"name"`
		Fingerprints []profile.Fingerprint `json:"fingerprints"`
		ID           string                `json:"id"`
	}
	idCases := []idCase{
		{
			Name: "single",
			Fingerprints: []profile.Fingerprint{
				fp("path", "", "equals", "/usr/bin/test"),
			},
		},
		{
			Name: "unsorted",
			Fingerprints: []profile.Fingerprint{
				fp("tag", "k", "prefix", "val"),
				fp("path", "", "equals", "/usr/bin/test"),
			},
		},
		{
			Name: "duplicates",
			Fingerprints: []profile.Fingerprint{
				fp("path", "", "equals", "/usr/bin/test"),
				fp("path", "", "equals", "/usr/bin/test"),
				fp("tag", "k", "equals", "v"),
			},
		},
		{
			Name: "merged-from",
			Fingerprints: []profile.Fingerprint{
				{Type: "path", Operation: "equals", Value: "/usr/bin/a", MergedFrom: "local/a"},
				{Type: "path", Operation: "equals", Value: "/usr/bin/a", MergedFrom: "local/b"},
			},
		},
	}
	for i := range idCases {
		// DeriveProfileID sorts the given slice in place (and compacts with a
		// zeroed tail); keep the original input for the fixture.
		original := append([]profile.Fingerprint(nil), idCases[i].Fingerprints...)
		idCases[i].ID = profile.DeriveProfileID(idCases[i].Fingerprints)
		idCases[i].Fingerprints = original
	}

	writeObject("profile.json", map[string]any{
		"profile":     string(dumped[1:]),
		"match_cases": matchCases,
		"id_cases":    idCases,
	})
}

// genNotifications records the JSON form of a notification.
func genNotifications() {
	n := &notifications.Notification{
		EventID:      "module:event-1",
		GUID:         "9f8b7c6d-5e4f-3a2b-1c0d-9e8f7a6b5c4d",
		Type:         notifications.Prompt,
		Title:        "Test Notification",
		Category:     "Test",
		Message:      "Do you want to continue?",
		ShowOnSystem: true,
		EventData: map[string]interface{}{
			"process": "curl",
			"count":   2,
		},
		Expires: 1_700_000_900,
		State:   notifications.Active,
		AvailableActions: []*notifications.Action{
			{
				ID:   "allow",
				Text: "Allow",
				Type: notifications.ActionTypeOpenSetting,
				Payload: notifications.ActionTypeOpenSettingPayload{
					Key:     "filter/defaultAction",
					Profile: "local/abc",
				},
				Visibility: notifications.ActionVisibilityDefault,
			},
			{
				ID:         "deny",
				Text:       "Deny",
				Type:       notifications.ActionTypeNone,
				Visibility: notifications.ActionVisibilityDetailed,
			},
			{
				ID:   "hook",
				Text: "Call",
				Type: notifications.ActionTypeWebhook,
				Payload: notifications.ActionTypeWebhookPayload{
					Method:       "POST",
					URL:          "/api/v1/test",
					Payload:      map[string]interface{}{"a": 1},
					ResultAction: "display",
				},
				Visibility: notifications.ActionVisibilityInAppOnly,
			},
		},
		SelectedActionID: "allow",
	}

	dumped, err := dsd.Dump(n, dsd.JSON)
	if err != nil {
		fail(err)
	}

	writeObject("notifications.json", map[string]any{
		"notification": string(dumped[1:]),
	})
}

// genStatus records the JSON forms of a module state update and of the system
// status record that is published via the runtime database.
func genStatus() {
	stateA := mgr.State{
		ID:      "module-a:error-1",
		Name:    "Module A Error",
		Message: "Something went wrong.",
		Type:    mgr.StateTypeError,
		Time:    time.Unix(1_700_000_000, 0).UTC(),
		Data:    map[string]interface{}{"code": 42},
	}
	stateB := mgr.State{
		ID:      "module-b:hint-1",
		Name:    "Module B Hint",
		Message: "Just so you know.",
		Type:    mgr.StateTypeHint,
		Time:    time.Unix(1_700_000_100, 0).UTC(),
	}
	update := mgr.StateUpdate{
		Module: "module-a",
		States: []mgr.State{stateA},
	}
	updateDump, err := dsd.Dump(update, dsd.JSON)
	if err != nil {
		fail(err)
	}

	record := &status.SystemStatusRecord{
		OnlineStatus: netenv.StatusSemiOnline,
		CaptivePortal: &netenv.CaptivePortal{
			URL:    "http://portal.example.com/login",
			Domain: "portal.example.com",
			IP:     net.ParseIP("192.0.2.10"),
		},
		Modules: []mgr.StateUpdate{
			{Module: "module-b", States: []mgr.State{stateB}},
			{Module: "module-a", States: []mgr.State{stateA}},
		},
	}
	record.WorstState.Module = "module-a"
	record.WorstState.State = stateA
	record.CreateMeta()
	record.SetKey("runtime:system/status")
	recordDump, err := dsd.Dump(record, dsd.JSON)
	if err != nil {
		fail(err)
	}

	writeObject("status.json", map[string]any{
		"state_update":  string(updateDump[1:]),
		"system_status": string(recordDump[1:]),
		"online_statuses": []any{
			map[string]any{"value": int(netenv.StatusUnknown), "name": netenv.StatusUnknown.String()},
			map[string]any{"value": int(netenv.StatusOffline), "name": netenv.StatusOffline.String()},
			map[string]any{"value": int(netenv.StatusLimited), "name": netenv.StatusLimited.String()},
			map[string]any{"value": int(netenv.StatusPortal), "name": netenv.StatusPortal.String()},
			map[string]any{"value": int(netenv.StatusSemiOnline), "name": netenv.StatusSemiOnline.String()},
			map[string]any{"value": int(netenv.StatusOnline), "name": netenv.StatusOnline.String()},
		},
	})
}

// genIntel records the filterlists bloom filters, DSDL decoding and the
// records written to the cache database.
func genIntel() {
	// Bloom filter parameters and content of a small test filter.
	type ringCase struct {
		Elements      int      `json:"elements"`
		FalsePositive float64  `json:"false_positive"`
		Size          uint64   `json:"size"`
		Hash          uint64   `json:"hash"`
		Values        []string `json:"values"`
		MarshalHex    string   `json:"marshal_hex"`
		Tests         []string `json:"tests"`
		TestResults   []bool   `json:"test_results"`
	}

	makeCase := func(elements int, falsePositive float64, values, tests []string) ringCase {
		r, err := ring.Init(elements, falsePositive)
		if err != nil {
			fail(err)
		}
		for _, value := range values {
			r.Add([]byte(value))
		}
		blob, err := r.MarshalBinary()
		if err != nil {
			fail(err)
		}
		// The size and hash rounds are unexported; read them from the
		// serialized filter (version byte, then two big-endian uint64s).
		results := make([]bool, 0, len(tests))
		for _, value := range tests {
			results = append(results, r.Test([]byte(value)))
		}
		return ringCase{
			Elements:      elements,
			FalsePositive: falsePositive,
			Size:          binary.BigEndian.Uint64(blob[1:9]),
			Hash:          binary.BigEndian.Uint64(blob[9:17]),
			Values:        values,
			MarshalHex:    hex.EncodeToString(blob),
			Tests:         tests,
			TestResults:   results,
		}
	}

	// The scoped filters of the filterlists package use these capacities with
	// a false positive rate of 0.001.
	type bfSize struct {
		Scope    string `json:"scope"`
		Elements int    `json:"elements"`
		Size     uint64 `json:"size"`
		Hash     uint64 `json:"hash"`
	}
	sizes := []bfSize{
		{Scope: "domain", Elements: 2000000},
		{Scope: "asn", Elements: 10000},
		{Scope: "country", Elements: 300},
		{Scope: "ipv4", Elements: 200000},
		{Scope: "ipv6", Elements: 20000},
	}
	for i := range sizes {
		r, err := ring.Init(sizes[i].Elements, 0.001)
		if err != nil {
			fail(err)
		}
		blob, err := r.MarshalBinary()
		if err != nil {
			fail(err)
		}
		sizes[i].Size = binary.BigEndian.Uint64(blob[1:9])
		sizes[i].Hash = binary.BigEndian.Uint64(blob[9:17])
	}

	values := []string{"example.com.", "192.0.2.10", "2001:db8::1", ""}
	ringTests := []ringCase{
		makeCase(1000, 0.001, values, append(values, "not-added.example.")),
		makeCase(100, 0.01, []string{"a", "b", "c"}, []string{"a", "b", "c", "d"}),
	}

	// DSDL filterlist file with four entries.
	type fixtureResource struct {
		SourceID   string `json:"sourceID"`
		ResourceID string `json:"resourceID"`
	}
	type fixtureEntry struct {
		Type      string            `json:"type"`
		Entity    string            `json:"entity"`
		Whitelist bool              `json:"whitelist"`
		Resources []fixtureResource `json:"resources"`
	}
	entries := []fixtureEntry{
		{
			Type:   "domain",
			Entity: "Example.COM",
			Resources: []fixtureResource{
				{SourceID: "list-a", ResourceID: "r1"},
				{SourceID: "list-a", ResourceID: "r2"},
				{SourceID: "list-b", ResourceID: "r3"},
			},
		},
		{
			Type:      "ipv4",
			Entity:    "192.0.2.10",
			Resources: []fixtureResource{{SourceID: "list-b", ResourceID: "r4"}},
		},
		{
			Type:      "domain",
			Entity:    "removed.example.",
			Whitelist: true,
			Resources: []fixtureResource{{SourceID: "list-a", ResourceID: "r5"}},
		},
		{
			Type:   "country",
			Entity: "XX",
		},
	}

	var body bytes.Buffer
	for _, entry := range entries {
		blob, err := dsd.Dump(entry, dsd.JSON)
		if err != nil {
			fail(err)
		}
		// Strip the format byte; the file header carries the format.
		blob = blob[1:]
		body.Write(varint.Pack64(uint64(len(blob))))
		body.Write(blob)
	}

	plain := append([]byte{dsd.LIST, dsd.JSON}, body.Bytes()...)

	var compressed bytes.Buffer
	gz := gzip.NewWriter(&compressed)
	if _, err := gz.Write(body.Bytes()); err != nil {
		fail(err)
	}
	if err := gz.Close(); err != nil {
		fail(err)
	}
	gzipped := append([]byte{dsd.LIST, dsd.GZIP, dsd.JSON}, compressed.Bytes()...)

	// Records written for the entries above, after normalization.
	type fixtureRecord struct {
		Key     string   `json:"key"`
		Deleted bool     `json:"deleted"`
		Sources []string `json:"sources"`
	}
	normalized := []fixtureRecord{
		{
			Key:     "cache:intel/filterlists/lists/domain/example.com.",
			Sources: []string{"list-a", "list-b"},
		},
		{
			Key:     "cache:intel/filterlists/lists/ipv4/192.0.2.10",
			Sources: []string{"list-b"},
		},
		{
			Key:     "cache:intel/filterlists/lists/domain/removed.example.",
			Deleted: true,
			Sources: []string{"list-a"},
		},
		{
			Key:     "cache:intel/filterlists/lists/country/XX",
			Sources: []string{},
		},
	}

	// JSON forms of the records.
	type entityRecordJSON struct {
		Value     string
		Sources   []string
		Type      string
		UpdatedAt int64
	}
	entityJSON, err := dsd.Dump(&entityRecordJSON{
		Value:     "example.com.",
		Sources:   []string{"list-a", "list-b"},
		Type:      "domain",
		UpdatedAt: 1_700_000_000,
	}, dsd.JSON)
	if err != nil {
		fail(err)
	}
	type versionRecordJSON struct {
		Version string
		Reset   string
	}
	versionJSON, err := dsd.Dump(&versionRecordJSON{
		Version: "v0.6.4",
		Reset:   "v0.6.0",
	}, dsd.JSON)
	if err != nil {
		fail(err)
	}

	// Block reasons for filter list matches.
	blockReason := intel.ListBlockReason{
		{
			Entity:        "example.com.",
			ActiveLists:   []string{"list-a", "list-b"},
			InactiveLists: []string{"list-c"},
		},
		{
			Entity:      "192.0.2.10",
			ActiveLists: []string{"list-b"},
		},
	}
	blockReasonJSON, err := blockReason.MarshalJSON()
	if err != nil {
		fail(err)
	}
	singleMatch := intel.ListMatch{
		Entity:        "example.com.",
		ActiveLists:   []string{"list-a", "list-b"},
		InactiveLists: []string{"list-c"},
	}

	// splitDomain results, using the Go public suffix list. This mirrors
	// service/intel/entity.go (the function is unexported there).
	type splitCase struct {
		Domain string   `json:"domain"`
		Parts  []string `json:"parts"`
	}
	splitDomains := []string{
		"www.books.amazon.co.uk.", "amazon.co.uk.", "books.amazon.co.uk.",
		"www.google.com", "example.com.", "a.b.ck", "www.ck", "ck",
		"cromulent", "foo.appspot.com", "deep.foo.appspot.com",
		"192.0.2.1", "", ".", "localhost", "XN--80AK6AA92E.com",
		"test.blogspot.com", "a.b.c.d.e.f.co.jp.",
		"foo.bar.example.unknowntld", "www.city.kawasaki.jp", "city.kawasaki.jp",
	}
	splitDomain := func(domain string) []string {
		d := strings.TrimSuffix(domain, ".")
		suffix, icann := publicsuffix.PublicSuffix(d)
		if suffix == d {
			return []string{domain}
		}
		labels := strings.FieldsFunc(d[:len(d)-len(suffix)], func(r rune) bool {
			return r == '.'
		})
		domains := make([]string, 0, len(labels)+1)
		for idx := range labels {
			domains = append(domains, strings.Join(labels[idx:], ".")+"."+suffix+".")
		}
		if !icann {
			domains = append(domains, suffix+".")
		}
		return domains
	}
	splitCases := make([]splitCase, 0, len(splitDomains))
	for _, domain := range splitDomains {
		splitCases = append(splitCases, splitCase{Domain: domain, Parts: splitDomain(domain)})
	}

	writeObject("intel.json", map[string]any{
		"split_domains": splitCases,
		"block_reason": map[string]any{
			"json":         string(blockReasonJSON),
			"string":       blockReason.String(),
			"match_string": singleMatch.String(),
		},
		"rings":          ringTests,
		"bf_sizes":       sizes,
		"entries":        entries,
		"file_plain":     base64.StdEncoding.EncodeToString(plain),
		"file_gzipped":   base64.StdEncoding.EncodeToString(gzipped),
		"records":        normalized,
		"entity_record":  string(entityJSON[1:]),
		"version_record": string(versionJSON[1:]),
	})
}
