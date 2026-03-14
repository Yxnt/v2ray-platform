package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"v2ray-platform/internal/config"
)

var usageMetricNamePattern = regexp.MustCompile(`user>>>([^>\s"]+)>>>traffic>>>(uplink|downlink)`)
var usageProtobufPattern = regexp.MustCompile(`name:\s*"user>>>([^">]+)>>>traffic>>>(uplink|downlink)"\s*value:\s*([0-9]+)`)
var usageUUIDPattern = regexp.MustCompile(`^[0-9a-fA-F-]{36}$`)

type agentState struct {
	NodeID        string                  `json:"node_id"`
	NodeToken     string                  `json:"node_token"`
	LastUsageHash string                  `json:"last_usage_hash,omitempty"`
	UsageTotals   map[string]usageCounter `json:"usage_totals,omitempty"`
}

type registerRequest struct {
	BootstrapToken string   `json:"bootstrap_token"`
	Name           string   `json:"name"`
	Region         string   `json:"region"`
	PublicHost     string   `json:"public_host"`
	Provider       string   `json:"provider"`
	Tags           []string `json:"tags"`
	RuntimeFlavor  string   `json:"runtime_flavor"`
}

type registerResponse struct {
	NodeID        string `json:"node_id"`
	NodeToken     string `json:"node_token"`
	ConfigVersion int64  `json:"config_version"`
	Config        string `json:"config"`
}

type heartbeatRequest struct {
	AppliedConfigVersion int64  `json:"applied_config_version"`
	PublicHost           string `json:"public_host"`
	Status               string `json:"status"`
	Arch                 string `json:"arch,omitempty"`
}

type heartbeatResponse struct {
	Status           string           `json:"status"`
	AgentMD5         string           `json:"agent_md5,omitempty"`
	AgentDownloadURL string           `json:"agent_download_url,omitempty"`
	PendingRemovals  []pendingRemoval `json:"pending_removals,omitempty"`
	PendingAdditions []pendingRemoval `json:"pending_additions,omitempty"` // same shape as removal
}

type pendingRemoval struct {
	MemberUUID  string `json:"member_uuid"`
	MemberEmail string `json:"member_email"`
}

type syncRequest struct {
	ConfigVersion int64  `json:"config_version"`
	Success       bool   `json:"success"`
	Message       string `json:"message"`
}

type configResponse struct {
	NodeID        string `json:"node_id"`
	ConfigVersion int64  `json:"config_version"`
	Config        string `json:"config"`
	UpdatedAt     string `json:"updated_at"`
}

type usageSnapshot struct {
	CredentialUUID string `json:"credential_uuid"`
	UplinkBytes    int64  `json:"uplink_bytes"`
	DownlinkBytes  int64  `json:"downlink_bytes"`
	CollectedAt    string `json:"collected_at,omitempty"`
}

type usageRequest struct {
	Snapshots []usageSnapshot `json:"snapshots"`
}

type usageCounter struct {
	UplinkBytes   int64 `json:"uplink_bytes"`
	DownlinkBytes int64 `json:"downlink_bytes"`
}

