package netutils

import (
	"sync"

	"github.com/google/gopacket"
	"github.com/google/gopacket/tcpassembly"
)

// SimpleStreamAssemblerManager is a simple manager for github.com/google/gopacket/tcpassembly.
type SimpleStreamAssemblerManager struct {
	InitLock      sync.Mutex
	lastAssembler *SimpleStreamAssembler
}

// New returns a new stream assembler.
func (m *SimpleStreamAssemblerManager) New(net, transport gopacket.Flow) tcpassembly.Stream {
	assembler := new(SimpleStreamAssembler)
	m.lastAssembler = assembler
	return assembler
}

// GetLastAssembler returns the newest created stream assembler.
func (m *SimpleStreamAssemblerManager) GetLastAssembler() *SimpleStreamAssembler {
	return m.lastAssembler
}

// SimpleStreamAssembler is a simple assembler for github.com/google/gopacket/tcpassembly.
type SimpleStreamAssembler struct {
	Cumulated    []byte
	CumulatedLen int
	Complete     bool
}

// NewSimpleStreamAssembler returns a new SimpleStreamAssembler.
func NewSimpleStreamAssembler() *SimpleStreamAssembler {
	return &SimpleStreamAssembler{}
}

// Reassembled implements tcpassembly.Stream's Reassembled function.
func (a *SimpleStreamAssembler) Reassembled(reassembly []tcpassembly.Reassembly) {
	for _, entry := range reassembly {
		a.Cumulated = append(a.Cumulated, entry.Bytes...)
	}
	a.CumulatedLen = len(a.Cumulated)
}

// ReassemblyComplete implements tcpassembly.Stream's ReassemblyComplete function.
func (a *SimpleStreamAssembler) ReassemblyComplete() {
	a.Complete = true
}
