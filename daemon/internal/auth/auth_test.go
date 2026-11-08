package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMiddleware(t *testing.T) {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })

	tests := []struct {
		name   string
		token  string // server token
		header string
		want   int
	}{
		{"missing header", "secret", "", http.StatusUnauthorized},
		{"wrong token", "secret", "Bearer nope", http.StatusUnauthorized},
		{"prefix of token", "secret", "Bearer secre", http.StatusUnauthorized},
		{"wrong scheme", "secret", "Basic secret", http.StatusUnauthorized},
		{"lowercase scheme", "secret", "bearer secret", http.StatusUnauthorized},
		{"no space", "secret", "Bearersecret", http.StatusUnauthorized},
		{"empty server token never matches", "", "Bearer ", http.StatusUnauthorized},
		{"correct token", "secret", "Bearer secret", http.StatusTeapot},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			Middleware(NewTokenValue(tt.token), ok).ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status = %d, want %d", rec.Code, tt.want)
			}
		})
	}
}

func TestNewToken(t *testing.T) {
	a, err := NewToken()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := NewToken()
	if len(a) != 64 || a == b {
		t.Fatalf("bad tokens %q %q", a, b)
	}
}
