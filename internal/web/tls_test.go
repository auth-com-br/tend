package web

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"os"
	"testing"
	"time"
)

// TestTheLocalAuthorityVouchesForThisMachineAndNothingElse: the page's
// certificate verifies, for this machine's private address, against the
// authority a phone installs; that authority is made once and kept, its key
// readable by its owner alone; and a certificate it signs for a name on the
// internet does not verify, whoever holds its key. If it regresses, either
// the phone refuses the page, or an authority installed on a phone could
// vouch for any site.
func TestTheLocalAuthorityVouchesForThisMachineAndNothingElse(t *testing.T) {
	dir := t.TempDir()
	ca, key, pemData, err := LoadCA(dir)
	if err != nil {
		t.Fatal(err)
	}
	again, _, _, err := LoadCA(dir)
	if err != nil || !again.Equal(ca) {
		t.Fatal("the authority should be made once and read back")
	}
	if _, keyPath := CAPaths(dir); true {
		if info, err := os.Stat(keyPath); err != nil || info.Mode().Perm()&0o077 != 0 {
			t.Error("the authority's key should be readable by its owner alone")
		}
	}
	if len(pemData) == 0 {
		t.Fatal("no certificate to give the phone")
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)

	leaf, err := LeafFor(ca, key, []string{"192.168.1.11", "8.8.8.8", "pop-os", "box.local"})
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(leaf.Certificate[0])
	for _, host := range []string{"192.168.1.11", "localhost", "box.local"} {
		if _, err := cert.Verify(x509.VerifyOptions{DNSName: host, Roots: roots}); err != nil {
			t.Errorf("%s: %v", host, err)
		}
	}
	if _, err := cert.Verify(x509.VerifyOptions{DNSName: "8.8.8.8", Roots: roots}); err == nil {
		t.Error("a public address should not be in the page's certificate")
	}

	// Signed by the authority for a site on the internet: refused.
	k, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	bad := &x509.Certificate{
		SerialNumber: big.NewInt(9), Subject: pkix.Name{CommonName: "bank"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		DNSNames: []string{"bank.example.com"}, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, bad, ca, &k.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	forged, _ := x509.ParseCertificate(der)
	if _, err := forged.Verify(x509.VerifyOptions{DNSName: "bank.example.com", Roots: roots}); err == nil {
		t.Error("the local authority must not vouch for a name on the internet")
	}
}
