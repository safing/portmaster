// Copyright 2009 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package tlslib

import (
	"bytes"
	"strings"
)

type ClientHelloMsg struct {
	Raw                          []byte
	Vers                         uint16
	Random                       []byte
	SessionId                    []byte
	CipherSuites                 []uint16
	CompressionMethods           []uint8
	NextProtoNeg                 bool
	ServerName                   string
	OcspStapling                 bool
	Scts                         bool
	SupportedCurves              []CurveID
	SupportedPoints              []uint8
	TicketSupported              bool
	SessionTicket                []uint8
	SignatureAndHashes           []signatureAndHash
	SecureRenegotiation          []byte
	SecureRenegotiationSupported bool
	AlpnProtocols                []string
}

func (m *ClientHelloMsg) equal(i interface{}) bool {
	m1, ok := i.(*ClientHelloMsg)
	if !ok {
		return false
	}

	return bytes.Equal(m.Raw, m1.Raw) &&
		m.Vers == m1.Vers &&
		bytes.Equal(m.Random, m1.Random) &&
		bytes.Equal(m.SessionId, m1.SessionId) &&
		eqUint16s(m.CipherSuites, m1.CipherSuites) &&
		bytes.Equal(m.CompressionMethods, m1.CompressionMethods) &&
		m.NextProtoNeg == m1.NextProtoNeg &&
		m.ServerName == m1.ServerName &&
		m.OcspStapling == m1.OcspStapling &&
		m.Scts == m1.Scts &&
		eqCurveIDs(m.SupportedCurves, m1.SupportedCurves) &&
		bytes.Equal(m.SupportedPoints, m1.SupportedPoints) &&
		m.TicketSupported == m1.TicketSupported &&
		bytes.Equal(m.SessionTicket, m1.SessionTicket) &&
		eqSignatureAndHashes(m.SignatureAndHashes, m1.SignatureAndHashes) &&
		m.SecureRenegotiationSupported == m1.SecureRenegotiationSupported &&
		bytes.Equal(m.SecureRenegotiation, m1.SecureRenegotiation) &&
		eqStrings(m.AlpnProtocols, m1.AlpnProtocols)
}

