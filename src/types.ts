import { DataSourceJsonData } from '@grafana/data';
import { DataQuery } from '@grafana/schema';

// Mirrors pkg/slayer.Query (Go). Keep in sync — they're two halves of the same
// wire contract. See SLayer's REST API docs:
// https://motley-slayer.readthedocs.io/en/latest/concepts/queries/

export interface SlayerMeasure {
  formula: string;
  name?: string;
  label?: string;
}

export interface SlayerDimension {
  name: string;
  label?: string;
}

export interface SlayerTimeDimension {
  dimension: string;
  granularity?: string;
}

// `source_model` accepts a plain model name (string) OR an inline
// `SlayerModel` / `ModelExtension` (dict) — see SLayer's SlayerQuery schema.
// We type the inline form loosely; SLayer remains the validator.
export type SlayerSourceModel = string | Record<string, unknown>;

export interface SlayerQuery extends DataQuery {
  source_model?: SlayerSourceModel;
  // Run-by-name: invoke a stored query-backed model. Mutually exclusive with
  // source_model + other query fields.
  name?: string;
  // Multi-stage / queries-as-models — each entry is itself a SlayerQuery and
  // can reference prior siblings by their `name`. See SLayer docs:
  // https://motley-slayer.readthedocs.io/en/latest/examples/06_multistage_queries/
  source_queries?: SlayerQuery[];
  measures?: SlayerMeasure[];
  dimensions?: SlayerDimension[];
  time_dimensions?: SlayerTimeDimension[];
  filters?: string[];
  limit?: number;
  offset?: number;
  variables?: Record<string, string | number>;
}

export const DEFAULT_QUERY: Partial<SlayerQuery> = {
  source_model: 'orders',
  measures: [{ formula: '*:count' }],
};

export interface SlayerOptions extends DataSourceJsonData {
  url?: string;
  // Optional override for the Grafana URL the plugin uses when its in-process
  // MCP server calls back into Grafana (defaults to http://localhost:3000 —
  // the plugin runs as a Grafana subprocess so localhost is correct in the
  // common case).
  grafana_url?: string;
}

export interface SlayerSecureOptions {
  // SLayer API key — forward-compat; SLayer ≤0.6.x has no auth.
  apiKey?: string;
  // Grafana service-account token, used by the plugin's MCP CallResource for
  // dashboard writes (POST /api/dashboards/db). Optional when Grafana is in
  // anonymous-Admin mode (the bundled demo). Required in production.
  grafanaToken?: string;
}
