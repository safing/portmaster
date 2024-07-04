package kextinterface

import (
	"bytes"
	"errors"
	"math/rand"
	"os"
	"testing"
)

func TestRustInfoFile(t *testing.T) {
	t.Parallel()

	file, err := os.Open("testdata/rust_info_test.bin")
	if err != nil {
		panic(err)
	}
	defer func() {
		_ = file.Close()
	}()
	first := true
	for {
		info, err := RecvInfo(file)
		// First info should be with invalid size.
		// This tests if invalid info data is handled properly.
		if first {
			if !errors.Is(err, ErrUnexpectedInfoSize) {
				t.Errorf("unexpected error: %s\n", err)
			}
			first = false
			continue
		}
		if err != nil {
			if errors.Is(err, ErrUnexpectedReadError) {
				t.Errorf("unexpected error: %s\n", err)
			}
			return
		}

		switch {
		case info.LogLine != nil:
			if info.LogLine.Severity != 1 {
				t.Errorf("unexpected Log severity: %d\n", info.LogLine.Severity)
			}
			if info.LogLine.Line != "prefix: test log" {
				t.Errorf("unexpected Log line: %s\n", info.LogLine.Line)
			}

		case info.ConnectionV4 != nil:
			conn := info.ConnectionV4
			expected := connectionV4Internal{
				ID:           1,
				ProcessID:    2,
				Direction:    3,
				Protocol:     4,
				LocalIP:      [4]byte{1, 2, 3, 4},
				RemoteIP:     [4]byte{2, 3, 4, 5},
				LocalPort:    5,
				RemotePort:   6,
				PayloadLayer: 7,
			}
			if conn.connectionV4Internal != expected {
				t.Errorf("unexpected ConnectionV4: %+v\n", conn)
			}
			if !bytes.Equal(conn.Payload, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}) {
				t.Errorf("unexpected ConnectionV4 payload: %+v\n", conn.Payload)
			}

		case info.ConnectionV6 != nil:
			conn := info.ConnectionV6
			expected := connectionV6Internal{
				ID:           1,
				ProcessID:    2,
				Direction:    3,
				Protocol:     4,
				LocalIP:      [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
				RemoteIP:     [16]byte{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17},
				LocalPort:    5,
				RemotePort:   6,
				PayloadLayer: 7,
			}
			if conn.connectionV6Internal != expected {
				t.Errorf("unexpected ConnectionV6: %+v\n", conn)
			}
			if !bytes.Equal(conn.Payload, []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}) {
				t.Errorf("unexpected ConnectionV6 payload: %+v\n", conn.Payload)
			}

		case info.ConnectionEndV4 != nil:
			endEvent := info.ConnectionEndV4
			expected := ConnectionEndV4{
				ProcessID:  1,
				Direction:  2,
				Protocol:   3,
				LocalIP:    [4]byte{1, 2, 3, 4},
				RemoteIP:   [4]byte{2, 3, 4, 5},
				LocalPort:  4,
				RemotePort: 5,
			}
			if *endEvent != expected {
				t.Errorf("unexpected ConnectionEndV4: %+v\n", endEvent)
			}

		case info.ConnectionEndV6 != nil:
			endEvent := info.ConnectionEndV6
			expected := ConnectionEndV6{
				ProcessID:  1,
				Direction:  2,
				Protocol:   3,
				LocalIP:    [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
				RemoteIP:   [16]byte{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17},
				LocalPort:  4,
				RemotePort: 5,
			}
			if *endEvent != expected {
				t.Errorf("unexpected ConnectionEndV6: %+v\n", endEvent)
			}

		case info.BandwidthStats != nil:
			stats := info.BandwidthStats
			if stats.Protocol != 1 {
				t.Errorf("unexpected Bandwidth stats protocol: %d\n", stats.Protocol)
			}

			if stats.ValuesV4 != nil {
				if len(stats.ValuesV4) != 2 {
					t.Errorf("unexpected Bandwidth stats value length: %d\n", len(stats.ValuesV4))
				}
				expected1 := BandwidthValueV4{
					LocalIP:          [4]byte{1, 2, 3, 4},
					LocalPort:        1,
					RemoteIP:         [4]byte{2, 3, 4, 5},
					RemotePort:       2,
					TransmittedBytes: 3,
					ReceivedBytes:    4,
				}
				if stats.ValuesV4[0] != expected1 {
					t.Errorf("unexpected Bandwidth stats value: %+v expected: %+v\n", stats.ValuesV4[0], expected1)
				}
				expected2 := BandwidthValueV4{
					LocalIP:          [4]byte{1, 2, 3, 4},
					LocalPort:        5,
					RemoteIP:         [4]byte{2, 3, 4, 5},
					RemotePort:       6,
					TransmittedBytes: 7,
					ReceivedBytes:    8,
				}
				if stats.ValuesV4[1] != expected2 {
					t.Errorf("unexpected Bandwidth stats value: %+v expected: %+v\n", stats.ValuesV4[1], expected2)
				}

			} else if stats.ValuesV6 != nil {
				if len(stats.ValuesV6) != 2 {
					t.Errorf("unexpected Bandwidth stats value length: %d\n", len(stats.ValuesV6))
				}

				expected1 := BandwidthValueV6{
					LocalIP:          [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
					LocalPort:        1,
					RemoteIP:         [16]byte{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17},
					RemotePort:       2,
					TransmittedBytes: 3,
					ReceivedBytes:    4,
				}
				if stats.ValuesV6[0] != expected1 {
					t.Errorf("unexpected Bandwidth stats value: %+v expected: %+v\n", stats.ValuesV6[0], expected1)
				}
				expected2 := BandwidthValueV6{
					LocalIP:          [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
					LocalPort:        5,
					RemoteIP:         [16]byte{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17},
					RemotePort:       6,
					TransmittedBytes: 7,
					ReceivedBytes:    8,
				}
				if stats.ValuesV6[1] != expected2 {
					t.Errorf("unexpected Bandwidth stats value: %+v expected: %+v\n", stats.ValuesV6[1], expected2)
				}

			}
		}
	}
}

func TestGenerateCommandFile(t *testing.T) {
	t.Parallel()

	file, err := os.Create("../protocol/testdata/go_command_test.bin")
	if err != nil {
		t.Errorf("failed to create file: %s", err)
	}
	defer func() {
		_ = file.Close()
	}()
	enums := []byte{
		CommandShutdown,
		CommandVerdict,
		CommandUpdateV4,
		CommandUpdateV6,
		CommandClearCache,
		CommandGetLogs,
		CommandBandwidthStats,
		CommandCleanEndedConnections,
	}

	selected := make([]byte, 5000)
	for i := range selected {
		selected[i] = enums[rand.Intn(len(enums))] //nolint:gosec
	}

	for _, value := range selected {
		switch value {
		case CommandShutdown:
			err := SendShutdownCommand(file)
			if err != nil {
				t.Fatal(err)
			}

		case CommandVerdict:
			err := SendVerdictCommand(file, Verdict{
				ID:      1,
				Verdict: 2,
			})
			if err != nil {
				t.Fatal(err)
			}

		case CommandUpdateV4:
			err := SendUpdateV4Command(file, UpdateV4{
				Protocol:      1,
				LocalAddress:  [4]byte{1, 2, 3, 4},
				LocalPort:     2,
				RemoteAddress: [4]byte{2, 3, 4, 5},
				RemotePort:    3,
				Verdict:       4,
			})
			if err != nil {
				t.Fatal(err)
			}

		case CommandUpdateV6:
			err := SendUpdateV6Command(file, UpdateV6{
				Protocol:      1,
				LocalAddress:  [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
				LocalPort:     2,
				RemoteAddress: [16]byte{2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17},
				RemotePort:    3,
				Verdict:       4,
			})
			if err != nil {
				t.Fatal(err)
			}

		case CommandClearCache:
			err := SendClearCacheCommand(file)
			if err != nil {
				t.Fatal(err)
			}

		case CommandGetLogs:
			err := SendGetLogsCommand(file)
			if err != nil {
				t.Fatal(err)
			}

		case CommandBandwidthStats:
			err := SendGetBandwidthStatsCommand(file)
			if err != nil {
				t.Fatal(err)
			}

		case CommandPrintMemoryStats:
			err := SendPrintMemoryStatsCommand(file)
			if err != nil {
				t.Fatal(err)
			}

		case CommandCleanEndedConnections:
			err := SendCleanEndedConnectionsCommand(file)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
}
