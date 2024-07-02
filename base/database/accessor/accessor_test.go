//nolint:maligned,unparam
package accessor

import (
	"encoding/json"
	"testing"

	"github.com/safing/portmaster/base/utils"
)

type TestStruct struct {
	S    string
	A    []string
	I    int
	I8   int8
	I16  int16
	I32  int32
	I64  int64
	UI   uint
	UI8  uint8
	UI16 uint16
	UI32 uint32
	UI64 uint64
	F32  float32
	F64  float64
	B    bool
}

var (
	testStruct = &TestStruct{
		S:    "banana",
		A:    []string{"black", "white"},
		I:    42,
		I8:   42,
		I16:  42,
		I32:  42,
		I64:  42,
		UI:   42,
		UI8:  42,
		UI16: 42,
		UI32: 42,
		UI64: 42,
		F32:  42.42,
		F64:  42.42,
		B:    true,
	}
	testJSONBytes, _ = json.Marshal(testStruct) //nolint:errchkjson
	testJSON         = string(testJSONBytes)
)

func testGetString(t *testing.T, acc Accessor, key string, shouldSucceed bool, expectedValue string) {
	t.Helper()

	v, ok := acc.GetString(key)
	switch {
	case !ok && shouldSucceed:
		t.Errorf("%s failed to get string with key %s", acc.Type(), key)
	case ok && !shouldSucceed:
		t.Errorf("%s should have failed to get string with key %s, it returned %v", acc.Type(), key, v)
	}
	if v != expectedValue {
		t.Errorf("%s returned an unexpected value: wanted %v, got %v", acc.Type(), expectedValue, v)
	}
}

func testGetStringArray(t *testing.T, acc Accessor, key string, shouldSucceed bool, expectedValue []string) {
	t.Helper()

	v, ok := acc.GetStringArray(key)
	switch {
	case !ok && shouldSucceed:
		t.Errorf("%s failed to get []string with key %s", acc.Type(), key)
	case ok && !shouldSucceed:
		t.Errorf("%s should have failed to get []string with key %s, it returned %v", acc.Type(), key, v)
	}
	if !utils.StringSliceEqual(v, expectedValue) {
		t.Errorf("%s returned an unexpected value: wanted %v, got %v", acc.Type(), expectedValue, v)
	}
}

func testGetInt(t *testing.T, acc Accessor, key string, shouldSucceed bool, expectedValue int64) {
	t.Helper()

	v, ok := acc.GetInt(key)
	switch {
	case !ok && shouldSucceed:
		t.Errorf("%s failed to get int with key %s", acc.Type(), key)
	case ok && !shouldSucceed:
		t.Errorf("%s should have failed to get int with key %s, it returned %v", acc.Type(), key, v)
	}
	if v != expectedValue {
		t.Errorf("%s returned an unexpected value: wanted %v, got %v", acc.Type(), expectedValue, v)
	}
}

func testGetFloat(t *testing.T, acc Accessor, key string, shouldSucceed bool, expectedValue float64) {
	t.Helper()

	v, ok := acc.GetFloat(key)
	switch {
	case !ok && shouldSucceed:
		t.Errorf("%s failed to get float with key %s", acc.Type(), key)
	case ok && !shouldSucceed:
		t.Errorf("%s should have failed to get float with key %s, it returned %v", acc.Type(), key, v)
	}
	if int64(v) != int64(expectedValue) {
		t.Errorf("%s returned an unexpected value: wanted %v, got %v", acc.Type(), expectedValue, v)
	}
}

func testGetBool(t *testing.T, acc Accessor, key string, shouldSucceed bool, expectedValue bool) {
	t.Helper()

	v, ok := acc.GetBool(key)
	switch {
	case !ok && shouldSucceed:
		t.Errorf("%s failed to get bool with key %s", acc.Type(), key)
	case ok && !shouldSucceed:
		t.Errorf("%s should have failed to get bool with key %s, it returned %v", acc.Type(), key, v)
	}
	if v != expectedValue {
		t.Errorf("%s returned an unexpected value: wanted %v, got %v", acc.Type(), expectedValue, v)
	}
}

func testExists(t *testing.T, acc Accessor, key string, shouldSucceed bool) {
	t.Helper()

	ok := acc.Exists(key)
	switch {
	case !ok && shouldSucceed:
		t.Errorf("%s should report key %s as existing", acc.Type(), key)
	case ok && !shouldSucceed:
		t.Errorf("%s should report key %s as non-existing", acc.Type(), key)
	}
}

func testSet(t *testing.T, acc Accessor, key string, shouldSucceed bool, valueToSet interface{}) {
	t.Helper()

	err := acc.Set(key, valueToSet)
	switch {
	case err != nil && shouldSucceed:
		t.Errorf("%s failed to set %s to %+v: %s", acc.Type(), key, valueToSet, err)
	case err == nil && !shouldSucceed:
		t.Errorf("%s should have failed to set %s to %+v", acc.Type(), key, valueToSet)
	}
}

