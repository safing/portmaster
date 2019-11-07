package verify

import (
	"crypto/x509"
	"fmt"
	"testing"
)

// func TestCertFetching(t *testing.T) {
//
// 	cert, err := GetOrFetchCert([]string{"http://cert.int-x3.letsencrypt.org/"})
// 	if err != nil {
// 		t.Errorf("failed to GetOrFetchCert: %s", err)
// 	}
// 	fmt.Printf("%v\n", cert)
//
// 	GetOrFetchCert([]string{"http://cert.int-x3.letsencrypt.org/"})
// 	if err != nil {
// 		t.Errorf("failed to GetOrFetchCert: %s", err)
// 	}
// 	fmt.Printf("%v\n", cert)
//
// }

func TestMissingChain(t *testing.T) {

	certPEM := []byte(`-----BEGIN CERTIFICATE-----
MIIEuzCCA6OgAwIBAgIQRMpyC8ARigQMjp7ywd0HOzANBgkqhkiG9w0BAQsFADBD
MQswCQYDVQQGEwJVUzEVMBMGA1UEChMMdGhhd3RlLCBJbmMuMR0wGwYDVQQDExR0
aGF3dGUgU0hBMjU2IFNTTCBDQTAeFw0xNjAxMTkwMDAwMDBaFw0xODAxMTgyMzU5
NTlaMHQxCzAJBgNVBAYTAkRFMRYwFAYDVQQIDA1OaWVkZXJzYWNoc2VuMREwDwYD
VQQHDAhIYW5ub3ZlcjEjMCEGA1UECgwaSGVpc2UgTWVkaWVuIEdtYkggJiBDby4g
S0cxFTATBgNVBAMMDHd3dy5oZWlzZS5kZTCCASIwDQYJKoZIhvcNAQEBBQADggEP
ADCCAQoCggEBAL+S5DqFzKXKpDuPKxSkhG/2ap4kFxBXv0u7gmAE30Cya16RASHt
oSZCjHPE2yyGhLaLTjnf6kC4AgJ4eQtStPb0Oc7NodEbeFYzn6ei1OyXmYD4V7kL
HYwjGIE3TZch4scb6peuNexYotHLB032KL/csScfdtDSpYg6ZEJ7kYI2MSqP4ogo
BbakfgVjCdTwi4PWfmRO080t6MUEfJbfqojxcVxO70femsvmteU5/7IaXXNCnnoF
KWl/G2WgmD8eBu2+HY9ojRrG5DrbKcz6XcNJCz88khQ+x/1EPsEEWjHiADfvl3HH
uWmFf0BBoB3V3oL+v4i1zu2ffZH6UdlekxMCAwEAAaOCAXgwggF0MCEGA1UdEQQa
MBiCDHd3dy5oZWlzZS5kZYIIaGVpc2UuZGUwCQYDVR0TBAIwADBuBgNVHSAEZzBl
MGMGBmeBDAECAjBZMCYGCCsGAQUFBwIBFhpodHRwczovL3d3dy50aGF3dGUuY29t
L2NwczAvBggrBgEFBQcCAjAjDCFodHRwczovL3d3dy50aGF3dGUuY29tL3JlcG9z
aXRvcnkwDgYDVR0PAQH/BAQDAgWgMB8GA1UdIwQYMBaAFCuaNa4BGDgw4XB6BeAR
dqPOvZAUMCsGA1UdHwQkMCIwIKAeoByGGmh0dHA6Ly90Zy5zeW1jYi5jb20vdGcu
Y3JsMB0GA1UdJQQWMBQGCCsGAQUFBwMBBggrBgEFBQcDAjBXBggrBgEFBQcBAQRL
MEkwHwYIKwYBBQUHMAGGE2h0dHA6Ly90Zy5zeW1jZC5jb20wJgYIKwYBBQUHMAKG
Gmh0dHA6Ly90Zy5zeW1jYi5jb20vdGcuY3J0MA0GCSqGSIb3DQEBCwUAA4IBAQAy
dryRQkVsQIxrhyGlAdGVR9ygOwtJBUq0najtb0+/HNEysN0QZjguNKDlzmHm1pAI
gYR5hTlQH93XqL4d1+UVRL61hiKJja0EEkHnDEtde9eRsyvVfBHRrUF/qV6ar3yG
0NHQdlZSIGpztKap4Za6RKxwgZid+LC1k67a4envcSPxHnREtR23mDIpe6u0NoQA
VhUDXwbAUHO2A/6dKvhQVlPsZES56hg0uPrA6ODCxHQeRId2mn+/HY2VnDkwfJRZ
NwVkZSvy4/Mi4cZYkkcW3Z9gePYDiNGe1wBr4/H3ffK63ek6W7Uy3ju2TpMiOyIB
gQDY9b+bJOhhWfRQhONh
-----END CERTIFICATE-----`)

	cert, err := ParsePEMCertificate(certPEM)
	if err != nil {
		t.Errorf("failed to parse cert: %s", err)
		return
	}

	_, err = cert.Verify(x509.VerifyOptions{})
	// chains, err = cert.Verify(x509.VerifyOptions{})
	if err != nil {
		err = fmt.Errorf("failed to verify certificate: %s", err)
	}

}
