package cloudfront

import (
	"context"
	"encoding/json"

	"v2ray-platform/internal/domain"
	"v2ray-platform/internal/store"
)

// SyncService executes sync plans against AWS CloudFront.
type SyncService struct {
	store  store.Store
	client Client
}

// NewSyncService creates a new sync service.
func NewSyncService(store store.Store, client Client) *SyncService {
	return &SyncService{store: store, client: client}
}

// SyncResult contains the outcome of a sync operation.
type SyncResult struct {
	ActionsApplied int    `json:"actionsApplied"`
	DriftStatus    string `json:"driftStatus"`
	SyncStatus     string `json:"syncStatus"`
	Error          string `json:"error,omitempty"`
}

// ExecutePlan runs the given sync plan against the CloudFront distribution.
// Updates the config's sync status in the store.
func (s *SyncService) ExecutePlan(ctx context.Context, plan *SyncPlan) (*SyncResult, error) {
	cfg, err := s.store.GetCloudFrontConfig()
	if err != nil {
		return nil, err
	}

	if plan.DriftStatus == "in_sync" {
		// Nothing to do
		s.updateStatus(cfg, "synced", "in_sync", "", plan.Actions)
		return &SyncResult{
			ActionsApplied: 0,
			DriftStatus:    "in_sync",
			SyncStatus:     "synced",
		}, nil
	}

	// Collect actions by type
	var toAdd []OriginState
	var toRemove []string
	var toUpdate []OriginState
	applied := 0

	for _, action := range plan.Actions {
		switch action.Action {
		case "add_origin":
			toAdd = append(toAdd, OriginState{
				OriginID:   action.OriginID,
				DomainName: action.Host,
				PathPrefix: "/" + action.RouteKey,
			})
			applied++
		case "replace_origin":
			toUpdate = append(toUpdate, OriginState{
				OriginID:   action.OriginID,
				DomainName: action.Host,
				PathPrefix: "/" + action.RouteKey,
			})
			applied++
		case "remove_origin":
			toRemove = append(toRemove, action.OriginID)
			applied++
		case "noop":
			// Skip
		}
	}

	// Execute mutations
	if err := s.client.UpdateOrigins(ctx, cfg.DistributionID, toAdd, toRemove, toUpdate); err != nil {
		s.updateStatus(cfg, "failed", plan.DriftStatus, err.Error(), plan.Actions)
		return &SyncResult{
			ActionsApplied: 0,
			DriftStatus:    plan.DriftStatus,
			SyncStatus:     "failed",
			Error:          err.Error(),
		}, nil
	}

	s.updateStatus(cfg, "synced", "in_sync", "", plan.Actions)
	return &SyncResult{
		ActionsApplied: applied,
		DriftStatus:    "in_sync",
		SyncStatus:     "synced",
	}, nil
}

func (s *SyncService) updateStatus(cfg *domain.CloudFrontConfig, syncStatus, driftStatus, syncErr string, actions []domain.CloudFrontSyncAction) {
	planJSON, _ := json.Marshal(actions)
	input := store.UpdateCloudFrontSyncInput{
		DistributionID:         cfg.DistributionID,
		DistributionDomainName: cfg.DistributionDomainName,
		Mode:                   cfg.Mode,
		PlanJSON:               string(planJSON),
		SyncStatus:             syncStatus,
		DriftStatus:            driftStatus,
		LastSyncError:          syncErr,
	}
	s.store.UpdateCloudFrontSyncStatus(input)
}
