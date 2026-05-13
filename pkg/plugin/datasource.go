package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/motleyai/grafana-slayer-datasource/pkg/models"
	"github.com/motleyai/grafana-slayer-datasource/pkg/slayer"
)

var (
	_ backend.QueryDataHandler      = (*Datasource)(nil)
	_ backend.CheckHealthHandler    = (*Datasource)(nil)
	_ backend.CallResourceHandler   = (*Datasource)(nil)
	_ instancemgmt.InstanceDisposer = (*Datasource)(nil)
)

// slayerClient is the subset of *slayer.Client used by the datasource — kept
// as an interface so tests can substitute a fake.
type slayerClient interface {
	Query(ctx context.Context, q slayer.Query) (*slayer.Response, error)
	Models(ctx context.Context) ([]slayer.ModelInfo, error)
	Health(ctx context.Context) error
}

func NewDatasource(_ context.Context, settings backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	cfg, err := models.LoadPluginSettings(settings)
	if err != nil {
		return nil, err
	}
	return &Datasource{
		client: slayer.NewClient(cfg.URL, cfg.Secrets.APIKey),
		url:    cfg.URL,
	}, nil
}

type Datasource struct {
	client slayerClient
	url    string
}

func (d *Datasource) Dispose() {}

func (d *Datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	response := backend.NewQueryDataResponse()
	for _, q := range req.Queries {
		response.Responses[q.RefID] = d.query(ctx, q)
	}
	return response, nil
}

func (d *Datasource) query(ctx context.Context, query backend.DataQuery) backend.DataResponse {
	var qm slayer.Query
	if len(query.JSON) > 0 {
		if err := json.Unmarshal(query.JSON, &qm); err != nil {
			return backend.ErrDataResponse(backend.StatusBadRequest, fmt.Sprintf("invalid query JSON: %v", err))
		}
	}
	// v0 default — fall back to a Jaffle Shop sample so first-time panels
	// render something instead of an empty error. Real QueryEditor lands in M3.
	if qm.Name == "" && qm.SourceModel == "" {
		qm.SourceModel = "orders"
		qm.Measures = []map[string]interface{}{
			{"formula": "*:count"},
			{"formula": "order_total:sum"},
		}
		qm.Dimensions = []map[string]interface{}{{"name": "store_id"}}
	}

	injectTimeVariables(&qm, query.TimeRange, query.Interval)
	maybeInjectTimeFilter(&qm)

	resp, err := d.client.Query(ctx, qm)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, fmt.Sprintf("slayer query: %v", err))
	}
	return backend.DataResponse{Frames: data.Frames{toFrame(query.RefID, &qm, resp)}}
}

// injectTimeVariables populates standard `__from`/`__to`/`__interval` variables
// on the SlayerQuery. SLayer's filter substitution is literal text replacement,
// so callers reference `{__from}` in filter strings; values land verbatim.
//   - __from / __to:        RFC3339 timestamps (quote them in SQL: `col >= '{__from}'`)
//   - __from_ms / __to_ms:  epoch milliseconds (unquoted numeric)
//   - __interval_ms:        Grafana's suggested bucket size, epoch ms
//
// Caller-supplied variables win — we only populate keys that aren't already set.
func injectTimeVariables(q *slayer.Query, tr backend.TimeRange, interval time.Duration) {
	if q.Variables == nil {
		q.Variables = map[string]interface{}{}
	}
	setIfAbsent(q.Variables, "__from", tr.From.UTC().Format(time.RFC3339))
	setIfAbsent(q.Variables, "__to", tr.To.UTC().Format(time.RFC3339))
	setIfAbsent(q.Variables, "__from_ms", tr.From.UnixMilli())
	setIfAbsent(q.Variables, "__to_ms", tr.To.UnixMilli())
	setIfAbsent(q.Variables, "__interval_ms", interval.Milliseconds())
}

func setIfAbsent(m map[string]interface{}, k string, v interface{}) {
	if _, ok := m[k]; !ok {
		m[k] = v
	}
}

