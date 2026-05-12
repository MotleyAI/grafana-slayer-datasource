package plugin

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/motleyai/grafana-slayer-datasource/pkg/slayer"
)

type fakeClient struct {
	resp      *slayer.Response
	queryErr  error
	healthErr error
	gotQuery  slayer.Query
}

func (f *fakeClient) Query(_ context.Context, q slayer.Query) (*slayer.Response, error) {
	f.gotQuery = q
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return f.resp, nil
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

func TestQueryData_BuildsFrameFromResponse(t *testing.T) {
	fc := &fakeClient{resp: sampleResp()}
	ds := &Datasource{client: fc, url: "test"}

	resp, err := ds.QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{RefID: "A"}},
	})
	if err != nil {
		t.Fatalf("QueryData: %v", err)
	}
	r, ok := resp.Responses["A"]
	if !ok {
		t.Fatal("missing RefID A in response")
	}
	if r.Error != nil {
		t.Fatalf("response error: %v", r.Error)
	}
	if len(r.Frames) != 1 {
		t.Fatalf("frames = %d, want 1", len(r.Frames))
	}
	f := r.Frames[0]
	if f.RefID != "A" {
		t.Errorf("frame RefID = %q", f.RefID)
	}
	if len(f.Fields) != 2 {
		t.Fatalf("fields = %d, want 2", len(f.Fields))
	}
	if f.Fields[0].Name != "orders.status" || f.Fields[1].Name != "orders._count" {
		t.Errorf("field names = [%q, %q]", f.Fields[0].Name, f.Fields[1].Name)
	}
	if f.Fields[0].Config.DisplayName != "Status" {
		t.Errorf("status display name = %q", f.Fields[0].Config.DisplayName)
	}
	if f.Fields[1].Config.DisplayName != "Order count" {
		t.Errorf("count display name = %q", f.Fields[1].Config.DisplayName)
	}
	if f.Fields[1].Len() != 2 {
		t.Errorf("count field len = %d", f.Fields[1].Len())
	}
}

func TestQueryData_DefaultsToSampleWhenEmpty(t *testing.T) {
	fc := &fakeClient{resp: sampleResp()}
	ds := &Datasource{client: fc, url: "test"}

	_, _ = ds.QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{RefID: "A"}},
	})
	if fc.gotQuery.SourceModel != "orders" {
		t.Errorf("default source_model = %q, want orders", fc.gotQuery.SourceModel)
	}
	if len(fc.gotQuery.Measures) == 0 {
		t.Error("default query should have ≥1 measure")
	}
	if len(fc.gotQuery.Dimensions) == 0 {
		t.Error("default query should have ≥1 dimension")
	}
}

func TestQueryData_PassesQueryThrough(t *testing.T) {
	fc := &fakeClient{resp: sampleResp()}
	ds := &Datasource{client: fc, url: "test"}

	js, _ := json.Marshal(slayer.Query{
		SourceModel: "customers",
		Measures:    []map[string]interface{}{{"formula": "*:count"}},
	})
	_, _ = ds.QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{RefID: "A", JSON: js}},
	})
	if fc.gotQuery.SourceModel != "customers" {
		t.Errorf("source_model = %q, want customers", fc.gotQuery.SourceModel)
	}
}

func TestCheckHealth_OK(t *testing.T) {
	ds := &Datasource{client: &fakeClient{}, url: "http://x"}
	res, err := ds.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if err != nil {
		t.Fatalf("CheckHealth: %v", err)
	}
	if res.Status != backend.HealthStatusOk {
		t.Errorf("status = %v, want OK", res.Status)
	}
}

func TestCheckHealth_Error(t *testing.T) {
	ds := &Datasource{client: &fakeClient{healthErr: context.Canceled}, url: "http://x"}
	res, err := ds.CheckHealth(context.Background(), &backend.CheckHealthRequest{})
	if err != nil {
		t.Fatalf("CheckHealth: %v", err)
	}
	if res.Status != backend.HealthStatusError {
		t.Errorf("status = %v, want Error", res.Status)
	}
}
