package web

// HTTPS for the page, which a phone's browser requires before it lets a page
// send notifications or be installed as an app. Either the user's own
// certificate — their VPN's, say (`tailscale cert`) — or one this machine
// signs with an authority of its own, made the first time, which the phone
// is told to trust once.
//
// That authority is constrained (X.509 name constraints) to private networks
// and local names: installed on a phone, it can vouch for this machine on the
// home network or the VPN and for nothing on the internet, so its key leaking
// from the desk would not let anyone pose as a bank to the phone.

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// privateNets are the networks the local authority may vouch for: the
// private ranges, the carrier-grade range VPNs such as Tailscale use, and
// this machine's own.
var privateNets = []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "100.64.0.0/10", "127.0.0.0/8", "fc00::/7", "::1/128"}

// CAPaths are where the local authority's certificate and key are kept.
func CAPaths(stateDir string) (cert, key string) {
	return filepath.Join(stateDir, "web-ca.crt"), filepath.Join(stateDir, "web-ca.key")
}

// LoadCA reads the local authority, making it the first time: its
// certificate (PEM, what a phone installs) and its key.
func LoadCA(stateDir string) (*x509.Certificate, *ecdsa.PrivateKey, []byte, error) {
	certPath, keyPath := CAPaths(stateDir)
	certPEM, err := os.ReadFile(certPath)
	keyPEM, kerr := os.ReadFile(keyPath)
	if err == nil && kerr == nil {
		cert, key, err := parseCA(certPEM, keyPEM)
		return cert, key, certPEM, err
	}
	if !os.IsNotExist(err) && err != nil {
		return nil, nil, nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}
	host, _ := os.Hostname()
	tmpl := &x509.Certificate{
		SerialNumber:                serial(),
		Subject:                     pkix.Name{CommonName: "tend on " + host, Organization: []string{"tend (local)"}},
		NotBefore:                   time.Now().Add(-time.Hour),
		NotAfter:                    time.Now().AddDate(10, 0, 0),
		IsCA:                        true,
		BasicConstraintsValid:       true,
		MaxPathLenZero:              true,
		KeyUsage:                    x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		PermittedDNSDomainsCritical: true,
		PermittedDNSDomains:         []string{"localhost", ".local", ".lan", ".home.arpa"},
	}
	for _, n := range privateNets {
		_, ipnet, _ := net.ParseCIDR(n)
		tmpl.PermittedIPRanges = append(tmpl.PermittedIPRanges, ipnet)
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, nil, err
	}
	if err := writePrivate(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})); err != nil {
		return nil, nil, nil, err
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return nil, nil, nil, err
	}
	cert, err := x509.ParseCertificate(der)
	return cert, key, certPEM, err
}

func parseCA(certPEM, keyPEM []byte) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	cb, _ := pem.Decode(certPEM)
	kb, _ := pem.Decode(keyPEM)
	if cb == nil || kb == nil {
		return nil, nil, errors.New("web: the local certificate authority's files are not PEM")
	}
	cert, err := x509.ParseCertificate(cb.Bytes)
	if err != nil {
		return nil, nil, err
	}
	k, err := x509.ParsePKCS8PrivateKey(kb.Bytes)
	if err != nil {
		return nil, nil, err
	}
	key, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, nil, errors.New("web: the local certificate authority's key is not ECDSA")
	}
	return cert, key, nil
}

// LeafFor is a certificate for this machine's addresses and names, signed by
// the local authority. Made at every start, so a new address (another
// network, the VPN coming up) is in it; valid a year, within what phones
// accept of a certificate from an authority the user installed.
func LeafFor(ca *x509.Certificate, caKey *ecdsa.PrivateKey, hosts []string) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: "tend web"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, h := range append([]string{"localhost", "127.0.0.1"}, hosts...) {
		if ip := net.ParseIP(h); ip != nil {
			if allowedIP(ip) {
				tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
			}
		} else if allowedName(h) {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("web: signing the page's certificate: %w", err)
	}
	return tls.Certificate{Certificate: [][]byte{der, ca.Raw}, PrivateKey: key}, nil
}

// allowedIP is an address the local authority may vouch for.
func allowedIP(ip net.IP) bool {
	for _, n := range privateNets {
		if _, ipnet, _ := net.ParseCIDR(n); ipnet.Contains(ip) {
			return true
		}
	}
	return false
}

// allowedName is a name the local authority may vouch for.
func allowedName(name string) bool {
	for _, d := range []string{"localhost", ".local", ".lan", ".home.arpa"} {
		if name == d || name == strings.TrimPrefix(d, ".") || strings.HasSuffix(name, d) {
			return true
		}
	}
	return false
}

func serial() *big.Int {
	n, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 120))
	return n
}
