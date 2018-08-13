// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package verify

import (
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"io/ioutil"
	"math/big"
	"net/http"
	"sort"
	"sync"
	"time"

	datastore "github.com/ipfs/go-datastore"

	"github.com/Safing/safing-core/crypto/hash"
	"github.com/Safing/safing-core/database"
	"github.com/Safing/safing-core/log"
)

// CARevocationInfo saves Information on revokation of Certificates of a Certificate Authority.
type CARevocationInfo struct {
	database.Base

	CRLDistributionPoints []string
	OCSPServers           []string
	CertificateURLs       []string

	LastCRLUpdate int64
	NextCRLUpdate int64

	cert *x509.Certificate
	Raw  []byte

	Expires int64
}

var (
	caRevocationInfoModel *CARevocationInfo // only use this as parameter for database.EnsureModel-like functions

	dupCrlReqMap  = make(map[string]*sync.Mutex)
	dupCrlReqLock sync.Mutex
)

func init() {
	database.RegisterModel(caRevocationInfoModel, func() database.Model { return new(CARevocationInfo) })
}

// Create saves CARevocationInfo with the provided name in the default namespace.
func (m *CARevocationInfo) Create(name string) error {
	return m.CreateObject(&database.CARevocationInfoCache, name, m)
}

// CreateInNamespace saves CARevocationInfo with the provided name in the provided namespace.
func (m *CARevocationInfo) CreateInNamespace(namespace *datastore.Key, name string) error {
	return m.CreateObject(namespace, name, m)
}

// Save saves CARevocationInfo.
func (m *CARevocationInfo) Save() error {
	return m.SaveObject(m)
}

func (m *CARevocationInfo) GetRevokedCert(serialNumber *big.Int) (*Cert, error) {
	return GetCertFromNamespace(m.GetKey(), fmt.Sprintf("S%x", serialNumber))
}

func (m *CARevocationInfo) CreateRevokedCert(cert *Cert, serialNumber *big.Int) error {
	return cert.CreateInNamespace(m.GetKey(), fmt.Sprintf("S%x", serialNumber))
}

// GetCARevocationInfo fetches CARevocationInfo with the provided name from the default namespace.
func GetCARevocationInfo(name string) (*CARevocationInfo, error) {
	return GetCARevocationInfoFromNamespace(&database.CARevocationInfoCache, name)
}

// GetCARevocationInfoFromNamespace fetches CARevocationInfo with the provided name from the provided namespace.
func GetCARevocationInfoFromNamespace(namespace *datastore.Key, name string) (*CARevocationInfo, error) {
	object, err := database.GetAndEnsureModel(namespace, name, caRevocationInfoModel)
	if err != nil {
		return nil, err
	}
	model, ok := object.(*CARevocationInfo)
	if !ok {
		return nil, database.NewMismatchError(object, caRevocationInfoModel)
	}
	return model, nil
}

// ensureCertParsed ensures that the certificate is parsed and available in the cert attribute
func (m *CARevocationInfo) ensureCertParsed() error {
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

// UpdateCRLDistributionPoints updates the CRL Distribution Points with new urls
func (m *CARevocationInfo) UpdateCRLDistributionPoints(newCRLDistributionPoints []string) {
	var found bool
	sort.Reverse(sort.StringSlice(newCRLDistributionPoints))
	for _, newEntry := range newCRLDistributionPoints {
		found = false
		for _, entry := range m.CRLDistributionPoints {
			if newEntry == entry {
				found = true
				break
			}
		}
		if !found {
			m.CRLDistributionPoints = append([]string{newEntry}, m.CRLDistributionPoints...)
		}
	}
}

// UpdateCRL fetches and imports the CRL belonging to a CA, if expired.
func UpdateCRL(caInfo *CARevocationInfo, ca *x509.Certificate, caID string) error {
	var err error

	// ensure we have caInfo
	if caInfo == nil {
		if ca == nil && caID == "" {
			return errors.New("verify: UpdateCRL must be called with at least one of: caInfo *CARevocationInfo, ca *x509.Certificate, caID string")
		}
		if caID == "" {
			caID = hash.Sum(ca.RawSubjectPublicKeyInfo, hash.SHA2_256).Safe64()
		}
		caInfo, err = GetCARevocationInfo(caID)
		if err != nil {
			return fmt.Errorf("verify: could not get CARevocationInfo for caID %s: %s", caID, err)
		}
	}

	// don't update if we still have a valid record
	if caInfo.NextCRLUpdate > time.Now().Unix() {
		return nil
	}

	// dedup requests
	dupCrlReqLock.Lock()
	mutex, requestActive := dupCrlReqMap[caID]
	if !requestActive {
		mutex = new(sync.Mutex)
		mutex.Lock()
		dupCrlReqMap[caID] = mutex
		dupCrlReqLock.Unlock()
	} else {
		dupCrlReqLock.Unlock()
		log.Tracef("verify: waiting for duplicate CRL import for CA %s to complete", caID)
		mutex.Lock()
		// only wait until duplicate request is finished, then return
		mutex.Unlock()
		return nil
	}
	defer func() {
		dupCrlReqLock.Lock()
		delete(dupCrlReqMap, caID)
		dupCrlReqLock.Unlock()
		mutex.Unlock()
	}()

	// fetch and import CRL
	for _, url := range caInfo.CRLDistributionPoints {

		// fetch CRL
		crl, err := fetchCRL(url)
		if err != nil {
			log.Warningf("verify: failed to import CRL from %s: %s", url, err)
			continue
		}

		// check CRL signature
		// TODO: how is revokation checked when verifying CRL signature?
		err = ca.CheckCRLSignature(crl)
		if err != nil {
			log.Warningf("verify: failed to import CRL from %s: %s", url, err)
			continue
		}

		log.Infof("verify: importing verified CRL for CA %s from %s", caID, url)

		// save to DB
		newExpiry := crl.TBSCertList.NextUpdate.Add(720 * time.Hour).Unix()
		caInfo.LastCRLUpdate = time.Now().Unix()
		caInfo.NextCRLUpdate = newExpiry
		for _, entry := range crl.TBSCertList.RevokedCertificates {

			// log.Tracef("verify: importing %d", entry.SerialNumber)

			// fetch or create rCert
			rCert, err := caInfo.GetRevokedCert(entry.SerialNumber)
			if err != nil {
				rCert = new(Cert)
			}

			// update expiry
			rCert.RevokedWithCRL = true
			if newExpiry > rCert.Expires {
				rCert.Expires = newExpiry
			}

			// save
			if rCert.GetKey() == nil {
				caInfo.CreateRevokedCert(rCert, entry.SerialNumber)
			} else {
				rCert.Save()
			}
		}

		log.Tracef("verify: import from %s finished.", url)
		caInfo.Save()

		return nil
	}

	return fmt.Errorf("verify: no or only failing CRLs available for CA %s", caID)
}

func fetchCRL(url string) (*pkix.CertificateList, error) {

	client := &http.Client{
		Timeout: 1 * time.Minute,
	}
	resp, err := client.Get(url)

	if err != nil {
		return nil, fmt.Errorf("failed to retrieve CRL: %s", err)
	} else if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("failed to retrieve CRL: non-200 status code: %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read CRL: %s", err)
	}
	resp.Body.Close()

	crl, err := x509.ParseCRL(body)
	if err != nil {
		return nil, fmt.Errorf("failed to parse CRL: %s", err)
	}

	return crl, nil
}
