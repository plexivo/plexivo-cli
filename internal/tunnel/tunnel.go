// Package tunnel is the laptop end of `plexivo db connect`: a local port
// whose every connection is carried over TLS to the database's cell and
// joined to the database there.
//
// The cell's node is accepted only when its certificate's SHA-256 is one the
// Brain listed for that cell; no certificate authority is trusted. Each local
// connection opens its own tunnel connection, so a database tool that opens
// several works as it would against the database itself.
package tunnel

import (
	"bufio"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/plexivo/plexivo-cli/internal/api"
)

// Source gives a current grant, renewing it when it is about to expire.
type Source struct {
	mu    sync.Mutex
	grant *api.Tunnel
	renew func() (*api.Tunnel, error)
}

// NewSource starts from a grant and renews with fn.
func NewSource(first *api.Tunnel, fn func() (*api.Tunnel, error)) *Source {
	return &Source{grant: first, renew: fn}
}

func (s *Source) current() (*api.Tunnel, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if time.Until(time.Unix(s.grant.ExpiresAt, 0)) < 2*time.Minute {
		g, err := s.renew()
		if err != nil {
			return nil, err
		}
		s.grant = g
	}
	return s.grant, nil
}

// Dial opens one tunnel connection to the database.
func Dial(g *api.Tunnel) (net.Conn, error) {
	allowed := map[string]bool{}
	for _, f := range g.Fingerprints {
		allowed[strings.ToLower(f)] = true
	}
	cfg := &tls.Config{
		MinVersion: tls.VersionTLS12,
		// The pinned fingerprints are the whole check: the node's certificate
		// is its own, not from a public authority.
		InsecureSkipVerify: true,
		VerifyPeerCertificate: func(raw [][]byte, _ [][]*x509.Certificate) error {
			if len(raw) == 0 {
				return errors.New("tunnel: the cell presented no certificate")
			}
			sum := sha256.Sum256(raw[0])
			if !allowed[hex.EncodeToString(sum[:])] {
				return errors.New("tunnel: the cell's certificate is not one Plexivo listed for it; refusing")
			}
			return nil
		},
	}
	d := &net.Dialer{Timeout: 10 * time.Second}
	c, err := tls.DialWithDialer(d, "tcp", fmt.Sprintf("%s:%d", g.Host, g.Port), cfg)
	if err != nil {
		return nil, err
	}
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	if _, err := io.WriteString(c, "PLEXIVO-TUNNEL/1 "+g.Token+"\n"); err != nil {
		c.Close()
		return nil, err
	}
	line, err := bufio.NewReaderSize(c, 64).ReadString('\n')
	if err != nil {
		c.Close()
		return nil, err
	}
	if strings.TrimSpace(line) != "OK" {
		c.Close()
		return nil, fmt.Errorf("tunnel: %s", strings.TrimSpace(strings.TrimPrefix(line, "DENIED")))
	}
	_ = c.SetDeadline(time.Time{})
	return c, nil
}

// Serve accepts local connections on ln and carries each through a tunnel.
func Serve(ln net.Listener, src *Source, logf func(string, ...any)) error {
	for {
		local, err := ln.Accept()
		if err != nil {
			return err
		}
		go func() {
			defer local.Close()
			g, err := src.current()
			if err != nil {
				logf("could not renew the tunnel: %v", err)
				return
			}
			remote, err := Dial(g)
			if err != nil {
				logf("%v", err)
				return
			}
			defer remote.Close()
			logf("connection opened")
			done := make(chan struct{}, 2)
			go func() { _, _ = io.Copy(remote, local); done <- struct{}{} }()
			go func() { _, _ = io.Copy(local, remote); done <- struct{}{} }()
			<-done
			logf("connection closed")
		}()
	}
}
