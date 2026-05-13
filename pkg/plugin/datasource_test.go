package plugin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/motleyai/grafana-slayer-datasource/pkg/slayer"
)

type fakeClient struct {
	resp      *slayer.Response
	queryErr  error
	healthErr error
	models    []slayer.ModelInfo
	modelsErr error
	gotQuery  slayer.Query
}

func (f *fakeClient) Query(_ context.Context, q slayer.Query) (*slayer.Response, error) {
	f.gotQuery = q
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return f.resp, nil
}
func (f *fakeClient) Models(_ context.Context) ([]slayer.ModelInfo, error) {
	return f.models, f.modelsErr
}
func (f *fakeClient) Health(_ context.Context) error { return f.healthErr }

func sampleResp() *slayer.Response {
	return &slayer.Response{
		Data: []map[string]interface{}{
			{"orders.status": "completed", "orders._count": float64(47)},
			{"orders.status": "pending", "orders._count": float64(12)},
		},
		RowCount: 2,
		Columns:  []string{"orders.status", "orders._count"},
		Attributes: &slayer.Attributes{
			Dimensions: map[string]slayer.FieldMetadata{"orders.status": {Label: "Status"}},
			Measures:   map[string]slayer.FieldMetadata{"orders._count": {Label: "Order count"}},
		},
	}
}

func newDS(fc *fakeClient) *Datasource {
	return &Datasource{client: fc, url: "http://slayer:5143"}
}

func TestQueryData_BuildsFrameFromResponse(t *testing.T) {
	fc := &fakeClient{resp: sampleResp()}
	// Send a query that names the same dim+measure as sampleResp() so the new
	// column-ordering pass can match by name.
	js, _ := json.Marshal(slayer.Query{
		SourceModel: "orders",
		Dimensions:  []map[string]interface{}{{"name": "status"}},
		Measures:    []map[string]interface{}{{"formula": "*:count"}},
	})
	resp, err := newDS(fc).QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{RefID: "A", JSON: js}},
	})
	if err != nil {
		t.Fatalf("QueryData: %v", err)
	}
	r := resp.Responses["A"]
	if r.Error != nil {
		t.Fatalf("response error: %v", r.Error)
	}
	f := r.Frames[0]
	if len(f.Fields) != 2 {
		t.Fatalf("fields = %d", len(f.Fields))
	}
	if f.Fields[0].Name != "orders.status" || f.Fields[1].Name != "orders._count" {
		t.Errorf("field names = [%q, %q]", f.Fields[0].Name, f.Fields[1].Name)
	}
	if f.Fields[0].Config.DisplayName != "Status" || f.Fields[1].Config.DisplayName != "Order count" {
		t.Errorf("display names wrong: %q / %q", f.Fields[0].Config.DisplayName, f.Fields[1].Config.DisplayName)
	}
}

func TestQueryData_DefaultsToSampleWhenEmpty(t *testing.T) {
	fc := &fakeClient{resp: sampleResp()}
	_, _ = newDS(fc).QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{RefID: "A"}},
	})
	if fc.gotQuery.SourceModel != "orders" || len(fc.gotQuery.Measures) == 0 || len(fc.gotQuery.Dimensions) == 0 {
		t.Errorf("unexpected default query: %+v", fc.gotQuery)
	}
}

func TestQueryData_PassesQueryThrough(t *testing.T) {
	fc := &fakeClient{resp: sampleResp()}
	js, _ := json.Marshal(slayer.Query{
		SourceModel: "customers",
		Measures:    []map[string]interface{}{{"formula": "*:count"}},
	})
	_, _ = newDS(fc).QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{RefID: "A", JSON: js}},
	})
	if fc.gotQuery.SourceModel != "customers" {
		t.Errorf("source_model = %q", fc.gotQuery.SourceModel)
	}
}

