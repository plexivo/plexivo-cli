// Package api is the CLI's client for the Plexivo Platform API.
package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultURL is the Platform API; PLEXIVO_API_URL overrides it.
const DefaultURL = "https://api.plexivo.io"

// Client calls the API with one token: a browser sign-in session or an API key.
type Client struct {
	URL     string
	Token   string
	HTTP    *http.Client
	Session bool
}

// Config is what `plexivo auth login` stores (~/.config/plexivo/config.json,
// 0600). A browser sign-in is a session with an expiry, an organisation and an
// access level; an API key (for CI) has none of them.
type Config struct {
	Token        string    `json:"token"`
	URL          string    `json:"url,omitempty"`
	Kind         string    `json:"kind,omitempty"` // "session" or "api_key"
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
	Organisation string    `json:"organisation,omitempty"`
	Access       string    `json:"access,omitempty"`
}

// ErrSessionExpired is what every command says once a sign-in has lapsed.
var ErrSessionExpired = errors.New("your Plexivo session has expired: run `plexivo auth login` to sign in again")

// Read returns the stored login, without the environment overrides.
func Read() (Config, error) {
	var cfg Config
	b, err := os.ReadFile(ConfigPath())
	if err != nil {
		return cfg, err
	}
	return cfg, json.Unmarshal(b, &cfg)
}

// ConfigPath is where the CLI keeps its login.
func ConfigPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = filepath.Join(os.Getenv("HOME"), ".config")
	}
	return filepath.Join(dir, "plexivo", "config.json")
}

// Load reads the login, preferring PLEXIVO_TOKEN and PLEXIVO_API_URL.
func Load() (*Client, error) {
	var cfg Config
	if b, err := os.ReadFile(ConfigPath()); err == nil {
		_ = json.Unmarshal(b, &cfg)
	}
	if t := os.Getenv("PLEXIVO_TOKEN"); t != "" {
		cfg.Token = t
	}
	if u := os.Getenv("PLEXIVO_API_URL"); u != "" {
		cfg.URL = u
	}
	if cfg.URL == "" {
		cfg.URL = DefaultURL
	}
	if cfg.Token == "" {
		return nil, errors.New("not signed in: run `plexivo auth login` (or set PLEXIVO_TOKEN for CI)")
	}
	session := cfg.Kind == "session" && os.Getenv("PLEXIVO_TOKEN") == ""
	if session && !cfg.ExpiresAt.IsZero() && time.Now().After(cfg.ExpiresAt) {
		return nil, ErrSessionExpired
	}
	return &Client{URL: strings.TrimSuffix(cfg.URL, "/"), Token: cfg.Token, HTTP: &http.Client{Timeout: 30 * time.Second}, Session: session}, nil
}

// Save stores a login with owner-only permissions.
func Save(cfg Config) error {
	p := ConfigPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(p, b, 0o600)
}

// Do sends a request and decodes the JSON answer into out (when not nil).
func (c *Client) Do(method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.URL+"/api/v1"+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "plexivo-cli")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode == http.StatusUnauthorized && c.Session {
		return ErrSessionExpired
	}
	if resp.StatusCode >= 300 {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(raw, &e)
		msg := e.Error.Message
		if msg == "" {
			msg = e.Message
		}
		if msg == "" {
			msg = strings.TrimSpace(string(raw))
		}
		return fmt.Errorf("%s %s: %d %s", method, path, resp.StatusCode, msg)
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// Tunnel is a grant from POST /deployments/:id/tunnel.
type Tunnel struct {
	Host         string   `json:"host"`
	Port         int      `json:"port"`
	Token        string   `json:"token"`
	ExpiresAt    int64    `json:"expires_at"`
	Fingerprints []string `json:"fingerprints"`
	Engine       string   `json:"engine"`
	LocalPort    int      `json:"local_port"`
}

// NewTunnel asks the Brain for a tunnel to a database or cache.
func (c *Client) NewTunnel(deploymentID string) (*Tunnel, error) {
	var out struct {
		Tunnel Tunnel `json:"tunnel"`
	}
	if err := c.Do("POST", "/deployments/"+deploymentID+"/tunnel", map[string]any{}, &out); err != nil {
		return nil, err
	}
	return &out.Tunnel, nil
}
