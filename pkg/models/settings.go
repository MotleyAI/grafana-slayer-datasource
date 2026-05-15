package models

import (
	"encoding/json"
	"fmt"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
)

const (
	DefaultURL        = "http://localhost:5143"
	DefaultGrafanaURL = "http://localhost:3000" // plugin runs as a Grafana subprocess; localhost is grafana itself.
)

type PluginSettings struct {
	URL string `json:"url"`
	// GrafanaURL is used by the plugin's MCP CallResource to call Grafana
	// back (dashboards write API). Defaults to localhost:3000 — the plugin
	// runs inside Grafana's container so that's nearly always right.
	GrafanaURL string                `json:"grafana_url"`
	Secrets    *SecretPluginSettings `json:"-"`
}

type SecretPluginSettings struct {
	// APIKey for SLayer (forward-compat; SLayer ≤0.6.x has no auth).
	APIKey string `json:"apiKey"`
	// GrafanaToken — service-account token the plugin uses for MCP write
	// operations (POST /api/dashboards/db). For production this is the
	// canonical config; the bundled demo relies on anonymous-Admin auth.
	GrafanaToken string `json:"grafanaToken"`
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
	if settings.GrafanaURL == "" {
		settings.GrafanaURL = DefaultGrafanaURL
	}
	settings.Secrets = loadSecretPluginSettings(source.DecryptedSecureJSONData)
	return &settings, nil
}

func loadSecretPluginSettings(source map[string]string) *SecretPluginSettings {
	return &SecretPluginSettings{
		APIKey:       source["apiKey"],
		GrafanaToken: source["grafanaToken"],
	}
}
