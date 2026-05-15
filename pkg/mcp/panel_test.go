package mcp

import (
	"reflect"
	"testing"
)

func TestNextPanelID(t *testing.T) {
	cases := []struct {
		name   string
		panels []any
		want   int
	}{
		{"empty dashboard", nil, 1},
		{"one panel", []any{map[string]any{"id": float64(7)}}, 8},
		{"max wins", []any{
			map[string]any{"id": float64(3)},
			map[string]any{"id": float64(11)},
			map[string]any{"id": float64(5)},
		}, 12},
		{"non-map entries skipped", []any{
			"not a panel",
			map[string]any{"id": float64(4)},
		}, 5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := NextPanelID(c.panels); got != c.want {
				t.Errorf("got %d, want %d", got, c.want)
			}
		})
	}
}

func TestNextGridPos(t *testing.T) {
	cases := []struct {
		name   string
		panels []any
		wantY  int
	}{
		{"empty", nil, 0},
		{"one panel below 0", []any{
			map[string]any{"gridPos": map[string]any{"x": float64(0), "y": float64(0), "w": float64(24), "h": float64(8)}},
		}, 8},
		{"multiple — pick max bottom", []any{
			map[string]any{"gridPos": map[string]any{"x": float64(0), "y": float64(0), "w": float64(12), "h": float64(4)}},
			map[string]any{"gridPos": map[string]any{"x": float64(12), "y": float64(4), "w": float64(12), "h": float64(9)}},
			map[string]any{"gridPos": map[string]any{"x": float64(0), "y": float64(4), "w": float64(12), "h": float64(8)}},
		}, 13},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gp := NextGridPos(c.panels)
			if gp["y"] != c.wantY {
				t.Errorf("y = %v, want %d", gp["y"], c.wantY)
			}
			if gp["w"] != 24 || gp["h"] != 8 {
				t.Errorf("unexpected default w/h: %+v", gp)
			}
		})
	}
}

func TestBuildPanel_ShapesPerType(t *testing.T) {
	target := map[string]any{
		"source_model": "orders",
		"measures":     []any{map[string]any{"formula": "*:count"}},
	}
	gridPos := map[string]any{"x": 0, "y": 0, "w": 24, "h": 8}

	for _, panelType := range []string{"table", "stat", "timeseries", "barchart"} {
		t.Run(panelType, func(t *testing.T) {
			p := BuildPanel(7, panelType, "Test", gridPos, "slayer", "motley-slayer-datasource", target)
			if p["id"] != 7 {
				t.Errorf("id = %v", p["id"])
			}
			if p["type"] != panelType {
				t.Errorf("type = %v", p["type"])
			}
			ds := p["datasource"].(map[string]any)
			if ds["uid"] != "slayer" || ds["type"] != "motley-slayer-datasource" {
				t.Errorf("datasource pointer wrong: %+v", ds)
			}
			targets := p["targets"].([]any)
			if len(targets) != 1 {
				t.Fatalf("targets = %d", len(targets))
			}
			t0 := targets[0].(map[string]any)
			if t0["refId"] != "A" {
				t.Errorf("refId = %v", t0["refId"])
			}
			if t0["source_model"] != "orders" {
				t.Errorf("source_model = %v — target lost the query payload", t0["source_model"])
			}
		})
	}
}

func TestBuildPanel_DefaultsTypeWhenBlank(t *testing.T) {
	p := BuildPanel(1, "", "x", map[string]any{}, "u", "t", map[string]any{})
	if p["type"] != "table" {
		t.Errorf("default panel type = %v, want table", p["type"])
	}
}

func TestQueryStringToObject(t *testing.T) {
	got, err := ParseQueryString(`{"source_model":"orders","measures":[{"formula":"*:count"}]}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if got["source_model"] != "orders" {
		t.Errorf("source_model = %v", got["source_model"])
	}
	if _, err := ParseQueryString("not json"); err == nil {
		t.Error("expected error on garbage")
	}
}

func TestSummariseDashboard_ProjectsRelevantFields(t *testing.T) {
	d := map[string]any{
		"uid":      "abc",
		"title":    "T",
		"tags":     []any{"x"},
		"timezone": "browser",
		"time":     map[string]any{"from": "now-1h", "to": "now"},
		"panels": []any{
			map[string]any{
				"id":      float64(1),
				"type":    "table",
				"title":   "P1",
				"gridPos": map[string]any{"x": 0, "y": 0, "w": 24, "h": 8},
				"targets": []any{map[string]any{"refId": "A", "source_model": "orders"}},
				"junk":    "should-not-appear",
			},
		},
	}
	out := SummariseDashboard(d)
	if out["uid"] != "abc" || out["title"] != "T" {
		t.Errorf("top-level fields wrong: %+v", out)
	}
	panels := out["panels"].([]map[string]any)
	if len(panels) != 1 {
		t.Fatalf("panels = %d", len(panels))
	}
	if _, hasJunk := panels[0]["junk"]; hasJunk {
		t.Error("summary should not carry through arbitrary fields")
	}
	if !reflect.DeepEqual(panels[0]["title"], "P1") {
		t.Errorf("panel title = %v", panels[0]["title"])
	}
}
