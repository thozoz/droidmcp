package clients

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTrackerCountsAndExpires(t *testing.T) {
	now := time.Unix(1000, 0)
	var changes []int
	tr := New(2*time.Minute, func(n int) { changes = append(changes, n) })
	tr.now = func() time.Time { return now }
	h := tr.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	hit := func(remote string) {
		r := httptest.NewRequest(http.MethodPost, "/mcp", nil)
		r.RemoteAddr = remote
		h.ServeHTTP(httptest.NewRecorder(), r)
	}

	hit("127.0.0.1:50000")
	hit("127.0.0.1:50001") // same client, new source port
	hit("192.168.1.23:4000")
	act := tr.Active()
	if len(act) != 2 {
		t.Fatalf("active = %+v, want 2 clients", act)
	}
	if act[0].Addr != "127.0.0.1" || act[0].Via != "adb" || act[0].Requests != 2 || act[1].Via != "lan" {
		t.Errorf("active = %+v", act)
	}

	now = now.Add(90 * time.Second)
	hit("[::1]:1")                  // keeps loopback family separate but still adb
	now = now.Add(60 * time.Second) // 150 s after the LAN client's last request
	tr.report()
	act = tr.Active()
	if len(act) != 1 || act[0].Addr != "::1" || act[0].Via != "adb" {
		t.Fatalf("after expiry = %+v", act)
	}
	if want := []int{1, 2, 3, 1}; !equal(changes, want) {
		t.Errorf("changes = %v, want %v", changes, want)
	}
}

func equal(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
