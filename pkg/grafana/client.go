// Package grafana is a thin REST client for the subset of Grafana's HTTP API
// the plugin's MCP tools call: list dashboards, read/write a dashboard, look
// up the SLayer datasource. The in-plugin MCP server (pkg/plugin via
// CallResource) constructs the Config from datasource settings.
package grafana

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Config holds the connection parameters. Auth is optional — when neither
// Token nor BasicUser is set, the client sends unauthenticated requests, which
// work against Grafana installs that enable anonymous-Admin auth (i.e. the
// bundled demo).
type Config struct {
	BaseURL    string
	Token      string // Bearer (Grafana service account); preferred when set
	BasicUser  string // basic-auth fallback
	BasicPass  string
	PluginID   string // motley-slayer-datasource by default
	HTTPClient *http.Client
}

// Client is the REST handle to Grafana. Safe for concurrent use; underlying
// http.Client owns its own connection pool.
type Client struct {
	cfg Config
}

// New constructs a Client. Applies defaults to BaseURL, PluginID, and
// HTTPClient when the caller leaves them blank.
func New(cfg Config) *Client {
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if cfg.PluginID == "" {
		cfg.PluginID = "motley-slayer-datasource"
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:3000"
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	return &Client{cfg: cfg}
}

// PluginID returns the datasource type the client filters for in auto-detect.
func (c *Client) PluginID() string { return c.cfg.PluginID }

func (c *Client) do(ctx context.Context, method, path string, body any) ([]byte, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.cfg.BaseURL+path, reader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	switch {
	case c.cfg.Token != "":
		req.Header.Set("Authorization", "Bearer "+c.cfg.Token)
	case c.cfg.BasicUser != "":
		req.SetBasicAuth(c.cfg.BasicUser, c.cfg.BasicPass)
		// else: no auth header — works against Grafana installs with
		// anonymous-Admin auth enabled (the bundled demo); a real install
		// without one of the three knobs will get 401 on the first call.
	}
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call grafana: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("grafana %s %s: HTTP %d: %s", method, path, resp.StatusCode, truncate(string(raw), 512))
	}
	return raw, nil
}

// DashboardSummary is the minimal subset of GET /api/search results.
type DashboardSummary struct {
	UID         string   `json:"uid"`
	Title       string   `json:"title"`
	URL         string   `json:"url"`
	FolderTitle string   `json:"folderTitle,omitempty"`
	Tags        []string `json:"tags,omitempty"`
}

// ListDashboards returns matching dashboards (limit 200). Empty query lists all.
func (c *Client) ListDashboards(ctx context.Context, query string) ([]DashboardSummary, error) {
	path := "/api/search?type=dash-db&limit=200"
	if query != "" {
		path += "&query=" + url.QueryEscape(query)
	}
	raw, err := c.do(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var out []DashboardSummary
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode /api/search: %w", err)
	}
	return out, nil
}

// DashboardWithMeta is the GET /api/dashboards/uid/:uid shape.
type DashboardWithMeta struct {
	Dashboard map[string]any `json:"dashboard"`
	Meta      map[string]any `json:"meta"`
}

// GetDashboard fetches a single dashboard by uid.
func (c *Client) GetDashboard(ctx context.Context, uid string) (*DashboardWithMeta, error) {
	raw, err := c.do(ctx, http.MethodGet, "/api/dashboards/uid/"+url.PathEscape(uid), nil)
	if err != nil {
		return nil, err
	}
	var out DashboardWithMeta
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode dashboard: %w", err)
	}
	return &out, nil
}

// SaveDashboardResult mirrors POST /api/dashboards/db's response.
type SaveDashboardResult struct {
	Status  string `json:"status"`
	UID     string `json:"uid"`
	URL     string `json:"url"`
	ID      int64  `json:"id"`
	Version int    `json:"version"`
}

// SaveDashboard PUTs a mutated dashboard back. Always overwrite=true because
// the version we read may have been bumped by another writer — the cautious
// alternative (refuse on version mismatch) is too surprising for agent flows.
func (c *Client) SaveDashboard(ctx context.Context, dashboard map[string]any, message string) (*SaveDashboardResult, error) {
	body := map[string]any{
		"dashboard": dashboard,
		"overwrite": true,
		"message":   message,
	}
	raw, err := c.do(ctx, http.MethodPost, "/api/dashboards/db", body)
	if err != nil {
		return nil, err
	}
	var out SaveDashboardResult
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode save response: %w", err)
	}
	return &out, nil
}

// DataSourceSummary is the per-datasource shape from GET /api/datasources.
type DataSourceSummary struct {
	UID       string `json:"uid"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	URL       string `json:"url"`
	IsDefault bool   `json:"isDefault"`
}

// FindSlayerDatasource picks a SLayer datasource by uid, by name, or — when
// both are empty — the first SLayer-typed datasource Grafana has registered.
// Used to fill in `targets[].datasource` on agent-created panels.
func (c *Client) FindSlayerDatasource(ctx context.Context, uid, name string) (*DataSourceSummary, error) {
	raw, err := c.do(ctx, http.MethodGet, "/api/datasources", nil)
	if err != nil {
		return nil, err
	}
	var all []DataSourceSummary
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, fmt.Errorf("decode /api/datasources: %w", err)
	}
	for _, d := range all {
		if uid != "" && d.UID == uid {
			return &d, nil
		}
		if name != "" && d.Name == name {
			return &d, nil
		}
	}
	if uid != "" || name != "" {
		return nil, fmt.Errorf("no datasource found matching uid=%q name=%q", uid, name)
	}
	for _, d := range all {
		if d.Type == c.cfg.PluginID {
			return &d, nil
		}
	}
	return nil, fmt.Errorf("no datasource of type %q is registered", c.cfg.PluginID)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