// maybeInjectTimeFilter adds `<time_dim> >= '{__from}' AND … <= '{__to}'` when:
//   - the query has ≥1 time_dimension defined, AND
//   - no existing filter references `{__from}` / `{__to}` / `{__from_ms}` / `{__to_ms}`
//     (user has explicitly handled time, leave them alone).
//
// Users who want the auto-injection to stop just write their own filter with
// any `__from*`/`__to*` reference — the auto-injection then yields to them.
func maybeInjectTimeFilter(q *slayer.Query) {
	if len(q.TimeDimensions) == 0 {
		return
	}
	for _, f := range q.Filters {
		if strings.Contains(f, "{__from") || strings.Contains(f, "{__to") {
			return
		}
	}
	first := q.TimeDimensions[0]
	col, _ := first["dimension"].(string)
	if col == "" {
		col, _ = first["name"].(string)
	}
	if col == "" {
		return
	}
	q.Filters = append(q.Filters,
		fmt.Sprintf("%s >= '{__from}' AND %s <= '{__to}'", col, col),
	)
}

func (d *Datasource) CheckHealth(ctx context.Context, _ *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	if err := d.client.Health(ctx); err != nil {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: fmt.Sprintf("SLayer unreachable at %s: %v", d.url, err),
		}, nil
	}
	return &backend.CheckHealthResult{
		Status:  backend.HealthStatusOk,
		Message: "Connected to SLayer at " + d.url,
	}, nil
}

// CallResource serves auxiliary endpoints the frontend hits via
// /api/datasources/uid/<uid>/resources/<path>. Routes:
//   - GET  /resources/models      → JSON list of SLayer models (dropdown autocomplete)
//   - POST /resources/metric-find → run a SlayerQuery and project its first
//                                   column into a MetricFindValue[] for template
//                                   variable dropdowns.
func (d *Datasource) CallResource(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	path := strings.Trim(req.Path, "/")
	switch path {
	case "models":
		return d.handleModels(ctx, sender)
	case "metric-find":
		return d.handleMetricFind(ctx, req, sender)
	default:
		return jsonStatus(sender, http.StatusNotFound, map[string]string{"error": fmt.Sprintf("unknown resource path: %q", path)})
	}
}

