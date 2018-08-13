// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package verify

import (
	"bytes"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/ioutil"
	"math/big"
	"net/http"
	"strings"

	"github.com/cloudflare/cfssl/crypto/pkcs7"
	datastore "github.com/ipfs/go-datastore"

	"github.com/Safing/safing-core/crypto/hash"
	"github.com/Safing/safing-core/database"
)

// Cert saves a certificate.
type Cert struct {
	database.Base

	cert *x509.Certificate
	Raw  []byte

	RevokedWithCRL    bool `*:",omitempty"`
	RevokedWithOneCRL bool `*:",omitempty"`
	RevokedWithCRLSet bool `*:",omitempty"`
	RevokedWithOCSP   bool `*:",omitempty"`

	OCSPFailed     bool
	NextOCSPUpdate int64

	LastSeen int64
	Expires  int64
}

var certModel *Cert // only use this as parameter for database.EnsureModel-like functions

func init() {
	database.RegisterModel(certModel, func() database.Model { return new(Cert) })
}

// ensureParsed ensures that the certificate is parsed and available in the cert attribute
func (m *Cert) ensureParsed() error {
	if m.cert != nil {
		if len(m.Raw) == 0 {
			return errors.New("certificate data not saved")
		}
		var err error
		m.cert, err = x509.ParseCertificate(m.Raw)
		if err != nil {
			return fmt.Errorf("could not parse certificate: %s", err)
		}
	}
	return nil
}

// GetCertificate returns the underlying x509.Certificate
func (m *Cert) GetCertificate() (*x509.Certificate, error) {
	if err := m.ensureParsed(); err != nil {
		return nil, err
	}
	return m.cert, nil
}

// IsRevoked returns if the certificate has been revoked.
func (m *Cert) IsRevoked(hardFail bool) bool {
	if m.RevokedWithCRL || m.RevokedWithOneCRL || m.RevokedWithCRLSet || m.RevokedWithOCSP {
		return true
	}
	if hardFail && m.OCSPFailed {
		return true
	}
	return false
}

// RevocationStatus returns the status of the certificate in form of a string to be appended to something like "The certificate is ".
func (m *Cert) RevocationStatus(hardFail bool) string {
	if !m.IsRevoked(hardFail) {
		return "not revoked"
	}
	var revokedBy []string
	if m.RevokedWithCRL {
		revokedBy = append(revokedBy, "CRL")
	}
	if m.RevokedWithOneCRL {
		revokedBy = append(revokedBy, "OneCRL")
	}
	if m.RevokedWithCRLSet {
		revokedBy = append(revokedBy, "CRLSet")
	}
	if m.RevokedWithOCSP {
		revokedBy = append(revokedBy, "OCSP")
	}
	if len(revokedBy) > 0 {
		return fmt.Sprintf("revoked by %s", strings.Join(revokedBy, ", "))
	}
	return fmt.Sprintf("possibly revoked (OCSP failed) - hardfailing as requested.")
}

// CreateWithUrl saves Cert in the default namespace using the certificate URL as the key.
func (m *Cert) CreateWithUrl(url string) error {
	return m.CreateObject(&database.CertCache, fmt.Sprintf("U%x", url), m)
}

// CreateWithSPKI saves Cert in the default namespace using the certificate SPKI as the key.
func (m *Cert) CreateWithSPKI(spki []byte) error {
	return m.CreateObject(&database.CertCache, fmt.Sprintf("K%x", hash.Sum(spki, hash.SHA2_256).Safe64()), m)
}

// CreateRevokedCert creates a new Cert in its CA's namespace with its Serial Number
func (m *Cert) CreateRevokedCert(caID string, serialNumber *big.Int) error {
	namespace := database.CARevocationInfoCache.ChildString(fmt.Sprintf("CARevocationInfo:%s", caID))
	return m.CreateInNamespace(&namespace, fmt.Sprintf("S%x", serialNumber))
}

// CreateInNamespace saves Cert with the provided name in the provided namespace.
func (m *Cert) CreateInNamespace(namespace *datastore.Key, name string) error {
	return m.CreateObject(namespace, name, m)
}

// Save saves Cert.
func (m *Cert) Save() error {
	return m.SaveObject(m)
}

