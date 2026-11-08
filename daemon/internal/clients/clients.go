// Package clients tracks who is using the daemon, so the phone can show it:
// a client is a source IP that made an authenticated request recently. Over
// adb forward every client is 127.0.0.1; over LAN each device has its own IP,
// so a stolen token shows up as an extra client.
package clients

import (
	"context"
	"net"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

type Client struct {
	Addr     string    `json:"addr"`
	Via      string    `json:"via"` // "adb" for loopback, else "lan"
	First    time.Time `json:"first"`
	Last     time.Time `json:"last"`
	Requests int       `json:"requests"`
}

// Tracker counts clients active within window and reports count changes.
// onChange runs on the request path; keep it non-blocking.
type Tracker struct {
	window   time.Duration
	onChange func(active int)
	now      func() time.Time

	mu       sync.Mutex
	seen     map[string]*Client
	reported int
}

func New(window time.Duration, onChange func(active int)) *Tracker {
	return &Tracker{window: window, onChange: onChange, now: time.Now, seen: map[string]*Client{}}
}

// Middleware records the request's source. Mount it behind authentication.
func (t *Tracker) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.record(r.RemoteAddr)
		next.ServeHTTP(w, r)
	})
}

func (t *Tracker) record(remote string) {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	now := t.now()
	t.mu.Lock()
	c := t.seen[host]
	if c == nil || now.Sub(c.Last) > t.window {
		via := "lan"
		if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
			via = "adb"
		}
		c = &Client{Addr: host, Via: via, First: now}
		t.seen[host] = c
	}
	c.Last = now
	c.Requests++
	t.mu.Unlock()
	t.report()
}

// Active returns clients seen within the window, most recent first, and
// forgets the rest.
func (t *Tracker) Active() []Client {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := t.now()
	var out []Client
	for k, c := range t.seen {
		if now.Sub(c.Last) > t.window {
			delete(t.seen, k)
			continue
		}
		out = append(out, *c)
	}
	slices.SortFunc(out, func(a, b Client) int {
		if c := b.Last.Compare(a.Last); c != 0 {
			return c
		}
		return strings.Compare(a.Addr, b.Addr)
	})
	return out
}

// report calls onChange when the number of active clients changed.
func (t *Tracker) report() {
	n := len(t.Active())
	t.mu.Lock()
	changed := n != t.reported
	t.reported = n
	t.mu.Unlock()
	if changed && t.onChange != nil {
		t.onChange(n)
	}
}

// Run expires idle clients so the count drops without new traffic.
func (t *Tracker) Run(ctx context.Context) {
	tick := time.NewTicker(t.window / 4)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			t.report()
		}
	}
}