func TestInjectTimeVariables_AlwaysPopulatesStandardKeys(t *testing.T) {
	fc := &fakeClient{resp: sampleResp()}
	from := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	to := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	_, _ = newDS(fc).QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{
			RefID:     "A",
			TimeRange: backend.TimeRange{From: from, To: to},
			Interval:  time.Minute,
		}},
	})
	v := fc.gotQuery.Variables
	if v == nil {
		t.Fatal("Variables not set")
	}
	if got, ok := v["__from"].(string); !ok || got != "2026-05-01T12:00:00Z" {
		t.Errorf("__from = %v (%T)", v["__from"], v["__from"])
	}
	if got, ok := v["__to"].(string); !ok || got != "2026-05-13T12:00:00Z" {
		t.Errorf("__to = %v", v["__to"])
	}
	if v["__from_ms"].(int64) != from.UnixMilli() {
		t.Errorf("__from_ms wrong: %v", v["__from_ms"])
	}
	if v["__interval_ms"].(int64) != 60000 {
		t.Errorf("__interval_ms = %v, want 60000", v["__interval_ms"])
	}
}

func TestInjectTimeVariables_CallerWins(t *testing.T) {
	// User pre-set __from in the query — we must not overwrite it.
	fc := &fakeClient{resp: sampleResp()}
	js, _ := json.Marshal(slayer.Query{
		SourceModel: "orders",
		Variables:   map[string]interface{}{"__from": "user-value"},
	})
	_, _ = newDS(fc).QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{
			RefID: "A", JSON: js,
			TimeRange: backend.TimeRange{From: time.Now(), To: time.Now()},
		}},
	})
	if got := fc.gotQuery.Variables["__from"]; got != "user-value" {
		t.Errorf("__from overwritten: %v", got)
	}
}

func TestMaybeInjectTimeFilter_AddsWhenTimeDimensionPresent(t *testing.T) {
	fc := &fakeClient{resp: sampleResp()}
	js, _ := json.Marshal(slayer.Query{
		SourceModel: "orders",
		TimeDimensions: []map[string]interface{}{
			{"dimension": "ordered_at", "granularity": "day"},
		},
	})
	_, _ = newDS(fc).QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{
			RefID: "A", JSON: js,
			TimeRange: backend.TimeRange{From: time.Now(), To: time.Now()},
		}},
	})
	if len(fc.gotQuery.Filters) != 1 {
		t.Fatalf("filters = %v", fc.gotQuery.Filters)
	}
	if fc.gotQuery.Filters[0] != "ordered_at >= '{__from}' AND ordered_at <= '{__to}'" {
		t.Errorf("filter = %q", fc.gotQuery.Filters[0])
	}
}

func TestMaybeInjectTimeFilter_RespectsExistingMacroReference(t *testing.T) {
	fc := &fakeClient{resp: sampleResp()}
	js, _ := json.Marshal(slayer.Query{
		SourceModel: "orders",
		TimeDimensions: []map[string]interface{}{
			{"dimension": "ordered_at", "granularity": "day"},
		},
		Filters: []string{"ordered_at > '{__from}' - INTERVAL 1 DAY"},
	})
	_, _ = newDS(fc).QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{RefID: "A", JSON: js}},
	})
	if len(fc.gotQuery.Filters) != 1 {
		t.Errorf("auto-injection should have yielded; got %v", fc.gotQuery.Filters)
	}
}

func TestMaybeInjectTimeFilter_SkipsWhenNoTimeDim(t *testing.T) {
	fc := &fakeClient{resp: sampleResp()}
	js, _ := json.Marshal(slayer.Query{SourceModel: "orders"})
	_, _ = newDS(fc).QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{RefID: "A", JSON: js}},
	})
	if len(fc.gotQuery.Filters) != 0 {
		t.Errorf("unexpected filter injection: %v", fc.gotQuery.Filters)
	}
}

