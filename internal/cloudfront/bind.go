package cloudfront

import (
	"encoding/json"

	"v2ray-platform/internal/domain"
	"v2ray-platform/internal/store"
)

// BindService merges platform node data with AWS origins to produce bindings.
type BindService struct {
	store store.Store
}

// NewBindService creates a new bind service.
func NewBindService(store store.Store) *BindService {
	return &BindService{store: store}
}

// BindResult contains the output of a bind operation.
type BindResult struct {
	Bindings       []domain.CloudFrontBinding `json:"bindings"`
	MatchedCount   int                        `json:"matchedCount"`
	UnmatchedCount int                        `json:"unmatchedCount"`
}

// BindNodes matches platform nodes against discovered CloudFront origins.
// It produces bindings by matching on route_key.
func (s *BindService) BindNodes() (*BindResult, error) {
	cfg, err := s.store.GetCloudFrontConfig()
	if err != nil {
		return nil, err
	}

	// Parse existing origins
	var origins []domain.CloudFrontOrigin
	if cfg.OriginsJSON != "" {
		if err := json.Unmarshal([]byte(cfg.OriginsJSON), &origins); err != nil {
			return nil, err
		}
	}

	// Get all nodes
	nodes := s.store.ListNodes()

	// Build origin lookup by routeKey
	originByRouteKey := make(map[string]domain.CloudFrontOrigin)
	for _, o := range origins {
		if o.RouteKey != "" {
			originByRouteKey[o.RouteKey] = o
		}
	}

	var bindings []domain.CloudFrontBinding
	matched := 0
	unmatched := 0

	for _, node := range nodes {
		if node.RouteKey == "" {
			unmatched++
			continue
		}
		origin, exists := originByRouteKey[node.RouteKey]
		if !exists {
			unmatched++
			continue
		}
		bindings = append(bindings, domain.CloudFrontBinding{
			NodeID:    node.ID,
			OriginID:  origin.OriginID,
			RouteKey:  node.RouteKey,
			GroupName: node.Name, // use node name as group name
		})
		matched++
	}

	// Persist bindings
	bindingsJSON, err := json.Marshal(bindings)
	if err != nil {
		return nil, err
	}
	if err := s.store.UpdateCloudFrontBindings(string(bindingsJSON)); err != nil {
		return nil, err
	}

	return &BindResult{
		Bindings:       bindings,
		MatchedCount:   matched,
		UnmatchedCount: unmatched,
	}, nil
}
