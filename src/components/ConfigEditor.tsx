import React, { ChangeEvent } from 'react';
import { InlineField, Input, SecretInput, FieldSet } from '@grafana/ui';
import { DataSourcePluginOptionsEditorProps } from '@grafana/data';
import { SlayerOptions, SlayerSecureOptions } from '../types';

type Props = DataSourcePluginOptionsEditorProps<SlayerOptions, SlayerSecureOptions>;

export function ConfigEditor(props: Props) {
  const { onOptionsChange, options } = props;
  const { jsonData, secureJsonFields, secureJsonData } = options;

  const onURLChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      jsonData: { ...jsonData, url: event.target.value },
    });
  };

  const onAPIKeyChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      secureJsonData: { ...(secureJsonData ?? {}), apiKey: event.target.value },
    });
  };

  const onResetAPIKey = () => {
    onOptionsChange({
      ...options,
      secureJsonFields: { ...(secureJsonFields ?? {}), apiKey: false },
      secureJsonData: { ...(secureJsonData ?? {}), apiKey: '' },
    });
  };

  const onGrafanaURLChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      jsonData: { ...jsonData, grafana_url: event.target.value },
    });
  };

  const onGrafanaTokenChange = (event: ChangeEvent<HTMLInputElement>) => {
    onOptionsChange({
      ...options,
      secureJsonData: { ...(secureJsonData ?? {}), grafanaToken: event.target.value },
    });
  };

  const onResetGrafanaToken = () => {
    onOptionsChange({
      ...options,
      secureJsonFields: { ...(secureJsonFields ?? {}), grafanaToken: false },
      secureJsonData: { ...(secureJsonData ?? {}), grafanaToken: '' },
    });
  };

  return (
    <FieldSet>
      <InlineField
        label="SLayer URL"
        labelWidth={20}
        tooltip="Base URL of the SLayer REST API (e.g. http://localhost:5143 or http://slayer:5143 inside docker-compose)."
      >
        <Input
          id="config-editor-url"
          value={jsonData.url ?? ''}
          onChange={onURLChange}
          placeholder="http://localhost:5143"
          width={50}
        />
      </InlineField>
      <InlineField
        label="API Key"
        labelWidth={20}
        tooltip="Reserved for a future SLayer auth release — SLayer ≤0.6.x has no auth; leave blank."
      >
        <SecretInput
          id="config-editor-api-key"
          isConfigured={secureJsonFields?.apiKey}
          value={secureJsonData?.apiKey ?? ''}
          placeholder="(none)"
          width={50}
          onReset={onResetAPIKey}
          onChange={onAPIKeyChange}
        />
      </InlineField>

      <h6 style={{ marginTop: 16 }}>Agent integration (MCP) — optional</h6>

      <InlineField
        label="Grafana URL"
        labelWidth={20}
        tooltip="URL the plugin's in-process MCP server uses when calling Grafana back (dashboard write API). Leave blank to default to http://localhost:3000."
      >
        <Input
          id="config-editor-grafana-url"
          value={jsonData.grafana_url ?? ''}
          onChange={onGrafanaURLChange}
          placeholder="http://localhost:3000"
          width={50}
        />
      </InlineField>
      <InlineField
        label="Grafana token"
        labelWidth={20}
        tooltip="Grafana service-account token (https://grafana.com/docs/grafana/latest/administration/service-accounts/) used by the plugin's MCP CallResource for dashboard writes. Leave blank for the bundled demo — anonymous Admin / basic-auth env-vars cover it. Required in production."
      >
        <SecretInput
          id="config-editor-grafana-token"
          isConfigured={secureJsonFields?.grafanaToken}
          value={secureJsonData?.grafanaToken ?? ''}
          placeholder="glsa_…"
          width={50}
          onReset={onResetGrafanaToken}
          onChange={onGrafanaTokenChange}
        />
      </InlineField>
    </FieldSet>
  );
}