func main() {
	cfg := config.LoadNodeAgent()
	client := &http.Client{Timeout: 10 * time.Second}

	state, err := loadState(cfg.StatePath)
	if err != nil {
		log.Fatal(err)
	}
	if state.NodeID == "" || state.NodeToken == "" {
		state, err = register(cfg, client)
		if err != nil {
			log.Fatal(err)
		}
		if err := saveState(cfg.StatePath, state); err != nil {
			log.Fatal(err)
		}
	}

	var appliedVersion int64
	ctx := context.Background()
	var lastUsageAttemptAt time.Time

	if appliedVersion, err = syncConfig(ctx, cfg, client, state, 0); err != nil {
		log.Printf("initial sync failed: %v", err)
	}

	ticker := time.NewTicker(cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		if hbResp, err := heartbeat(cfg, client, state, appliedVersion); err != nil {
			log.Printf("heartbeat failed: %v", err)
		} else {
			if hbResp.AgentMD5 != "" && hbResp.AgentDownloadURL != "" {
				// Check if we need to self-update.
				if err := maybeSelfUpdate(hbResp.AgentMD5, hbResp.AgentDownloadURL); err != nil {
					log.Printf("self-update check failed: %v", err)
				}
				// maybeSelfUpdate calls os.Exit on success, so we only reach here if no update needed.
			}
			// Immediately remove suspended/expired users from the running V2Ray instance
			// via the gRPC API, without waiting for a full config reload.
			for _, r := range hbResp.PendingRemovals {
				if err := removeV2RayUser(cfg, r.MemberEmail); err != nil {
					log.Printf("v2ray rmui failed (uuid=%s email=%s): %v", r.MemberUUID, r.MemberEmail, err)
				} else {
					log.Printf("[remove] removed user from V2Ray runtime (email=%s)", r.MemberEmail)
				}
			}
			// Immediately re-add restored users to the running V2Ray instance.
			for _, a := range hbResp.PendingAdditions {
				if err := addV2RayUser(cfg, a.MemberUUID, a.MemberEmail); err != nil {
					log.Printf("v2ray adui failed (uuid=%s email=%s): %v", a.MemberUUID, a.MemberEmail, err)
				} else {
					log.Printf("[add] re-added user to V2Ray runtime (email=%s)", a.MemberEmail)
				}
			}
		}
		if version, err := syncConfig(ctx, cfg, client, state, appliedVersion); err != nil {
			log.Printf("config sync failed: %v", err)
		} else {
			appliedVersion = version
		}
		if shouldCollectUsage(cfg, lastUsageAttemptAt) {
			lastUsageAttemptAt = time.Now().UTC()
			log.Printf("[usage] collecting (source=%s cmd=%q)", cfg.UsageSource, cfg.UsageQueryCommand)
			if changedState, err := maybeUploadUsage(cfg, client, state); err != nil {
				log.Printf("usage upload failed: %v", err)
			} else if !agentStatesEqual(changedState, state) {
				state = changedState
				if err := saveState(cfg.StatePath, state); err != nil {
					log.Printf("save agent state failed: %v", err)
				}
			}
		}
		<-ticker.C
	}
}

func register(cfg config.NodeAgentConfig, client *http.Client) (agentState, error) {
	reqBody := registerRequest{
		BootstrapToken: cfg.BootstrapToken,
		Name:           cfg.NodeName,
		Region:         cfg.NodeRegion,
		PublicHost:     cfg.NodePublicHost,
		Provider:       cfg.NodeProvider,
		Tags:           cfg.NodeTags,
		RuntimeFlavor:  cfg.RuntimeFlavor,
	}
	var out registerResponse
	if err := postJSON(client, cfg.ControlPlaneURL+"/api/agent/register", "", reqBody, &out); err != nil {
		return agentState{}, err
	}
	if err := os.WriteFile(cfg.ConfigOutputPath, []byte(out.Config), 0o600); err != nil {
		return agentState{}, err
	}
	return agentState{NodeID: out.NodeID, NodeToken: out.NodeToken}, nil
}

func heartbeat(cfg config.NodeAgentConfig, client *http.Client, state agentState, appliedVersion int64) (heartbeatResponse, error) {
	reqBody := heartbeatRequest{
		AppliedConfigVersion: appliedVersion,
		PublicHost:           cfg.NodePublicHost,
		Status:               "online",
		Arch:                 runtime.GOARCH,
	}
	var resp heartbeatResponse
	if err := postJSON(client, cfg.ControlPlaneURL+"/api/agent/heartbeat", state.NodeToken, reqBody, &resp); err != nil {
		return heartbeatResponse{}, err
	}
	return resp, nil
}

func syncConfig(ctx context.Context, cfg config.NodeAgentConfig, client *http.Client, state agentState, currentVersion int64) (int64, error) {
	resp, err := getJSON(client, cfg.ControlPlaneURL+"/api/agent/config", state.NodeToken)
	if err != nil {
		return currentVersion, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return currentVersion, errors.New(strings.TrimSpace(string(body)))
	}
	var out configResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return currentVersion, err
	}
	if out.ConfigVersion == currentVersion && out.Config != "" {
		return currentVersion, nil
	}
	if err := os.WriteFile(cfg.ConfigOutputPath, []byte(out.Config), 0o600); err != nil {
		_ = reportSyncResult(cfg, client, state, out.ConfigVersion, false, err.Error())
		return currentVersion, err
	}
	if err := runReload(ctx, cfg.ReloadCommand); err != nil {
		_ = reportSyncResult(cfg, client, state, out.ConfigVersion, false, err.Error())
		return currentVersion, err
	}
	if err := reportSyncResult(cfg, client, state, out.ConfigVersion, true, "applied"); err != nil {
		return currentVersion, err
	}
	return out.ConfigVersion, nil
}

