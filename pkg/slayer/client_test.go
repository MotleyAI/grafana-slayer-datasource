package slayer

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClient_Query_SendsAndDecodes(t *testing.T) {
	var gotPath, gotMethod, gotAuth string
	var gotBody Query

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [{"orders.status": "completed", "orders._count": 47}],
			"row_count": 1,
			"columns": ["orders.status", "orders._count"],
			"attributes": {
				"dimensions": {"orders.status": {"label": "Status"}},
				"measures": {"orders._count": {"label": "Order count", "format": {"type": "integer"}}}
			}
		}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "secret-token")
	resp, err := c.Query(context.Background(), Query{
		SourceModel: "orders",
		Measures:    []map[string]interface{}{{"formula": "*:count"}},
		Dimensions:  []map[string]interface{}{{"name": "status"}},
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/query" {
		t.Errorf("path = %q, want /query", gotPath)
	}
	if gotAuth != "Bearer secret-token" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotBody.SourceModel != "orders" {
		t.Errorf("source_model = %q", gotBody.SourceModel)
	}
	if resp.RowCount != 1 {
		t.Errorf("row_count = %d", resp.RowCount)
	}
	if got := resp.Data[0]["orders._count"]; got.(float64) != 47 {
		t.Errorf("data[0].count = %v", got)
	}
	if resp.Attributes == nil || resp.Attributes.Measures["orders._count"].Label != "Order count" {
		t.Errorf("missing measure label: %+v", resp.Attributes)
	}
	if resp.Attributes.Measures["orders._count"].Format == nil ||
		resp.Attributes.Measures["orders._count"].Format.Type != "integer" {
		t.Errorf("missing measure format: %+v", resp.Attributes.Measures["orders._count"].Format)
	}
}

func TestClient_Query_NoAuthHeader_WhenNoKey(t *testing.T) {
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[],"row_count":0,"columns":[]}`))
	}))
	defer srv.Close()
	_, _ = NewClient(srv.URL, "").Query(context.Background(), Query{SourceModel: "x"})
	if sawAuth != "" {
		t.Errorf("Authorization sent without key: %q", sawAuth)
	}
}

func TestClient_Query_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"detail":"bad request"}`))
	}))
	defer srv.Close()
	_, err := NewClient(srv.URL, "").Query(context.Background(), Query{SourceModel: "x"})
	if err == nil {
		t.Fatal("want error on 400, got nil")
	}
}

func TestClient_Health(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			http.Error(w, "nope", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()
	if err := NewClient(srv.URL, "").Health(context.Background()); err != nil {
		t.Errorf("Health: %v", err)
	}
}
