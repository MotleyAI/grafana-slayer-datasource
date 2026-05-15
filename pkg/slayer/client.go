// Package slayer is a thin HTTP client for SLayer's REST API
// (https://motley-slayer.readthedocs.io/en/latest/reference/rest-api/).
package slayer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client talks to a SLayer server.
type Client struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// NewClient returns a Client with a sensible default HTTP timeout.
// APIKey is forward-compat — SLayer ≤0.6.x has no auth; the Authorization
// header is only emitted when a non-empty key is supplied.
func NewClient(baseURL, apiKey string) *Client {
	return &Client{
		BaseURL:    baseURL,
		APIKey:     apiKey,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
	}
}

// Query mirrors SLayer's QueryRequest. Field shapes match SlayerQuery v3.
// Measures / Dimensions / TimeDimensions / Order accept dict-shaped entries
// (string entries like "*:count" are also accepted by SLayer's pre-validators,
// but Go callers should prefer dicts).
//
// SourceQueries is the multi-stage / queries-as-models construct
// (https://motley-slayer.readthedocs.io/en/latest/examples/06_multistage_queries/).
// It's kept as json.RawMessage so the recursive nested-query shape passes
// through verbatim without our struct dictating the inner field set — useful
// for forward-compat with SlayerQuery additions and saves us a recursive type.
type Query struct {
	Name           string                   `json:"name,omitempty"`
	SourceModel    string                   `json:"source_model,omitempty"`
	SourceQueries  json.RawMessage          `json:"source_queries,omitempty"`
	Measures       []map[string]interface{} `json:"measures,omitempty"`
	Dimensions     []map[string]interface{} `json:"dimensions,omitempty"`
	TimeDimensions []map[string]interface{} `json:"time_dimensions,omitempty"`
	Filters        []string                 `json:"filters,omitempty"`
	Order          []map[string]interface{} `json:"order,omitempty"`
	Limit          *int                     `json:"limit,omitempty"`
	Offset         *int                     `json:"offset,omitempty"`
	Variables      map[string]interface{}   `json:"variables,omitempty"`
}

// NumberFormat mirrors SLayer's NumberFormat model. All fields optional —
// SLayer sets `type` (e.g. "integer", "float", "currency", "percent") and
// optionally `precision`/`symbol`. Mapping to Grafana units is a follow-up.
type NumberFormat struct {
	Type      string  `json:"type,omitempty"`
	Precision *int    `json:"precision,omitempty"`
	Symbol    *string `json:"symbol,omitempty"`
}

// FieldMetadata is per-column display metadata returned in QueryResponse.attributes.
type FieldMetadata struct {
	Label  string        `json:"label,omitempty"`
	Format *NumberFormat `json:"format,omitempty"`
}

// Attributes carries dimension and measure metadata, keyed by column alias.
type Attributes struct {
	Dimensions map[string]FieldMetadata `json:"dimensions"`
	Measures   map[string]FieldMetadata `json:"measures"`
}

// Response mirrors SLayer's QueryResponse.
type Response struct {
	Data       []map[string]interface{} `json:"data"`
	RowCount   int                      `json:"row_count"`
	Columns    []string                 `json:"columns"`
	SQL        string                   `json:"sql,omitempty"`
	Attributes *Attributes              `json:"attributes,omitempty"`
}

// Query runs a SLayer query against POST /query.
//
// Wire shape: SLayer's REST accepts `Union[QueryRequest, List[QueryRequest]]`.
// We send the list form when `q.SourceQueries` is non-empty (multi-stage) and
// the single-object form otherwise — see buildQueryBody for the encoding
// details.
func (c *Client) Query(ctx context.Context, q Query) (*Response, error) {
	body, err := buildQueryBody(q)
	if err != nil {
		return nil, fmt.Errorf("marshal query: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL+"/query", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call slayer: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("slayer %d: %s", resp.StatusCode, string(raw))
	}
	var out Response
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode response: %w (body: %s)", err, truncate(string(raw), 512))
	}
	return &out, nil
}

// ModelInfo is the shape returned by GET /models — name + datasource.
type ModelInfo struct {
	Name       string `json:"name"`
	DataSource string `json:"data_source"`
}

// Models lists all SLayer models for dropdown autocomplete in the QueryEditor.
func (c *Client) Models(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call slayer: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("slayer /models %d: %s", resp.StatusCode, truncate(string(raw), 256))
	}
	var out []ModelInfo
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, fmt.Errorf("decode /models: %w", err)
	}
	return out, nil
}

// Health pings GET /health. Returns nil on 200.
func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/health", nil)
	if err != nil {
		return err
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("call slayer: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("slayer health %d", resp.StatusCode)
	}
	return nil
}

// buildQueryBody encodes a Query for SLayer's POST /query. SLayer ≥0.6.7
// accepts `Union[QueryRequest, QueryListRequest]`:
//
//   - Without source_queries — a single QueryRequest object (legacy shape).
//   - With source_queries — a QueryListRequest wrapper:
//     `{"queries": [stage_1, stage_2, …, outer_without_source_queries]}`.
//     Earlier entries are intermediate stages (each carrying its own `name`)
//     and the outer query (with its `source_queries` field cleared to avoid
//     recursion) is appended last. The outer query's `source_model` can
//     reference any prior stage's `name`.
//
// `SourceQueries` is a json.RawMessage so the recursive nested-query shape
// passes through verbatim — we don't need to type the inner fields, and SLayer
// stays the only validator of the inner SlayerQuery schema.
func buildQueryBody(q Query) ([]byte, error) {
	if len(q.SourceQueries) == 0 {
		return json.Marshal(q)
	}
	var stages []json.RawMessage
	if err := json.Unmarshal(q.SourceQueries, &stages); err != nil {
		return nil, fmt.Errorf("source_queries: %w", err)
	}
	outer := q
	outer.SourceQueries = nil
	outerBytes, err := json.Marshal(outer)
	if err != nil {
		return nil, fmt.Errorf("outer query: %w", err)
	}
	list := make([]json.RawMessage, 0, len(stages)+1)
	list = append(list, stages...)
	list = append(list, outerBytes)
	return json.Marshal(struct {
		Queries []json.RawMessage `json:"queries"`
	}{Queries: list})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