func (m *ClientHelloMsg) marshal() []byte {
	if m.Raw != nil {
		return m.Raw
	}

	length := 2 + 32 + 1 + len(m.SessionId) + 2 + len(m.CipherSuites)*2 + 1 + len(m.CompressionMethods)
	numExtensions := 0
	extensionsLength := 0
	if m.NextProtoNeg {
		numExtensions++
	}
	if m.OcspStapling {
		extensionsLength += 1 + 2 + 2
		numExtensions++
	}
	if len(m.ServerName) > 0 {
		extensionsLength += 5 + len(m.ServerName)
		numExtensions++
	}
	if len(m.SupportedCurves) > 0 {
		extensionsLength += 2 + 2*len(m.SupportedCurves)
		numExtensions++
	}
	if len(m.SupportedPoints) > 0 {
		extensionsLength += 1 + len(m.SupportedPoints)
		numExtensions++
	}
	if m.TicketSupported {
		extensionsLength += len(m.SessionTicket)
		numExtensions++
	}
	if len(m.SignatureAndHashes) > 0 {
		extensionsLength += 2 + 2*len(m.SignatureAndHashes)
		numExtensions++
	}
	if m.SecureRenegotiationSupported {
		extensionsLength += 1 + len(m.SecureRenegotiation)
		numExtensions++
	}
	if len(m.AlpnProtocols) > 0 {
		extensionsLength += 2
		for _, s := range m.AlpnProtocols {
			if l := len(s); l == 0 || l > 255 {
				panic("invalid ALPN protocol")
			}
			extensionsLength++
			extensionsLength += len(s)
		}
		numExtensions++
	}
	if m.Scts {
		numExtensions++
	}
	if numExtensions > 0 {
		extensionsLength += 4 * numExtensions
		length += 2 + extensionsLength
	}

	x := make([]byte, 4+length)
	x[0] = TypeClientHello
	x[1] = uint8(length >> 16)
	x[2] = uint8(length >> 8)
	x[3] = uint8(length)
	x[4] = uint8(m.Vers >> 8)
	x[5] = uint8(m.Vers)
	copy(x[6:38], m.Random)
	x[38] = uint8(len(m.SessionId))
	copy(x[39:39+len(m.SessionId)], m.SessionId)
	y := x[39+len(m.SessionId):]
	y[0] = uint8(len(m.CipherSuites) >> 7)
	y[1] = uint8(len(m.CipherSuites) << 1)
	for i, suite := range m.CipherSuites {
		y[2+i*2] = uint8(suite >> 8)
		y[3+i*2] = uint8(suite)
	}
	z := y[2+len(m.CipherSuites)*2:]
	z[0] = uint8(len(m.CompressionMethods))
	copy(z[1:], m.CompressionMethods)

	z = z[1+len(m.CompressionMethods):]
	if numExtensions > 0 {
		z[0] = byte(extensionsLength >> 8)
		z[1] = byte(extensionsLength)
		z = z[2:]
	}
	if m.NextProtoNeg {
		z[0] = byte(extensionNextProtoNeg >> 8)
		z[1] = byte(extensionNextProtoNeg & 0xff)
		// The length is always 0
		z = z[4:]
	}
	if len(m.ServerName) > 0 {
		z[0] = byte(extensionServerName >> 8)
		z[1] = byte(extensionServerName & 0xff)
		l := len(m.ServerName) + 5
		z[2] = byte(l >> 8)
		z[3] = byte(l)
		z = z[4:]

		// RFC 3546, section 3.1
		//
		// struct {
		//     NameType name_type;
		//     select (name_type) {
		//         case host_name: HostName;
		//     } name;
		// } ServerName;
		//
		// enum {
		//     host_name(0), (255)
		// } NameType;
		//
		// opaque HostName<1..2^16-1>;
		//
		// struct {
		//     ServerName server_name_list<1..2^16-1>
		// } ServerNameList;

		z[0] = byte((len(m.ServerName) + 3) >> 8)
		z[1] = byte(len(m.ServerName) + 3)
		z[3] = byte(len(m.ServerName) >> 8)
		z[4] = byte(len(m.ServerName))
		copy(z[5:], []byte(m.ServerName))
		z = z[l:]
	}
	if m.OcspStapling {
		// RFC 4366, section 3.6
		z[0] = byte(extensionStatusRequest >> 8)
		z[1] = byte(extensionStatusRequest)
		z[2] = 0
		z[3] = 5
		z[4] = 1 // OCSP type
		// Two zero valued uint16s for the two lengths.
		z = z[9:]
	}
	if len(m.SupportedCurves) > 0 {
		// http://tools.ietf.org/html/rfc4492#section-5.5.1
		z[0] = byte(extensionSupportedCurves >> 8)
		z[1] = byte(extensionSupportedCurves)
		l := 2 + 2*len(m.SupportedCurves)
		z[2] = byte(l >> 8)
		z[3] = byte(l)
		l -= 2
		z[4] = byte(l >> 8)
		z[5] = byte(l)
		z = z[6:]
		for _, curve := range m.SupportedCurves {
			z[0] = byte(curve >> 8)
			z[1] = byte(curve)
			z = z[2:]
		}
	}
	if len(m.SupportedPoints) > 0 {
		// http://tools.ietf.org/html/rfc4492#section-5.5.2
		z[0] = byte(extensionSupportedPoints >> 8)
		z[1] = byte(extensionSupportedPoints)
		l := 1 + len(m.SupportedPoints)
		z[2] = byte(l >> 8)
		z[3] = byte(l)
		l--
		z[4] = byte(l)
		z = z[5:]
		for _, pointFormat := range m.SupportedPoints {
			z[0] = pointFormat
			z = z[1:]
		}
	}
	if m.TicketSupported {
		// http://tools.ietf.org/html/rfc5077#section-3.2
		z[0] = byte(extensionSessionTicket >> 8)
		z[1] = byte(extensionSessionTicket)
		l := len(m.SessionTicket)
		z[2] = byte(l >> 8)
		z[3] = byte(l)
		z = z[4:]
		copy(z, m.SessionTicket)
		z = z[len(m.SessionTicket):]
	}
	if len(m.SignatureAndHashes) > 0 {
		// https://tools.ietf.org/html/rfc5246#section-7.4.1.4.1
		z[0] = byte(extensionSignatureAlgorithms >> 8)
		z[1] = byte(extensionSignatureAlgorithms)
		l := 2 + 2*len(m.SignatureAndHashes)
		z[2] = byte(l >> 8)
		z[3] = byte(l)
		z = z[4:]

		l -= 2
		z[0] = byte(l >> 8)
		z[1] = byte(l)
		z = z[2:]
		for _, sigAndHash := range m.SignatureAndHashes {
			z[0] = sigAndHash.hash
			z[1] = sigAndHash.signature
			z = z[2:]
		}
	}
	if m.SecureRenegotiationSupported {
		z[0] = byte(extensionRenegotiationInfo >> 8)
		z[1] = byte(extensionRenegotiationInfo & 0xff)
		z[2] = 0
		z[3] = byte(len(m.SecureRenegotiation) + 1)
		z[4] = byte(len(m.SecureRenegotiation))
		z = z[5:]
		copy(z, m.SecureRenegotiation)
		z = z[len(m.SecureRenegotiation):]
	}
	if len(m.AlpnProtocols) > 0 {
		z[0] = byte(extensionALPN >> 8)
		z[1] = byte(extensionALPN & 0xff)
		lengths := z[2:]
		z = z[6:]

		stringsLength := 0
		for _, s := range m.AlpnProtocols {
			l := len(s)
			z[0] = byte(l)
			copy(z[1:], s)
			z = z[1+l:]
			stringsLength += 1 + l
		}

		lengths[2] = byte(stringsLength >> 8)
		lengths[3] = byte(stringsLength)
		stringsLength += 2
		lengths[0] = byte(stringsLength >> 8)
		lengths[1] = byte(stringsLength)
	}
	if m.Scts {
		// https://tools.ietf.org/html/rfc6962#section-3.3.1
		z[0] = byte(extensionSCT >> 8)
		z[1] = byte(extensionSCT)
		// zero uint16 for the zero-length extension_data
		z = z[4:]
	}

	m.Raw = x

	return x
}

