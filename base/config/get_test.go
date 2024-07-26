package config

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/safing/portmaster/base/log"
)

func parseAndReplaceConfig(jsonData string) error {
	m, err := JSONToMap([]byte(jsonData))
	if err != nil {
		return err
	}

	validationErrors, _ := ReplaceConfig(m)
	if len(validationErrors) > 0 {
		return fmt.Errorf("%d errors, first: %w", len(validationErrors), validationErrors[0])
	}
	return nil
}

func parseAndReplaceDefaultConfig(jsonData string) error {
	m, err := JSONToMap([]byte(jsonData))
	if err != nil {
		return err
	}

	validationErrors, _ := ReplaceDefaultConfig(m)
	if len(validationErrors) > 0 {
		return fmt.Errorf("%d errors, first: %w", len(validationErrors), validationErrors[0])
	}
	return nil
}

func quickRegister(t *testing.T, key string, optType OptionType, defaultValue interface{}) {
	t.Helper()

	err := Register(&Option{
		Name:           key,
		Key:            key,
		Description:    "test config",
		ReleaseLevel:   ReleaseLevelStable,
		ExpertiseLevel: ExpertiseLevelUser,
		OptType:        optType,
		DefaultValue:   defaultValue,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestGet(t *testing.T) { //nolint:paralleltest
	// reset
	options = make(map[string]*Option)

	err := log.Start()
	if err != nil {
		t.Fatal(err)
	}

	quickRegister(t, "monkey", OptTypeString, "c")
	quickRegister(t, "zebras/zebra", OptTypeStringArray, []string{"a", "b"})
	quickRegister(t, "elephant", OptTypeInt, -1)
	quickRegister(t, "hot", OptTypeBool, false)
	quickRegister(t, "cold", OptTypeBool, true)

	err = parseAndReplaceConfig(`
	{
		"monkey": "a",
		"zebras": {
			"zebra": ["black", "white"]
		},
		"elephant": 2,
		"hot": true,
		"cold": false
	}
	`)
	if err != nil {
		t.Fatal(err)
	}

	err = parseAndReplaceDefaultConfig(`
	{
		"monkey": "b",
		"snake": "0",
		"elephant": 0
	}
	`)
	if err != nil {
		t.Fatal(err)
	}

	monkey := GetAsString("monkey", "none")
	if monkey() != "a" {
		t.Errorf("monkey should be a, is %s", monkey())
	}

	zebra := GetAsStringArray("zebras/zebra", []string{})
	if len(zebra()) != 2 || zebra()[0] != "black" || zebra()[1] != "white" {
		t.Errorf("zebra should be [\"black\", \"white\"], is %v", zebra())
	}

	elephant := GetAsInt("elephant", -1)
	if elephant() != 2 {
		t.Errorf("elephant should be 2, is %d", elephant())
	}

	hot := GetAsBool("hot", false)
	if !hot() {
		t.Errorf("hot should be true, is %v", hot())
	}

	cold := GetAsBool("cold", true)
	if cold() {
		t.Errorf("cold should be false, is %v", cold())
	}

	err = parseAndReplaceConfig(`
	{
		"monkey": "3"
	}
	`)
	if err != nil {
		t.Fatal(err)
	}

	if monkey() != "3" {
		t.Errorf("monkey should be 0, is %s", monkey())
	}

	if elephant() != 0 {
		t.Errorf("elephant should be 0, is %d", elephant())
	}

	zebra()
	hot()

	// concurrent
	GetAsString("monkey", "none")()
	GetAsStringArray("zebras/zebra", []string{})()
	GetAsInt("elephant", -1)()
	GetAsBool("hot", false)()

	// perspective

	// load data
	pLoaded := make(map[string]interface{})
	err = json.Unmarshal([]byte(`{
		"monkey": "a",
		"zebras": {
			"zebra": ["black", "white"]
		},
		"elephant": 2,
		"hot": true,
		"cold": false
	}`), &pLoaded)
	if err != nil {
		t.Fatal(err)
	}

	// create
	p, err := NewPerspective(pLoaded)
	if err != nil {
		t.Fatal(err)
	}

	monkeyVal, ok := p.GetAsString("monkey")
	if !ok || monkeyVal != "a" {
		t.Errorf("[perspective] monkey should be a, is %+v", monkeyVal)
	}

	zebraVal, ok := p.GetAsStringArray("zebras/zebra")
	if !ok || len(zebraVal) != 2 || zebraVal[0] != "black" || zebraVal[1] != "white" {
		t.Errorf("[perspective] zebra should be [\"black\", \"white\"], is %+v", zebraVal)
	}

	elephantVal, ok := p.GetAsInt("elephant")
	if !ok || elephantVal != 2 {
		t.Errorf("[perspective] elephant should be 2, is %+v", elephantVal)
	}

	hotVal, ok := p.GetAsBool("hot")
	if !ok || !hotVal {
		t.Errorf("[perspective] hot should be true, is %+v", hotVal)
	}

	coldVal, ok := p.GetAsBool("cold")
	if !ok || coldVal {
		t.Errorf("[perspective] cold should be false, is %+v", coldVal)
	}
}

func TestReleaseLevel(t *testing.T) { //nolint:paralleltest
	// reset
	options = make(map[string]*Option)
	registerReleaseLevelOption()

	// setup
	subsystemOption := &Option{
		Name:           "test subsystem",
		Key:            "subsystem/test",
		Description:    "test config",
		ReleaseLevel:   ReleaseLevelStable,
		ExpertiseLevel: ExpertiseLevelUser,
		OptType:        OptTypeBool,
		DefaultValue:   false,
	}
	err := Register(subsystemOption)
	if err != nil {
		t.Fatal(err)
	}
	err = SetConfigOption("subsystem/test", true)
	if err != nil {
		t.Fatal(err)
	}
	testSubsystem := GetAsBool("subsystem/test", false)

	// test option level stable
	subsystemOption.ReleaseLevel = ReleaseLevelStable
	err = SetConfigOption(releaseLevelKey, ReleaseLevelNameStable)
	if err != nil {
		t.Fatal(err)
	}
	if !testSubsystem() {
		t.Error("should be active")
	}
	err = SetConfigOption(releaseLevelKey, ReleaseLevelNameBeta)
	if err != nil {
		t.Fatal(err)
	}
	if !testSubsystem() {
		t.Error("should be active")
	}
	err = SetConfigOption(releaseLevelKey, ReleaseLevelNameExperimental)
	if err != nil {
		t.Fatal(err)
	}
	if !testSubsystem() {
		t.Error("should be active")
	}

	// test option level beta
	subsystemOption.ReleaseLevel = ReleaseLevelBeta
	err = SetConfigOption(releaseLevelKey, ReleaseLevelNameStable)
	if err != nil {
		t.Fatal(err)
	}
	if testSubsystem() {
		t.Errorf("should be inactive: opt=%d system=%d", subsystemOption.ReleaseLevel, getReleaseLevel())
	}
	err = SetConfigOption(releaseLevelKey, ReleaseLevelNameBeta)
	if err != nil {
		t.Fatal(err)
	}
	if !testSubsystem() {
		t.Error("should be active")
	}
	err = SetConfigOption(releaseLevelKey, ReleaseLevelNameExperimental)
	if err != nil {
		t.Fatal(err)
	}
	if !testSubsystem() {
		t.Error("should be active")
	}

	// test option level experimental
	subsystemOption.ReleaseLevel = ReleaseLevelExperimental
	err = SetConfigOption(releaseLevelKey, ReleaseLevelNameStable)
	if err != nil {
		t.Fatal(err)
	}
	if testSubsystem() {
		t.Error("should be inactive")
	}
	err = SetConfigOption(releaseLevelKey, ReleaseLevelNameBeta)
	if err != nil {
		t.Fatal(err)
	}
	if testSubsystem() {
		t.Error("should be inactive")
	}
	err = SetConfigOption(releaseLevelKey, ReleaseLevelNameExperimental)
	if err != nil {
		t.Fatal(err)
	}
	if !testSubsystem() {
		t.Error("should be active")
	}
}

func BenchmarkGetAsStringCached(b *testing.B) {
	// reset
	options = make(map[string]*Option)

	// Setup
	err := parseAndReplaceConfig(`{
		"monkey": "banana"
	}`)
	if err != nil {
		b.Fatal(err)
	}
	monkey := GetAsString("monkey", "no banana")

	// Reset timer for precise results
	b.ResetTimer()

	// Start benchmark
	for range b.N {
		monkey()
	}
}

func BenchmarkGetAsStringRefetch(b *testing.B) {
	// Setup
	err := parseAndReplaceConfig(`{
		"monkey": "banana"
	}`)
	if err != nil {
		b.Fatal(err)
	}

	// Reset timer for precise results
	b.ResetTimer()

	// Start benchmark
	for range b.N {
		getValueCache("monkey", nil, OptTypeString)
	}
}

func BenchmarkGetAsIntCached(b *testing.B) {
	// Setup
	err := parseAndReplaceConfig(`{
		"elephant": 1
	}`)
	if err != nil {
		b.Fatal(err)
	}
	elephant := GetAsInt("elephant", -1)

	// Reset timer for precise results
	b.ResetTimer()

	// Start benchmark
	for range b.N {
		elephant()
	}
}

func BenchmarkGetAsIntRefetch(b *testing.B) {
	// Setup
	err := parseAndReplaceConfig(`{
		"elephant": 1
	}`)
	if err != nil {
		b.Fatal(err)
	}

	// Reset timer for precise results
	b.ResetTimer()

	// Start benchmark
	for range b.N {
		getValueCache("elephant", nil, OptTypeInt)
	}
}
