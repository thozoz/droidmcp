package mcp

import (
	"context"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// In stateless mode the SDK does not tie a tool's context to its HTTP request,
// and it cannot match a client's cancel notification to the running call. So
// a client that gives up (or drops the connection) would leave a long tool such
// as wait_for polling the phone. trackRequests tags each /mcp request so a
// tool can find its request's context and stop with it.

const callHeader = "X-Droidmcp-Call"

var (
	reqCtxs sync.Map // call id -> request context
	reqSeq  atomic.Uint64
)

func trackRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := strconv.FormatUint(reqSeq.Add(1), 10)
		reqCtxs.Store(id, r.Context())
		defer reqCtxs.Delete(id)
		r.Header.Set(callHeader, id) // overrides any client-sent value
		next.ServeHTTP(w, r)
	})
}

// withRequest returns ctx, also cancelled when the HTTP request that carried
// req ends.
func withRequest(ctx context.Context, req *sdk.CallToolRequest) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(ctx)
	if req == nil || req.Extra == nil || req.Extra.Header == nil {
		return ctx, cancel
	}
	v, ok := reqCtxs.Load(req.Extra.Header.Get(callHeader))
	if !ok {
		return ctx, cancel
	}
	stop := context.AfterFunc(v.(context.Context), cancel)
	return ctx, func() { stop(); cancel() }
}