func reportSyncResult(cfg config.NodeAgentConfig, client *http.Client, state agentState, version int64, success bool, message string) error {
	return postJSON(client, cfg.ControlPlaneURL+"/api/agent/sync-result", state.NodeToken, syncRequest{
		ConfigVersion: version,
		Success:       success,
		Message:       message,
	}, nil)
}

func maybeUploadUsage(cfg config.NodeAgentConfig, client *http.Client, state agentState) (agentState, error) {
	switch cfg.UsageSource {
	case "", "disabled":
		return state, nil
	case "file":
		return maybeUploadUsageFromFile(cfg, client, state)
	case "runtime":
		return maybeUploadUsageFromRuntime(cfg, client, state)
	default:
		return state, fmt.Errorf("unsupported NODE_USAGE_SOURCE %q", cfg.UsageSource)
	}
}

func maybeUploadUsageFromFile(cfg config.NodeAgentConfig, client *http.Client, state agentState) (agentState, error) {
	if strings.TrimSpace(cfg.UsageInputPath) == "" {
		return state, nil
	}
	data, err := os.ReadFile(cfg.UsageInputPath)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	hash := sha256Hex(string(data))
	if hash == state.LastUsageHash {
		return state, nil
	}
	var payload usageRequest
	if err := json.Unmarshal(data, &payload); err != nil {
		return state, err
	}
	if len(payload.Snapshots) == 0 {
		state.LastUsageHash = hash
		return state, nil
	}
	if err := postJSON(client, cfg.ControlPlaneURL+"/api/agent/usage", state.NodeToken, payload, nil); err != nil {
		return state, err
	}
	state.LastUsageHash = hash
	return state, nil
}

func maybeUploadUsageFromRuntime(cfg config.NodeAgentConfig, client *http.Client, state agentState) (agentState, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	output, err := queryRuntimeUsage(ctx, cfg)
	if err != nil {
		log.Printf("[usage] query command failed: %v", err)
		return state, err
	}
	log.Printf("[usage] raw output (%d bytes): %s", len(output), strings.TrimSpace(string(output)))
	currentTotals, err := parseUsageCounters(output)
	if err != nil {
		log.Printf("[usage] parse failed: %v", err)
		return state, err
	}
	log.Printf("[usage] parsed %d user counter(s)", len(currentTotals))
	snapshots, nextTotals := diffUsageCounters(currentTotals, state.UsageTotals, time.Now().UTC())
	state.UsageTotals = nextTotals
	if len(snapshots) == 0 {
		log.Printf("[usage] no delta since last collection, skipping upload")
		return state, nil
	}
	log.Printf("[usage] uploading %d snapshot(s) to control plane", len(snapshots))
	if err := postJSON(client, cfg.ControlPlaneURL+"/api/agent/usage", state.NodeToken, usageRequest{Snapshots: snapshots}, nil); err != nil {
		log.Printf("[usage] upload failed: %v", err)
		return state, err
	}
	log.Printf("[usage] upload OK")
	return state, nil
}

func shouldCollectUsage(cfg config.NodeAgentConfig, lastAttemptAt time.Time) bool {
	if cfg.UsageSource == "" || cfg.UsageSource == "disabled" {
		return false
	}
	if cfg.UsageCollectionInterval <= 0 {
		return true
	}
	return lastAttemptAt.IsZero() || time.Since(lastAttemptAt) >= cfg.UsageCollectionInterval
}

func queryRuntimeUsage(ctx context.Context, cfg config.NodeAgentConfig) ([]byte, error) {
	var cmd *exec.Cmd
	if strings.TrimSpace(cfg.UsageQueryCommand) != "" {
		cmd = exec.CommandContext(ctx, "sh", "-c", cfg.UsageQueryCommand)
	} else if strings.EqualFold(cfg.RuntimeFlavor, "xray") {
		cmd = exec.CommandContext(ctx, "xray", "api", "statsquery", "--server="+cfg.UsageQueryServer)
	} else {
		cmd = exec.CommandContext(ctx, "v2ctl", "api", "--server="+cfg.UsageQueryServer, "StatsService.QueryStats", `pattern: "" reset: false`)
	}
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = err.Error()
		}
		return nil, errors.New(msg)
	}
	return output, nil
}