// GetCertWithURL fetches Cert from the default namespace using the certificate URL as the key.
func GetCertWithURL(url string) (*Cert, error) {
	return GetCertFromNamespace(&database.CertCache, fmt.Sprintf("U%x", url))
}

// GetCertWithSPKI fetches Cert from the default namespace using the certificate SPKI as the key.
func GetCertWithSPKI(spki []byte) (*Cert, error) {
	return GetCertFromNamespace(&database.CertCache, fmt.Sprintf("K%x", hash.Sum(spki, hash.SHA2_256).Safe64()))
}

// GetCertFromNamespace gets Cert with the provided name from the provided namespace.
func GetCertFromNamespace(namespace *datastore.Key, name string) (*Cert, error) {
	object, err := database.GetAndEnsureModel(namespace, name, certModel)
	if err != nil {
		return nil, err
	}
	model, ok := object.(*Cert)
	if !ok {
		return nil, database.NewMismatchError(object, certModel)
	}
	return model, nil
}

// GetRevokedCert gets Cert from its CA's namespace with its Serial Number
func GetRevokedCert(caID string, serialNumber *big.Int) (*Cert, error) {
	namespace := database.CARevocationInfoCache.ChildString(fmt.Sprintf("CARevocationInfo:%s", caID))
	return GetCertFromNamespace(&namespace, fmt.Sprintf("S%x", serialNumber))
}

func GetOrFetchCert(urls []string) (*x509.Certificate, error) {
	// TODO: handle root CAs
	for _, url := range urls {
		cert, err := GetCertWithURL(url)
		if err != nil {
			continue
		}
		certificate, err := x509.ParseCertificate(cert.Raw)
		if err == nil {
			return certificate, nil
		}
	}
	return ImportCert(urls)
}

func GetOrFetchIssuer(cert *x509.Certificate) (*x509.Certificate, error) {
	if len(cert.IssuingCertificateURL) == 0 {
		return nil, errors.New("no issuing certificate URLs")
	}
	return GetOrFetchCert(cert.IssuingCertificateURL)
}

func ImportCert(urls []string) (*x509.Certificate, error) {
	var err error
	for _, url := range urls {

		var resp *http.Response
		resp, err = http.Get(url)
		if err != nil {
			err = fmt.Errorf("could not fetch certificate from %s: %s", url, err)
			continue
		}

		var data []byte
		data, err = ioutil.ReadAll(resp.Body)
		if err != nil {
			err = fmt.Errorf("could not read certificate from %s: %s", url, err)
			continue
		}
		resp.Body.Close()

		var cert *x509.Certificate
		cert, err = x509.ParseCertificate(data)
		if err != nil {
			cert, err = ParsePEMCertificate(data)
		}
		if err != nil {
			err = fmt.Errorf("could not parse certificate from %s: %s", url, err)
			continue
		}

		// verify

		_, err = cert.Verify(x509.VerifyOptions{})
		// chains, err = cert.Verify(x509.VerifyOptions{})
		if err != nil {
			err = fmt.Errorf("failed to verify certificate from %s: %s", url, err)
		}

		// check revocation

		// save

		return cert, nil

	}

	return nil, fmt.Errorf("no or only failing Certs available, last error: %s", err)

}

// ParsePEMCertificate parses and returns a PEM-encoded certificate,
// can handle PEM encoded PKCS #7 structures.
func ParsePEMCertificate(certPEM []byte) (*x509.Certificate, error) {

	block, _ := pem.Decode(bytes.TrimSpace(certPEM))
	if block == nil {
		return nil, errors.New("decoding failed")
	}

	// if len(rest) > 0 {
	//   return nil, errors.New("decoding failed: the PEM data should contain only one object")
	// }

	cert, err := x509.ParseCertificate(block.Bytes)
	if err == nil {
		return cert, nil
	}

	pkcs7data, err := pkcs7.ParsePKCS7(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing failed: %s", err)
	}
	if pkcs7data.ContentInfo != "SignedData" {
		return nil, errors.New("parsing failed: only PKCS #7 Signed Data Content Info supported for certificate parsing")
	}
	certs := pkcs7data.Content.SignedData.Certificates
	if certs == nil {
		return nil, errors.New("PKCS #7 structure contains no certificates")
	}
	// if len(certs) > 1 {
	//   return nil, errors.New("decoding failed: the PKCS7 object in the PEM data should contain only one certificate"))
	// }
	return certs[0], nil

}
