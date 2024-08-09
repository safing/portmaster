package container

import (
	"bytes"
	"testing"

	"github.com/safing/portmaster/base/utils"
)

var (
	testData         = []byte("The quick brown fox jumps over the lazy dog")
	testDataSplitted = [][]byte{
		[]byte("T"),
		[]byte("he"),
		[]byte(" qu"),
		[]byte("ick "),
		[]byte("brown"),
		[]byte(" fox j"),
		[]byte("umps ov"),
		[]byte("er the l"),
		[]byte("azy dog"),
	}
)

func TestContainerDataHandling(t *testing.T) {
	t.Parallel()

	c1 := New(utils.DuplicateBytes(testData))
	c1c := c1.carbonCopy()

	c2 := New()
	for range len(testData) {
		oneByte := make([]byte, 1)
		c1c.WriteToSlice(oneByte)
		c2.Append(oneByte)
	}
	c2c := c2.carbonCopy()

	c3 := New()
	for i := len(c2c.compartments) - 1; i >= c2c.offset; i-- {
		c3.Prepend(c2c.compartments[i])
	}
	c3c := c3.carbonCopy()

	d4 := make([]byte, len(testData)*2)
	n, _ := c3c.WriteToSlice(d4)
	d4 = d4[:n]
	c3c = c3.carbonCopy()

	d5 := make([]byte, len(testData))
	for i := range len(testData) {
		c3c.WriteToSlice(d5[i : i+1])
	}

	c6 := New()
	c6.Replace(testData)

	c7 := New(testDataSplitted[0])
	for i := 1; i < len(testDataSplitted); i++ {
		c7.Append(testDataSplitted[i])
	}

	c8 := New(testDataSplitted...)
	for range 110 {
		c8.Prepend(nil)
	}
	c8.clean()

	c9 := c8.PeekContainer(len(testData))

	c10 := c9.PeekContainer(len(testData) - 1)
	c10.Append(testData[len(testData)-1:])

	compareMany(t, testData, c1.CompileData(), c2.CompileData(), c3.CompileData(), d4, d5, c6.CompileData(), c7.CompileData(), c8.CompileData(), c9.CompileData(), c10.CompileData())
}

func compareMany(t *testing.T, reference []byte, other ...[]byte) {
	t.Helper()

	for i, cmp := range other {
		if !bytes.Equal(reference, cmp) {
			t.Errorf("sample %d does not match reference: sample is '%s'", i+1, string(cmp))
		}
	}
}

func TestDataFetching(t *testing.T) {
	t.Parallel()

	c1 := New(utils.DuplicateBytes(testData))
	data := c1.GetMax(1)
	if string(data[0]) != "T" {
		t.Errorf("failed to GetMax(1), got %s, expected %s", string(data), "T")
	}

	_, err := c1.Get(1000)
	if err == nil {
		t.Error("should fail")
	}

	_, err = c1.GetAsContainer(1000)
	if err == nil {
		t.Error("should fail")
	}
}

func TestBlocks(t *testing.T) {
	t.Parallel()

	c1 := New(utils.DuplicateBytes(testData))
	c1.PrependLength()

	n, err := c1.GetNextN8()
	if err != nil {
		t.Errorf("GetNextN8() failed: %s", err)
	}
	if n != 43 {
		t.Errorf("n should be 43, was %d", n)
	}
	c1.PrependLength()

	n2, err := c1.GetNextN16()
	if err != nil {
		t.Errorf("GetNextN16() failed: %s", err)
	}
	if n2 != 43 {
		t.Errorf("n should be 43, was %d", n2)
	}
	c1.PrependLength()

	n3, err := c1.GetNextN32()
	if err != nil {
		t.Errorf("GetNextN32() failed: %s", err)
	}
	if n3 != 43 {
		t.Errorf("n should be 43, was %d", n3)
	}
	c1.PrependLength()

	n4, err := c1.GetNextN64()
	if err != nil {
		t.Errorf("GetNextN64() failed: %s", err)
	}
	if n4 != 43 {
		t.Errorf("n should be 43, was %d", n4)
	}
}

func TestContainerBlockHandling(t *testing.T) {
	t.Parallel()

	c1 := New(utils.DuplicateBytes(testData))
	c1.PrependLength()
	c1.AppendAsBlock(testData)
	c1c := c1.carbonCopy()

	c2 := New(nil)
	for range c1.Length() {
		oneByte := make([]byte, 1)
		c1c.WriteToSlice(oneByte)
		c2.Append(oneByte)
	}

	c3 := New(testDataSplitted[0])
	for i := 1; i < len(testDataSplitted); i++ {
		c3.Append(testDataSplitted[i])
	}
	c3.PrependLength()

	d1, err := c1.GetNextBlock()
	if err != nil {
		t.Errorf("GetNextBlock failed: %s", err)
	}
	d2, err := c1.GetNextBlock()
	if err != nil {
		t.Errorf("GetNextBlock failed: %s", err)
	}
	d3, err := c2.GetNextBlock()
	if err != nil {
		t.Errorf("GetNextBlock failed: %s", err)
	}
	d4, err := c2.GetNextBlock()
	if err != nil {
		t.Errorf("GetNextBlock failed: %s", err)
	}
	d5, err := c3.GetNextBlock()
	if err != nil {
		t.Errorf("GetNextBlock failed: %s", err)
	}

	compareMany(t, testData, d1, d2, d3, d4, d5)
}

func TestContainerMisc(t *testing.T) {
	t.Parallel()

	c1 := New()
	d1 := c1.CompileData()
	if len(d1) > 0 {
		t.Fatalf("empty container should not hold any data")
	}
}

func TestDeprecated(t *testing.T) {
	t.Parallel()

	NewContainer(utils.DuplicateBytes(testData))
}