func (d *Datasource) handleModels(ctx context.Context, sender backend.CallResourceResponseSender) error {
	list, err := d.client.Models(ctx)
	if err != nil {
		return jsonStatus(sender, http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	return jsonStatus(sender, http.StatusOK, list)
}

// metricFindValue mirrors @grafana/data's MetricFindValue.
type metricFindValue struct {
	Text  string      `json:"text"`
	Value interface{} `json:"value,omitempty"`
}

func (d *Datasource) handleMetricFind(ctx context.Context, req *backend.CallResourceRequest, sender backend.CallResourceResponseSender) error {
	var q slayer.Query
	if len(req.Body) > 0 {
		if err := json.Unmarshal(req.Body, &q); err != nil {
			return jsonStatus(sender, http.StatusBadRequest, map[string]string{"error": fmt.Sprintf("invalid metric-find query: %v", err)})
		}
	}
	if q.SourceModel == "" && q.Name == "" {
		return jsonStatus(sender, http.StatusBadRequest, map[string]string{"error": "metric-find query needs source_model or name"})
	}
	resp, err := d.client.Query(ctx, q)
	if err != nil {
		return jsonStatus(sender, http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
	if len(resp.Columns) == 0 {
		return jsonStatus(sender, http.StatusOK, []metricFindValue{})
	}
	col := resp.Columns[0]
	out := make([]metricFindValue, 0, len(resp.Data))
	for _, row := range resp.Data {
		v, ok := row[col]
		if !ok || v == nil {
			continue
		}
		out = append(out, metricFindValue{Text: fmt.Sprintf("%v", v), Value: v})
	}
	return jsonStatus(sender, http.StatusOK, out)
}

func jsonStatus(sender backend.CallResourceResponseSender, status int, body interface{}) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return sender.Send(&backend.CallResourceResponse{
		Status:  status,
		Headers: map[string][]string{"Content-Type": {"application/json"}},
		Body:    raw,
	})
}

// toFrame converts a SLayer query response into a Grafana data.Frame.
// Column types are decided per-column:
//   - any column named in the query's time_dimensions ⇒ time.Time
//   - any column listed in response.attributes.measures ⇒ float64
//   - anything else ⇒ string
//
// Column order follows the query spec (dimensions → time_dimensions → measures,
// each list in user-supplied order) rather than SLayer's response order, which
// is alphabetical and surprises Grafana users. Any unrecognized columns
// (compound-formula intermediates that SLayer materializes) trail at the end.
//
// Time-series pivoting (multi-series via dimensions) and Grafana-unit mapping
// from FieldMetadata.format are follow-up work.
func toFrame(refID string, q *slayer.Query, resp *slayer.Response) *data.Frame {
	timeCols := timeColumnSet(q)
	ordered := orderColumns(q, resp.Columns)
	frame := data.NewFrame("response")
	frame.RefID = refID
	for _, col := range ordered {
		frame.Fields = append(frame.Fields, buildField(col, resp, timeCols[col]))
	}
	return frame
}

// orderColumns reshuffles resp.Columns into the user-visible order suggested
// by the query: dimensions first (in query order), then time_dimensions, then
// measures. Measures use the explicit `name` when set, otherwise the canonical
// alias SLayer derives for simple aggregations (`*:count` → `_count`,
// `revenue:sum` → `revenue_sum`). Columns the planner can't account for
// (compound-formula intermediates, unnamed transforms) fall through to the
// tail in SLayer's original order — they're still visible, just not promoted.
func orderColumns(q *slayer.Query, cols []string) []string {
	if q == nil || q.SourceModel == "" {
		return cols
	}
	prefix := q.SourceModel + "."
	available := make(map[string]bool, len(cols))
	for _, c := range cols {
		available[c] = true
	}
	placed := map[string]bool{}
	ordered := make([]string, 0, len(cols))
	add := func(suffix string) {
		if suffix == "" {
			return
		}
		full := prefix + suffix
		if !available[full] || placed[full] {
			return
		}
		ordered = append(ordered, full)
		placed[full] = true
	}
	for _, d := range q.Dimensions {
		name, _ := d["name"].(string)
		add(name)
	}
	for _, td := range q.TimeDimensions {
		col, _ := td["dimension"].(string)
		if col == "" {
			col, _ = td["name"].(string)
		}
		add(col)
	}
	for _, m := range q.Measures {
		if name, ok := m["name"].(string); ok && name != "" {
			add(name)
			continue
		}
		if formula, ok := m["formula"].(string); ok {
			add(canonicalMeasureSuffix(formula))
		}
	}
	// SLayer materializes dependencies of compound formulas as their own
	// columns; sweep them up here in SLayer's order so nothing is lost.
	for _, c := range cols {
		if !placed[c] {
			ordered = append(ordered, c)
		}
	}
	return ordered
}

// canonicalMeasureSuffix returns the result-column suffix SLayer emits for an
// un-renamed measure formula, or "" when we can't determine it cheaply
// (compound expressions like `cumsum(...)` or `revenue:sum / *:count`).
// The mapping mirrors SLayer's CLAUDE.md naming convention:
//
//	`*:count`         → `_count`
//	`revenue:sum`     → `revenue_sum`
//	`order_total:max` → `order_total_max`
func canonicalMeasureSuffix(formula string) string {
	formula = strings.TrimSpace(formula)
	if strings.HasPrefix(formula, "*:") {
		rest := formula[2:]
		if isSimpleIdentifier(rest) {
			return "_" + rest
		}
		return ""
	}
	if !isSimpleAggregation(formula) {
		return ""
	}
	i := strings.IndexByte(formula, ':')
	return formula[:i] + "_" + formula[i+1:]
}

// isSimpleAggregation returns true for strings of the form `<ident>:<ident>` —
// anything with function calls, arithmetic, or whitespace is rejected.
func isSimpleAggregation(s string) bool {
	if strings.ContainsAny(s, "()/+-* \t\n,") {
		return false
	}
	colons := 0
	for _, r := range s {
		if r == ':' {
			colons++
		}
	}
	return colons == 1
}

func isSimpleIdentifier(s string) bool {
	return !strings.ContainsAny(s, "()/+-* \t\n,:")
}

// timeColumnSet derives the set of fully-qualified result column names that
// should be emitted as time.Time. SLayer's response column naming is
// `<source_model>.<dimension>` (per docs), so we apply the same rule.
func timeColumnSet(q *slayer.Query) map[string]bool {
	out := map[string]bool{}
	if q == nil {
		return out
	}
	for _, td := range q.TimeDimensions {
		col, _ := td["dimension"].(string)
		if col == "" {
			col, _ = td["name"].(string)
		}
		if col == "" {
			continue
		}
		if q.SourceModel != "" && !strings.Contains(col, ".") {
			out[q.SourceModel+"."+col] = true
		}
		out[col] = true
	}
	return out
}

func buildField(col string, resp *slayer.Response, isTime bool) *data.Field {
	var meta slayer.FieldMetadata
	isMeasure := false
	if resp.Attributes != nil {
		if m, ok := resp.Attributes.Measures[col]; ok {
			isMeasure, meta = true, m
		} else if dm, ok := resp.Attributes.Dimensions[col]; ok {
			meta = dm
		}
	}
	var field *data.Field
	switch {
	case isTime:
		values := make([]*time.Time, len(resp.Data))
		for i, row := range resp.Data {
			if v, ok := row[col]; ok && v != nil {
				if s, ok := v.(string); ok {
					if t, err := parseSlayerTime(s); err == nil {
						values[i] = &t
					}
				}
			}
		}
		field = data.NewField(col, nil, values)
	case isMeasure:
		values := make([]*float64, len(resp.Data))
		for i, row := range resp.Data {
			if v, ok := row[col]; ok && v != nil {
				if f, ok := v.(float64); ok {
					values[i] = &f
				}
			}
		}
		field = data.NewField(col, nil, values)
	default:
		values := make([]*string, len(resp.Data))
		for i, row := range resp.Data {
			if v, ok := row[col]; ok && v != nil {
				s := fmt.Sprintf("%v", v)
				values[i] = &s
			}
		}
		field = data.NewField(col, nil, values)
	}
	if meta.Label != "" || meta.Format != nil {
		cfg := &data.FieldConfig{DisplayName: meta.Label}
		applyFormatToConfig(cfg, meta.Format)
		field.Config = cfg
	}
	return field
}

// applyFormatToConfig populates Grafana FieldConfig.Unit / .Decimals from
// SLayer's per-column format hint. Panel-side fieldConfig.defaults still
// override what we set here — Grafana merges in the order: frame field
// config (us) < panel defaults < panel overrides. So a dashboard that wants
// a different unit can specify one and win.
func applyFormatToConfig(cfg *data.FieldConfig, f *slayer.NumberFormat) {
	if f == nil {
		return
	}
	if unit := slayerFormatToGrafanaUnit(f); unit != "" {
		cfg.Unit = unit
	}
	if f.Precision != nil {
		p := uint16(*f.Precision)
		cfg.Decimals = &p
	}
}

// slayerFormatToGrafanaUnit maps SLayer's NumberFormatType (percent / currency /
// integer / float) onto a Grafana unit code. `short` auto-scales raw numbers
// (K/M/B suffixes); `percent` assumes 0-100 (the conventional SLayer percent
// shape — `percentunit` would be needed if SLayer ever emits 0-1 values).
func slayerFormatToGrafanaUnit(f *slayer.NumberFormat) string {
	switch f.Type {
	case "percent":
		return "percent"
	case "currency":
		return currencyUnitForSymbol(f.Symbol)
	case "integer", "float":
		return "short"
	}
	return ""
}

// currencyUnitForSymbol resolves SLayer's currency symbol to a Grafana
// currency unit code. SLayer defaults the symbol to "$" for CURRENCY type
// when none is set, so a nil pointer maps to USD. Unknown symbols fall
// back to USD (best-effort — users can override per-panel).
func currencyUnitForSymbol(symbol *string) string {
	if symbol == nil {
		return "currencyUSD"
	}
	switch *symbol {
	case "$":
		return "currencyUSD"
	case "€":
		return "currencyEUR"
	case "£":
		return "currencyGBP"
	case "¥":
		return "currencyJPY"
	case "₽":
		return "currencyRUB"
	case "₹":
		return "currencyINR"
	case "CHF":
		return "currencyCHF"
	}
	return "currencyUSD"
}

// parseSlayerTime accepts the few date/timestamp shapes SLayer emits depending
// on column type and granularity. Returns (time.Time, error) — caller treats
// parse failures as null.
func parseSlayerTime(s string) (time.Time, error) {
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable time: %q", s)
}
