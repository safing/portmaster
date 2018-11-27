package verify

import (
	"crypto/x509"
	"errors"
	"fmt"
	"time"

	"github.com/Safing/portbase/crypto/hash"
	"github.com/Safing/portbase/database"
)

// useful references:
// https://github.com/cloudflare/cfssl/blob/master/revoke/revoke.go
// Mozilla OneCRL
// https://blog.mozilla.org/security/2015/03/03/revoking-intermediate-certificates-introducing-onecrl/
// https://wiki.mozilla.org/CA:ImprovingRevocation
// Google CRLSet
// https://security.stackexchange.com/questions/55811/how-are-crlsets-more-secure
// https://www.imperialviolet.org/2012/02/05/crlsets.html
// RE: https://www.grc.com/revocation/crlsets.htm
// RE: RE: https://www.imperialviolet.org/2014/04/29/revocationagain.html

// FullCheckBytes does a full certificate check, certificates are provided as raw bytes.
// It parses the raw certificates and calls FullCheck.
func FullCheckBytes(name string, certBytes [][]byte) (bool, error) {
	certs := make([]*x509.Certificate, len(certBytes))
	for key, bytes := range certBytes {
		cert, err := x509.ParseCertificate(bytes)
		if err != nil {
			return false, errors.New("verify: failed to parse certificate: " + err.Error())
		}
		certs[key] = cert
	}

	return FullCheck(name, certs)
}

// FullCheck does a full certificate check.
// Calls CheckSignatures, CheckRecovation and CheckCertificateTransparency(TODO).
func FullCheck(name string, chain []*x509.Certificate) (bool, error) {
	verifiedChain, err := CheckSignatures(name, chain)
	if err != nil {
		return false, fmt.Errorf("verify: certificate invalid: %s", err)
	}

	return CheckRecovation(verifiedChain)
}

func CheckSignatures(name string, chain []*x509.Certificate) ([]*x509.Certificate, error) {
	if len(chain) == 0 {
		return nil, errors.New("no certificates supplied")
	}

	opts := x509.VerifyOptions{
		// Roots:         c.config.RootCAs,
		// CurrentTime:   time.Now(),
		DNSName:       name,
		Intermediates: x509.NewCertPool(),
	}

	for i, cert := range chain {
		if i == 0 {
			continue
		}
		opts.Intermediates.AddCert(cert)
	}

	verifiedChains, err := chain[0].Verify(opts)
	if err != nil {
		return nil, err
	}

	// TODO: further process all verified chains (revocation / CT)
	return verifiedChains[0], nil
}

// TODO
// func CheckCertificateTransparency(chain []*x509.Certificate, securityLevel int8) {
// }

func CheckKnownRevocation(verifiedChain []*x509.Certificate) (bool, error) {
	for i := 0; i < len(verifiedChain)-1; i++ {
		caID := hash.Sum(verifiedChain[i+1].RawSubjectPublicKeyInfo, hash.SHA2_256).Safe64()
		rCert, err := GetRevokedCert(caID, verifiedChain[i].SerialNumber)
		if err != nil {
			if err != database.ErrNotFound {
				return true, nil
			}
			return true, fmt.Errorf("verify: failed to get rCert from database: %s", err)
		}
		if rCert.IsRevoked(false) {
			return false, nil
		}
		if rCert.OCSPFailed {
			return true, errors.New("verify: OCSP failed in the past")
		}
	}
	return true, nil
}

func CheckRecovation(verifiedChain []*x509.Certificate) (bool, error) {

	for i := 0; i < len(verifiedChain)-1; i++ {
		ok, err := checkCertRevocation(verifiedChain[i], verifiedChain[i+1])
		if !ok {
			return ok, err
		}
	}
	return true, nil

}

func checkCertRevocation(cert, ca *x509.Certificate) (bool, error) {

	// proper use?
	if cert == nil {
		return false, errors.New("verify: no certificate supplied")
	}

	// check if recocation is supported
	if len(cert.CRLDistributionPoints) == 0 && len(cert.OCSPServer) == 0 {
		return true, fmt.Errorf("verify: certificate does not support OCSP or CRL.")
	}

	var err error
	if ca == nil {
		ca, err = GetOrFetchIssuer(cert)
		if err != nil {
			return true, err
		}
	}
	caID := hash.Sum(ca.RawSubjectPublicKeyInfo, hash.SHA2_256).Safe64()

	// check cert
	rCert, err := GetRevokedCert(caID, cert.SerialNumber)
	if err != nil {
		if err != database.ErrNotFound {
			return true, fmt.Errorf("verify: failed to get Cert: %s", err)
		}
		rCert = &Cert{
			cert: cert,
			// Raw: cert.Raw,
			LastSeen: time.Now().Unix(),
			Expires:  time.Now().Add(7 * 24 * time.Hour).Unix(),
		}
		rCert.CreateRevokedCert(caID, cert.SerialNumber)
	} else if rCert.IsRevoked(false) {
		return false, fmt.Errorf("verify: certificate is %s", rCert.RevocationStatus(false))
	}

	// update OCSP
	// TODO: check OCSP stapling data
	// TODO: is MustStaple already handled by golang? If not, handle it!
	if len(cert.OCSPServer) > 0 {
		if rCert == nil {
			rCert = new(Cert)
		}
		rCert, err = UpdateOCSP(rCert, cert, ca, caID)
		if err == nil && !rCert.OCSPFailed {
			// update CRL later
			go checkCRL(rCert, cert, ca, caID, false, true)
			if rCert.IsRevoked(false) {
				return false, fmt.Errorf("verify: certificate is %s", rCert.RevocationStatus(false))
			}
			return true, nil
		}
	}

	// update CRL
	return checkCRL(rCert, cert, ca, caID, false, false)
}

func checkCRL(rCert *Cert, cert, ca *x509.Certificate, caID string, hardFail bool, postpone bool) (bool, error) {

	if postpone {
		// postpone a bit to finish packet processing
		// TODO: use microtask management
		time.Sleep(1 * time.Second)
	}

	caInfo, err := GetCARevocationInfo(caID)
	if err != nil {
		if err != database.ErrNotFound {
			return true, fmt.Errorf("verify: failed to get CARevocationInfo: %s", err)
		}
		caInfo = &CARevocationInfo{
			CRLDistributionPoints: cert.CRLDistributionPoints,
			OCSPServers:           cert.OCSPServer,
			CertificateURLs:       cert.IssuingCertificateURL,
			Raw:                   ca.Raw,
			Expires:               time.Now().Add(30 * 24 * time.Hour).Unix(),
		}
		caInfo.Create(caID)
	}

	UpdateCRL(caInfo, ca, caID)

	rCert, err = GetRevokedCert(caID, cert.SerialNumber)
	if err != nil {
		return true, fmt.Errorf("verify: failed to get Cert: %s", err)
	}
	if rCert.IsRevoked(false) {
		return false, fmt.Errorf("verify: certificate is %s", rCert.RevocationStatus(false))
	}
	return true, nil

}
