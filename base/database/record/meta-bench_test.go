package record

// Benchmark:
// BenchmarkAllocateBytes-8                	2000000000	         0.76 ns/op
// BenchmarkAllocateStruct1-8              	2000000000	         0.76 ns/op
// BenchmarkAllocateStruct2-8              	2000000000	         0.79 ns/op
// BenchmarkMetaSerializeContainer-8       	 1000000	      1703 ns/op
// BenchmarkMetaUnserializeContainer-8     	 2000000	       950 ns/op
// BenchmarkMetaSerializeVarInt-8          	 3000000	       457 ns/op
// BenchmarkMetaUnserializeVarInt-8        	20000000	        62.9 ns/op
// BenchmarkMetaSerializeWithXDR2-8        	 1000000	      2360 ns/op
// BenchmarkMetaUnserializeWithXDR2-8      	  500000	      3189 ns/op
// BenchmarkMetaSerializeWithColfer-8      	10000000	       237 ns/op
// BenchmarkMetaUnserializeWithColfer-8    	20000000	        51.7 ns/op
// BenchmarkMetaSerializeWithCodegen-8     	50000000	        23.7 ns/op
// BenchmarkMetaUnserializeWithCodegen-8   	100000000	        18.9 ns/op
// BenchmarkMetaSerializeWithDSDJSON-8     	 1000000	      2398 ns/op
// BenchmarkMetaUnserializeWithDSDJSON-8   	  300000	      6264 ns/op

import (
	"testing"
	"time"

	"github.com/safing/structures/container"
	"github.com/safing/structures/dsd"
	"github.com/safing/structures/varint"
)

var testMeta = &Meta{
	Created:   time.Now().Unix(),
	Modified:  time.Now().Unix(),
	Expires:   time.Now().Unix(),
	Deleted:   time.Now().Unix(),
	secret:    true,
	cronjewel: true,
}

func BenchmarkAllocateBytes(b *testing.B) {
	for range b.N {
		_ = make([]byte, 33)
	}
}

func BenchmarkAllocateStruct1(b *testing.B) {
	for range b.N {
		var newMeta Meta
		_ = newMeta
	}
}

func BenchmarkAllocateStruct2(b *testing.B) {
	for range b.N {
		_ = Meta{}
	}
}

func BenchmarkMetaSerializeContainer(b *testing.B) {
	// Start benchmark
	for range b.N {
		c := container.New()
		c.AppendNumber(uint64(testMeta.Created))
		c.AppendNumber(uint64(testMeta.Modified))
		c.AppendNumber(uint64(testMeta.Expires))
		c.AppendNumber(uint64(testMeta.Deleted))
		switch {
		case testMeta.secret && testMeta.cronjewel:
			c.AppendNumber(3)
		case testMeta.secret:
			c.AppendNumber(1)
		case testMeta.cronjewel:
			c.AppendNumber(2)
		default:
			c.AppendNumber(0)
		}
	}
}

func BenchmarkMetaUnserializeContainer(b *testing.B) {
	// Setup
	c := container.New()
	c.AppendNumber(uint64(testMeta.Created))
	c.AppendNumber(uint64(testMeta.Modified))
	c.AppendNumber(uint64(testMeta.Expires))
	c.AppendNumber(uint64(testMeta.Deleted))
	switch {
	case testMeta.secret && testMeta.cronjewel:
		c.AppendNumber(3)
	case testMeta.secret:
		c.AppendNumber(1)
	case testMeta.cronjewel:
		c.AppendNumber(2)
	default:
		c.AppendNumber(0)
	}
	encodedData := c.CompileData()

	// Reset timer for precise results
	b.ResetTimer()

	// Start benchmark
	for range b.N {
		var newMeta Meta
		var err error
		var num uint64
		c := container.New(encodedData)
		num, err = c.GetNextN64()
		newMeta.Created = int64(num)
		if err != nil {
			b.Errorf("could not decode: %s", err)
			return
		}
		num, err = c.GetNextN64()
		newMeta.Modified = int64(num)
		if err != nil {
			b.Errorf("could not decode: %s", err)
			return
		}
		num, err = c.GetNextN64()
		newMeta.Expires = int64(num)
		if err != nil {
			b.Errorf("could not decode: %s", err)
			return
		}
		num, err = c.GetNextN64()
		newMeta.Deleted = int64(num)
		if err != nil {
			b.Errorf("could not decode: %s", err)
			return
		}

		flags, err := c.GetNextN8()
		if err != nil {
			b.Errorf("could not decode: %s", err)
			return
		}

		switch flags {
		case 3:
			newMeta.secret = true
			newMeta.cronjewel = true
		case 2:
			newMeta.cronjewel = true
		case 1:
			newMeta.secret = true
		case 0:
		default:
			b.Errorf("invalid flag value: %d", flags)
			return
		}
	}
}

