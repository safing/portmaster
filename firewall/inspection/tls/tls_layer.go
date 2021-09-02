package tls

import (
	"crypto/x509"
	"encoding/binary"
	"fmt"
	"strings"

	"github.com/google/gopacket/layers"
	"github.com/safing/portbase/log"
)

type TLS struct {
	NumberOfRecords int

	Handshake        []TLSHandshakeRecord
	ChangeCipherSpec []TLSChangeCipherSpecRecord
	AppData          []TLSAppDataRecord
	Alert            []TLSAlertRecord
}

func (tls *TLS) String() string {
	// TODO(ppacher): add other record types
	var records []string
	for _, r := range tls.Handshake {
		records = append(records, r.String())
	}
	for _, r := range tls.ChangeCipherSpec {
		records = append(records, r.String())
	}
	for _, r := range tls.AppData {
		records = append(records, r.String())
	}
	for _, r := range tls.Alert {
		records = append(records, r.String())
	}
	return fmt.Sprintf("TLS-Records (#%d): %s", tls.NumberOfRecords, strings.Join(records, ";"))
}

func (tls *TLS) HasClientHello() bool {
	for _, h := range tls.Handshake {
		if h.ClientHello != nil {
			return true
		}
	}
	return false
}

func (tls *TLS) HasServerHello() bool {
	for _, h := range tls.Handshake {
		if h.ServerHello != nil {
			return true
		}
	}
	return false
}

func (tls *TLS) HasCertificate() bool {
	for _, h := range tls.Handshake {
		if h.CertificateMsg != nil {
			return true
		}
	}
	return false
}

// Version is a short-cut for searching the TLS records for either a
// server or client hello message and returning the TLS version
// specified there. If both hello messages are available, the server-
// hello takes precedence.
func (tls *TLS) Version() layers.TLSVersion {
	var (
		serverHelloVersion layers.TLSVersion
		clientHelloVersion layers.TLSVersion
	)
	for _, h := range tls.Handshake {
		if h.ServerHello != nil {
			serverHelloVersion = layers.TLSVersion(h.ServerHello.Vers)
		}
		if h.ClientHello != nil {
			clientHelloVersion = layers.TLSVersion(h.ClientHello.Vers)
		}
	}
	if serverHelloVersion > 0 {
		return serverHelloVersion
	}
	return clientHelloVersion
}

// SNI is a short utility method for accessing the ServerName sent
// in ClientHello messages. If there is not ClientHello handshake
// record in tls an empty string is returned.
func (tls *TLS) SNI() string {
	if len(tls.Handshake) == 0 {
		return ""
	}
	for _, r := range tls.Handshake {
		if r.ClientHello != nil {
			return r.ClientHello.ServerName
		}
	}
	return ""
}

func (tls *TLS) ServerCertChain() []*x509.Certificate {
	if len(tls.Handshake) == 0 {
		return nil
	}
	for _, r := range tls.Handshake {
		if r.CertificateMsg != nil {
			return r.Certificates()
		}
	}
	return nil
}

func (tls *TLS) DecodeNextRecord(data []byte, encrypted bool, truncated *bool, strict bool) error {
	var h layers.TLSRecordHeader
	h.ContentType = layers.TLSType(data[0])
	h.Version = layers.TLSVersion(binary.BigEndian.Uint16(data[1:3]))
	h.Length = binary.BigEndian.Uint16(data[3:5])

	hl := 5
	tl := hl + int(h.Length)
	if len(data) < tl {
		*truncated = true
		return fmt.Errorf("TLS packet length missmatch: %d is less than %d", len(data), tl)
	}

	tls.NumberOfRecords++

	var s fmt.Stringer
	switch h.ContentType {
	default:
		if !strict {
			log.Errorf("unknown TLS record type: %02x (length=%d version=%04x)", uint8(h.ContentType), h.Length, h.Version)
		} else {
			return fmt.Errorf("unknown TLS record type: %02d", uint8(h.ContentType))
		}
	case layers.TLSChangeCipherSpec:
		encrypted = true
		var r TLSChangeCipherSpecRecord
		if err := r.Decode(h, data[hl:tl]); err != nil {
			return err
		}
		tls.ChangeCipherSpec = append(tls.ChangeCipherSpec, r)
		s = &r

	case layers.TLSAlert:
		var r TLSAlertRecord
		if err := r.Decode(h, data[hl:tl]); err != nil {
			return err
		}
		tls.Alert = append(tls.Alert, r)
		s = &r

	case layers.TLSApplicationData:
		var r TLSAppDataRecord
		if err := r.Decode(h, data[hl:tl]); err != nil {
			return err
		}
		tls.AppData = append(tls.AppData, r)
		s = &r

	case layers.TLSHandshake:
		var r TLSHandshakeRecord
		if err := r.Decode(h, data[hl:tl], encrypted); err != nil {
			return err
		}
		tls.Handshake = append(tls.Handshake, r)
		s = &r
	}

	if s == nil {
		log.Debugf("tls-records: found record %s", formatRecordHeader(h))
	} else {
		log.Debugf("tls-records: found record %s", s.String())
	}

	if len(data) == tl {
		// this was the last TLS record in this
		// packet.
		return nil
	}

	//log.Debugf("tls-records: decoding next record: bytes-left: %d (encrypted=%v)", len(data)-tl, encrypted)

	// continue with the next record
	return tls.DecodeNextRecord(data[tl:], encrypted, truncated, strict)
}

type TLSAlertRecord struct {
	layers.TLSRecordHeader
}

func (r *TLSAlertRecord) Decode(h layers.TLSRecordHeader, data []byte) error {
	r.TLSRecordHeader = h
	return nil
}

func (r *TLSAlertRecord) String() string {
	return formatRecordHeader(r.TLSRecordHeader)
}

type TLSAppDataRecord struct {
	layers.TLSRecordHeader
}

func (r *TLSAppDataRecord) Decode(h layers.TLSRecordHeader, data []byte) error {
	r.TLSRecordHeader = h
	return nil
}

func (r *TLSAppDataRecord) String() string {
	return formatRecordHeader(r.TLSRecordHeader)
}

type TLSChangeCipherSpecRecord struct {
	layers.TLSRecordHeader
}

func (r *TLSChangeCipherSpecRecord) Decode(h layers.TLSRecordHeader, data []byte) error {
	r.TLSRecordHeader = h
	return nil
}

func (r *TLSChangeCipherSpecRecord) String() string {
	return formatRecordHeader(r.TLSRecordHeader)
}

func formatRecordHeader(h layers.TLSRecordHeader) string {
	return fmt.Sprintf("%s (%02d), length=%d vers=%s", h.ContentType.String(), uint8(h.ContentType), h.Length, h.Version.String())
}