func parseUsageCounters(output []byte) (map[string]usageCounter, error) {
	counters := map[string]usageCounter{}

	// Strategy 1: V2Ray 5.x / Xray JSON output
	// {"stat":[{"name":"user>>>UUID>>>traffic>>>uplink","value":"12345"}]}
	var jsonResp struct {
		Stat []struct {
			Name  string          `json:"name"`
			Value json.RawMessage `json:"value"` // can be string "123" or number 123
		} `json:"stat"`
	}
	if err := json.Unmarshal(output, &jsonResp); err == nil && len(jsonResp.Stat) > 0 {
		for _, item := range jsonResp.Stat {
			match := usageMetricNamePattern.FindStringSubmatch(item.Name)
			if len(match) != 3 {
				continue
			}
			// value may be JSON string "12345" or JSON number 12345
			valStr := strings.Trim(string(item.Value), `"`)
			applyUsageMetric(counters, match[1], match[2], valStr)
		}
		if len(counters) > 0 {
			return counters, nil
		}
	}

	// Strategy 2: protobuf text — name and value on the same line
	// name: "user>>>UUID>>>traffic>>>uplink" value: 12345
	text := string(output)
	if matches := usageProtobufPattern.FindAllStringSubmatch(text, -1); len(matches) > 0 {
		for _, match := range matches {
			applyUsageMetric(counters, match[1], match[2], match[3])
		}
		if len(counters) > 0 {
			return counters, nil
		}
	}

	// Strategy 3: generic line-by-line (v2ctl legacy or unknown format)
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		match := usageMetricNamePattern.FindStringSubmatch(line)
		if len(match) != 3 {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		valueText := strings.TrimSpace(strings.TrimPrefix(fields[len(fields)-1], "value:"))
		valueText = strings.TrimSpace(strings.TrimRight(strings.TrimRight(valueText, ","), "}"))
		applyUsageMetric(counters, match[1], match[2], valueText)
	}
	if len(counters) == 0 {
		return nil, errors.New("no user traffic counters found in runtime stats output")
	}
	return counters, nil
}

func applyUsageMetric(counters map[string]usageCounter, credentialUUID, direction, valueText string) {
	if !usageUUIDPattern.MatchString(strings.TrimSpace(credentialUUID)) {
		return
	}
	value, err := strconv.ParseInt(strings.TrimSpace(valueText), 10, 64)
	if err != nil {
		return
	}
	counter := counters[strings.ToLower(credentialUUID)]
	switch direction {
	case "uplink":
		counter.UplinkBytes = value
	case "downlink":
		counter.DownlinkBytes = value
	}
	counters[strings.ToLower(credentialUUID)] = counter
}

func diffUsageCounters(current, previous map[string]usageCounter, collectedAt time.Time) ([]usageSnapshot, map[string]usageCounter) {
	if current == nil {
		current = map[string]usageCounter{}
	}
	next := make(map[string]usageCounter, len(current))
	keys := make([]string, 0, len(current))
	for key, counter := range current {
		next[key] = counter
		keys = append(keys, key)
	}
	sort.Strings(keys)
	snapshots := make([]usageSnapshot, 0, len(keys))
	for _, credentialUUID := range keys {
		counter := current[credentialUUID]
		prev := usageCounter{}
		if previous != nil {
			prev = previous[credentialUUID]
		}
		uplink := diffCounter(counter.UplinkBytes, prev.UplinkBytes)
		downlink := diffCounter(counter.DownlinkBytes, prev.DownlinkBytes)
		if uplink == 0 && downlink == 0 {
			continue
		}
		snapshots = append(snapshots, usageSnapshot{
			CredentialUUID: credentialUUID,
			UplinkBytes:    uplink,
			DownlinkBytes:  downlink,
			CollectedAt:    collectedAt.Format(time.RFC3339),
		})
	}
	return snapshots, next
}

func diffCounter(current, previous int64) int64 {
	if current < previous {
		return current
	}
	return current - previous
}

func sha256Hex(value string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}

func agentStatesEqual(a, b agentState) bool {
	return a.NodeID == b.NodeID &&
		a.NodeToken == b.NodeToken &&
		a.LastUsageHash == b.LastUsageHash &&
		usageCountersEqual(a.UsageTotals, b.UsageTotals)
}

