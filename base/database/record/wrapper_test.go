package record

import (
	"bytes"
	"testing"

	"github.com/safing/structures/dsd"
)

func TestWrapper(t *testing.T) {
	t.Parallel()

	// check model interface compliance
	var m Record
	w := &Wrapper{}
	m = w
	_ = m

	// create test data
	testData := []byte(`{"a": "b"}`)
	encodedTestData := []byte(`J{"a": "b"}`)

	// test wrapper
	wrapper, err := NewWrapper("test:a", &Meta{}, dsd.JSON, testData)
	if err != nil {
		t.Fatal(err)
	}
	if wrapper.Format != dsd.JSON {
		t.Error("format mismatch")
	}
	if !bytes.Equal(testData, wrapper.Data) {
		t.Error("data mismatch")
	}

	encoded, err := wrapper.Marshal(wrapper, dsd.JSON)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encodedTestData, encoded) {
		t.Error("marshal mismatch")
	}

	wrapper.SetMeta(&Meta{})
	wrapper.meta.Update()
	raw, err := wrapper.MarshalRecord(wrapper)
	if err != nil {
		t.Fatal(err)
	}

	wrapper2, err := NewRawWrapper("test", "a", raw)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(testData, wrapper2.Data) {
		t.Error("marshal mismatch")
	}
}
