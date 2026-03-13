package main

import (
	"testing"
	"time"
)

func TestParseUsageCountersFromProtobufOutput(t *testing.T) {
	output := []byte(`stat: {
  name: "user>>>11111111-1111-4111-8111-111111111111>>>traffic>>>uplink"
  value: 120
}
stat: {
  name: "user>>>11111111-1111-4111-8111-111111111111>>>traffic>>>downlink"
  value: 340
}
stat: {
  name: "user>>>not-a-uuid>>>traffic>>>uplink"
  value: 999
}`)
	counters, err := parseUsageCounters(output)
	if err != nil {
		t.Fatal(err)
	}
	got := counters["11111111-1111-4111-8111-111111111111"]
	if got.UplinkBytes != 120 || got.DownlinkBytes != 340 {
		t.Fatalf("unexpected counters: %+v", got)
	}
	if len(counters) != 1 {
		t.Fatalf("unexpected counters map: %+v", counters)
	}
}

// TestParseUsageCountersFromV2RayJSON covers V2Ray 5.x / Xray JSON output format.
func TestParseUsageCountersFromV2RayJSON(t *testing.T) {
	// value as JSON string (proto3 int64 serialised as string)
	jsonString := []byte(`{"stat":[{"name":"user>>>11111111-1111-4111-8111-111111111111>>>traffic>>>uplink","value":"500"},{"name":"user>>>11111111-1111-4111-8111-111111111111>>>traffic>>>downlink","value":"800"},{"name":"user>>>not-a-uuid>>>traffic>>>uplink","value":"1"}]}`)
	counters, err := parseUsageCounters(jsonString)
	if err != nil {
		t.Fatal(err)
	}
	got := counters["11111111-1111-4111-8111-111111111111"]
	if got.UplinkBytes != 500 || got.DownlinkBytes != 800 {
		t.Fatalf("unexpected counters: %+v", got)
	}

	// value as JSON number
	jsonNumber := []byte(`{"stat":[{"name":"user>>>22222222-2222-4222-8222-222222222222>>>traffic>>>uplink","value":999},{"name":"user>>>22222222-2222-4222-8222-222222222222>>>traffic>>>downlink","value":111}]}`)
	counters2, err := parseUsageCounters(jsonNumber)
	if err != nil {
		t.Fatal(err)
	}
	got2 := counters2["22222222-2222-4222-8222-222222222222"]
	if got2.UplinkBytes != 999 || got2.DownlinkBytes != 111 {
		t.Fatalf("unexpected counters: %+v", got2)
	}
}

func TestDiffUsageCountersHandlesReset(t *testing.T) {
	now := time.Date(2026, 3, 10, 14, 0, 0, 0, time.UTC)
	snapshots, next := diffUsageCounters(
		map[string]usageCounter{
			"11111111-1111-4111-8111-111111111111": {UplinkBytes: 30, DownlinkBytes: 50},
		},
		map[string]usageCounter{
			"11111111-1111-4111-8111-111111111111": {UplinkBytes: 100, DownlinkBytes: 40},
		},
		now,
	)
	if len(snapshots) != 1 {
		t.Fatalf("expected one snapshot, got %+v", snapshots)
	}
	if snapshots[0].UplinkBytes != 30 || snapshots[0].DownlinkBytes != 10 {
		t.Fatalf("unexpected snapshot delta: %+v", snapshots[0])
	}
	if next["11111111-1111-4111-8111-111111111111"].UplinkBytes != 30 {
		t.Fatalf("unexpected next totals: %+v", next)
	}
}



