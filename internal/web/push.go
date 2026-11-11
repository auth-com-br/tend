package web

// Web Push: a phone that turned notifications on is told when an agent needs
// it, with the page closed. The browser gives the page a subscription — an
// address at its vendor's push service and the keys to encrypt for it — and
// this process posts each notification there: encrypted for the phone alone
// (RFC 8291, so the push service carries what it cannot read) and signed with
// this server's key (VAPID, RFC 8292, so the service knows who sends). All of
// it is the standard library; no dependency.
//
// A browser allows it only on a secure page — HTTPS — which is why turning
// notifications on needs `[web] https` (tls.go).

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// recordSize is the single record's size the message says it is in: one
// record holds any notification this sends.
const recordSize = 4096

var b64 = base64.RawURLEncoding

// Subscription is what a browser's PushManager.subscribe gives the page,
// as its toJSON writes it.
type Subscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
}

// valid checks a subscription a page sent: an https address and keys of the
// right sizes, so a malformed one is refused at once rather than at the
// first notification.
func (s Subscription) valid() error {
	u, err := url.Parse(s.Endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return errors.New("push: the endpoint is not an https address")
	}
	if k, err := b64.DecodeString(strings.TrimRight(s.Keys.P256dh, "=")); err != nil || len(k) != 65 {
		return errors.New("push: p256dh is not a P-256 public key")
	}
	if a, err := b64.DecodeString(strings.TrimRight(s.Keys.Auth, "=")); err != nil || len(a) != 16 {
		return errors.New("push: auth is not 16 bytes")
	}
	return nil
}

// --- VAPID key --------------------------------------------------------------

// VAPIDPath is where a session's push signing key is kept.
func VAPIDPath(stateDir, session string) string {
	return filepath.Join(stateDir, "web-"+session+".vapid.pem")
}

// LoadVAPID reads the push signing key, making it the first time. It is the
// server's identity to the push services: a new one would leave every phone
// subscribed with the old one unreachable, so it is kept, mode 0600.
func LoadVAPID(path string) (*ecdsa.PrivateKey, error) {
	if data, err := os.ReadFile(path); err == nil {
		block, _ := pem.Decode(data)
		if block == nil {
			return nil, fmt.Errorf("push: %s is not a PEM key", path)
		}
		key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err != nil {
			return nil, err
		}
		k, ok := key.(*ecdsa.PrivateKey)
		if !ok || k.Curve != elliptic.P256() {
			return nil, fmt.Errorf("push: %s is not a P-256 key", path)
		}
		return k, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err := writePrivate(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})); err != nil {
		return nil, err
	}
	return key, nil
}

// VAPIDPublic is the key a page subscribes with (applicationServerKey): the
// public point, uncompressed, base64url.
func VAPIDPublic(key *ecdsa.PrivateKey) string {
	pub, err := key.PublicKey.ECDH()
	if err != nil {
		return ""
	}
	return b64.EncodeToString(pub.Bytes())
}

// vapidToken is the signed claim a push service checks: who it is for (the
// endpoint's origin), until when, and who to tell if it misbehaves.
func vapidToken(key *ecdsa.PrivateKey, audience, subject string, expires time.Time) (string, error) {
	header := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, err := json.Marshal(map[string]any{"aud": audience, "exp": expires.Unix(), "sub": subject})
	if err != nil {
		return "", err
	}
	signing := header + "." + b64.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		return "", err
	}
	// ES256 is r and s, 32 bytes each, not ASN.1.
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signing + "." + b64.EncodeToString(sig), nil
}

// --- encryption (RFC 8291) ---------------------------------------------------

// encrypt is a notification's body for one subscription, encrypted with a
// fresh key and salt.
func encrypt(sub Subscription, plaintext []byte) ([]byte, error) {
	server, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	ua, err := b64.DecodeString(strings.TrimRight(sub.Keys.P256dh, "="))
	if err != nil {
		return nil, err
	}
	auth, err := b64.DecodeString(strings.TrimRight(sub.Keys.Auth, "="))
	if err != nil {
		return nil, err
	}
	return encryptWith(server, ua, auth, salt, plaintext)
}

