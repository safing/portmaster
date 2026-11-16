package rng

import (
	"testing"
)

func init() {
	var err error
	module, err = New(struct{}{})
	if err != nil {
		panic(err)
	}

	err = module.Start()
	if err != nil {
		panic(err)
	}
}

func TestRNG(t *testing.T) { //nolint:paralleltest // Modifies the global rngCipher.
	key := make([]byte, 16)

	// Hold the rng lock, as the generator reads rngCipher when reseeding.
	rngLock.Lock()
	rngCipher = "aes"
	_, err := newCipher(key)
	if err != nil {
		t.Errorf("failed to create aes cipher: %s", err)
	}

	rngCipher = "serpent"
	_, err = newCipher(key)
	if err != nil {
		t.Errorf("failed to create serpent cipher: %s", err)
	}
	rngCipher = "aes"
	rngLock.Unlock()

	b := make([]byte, 32)
	_, err = Read(b)
	if err != nil {
		t.Errorf("Read failed: %s", err)
	}
	_, err = Reader.Read(b)
	if err != nil {
		t.Errorf("Read failed: %s", err)
	}

	_, err = Bytes(32)
	if err != nil {
		t.Errorf("Bytes failed: %s", err)
	}

	_, err = Number(100)
	if err != nil {
		t.Errorf("Number failed: %s", err)
	}
}