func TestAccessor(t *testing.T) {
	t.Parallel()

	// Test interface compliance.
	accs := []Accessor{
		NewJSONAccessor(&testJSON),
		NewJSONBytesAccessor(&testJSONBytes),
		NewStructAccessor(testStruct),
	}

	// get
	for _, acc := range accs {
		testGetString(t, acc, "S", true, "banana")
		testGetStringArray(t, acc, "A", true, []string{"black", "white"})
		testGetInt(t, acc, "I", true, 42)
		testGetInt(t, acc, "I8", true, 42)
		testGetInt(t, acc, "I16", true, 42)
		testGetInt(t, acc, "I32", true, 42)
		testGetInt(t, acc, "I64", true, 42)
		testGetInt(t, acc, "UI", true, 42)
		testGetInt(t, acc, "UI8", true, 42)
		testGetInt(t, acc, "UI16", true, 42)
		testGetInt(t, acc, "UI32", true, 42)
		testGetInt(t, acc, "UI64", true, 42)
		testGetFloat(t, acc, "F32", true, 42.42)
		testGetFloat(t, acc, "F64", true, 42.42)
		testGetBool(t, acc, "B", true, true)
	}

	// set
	for _, acc := range accs {
		testSet(t, acc, "S", true, "coconut")
		testSet(t, acc, "A", true, []string{"green", "blue"})
		testSet(t, acc, "I", true, uint32(44))
		testSet(t, acc, "I8", true, uint64(44))
		testSet(t, acc, "I16", true, uint8(44))
		testSet(t, acc, "I32", true, uint16(44))
		testSet(t, acc, "I64", true, 44)
		testSet(t, acc, "UI", true, 44)
		testSet(t, acc, "UI8", true, int64(44))
		testSet(t, acc, "UI16", true, int32(44))
		testSet(t, acc, "UI32", true, int8(44))
		testSet(t, acc, "UI64", true, int16(44))
		testSet(t, acc, "F32", true, 44.44)
		testSet(t, acc, "F64", true, 44.44)
		testSet(t, acc, "B", true, false)
	}

	// get again to check if new values were set
	for _, acc := range accs {
		testGetString(t, acc, "S", true, "coconut")
		testGetStringArray(t, acc, "A", true, []string{"green", "blue"})
		testGetInt(t, acc, "I", true, 44)
		testGetInt(t, acc, "I8", true, 44)
		testGetInt(t, acc, "I16", true, 44)
		testGetInt(t, acc, "I32", true, 44)
		testGetInt(t, acc, "I64", true, 44)
		testGetInt(t, acc, "UI", true, 44)
		testGetInt(t, acc, "UI8", true, 44)
		testGetInt(t, acc, "UI16", true, 44)
		testGetInt(t, acc, "UI32", true, 44)
		testGetInt(t, acc, "UI64", true, 44)
		testGetFloat(t, acc, "F32", true, 44.44)
		testGetFloat(t, acc, "F64", true, 44.44)
		testGetBool(t, acc, "B", true, false)
	}

	// failures
	for _, acc := range accs {
		testSet(t, acc, "S", false, true)
		testSet(t, acc, "S", false, false)
		testSet(t, acc, "S", false, 1)
		testSet(t, acc, "S", false, 1.1)

		testSet(t, acc, "A", false, "1")
		testSet(t, acc, "A", false, true)
		testSet(t, acc, "A", false, false)
		testSet(t, acc, "A", false, 1)
		testSet(t, acc, "A", false, 1.1)

		testSet(t, acc, "I", false, "1")
		testSet(t, acc, "I8", false, "1")
		testSet(t, acc, "I16", false, "1")
		testSet(t, acc, "I32", false, "1")
		testSet(t, acc, "I64", false, "1")
		testSet(t, acc, "UI", false, "1")
		testSet(t, acc, "UI8", false, "1")
		testSet(t, acc, "UI16", false, "1")
		testSet(t, acc, "UI32", false, "1")
		testSet(t, acc, "UI64", false, "1")

		testSet(t, acc, "F32", false, "1.1")
		testSet(t, acc, "F64", false, "1.1")

		testSet(t, acc, "B", false, "false")
		testSet(t, acc, "B", false, 1)
		testSet(t, acc, "B", false, 1.1)
	}

	// get again to check if values werent changed when an error occurred
	for _, acc := range accs {
		testGetString(t, acc, "S", true, "coconut")
		testGetStringArray(t, acc, "A", true, []string{"green", "blue"})
		testGetInt(t, acc, "I", true, 44)
		testGetInt(t, acc, "I8", true, 44)
		testGetInt(t, acc, "I16", true, 44)
		testGetInt(t, acc, "I32", true, 44)
		testGetInt(t, acc, "I64", true, 44)
		testGetInt(t, acc, "UI", true, 44)
		testGetInt(t, acc, "UI8", true, 44)
		testGetInt(t, acc, "UI16", true, 44)
		testGetInt(t, acc, "UI32", true, 44)
		testGetInt(t, acc, "UI64", true, 44)
		testGetFloat(t, acc, "F32", true, 44.44)
		testGetFloat(t, acc, "F64", true, 44.44)
		testGetBool(t, acc, "B", true, false)
	}

	// test existence
	for _, acc := range accs {
		testExists(t, acc, "S", true)
		testExists(t, acc, "A", true)
		testExists(t, acc, "I", true)
		testExists(t, acc, "I8", true)
		testExists(t, acc, "I16", true)
		testExists(t, acc, "I32", true)
		testExists(t, acc, "I64", true)
		testExists(t, acc, "UI", true)
		testExists(t, acc, "UI8", true)
		testExists(t, acc, "UI16", true)
		testExists(t, acc, "UI32", true)
		testExists(t, acc, "UI64", true)
		testExists(t, acc, "F32", true)
		testExists(t, acc, "F64", true)
		testExists(t, acc, "B", true)
	}

	// test non-existence
	for _, acc := range accs {
		testExists(t, acc, "X", false)
	}
}
