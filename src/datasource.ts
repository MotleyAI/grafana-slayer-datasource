import { DataSourceInstanceSettings, CoreApp, ScopedVars, MetricFindValue } from '@grafana/data';
import { DataSourceWithBackend, getTemplateSrv } from '@grafana/runtime';

import { DEFAULT_QUERY, SlayerOptions, SlayerQuery, SlayerSourceModel } from './types';

export class DataSource extends DataSourceWithBackend<SlayerQuery, SlayerOptions> {
  constructor(instanceSettings: DataSourceInstanceSettings<SlayerOptions>) {
    super(instanceSettings);
  }

  getDefaultQuery(_: CoreApp): Partial<SlayerQuery> {
    return DEFAULT_QUERY;
  }

  // Interpolate Grafana template variables ($var / ${var}) inside string
  // fields before the query is shipped to the backend. SLayer's own `{__from}`
  // substitution happens server-side and uses different delimiters, so the
  // two passes don't conflict.
  applyTemplateVariables(query: SlayerQuery, scopedVars: ScopedVars): SlayerQuery {
    const t = getTemplateSrv();
    const interp = (s?: string) => (s ? t.replace(s, scopedVars) : s);
    // source_model may be a model-name string OR an inline SlayerModel /
    // ModelExtension object — only interpolate the string form.
    const sm: SlayerSourceModel | undefined =
      typeof query.source_model === 'string' ? interp(query.source_model) : query.source_model;
    return {
      ...query,
      source_model: sm,
      filters: query.filters?.map((f) => interp(f)!),
    };
  }

  filterQuery(query: SlayerQuery): boolean {
    return !!(query.source_model || query.name);
  }

  // Populate Grafana dashboard template variables. The variable definition is a
  // JSON-encoded SlayerQuery; the backend's /resources/metric-find runs it and
  // projects the first column's distinct values into MetricFindValue[].
  async metricFindQuery(queryStr: string): Promise<MetricFindValue[]> {
    if (!queryStr || queryStr.trim() === '') {
      return [];
    }
    let parsed: unknown;
    try {
      parsed = JSON.parse(queryStr);
    } catch (err) {
      throw new Error(`SLayer template variable query must be JSON SlayerQuery (got: ${(err as Error).message})`);
    }
    const body = await this.postResource('metric-find', parsed);
    return Array.isArray(body) ? (body as MetricFindValue[]) : [];
  }
}
