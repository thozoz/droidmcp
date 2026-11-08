// Package auth implements the bearer-token check that guards every daemon endpoint.
package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"
	"sync/atomic"
)

// NewToken returns 32 random bytes, hex encoded.
func NewToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Token holds the current bearer token; it can be rotated while serving.
type Token struct{ v atomic.Pointer[string] }

func NewTokenValue(s string) *Token {
	t := &Token{}
	t.Set(s)
	return t
}

func (t *Token) Get() string  { return *t.v.Load() }
func (t *Token) Set(s string) { t.v.Store(&s) }

// Middleware rejects any request whose Authorization header is not
// "Bearer <current token>". The comparison is constant-time.
func Middleware(token *Token, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		want := []byte(token.Get())
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || len(want) == 0 || subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			w.Header().Set("WWW-Authenticate", `Bearer realm="droidmcp"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