func (m *ClientHelloMsg) Unmarshal(data []byte) bool {
	if len(data) < 42 {
		return false
	}
	m.Raw = data
	m.Vers = uint16(data[4])<<8 | uint16(data[5])
	m.Random = data[6:38]
	sessionIdLen := int(data[38])
	if sessionIdLen > 32 || len(data) < 39+sessionIdLen {
		return false
	}
	m.SessionId = data[39 : 39+sessionIdLen]
	data = data[39+sessionIdLen:]
	if len(data) < 2 {
		return false
	}
	// cipherSuiteLen is the number of bytes of cipher suite numbers. Since
	// they are uint16s, the number must be even.
	cipherSuiteLen := int(data[0])<<8 | int(data[1])
	if cipherSuiteLen%2 == 1 || len(data) < 2+cipherSuiteLen {
		return false
	}
	numCipherSuites := cipherSuiteLen / 2
	m.CipherSuites = make([]uint16, numCipherSuites)
	for i := 0; i < numCipherSuites; i++ {
		m.CipherSuites[i] = uint16(data[2+2*i])<<8 | uint16(data[3+2*i])
		if m.CipherSuites[i] == scsvRenegotiation {
			m.SecureRenegotiationSupported = true
		}
	}
	data = data[2+cipherSuiteLen:]
	if len(data) < 1 {
		return false
	}
	compressionMethodsLen := int(data[0])
	if len(data) < 1+compressionMethodsLen {
		return false
	}
	m.CompressionMethods = data[1 : 1+compressionMethodsLen]

	data = data[1+compressionMethodsLen:]

	m.NextProtoNeg = false
	m.ServerName = ""
	m.OcspStapling = false
	m.TicketSupported = false
	m.SessionTicket = nil
	m.SignatureAndHashes = nil
	m.AlpnProtocols = nil
	m.Scts = false

	if len(data) == 0 {
		// ClientHello is optionally followed by extension data
		return true
	}
	if len(data) < 2 {
		return false
	}

	extensionsLength := int(data[0])<<8 | int(data[1])
	data = data[2:]
	if extensionsLength != len(data) {
		return false
	}

	for len(data) != 0 {
		if len(data) < 4 {
			return false
		}
		extension := uint16(data[0])<<8 | uint16(data[1])
		length := int(data[2])<<8 | int(data[3])
		data = data[4:]
		if len(data) < length {
			return false
		}

		switch extension {
		case extensionServerName:
			d := data[:length]
			if len(d) < 2 {
				return false
			}
			namesLen := int(d[0])<<8 | int(d[1])
			d = d[2:]
			if len(d) != namesLen {
				return false
			}
			for len(d) > 0 {
				if len(d) < 3 {
					return false
				}
				nameType := d[0]
				nameLen := int(d[1])<<8 | int(d[2])
				d = d[3:]
				if len(d) < nameLen {
					return false
				}
				if nameType == 0 {
					m.ServerName = string(d[:nameLen])
					// An SNI value may not include a
					// trailing dot. See
					// https://tools.ietf.org/html/rfc6066#section-3.
					if strings.HasSuffix(m.ServerName, ".") {
						return false
					}
					break
				}
				d = d[nameLen:]
			}
		case extensionNextProtoNeg:
			if length > 0 {
				return false
			}
			m.NextProtoNeg = true
		case extensionStatusRequest:
			m.OcspStapling = length > 0 && data[0] == statusTypeOCSP
		case extensionSupportedCurves:
			// http://tools.ietf.org/html/rfc4492#section-5.5.1
			if length < 2 {
				return false
			}
			l := int(data[0])<<8 | int(data[1])
			if l%2 == 1 || length != l+2 {
				return false
			}
			numCurves := l / 2
			m.SupportedCurves = make([]CurveID, numCurves)
			d := data[2:]
			for i := 0; i < numCurves; i++ {
				m.SupportedCurves[i] = CurveID(d[0])<<8 | CurveID(d[1])
				d = d[2:]
			}
		case extensionSupportedPoints:
			// http://tools.ietf.org/html/rfc4492#section-5.5.2
			if length < 1 {
				return false
			}
			l := int(data[0])
			if length != l+1 {
				return false
			}
			m.SupportedPoints = make([]uint8, l)
			copy(m.SupportedPoints, data[1:])
		case extensionSessionTicket:
			// http://tools.ietf.org/html/rfc5077#section-3.2
			m.TicketSupported = true
			m.SessionTicket = data[:length]
		case extensionSignatureAlgorithms:
			// https://tools.ietf.org/html/rfc5246#section-7.4.1.4.1
			if length < 2 || length&1 != 0 {
				return false
			}
			l := int(data[0])<<8 | int(data[1])
			if l != length-2 {
				return false
			}
			n := l / 2
			d := data[2:]
			m.SignatureAndHashes = make([]signatureAndHash, n)
			for i := range m.SignatureAndHashes {
				m.SignatureAndHashes[i].hash = d[0]
				m.SignatureAndHashes[i].signature = d[1]
				d = d[2:]
			}
		case extensionRenegotiationInfo:
			if length == 0 {
				return false
			}
			d := data[:length]
			l := int(d[0])
			d = d[1:]
			if l != len(d) {
				return false
			}

			m.SecureRenegotiation = d
			m.SecureRenegotiationSupported = true
		case extensionALPN:
			if length < 2 {
				return false
			}
			l := int(data[0])<<8 | int(data[1])
			if l != length-2 {
				return false
			}
			d := data[2:length]
			for len(d) != 0 {
				stringLen := int(d[0])
				d = d[1:]
				if stringLen == 0 || stringLen > len(d) {
					return false
				}
				m.AlpnProtocols = append(m.AlpnProtocols, string(d[:stringLen]))
				d = d[stringLen:]
			}
		case extensionSCT:
			m.Scts = true
			if length != 0 {
				return false
			}
		}
		data = data[length:]
	}

	return true
}

type ServerHelloMsg struct {
	Raw                          []byte
	Vers                         uint16
	Random                       []byte
	SessionId                    []byte
	CipherSuite                  uint16
	CompressionMethod            uint8
	NextProtoNeg                 bool
	NextProtos                   []string
	OcspStapling                 bool
	Scts                         [][]byte
	TicketSupported              bool
	SecureRenegotiation          []byte
	SecureRenegotiationSupported bool
	AlpnProtocol                 string
}

