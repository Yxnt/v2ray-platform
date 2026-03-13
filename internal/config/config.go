package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type ControlPlaneConfig struct {
	ListenAddr              string
	AdminToken              string
	DatabaseURL             string
	SessionSecret           string
	PreviousSessionSecrets  []string
	SessionTTL              time.Duration
	BootstrapAdminEmail     string
	BootstrapAdminPassword  string
	LifecycleSweepInterval  time.Duration
	AlertEvaluationInterval time.Duration
	NodeOfflineAfter        time.Duration
	AlertWebhookURL         string
	DBMaxOpenConns          int
	DBMaxIdleConns          int
	DBConnMaxLifetime       time.Duration
	ServiceName             string
	RevisionName            string
	AgentDownloadURL        string
}

type NodeAgentConfig struct {
	ControlPlaneURL         string
	BootstrapToken          string
	NodeName                string
	NodeRegion              string
	NodePublicHost          string
	NodeProvider            string
	NodeTags                []string
	RuntimeFlavor           string
	StatePath               string
	ConfigOutputPath        string
	UsageSource             string
	UsageInputPath          string
	UsageQueryServer        string
	UsageQueryCommand       string
	UsageCollectionInterval time.Duration
	HeartbeatInterval       time.Duration
	ReloadCommand           string
}

func LoadControlPlane() ControlPlaneConfig {
	listenAddr := envOr("CONTROL_PLANE_LISTEN_ADDR", "")
	if listenAddr == "" {
		if port := os.Getenv("PORT"); port != "" {
			listenAddr = ":" + port
		} else {
			listenAddr = ":8080"
		}
	}
	return ControlPlaneConfig{
		ListenAddr:              listenAddr,
		AdminToken:              os.Getenv("CONTROL_PLANE_ADMIN_TOKEN"),
		DatabaseURL:             os.Getenv("DATABASE_URL"),
		SessionSecret:           envOr("CONTROL_PLANE_SESSION_SECRET", "dev-session-secret"),
		PreviousSessionSecrets:  splitCSV(os.Getenv("CONTROL_PLANE_PREVIOUS_SESSION_SECRETS")),
		SessionTTL:              time.Duration(mustAtoi(envOr("CONTROL_PLANE_SESSION_TTL_HOURS", "24"))) * time.Hour,
		BootstrapAdminEmail:     os.Getenv("BOOTSTRAP_ADMIN_EMAIL"),
		BootstrapAdminPassword:  os.Getenv("BOOTSTRAP_ADMIN_PASSWORD"),
		LifecycleSweepInterval:  time.Duration(mustAtoi(envOr("CONTROL_PLANE_LIFECYCLE_SWEEP_SECONDS", "60"))) * time.Second,
		AlertEvaluationInterval: time.Duration(mustAtoi(envOr("CONTROL_PLANE_ALERT_EVALUATION_SECONDS", "60"))) * time.Second,
		NodeOfflineAfter:        time.Duration(mustAtoi(envOr("CONTROL_PLANE_NODE_OFFLINE_SECONDS", "120"))) * time.Second,
		AlertWebhookURL:         os.Getenv("CONTROL_PLANE_ALERT_WEBHOOK_URL"),
		DBMaxOpenConns:          mustAtoi(envOr("CONTROL_PLANE_DB_MAX_OPEN_CONNS", "10")),
		DBMaxIdleConns:          mustAtoi(envOr("CONTROL_PLANE_DB_MAX_IDLE_CONNS", "5")),
		DBConnMaxLifetime:       time.Duration(mustAtoi(envOr("CONTROL_PLANE_DB_CONN_MAX_LIFETIME_SECONDS", "300"))) * time.Second,
		ServiceName:             os.Getenv("K_SERVICE"),
		RevisionName:            os.Getenv("K_REVISION"),
		AgentDownloadURL:        envOr("AGENT_DOWNLOAD_URL", "https://github.com/Yxnt/v2ray-platform/releases/download/latest/node-agent-linux-amd64"),
	}
}

func LoadNodeAgent() NodeAgentConfig {
	intervalSeconds, _ := strconv.Atoi(envOr("NODE_HEARTBEAT_INTERVAL_SECONDS", "30"))
	usageIntervalSeconds, _ := strconv.Atoi(envOr("NODE_USAGE_COLLECTION_INTERVAL_SECONDS", "60"))
	usageSource := strings.TrimSpace(strings.ToLower(os.Getenv("NODE_USAGE_SOURCE")))
	if usageSource == "" {
		if strings.TrimSpace(os.Getenv("NODE_USAGE_INPUT_PATH")) != "" {
			usageSource = "file"
		} else {
			usageSource = "disabled"
		}
	}
	return NodeAgentConfig{
		ControlPlaneURL:         envOr("CONTROL_PLANE_URL", "http://127.0.0.1:8080"),
		BootstrapToken:          os.Getenv("BOOTSTRAP_TOKEN"),
		NodeName:                envOr("NODE_NAME", hostnameOr("unnamed-node")),
		NodeRegion:              envOr("NODE_REGION", "unknown"),
		NodePublicHost:          envOr("NODE_PUBLIC_HOST", ""),
		NodeProvider:            envOr("NODE_PROVIDER", ""),
		NodeTags:                splitCSV(os.Getenv("NODE_TAGS")),
		RuntimeFlavor:           envOr("RUNTIME_FLAVOR", "v2ray"),
		StatePath:               envOr("AGENT_STATE_PATH", "/var/lib/v2ray-platform/agent-state.json"),
		ConfigOutputPath:        envOr("NODE_CONFIG_OUTPUT_PATH", "/etc/v2ray/config.generated.json"),
		UsageSource:             usageSource,
		UsageInputPath:          os.Getenv("NODE_USAGE_INPUT_PATH"),
		UsageQueryServer:        envOr("NODE_USAGE_QUERY_SERVER", "127.0.0.1:10085"),
		UsageQueryCommand:       os.Getenv("NODE_USAGE_QUERY_COMMAND"),
		UsageCollectionInterval: time.Duration(usageIntervalSeconds) * time.Second,
		HeartbeatInterval:       time.Duration(intervalSeconds) * time.Second,
		ReloadCommand:           os.Getenv("NODE_RELOAD_COMMAND"),
	}
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func hostnameOr(fallback string) string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return fallback
	}
	return name
}

func mustAtoi(value string) int {
	n, err := strconv.Atoi(value)
	if err != nil {
		return 24
	}
	return n
}
