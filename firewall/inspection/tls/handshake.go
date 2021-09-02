package tls

import (
	"crypto/x509"
	"errors"
	"fmt"
	"sync"

	"github.com/google/gopacket/layers"
	utls "github.com/refraction-networking/utls"
	"github.com/safing/portbase/log"
)

// TLSHandshakeType defines the type of TLS handshake
type TLSHandshakeType uint8

// TLSHandshakeType known values.
const (
	TLSHandshakeHelloRequest       TLSHandshakeType = 0
	TLSHandshakeClientHello        TLSHandshakeType = 1
	TLSHandshakeServerHello        TLSHandshakeType = 2
	TLSHandshakeCertificate        TLSHandshakeType = 11
	TLSHandshakeServerKeyExchange  TLSHandshakeType = 12
	TLSHandshakeCertificateRequest TLSHandshakeType = 13
	TLSHandshakeServerDone         TLSHandshakeType = 14
	TLSHandshakeCertificateVerify  TLSHandshakeType = 15
	TLSHandshakeClientKeyExchange  TLSHandshakeType = 16
	TLSHandshakeFinished           TLSHandshakeType = 20
)

func (tht TLSHandshakeType) String() string {
	switch tht {
	case TLSHandshakeHelloRequest:
		return "HelloRequest"
	case TLSHandshakeClientHello:
		return "ClientHello"
	case TLSHandshakeServerHello:
		return "ServerHello"
	case TLSHandshakeCertificate:
		return "Certificate"
	case TLSHandshakeServerKeyExchange:
		return "ServerKeyExchange"
	case TLSHandshakeCertificateRequest:
		return "CertificateRequest"
	case TLSHandshakeServerDone:
		return "ServerDone"
	case TLSHandshakeCertificateVerify:
		return "CertificateVerify"
	case TLSHandshakeClientKeyExchange:
		return "ClientKeyExchange"
	case TLSHandshakeFinished:
		return "Finished"
	default:
		return "Unknown"
	}
}

var validHandshakeValues = map[TLSHandshakeType]bool{
	TLSHandshakeHelloRequest:       true,
	TLSHandshakeClientHello:        true,
	TLSHandshakeServerHello:        true,
	TLSHandshakeCertificate:        true,
	TLSHandshakeServerKeyExchange:  true,
	TLSHandshakeCertificateRequest: true,
	TLSHandshakeServerDone:         true,
	TLSHandshakeCertificateVerify:  true,
	TLSHandshakeClientKeyExchange:  true,
	TLSHandshakeFinished:           true,
}

// isEncrypted checks if packet seems encrypted (heuristics)
func isEncrypted(data []byte) bool {
	// heuristics used by wireshark
	// https://github.com/wireshark/wireshark/blob/d5fe2d494c6475263b954a36812b888b11e1a50b/epan/dissectors/packet-tls.c#L2158a
	if len(data) < 16 {
		return false
	}
	if len(data) > 0x010000 {
		return true
	}

	_, ok := validHandshakeValues[TLSHandshakeType(data[0])]
	return !ok
}

type TLSHandshakeRecord struct {
	layers.TLSRecordHeader

	Type   *TLSHandshakeType
	Record []byte

	ClientHello    *utls.ClientHelloMsg
	ServerHello    *utls.ServerHelloMsg
	CertificateMsg *utls.CertificateMsg

	certChainOnce sync.Once
	certChain     []*x509.Certificate
}

func (r *TLSHandshakeRecord) Certificates() []*x509.Certificate {
	if r.CertificateMsg == nil {
		return nil
	}

	r.certChainOnce.Do(func() {
		certs := make([]*x509.Certificate, len(r.CertificateMsg.Certificates))
		for idx, c := range r.CertificateMsg.Certificates {
			cert, err := x509.ParseCertificate(c)
			if err != nil {
				log.Errorf("failed to parse certificate: %s", err)
				certs[idx] = nil
			} else {
				certs[idx] = cert
			}
		}
		r.certChain = certs
	})
	return r.certChain
}

func (r *TLSHandshakeRecord) String() string {
	m := formatRecordHeader(r.TLSRecordHeader) + fmt.Sprintf(" Handshake-Type: %s", r.Type.String())
	if r.ClientHello != nil {
		v := layers.TLSVersion(r.ClientHello.Vers).String()
		m += fmt.Sprintf("{Version: %s, SNI: %s}", v, r.ClientHello.ServerName)
	}
	if r.ServerHello != nil {
		v := layers.TLSVersion(r.ServerHello.Vers).String()
		m += fmt.Sprintf("{Version: %s}", v)
	}
	if r.CertificateMsg != nil {
		m += fmt.Sprintf("{}")
	}
	return m
}

func (r *TLSHandshakeRecord) Decode(h layers.TLSRecordHeader, data []byte, encrypted bool) error {
	r.TLSRecordHeader = h
	r.Record = data

	encrypted = encrypted || isEncrypted(data)
	/*
		if encrypted {
			return nil
		}
	*/

	tlsType := TLSHandshakeType(data[0])
	r.Type = &tlsType

	switch tlsType {
	case TLSHandshakeClientHello:
		msg := utls.UnmarshalClientHello(r.Record)
		if msg == nil {
			return errors.New("failed to parse ClientHello")
		}
		r.ClientHello = msg
	case TLSHandshakeServerHello:
		msg := utls.UnmarshalServerHello(r.Record)
		if msg == nil {
			return errors.New("failed to parse ServerHello")
		}
		r.ServerHello = msg
	case TLSHandshakeCertificate:
		msg := utls.UnmarshalCertificateMsg(r.Record)
		if msg == nil {
			return errors.New("failed to parse Certificate")
		}
		r.CertificateMsg = msg
	}

	return nil
}