func (m *ServerHelloMsg) equal(i interface{}) bool {
	m1, ok := i.(*ServerHelloMsg)
	if !ok {
		return false
	}

	if len(m.Scts) != len(m1.Scts) {
		return false
	}
	for i, sct := range m.Scts {
		if !bytes.Equal(sct, m1.Scts[i]) {
			return false
		}
	}

	return bytes.Equal(m.Raw, m1.Raw) &&
		m.Vers == m1.Vers &&
		bytes.Equal(m.Random, m1.Random) &&
		bytes.Equal(m.SessionId, m1.SessionId) &&
		m.CipherSuite == m1.CipherSuite &&
		m.CompressionMethod == m1.CompressionMethod &&
		m.NextProtoNeg == m1.NextProtoNeg &&
		eqStrings(m.NextProtos, m1.NextProtos) &&
		m.OcspStapling == m1.OcspStapling &&
		m.TicketSupported == m1.TicketSupported &&
		m.SecureRenegotiationSupported == m1.SecureRenegotiationSupported &&
		bytes.Equal(m.SecureRenegotiation, m1.SecureRenegotiation) &&
		m.AlpnProtocol == m1.AlpnProtocol
}

func (m *ServerHelloMsg) marshal() []byte {
	if m.Raw != nil {
		return m.Raw
	}

	length := 38 + len(m.SessionId)
	numExtensions := 0
	extensionsLength := 0

	nextProtoLen := 0
	if m.NextProtoNeg {
		numExtensions++
		for _, v := range m.NextProtos {
			nextProtoLen += len(v)
		}
		nextProtoLen += len(m.NextProtos)
		extensionsLength += nextProtoLen
	}
	if m.OcspStapling {
		numExtensions++
	}
	if m.TicketSupported {
		numExtensions++
	}
	if m.SecureRenegotiationSupported {
		extensionsLength += 1 + len(m.SecureRenegotiation)
		numExtensions++
	}
	if alpnLen := len(m.AlpnProtocol); alpnLen > 0 {
		if alpnLen >= 256 {
			panic("invalid ALPN protocol")
		}
		extensionsLength += 2 + 1 + alpnLen
		numExtensions++
	}
	sctLen := 0
	if len(m.Scts) > 0 {
		for _, sct := range m.Scts {
			sctLen += len(sct) + 2
		}
		extensionsLength += 2 + sctLen
		numExtensions++
	}

	if numExtensions > 0 {
		extensionsLength += 4 * numExtensions
		length += 2 + extensionsLength
	}

	x := make([]byte, 4+length)
	x[0] = TypeServerHello
	x[1] = uint8(length >> 16)
	x[2] = uint8(length >> 8)
	x[3] = uint8(length)
	x[4] = uint8(m.Vers >> 8)
	x[5] = uint8(m.Vers)
	copy(x[6:38], m.Random)
	x[38] = uint8(len(m.SessionId))
	copy(x[39:39+len(m.SessionId)], m.SessionId)
	z := x[39+len(m.SessionId):]
	z[0] = uint8(m.CipherSuite >> 8)
	z[1] = uint8(m.CipherSuite)
	z[2] = m.CompressionMethod

	z = z[3:]
	if numExtensions > 0 {
		z[0] = byte(extensionsLength >> 8)
		z[1] = byte(extensionsLength)
		z = z[2:]
	}
	if m.NextProtoNeg {
		z[0] = byte(extensionNextProtoNeg >> 8)
		z[1] = byte(extensionNextProtoNeg & 0xff)
		z[2] = byte(nextProtoLen >> 8)
		z[3] = byte(nextProtoLen)
		z = z[4:]

		for _, v := range m.NextProtos {
			l := len(v)
			if l > 255 {
				l = 255
			}
			z[0] = byte(l)
			copy(z[1:], []byte(v[0:l]))
			z = z[1+l:]
		}
	}
	if m.OcspStapling {
		z[0] = byte(extensionStatusRequest >> 8)
		z[1] = byte(extensionStatusRequest)
		z = z[4:]
	}
	if m.TicketSupported {
		z[0] = byte(extensionSessionTicket >> 8)
		z[1] = byte(extensionSessionTicket)
		z = z[4:]
	}
	if m.SecureRenegotiationSupported {
		z[0] = byte(extensionRenegotiationInfo >> 8)
		z[1] = byte(extensionRenegotiationInfo & 0xff)
		z[2] = 0
		z[3] = byte(len(m.SecureRenegotiation) + 1)
		z[4] = byte(len(m.SecureRenegotiation))
		z = z[5:]
		copy(z, m.SecureRenegotiation)
		z = z[len(m.SecureRenegotiation):]
	}
	if alpnLen := len(m.AlpnProtocol); alpnLen > 0 {
		z[0] = byte(extensionALPN >> 8)
		z[1] = byte(extensionALPN & 0xff)
		l := 2 + 1 + alpnLen
		z[2] = byte(l >> 8)
		z[3] = byte(l)
		l -= 2
		z[4] = byte(l >> 8)
		z[5] = byte(l)
		l -= 1
		z[6] = byte(l)
		copy(z[7:], []byte(m.AlpnProtocol))
		z = z[7+alpnLen:]
	}
	if sctLen > 0 {
		z[0] = byte(extensionSCT >> 8)
		z[1] = byte(extensionSCT)
		l := sctLen + 2
		z[2] = byte(l >> 8)
		z[3] = byte(l)
		z[4] = byte(sctLen >> 8)
		z[5] = byte(sctLen)

		z = z[6:]
		for _, sct := range m.Scts {
			z[0] = byte(len(sct) >> 8)
			z[1] = byte(len(sct))
			copy(z[2:], sct)
			z = z[len(sct)+2:]
		}
	}

	m.Raw = x

	return x
}

