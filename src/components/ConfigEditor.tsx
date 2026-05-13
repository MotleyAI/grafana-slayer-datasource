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
      secureJsonData: { apiKey: event.target.value },
    });
  };

  const onResetAPIKey = () => {
    onOptionsChange({
      ...options,
      secureJsonFields: { ...(secureJsonFields ?? {}), apiKey: false },
      secureJsonData: { ...(secureJsonData ?? {}), apiKey: '' },
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
    </FieldSet>
  );
}
