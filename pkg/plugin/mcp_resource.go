package plugin

import (
	"bytes"
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	mcpserver "github.com/mark3labs/mcp-go/server"
)

// handleMCP bridges Grafana's CallResource transport to mcp-go's
// StreamableHTTPServer. The agent connects to
//
//	POST /api/datasources/uid/<ds>/resources/mcp
//
// Grafana strips the prefix and delivers us a CallResourceRequest whose
// Path is "mcp" (and may have child paths for the spec's session endpoints).
// We synthesise an http.Request, fan it into mcp-go's handler, and turn the
// ResponseWriter writes back into CallResourceResponse Send() calls. Each
// Send flushes downstream — Grafana's CallResource path supports chunked
// streaming end-to-end (precedent: grafana-llm-plugin streams LLM tokens
// the same way).
func (d *Datasource) handleMCP(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	if d.mcpHTTP == nil {
		return jsonStatus(sender, http.StatusServiceUnavailable, map[string]string{
			"error": "MCP server is not initialised on this datasource instance.",
		})
	}

	// Rewrite the URL path so mcp-go's StreamableHTTPServer recognises it.
	// We mount it at "/mcp" via WithEndpointPath, so an incoming Path of
	// "mcp" / "/mcp" / "mcp/foo" maps to "/mcp" / "/mcp" / "/mcp/foo".
	trimmed := strings.TrimLeft(req.Path, "/")
	u := &url.URL{Path: "/" + trimmed}

	httpReq, err := http.NewRequestWithContext(ctx, req.Method, u.String(), bytes.NewReader(req.Body))
	if err != nil {
		return jsonStatus(sender, http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	httpReq.URL = u
	for k, vs := range req.Headers {
		for _, v := range vs {
			httpReq.Header.Add(k, v)
		}
	}
	if httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	w := &callResourceResponseWriter{sender: sender}
	d.mcpHTTP.ServeHTTP(w, httpReq)

	// mcp-go normally writes a response, but if a handler returns without
	// calling Write (rare, but possible), close out cleanly so the client
	// doesn't hang.
	if !w.headerSent {
		return jsonStatus(sender, http.StatusNoContent, nil)
	}
	return nil
}

// callResourceResponseWriter adapts a Grafana CallResourceResponseSender into
// the http.ResponseWriter + http.Flusher interfaces mcp-go expects.
//
// First Write call sends status + headers + body together (one Send). Subsequent
// Writes send body-only chunks. Flush is a no-op because each Send already
// flushes through Grafana's chunked-transfer pipe.
type callResourceResponseWriter struct {
	sender     backend.CallResourceResponseSender
	headers    http.Header
	status     int
	headerSent bool
}

var _ http.ResponseWriter = (*callResourceResponseWriter)(nil)
var _ http.Flusher = (*callResourceResponseWriter)(nil)

func (w *callResourceResponseWriter) Header() http.Header {
	if w.headers == nil {
		w.headers = http.Header{}
	}
	return w.headers
}

func (w *callResourceResponseWriter) WriteHeader(status int) {
	w.status = status
}

func (w *callResourceResponseWriter) Write(b []byte) (int, error) {
	if w.headerSent {
		body := append([]byte(nil), b...) // sender may retain the slice
		if err := w.sender.Send(&backend.CallResourceResponse{Body: body}); err != nil {
			return 0, err
		}
		return len(b), nil
	}
	status := w.status
	if status == 0 {
		status = http.StatusOK
	}
	resp := &backend.CallResourceResponse{
		Status:  status,
		Headers: w.headers,
		Body:    append([]byte(nil), b...),
	}
	if err := w.sender.Send(resp); err != nil {
		return 0, err
	}
	w.headerSent = true
	return len(b), nil
}

// Flush satisfies http.Flusher. Streaming through CallResource already flushes
// per Send (Grafana writes each gRPC chunk straight to the HTTP pipe), so
// there's nothing extra to do here — but mcp-go's chunked writes check that
// the writer implements this interface before opting into stream mode.
func (w *callResourceResponseWriter) Flush() {}

// mcpHTTPHandler is the narrow interface the Datasource holds for the MCP
// transport — keeping it abstract avoids leaking mcp-go's concrete type to
// the rest of the plugin and lets tests stub it.
type mcpHTTPHandler interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}

// Compile-time assertion: mcp-go's StreamableHTTPServer satisfies our interface.
var _ mcpHTTPHandler = (*mcpserver.StreamableHTTPServer)(nil)