func (m *ServerHelloMsg) Unmarshal(data []byte) bool {
	if len(data) < 42 {
		return false
	}
	m.Raw = data
	m.Vers = uint16(data[4])<<8 | uint16(data[5])
	m.Random = data[6:38]
	sessionIdLen := int(data[38])
	if sessionIdLen > 32 || len(data) < 39+sessionIdLen {
		return false
	}
	m.SessionId = data[39 : 39+sessionIdLen]
	data = data[39+sessionIdLen:]
	if len(data) < 3 {
		return false
	}
	m.CipherSuite = uint16(data[0])<<8 | uint16(data[1])
	m.CompressionMethod = data[2]
	data = data[3:]

	m.NextProtoNeg = false
	m.NextProtos = nil
	m.OcspStapling = false
	m.Scts = nil
	m.TicketSupported = false
	m.AlpnProtocol = ""

	if len(data) == 0 {
		// ServerHello is optionally followed by extension data
		return true
	}
	if len(data) < 2 {
		return false
	}

	extensionsLength := int(data[0])<<8 | int(data[1])
	data = data[2:]
	if len(data) != extensionsLength {
		return false
	}

	for len(data) != 0 {
		if len(data) < 4 {
			return false
		}
		extension := uint16(data[0])<<8 | uint16(data[1])
		length := int(data[2])<<8 | int(data[3])
		data = data[4:]
		if len(data) < length {
			return false
		}

		switch extension {
		case extensionNextProtoNeg:
			m.NextProtoNeg = true
			d := data[:length]
			for len(d) > 0 {
				l := int(d[0])
				d = d[1:]
				if l == 0 || l > len(d) {
					return false
				}
				m.NextProtos = append(m.NextProtos, string(d[:l]))
				d = d[l:]
			}
		case extensionStatusRequest:
			if length > 0 {
				return false
			}
			m.OcspStapling = true
		case extensionSessionTicket:
			if length > 0 {
				return false
			}
			m.TicketSupported = true
		case extensionRenegotiationInfo:
			if length == 0 {
				return false
			}
			d := data[:length]
			l := int(d[0])
			d = d[1:]
			if l != len(d) {
				return false
			}

			m.SecureRenegotiation = d
			m.SecureRenegotiationSupported = true
		case extensionALPN:
			d := data[:length]
			if len(d) < 3 {
				return false
			}
			l := int(d[0])<<8 | int(d[1])
			if l != len(d)-2 {
				return false
			}
			d = d[2:]
			l = int(d[0])
			if l != len(d)-1 {
				return false
			}
			d = d[1:]
			if len(d) == 0 {
				// ALPN protocols must not be empty.
				return false
			}
			m.AlpnProtocol = string(d)
		case extensionSCT:
			d := data[:length]

			if len(d) < 2 {
				return false
			}
			l := int(d[0])<<8 | int(d[1])
			d = d[2:]
			if len(d) != l || l == 0 {
				return false
			}

			m.Scts = make([][]byte, 0, 3)
			for len(d) != 0 {
				if len(d) < 2 {
					return false
				}
				sctLen := int(d[0])<<8 | int(d[1])
				d = d[2:]
				if sctLen == 0 || len(d) < sctLen {
					return false
				}
				m.Scts = append(m.Scts, d[:sctLen])
				d = d[sctLen:]
			}
		}
		data = data[length:]
	}

	return true
}

type CertificateMsg struct {
	Raw          []byte
	Certificates [][]byte
}

func (m *CertificateMsg) equal(i interface{}) bool {
	m1, ok := i.(*CertificateMsg)
	if !ok {
		return false
	}

	return bytes.Equal(m.Raw, m1.Raw) &&
		eqByteSlices(m.Certificates, m1.Certificates)
}

func (m *CertificateMsg) marshal() (x []byte) {
	if m.Raw != nil {
		return m.Raw
	}

	var i int
	for _, slice := range m.Certificates {
		i += len(slice)
	}

	length := 3 + 3*len(m.Certificates) + i
	x = make([]byte, 4+length)
	x[0] = TypeCertificate
	x[1] = uint8(length >> 16)
	x[2] = uint8(length >> 8)
	x[3] = uint8(length)

	certificateOctets := length - 3
	x[4] = uint8(certificateOctets >> 16)
	x[5] = uint8(certificateOctets >> 8)
	x[6] = uint8(certificateOctets)

	y := x[7:]
	for _, slice := range m.Certificates {
		y[0] = uint8(len(slice) >> 16)
		y[1] = uint8(len(slice) >> 8)
		y[2] = uint8(len(slice))
		copy(y[3:], slice)
		y = y[3+len(slice):]
	}

	m.Raw = x
	return
}

func (m *CertificateMsg) Unmarshal(data []byte) bool {
	if len(data) < 7 {
		return false
	}

	m.Raw = data
	certsLen := uint32(data[4])<<16 | uint32(data[5])<<8 | uint32(data[6])
	if uint32(len(data)) != certsLen+7 {
		return false
	}

	numCerts := 0
	d := data[7:]
	for certsLen > 0 {
		if len(d) < 4 {
			return false
		}
		certLen := uint32(d[0])<<16 | uint32(d[1])<<8 | uint32(d[2])
		if uint32(len(d)) < 3+certLen {
			return false
		}
		d = d[3+certLen:]
		certsLen -= 3 + certLen
		numCerts++
	}

	m.Certificates = make([][]byte, numCerts)
	d = data[7:]
	for i := 0; i < numCerts; i++ {
		certLen := uint32(d[0])<<16 | uint32(d[1])<<8 | uint32(d[2])
		m.Certificates[i] = d[3 : 3+certLen]
		d = d[3+certLen:]
	}

	return true
}