func TestToFrame_TimeDimensionParsedAsTime(t *testing.T) {
	fc := &fakeClient{resp: &slayer.Response{
		Data: []map[string]interface{}{
			{"orders.ordered_at": "2026-04-13T00:00:00", "orders._count": float64(767)},
			{"orders.ordered_at": "2026-04-14T00:00:00", "orders._count": float64(278)},
		},
		Columns: []string{"orders.ordered_at", "orders._count"},
		Attributes: &slayer.Attributes{
			Measures: map[string]slayer.FieldMetadata{"orders._count": {}},
		},
	}}
	js, _ := json.Marshal(slayer.Query{
		SourceModel:    "orders",
		TimeDimensions: []map[string]interface{}{{"dimension": "ordered_at", "granularity": "day"}},
	})
	resp, _ := newDS(fc).QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{
			RefID: "A", JSON: js,
			TimeRange: backend.TimeRange{From: time.Now(), To: time.Now()},
		}},
	})
	f := resp.Responses["A"].Frames[0]
	if f.Fields[0].Type() != data.FieldTypeNullableTime {
		t.Errorf("ordered_at field type = %v, want NullableTime", f.Fields[0].Type())
	}
	if f.Fields[1].Type() != data.FieldTypeNullableFloat64 {
		t.Errorf("_count field type = %v, want NullableFloat64", f.Fields[1].Type())
	}
}

func TestToFrame_ColumnsFollowQueryOrder(t *testing.T) {
	// SLayer returns columns alphabetically — `_count` before `aov` before
	// `order_total_sum` before `stores.name`. We want the user's query order:
	// dimensions first, then measures in spec order.
	fc := &fakeClient{resp: &slayer.Response{
		Data: []map[string]interface{}{
			{
				"orders._count":           float64(47),
				"orders.aov":              float64(11.0),
				"orders.order_total_sum":  float64(517.0),
				"orders.stores.name":      "Brooklyn",
			},
		},
		Columns: []string{
			"orders._count",
			"orders.aov",
			"orders.order_total_sum",
			"orders.stores.name",
		},
		Attributes: &slayer.Attributes{
			Measures: map[string]slayer.FieldMetadata{
				"orders._count":          {},
				"orders.aov":             {},
				"orders.order_total_sum": {},
			},
		},
	}}
	js, _ := json.Marshal(slayer.Query{
		SourceModel: "orders",
		Measures: []map[string]interface{}{
			{"formula": "*:count"},
			{"formula": "order_total:sum"},
			{"formula": "order_total:sum / *:count", "name": "aov"},
		},
		Dimensions: []map[string]interface{}{{"name": "stores.name"}},
	})
	resp, _ := newDS(fc).QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{RefID: "A", JSON: js}},
	})
	f := resp.Responses["A"].Frames[0]
	got := make([]string, len(f.Fields))
	for i, fld := range f.Fields {
		got[i] = fld.Name
	}
	want := []string{
		"orders.stores.name",     // dim first
		"orders._count",          // measures in query order — canonical name for *:count
		"orders.order_total_sum", // canonical name for order_total:sum
		"orders.aov",             // explicitly named, compound formula
	}
	if !equalStrings(got, want) {
		t.Errorf("column order:\n  got  %v\n  want %v", got, want)
	}
}

