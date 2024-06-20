package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"runtime"
	"strconv"
	"time"

	"github.com/safing/portmaster/base/log"
	"github.com/safing/portmaster/base/rng"
)

var (
	module *modules.Module

	outputFile *os.File
	outputSize uint64 = 1000000
)

func init() {
	module = modules.Register("main", prep, start, nil, "rng")
}

func main() {
	runtime.GOMAXPROCS(1)
	os.Exit(run.Run())
}

func prep() error {
	if len(os.Args) < 3 {
		fmt.Printf("usage: ./%s {fortuna|tickfeeder} <file> [output size in MB]", os.Args[0])
		return modules.ErrCleanExit
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
		module.StartWorker("fortuna", fortuna)

	case "tickfeeder":
		module.StartWorker("noise", noise)
		module.StartWorker("tickfeeder", tickfeeder)

	default:
		return fmt.Errorf("usage: ./%s {fortuna|tickfeeder}", os.Args[0])
	}

	return nil
}

func fortuna(_ context.Context) error {
	var bytesWritten uint64

	for {
		if module.IsStopping() {
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

	go modules.Shutdown() //nolint:errcheck
	return nil
}

func tickfeeder(ctx context.Context) error {
	var bytesWritten uint64
	var value int64
	var pushes int

	for {
		if module.IsStopping() {
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

	go modules.Shutdown() //nolint:errcheck
	return nil
}

func noise(ctx context.Context) error {
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
