package render

import (
	"encoding/json"

	"v2ray-platform/internal/domain"
)

type v2rayConfig struct {
	Log       map[string]any `json:"log"`
	Stats     map[string]any `json:"stats"`
	API       map[string]any `json:"api"`
	Inbounds  []any          `json:"inbounds"`
	Outbounds []any          `json:"outbounds"`
	Routing   map[string]any `json:"routing"`
	DNS       map[string]any `json:"dns"`
	Policy    map[string]any `json:"policy"`
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

	wsPath := "/" + node.Name

	cfg := v2rayConfig{
		Log:   map[string]any{"loglevel": "info"},
		Stats: map[string]any{},
		API: map[string]any{
			"tag":      "api",
			"services": []string{"StatsService"},
		},
		Inbounds: []any{
			// Stats API inbound — queried by node-agent for per-user traffic.
			map[string]any{
				"listen":   "127.0.0.1",
				"port":     10085,
				"protocol": "dokodemo-door",
				"settings": map[string]any{"address": "127.0.0.1"},
				"tag":      "api",
			},
			map[string]any{
				"port":     23333,
				"listen":   "127.0.0.1",
				"tag":      "vmess-inbound",
				"protocol": "vmess",
				"settings": map[string]any{
					"clients":    clients,
					"decryption": "none",
				},
				"sniffing": map[string]any{
					"enabled":      true,
					"destOverride": []string{"http", "tls"},
				},
				"streamSettings": map[string]any{
					"network":  "ws",
					"security": "none",
					"wsSettings": map[string]any{
						"path": wsPath,
					},
				},
			},
		},
		Outbounds: []any{
			map[string]any{
				"protocol": "freedom",
				"settings": map[string]any{"userLevel": 0},
				"tag":      "direct",
			},
			map[string]any{
				"protocol": "blackhole",
				"settings": map[string]any{},
				"tag":      "blocked",
			},
			map[string]any{
				"tag":      "api",
				"protocol": "freedom",
				"settings": map[string]any{},
			},
		},
		Routing: map[string]any{
			"domainStrategy": "AsIS",
			"domainMatcher":  "mph",
			"rules": []any{
				// Route stats API traffic to the api outbound tag.
				map[string]any{
					"type":        "field",
					"inboundTag":  []string{"api"},
					"outboundTag": "api",
				},
				map[string]any{
					"type":        "field",
					"outboundTag": "blocked",
					"protocol":    []string{"bittorrent"},
				},
				map[string]any{
					"type":        "field",
					"ip":          []string{"geoip:private"},
					"outboundTag": "blocked",
				},
				map[string]any{
					"type":        "field",
					"domain":      []string{"geosite:category-ads"},
					"outboundTag": "blocked",
				},
			},
		},
		DNS: map[string]any{
			"hosts": map[string]any{
				"domain:v2fly.org":        "www.vicemc.net",
				"domain:github.io":        "pages.github.com",
				"domain:wikipedia.org":    "www.wikimedia.org",
				"domain:shadowsocks.org":  "electronicsrealm.com",
			},
			"servers": []any{
				"1.1.1.1",
				map[string]any{
					"address": "1.2.4.8",
					"port":    53,
					"domains": []string{"geosite:cn"},
				},
				"8.8.8.8",
				"localhost",
			},
		},
		Policy: map[string]any{
			"levels": map[string]any{
				"0": map[string]any{
					"uplinkOnly":   0,
					"downlinkOnly": 0,
				},
			},
			"system": map[string]any{
				"statsInboundUplink":    true,
				"statsInboundDownlink":  true,
				"statsOutboundUplink":   true,
				"statsOutboundDownlink": true,
			},
		},
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}
