import React, { useMemo, useState } from 'react';
import {
  Button,
  CollapsableSection,
  Field,
  FieldSet,
  InlineField,
  InlineSwitch,
  Input,
  Select,
  Stack,
  TextArea,
} from '@grafana/ui';
import { QueryEditorProps, SelectableValue } from '@grafana/data';
import { DataSource } from '../datasource';
import {
  SlayerDimension,
  SlayerMeasure,
  SlayerOptions,
  SlayerQuery,
  SlayerSourceModel,
  SlayerTimeDimension,
} from '../types';

type Props = QueryEditorProps<DataSource, SlayerQuery, SlayerOptions>;

// SLayer granularity options for the time-dimension dropdown. Empty string is
// "unset" — SLayer treats absence as no truncation.
const GRANULARITIES: Array<SelectableValue<string>> = [
  { label: '(none)', value: '' },
  { label: 'second', value: 'second' },
  { label: 'minute', value: 'minute' },
  { label: 'hour', value: 'hour' },
  { label: 'day', value: 'day' },
  { label: 'week', value: 'week' },
  { label: 'month', value: 'month' },
  { label: 'quarter', value: 'quarter' },
  { label: 'year', value: 'year' },
];

// ──────────────────────────────────────────────────────────────────────────
// textarea ↔ structured-value helpers, one block per editor field.
// ──────────────────────────────────────────────────────────────────────────

const measuresToText = (m?: SlayerMeasure[]) =>
  (m ?? []).map((x) => (x.name ? `${x.formula} AS ${x.name}` : x.formula)).join('\n');

const textToMeasures = (s: string): SlayerMeasure[] =>
  s
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean)
    .map((l) => {
      const m = l.match(/^(.+?)\s+AS\s+(\S+)$/i);
      return m ? { formula: m[1].trim(), name: m[2].trim() } : { formula: l };
    });

const dimensionsToText = (d?: SlayerDimension[]) => (d ?? []).map((x) => x.name).join('\n');
const textToDimensions = (s: string): SlayerDimension[] =>
  s
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean)
    .map((name) => ({ name }));

const timeDimNamesToText = (t?: SlayerTimeDimension[]) =>
  (t ?? []).map((x) => x.dimension).join('\n');

const filtersToText = (f?: string[]) => (f ?? []).join('\n');
const textToFilters = (s: string): string[] =>
  s
    .split('\n')
    .map((l) => l.trim())
    .filter(Boolean);

// stripUiState drops Grafana-internal fields so the JSON view shows only the
// payload SLayer will actually receive.
const UI_KEYS = new Set(['refId', 'hide', 'datasource', 'key', 'queryType']);
function toWireJson(q: SlayerQuery): object {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(q)) {
    if (UI_KEYS.has(k)) {
      continue;
    }
    if (v === undefined || v === null) {
      continue;
    }
    if (Array.isArray(v) && v.length === 0) {
      continue;
    }
    if (typeof v === 'string' && v === '') {
      continue;
    }
    out[k] = v;
  }
  return out;
}

function currentGranularity(q: SlayerQuery): string {
  return q.time_dimensions?.[0]?.granularity ?? '';
}

// Source model can be a plain string (model name) or an inline dict
// (SlayerModel / ModelExtension). UI splits them into two widgets and SLayer
// gets whichever is non-empty; inline JSON wins when both are set.
const sourceModelAsString = (sm?: SlayerSourceModel): string =>
  typeof sm === 'string' ? sm : '';
const sourceModelAsInlineText = (sm?: SlayerSourceModel): string =>
  sm && typeof sm === 'object' ? JSON.stringify(sm, null, 2) : '';

// ──────────────────────────────────────────────────────────────────────────
// StageEditor — the form for a single SlayerQuery, used by both the outer
// query and each entry in source_queries. `showName` is true for stages so
// their `name` (the reference handle from later stages) shows up at the top.
// ──────────────────────────────────────────────────────────────────────────

interface StageEditorProps {
  query: SlayerQuery;
  onChange: (q: SlayerQuery) => void;
  onCommit: () => void; // call after edits to re-run the query
  showName?: boolean;
}

