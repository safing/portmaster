// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package netutils

import (
	"sync"

	"github.com/google/gopacket"
	"github.com/google/gopacket/tcpassembly"
)

type SimpleStreamAssemblerManager struct {
	InitLock      sync.Mutex
	lastAssembler *SimpleStreamAssembler
}

func (m *SimpleStreamAssemblerManager) New(net, transport gopacket.Flow) tcpassembly.Stream {
	assembler := new(SimpleStreamAssembler)
	m.lastAssembler = assembler
	return assembler
}

func (m *SimpleStreamAssemblerManager) GetLastAssembler() *SimpleStreamAssembler {
	// defer func() {
	// 	m.lastAssembler = nil
	// }()
	return m.lastAssembler
}

type SimpleStreamAssembler struct {
	Cumulated    []byte
	CumulatedLen int
	Complete     bool
}

func NewSimpleStreamAssembler() *SimpleStreamAssembler {
	return new(SimpleStreamAssembler)
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