func usageCountersEqual(a, b map[string]usageCounter) bool {
	if len(a) != len(b) {
		return false
	}
	for key, av := range a {
		if bv, ok := b[key]; !ok || av != bv {
			return false
		}
	}
	return true
}

func runReload(ctx context.Context, command string) error {
	if strings.TrimSpace(command) == "" {
		return nil
	}
	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// removeV2RayUser calls "v2ray api rmui" to dynamically remove a user from the
// running V2Ray instance without reloading the config file.
// This provides immediate effect; the config rebuild (already triggered by the control
// plane) ensures the user is excluded from the next persisted config as well.
func removeV2RayUser(cfg config.NodeAgentConfig, email string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx,
		"v2ray", "api", "rmui",
		"--server="+cfg.UsageQueryServer,
		"-inboundTag=vmess-inbound",
		"-email="+email,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// addV2RayUser calls "v2ray api adui" to dynamically re-add a restored user to the
// running V2Ray instance without reloading the config file.
func addV2RayUser(cfg config.NodeAgentConfig, uuid, email string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// VMess inbound user JSON — only id/alterId/email/level are valid server-side fields.
	// The "security" field is a client-side concept and must be omitted here.
	userJSON := fmt.Sprintf(`{"id":"%s","alterId":0,"email":"%s","level":0}`, uuid, email)
	cmd := exec.CommandContext(ctx,
		"v2ray", "api", "adui",
		"--server="+cfg.UsageQueryServer,
		"-inboundTag=vmess-inbound",
		"-users="+userJSON,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// selfMD5 computes the MD5 hex digest of the running binary.
func selfMD5() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	f, err := os.Open(exe)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := md5.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}

// maybeSelfUpdate checks if the remote binary MD5 differs from the current
// binary. If so, downloads the new binary, replaces self, and re-execs.
func maybeSelfUpdate(remoteMD5, downloadURL string) error {
	current, err := selfMD5()
	if err != nil {
		return fmt.Errorf("compute self MD5: %w", err)
	}
	if strings.EqualFold(current, remoteMD5) {
		return nil // already up to date
	}
	log.Printf("self-update: current MD5=%s remote MD5=%s — downloading update", current, remoteMD5)

	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("download agent: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download agent: HTTP %d", resp.StatusCode)
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	// Write to a temp file alongside the current binary.
	tmp, err := os.CreateTemp(strings.TrimSuffix(exe, "/"+strings.Split(exe, "/")[len(strings.Split(exe, "/"))-1]), ".node-agent-update-*")
	if err != nil {
		// fallback to system temp dir
		tmp, err = os.CreateTemp("", ".node-agent-update-*")
		if err != nil {
			return fmt.Errorf("create temp file: %w", err)
		}
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	h := md5.New()
	if _, err := io.Copy(io.MultiWriter(tmp, h), resp.Body); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	tmp.Close()

	// Verify MD5 of downloaded file.
	downloadedMD5 := fmt.Sprintf("%x", h.Sum(nil))
	if !strings.EqualFold(downloadedMD5, remoteMD5) {
		return fmt.Errorf("self-update MD5 mismatch: expected %s got %s", remoteMD5, downloadedMD5)
	}

	// Make executable and atomically replace.
	if err := os.Chmod(tmpPath, 0o755); err != nil {
		return fmt.Errorf("chmod temp file: %w", err)
	}
	if err := os.Rename(tmpPath, exe); err != nil {
		return fmt.Errorf("replace binary: %w", err)
	}

	log.Printf("self-update: replaced binary at %s — restarting", exe)
	// Re-exec self to pick up the new binary.
	if err := syscall.Exec(exe, os.Args, os.Environ()); err != nil {
		// syscall.Exec replaces the process; if we get here something went wrong.
		return fmt.Errorf("re-exec: %w", err)
	}
	return nil // unreachable
}

func loadState(path string) (agentState, error) {
	var state agentState
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	err = json.Unmarshal(data, &state)
	return state, err
}

func saveState(path string, state agentState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

func postJSON(client *http.Client, url string, bearer string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return errors.New(strings.TrimSpace(string(msg)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func getJSON(client *http.Client, url string, bearer string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	return client.Do(req)
}