function StageEditor({ query, onChange, onCommit, showName }: StageEditorProps) {
  const [inlineDraft, setInlineDraft] = useState<string | null>(null);
  const [inlineError, setInlineError] = useState<string | null>(null);

  const update = (patch: Partial<SlayerQuery>) => onChange({ ...query, ...patch });

  const writeTimeDims = (names: string[], gran: string) => {
    update({
      time_dimensions: names.map((dim) =>
        gran ? { dimension: dim, granularity: gran } : { dimension: dim }
      ),
    });
  };
  const onTimeDimNamesChange = (text: string) => {
    const names = text
      .split('\n')
      .map((l) => l.trim())
      .filter(Boolean);
    writeTimeDims(names, currentGranularity(query));
  };
  const onGranularityChange = (gran: string) => {
    const names = (query.time_dimensions ?? []).map((td) => td.dimension);
    writeTimeDims(names, gran);
    onCommit();
  };

  const inlineText = inlineDraft ?? sourceModelAsInlineText(query.source_model);
  const commitInline = () => {
    if (inlineDraft === null) {
      return;
    }
    const trimmed = inlineDraft.trim();
    if (trimmed === '') {
      // Inline cleared → fall back to the plain-string source_model (or undefined).
      update({ source_model: sourceModelAsString(query.source_model) || undefined });
      setInlineDraft(null);
      setInlineError(null);
      onCommit();
      return;
    }
    try {
      const parsed = JSON.parse(trimmed);
      if (parsed === null || typeof parsed !== 'object' || Array.isArray(parsed)) {
        setInlineError('Inline source model must be a JSON object.');
        return;
      }
      update({ source_model: parsed as Record<string, unknown> });
      setInlineDraft(null);
      setInlineError(null);
      onCommit();
    } catch (err) {
      setInlineError(`Invalid JSON: ${(err as Error).message}`);
    }
  };

  return (
    <>
      {showName && (
        <InlineField
          label="Sub-query name"
          labelWidth={20}
          tooltip="Used as a reference handle: other sub-queries or the outer query can set `Model` to this name. SLayer auto-sorts the DAG, so the order in this list doesn't matter — references do."
        >
          <Input
            width={40}
            value={query.name ?? ''}
            onChange={(e) => update({ name: e.currentTarget.value || undefined })}
            onBlur={onCommit}
            placeholder="e.g. customer_first"
          />
        </InlineField>
      )}

      <InlineField
        label="Model"
        labelWidth={20}
        tooltip="SLayer model name (e.g. 'orders') or the name of a prior stage."
      >
        <Input
          width={40}
          value={sourceModelAsString(query.source_model)}
          onChange={(e) =>
            // Setting the string clears any inline dict — they're alternatives.
            update({ source_model: e.currentTarget.value || undefined })
          }
          onBlur={onCommit}
          placeholder="orders"
        />
      </InlineField>

      <Field
        label="Measures"
        description="One per line. Use 'formula' or 'formula AS name'. Examples: *:count, revenue:sum, cumsum(revenue:sum) AS running_rev"
      >
        <TextArea
          rows={3}
          value={measuresToText(query.measures)}
          onChange={(e) => update({ measures: textToMeasures(e.currentTarget.value) })}
          onBlur={onCommit}
          placeholder="*:count"
        />
      </Field>

      <Field
        label="Dimensions"
        description="One column per line. For joined dims, use dotted syntax (customers.name)."
      >
        <TextArea
          rows={2}
          value={dimensionsToText(query.dimensions)}
          onChange={(e) => update({ dimensions: textToDimensions(e.currentTarget.value) })}
          onBlur={onCommit}
          placeholder="store_id"
        />
      </Field>

      <Field
        label="Time dimensions"
        description="One column name per line — the granularity below applies to all entries."
      >
        <TextArea
          rows={2}
          value={timeDimNamesToText(query.time_dimensions)}
          onChange={(e) => onTimeDimNamesChange(e.currentTarget.value)}
          onBlur={onCommit}
          placeholder="ordered_at"
        />
      </Field>

      <InlineField
        label="Granularity"
        labelWidth={20}
        tooltip="Bucket size applied to every time dimension above."
      >
        <Select
          width={20}
          value={currentGranularity(query)}
          options={GRANULARITIES}
          onChange={(v) => onGranularityChange(v?.value ?? '')}
        />
      </InlineField>

      <Field
        label="Filters"
        description="One filter expression per line. Dashboard time range is auto-injected when a time dimension is set; reference {__from} / {__to} explicitly to opt out."
      >
        <TextArea
          rows={2}
          value={filtersToText(query.filters)}
          onChange={(e) => update({ filters: textToFilters(e.currentTarget.value) })}
          onBlur={onCommit}
          placeholder="status == 'completed'"
        />
      </Field>

      <InlineField label="Limit" labelWidth={20} tooltip="Optional row limit applied by SLayer.">
        <Input
          type="number"
          width={20}
          value={query.limit ?? ''}
          onChange={(e) =>
            update({
              limit: e.currentTarget.value ? Number(e.currentTarget.value) : undefined,
            })
          }
          onBlur={onCommit}
        />
      </InlineField>

      <Field
        label="Inline source model (advanced)"
        description="Optional JSON for sub-queries that need an inline SlayerModel or ModelExtension — e.g. extra joins or derived columns. When non-empty, takes precedence over the plain Model field above."
        invalid={!!inlineError}
        error={inlineError ?? undefined}
      >
        <TextArea
          rows={4}
          value={inlineText}
          onChange={(e) => setInlineDraft(e.currentTarget.value)}
          onBlur={commitInline}
          placeholder='{"source_name": "orders", "joins": [...], "columns": [...]}'
        />
      </Field>
    </>
  );
}

// ──────────────────────────────────────────────────────────────────────────
// SourceQueriesEditor — list of nested stages. Each rendered as a collapsible
// section containing a full StageEditor; the parent gets add / remove / move
// controls.
// ──────────────────────────────────────────────────────────────────────────

interface SourceQueriesEditorProps {
  stages: SlayerQuery[];
  onChange: (stages: SlayerQuery[] | undefined) => void;
  onCommit: () => void;
}

