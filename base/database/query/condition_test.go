package query

import "testing"

func testSuccess(t *testing.T, c Condition) {
	t.Helper()

	err := c.check()
	if err != nil {
		t.Errorf("failed: %s", err)
	}
}

func TestInterfaces(t *testing.T) {
	t.Parallel()

	testSuccess(t, newIntCondition("banana", Equals, uint(1)))
	testSuccess(t, newIntCondition("banana", Equals, uint8(1)))
	testSuccess(t, newIntCondition("banana", Equals, uint16(1)))
	testSuccess(t, newIntCondition("banana", Equals, uint32(1)))
	testSuccess(t, newIntCondition("banana", Equals, int(1)))
	testSuccess(t, newIntCondition("banana", Equals, int8(1)))
	testSuccess(t, newIntCondition("banana", Equals, int16(1)))
	testSuccess(t, newIntCondition("banana", Equals, int32(1)))
	testSuccess(t, newIntCondition("banana", Equals, int64(1)))
	testSuccess(t, newIntCondition("banana", Equals, "1"))

	testSuccess(t, newFloatCondition("banana", FloatEquals, uint(1)))
	testSuccess(t, newFloatCondition("banana", FloatEquals, uint8(1)))
	testSuccess(t, newFloatCondition("banana", FloatEquals, uint16(1)))
	testSuccess(t, newFloatCondition("banana", FloatEquals, uint32(1)))
	testSuccess(t, newFloatCondition("banana", FloatEquals, int(1)))
	testSuccess(t, newFloatCondition("banana", FloatEquals, int8(1)))
	testSuccess(t, newFloatCondition("banana", FloatEquals, int16(1)))
	testSuccess(t, newFloatCondition("banana", FloatEquals, int32(1)))
	testSuccess(t, newFloatCondition("banana", FloatEquals, int64(1)))
	testSuccess(t, newFloatCondition("banana", FloatEquals, float32(1)))
	testSuccess(t, newFloatCondition("banana", FloatEquals, float64(1)))
	testSuccess(t, newFloatCondition("banana", FloatEquals, "1.1"))

	testSuccess(t, newStringCondition("banana", SameAs, "coconut"))
	testSuccess(t, newRegexCondition("banana", Matches, "coconut"))
	testSuccess(t, newStringSliceCondition("banana", FloatEquals, []string{"banana", "coconut"}))
	testSuccess(t, newStringSliceCondition("banana", FloatEquals, "banana,coconut"))
}

func testCondError(t *testing.T, c Condition) {
	t.Helper()

	err := c.check()
	if err == nil {
		t.Error("should fail")
	}
}

func TestConditionErrors(t *testing.T) {
	t.Parallel()

	// test invalid value types
	testCondError(t, newBoolCondition("banana", Is, 1))
	testCondError(t, newFloatCondition("banana", FloatEquals, true))
	testCondError(t, newIntCondition("banana", Equals, true))
	testCondError(t, newStringCondition("banana", SameAs, 1))
	testCondError(t, newRegexCondition("banana", Matches, 1))
	testCondError(t, newStringSliceCondition("banana", Matches, 1))

	// test error presence
	testCondError(t, newBoolCondition("banana", errorPresent, true))
	testCondError(t, And(newBoolCondition("banana", errorPresent, true)))
	testCondError(t, Or(newBoolCondition("banana", errorPresent, true)))
	testCondError(t, newExistsCondition("banana", errorPresent))
	testCondError(t, newFloatCondition("banana", errorPresent, 1.1))
	testCondError(t, newIntCondition("banana", errorPresent, 1))
	testCondError(t, newStringCondition("banana", errorPresent, "coconut"))
	testCondError(t, newRegexCondition("banana", errorPresent, "coconut"))
}

func TestWhere(t *testing.T) {
	t.Parallel()

	c := Where("", 254, nil)
	err := c.check()
	if err == nil {
		t.Error("should fail")
	}
}
