package verify

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"
	"time"

	"golang.org/x/crypto/ocsp"

	"github.com/safing/portbase/crypto/hash"
	"github.com/safing/portbase/log"
)

var (
	ocspOpts = ocsp.RequestOptions{
		Hash: crypto.SHA1,
	}

	dupOcspReqMap  = make(map[string]*sync.Mutex)
	dupOcspReqLock sync.Mutex
)

func UpdateOCSP(rCert *Cert, cert, ca *x509.Certificate, caID string) (*Cert, error) {

	if rCert.NextOCSPUpdate > time.Now().Unix() {
		return rCert, nil
	}

	var err error

	// get CA if necessary
	if ca == nil {
		ca, err = GetOrFetchIssuer(cert)
		if err != nil {
			return rCert, err
		}
	}
	if caID == "" {
		caID = hash.Sum(ca.RawSubjectPublicKeyInfo, hash.SHA2_256).Safe64()
	}

	// dedup requests
	dupOcspReqLock.Lock()
	mutex, requestActive := dupOcspReqMap[rCert.FmtKey()]
	if !requestActive {
		mutex = new(sync.Mutex)
		mutex.Lock()
		dupOcspReqMap[rCert.FmtKey()] = mutex
		dupOcspReqLock.Unlock()
	} else {
		dupOcspReqLock.Unlock()
		log.Tracef("waiting for duplicate OCSP request for %s to complete", rCert.FmtKey())
		mutex.Lock()
		// only wait until duplicate request is finished, then return
		mutex.Unlock()
		// refetch rCert
		rCert, err = GetRevokedCert(caID, cert.SerialNumber)
		if err != nil {
			return nil, fmt.Errorf("failed to refetch rCert %s: %s", rCert.FmtKey(), err)
		}
		return rCert, nil
	}
	defer func() {
		dupOcspReqLock.Lock()
		delete(dupOcspReqMap, rCert.FmtKey())
		dupOcspReqLock.Unlock()
		mutex.Unlock()
	}()

	// create request
	ocspRequest, err := ocsp.CreateRequest(cert, ca, &ocspOpts)
	if err != nil {
		return rCert, err
	}

	// fetch
	var resp *ocsp.Response
	for _, url := range cert.OCSPServer {
		resp, err = fetchOCSP(url, ocspRequest, cert, ca)
		if err != nil {
			continue
		}

		rCert.NextOCSPUpdate = resp.NextUpdate.Unix()
		if rCert.NextOCSPUpdate > rCert.Expires {
			rCert.Expires = rCert.NextOCSPUpdate
		}
		rCert.RevokedWithOCSP = resp.Status != ocsp.Good
		rCert.OCSPFailed = false
		rCert.Save()
		return rCert, nil
	}

	rCert.NextOCSPUpdate = time.Now().Add(120 * time.Second).Unix()
	rCert.OCSPFailed = true
	rCert.Save()
	return rCert, fmt.Errorf("all OCSP servers failed, last error: %s", err)
}

// fetchOCSP attempts to request an OCSP response from the
// server. The error only indicates a failure to *fetch* the
// certificate, and *does not* mean the certificate is valid.
func fetchOCSP(server string, req []byte, cert, ca *x509.Certificate) (*ocsp.Response, error) {

	var resp *http.Response
	var err error
	if len(req) > 256 {
		buf := bytes.NewBuffer(req)
		resp, err = http.Post(server, "application/ocsp-request", buf)
	} else {
		reqURL := server + "/" + base64.StdEncoding.EncodeToString(req)
		resp, err = http.Get(reqURL)
	}

	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("failed to retrieve OSCP")
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	resp.Body.Close()

	return ocsp.ParseResponseForCert(body, cert, ca)
}
