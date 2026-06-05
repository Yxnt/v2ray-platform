package cloudfront

import (
	"strings"

	"v2ray-platform/internal/domain"
)

// SyncPlan holds the actions needed to reconcile drift.
type SyncPlan struct {
	Actions     []domain.CloudFrontSyncAction
	DriftStatus string // "in_sync", "drifted", "conflict"
}

// Plan computes the sync plan by comparing platform bindings with AWS origins.
// bindings: platform's desired bindings (from cloudfront_configs.bindings_json)
// origins: current AWS CloudFront origins
// nodeHosts: map of nodeID -> publicHost (for constructing origin domains)
// routeKeys: map of nodeID -> routeKey (for constructing origin paths)
func Plan(bindings []domain.CloudFrontBinding, origins []OriginState, nodeHosts map[string]string, routeKeys map[string]string) *SyncPlan {
	// Build lookup maps
	originByRouteKey := make(map[string]OriginState)
	for _, o := range origins {
		// Extract route key from path prefix (strip leading "/")
		key := strings.TrimPrefix(o.PathPrefix, "/")
		if key != "" {
			originByRouteKey[key] = o
		}
	}

	bindingByRouteKey := make(map[string]domain.CloudFrontBinding)
	for _, b := range bindings {
		bindingByRouteKey[b.RouteKey] = b
	}

	var actions []domain.CloudFrontSyncAction
	driftStatus := "in_sync"

	// Check each binding: does it have a matching origin?
	for _, b := range bindings {
		host := nodeHosts[b.NodeID]
		if host == "" {
			// Node has no public host -- skip (will be handled by bind service)
			continue
		}

		existing, exists := originByRouteKey[b.RouteKey]
		if !exists {
			// Origin missing -- need to add
			actions = append(actions, domain.CloudFrontSyncAction{
				Action:    "add_origin",
				OriginID:  b.OriginID,
				Host:      host,
				RouteKey:  b.RouteKey,
				GroupName: b.GroupName,
				Reason:    "origin missing from distribution",
			})
			driftStatus = "drifted"
		} else if existing.DomainName != host {
			// Origin exists but host differs -- replace
			actions = append(actions, domain.CloudFrontSyncAction{
				Action:    "replace_origin",
				OriginID:  b.OriginID,
				Host:      host,
				RouteKey:  b.RouteKey,
				GroupName: b.GroupName,
				Reason:    "origin host mismatch: want " + host + ", have " + existing.DomainName,
			})
			driftStatus = "drifted"
		}
		// If origin matches -- no action needed
	}

	// Check for extra origins in AWS not in bindings
	for routeKey, origin := range originByRouteKey {
		if _, inBindings := bindingByRouteKey[routeKey]; !inBindings {
			actions = append(actions, domain.CloudFrontSyncAction{
				Action:   "remove_origin",
				OriginID: origin.OriginID,
				RouteKey: routeKey,
				Reason:   "origin exists in distribution but not in platform bindings",
			})
			driftStatus = "drifted"
		}
	}

	if len(actions) == 0 {
		actions = []domain.CloudFrontSyncAction{{Action: "noop", Reason: "distribution is in sync"}}
	}

	return &SyncPlan{
		Actions:     actions,
		DriftStatus: driftStatus,
	}
}