type ServerKeyExchangeMsg struct {
	Raw []byte
	Key []byte
}

func (m *ServerKeyExchangeMsg) equal(i interface{}) bool {
	m1, ok := i.(*ServerKeyExchangeMsg)
	if !ok {
		return false
	}

	return bytes.Equal(m.Raw, m1.Raw) &&
		bytes.Equal(m.Key, m1.Key)
}

func (m *ServerKeyExchangeMsg) marshal() []byte {
	if m.Raw != nil {
		return m.Raw
	}
	length := len(m.Key)
	x := make([]byte, length+4)
	x[0] = TypeServerKeyExchange
	x[1] = uint8(length >> 16)
	x[2] = uint8(length >> 8)
	x[3] = uint8(length)
	copy(x[4:], m.Key)

	m.Raw = x
	return x
}

func (m *ServerKeyExchangeMsg) Unmarshal(data []byte) bool {
	m.Raw = data
	if len(data) < 4 {
		return false
	}
	m.Key = data[4:]
	return true
}

type CertificateStatusMsg struct {
	Raw        []byte
	StatusType uint8
	Response   []byte
}

func (m *CertificateStatusMsg) equal(i interface{}) bool {
	m1, ok := i.(*CertificateStatusMsg)
	if !ok {
		return false
	}

	return bytes.Equal(m.Raw, m1.Raw) &&
		m.StatusType == m1.StatusType &&
		bytes.Equal(m.Response, m1.Response)
}

func (m *CertificateStatusMsg) marshal() []byte {
	if m.Raw != nil {
		return m.Raw
	}

	var x []byte
	if m.StatusType == statusTypeOCSP {
		x = make([]byte, 4+4+len(m.Response))
		x[0] = TypeCertificateStatus
		l := len(m.Response) + 4
		x[1] = byte(l >> 16)
		x[2] = byte(l >> 8)
		x[3] = byte(l)
		x[4] = statusTypeOCSP

		l -= 4
		x[5] = byte(l >> 16)
		x[6] = byte(l >> 8)
		x[7] = byte(l)
		copy(x[8:], m.Response)
	} else {
		x = []byte{TypeCertificateStatus, 0, 0, 1, m.StatusType}
	}

	m.Raw = x
	return x
}

func (m *CertificateStatusMsg) Unmarshal(data []byte) bool {
	m.Raw = data
	if len(data) < 5 {
		return false
	}
	m.StatusType = data[4]

	m.Response = nil
	if m.StatusType == statusTypeOCSP {
		if len(data) < 8 {
			return false
		}
		respLen := uint32(data[5])<<16 | uint32(data[6])<<8 | uint32(data[7])
		if uint32(len(data)) != 4+4+respLen {
			return false
		}
		m.Response = data[8:]
	}
	return true
}

type ServerHelloDoneMsg struct{}

func (m *ServerHelloDoneMsg) equal(i interface{}) bool {
	_, ok := i.(*ServerHelloDoneMsg)
	return ok
}

func (m *ServerHelloDoneMsg) marshal() []byte {
	x := make([]byte, 4)
	x[0] = TypeServerHelloDone
	return x
}

func (m *ServerHelloDoneMsg) Unmarshal(data []byte) bool {
	return len(data) == 4
}

type ClientKeyExchangeMsg struct {
	Raw        []byte
	Ciphertext []byte
}

func (m *ClientKeyExchangeMsg) equal(i interface{}) bool {
	m1, ok := i.(*ClientKeyExchangeMsg)
	if !ok {
		return false
	}

	return bytes.Equal(m.Raw, m1.Raw) &&
		bytes.Equal(m.Ciphertext, m1.Ciphertext)
}

func (m *ClientKeyExchangeMsg) marshal() []byte {
	if m.Raw != nil {
		return m.Raw
	}
	length := len(m.Ciphertext)
	x := make([]byte, length+4)
	x[0] = TypeClientKeyExchange
	x[1] = uint8(length >> 16)
	x[2] = uint8(length >> 8)
	x[3] = uint8(length)
	copy(x[4:], m.Ciphertext)

	m.Raw = x
	return x
}

func (m *ClientKeyExchangeMsg) Unmarshal(data []byte) bool {
	m.Raw = data
	if len(data) < 4 {
		return false
	}
	l := int(data[1])<<16 | int(data[2])<<8 | int(data[3])
	if l != len(data)-4 {
		return false
	}
	m.Ciphertext = data[4:]
	return true
}

type FinishedMsg struct {
	Raw        []byte
	VerifyData []byte
}

func (m *FinishedMsg) equal(i interface{}) bool {
	m1, ok := i.(*FinishedMsg)
	if !ok {
		return false
	}

	return bytes.Equal(m.Raw, m1.Raw) &&
		bytes.Equal(m.VerifyData, m1.VerifyData)
}

func (m *FinishedMsg) marshal() (x []byte) {
	if m.Raw != nil {
		return m.Raw
	}

	x = make([]byte, 4+len(m.VerifyData))
	x[0] = TypeFinished
	x[3] = byte(len(m.VerifyData))
	copy(x[4:], m.VerifyData)
	m.Raw = x
	return
}

