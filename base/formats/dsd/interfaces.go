package dsd

// GenCodeCompatible is an interface to identify and use gencode compatible structs.
type GenCodeCompatible interface {
	// GenCodeMarshal gencode marshalls the struct into the given byte array, or a new one if its too small.
	GenCodeMarshal(buf []byte) ([]byte, error)
	// GenCodeUnmarshal gencode unmarshalls the struct and returns the bytes read.
	GenCodeUnmarshal(buf []byte) (uint64, error)
}
