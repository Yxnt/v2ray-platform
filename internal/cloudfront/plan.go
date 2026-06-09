package cloudfront

import (
	"strings"

	"v2ray-platform/internal/domain"
)

// SyncPlan holds the actions needed to reconcile CloudFront path routing.
type SyncPlan struct {
	Actions       []RouteAction
	RewriteRoutes []RewriteRoute
	DriftStatus   string // "in_sync", "drifted", "conflict"
}

type routeState struct {
	RouteKey string
	OriginID string
	Host     string
	Managed  bool
}

// Plan computes the sync plan by comparing platform bindings with the current
// CloudFront route table reconstructed from origins and cache behaviors.
func Plan(bindings []domain.CloudFrontBinding, dist *DistributionState, nodeHosts map[string]string, nodePaths map[string]string) *SyncPlan {
	desiredByRouteKey := make(map[string]domain.CloudFrontBinding, len(bindings))
	for _, b := range bindings {
		if strings.TrimSpace(b.RouteKey) == "" {
			continue
		}
		desiredByRouteKey[b.RouteKey] = b
	}

	originByID := make(map[string]OriginState, len(dist.Origins))
	for _, origin := range dist.Origins {
		originByID[origin.OriginID] = origin
	}

	currentByRouteKey := make(map[string]routeState, len(dist.Behaviors))
	for _, behavior := range dist.Behaviors {
		routeKey := strings.TrimPrefix(strings.TrimSpace(behavior.PathPattern), "/")
		if routeKey == "" {
			continue
		}
		origin := originByID[behavior.OriginID]
		currentByRouteKey[routeKey] = routeState{
			RouteKey: routeKey,
			OriginID: behavior.OriginID,
			Host:     origin.DomainName,
			Managed:  isManagedOriginID(behavior.OriginID),
		}
	}

	actions := make([]RouteAction, 0)
	rewrites := make([]RewriteRoute, 0, len(desiredByRouteKey))
	driftStatus := "in_sync"

	for routeKey, binding := range desiredByRouteKey {
		host := strings.TrimSpace(nodeHosts[binding.NodeID])
		if host == "" {
			actions = append(actions, RouteAction{
				Action:    "conflict",
				RouteKey:  routeKey,
				OriginID:  binding.OriginID,
				GroupName: binding.GroupName,
				Reason:    "node public_host is required for cloudfront origin",
			})
			driftStatus = "conflict"
			continue
		}
		rewritePath := strings.TrimSpace(nodePaths[binding.NodeID])
		if rewritePath == "" {
			rewritePath = "/" + strings.TrimSpace(binding.GroupName)
		}
		if rewritePath != "" && !strings.HasPrefix(rewritePath, "/") {
			rewritePath = "/" + rewritePath
		}
		if rewritePath != "" && rewritePath != "/" {
			rewrites = append(rewrites, RewriteRoute{
				RouteKey: routeKey,
				Path:     rewritePath,
			})
		}

		current, exists := currentByRouteKey[routeKey]
		switch {
		case !exists:
			actions = append(actions, RouteAction{
				Action:    "add_route",
				RouteKey:  routeKey,
				OriginID:  binding.OriginID,
				Host:      host,
				GroupName: binding.GroupName,
				Reason:    "route missing from distribution",
			})
			driftStatus = "drifted"
		case !current.Managed:
			actions = append(actions, RouteAction{
				Action:    "conflict",
				RouteKey:  routeKey,
				OriginID:  current.OriginID,
				Host:      current.Host,
				GroupName: binding.GroupName,
				Reason:    "route is occupied by an unmanaged origin",
			})
			driftStatus = "conflict"
		case current.OriginID != binding.OriginID || current.Host != host:
			actions = append(actions, RouteAction{
				Action:    "replace_route",
				RouteKey:  routeKey,
				OriginID:  binding.OriginID,
				Host:      host,
				GroupName: binding.GroupName,
				Reason:    "route target differs from platform binding",
			})
			driftStatus = "drifted"
		}
	}

	for routeKey, current := range currentByRouteKey {
		if _, ok := desiredByRouteKey[routeKey]; ok {
			continue
		}
		if !current.Managed {
			continue
		}
		actions = append(actions, RouteAction{
			Action:   "remove_route",
			RouteKey: routeKey,
			OriginID: current.OriginID,
			Host:     current.Host,
			Reason:   "route exists in distribution but not in platform bindings",
		})
		driftStatus = "drifted"
	}

	if len(actions) == 0 {
		actions = []RouteAction{{Action: "noop", Reason: "distribution is in sync"}}
	}

	return &SyncPlan{
		Actions:       actions,
		RewriteRoutes: rewrites,
		DriftStatus:   driftStatus,
	}
}
