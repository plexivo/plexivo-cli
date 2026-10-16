package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// server answers /cli/token with the given replies in order, then success.
func server(t *testing.T, replies ...string) (*httptest.Server, *int32) {
	var calls int32
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/cli/device":
			_ = json.NewEncoder(w).Encode(Device{DeviceCode: "dc", UserCode: "BCDF-GHJK", VerificationURIComplete: "https://cloud.example/cli?code=BCDF-GHJK", ExpiresIn: 600, Interval: 5})
		case "/api/v1/cli/token":
			n := int(atomic.AddInt32(&calls, 1)) - 1
			if n < len(replies) {
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]string{"error": replies[n]})
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"plx_live_x","expires_at":"2030-01-01T08:00:00Z","access":"operate","organisation":{"id":"t1","name":"Acme"}}`))
		}
	})), &calls
}

func TestSignInWaitsSlowsDownAndCollectsTheSession(t *testing.T) {
	srv, calls := server(t, "authorization_pending", "slow_down", "authorization_pending")
	defer srv.Close()
	d, err := Start(srv.Client(), srv.URL)
	if err != nil || d.UserCode != "BCDF-GHJK" {
		t.Fatalf("start: %v %+v", err, d)
	}
	var waits []time.Duration
	s, err := Wait(srv.Client(), srv.URL, d, func(d time.Duration) { waits = append(waits, d) })
	if err != nil {
		t.Fatal(err)
	}
	if s.AccessToken != "plx_live_x" || s.Access != "operate" || s.Organisation.Name != "Acme" {
		t.Fatalf("session = %+v", s)
	}
	if *calls != 4 {
		t.Fatalf("polled %d times", *calls)
	}
	// slow_down adds five seconds to every later poll.
	if waits[0] != 5*time.Second || waits[2] != 10*time.Second || waits[3] != 10*time.Second {
		t.Fatalf("waits = %v", waits)
	}
}

func TestDeniedAndExpiredEndTheSignIn(t *testing.T) {
	for reply, want := range map[string]error{"access_denied": ErrDenied, "expired_token": ErrExpired} {
		srv, _ := server(t, reply)
		d, _ := Start(srv.Client(), srv.URL)
		if _, err := Wait(srv.Client(), srv.URL, d, func(time.Duration) {}); err != want {
			t.Errorf("%s: err = %v", reply, err)
		}
		srv.Close()
	}
}
