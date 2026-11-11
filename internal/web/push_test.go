package web

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/hkdf"
	"crypto/sha256"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func unb64(t *testing.T, s string) []byte {
	t.Helper()
	b, err := b64.DecodeString(strings.ReplaceAll(s, " ", ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestPushEncryptionIsRFC8291 encrypts RFC 8291's own example — its keys,
// salt and plaintext (Appendix A) — and must produce the message the RFC
// shows, byte for byte. If it regresses, every phone's browser throws the
// notification away as undecryptable, silently.
func TestPushEncryptionIsRFC8291(t *testing.T) {
	server, err := ecdh.P256().NewPrivateKey(unb64(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"))
	if err != nil {
		t.Fatal(err)
	}
	if got, want := b64.EncodeToString(server.PublicKey().Bytes()),
		"BP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A8"; got != want {
		t.Fatalf("as_public %s", got)
	}
	got, err := encryptWith(server,
		unb64(t, "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"),
		unb64(t, "BTBZMqHH6r4Tts7J_aSIgg"),
		unb64(t, "DGv6ra1nlYgDCS1FRnbzlw"),
		[]byte("When I grow up, I want to be a watermelon"))
	if err != nil {
		t.Fatal(err)
	}
	want := "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27ml" +
		"mlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPT" +
		"pK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN"
	if b64.EncodeToString(got) != want {
		t.Errorf("message\n got  %s\n want %s", b64.EncodeToString(got), want)
	}
}

// decryptAsPhone is the browser's side of RFC 8291, for checking what a
// push service was handed.
func decryptAsPhone(t *testing.T, ua *ecdh.PrivateKey, auth, body []byte) []byte {
	t.Helper()
	salt, idlen := body[:16], int(body[20])
	asPublic, ciphertext := body[21:21+idlen], body[21+idlen:]
	as, err := ecdh.P256().NewPublicKey(asPublic)
	if err != nil {
		t.Fatal(err)
	}
	shared, _ := ua.ECDH(as)
	prkKey, _ := hkdf.Extract(sha256.New, shared, auth)
	ikm, _ := hkdf.Expand(sha256.New, prkKey, "WebPush: info\x00"+string(ua.PublicKey().Bytes())+string(asPublic), 32)
	prk, _ := hkdf.Extract(sha256.New, ikm, salt)
	cek, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	nonce, _ := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		t.Fatalf("the phone could not decrypt it: %v", err)
	}
	if plain[len(plain)-1] != 0x02 {
		t.Fatal("no last-record delimiter")
	}
	return plain[:len(plain)-1]
}

// TestAPushReachesThePhoneAndAGoneOneIsForgotten: a notice is posted to the
// subscription's push service encrypted for the phone, signed with this
// server's key (VAPID) for that service's origin, and readable by the phone;
// a service answering 410 takes the subscription off. If it regresses, the
// phone hears nothing, or the server keeps posting to a phone long gone.
func TestAPushReachesThePhoneAndAGoneOneIsForgotten(t *testing.T) {
	key, err := LoadVAPID(filepath.Join(t.TempDir(), "k.pem"))
	if err != nil {
		t.Fatal(err)
	}
	phone, _ := ecdh.P256().GenerateKey(nil)
	auth := []byte("0123456789abcdef")

	var got struct {
		auth, encoding string
		body           []byte
	}
	gone := false
	service := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if gone {
			w.WriteHeader(http.StatusGone)
			return
		}
		got.auth, got.encoding = r.Header.Get("Authorization"), r.Header.Get("Content-Encoding")
		got.body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer service.Close()

	p := NewPusher(key, filepath.Join(t.TempDir(), "subs.json"), "mailto:tend@example.com")
	p.client = service.Client()
	sub := Subscription{Endpoint: service.URL + "/push/abc"}
	sub.Keys.P256dh = b64.EncodeToString(phone.PublicKey().Bytes())
	sub.Keys.Auth = b64.EncodeToString(auth)
	if err := p.Subscribe(sub); err != nil {
		t.Fatal(err)
	}
	if err := p.Send(Notice{Title: "claude needs attention", Body: "Do you want to proceed?", Pane: 7}); err != nil {
		t.Fatal(err)
	}
	if got.encoding != "aes128gcm" {
		t.Errorf("Content-Encoding %q", got.encoding)
	}
	var n Notice
	if err := json.Unmarshal(decryptAsPhone(t, phone, auth, got.body), &n); err != nil || n.Body != "Do you want to proceed?" || n.Pane != 7 {
		t.Errorf("the phone read %+v (%v)", n, err)
	}

	// The signature: a JWT for the service's origin, verified with the key
	// the header names, which is the one pages subscribe with.
	parts := strings.Split(strings.TrimPrefix(got.auth, "vapid t="), ", k=")
	if len(parts) != 2 || parts[1] != p.PublicKey() {
		t.Fatalf("Authorization %q", got.auth)
	}
	jwt := strings.Split(parts[0], ".")
	var claims struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}
	_ = json.Unmarshal(unb64(t, jwt[1]), &claims)
	if claims.Aud != service.URL || claims.Sub != "mailto:tend@example.com" || claims.Exp < time.Now().Unix() {
		t.Errorf("claims %+v", claims)
	}
	sig := unb64(t, jwt[2])
	digest := sha256.Sum256([]byte(jwt[0] + "." + jwt[1]))
	if !ecdsa.Verify(&key.PublicKey, digest[:], bigFrom(sig[:32]), bigFrom(sig[32:])) {
		t.Error("the VAPID signature does not verify")
	}

	gone = true
	_ = p.Send(Notice{Title: "x"})
	if p.Count() != 0 {
		t.Error("a subscription its service says is gone should be forgotten")
	}
	if reloaded := NewPusher(key, p.path, ""); reloaded.Count() != 0 {
		t.Error("…and stay forgotten on disk")
	}
}

// TestTheVAPIDKeyIsKeptAndPrivate: the push key is made once and read back
// the same, from a file only its owner can read.
func TestTheVAPIDKeyIsKeptAndPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "k.pem")
	a, err := LoadVAPID(path)
	if err != nil {
		t.Fatal(err)
	}
	b, err := LoadVAPID(path)
	if err != nil || VAPIDPublic(a) != VAPIDPublic(b) {
		t.Fatal("the key should come back the same")
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm()&0o077 != 0 {
		t.Errorf("the key file should be readable by its owner alone (%v)", err)
	}
	if bad := (Subscription{Endpoint: "http://insecure/x"}); bad.valid() == nil {
		t.Error("an http endpoint should be refused")
	}
}