// encryptWith is RFC 8291's aes128gcm message, its keys given: the
// application server's key, the user agent's public key and authentication
// secret, and the salt. Section 3.4 and Appendix A, step by step.
func encryptWith(server *ecdh.PrivateKey, uaPublic, auth, salt, plaintext []byte) ([]byte, error) {
	if len(plaintext)+1+16 > recordSize-86 {
		return nil, errors.New("push: the notification is too long for one record")
	}
	ua, err := ecdh.P256().NewPublicKey(uaPublic)
	if err != nil {
		return nil, err
	}
	shared, err := server.ECDH(ua)
	if err != nil {
		return nil, err
	}
	asPublic := server.PublicKey().Bytes()
	// PRK_key = HMAC-SHA-256(auth_secret, ecdh_secret)
	prkKey, err := hkdf.Extract(sha256.New, shared, auth)
	if err != nil {
		return nil, err
	}
	// IKM = HMAC-SHA-256(PRK_key, "WebPush: info" || 0x00 || ua_public || as_public || 0x01)
	keyInfo := "WebPush: info\x00" + string(uaPublic) + string(asPublic)
	ikm, err := hkdf.Expand(sha256.New, prkKey, keyInfo, 32)
	if err != nil {
		return nil, err
	}
	// PRK = HMAC-SHA-256(salt, IKM)
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// The last (here, only) record ends with the 0x02 delimiter.
	padded := append(append([]byte(nil), plaintext...), 0x02)
	var out bytes.Buffer
	out.Write(salt)
	_ = binary.Write(&out, binary.BigEndian, uint32(recordSize))
	out.WriteByte(byte(len(asPublic)))
	out.Write(asPublic)
	out.Write(gcm.Seal(nil, nonce, padded, nil))
	return out.Bytes(), nil
}

// --- sending -----------------------------------------------------------------

// errGone is a subscription its push service says is no more: the phone
// turned notifications off, or the browser dropped it.
var errGone = errors.New("push: the subscription is gone")

// Pusher sends notifications to the phones subscribed to a session.
type Pusher struct {
	key     *ecdsa.PrivateKey
	subject string
	path    string
	client  *http.Client

	mu   sync.Mutex
	subs []Subscription
}

// PushPath is where a session's subscribed phones are kept.
func PushPath(stateDir, session string) string {
	return filepath.Join(stateDir, "web-"+session+".push.json")
}

// NewPusher is the sender for a session, with the phones subscribed before.
// subject is how a push service can reach the sender: a mailto: or https:.
func NewPusher(key *ecdsa.PrivateKey, path, subject string) *Pusher {
	p := &Pusher{key: key, subject: subject, path: path, client: &http.Client{Timeout: 15 * time.Second}}
	if data, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(data, &p.subs)
	}
	return p
}

// PublicKey is the key a page subscribes with.
func (p *Pusher) PublicKey() string { return VAPIDPublic(p.key) }

// Subscribe adds a phone, or keeps it once when it subscribes again.
func (p *Pusher) Subscribe(sub Subscription) error {
	if err := sub.valid(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, s := range p.subs {
		if s.Endpoint == sub.Endpoint {
			p.subs[i] = sub
			return p.saveLocked()
		}
	}
	p.subs = append(p.subs, sub)
	return p.saveLocked()
}

// Unsubscribe takes a phone off.
func (p *Pusher) Unsubscribe(endpoint string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	kept := p.subs[:0]
	for _, s := range p.subs {
		if s.Endpoint != endpoint {
			kept = append(kept, s)
		}
	}
	p.subs = kept
	return p.saveLocked()
}

// Count is how many phones are subscribed.
func (p *Pusher) Count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.subs)
}

func (p *Pusher) saveLocked() error {
	data, err := json.MarshalIndent(p.subs, "", "  ")
	if err != nil {
		return err
	}
	return writePrivate(p.path, data)
}

// Notice is what a phone shows: the notification's title and text, and the
// pane a tap opens.
type Notice struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Pane  uint64 `json:"pane,omitempty"`
	// Tag replaces an earlier notification about the same thing rather than
	// piling one more on.
	Tag string `json:"tag,omitempty"`
}

// Send tells every subscribed phone, and forgets the ones whose push
// service says they are gone. It reports the first other failure.
func (p *Pusher) Send(n Notice) error {
	body, err := json.Marshal(n)
	if err != nil {
		return err
	}
	p.mu.Lock()
	subs := append([]Subscription(nil), p.subs...)
	p.mu.Unlock()
	var first error
	for _, sub := range subs {
		err := p.send(sub, body)
		switch {
		case errors.Is(err, errGone):
			_ = p.Unsubscribe(sub.Endpoint)
		case err != nil && first == nil:
			first = err
		}
	}
	return first
}

func (p *Pusher) send(sub Subscription, payload []byte) error {
	body, err := encrypt(sub, payload)
	if err != nil {
		return err
	}
	u, err := url.Parse(sub.Endpoint)
	if err != nil {
		return err
	}
	token, err := vapidToken(p.key, u.Scheme+"://"+u.Host, p.subject, time.Now().Add(12*time.Hour))
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("Content-Encoding", "aes128gcm")
	// A day: a phone off for the night still hears that an agent waits.
	req.Header.Set("TTL", "86400")
	req.Header.Set("Urgency", "high")
	req.Header.Set("Authorization", "vapid t="+token+", k="+p.PublicKey())
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone:
		return errGone
	case resp.StatusCode >= 300:
		return fmt.Errorf("push: %s answered %s", u.Host, resp.Status)
	}
	return nil
}

// writePrivate writes a file only its owner reads, all at once: a crash
// halfway leaves the old file, not half of a key.
func writePrivate(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// bigFrom is a big.Int from bytes, for tests that check a signature.
func bigFrom(b []byte) *big.Int { return new(big.Int).SetBytes(b) }