func TestOrderColumns_UnrecognizedFieldsTrail(t *testing.T) {
	// A column SLayer materializes that the query doesn't mention (e.g. an
	// intermediate from a compound formula whose dependencies leak as columns)
	// should still appear in the frame, just last.
	q := &slayer.Query{
		SourceModel: "orders",
		Measures:    []map[string]interface{}{{"formula": "*:count"}},
		Dimensions:  []map[string]interface{}{{"name": "store_id"}},
	}
	cols := []string{"orders._count", "orders.unexpected", "orders.store_id"}
	got := orderColumns(q, cols)
	want := []string{"orders.store_id", "orders._count", "orders.unexpected"}
	if !equalStrings(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestApplyFormatToConfig_TypeMapping(t *testing.T) {
	intPtr := func(i int) *int { return &i }
	strPtr := func(s string) *string { return &s }

	cases := []struct {
		name     string
		fmt      *slayer.NumberFormat
		wantUnit string
		wantDec  *uint16
	}{
		{"nil format leaves config unchanged", nil, "", nil},
		{"integer → short", &slayer.NumberFormat{Type: "integer"}, "short", nil},
		{"float → short", &slayer.NumberFormat{Type: "float"}, "short", nil},
		{"percent → percent", &slayer.NumberFormat{Type: "percent"}, "percent", nil},
		{"currency default symbol → USD", &slayer.NumberFormat{Type: "currency"}, "currencyUSD", nil},
		{"currency $", &slayer.NumberFormat{Type: "currency", Symbol: strPtr("$")}, "currencyUSD", nil},
		{"currency €", &slayer.NumberFormat{Type: "currency", Symbol: strPtr("€")}, "currencyEUR", nil},
		{"currency £", &slayer.NumberFormat{Type: "currency", Symbol: strPtr("£")}, "currencyGBP", nil},
		{"currency CHF", &slayer.NumberFormat{Type: "currency", Symbol: strPtr("CHF")}, "currencyCHF", nil},
		{"currency unknown → USD fallback", &slayer.NumberFormat{Type: "currency", Symbol: strPtr("XYZ")}, "currencyUSD", nil},
		{"unknown type leaves unit unset", &slayer.NumberFormat{Type: "weird"}, "", nil},
		{"precision propagates as decimals", &slayer.NumberFormat{Type: "float", Precision: intPtr(3)}, "short", func() *uint16 { v := uint16(3); return &v }()},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cfg := &data.FieldConfig{}
			applyFormatToConfig(cfg, c.fmt)
			if cfg.Unit != c.wantUnit {
				t.Errorf("unit = %q, want %q", cfg.Unit, c.wantUnit)
			}
			if (cfg.Decimals == nil) != (c.wantDec == nil) {
				t.Errorf("decimals presence = %v, want %v", cfg.Decimals != nil, c.wantDec != nil)
			} else if cfg.Decimals != nil && c.wantDec != nil && *cfg.Decimals != *c.wantDec {
				t.Errorf("decimals = %d, want %d", *cfg.Decimals, *c.wantDec)
			}
		})
	}
}

func TestBuildField_AppliesFormatHint(t *testing.T) {
	// A measure with SLayer-supplied format metadata should land Unit and
	// Decimals on the Grafana FieldConfig automatically.
	symbol := "€"
	precision := 2
	resp := &slayer.Response{
		Data: []map[string]interface{}{{"orders.revenue_sum": float64(99.5)}},
		Columns: []string{"orders.revenue_sum"},
		Attributes: &slayer.Attributes{
			Measures: map[string]slayer.FieldMetadata{
				"orders.revenue_sum": {
					Label: "Revenue",
					Format: &slayer.NumberFormat{
						Type:      "currency",
						Symbol:    &symbol,
						Precision: &precision,
					},
				},
			},
		},
	}
	field := buildField("orders.revenue_sum", resp, false)
	if field.Config == nil {
		t.Fatal("expected FieldConfig to be set when format hint present")
	}
	if field.Config.Unit != "currencyEUR" {
		t.Errorf("Unit = %q, want currencyEUR", field.Config.Unit)
	}
	if field.Config.Decimals == nil || *field.Config.Decimals != 2 {
		t.Errorf("Decimals = %v, want 2", field.Config.Decimals)
	}
	if field.Config.DisplayName != "Revenue" {
		t.Errorf("DisplayName = %q", field.Config.DisplayName)
	}
}

func TestCanonicalMeasureSuffix(t *testing.T) {
	cases := map[string]string{
		"*:count":                     "_count",
		"*:count_distinct":            "_count_distinct",
		"revenue:sum":                 "revenue_sum",
		"order_total:max":             "order_total_max",
		"cumsum(revenue:sum)":         "",
		"revenue:sum / *:count":       "",
		"change_pct(revenue:sum)":     "",
		"justakey":                    "",
	}
	for in, want := range cases {
		if got := canonicalMeasureSuffix(in); got != want {
			t.Errorf("canonicalMeasureSuffix(%q) = %q, want %q", in, got, want)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestQueryData_PreservesSourceQueries(t *testing.T) {
	// Nested SlayerQuery (queries-as-models) must round-trip through the Go
	// type without losing fields — otherwise multi-stage queries from the
	// frontend silently degrade to a single-stage one.
	fc := &fakeClient{resp: sampleResp()}
	rawJSON := []byte(`{
		"measures": [{"formula": "rev:avg"}],
		"source_queries": [{
			"name": "by_store",
			"source_model": "orders",
			"measures": [{"formula": "order_total:sum", "name": "rev"}],
			"dimensions": [{"name": "store_id"}]
		}]
	}`)
	_, _ = newDS(fc).QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{
			RefID: "A", JSON: rawJSON,
			TimeRange: backend.TimeRange{From: time.Now(), To: time.Now()},
		}},
	})
	if len(fc.gotQuery.SourceQueries) == 0 {
		t.Fatal("source_queries not passed through to slayer client")
	}
	var inner []map[string]interface{}
	if err := json.Unmarshal(fc.gotQuery.SourceQueries, &inner); err != nil {
		t.Fatalf("source_queries didn't survive as JSON: %v", err)
	}
	if len(inner) != 1 || inner[0]["name"] != "by_store" {
		t.Errorf("inner stage altered or dropped: %+v", inner)
	}
}

