package grafana

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeGrafana spins up an httptest server stubbing the three Grafana
// endpoints we hit. Each test pre-populates the routes it cares about.
type fakeGrafana struct {
	t              *testing.T
	server         *httptest.Server
	wantAuth       string // Authorization header value the test expects (Bearer ...)
	wantBasicAuth  bool
	searchResults  []DashboardSummary
	dashboards     map[string]map[string]any
	saveCallBody   map[string]any
	saveResponse   SaveDashboardResult
	datasources    []DataSourceSummary
}

func newFakeGrafana(t *testing.T) *fakeGrafana {
	f := &fakeGrafana{
		t:           t,
		dashboards:  map[string]map[string]any{},
		datasources: []DataSourceSummary{},
	}
	mux := http.NewServeMux()

	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		f.assertAuth(r)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(f.searchResults)
	})
	mux.HandleFunc("/api/dashboards/uid/", func(w http.ResponseWriter, r *http.Request) {
		f.assertAuth(r)
		uid := strings.TrimPrefix(r.URL.Path, "/api/dashboards/uid/")
		d, ok := f.dashboards[uid]
		if !ok {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"dashboard": d, "meta": map[string]any{}})
	})
	mux.HandleFunc("/api/dashboards/db", func(w http.ResponseWriter, r *http.Request) {
		f.assertAuth(r)
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &f.saveCallBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(f.saveResponse)
	})
	mux.HandleFunc("/api/datasources", func(w http.ResponseWriter, r *http.Request) {
		f.assertAuth(r)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(f.datasources)
	})

	f.server = httptest.NewServer(mux)
	return f
}

func (f *fakeGrafana) assertAuth(r *http.Request) {
	if f.wantBasicAuth {
		u, p, ok := r.BasicAuth()
		if !ok || u == "" || p == "" {
			f.t.Errorf("expected basic auth, got %q", r.Header.Get("Authorization"))
		}
		return
	}
	if f.wantAuth != "" && r.Header.Get("Authorization") != f.wantAuth {
		f.t.Errorf("Authorization = %q, want %q", r.Header.Get("Authorization"), f.wantAuth)
	}
}

func (f *fakeGrafana) close() { f.server.Close() }

func (f *fakeGrafana) client(useBasic bool) *Client {
	cfg := Config{
		BaseURL:    f.server.URL,
		PluginID:   "motley-slayer-datasource",
		HTTPClient: &http.Client{Timeout: 5 * time.Second},
	}
	if useBasic {
		cfg.BasicUser = "admin"
		cfg.BasicPass = "admin"
	} else {
		cfg.Token = "glsa_test"
	}
	return New(cfg)
}

func TestGrafanaClient_ListDashboards_TokenAuth(t *testing.T) {
	f := newFakeGrafana(t)
	defer f.close()
	f.wantAuth = "Bearer glsa_test"
	f.searchResults = []DashboardSummary{
		{UID: "abc", Title: "Sales", Tags: []string{"slayer"}},
		{UID: "xyz", Title: "Ops"},
	}
	got, err := f.client(false).ListDashboards(context.Background(), "")
	if err != nil {
		t.Fatalf("ListDashboards: %v", err)
	}
	if len(got) != 2 || got[0].UID != "abc" {
		t.Errorf("unexpected: %+v", got)
	}
}

func TestGrafanaClient_BasicAuthFallback(t *testing.T) {
	f := newFakeGrafana(t)
	defer f.close()
	f.wantBasicAuth = true
	f.searchResults = []DashboardSummary{{UID: "abc", Title: "Sales"}}
	if _, err := f.client(true).ListDashboards(context.Background(), ""); err != nil {
		t.Errorf("basic-auth call failed: %v", err)
	}
}

func TestGrafanaClient_GetAndSaveDashboard(t *testing.T) {
	f := newFakeGrafana(t)
	defer f.close()
	f.wantAuth = "Bearer glsa_test"
	f.dashboards["slayer-jaffle-demo"] = map[string]any{
		"uid":    "slayer-jaffle-demo",
		"title":  "Jaffle Shop",
		"panels": []any{map[string]any{"id": float64(1), "type": "stat"}},
	}
	f.saveResponse = SaveDashboardResult{Status: "success", UID: "slayer-jaffle-demo", Version: 5}

	c := f.client(false)
	d, err := c.GetDashboard(context.Background(), "slayer-jaffle-demo")
	if err != nil {
		t.Fatalf("GetDashboard: %v", err)
	}
	if d.Dashboard["title"] != "Jaffle Shop" {
		t.Errorf("title = %v", d.Dashboard["title"])
	}

	// Append a panel and PUT it back.
	d.Dashboard["panels"] = append(d.Dashboard["panels"].([]any), map[string]any{"id": float64(2), "type": "table"})
	res, err := c.SaveDashboard(context.Background(), d.Dashboard, "test")
	if err != nil {
		t.Fatalf("SaveDashboard: %v", err)
	}
	if res.Status != "success" {
		t.Errorf("status = %q", res.Status)
	}
	body := f.saveCallBody
	if body["overwrite"] != true {
		t.Errorf("overwrite not set: %+v", body)
	}
	sent := body["dashboard"].(map[string]any)
	if len(sent["panels"].([]any)) != 2 {
		t.Errorf("expected 2 panels in saved body, got %v", sent["panels"])
	}
}

func TestFindSlayerDatasource(t *testing.T) {
	f := newFakeGrafana(t)
	defer f.close()
	f.wantAuth = "Bearer glsa_test"
	f.datasources = []DataSourceSummary{
		{UID: "prom", Name: "prometheus", Type: "prometheus"},
		{UID: "slayer", Name: "slayer", Type: "motley-slayer-datasource"},
	}
	c := f.client(false)

	// auto-detect
	ds, err := c.FindSlayerDatasource(context.Background(), "", "")
	if err != nil || ds.UID != "slayer" {
		t.Errorf("auto-detect: %v %+v", err, ds)
	}
	// by uid
	ds, err = c.FindSlayerDatasource(context.Background(), "slayer", "")
	if err != nil || ds.UID != "slayer" {
		t.Errorf("by uid: %v %+v", err, ds)
	}
	// not found
	if _, err := c.FindSlayerDatasource(context.Background(), "missing", ""); err == nil {
		t.Error("expected error on missing uid")
	}
}

func TestGrafanaClient_NoAuth_SendsNoAuthHeader(t *testing.T) {
	// Anonymous-Admin Grafana installs accept unauthenticated requests as
	// admin. The client must not send any Authorization header in that mode.
	var seenAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()
	c := New(Config{BaseURL: srv.URL})
	_, _ = c.ListDashboards(context.Background(), "")
	if seenAuth != "" {
		t.Errorf("unexpected Authorization header in no-auth mode: %q", seenAuth)
	}
}
