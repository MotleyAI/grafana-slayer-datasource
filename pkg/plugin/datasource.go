package plugin

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/data"
	"github.com/motleyai/grafana-slayer-datasource/pkg/models"
	"github.com/motleyai/grafana-slayer-datasource/pkg/slayer"
)

var (
	_ backend.QueryDataHandler      = (*Datasource)(nil)
	_ backend.CheckHealthHandler    = (*Datasource)(nil)
	_ instancemgmt.InstanceDisposer = (*Datasource)(nil)
)

// slayerClient is the subset of *slayer.Client used by the datasource — kept
// as an interface so tests can substitute a fake.
type slayerClient interface {
	Query(ctx context.Context, q slayer.Query) (*slayer.Response, error)
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
	resp, err := d.client.Query(ctx, qm)
	if err != nil {
		return backend.ErrDataResponse(backend.StatusInternal, fmt.Sprintf("slayer query: %v", err))
	}
	return backend.DataResponse{Frames: data.Frames{toFrame(query.RefID, resp)}}
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

// toFrame converts a SLayer query response into a Grafana data.Frame.
// v0: table-shaped — measures as nullable float64, everything else as nullable
// strings. Time-series pivoting and FieldMetadata.format → Grafana-unit mapping
// land in a follow-up milestone.
func toFrame(refID string, resp *slayer.Response) *data.Frame {
	frame := data.NewFrame("response")
	frame.RefID = refID
	for _, col := range resp.Columns {
		frame.Fields = append(frame.Fields, buildField(col, resp))
	}
	return frame
}

func buildField(col string, resp *slayer.Response) *data.Field {
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
	if isMeasure {
		values := make([]*float64, len(resp.Data))
		for i, row := range resp.Data {
			if v, ok := row[col]; ok && v != nil {
				if f, ok := v.(float64); ok {
					values[i] = &f
				}
			}
		}
		field = data.NewField(col, nil, values)
	} else {
		values := make([]*string, len(resp.Data))
		for i, row := range resp.Data {
			if v, ok := row[col]; ok && v != nil {
				s := fmt.Sprintf("%v", v)
				values[i] = &s
			}
		}
		field = data.NewField(col, nil, values)
	}
	if meta.Label != "" {
		field.Config = &data.FieldConfig{DisplayName: meta.Label}
	}
	return field
}