func BenchmarkMetaSerializeVarInt(b *testing.B) {
	// Start benchmark
	for range b.N {
		encoded := make([]byte, 33)
		offset := 0
		data := varint.Pack64(uint64(testMeta.Created))
		for _, part := range data {
			encoded[offset] = part
			offset++
		}
		data = varint.Pack64(uint64(testMeta.Modified))
		for _, part := range data {
			encoded[offset] = part
			offset++
		}
		data = varint.Pack64(uint64(testMeta.Expires))
		for _, part := range data {
			encoded[offset] = part
			offset++
		}
		data = varint.Pack64(uint64(testMeta.Deleted))
		for _, part := range data {
			encoded[offset] = part
			offset++
		}

		switch {
		case testMeta.secret && testMeta.cronjewel:
			encoded[offset] = 3
		case testMeta.secret:
			encoded[offset] = 1
		case testMeta.cronjewel:
			encoded[offset] = 2
		default:
			encoded[offset] = 0
		}
	}
}

func BenchmarkMetaUnserializeVarInt(b *testing.B) {
	// Setup
	encoded := make([]byte, 33)
	offset := 0
	data := varint.Pack64(uint64(testMeta.Created))
	for _, part := range data {
		encoded[offset] = part
		offset++
	}
	data = varint.Pack64(uint64(testMeta.Modified))
	for _, part := range data {
		encoded[offset] = part
		offset++
	}
	data = varint.Pack64(uint64(testMeta.Expires))
	for _, part := range data {
		encoded[offset] = part
		offset++
	}
	data = varint.Pack64(uint64(testMeta.Deleted))
	for _, part := range data {
		encoded[offset] = part
		offset++
	}

	switch {
	case testMeta.secret && testMeta.cronjewel:
		encoded[offset] = 3
	case testMeta.secret:
		encoded[offset] = 1
	case testMeta.cronjewel:
		encoded[offset] = 2
	default:
		encoded[offset] = 0
	}
	offset++
	encodedData := encoded[:offset]

	// Reset timer for precise results
	b.ResetTimer()

	// Start benchmark
	for range b.N {
		var newMeta Meta
		offset = 0

		num, n, err := varint.Unpack64(encodedData)
		if err != nil {
			b.Error(err)
			return
		}
		testMeta.Created = int64(num)
		offset += n

		num, n, err = varint.Unpack64(encodedData[offset:])
		if err != nil {
			b.Error(err)
			return
		}
		testMeta.Modified = int64(num)
		offset += n

		num, n, err = varint.Unpack64(encodedData[offset:])
		if err != nil {
			b.Error(err)
			return
		}
		testMeta.Expires = int64(num)
		offset += n

		num, n, err = varint.Unpack64(encodedData[offset:])
		if err != nil {
			b.Error(err)
			return
		}
		testMeta.Deleted = int64(num)
		offset += n

		switch encodedData[offset] {
		case 3:
			newMeta.secret = true
			newMeta.cronjewel = true
		case 2:
			newMeta.cronjewel = true
		case 1:
			newMeta.secret = true
		case 0:
		default:
			b.Errorf("invalid flag value: %d", encodedData[offset])
			return
		}
	}
}

func BenchmarkMetaSerializeWithCodegen(b *testing.B) {
	for range b.N {
		_, err := testMeta.GenCodeMarshal(nil)
		if err != nil {
			b.Errorf("failed to serialize with codegen: %s", err)
			return
		}
	}
}

func BenchmarkMetaUnserializeWithCodegen(b *testing.B) {
	// Setup
	encodedData, err := testMeta.GenCodeMarshal(nil)
	if err != nil {
		b.Errorf("failed to serialize with codegen: %s", err)
		return
	}

	// Reset timer for precise results
	b.ResetTimer()

	// Start benchmark
	for range b.N {
		var newMeta Meta
		_, err := newMeta.GenCodeUnmarshal(encodedData)
		if err != nil {
			b.Errorf("failed to unserialize with codegen: %s", err)
			return
		}
	}
}

func BenchmarkMetaSerializeWithDSDJSON(b *testing.B) {
	for range b.N {
		_, err := dsd.Dump(testMeta, dsd.JSON)
		if err != nil {
			b.Errorf("failed to serialize with DSD/JSON: %s", err)
			return
		}
	}
}

func BenchmarkMetaUnserializeWithDSDJSON(b *testing.B) {
	// Setup
	encodedData, err := dsd.Dump(testMeta, dsd.JSON)
	if err != nil {
		b.Errorf("failed to serialize with DSD/JSON: %s", err)
		return
	}

	// Reset timer for precise results
	b.ResetTimer()

	// Start benchmark
	for range b.N {
		var newMeta Meta
		_, err := dsd.Load(encodedData, &newMeta)
		if err != nil {
			b.Errorf("failed to unserialize with DSD/JSON: %s", err)
			return
		}
	}
}
