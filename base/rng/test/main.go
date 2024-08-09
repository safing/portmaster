package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/rng"
	"github.com/safing/portmaster/service/mgr"
)

// Test tests the rng.
type Test struct {
	mgr *mgr.Manager

	instance instance
}

var (
	module     *Test
	shimLoaded atomic.Bool

	outputFile *os.File
	outputSize uint64 = 1000000
)

func init() {
	// module = modules.Register("main", prep, start, nil, "rng")
}

func main() {
	runtime.GOMAXPROCS(1)
	var err error
	module, err = New(struct{}{})
	if err != nil {
		fmt.Printf("failed to initialize module: %s", err)
		return
	}

	err = start()
	if err != nil {
		fmt.Printf("failed to initialize module: %s", err)
		return
	}
}

func prep() error {
	if len(os.Args) < 3 {
		return fmt.Errorf("usage: ./%s {fortuna|tickfeeder} <file> [output size in MB]", os.Args[0])
	}

	switch os.Args[1] {
	case "fortuna":
	case "tickfeeder":
	default:
		return fmt.Errorf("usage: %s {fortuna|tickfeeder}", os.Args[0])
	}

	if len(os.Args) > 3 {
		n, err := strconv.ParseUint(os.Args[3], 10, 64)
		if err != nil {
			return fmt.Errorf("failed to parse output size: %w", err)
		}
		outputSize = n * 1000000
	}

	var err error
	outputFile, err = os.OpenFile(os.Args[2], os.O_CREATE|os.O_WRONLY, 0o0644) //nolint:gosec
	if err != nil {
		return fmt.Errorf("failed to open output file: %w", err)
	}

	return nil
}

//nolint:gocognit
func start() error {
	// generates 1MB and writes to stdout

	log.Infof("writing %dMB to stdout, a \".\" will be printed at every 1024 bytes.", outputSize/1000000)

	switch os.Args[1] {
	case "fortuna":
		module.mgr.Go("fortuna", fortuna)

	case "tickfeeder":
		module.mgr.Go("noise", noise)
		module.mgr.Go("tickfeeder", tickfeeder)

	default:
		return fmt.Errorf("usage: ./%s {fortuna|tickfeeder}", os.Args[0])
	}

	return nil
}

func fortuna(_ *mgr.WorkerCtx) error {
	var bytesWritten uint64

	for {
		if module.mgr.IsDone() {
			return nil
		}

		b, err := rng.Bytes(64)
		if err != nil {
			return err
		}
		_, err = outputFile.Write(b)
		if err != nil {
			return err
		}

		bytesWritten += 64
		if bytesWritten%1024 == 0 {
			_, _ = os.Stderr.WriteString(".")
		}
		if bytesWritten%65536 == 0 {
			fmt.Fprintf(os.Stderr, "\n%d bytes written\n", bytesWritten)
		}
		if bytesWritten >= outputSize {
			_, _ = os.Stderr.WriteString("\n")
			break
		}
	}

	go module.mgr.Cancel() //nolint:errcheck
	return nil
}

func tickfeeder(ctx *mgr.WorkerCtx) error {
	var bytesWritten uint64
	var value int64
	var pushes int

	for {
		if module.mgr.IsDone() {
			return nil
		}

		time.Sleep(10 * time.Nanosecond)

		value = (value << 1) | (time.Now().UnixNano() % 2)
		pushes++

		if pushes >= 64 {
			b := make([]byte, 8)
			binary.LittleEndian.PutUint64(b, uint64(value))
			_, err := outputFile.Write(b)
			if err != nil {
				return err
			}
			bytesWritten += 8
			if bytesWritten%1024 == 0 {
				_, _ = os.Stderr.WriteString(".")
			}
			if bytesWritten%65536 == 0 {
				fmt.Fprintf(os.Stderr, "\n%d bytes written\n", bytesWritten)
			}
			pushes = 0
		}

		if bytesWritten >= outputSize {
			_, _ = os.Stderr.WriteString("\n")
			break
		}
	}

	go module.mgr.Cancel() //nolint:errcheck
	return nil
}

func noise(ctx *mgr.WorkerCtx) error {
	// do some aes ctr for noise

	key, _ := hex.DecodeString("6368616e676520746869732070617373")
	data := []byte("some plaintext x")

	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}

	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		panic(err)
	}

	stream := cipher.NewCTR(block, iv)
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			stream.XORKeyStream(data, data)
		}
	}
}

// New returns a new rng test.
func New(instance instance) (*Test, error) {
	if !shimLoaded.CompareAndSwap(false, true) {
		return nil, errors.New("only one instance allowed")
	}

	m := mgr.New("geoip")
	module = &Test{
		mgr:      m,
		instance: instance,
	}

	if err := prep(); err != nil {
		return nil, err
	}

	return module, nil
}

type instance interface{}
