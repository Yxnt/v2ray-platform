package render

import (
	"encoding/json"

	"v2ray-platform/internal/domain"
)

type v2rayConfig struct {
	Log       map[string]any `json:"log"`
	API       map[string]any `json:"api"`
	Stats     map[string]any `json:"stats"`
	Policy    map[string]any `json:"policy"`
	Inbounds  []any          `json:"inbounds"`
	Outbounds []any          `json:"outbounds"`
	Routing   map[string]any `json:"routing"`
}

type vmessClient struct {
	Email string `json:"email"`
	ID    string `json:"id"`
	Level int    `json:"level"`
}

func RenderNodeConfig(node domain.Node, creds []domain.NodeCredential) (string, error) {
	clients := make([]vmessClient, 0, len(creds))
	for _, cred := range creds {
		clients = append(clients, vmessClient{
			Email: cred.UUID,
			ID:    cred.UUID,
			Level: 0,
		})
	}

	cfg := v2rayConfig{
		Log:   map[string]any{"loglevel": "warning"},
		API:   map[string]any{"tag": "stats-api", "services": []string{"StatsService"}},
		Stats: map[string]any{},
		Policy: map[string]any{
			"levels": map[string]any{
				"0": map[string]any{
					"statsUserUplink":   true,
					"statsUserDownlink": true,
				},
			},
			"system": map[string]any{
				"statsInboundUplink":    true,
				"statsInboundDownlink":  true,
				"statsOutboundUplink":   true,
				"statsOutboundDownlink": true,
			},
		},
		Inbounds: []any{
			map[string]any{
				"port":     10000,
				"listen":   "0.0.0.0",
				"protocol": "vmess",
				"settings": map[string]any{
					"clients": clients,
				},
				"streamSettings": map[string]any{
					"network": "ws",
					"wsSettings": map[string]any{
						"path": "/ray",
					},
				},
				"tag": "main",
			},
			map[string]any{
				"listen":   "127.0.0.1",
				"port":     10085,
				"protocol": "dokodemo-door",
				"settings": map[string]any{
					"address": "127.0.0.1",
				},
				"tag": "stats-api-in",
			},
		},
		Outbounds: []any{
			map[string]any{"protocol": "freedom", "tag": "internet"},
			map[string]any{"protocol": "blackhole", "tag": "blocked"},
		},
		Routing: map[string]any{
			"domainStrategy": "IPIfNonMatch",
			"rules": []any{
				map[string]any{
					"type":        "field",
					"inboundTag":  []string{"stats-api-in"},
					"outboundTag": "stats-api",
				},
				map[string]any{
					"type":        "field",
					"ip":          []string{"geoip:private"},
					"outboundTag": "blocked",
				},
			},
		},
	}

	if node.RuntimeFlavor == "xray" {
		cfg.Log["loglevel"] = "info"
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
