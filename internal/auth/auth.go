// Package auth signs the CLI in through the browser, the way `gcloud auth
// login` does: the OAuth device authorization flow (RFC 8628). The CLI shows
// a short code, the person approves it in the Plexivo console for one
// organisation and one access level, and the CLI receives a session that
// expires after the organisation's session length.
package auth

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"
)

// Device is the answer to POST /cli/device.
type Device struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// Session is what the CLI keeps once the sign-in is approved.
type Session struct {
	AccessToken  string    `json:"access_token"`
	ExpiresAt    time.Time `json:"expires_at"`
	Access       string    `json:"access"`
	SessionID    string    `json:"session_id"`
	TenantID     string    `json:"tenant_id"`
	Organisation struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"organisation"`
}

// Errors the console can end a sign-in with.
var (
	ErrDenied  = errors.New("the sign-in was denied in the console")
	ErrExpired = errors.New("the code expired before it was approved; run plexivo auth login again")
)

// Start asks the API for a code.
func Start(client *http.Client, baseURL string) (*Device, error) {
	host, _ := os.Hostname()
	var d Device
	err := post(client, baseURL+"/api/v1/cli/device", map[string]string{
		"client_name": host,
		"client_os":   runtime.GOOS + "/" + runtime.GOARCH,
	}, &d, nil)
	if err != nil {
		return nil, err
	}
	if d.Interval <= 0 {
		d.Interval = 5
	}
	return &d, nil
}

// Wait polls until the code is approved, denied or expired. sleep is
// injectable so tests do not wait.
func Wait(client *http.Client, baseURL string, d *Device, sleep func(time.Duration)) (*Session, error) {
	interval := time.Duration(d.Interval) * time.Second
	deadline := time.Now().Add(time.Duration(d.ExpiresIn) * time.Second)
	for {
		sleep(interval)
		var s Session
		var oauthErr struct {
			Error string `json:"error"`
		}
		err := post(client, baseURL+"/api/v1/cli/token", map[string]string{"device_code": d.DeviceCode}, &s, &oauthErr)
		switch {
		case err == nil:
			return &s, nil
		case oauthErr.Error == "authorization_pending":
		case oauthErr.Error == "slow_down":
			interval += 5 * time.Second
		case oauthErr.Error == "access_denied":
			return nil, ErrDenied
		case oauthErr.Error == "expired_token":
			return nil, ErrExpired
		default:
			return nil, err
		}
		if d.ExpiresIn > 0 && time.Now().After(deadline) {
			return nil, ErrExpired
		}
	}
}

// Revoke ends a session on the server (plexivo auth logout). Best effort.
func Revoke(client *http.Client, baseURL, token string) error {
	return post(client, baseURL+"/api/v1/cli/revoke", map[string]string{"token": token}, nil, nil)
}

// OpenBrowser opens the approval page; false when no browser could be opened.
func OpenBrowser(url string) bool {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		if os.Getenv("DISPLAY") == "" && os.Getenv("WAYLAND_DISPLAY") == "" {
			return false
		}
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start() == nil
}

// post sends JSON; on a 4xx it decodes the body into oauthErr (when given)
// and returns an error.
func post(client *http.Client, url string, body, out, oauthErr any) error {
	b, _ := json.Marshal(body)
	req, err := http.NewRequest("POST", url, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "plexivo-cli")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		if oauthErr != nil {
			_ = json.Unmarshal(raw, oauthErr)
		}
		return fmt.Errorf("%d %s", resp.StatusCode, bytes.TrimSpace(raw))
	}
	if out != nil {
		return json.Unmarshal(raw, out)
	}
	return nil
}
