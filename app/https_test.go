package app

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHTTPSSettings(t *testing.T) {
	env := map[string]string{
		"MYAPP_PUBLIC_URL":      "https://app.example.com/",
		"MYAPP_TRUSTED_PROXIES": "127.0.0.1/32, 192.0.2.0/24",
	}
	h, err := readHTTPS("myapp", func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	cfg := map[string]any{"identity": map[string]any{}}
	h.apply(cfg)
	id := cfg["identity"].(map[string]any)
	if id["public_url"] != "https://app.example.com" || len(id["trusted_proxies"].([]any)) != 2 {
		t.Fatalf("identity config %v", id)
	}
	// An app without sign-in has no identity block, and none is added.
	off := map[string]any{}
	h.apply(off)
	if _, ok := off["identity"]; ok {
		t.Fatal("identity block added to an app without sign-in")
	}
	t.Setenv("SURFACE_ALLOWED_HOSTS", "other.example")
	h.allowHost()
	h.allowHost() // once only
	if got := os.Getenv("SURFACE_ALLOWED_HOSTS"); got != "other.example,app.example.com" {
		t.Fatalf("allowed hosts %q", got)
	}
	for _, bad := range []map[string]string{
		{"MYAPP_PUBLIC_URL": "app.example.com"},
		{"MYAPP_TLS_CERT": "/x.pem"},
	} {
		if _, err := readHTTPS("myapp", func(k string) string { return bad[k] }); err == nil {
			t.Fatalf("accepted %v", bad)
		}
	}
}

func writeCert(t *testing.T, dir, cn string) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: cn},
		DNSNames: []string{cn}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := x509.MarshalECPrivateKey(key)
	cert, keyf := filepath.Join(dir, "c.pem"), filepath.Join(dir, "k.pem")
	_ = os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	_ = os.WriteFile(keyf, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600)
	return cert, keyf
}

func TestTLSListenerServesAndReloads(t *testing.T) {
	dir := t.TempDir()
	cert, key := writeCert(t, dir, "one.example.test")
	h := httpsSettings{certFile: cert, keyFile: key}
	ln, err := h.tlsListener("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go func() {
		for {
			c, aerr := ln.Accept()
			if aerr != nil {
				return
			}
			go func() { _ = c.(*tls.Conn).Handshake(); _ = c.Close() }()
		}
	}()
	name := func() string {
		c, derr := tls.Dial("tcp", ln.Addr().String(), &tls.Config{InsecureSkipVerify: true}) //nolint:gosec // a test against a self-signed certificate
		if derr != nil {
			t.Fatal(derr)
		}
		defer func() { _ = c.Close() }()
		return c.ConnectionState().PeerCertificates[0].Subject.CommonName
	}
	if got := name(); got != "one.example.test" {
		t.Fatalf("served %q", got)
	}
	// A renewed certificate is picked up without a restart.
	r := &certReloader{cert: cert, key: key}
	if _, lerr := r.load(); lerr != nil {
		t.Fatal(lerr)
	}
	writeCert(t, dir, "two.example.test")
	later := time.Now().Add(time.Minute)
	_ = os.Chtimes(cert, later, later)
	r.mu.Lock()
	r.checked = time.Time{} // as if 30 s had passed
	r.mu.Unlock()
	c, err := r.get(nil)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(c.Certificate[0])
	if err != nil || leaf.Subject.CommonName != "two.example.test" {
		t.Fatalf("after renewal %v %v", leaf, err)
	}
	if _, err := (httpsSettings{certFile: filepath.Join(dir, "missing.pem"), keyFile: key}).tlsListener("127.0.0.1:0"); err == nil {
		t.Fatal("a missing certificate was accepted")
	}
}
