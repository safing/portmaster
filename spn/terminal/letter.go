package terminal

import (
	"github.com/safing/jess"
	"github.com/safing/structures/container"
	"github.com/safing/structures/varint"
)

// LetterFromWire parses a letter received over a network connection.
//
// It parses the same wire format (version 1) and returns the same result as
// jess.LetterFromWire, but checks the size of every block against the data
// that is available before reading the block. jess.LetterFromWire reads
// blocks with container.GetNextBlock, which allocates the received block size
// before checking that the data is there. As letters are parsed before they
// are authenticated, anyone who can send a few bytes to a crane or terminal
// could otherwise make it allocate terabytes, which crashes the process with
// an unrecoverable "out of memory" fatal error (see GetNextBlock).
// Always use this function instead of jess.LetterFromWire for data received
// from other parties.
func LetterFromWire(c *container.Container) (*jess.Letter, error) {
	// Reading from a container without data panics.
	if c.Length() == 0 {
		return nil, varint.ErrBufTooSmall
	}

	letter := &jess.Letter{}

	// Wire Format Version: varint
	wireFormatVersion, err := c.GetNextN8()
	if err != nil {
		return nil, err
	}
	if wireFormatVersion != 1 {
		return nil, jess.ErrIncompatibleWireFormatVersion
	}

	// Flags: varint
	// 	 - 1: Setup Msg (includes Version and Tools)
	// 	 - 2: Sending Keys
	// 	 - 4: Apply Keys
	flags, err := c.GetNextN64()
	if err != nil {
		return nil, err
	}
	setupMsg := flags&1 > 0
	sendingKeys := flags&2 > 0
	letter.ApplyKeys = flags&4 > 0

	if setupMsg {
		// Version: varint (if Setup Msg)
		letter.Version, err = c.GetNextN8()
		if err != nil {
			return nil, err
		}

		// SuiteID: byte block (if Setup Msg)
		suiteID, err := GetNextBlock(c)
		if err != nil {
			return nil, err
		}
		letter.SuiteID = string(suiteID)
	}

	if sendingKeys {
		// Keys:
		// 	 - Amount: varint
		// 	 - IDs/Values: byte blocks
		n, err := c.GetNextN8()
		if err != nil {
			return nil, err
		}
		letter.Keys = make([]*jess.Seal, n)
		for i := range letter.Keys {
			signetID, err := GetNextBlock(c)
			if err != nil {
				return nil, err
			}
			sealValue, err := GetNextBlock(c)
			if err != nil {
				return nil, err
			}
			letter.Keys[i] = &jess.Seal{
				ID:    string(signetID),
				Value: sealValue,
			}
		}
	}

	// Nonce: byte block
	letter.Nonce, err = GetNextBlock(c)
	if err != nil {
		return nil, err
	}

	// Data: byte block
	letter.Data, err = GetNextBlock(c)
	if err != nil {
		return nil, err
	}

	// MAC: byte block
	letter.Mac, err = GetNextBlock(c)
	if err != nil {
		return nil, err
	}

	return letter, nil
}
