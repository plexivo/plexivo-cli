package tunnel

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"io"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/plexivo/plexivo-cli/internal/api"
)

// A stand-in for the cell's node: answers OK to "good" and echoes.
func node(t *testing.T) (addr, fingerprint string) {
	t.Helper()
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "node"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, _ := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	sum := sha256.Sum256(der)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				r := bufio.NewReader(c)
				line, _ := r.ReadString('\n')
				if strings.TrimSpace(line) != "PLEXIVO-TUNNEL/1 good" {
					_, _ = io.WriteString(c, "DENIED token unknown or expired\n")
					return
				}
				_, _ = io.WriteString(c, "OK\n")
				_, _ = io.Copy(c, r)
			}()
		}
	}()
	return ln.Addr().String(), hex.EncodeToString(sum[:])
}

func grant(addr, token string, fps ...string) *api.Tunnel {
	host, port, _ := net.SplitHostPort(addr)
	var p int
	for _, ch := range port {
		p = p*10 + int(ch-'0')
	}
	return &api.Tunnel{Host: host, Port: p, Token: token, Fingerprints: fps, ExpiresAt: time.Now().Add(time.Hour).Unix()}
}

func TestDialTrustsOnlyListedFingerprints(t *testing.T) {
	addr, fp := node(t)

	if _, err := Dial(grant(addr, "good", strings.Repeat("0", 64))); err == nil {
		t.Fatal("connected to a node whose certificate was not listed")
	}
	if _, err := Dial(grant(addr, "bad", fp)); err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("a refused token should say so, got %v", err)
	}
	c, err := Dial(grant(addr, "good", fp))
	if err != nil {
		t.Fatalf("the listed node with a good token was refused: %v", err)
	}
	defer c.Close()
	_, _ = io.WriteString(c, "ping\n")
	if got, _ := bufio.NewReader(c).ReadString('\n'); got != "ping\n" {
		t.Fatalf("bytes did not pass: %q", got)
	}
}
