// Package mcp registers the SLayer-Grafana MCP tools and exposes helpers
// (panel construction, dashboard summarisation) on a Grafana REST client.
// Mounted by the plugin's CallResource handler on
// /api/datasources/uid/<slayer-ds>/resources/mcp.
package mcp

import "encoding/json"

// BuildPanel produces the minimum panel JSON template, parameterised on type.
// Grafana's frontend fills in the rest from its panel-plugin defaults, so we
// keep this lean — just enough to render correctly out of the box.
func BuildPanel(
	id int,
	panelType string,
	title string,
	gridPos map[string]any,
	datasourceUID string,
	datasourceType string,
	target map[string]any,
) map[string]any {
	if panelType == "" {
		panelType = "table"
	}
	if title == "" {
		title = "(untitled)"
	}
	target["refId"] = "A"
	target["datasource"] = map[string]any{
		"uid":  datasourceUID,
		"type": datasourceType,
	}
	panel := map[string]any{
		"id":         id,
		"type":       panelType,
		"title":      title,
		"gridPos":    gridPos,
		"datasource": map[string]any{"uid": datasourceUID, "type": datasourceType},
		"targets":    []any{target},
	}
	switch panelType {
	case "stat":
		panel["options"] = map[string]any{
			"reduceOptions": map[string]any{
				"calcs":  []any{"lastNotNull"},
				"fields": "",
				"values": false,
			},
			"colorMode": "value",
			"graphMode": "area",
			"textMode":  "auto",
		}
	case "timeseries":
		panel["fieldConfig"] = map[string]any{
			"defaults": map[string]any{
				"custom": map[string]any{
					"drawStyle":   "line",
					"lineWidth":   2,
					"fillOpacity": 15,
					"showPoints":  "never",
				},
			},
		}
	case "barchart":
		panel["options"] = map[string]any{
			"orientation": "horizontal",
			"showValue":   "auto",
		}
	case "table":
		panel["options"] = map[string]any{
			"showHeader": true,
			"cellHeight": "sm",
		}
	}
	return panel
}

// NextPanelID returns max(existing ids)+1, or 1 if the dashboard has no panels.
func NextPanelID(panels []any) int {
	maxID := 0
	for _, p := range panels {
		m, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if v, ok := m["id"]; ok {
			if f, ok := v.(float64); ok && int(f) > maxID {
				maxID = int(f)
			}
		}
	}
	return maxID + 1
}

// NextGridPos picks (x=0, y=below-everything, w=24, h=8) — a full-width band
// at the bottom of the dashboard. Trivial collision-free placement; the user
// can drag-resize after the agent creates the panel.
func NextGridPos(panels []any) map[string]any {
	maxBottom := 0
	for _, p := range panels {
		m, ok := p.(map[string]any)
		if !ok {
			continue
		}
		gp, ok := m["gridPos"].(map[string]any)
		if !ok {
			continue
		}
		y, _ := gp["y"].(float64)
		h, _ := gp["h"].(float64)
		if bottom := int(y) + int(h); bottom > maxBottom {
			maxBottom = bottom
		}
	}
	return map[string]any{"x": 0, "y": maxBottom, "w": 24, "h": 8}
}

// PluckPanels returns the panels list from a dashboard map, or nil when the
// dashboard has none yet.
func PluckPanels(dashboard map[string]any) []any {
	v, ok := dashboard["panels"]
	if !ok || v == nil {
		return nil
	}
	if arr, ok := v.([]any); ok {
		return arr
	}
	return nil
}

// ParseQueryString parses an agent-supplied SlayerQuery JSON string into a map.
func ParseQueryString(s string) (map[string]any, error) {
	var out map[string]any
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SummariseDashboard projects a full dashboard down to the agent-relevant
// fields so we don't blow context on Grafana internals.
func SummariseDashboard(d map[string]any) map[string]any {
	out := map[string]any{
		"uid":      d["uid"],
		"title":    d["title"],
		"tags":     d["tags"],
		"timezone": d["timezone"],
	}
	if tr, ok := d["time"]; ok {
		out["time"] = tr
	}
	panels := PluckPanels(d)
	summaries := make([]map[string]any, 0, len(panels))
	for _, p := range panels {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		panel := map[string]any{
			"id":      pm["id"],
			"type":    pm["type"],
			"title":   pm["title"],
			"gridPos": pm["gridPos"],
		}
		if targets, ok := pm["targets"].([]any); ok && len(targets) > 0 {
			panel["targets"] = targets
		}
		summaries = append(summaries, panel)
	}
	out["panels"] = summaries
	return out
}