func (m *FinishedMsg) Unmarshal(data []byte) bool {
	m.Raw = data
	if len(data) < 4 {
		return false
	}
	m.VerifyData = data[4:]
	return true
}

type NextProtoMsg struct {
	Raw   []byte
	Proto string
}

func (m *NextProtoMsg) equal(i interface{}) bool {
	m1, ok := i.(*NextProtoMsg)
	if !ok {
		return false
	}

	return bytes.Equal(m.Raw, m1.Raw) &&
		m.Proto == m1.Proto
}

func (m *NextProtoMsg) marshal() []byte {
	if m.Raw != nil {
		return m.Raw
	}
	l := len(m.Proto)
	if l > 255 {
		l = 255
	}

	padding := 32 - (l+2)%32
	length := l + padding + 2
	x := make([]byte, length+4)
	x[0] = TypeNextProtocol
	x[1] = uint8(length >> 16)
	x[2] = uint8(length >> 8)
	x[3] = uint8(length)

	y := x[4:]
	y[0] = byte(l)
	copy(y[1:], []byte(m.Proto[0:l]))
	y = y[1+l:]
	y[0] = byte(padding)

	m.Raw = x

	return x
}

func (m *NextProtoMsg) Unmarshal(data []byte) bool {
	m.Raw = data

	if len(data) < 5 {
		return false
	}
	data = data[4:]
	protoLen := int(data[0])
	data = data[1:]
	if len(data) < protoLen {
		return false
	}
	m.Proto = string(data[0:protoLen])
	data = data[protoLen:]

	if len(data) < 1 {
		return false
	}
	paddingLen := int(data[0])
	data = data[1:]
	if len(data) != paddingLen {
		return false
	}

	return true
}

type CertificateRequestMsg struct {
	Raw []byte
	// hasSignatureAndHash indicates whether this message includes a list
	// of signature and hash functions. This change was introduced with TLS
	// 1.2.
	HasSignatureAndHash bool

	CertificateTypes       []byte
	SignatureAndHashes     []signatureAndHash
	CertificateAuthorities [][]byte
}

func (m *CertificateRequestMsg) equal(i interface{}) bool {
	m1, ok := i.(*CertificateRequestMsg)
	if !ok {
		return false
	}

	return bytes.Equal(m.Raw, m1.Raw) &&
		bytes.Equal(m.CertificateTypes, m1.CertificateTypes) &&
		eqByteSlices(m.CertificateAuthorities, m1.CertificateAuthorities) &&
		eqSignatureAndHashes(m.SignatureAndHashes, m1.SignatureAndHashes)
}

func (m *CertificateRequestMsg) marshal() (x []byte) {
	if m.Raw != nil {
		return m.Raw
	}

	// See http://tools.ietf.org/html/rfc4346#section-7.4.4
	length := 1 + len(m.CertificateTypes) + 2
	casLength := 0
	for _, ca := range m.CertificateAuthorities {
		casLength += 2 + len(ca)
	}
	length += casLength

	if m.HasSignatureAndHash {
		length += 2 + 2*len(m.SignatureAndHashes)
	}

	x = make([]byte, 4+length)
	x[0] = TypeCertificateRequest
	x[1] = uint8(length >> 16)
	x[2] = uint8(length >> 8)
	x[3] = uint8(length)

	x[4] = uint8(len(m.CertificateTypes))

	copy(x[5:], m.CertificateTypes)
	y := x[5+len(m.CertificateTypes):]

	if m.HasSignatureAndHash {
		n := len(m.SignatureAndHashes) * 2
		y[0] = uint8(n >> 8)
		y[1] = uint8(n)
		y = y[2:]
		for _, sigAndHash := range m.SignatureAndHashes {
			y[0] = sigAndHash.hash
			y[1] = sigAndHash.signature
			y = y[2:]
		}
	}

	y[0] = uint8(casLength >> 8)
	y[1] = uint8(casLength)
	y = y[2:]
	for _, ca := range m.CertificateAuthorities {
		y[0] = uint8(len(ca) >> 8)
		y[1] = uint8(len(ca))
		y = y[2:]
		copy(y, ca)
		y = y[len(ca):]
	}

	m.Raw = x
	return
}

func (m *CertificateRequestMsg) Unmarshal(data []byte) bool {
	m.Raw = data

	if len(data) < 5 {
		return false
	}

	length := uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3])
	if uint32(len(data))-4 != length {
		return false
	}

	numCertTypes := int(data[4])
	data = data[5:]
	if numCertTypes == 0 || len(data) <= numCertTypes {
		return false
	}

	m.CertificateTypes = make([]byte, numCertTypes)
	if copy(m.CertificateTypes, data) != numCertTypes {
		return false
	}

	data = data[numCertTypes:]

	if m.HasSignatureAndHash {
		if len(data) < 2 {
			return false
		}
		sigAndHashLen := uint16(data[0])<<8 | uint16(data[1])
		data = data[2:]
		if sigAndHashLen&1 != 0 {
			return false
		}
		if len(data) < int(sigAndHashLen) {
			return false
		}
		numSigAndHash := sigAndHashLen / 2
		m.SignatureAndHashes = make([]signatureAndHash, numSigAndHash)
		for i := range m.SignatureAndHashes {
			m.SignatureAndHashes[i].hash = data[0]
			m.SignatureAndHashes[i].signature = data[1]
			data = data[2:]
		}
	}

	if len(data) < 2 {
		return false
	}
	casLength := uint16(data[0])<<8 | uint16(data[1])
	data = data[2:]
	if len(data) < int(casLength) {
		return false
	}
	cas := make([]byte, casLength)
	copy(cas, data)
	data = data[casLength:]

	m.CertificateAuthorities = nil
	for len(cas) > 0 {
		if len(cas) < 2 {
			return false
		}
		caLen := uint16(cas[0])<<8 | uint16(cas[1])
		cas = cas[2:]

		if len(cas) < int(caLen) {
			return false
		}

		m.CertificateAuthorities = append(m.CertificateAuthorities, cas[:caLen])
		cas = cas[caLen:]
	}

	return len(data) == 0
}