function SourceQueriesEditor({ stages, onChange, onCommit }: SourceQueriesEditorProps) {
  const updateAt = (i: number, stage: SlayerQuery) => {
    const next = stages.slice();
    next[i] = stage;
    onChange(next);
  };
  const removeAt = (i: number) => {
    const next = stages.slice();
    next.splice(i, 1);
    onChange(next.length ? next : undefined);
    onCommit();
  };
  const moveAt = (i: number, delta: number) => {
    const j = i + delta;
    if (j < 0 || j >= stages.length) {
      return;
    }
    const next = stages.slice();
    [next[i], next[j]] = [next[j], next[i]];
    onChange(next);
    onCommit();
  };
  const addStage = () => {
    onChange([...stages, { refId: '', name: '' } as SlayerQuery]);
  };

  return (
    <>
      {stages.length === 0 && (
        <div style={{ marginBottom: 8, opacity: 0.7 }}>
          No sub-queries — single-query execution. Click <em>Add sub-query</em> to build a DAG;
          the outer form above is the entry point, and any sub-query referenced by name from
          there (or transitively) gets included.
        </div>
      )}
      {stages.map((stage, i) => (
        <CollapsableSection
          key={i}
          isOpen
          label={`Sub-query ${i + 1}${stage.name ? ` — ${stage.name}` : ''}`}
        >
          <StageEditor
            query={stage}
            onChange={(s) => updateAt(i, s)}
            onCommit={onCommit}
            showName
          />
          <Stack gap={1} direction="row">
            <Button size="sm" variant="secondary" onClick={() => moveAt(i, -1)} disabled={i === 0}>
              ↑ Move up
            </Button>
            <Button
              size="sm"
              variant="secondary"
              onClick={() => moveAt(i, 1)}
              disabled={i === stages.length - 1}
            >
              ↓ Move down
            </Button>
            <Button size="sm" variant="destructive" onClick={() => removeAt(i)}>
              Remove sub-query
            </Button>
          </Stack>
        </CollapsableSection>
      ))}
      <div style={{ marginTop: 8 }}>
        <Button size="sm" variant="secondary" onClick={addStage}>
          + Add sub-query
        </Button>
      </div>
    </>
  );
}

// ──────────────────────────────────────────────────────────────────────────
// Top-level QueryEditor: form/JSON toggle + outer StageEditor + stages list.
// ──────────────────────────────────────────────────────────────────────────

export function QueryEditor({ query, onChange, onRunQuery }: Props) {
  const [jsonMode, setJsonMode] = useState(false);
  const [jsonDraft, setJsonDraft] = useState<string | null>(null);
  const [jsonError, setJsonError] = useState<string | null>(null);

  const enterJsonMode = () => {
    setJsonDraft(JSON.stringify(toWireJson(query), null, 2));
    setJsonError(null);
    setJsonMode(true);
  };
  const exitJsonMode = () => {
    setJsonDraft(null);
    setJsonError(null);
    setJsonMode(false);
  };
  const commitJson = () => {
    if (jsonDraft === null) {
      return;
    }
    try {
      const parsed = JSON.parse(jsonDraft);
      onChange({ ...parsed, refId: query.refId });
      setJsonError(null);
      onRunQuery();
    } catch (err) {
      setJsonError(`Invalid JSON: ${(err as Error).message}`);
    }
  };
  const wirePreview = useMemo(() => JSON.stringify(toWireJson(query), null, 2), [query]);

  return (
    <FieldSet>
      <InlineField
        label="Editor"
        tooltip="Form covers common queries; JSON mode lets you write any SlayerQuery payload directly."
      >
        <InlineSwitch
          value={jsonMode}
          onChange={() => (jsonMode ? exitJsonMode() : enterJsonMode())}
          label={jsonMode ? 'JSON' : 'Form'}
        />
      </InlineField>

      {jsonMode ? (
        <Field
          label="Query JSON"
          description="Raw SlayerQuery payload. See https://motley-slayer.readthedocs.io/en/latest/concepts/queries/"
          invalid={!!jsonError}
          error={jsonError ?? undefined}
        >
          <TextArea
            rows={20}
            value={jsonDraft ?? wirePreview}
            onChange={(e) => setJsonDraft(e.currentTarget.value)}
            onBlur={commitJson}
          />
        </Field>
      ) : (
        <>
          <StageEditor
            query={query}
            onChange={(q) => onChange({ ...q, refId: query.refId })}
            onCommit={onRunQuery}
          />

          <CollapsableSection
            label={`Multi-stage execution${
              query.source_queries?.length
                ? ` — ${query.source_queries.length} sub-quer${query.source_queries.length === 1 ? 'y' : 'ies'}`
                : ''
            }`}
            isOpen={!!query.source_queries?.length}
          >
            <SourceQueriesEditor
              stages={query.source_queries ?? []}
              onChange={(stages) => onChange({ ...query, source_queries: stages })}
              onCommit={onRunQuery}
            />
          </CollapsableSection>
        </>
      )}
    </FieldSet>
  );
}
