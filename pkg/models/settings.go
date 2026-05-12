package models

import (
	"encoding/json"
	"fmt"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

const DefaultURL = "http://localhost:5143"

type PluginSettings struct {
	URL     string                `json:"url"`
	Secrets *SecretPluginSettings `json:"-"`
}

type SecretPluginSettings struct {
	APIKey string `json:"apiKey"`
}

func LoadPluginSettings(source backend.DataSourceInstanceSettings) (*PluginSettings, error) {
	settings := PluginSettings{}
	if len(source.JSONData) > 0 {
		if err := json.Unmarshal(source.JSONData, &settings); err != nil {
			return nil, fmt.Errorf("unmarshal PluginSettings: %w", err)
		}
	}
	if settings.URL == "" {
		settings.URL = DefaultURL
	}
	settings.Secrets = loadSecretPluginSettings(source.DecryptedSecureJSONData)
	return &settings, nil
}

func loadSecretPluginSettings(source map[string]string) *SecretPluginSettings {
	return &SecretPluginSettings{
		APIKey: source["apiKey"],
	}
}
