// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package tls

import (
	"crypto/x509"
	"fmt"
	"runtime"
	"strings"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/tcpassembly"

	"github.com/Safing/safing-core/configuration"
	"github.com/Safing/safing-core/crypto/verify"
	"github.com/Safing/safing-core/firewall/inspection"
	"github.com/Safing/safing-core/firewall/inspection/tls/tlslib"
	"github.com/Safing/safing-core/log"
	"github.com/Safing/safing-core/network"
	"github.com/Safing/safing-core/network/netutils"
	"github.com/Safing/safing-core/network/packet"
)

// TODO:
// - delete insecure cipher suites from clienthello as of level (-> configuration)
// - reject connection if serverhello initiates insecure cipher suite (-> configuration)
// - reject TLS without SNI as of level (-> configuration)

var (
	tlsInspectorIndex int
	assemblerManager  *netutils.SimpleStreamAssemblerManager
	assembler         *tcpassembly.Assembler

	config = configuration.Get()
)

const (
	statusWaitForMore uint8 = iota
	statusNotTLS
	statusProtocolViolation
	statusInvalidCertificate
	statusVerified
)

func init() {
	tlsInspectorIndex = inspection.RegisterInspector("TLS", inspector, network.ACCEPT)

	assemblerManager = new(netutils.SimpleStreamAssemblerManager)
	streamPool := tcpassembly.NewStreamPool(assemblerManager)
	assembler = tcpassembly.NewAssembler(streamPool)
}

type TLSInspection struct {
	AssemblerI             *netutils.SimpleStreamAssembler
	AssemblerO             *netutils.SimpleStreamAssembler
	PacketsSeen            uint
	ServerName             string
	Resuming               bool
	WaitingForVerification bool
	verification           chan bool
	SecurityLevel          int8
	EnforceCT              bool
	EnforceRevocation      bool
	DenyInsecureTLS        bool
	DenyTLSWithoutSNI      bool
}

