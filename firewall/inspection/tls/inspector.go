package tls

import (
	"crypto/x509"

	utls "github.com/refraction-networking/utls"
	"github.com/safing/portbase/log"
	"github.com/safing/portmaster/firewall/inspection"
	"github.com/safing/portmaster/network"
	"github.com/safing/portmaster/network/packet"
)

// Inspector implements a simple plain HTTP inspector.
type Inspector struct {
	data           []byte
	tries          int
	sni            string
	hasClientHello bool
	hasServerHello bool
}

// Reason implements endpoints.Reason and is used when certificate
// verification fails.
type Reason struct {
	Error string
}

func (r *Reason) String() string       { return r.Error }
func (r *Reason) Context() interface{} { return r }

func (inspector *Inspector) HandleStream(conn *network.Connection, dir network.FlowDirection, data []byte) (network.Verdict, network.VerdictReason, error) {
	inspector.data = append(inspector.data, data...)
	if len(inspector.data) < 5 {
		if len(inspector.data) == 0 {
			// This is the very very first packet of the TCP handshake
			// so we need to wait for more ...
			return network.VerdictUndecided, nil, nil
		}
		// TLS data size to short. This is likely not a TLS connection at all ...
		return network.VerdictUndeterminable, nil, nil
	}

	tls := new(TLS)
	var truncated bool
	if err := tls.DecodeNextRecord(inspector.data, false, &truncated, true); err != nil {
		// don't stop inspecting if we just have to less data
		if truncated && tls.NumberOfRecords > 0 {
			inspector.tries++
			log.Debugf("truncated TLS packet: %s", err)
			return network.VerdictUndecided, nil, nil
		}
		// we're done here ...
		log.Tracef("failed to decode TLS records: %s", err)
		return network.VerdictUndeterminable, nil, nil
	}

	// if there's no client-hello yet there must be one within the first few
	// tries.
	if !inspector.hasClientHello {
		if tls.HasClientHello() {
			inspector.hasClientHello = true
			inspector.tries = 0

			// since we have a client hello we can verify the SNI
			if sni := tls.SNI(); sni != "" && sni != conn.Entity.Domain {
				inspector.sni = sni
				log.Errorf("Found connection with entity domain (%s) not matching TLS SNI (%s.)", conn.Entity.Domain, sni)
				// TODO(ppacher): update connection entity and re-evaluate the whole connection
			}

			// continue inspection until we receive a server-hello
			return network.VerdictUndecided, nil, nil
		} else if inspector.tries > 5 {
			// we already tried 5 times to get the client hello message
			// it's time to give up now, this seems not to be a TLS
			// encrypted connection.
			return network.VerdictUndeterminable, nil, nil
		}

		// without a client hello we cannot really expect much
		// more useful data right now so retry the next time
		inspector.tries++
		return network.VerdictUndecided, nil, nil
	}

	if !inspector.hasServerHello {
		if tls.HasServerHello() {
			inspector.hasServerHello = true
			inspector.tries = 0

			// update the connection with the TLS data we already
			// have.
			v := tls.Version()
			conn.TLS = &network.TLSContext{
				Version:    v.String(),
				VersionRaw: uint16(v),
				SNI:        inspector.sni,
			}

			// TODO(ppacher): we could actually wait for ChangeCipherSpec for that ....
			conn.Encrypted = true

			// ok we got the server hello now. If the TLS connection
			// agreed upon using TLS1.3 there's nothing more we can
			// do here.
			if tls.Version() == utls.VersionTLS13 {
				return network.VerdictUndeterminable, nil, nil
			}

			// this is a TLS version lower than TLS1.3 so we
			// can inspect the certificate of the remote peer.
			// do not return here as the same TLS message might
			// already contain the certificate message ...
		} else if inspector.tries > 10 {
			// we waited for 10 packets to get the full certificate
			// message and still don't succeeded. We can give up on
			// that ...
			return network.VerdictUndeterminable, nil, nil
		}

		// without a server-hello there won't be a CertificateMsg
		// so we're done for now ...
		inspector.tries++
		return network.VerdictUndecided, nil, nil
	}

	// if we reach this point we have a client and server hello, know the TLS
	// version is lower than TLS1.3 and thus wait for the certificate message
	// so we can parse and verify the cert-chain.
	if tls.HasCertificate() {
		certs := tls.ServerCertChain()

		var intermediateCerts *x509.CertPool
		if len(certs) > 1 {
			intermediateCerts = x509.NewCertPool()
			for _, c := range certs[1:] {
				intermediateCerts.AddCert(c)
			}
		}

		// TODO(ppacher): CRL and OCSP is missing here
		chain, err := certs[0].Verify(x509.VerifyOptions{
			DNSName:       inspector.sni,
			Intermediates: intermediateCerts,
		})
		if err != nil {
			// verification failed, block the connection.
			return network.VerdictBlock, &Reason{Error: err.Error()}, nil
		}
		conn.TLS.SetChains(chain)

		// we're done inspecting the TLS session
		return network.VerdictUndeterminable, nil, nil
	}

	if inspector.tries > 10 {
		return network.VerdictUndeterminable, nil, nil
	}
	// we're still waiting for the certificate ...
	inspector.tries++
	return network.VerdictUndecided, nil, nil
}

// TODO(ppacher): get rid of this function, we already specify the name
// in inspection:RegisterInspector ...
func (*Inspector) Name() string {
	return "TLS"
}

// Destory does nothing ...
func (*Inspector) Destroy() error {
	return nil
}

func init() {
	inspection.MustRegister(&inspection.Registration{
		Name:  "TLS",
		Order: 1, // we don't actually care
		Factory: func(conn *network.Connection, pkt packet.Packet) (network.Inspector, error) {
			// We only inspect TCP sessions
			if conn.IPProtocol != packet.TCP {
				return nil, nil
			}

			return &Inspector{}, nil
		},
	})
}

// compile time check if Inspector actually satisfies
// the expected interfaces.
var _ network.StreamHandler = new(Inspector)