func TestCheckHealth_OK(t *testing.T) {
	res, _ := newDS(&fakeClient{}).CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if res.Status != backend.HealthStatusOk {
		t.Errorf("status = %v", res.Status)
	}
}

func TestCheckHealth_Error(t *testing.T) {
	res, _ := newDS(&fakeClient{healthErr: context.Canceled}).CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if res.Status != backend.HealthStatusError {
		t.Errorf("status = %v", res.Status)
	}
}

// captureSender records the last CallResourceResponse sent by the handler.
type captureSender struct{ resp *backend.CallResourceResponse }

func (c *captureSender) Send(r *backend.CallResourceResponse) error { c.resp = r; return nil }

func TestCallResource_Models(t *testing.T) {
	fc := &fakeClient{models: []slayer.ModelInfo{{Name: "orders", DataSource: "jaffle_shop"}}}
	cs := &captureSender{}
	_ = newDS(fc).CallResource(context.Background(), &backend.CallResourceRequest{Path: "models"}, cs)
	if cs.resp.Status != 200 {
		t.Fatalf("status = %d, body=%s", cs.resp.Status, string(cs.resp.Body))
	}
	var got []slayer.ModelInfo
	if err := json.Unmarshal(cs.resp.Body, &got); err != nil || len(got) != 1 || got[0].Name != "orders" {
		t.Errorf("body = %s (err=%v)", string(cs.resp.Body), err)
	}
}

func TestCallResource_MetricFind(t *testing.T) {
	fc := &fakeClient{resp: &slayer.Response{
		Data: []map[string]interface{}{
			{"orders.store_id": "store-a"},
			{"orders.store_id": "store-b"},
		},
		Columns: []string{"orders.store_id"},
	}}
	body, _ := json.Marshal(slayer.Query{
		SourceModel: "orders",
		Dimensions:  []map[string]interface{}{{"name": "store_id"}},
	})
	cs := &captureSender{}
	_ = newDS(fc).CallResource(context.Background(), &backend.CallResourceRequest{
		Path: "metric-find",
		Body: body,
	}, cs)
	if cs.resp.Status != 200 {
		t.Fatalf("status = %d, body=%s", cs.resp.Status, string(cs.resp.Body))
	}
	var got []map[string]interface{}
	_ = json.Unmarshal(cs.resp.Body, &got)
	if len(got) != 2 || got[0]["text"] != "store-a" {
		t.Errorf("body = %s", string(cs.resp.Body))
	}
}

func TestCallResource_MetricFind_RejectsEmptyQuery(t *testing.T) {
	cs := &captureSender{}
	_ = newDS(&fakeClient{}).CallResource(context.Background(), &backend.CallResourceRequest{
		Path: "metric-find",
		Body: []byte(`{}`),
	}, cs)
	if cs.resp.Status != 400 {
		t.Errorf("status = %d, want 400", cs.resp.Status)
	}
}

func TestCallResource_Unknown(t *testing.T) {
	cs := &captureSender{}
	_ = newDS(&fakeClient{}).CallResource(context.Background(), &backend.CallResourceRequest{Path: "nope"}, cs)
	if cs.resp.Status != 404 {
		t.Errorf("status = %d, want 404", cs.resp.Status)
	}
}

