package render

import (
	"strings"
	"testing"

	"v2ray-platform/internal/domain"
)

func TestRenderNodeConfigIncludesCredentials(t *testing.T) {
	node := domain.Node{ID: "node-1", Name: "hk-01", RuntimeFlavor: "v2ray"}
	creds := []domain.NodeCredential{
		{UUID: "11111111-1111-4111-8111-111111111111", Email: "alice+node-1@example.com"},
		{UUID: "22222222-2222-4222-8222-222222222222", Email: "bob+node-1@example.com"},
	}

	cfg, err := RenderNodeConfig(node, creds)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
		"/hk-01",
		"23333",
		"127.0.0.1",
		"vmess-inbound",
		"bittorrent",
		"geosite:category-ads",
	} {
		if !strings.Contains(cfg, want) {
			t.Fatalf("expected config to contain %q, got:\n%s", want, cfg)
		}
	}

	// Stats API must not be present
	for _, absent := range []string{"StatsService", "stats-api-in", "10085", "dokodemo-door"} {
		if strings.Contains(cfg, absent) {
			t.Fatalf("expected config NOT to contain %q", absent)
		}
	}
}
