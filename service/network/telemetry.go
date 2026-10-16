package network

import (
	"encoding/json"
	"net"

	"github.com/safing/portmaster/base/log"
)

// ConnectionTelemetry holds the mapped features to be passed
// to the external HIDS/HIPS Python sidecar logic via Unix socket.
type ConnectionTelemetry struct {
	PID           int    `json:"pid"`
	BinaryPath    string `json:"binaryPath"`
	DestIP        string `json:"destIP"`
	BytesSent     uint64 `json:"bytesSent"`
	BytesReceived uint64 `json:"bytesReceived"`
	Started       int64  `json:"started"`
	Ended         int64  `json:"ended"`
}

// sendTelemetry extracts required telemetry fields from a finalized connection
// and writes them over a non-blocking UDS connection to /tmp/portmaster_telemetry.sock
func sendTelemetry(conn *Connection) {
	if conn == nil {
		return
	}

	ipStr := ""
	if conn.Entity != nil {
		ipStr = conn.Entity.IP.String()
	}

	telemetry := ConnectionTelemetry{
		PID:           conn.PID,
		BinaryPath:    conn.ProcessContext.BinaryPath,
		DestIP:        ipStr,
		BytesSent:     conn.BytesSent,
		BytesReceived: conn.BytesReceived,
		Started:       conn.Started,
		Ended:         conn.Ended,
	}

	data, err := json.Marshal(telemetry)
	if err != nil {
		log.Errorf("telemetry: failed to marshal connection telemetry: %s", err)
		return
	}

	// Dispatch non-blocking dial and send to prevent slowing down packet filter
	go func(payload []byte) {
		socketConn, err := net.Dial("unix", "/tmp/portmaster_telemetry.sock")
		if err != nil {
			// Fail silently or log minimally; do not block Core service.
			// The python sidecar might not be running yet or at all.
			return
		}
		defer socketConn.Close()

		_, err = socketConn.Write(append(payload, '\n'))
		if err != nil {
			log.Errorf("telemetry: failed to write to socket: %s", err)
		}
	}(data)
}