func inspector(pkt packet.Packet, link *network.Link) uint8 {

	// obviously, only inspect TCP
	if pkt.GetIPHeader().Protocol != packet.TCP {
		return inspection.STOP_INSPECTING
	}

	// only check outgoing
	if link.Connection().Direction == network.Inbound {
		return inspection.STOP_INSPECTING
	}

	// get or create link-specific inspection data
	var tlsInspection *TLSInspection
	inspectorData, ok := link.InspectorData[uint8(tlsInspectorIndex)]
	if ok {
		tlsInspection, ok = inspectorData.(*TLSInspection)
	}
	if !ok {
		tlsInspection = new(TLSInspection)
		link.InspectorData[uint8(tlsInspectorIndex)] = tlsInspection

		// load config for link
		tlsInspection.SecurityLevel = link.Connection().Process().Profile.SecurityLevel
		config.Changed()
		config.RLock()
		tlsInspection.EnforceCT = config.EnforceCT.IsSetWithLevel(tlsInspection.SecurityLevel)
		tlsInspection.EnforceRevocation = config.EnforceRevocation.IsSetWithLevel(tlsInspection.SecurityLevel)
		tlsInspection.DenyInsecureTLS = config.DenyInsecureTLS.IsSetWithLevel(tlsInspection.SecurityLevel)
		tlsInspection.DenyTLSWithoutSNI = config.DenyTLSWithoutSNI.IsSetWithLevel(tlsInspection.SecurityLevel)
		config.RUnlock()

	}

	if tlsInspection.WaitingForVerification {
		select {
		case verified := <-tlsInspection.verification:
			if verified {
				return inspection.STOP_INSPECTING
			}
			return inspection.BLOCK_LINK
		default:
			return inspection.DO_NOTHING
		}
	}

	var err error
	var parser *gopacket.DecodingLayerParser
	var decoded []gopacket.LayerType

	// TODO: pool allocated space for reuse -> performance!
	var ip4 layers.IPv4
	var ip6 layers.IPv6
	var tcp layers.TCP

	var payload gopacket.Payload

	switch pkt.IPVersion() {
	case packet.IPv4:
		parser = gopacket.NewDecodingLayerParser(layers.LayerTypeIPv4, &ip4, &tcp, &payload)
		err = parser.DecodeLayers(*pkt.GetPayload(), &decoded)
	case packet.IPv6:
		parser = gopacket.NewDecodingLayerParser(layers.LayerTypeIPv6, &ip6, &tcp, &payload)
		err = parser.DecodeLayers(*pkt.GetPayload(), &decoded)
	default:
		log.Warningf("TLS inspector: %s: not IPv4 or IPv6", pkt.IPVersion().String())
		return inspection.DO_NOTHING
	}

	if err != nil {
		log.Warningf("TLS inspector: %s: failed to parse packet: %s", pkt, err)
		return inspection.DO_NOTHING
	}

	// Stop after 10 packets
	tlsInspection.PacketsSeen += 1
	if tlsInspection.PacketsSeen > 20 {
		if tlsInspection.Resuming {
			log.Debugf("TLS inspector: resumed TLS session: %s", link.String())
		} else {
			log.Debugf("TLS inspector: not TLS: %s", link.String())
		}
		return inspection.STOP_INSPECTING
	}

	// TCP Stream building

	var streamAssembler *netutils.SimpleStreamAssembler
	inbound := pkt.IsInbound()

	// load assembler
	if inbound {
		streamAssembler = tlsInspection.AssemblerI
	} else {
		streamAssembler = tlsInspection.AssemblerO
	}

	// BUG:
	// 	panic: runtime error: index out of range
	//
	// goroutine 1120 [running]:
	// safing/vendor/github.com/google/gopacket/tcpassembly.(*Assembler).sendToConnection(0xc4200d2e40, 0xc42172f140)
	// 	/home/dr/.go/src/safing/vendor/github.com/google/gopacket/tcpassembly/assembly.go:631 +0xc2
	// safing/vendor/github.com/google/gopacket/tcpassembly.(*Assembler).AssembleWithTimestamp(0xc4200d2e40, 0x1, 0x4, 0x4, 0x97fa8c0, 0x0, 0x559063c1, 0x0, 0xc42126ec80, 0xed0c9eb39, ...)
	// 	/home/dr/.go/src/safing/vendor/github.com/google/gopacket/tcpassembly/assembly.go:605 +0x3d8
	// safing/vendor/github.com/google/gopacket/tcpassembly.(*Assembler).Assemble(0xc4200d2e40, 0x1, 0x4, 0x4, 0x97fa8c0, 0x0, 0x559063c1, 0x0, 0xc42126ec80)
	// 	/home/dr/.go/src/safing/vendor/github.com/google/gopacket/tcpassembly/assembly.go:518 +0x82
	// safing/firewall/inspection/tls.inspector(0xec5f00, 0xc421b88780, 0xc422c20f30, 0xc423d75380)
	// 	/home/dr/.go/src/safing/firewall/inspection/tls/tls.go:122 +0x414
	// safing/firewall/inspection.RunInspectors(0xec5f00, 0xc421b88780, 0xc422c20f30, 0xc421545fb8)
	// 	/home/dr/.go/src/safing/firewall/inspection/inspection.go:62 +0xfe
	// safing/firewall.inspectThenVerdict(0xec5f00, 0xc421b88780, 0xc422c20f30)
	// 	/home/dr/.go/src/safing/firewall/firewall.go:144 +0x43
	// safing/network.(*Link).packetHandler(0xc422c20f30)
	// 	/home/dr/.go/src/safing/network/link.go:109 +0x89
	// created by safing/network.(*Link).SetFirewallHandler
	// 	/home/dr/.go/src/safing/network/link.go:61 +0xdd

	// assemble and save assembler if first time
	if streamAssembler != nil {
		if pkt.IPVersion() == packet.IPv4 {
			assembler.Assemble(ip4.NetworkFlow(), &tcp)
			// FIXME: panic: runtime error: index out of range
			//
			// goroutine 772 [running]:
			// safing/vendor/github.com/google/gopacket/tcpassembly.(*Assembler).sendToConnection(0xc4200eed80, 0xc4222b1fa0)
			// 	/home/dr/.go/src/safing/vendor/github.com/google/gopacket/tcpassembly/assembly.go:631 +0xc2
			// safing/vendor/github.com/google/gopacket/tcpassembly.(*Assembler).AssembleWithTimestamp(0xc4200eed80, 0x1, 0x4, 0x4, 0x97fa8c0, 0x0, 0x8e17d9ac, 0x0, 0xc42175a8c0, 0xed0e03a0a, ...)
			// 	/home/dr/.go/src/safing/vendor/github.com/google/gopacket/tcpassembly/assembly.go:605 +0x3d8
			// safing/vendor/github.com/google/gopacket/tcpassembly.(*Assembler).Assemble(0xc4200eed80, 0x1, 0x4, 0x4, 0x97fa8c0, 0x0, 0x8e17d9ac, 0x0, 0xc42175a8c0)
			// 	/home/dr/.go/src/safing/vendor/github.com/google/gopacket/tcpassembly/assembly.go:518 +0x82
			// safing/firewall/inspection/tls.inspector(0xed40c0, 0xc421cce780, 0xc4229e42d0, 0xc42155f6c0)
			// 	/home/dr/.go/src/safing/firewall/inspection/tls/tls.go:143 +0x414
			// safing/firewall/inspection.RunInspectors(0xed40c0, 0xc421cce780, 0xc4229e42d0, 0xc421d19fb8)
			// 	/home/dr/.go/src/safing/firewall/inspection/inspection.go:62 +0xfe
			// safing/firewall.inspectThenVerdict(0xed40c0, 0xc421cce780, 0xc4229e42d0)
			// 	/home/dr/.go/src/safing/firewall/firewall.go:149 +0x43
			// safing/network.(*Link).packetHandler(0xc4229e42d0)
			// 	/home/dr/.go/src/safing/network/link.go:110 +0x8c
			// created by safing/network.(*Link).SetFirewallHandler
			// 	/home/dr/.go/src/safing/network/link.go:61 +0xe7
		} else {
			assembler.Assemble(ip6.NetworkFlow(), &tcp)
		}
	} else {
		assemblerManager.InitLock.Lock()
		if pkt.IPVersion() == packet.IPv4 {
			assembler.Assemble(ip4.NetworkFlow(), &tcp)
		} else {
			assembler.Assemble(ip6.NetworkFlow(), &tcp)
		}
		if inbound {
			streamAssembler = assemblerManager.GetLastAssembler()
			tlsInspection.AssemblerI = streamAssembler
		} else {
			streamAssembler = assemblerManager.GetLastAssembler()
			tlsInspection.AssemblerO = streamAssembler
		}
		assemblerManager.InitLock.Unlock()
	}

	if streamAssembler == nil {
		return inspection.DO_NOTHING
	}

	for {

		// check if we have a possible tls message header
		if len(streamAssembler.Cumulated) < 5 {
			return inspection.DO_NOTHING
		}

		// get tls message length
		tlsMessageLen := int(streamAssembler.Cumulated[3])*256 + int(streamAssembler.Cumulated[4]) + 5

		// check if we have full tls message
		if len(streamAssembler.Cumulated) < tlsMessageLen {
			return inspection.DO_NOTHING
		}

		action := processMessage(tlsInspection, streamAssembler.Cumulated[:tlsMessageLen], pkt, link)
		// BUG: slice bounds out of range
		if tlsMessageLen == len(streamAssembler.Cumulated) {
			streamAssembler.Cumulated = make([]byte, 0, 0)
		} else if tlsMessageLen < len(streamAssembler.Cumulated) {
			streamAssembler.Cumulated = streamAssembler.Cumulated[tlsMessageLen:]
		} else {
			log.Warningf("TLS inspector: processed more than available, resetting buffer")
			streamAssembler.Cumulated = make([]byte, 0, 0)
		}

		if action != inspection.DO_NOTHING {
			return action
		}

	}

}

