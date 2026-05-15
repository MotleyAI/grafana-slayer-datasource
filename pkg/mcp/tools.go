package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/motleyai/grafana-slayer-datasource/pkg/grafana"
)

// RegisterTools wires every MCP tool the agent can invoke. Three tools:
//   - list_dashboards          → discover what's already there
//   - inspect_dashboard        → read panels/queries on a specific dashboard
//   - add_panel_to_dashboard   → append a SLayer-backed panel
func RegisterTools(s *server.MCPServer, gf *grafana.Client) {
	s.AddTool(listDashboardsTool(), listDashboardsHandler(gf))
	s.AddTool(inspectDashboardTool(), inspectDashboardHandler(gf))
	s.AddTool(addPanelToDashboardTool(), addPanelToDashboardHandler(gf))
}

// NewServer convenience: name + version + RegisterTools.
func NewServer(name, version string, gf *grafana.Client) *server.MCPServer {
	s := server.NewMCPServer(name, version, server.WithToolCapabilities(false))
	RegisterTools(s, gf)
	return s
}

// ──────────────────────────────────────────────────────────────────────────
// list_dashboards
// ──────────────────────────────────────────────────────────────────────────

func listDashboardsTool() mcp.Tool {
	return mcp.NewTool("list_dashboards",
		mcp.WithDescription(
			"List Grafana dashboards. Returns each dashboard's uid, title, folder, tags, and URL. "+
				"Use this to find the dashboard_uid for add_panel_to_dashboard.",
		),
		mcp.WithString("query",
			mcp.Description("Optional substring to filter dashboard titles."),
		),
	)
}

func listDashboardsHandler(gf *grafana.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		q := req.GetString("query", "")
		dashboards, err := gf.ListDashboards(ctx, q)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		raw, err := json.MarshalIndent(dashboards, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(string(raw)), nil
	}
}

// ──────────────────────────────────────────────────────────────────────────
// inspect_dashboard
// ──────────────────────────────────────────────────────────────────────────

func inspectDashboardTool() mcp.Tool {
	return mcp.NewTool("inspect_dashboard",
		mcp.WithDescription(
			"Read a Grafana dashboard's structure: title, tags, time range, and each panel's "+
				"id, type, title, gridPos, and target query (the SlayerQuery payload). Use to see "+
				"what's already on a dashboard before adding panels.",
		),
		mcp.WithString("dashboard_uid",
			mcp.Required(),
			mcp.Description("The dashboard uid (from list_dashboards)."),
		),
	)
}

func inspectDashboardHandler(gf *grafana.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		uid, err := req.RequireString("dashboard_uid")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		dash, err := gf.GetDashboard(ctx, uid)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		out := SummariseDashboard(dash.Dashboard)
		raw, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(string(raw)), nil
	}
}

// ──────────────────────────────────────────────────────────────────────────
// add_panel_to_dashboard
// ──────────────────────────────────────────────────────────────────────────

func addPanelToDashboardTool() mcp.Tool {
	return mcp.NewTool("add_panel_to_dashboard",
		mcp.WithDescription(
			"Append a SLayer-backed panel to an existing Grafana dashboard. "+
				"The panel's query is a SlayerQuery — same DSL as SLayer's own MCP `query` tool: "+
				"`source_model`, `measures`, `dimensions`, `time_dimensions`, `filters`, etc. "+
				"For multi-stage / cohort-style queries, include `source_queries` inline. "+
				"The panel is placed full-width below existing panels; the user can drag/resize "+
				"afterwards in Grafana's panel editor.",
		),
		mcp.WithString("dashboard_uid",
			mcp.Required(),
			mcp.Description("Target dashboard uid (from list_dashboards)."),
		),
		mcp.WithString("title",
			mcp.Required(),
			mcp.Description("Human-readable panel title."),
		),
		mcp.WithString("slayer_query",
			mcp.Required(),
			mcp.Description(
				"JSON-encoded SlayerQuery — pass as a string. "+
					`Examples: {"source_model":"orders","measures":[{"formula":"*:count"}]} `+
					`or {"source_model":"orders","measures":[{"formula":"order_total:sum"}],`+
					`"time_dimensions":[{"dimension":"ordered_at","granularity":"day"}]}.`,
			),
		),
		mcp.WithString("panel_type",
			mcp.Description("Grafana panel renderer: `table` (default), `stat`, `timeseries`, or `barchart`."),
		),
		mcp.WithString("datasource_uid",
			mcp.Description("SLayer datasource uid. Defaults to the first registered SLayer datasource."),
		),
	)
}

func addPanelToDashboardHandler(gf *grafana.Client) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		dashUID, err := req.RequireString("dashboard_uid")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		title, err := req.RequireString("title")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		queryRaw, err := req.RequireString("slayer_query")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		target, err := ParseQueryString(queryRaw)
		if err != nil {
			return mcp.NewToolResultError("slayer_query is not valid JSON: " + err.Error()), nil
		}
		panelType := strings.ToLower(req.GetString("panel_type", "table"))
		dsUID := req.GetString("datasource_uid", "")

		ds, err := gf.FindSlayerDatasource(ctx, dsUID, "")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		dash, err := gf.GetDashboard(ctx, dashUID)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		panels := PluckPanels(dash.Dashboard)
		newPanel := BuildPanel(
			NextPanelID(panels),
			panelType,
			title,
			NextGridPos(panels),
			ds.UID,
			ds.Type,
			target,
		)
		dash.Dashboard["panels"] = append(panels, newPanel)

		result, err := gf.SaveDashboard(
			ctx,
			dash.Dashboard,
			fmt.Sprintf("slayer-grafana: add panel %q", title),
		)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		summary := map[string]any{
			"status":         result.Status,
			"dashboard_uid":  result.UID,
			"dashboard_url":  result.URL,
			"panel_id":       newPanel["id"],
			"panel_type":     panelType,
			"panel_title":    title,
			"datasource_uid": ds.UID,
		}
		raw, _ := json.MarshalIndent(summary, "", "  ")
		return mcp.NewToolResultText(string(raw)), nil
	}
}