type CertificateVerifyMsg struct {
	Raw                 []byte
	HasSignatureAndHash bool
	SignatureAndHash    signatureAndHash
	Signature           []byte
}

func (m *CertificateVerifyMsg) equal(i interface{}) bool {
	m1, ok := i.(*CertificateVerifyMsg)
	if !ok {
		return false
	}

	return bytes.Equal(m.Raw, m1.Raw) &&
		m.HasSignatureAndHash == m1.HasSignatureAndHash &&
		m.SignatureAndHash.hash == m1.SignatureAndHash.hash &&
		m.SignatureAndHash.signature == m1.SignatureAndHash.signature &&
		bytes.Equal(m.Signature, m1.Signature)
}

func (m *CertificateVerifyMsg) marshal() (x []byte) {
	if m.Raw != nil {
		return m.Raw
	}

	// See http://tools.ietf.org/html/rfc4346#section-7.4.8
	siglength := len(m.Signature)
	length := 2 + siglength
	if m.HasSignatureAndHash {
		length += 2
	}
	x = make([]byte, 4+length)
	x[0] = TypeCertificateVerify
	x[1] = uint8(length >> 16)
	x[2] = uint8(length >> 8)
	x[3] = uint8(length)
	y := x[4:]
	if m.HasSignatureAndHash {
		y[0] = m.SignatureAndHash.hash
		y[1] = m.SignatureAndHash.signature
		y = y[2:]
	}
	y[0] = uint8(siglength >> 8)
	y[1] = uint8(siglength)
	copy(y[2:], m.Signature)

	m.Raw = x

	return
}

func (m *CertificateVerifyMsg) Unmarshal(data []byte) bool {
	m.Raw = data

	if len(data) < 6 {
		return false
	}

	length := uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3])
	if uint32(len(data))-4 != length {
		return false
	}

	data = data[4:]
	if m.HasSignatureAndHash {
		m.SignatureAndHash.hash = data[0]
		m.SignatureAndHash.signature = data[1]
		data = data[2:]
	}

	if len(data) < 2 {
		return false
	}
	siglength := int(data[0])<<8 + int(data[1])
	data = data[2:]
	if len(data) != siglength {
		return false
	}

	m.Signature = data

	return true
}

type NewSessionTicketMsg struct {
	Raw    []byte
	Ticket []byte
}

func (m *NewSessionTicketMsg) equal(i interface{}) bool {
	m1, ok := i.(*NewSessionTicketMsg)
	if !ok {
		return false
	}

	return bytes.Equal(m.Raw, m1.Raw) &&
		bytes.Equal(m.Ticket, m1.Ticket)
}

func (m *NewSessionTicketMsg) marshal() (x []byte) {
	if m.Raw != nil {
		return m.Raw
	}

	// See http://tools.ietf.org/html/rfc5077#section-3.3
	ticketLen := len(m.Ticket)
	length := 2 + 4 + ticketLen
	x = make([]byte, 4+length)
	x[0] = TypeNewSessionTicket
	x[1] = uint8(length >> 16)
	x[2] = uint8(length >> 8)
	x[3] = uint8(length)
	x[8] = uint8(ticketLen >> 8)
	x[9] = uint8(ticketLen)
	copy(x[10:], m.Ticket)

	m.Raw = x

	return
}

func (m *NewSessionTicketMsg) Unmarshal(data []byte) bool {
	m.Raw = data

	if len(data) < 10 {
		return false
	}

	length := uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3])
	if uint32(len(data))-4 != length {
		return false
	}

	ticketLen := int(data[8])<<8 + int(data[9])
	if len(data)-10 != ticketLen {
		return false
	}

	m.Ticket = data[10:]

	return true
}

type HelloRequestMsg struct {
}

func (*HelloRequestMsg) marshal() []byte {
	return []byte{TypeHelloRequest, 0, 0, 0}
}

func (*HelloRequestMsg) Unmarshal(data []byte) bool {
	return len(data) == 4
}

func eqUint16s(x, y []uint16) bool {
	if len(x) != len(y) {
		return false
	}
	for i, v := range x {
		if y[i] != v {
			return false
		}
	}
	return true
}

func eqCurveIDs(x, y []CurveID) bool {
	if len(x) != len(y) {
		return false
	}
	for i, v := range x {
		if y[i] != v {
			return false
		}
	}
	return true
}

func eqStrings(x, y []string) bool {
	if len(x) != len(y) {
		return false
	}
	for i, v := range x {
		if y[i] != v {
			return false
		}
	}
	return true
}

func eqByteSlices(x, y [][]byte) bool {
	if len(x) != len(y) {
		return false
	}
	for i, v := range x {
		if !bytes.Equal(v, y[i]) {
			return false
		}
	}
	return true
}

func eqSignatureAndHashes(x, y []signatureAndHash) bool {
	if len(x) != len(y) {
		return false
	}
	for i, v := range x {
		v2 := y[i]
		if v.hash != v2.hash || v.signature != v2.signature {
			return false
		}
	}
	return true
}