func processMessage(tlsInspection *TLSInspection, data []byte, pkt packet.Packet, link *network.Link) (action uint8) {

	// we are only interested in handshake messages
	if data[0] != uint8(tlslib.RecordTypeHandshake) {
		// log.Tracef("TLS inspector: %s: got %s", pkt, tlsRecordType)
		return inspection.DO_NOTHING
	}

	// TODO: handle tls session resumption: session tickets / session id
	_, ok := tlsHandshakeTypeNames[data[5]]
	if !ok {
		// tlsHandshakeType = "UNKNOWN"
		log.Tracef("TLS inspector: %s: UNKNOWN handshake %d:", pkt, data[5])
	}
	// log.Tracef("TLS inspector: %s: got %s", pkt, tlsHandshakeType)

	if pkt.IsOutbound() {
		switch data[5] {

		// ClientHello
		case tlslib.TypeClientHello:
			var msg tlslib.ClientHelloMsg
			if msg.Unmarshal(data[5:]) {
				// log.Tracef("TLS inspector: %s: ClientHello: %s", pkt, msg.ServerName)

				if tlsInspection.DenyTLSWithoutSNI && msg.ServerName == "" {
					log.Infof("TLS inspector: %s does not use SNI, blocking", link)
					link.AddReason(fmt.Sprintf("TLS does not use SNI"))
					return inspection.BLOCK_LINK
				}

				if tlsInspection.DenyInsecureTLS && msg.Vers < 0x0301 {
					log.Infof("TLS inspector: %s uses version prior TLS1.0, blocking", link)
					link.AddReason(fmt.Sprintf("TLS uses version prior to TLS1.0"))
					return inspection.BLOCK_LINK
				}

				tlsInspection.ServerName = msg.ServerName
				if len(msg.SessionId) > 0 || len(msg.SessionTicket) > 0 {
					tlsInspection.Resuming = true
				}

				return
			}
			log.Warningf("TLS inspector: %s: failed to parse ClientHello", pkt)

		}
	} else {
		switch data[5] {

		// ServerHello
		case tlslib.TypeServerHello:
			var msg tlslib.ServerHelloMsg
			if msg.Unmarshal(data[5:]) {

				if tlsInspection.DenyInsecureTLS {
					cs, ok := cipherSuiteNames[msg.CipherSuite]
					if !ok {
						log.Infof("TLS inspector: %s uses unknown cipher suite, blocking", link)
						link.AddReason(fmt.Sprintf("TLS uses unknown cipher suite"))
						return inspection.BLOCK_LINK
					}

					// We think matching this way is more secure, as it leaves less possibility for bugs, makes the code more readable, and easier to verify.
					switch {
					case strings.HasSuffix(cs, "MD5"):
					case strings.HasSuffix(cs, "SHA"):
					case strings.Contains(cs, "NULL"):
					case strings.Contains(cs, "RC4"):
					case strings.Contains(cs, "DES"):
					case strings.Contains(cs, "IDEA"):
					case strings.Contains(cs, "anon"):
					default:
						return
					}

					log.Infof("TLS inspector: %s uses insecure cipher suite, blocking", link)
					link.AddReason(fmt.Sprintf("TLS uses insecure cipher suite"))
					return inspection.BLOCK_LINK
				}

				return

			}
			log.Warningf("TLS inspector: %s: failed to parse server hello", pkt)

		// Certificate from server
		case tlslib.TypeCertificate:
			var msg tlslib.CertificateMsg
			if msg.Unmarshal(data[5:]) {

				// parse certificates
				certs := make([]*x509.Certificate, len(msg.Certificates))
				for key, bytes := range msg.Certificates {
					cert, err := x509.ParseCertificate(bytes)
					if err != nil {
						if tlsInspection.EnforceRevocation {
							log.Infof("TLS inspector: %s: failed to parse cert, denying")
							link.AddReason("failed to parse TLS certificates")
							return inspection.BLOCK_LINK
						}
						return inspection.STOP_INSPECTING
					}
					certs[key] = cert
				}

				// always check signatures
				if tlsInspection.ServerName == "" && len(certs[0].DNSNames) > 0 {
					// ignore missing SNI, as we already checked for that earlier
					tlsInspection.ServerName = certs[0].DNSNames[0]
				}
				verifiedChain, err := verify.CheckSignatures(tlsInspection.ServerName, certs)
				if err != nil {
					log.Infof("TLS inspector: certificate invalid: %s, denying", err)
					link.AddReason(fmt.Sprintf("certificate invalid: %s", err))
					return inspection.BLOCK_LINK
				}

				// always check if we already know of cert revocation
				ok, _ := verify.CheckKnownRevocation(verifiedChain)
				if !ok {
					log.Infof("TLS inspector: certificate is revoked, denying", err)
					link.AddReason("certificate is revoked")
					return inspection.BLOCK_LINK
				}

				// check recocation, either now or later
				if tlsInspection.EnforceRevocation {
					ok, err := verify.CheckRecovation(verifiedChain)
					if !ok {
						log.Infof("TLS inspector: %s: failed to check revocation: %s", link, err)
						link.AddReason(fmt.Sprintf("failed to check revocation: %s", err))
						return inspection.BLOCK_LINK
					}
					if err != nil {
						log.Infof("TLS inspector: %s: softfailed to check revocation: %s", link, err)
						return inspection.STOP_INSPECTING
					}
					log.Infof("TLS inspector: %s: verified certificate", link)
					return inspection.STOP_INSPECTING
				} else {
					tlsInspection.verification = make(chan bool, 1)
					tlsInspection.WaitingForVerification = true
					go func() {
						runtime.Gosched()
						ok, err := verify.CheckRecovation(verifiedChain)
						if !ok {
							log.Infof("TLS inspector (delayed): %s: failed to check revocation: %s", link, err)
							// TODO: think about locking Reason, right now
							link.AddReason(fmt.Sprintf("failed to check revocation: %s", err))
						} else if err != nil {
							log.Infof("TLS inspector (delayed): %s: softfailed to check revocation: %s", link, err)
						} else {
							log.Infof("TLS inspector (delayed): %s: verified certificate", link)
						}
						tlsInspection.verification <- ok
					}()
					return inspection.DO_NOTHING
				}

			}
			log.Warningf("TLS inspector: %s: failed to parse server cert msg", link)

		}
	}

	return

}
