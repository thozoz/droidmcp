// Command droidmcp-bridge runs on the computer as a stdio MCP server and
// forwards every call to the daemon on the phone. It owns the adb plumbing:
// it waits for the device, (re)creates the port forward and reads the
// daemon's token over adb, so the MCP client keeps one stable connection
// across cable unplugs and daemon restarts.
//
//	claude mcp add android -- droidmcp-bridge [-serial X] [-port 8765]
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var version = "dev"

func main() {
	port := flag.Int("port", 8765, "daemon port on the phone (forwarded to the same local port)")
	adbPath := flag.String("adb", "adb", "adb executable")
	serial := flag.String("serial", "", "device serial (adb -s), when several are attached")
	token := flag.String("token", "", "fixed daemon token; by default it is read from the phone over adb")
	tokenFile := flag.String("token-file", "/data/local/tmp/droidmcp.token", "token path on the phone")
	flag.Parse()
	log.SetOutput(os.Stderr) // stdout carries the MCP protocol
	log.SetPrefix("droidmcp-bridge: ")

	adb := func(ctx context.Context, args ...string) (string, error) {
		if *serial != "" {
			args = append([]string{"-s", *serial}, args...)
		}
		out, err := exec.CommandContext(ctx, *adbPath, args...).CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("adb %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
		return string(out), nil
	}
	fwd := fmt.Sprintf("tcp:%d", *port)
	up := &upstream{
		url: fmt.Sprintf("http://127.0.0.1:%d/mcp", *port),
		prepare: func(ctx context.Context) error {
			ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			defer cancel()
			if _, err := adb(ctx, "wait-for-device"); err != nil {
				return fmt.Errorf("no phone connected: %w", err)
			}
			_, err := adb(ctx, "forward", fwd, fwd)
			return err
		},
		token: func(ctx context.Context) (string, error) {
			if *token != "" {
				return *token, nil
			}
			out, err := adb(ctx, "shell", "cat", *tokenFile)
			if t := strings.TrimSpace(out); err == nil && t != "" && !strings.Contains(t, " ") {
				return t, nil
			}
			return "", fmt.Errorf("daemon not running? start it with start.sh (%v)", err)
		},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	srv := sdk.NewServer(&sdk.Implementation{Name: "droidmcp", Version: version}, nil)

	// Fetch tools synchronously if phone is already reachable so client gets them on initial tools/list.
	initCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	tools, err := up.tools(initCtx)
	cancel()
	if err == nil {
		for _, t := range tools {
			srv.AddTool(t, forward(up, t))
		}
		log.Printf("connected; %d tools", len(tools))
	} else {
		go syncTools(ctx, srv, up)
	}

	if err := srv.Run(ctx, &sdk.StdioTransport{}); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}

// syncTools mirrors the daemon's tools once it is reachable. Until then the
// server has no tools; adding them later sends tools/list_changed.
func syncTools(ctx context.Context, srv *sdk.Server, up *upstream) {
	for {
		tctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		tools, err := up.tools(tctx)
		cancel()
		if err == nil {
			for _, t := range tools {
				srv.AddTool(t, forward(up, t))
			}
			log.Printf("connected; %d tools", len(tools))
			return
		}
		log.Printf("waiting for the phone: %v", err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

func forward(up *upstream, t *sdk.Tool) sdk.ToolHandler {
	readOnly := t.Annotations != nil && t.Annotations.ReadOnlyHint
	return func(ctx context.Context, req *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		res, err := up.call(ctx, t.Name, req.Params.Arguments, readOnly)
		if err != nil {
			// A tool error, not a protocol error, so the model sees it and can retry.
			return &sdk.CallToolResult{IsError: true, Content: []sdk.Content{&sdk.TextContent{Text: err.Error()}}}, nil
		}
		return res, nil
	}
}
