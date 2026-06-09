package cloudfront

import (
	"context"
	"encoding/json"

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
func (s *SyncService) ExecutePlan(ctx context.Context, plan *SyncPlan) (*SyncResult, error) {
	cfg, err := s.store.GetCloudFrontConfig()
	if err != nil {
		return nil, err
	}

	if plan.DriftStatus == "in_sync" {
		if len(plan.RewriteRoutes) > 0 {
			if err := s.client.ApplyDistributionRoutes(ctx, cfg.DistributionID, nil, plan.RewriteRoutes); err != nil {
				s.updateStatus(cfg.DistributionID, cfg.DistributionDomainName, cfg.Mode, "failed", plan.DriftStatus, err.Error(), plan.Actions)
				return &SyncResult{ActionsApplied: 0, DriftStatus: plan.DriftStatus, SyncStatus: "failed", Error: err.Error()}, nil
			}
		}
		s.updateStatus(cfg.DistributionID, cfg.DistributionDomainName, cfg.Mode, "synced", "in_sync", "", plan.Actions)
		return &SyncResult{ActionsApplied: 0, DriftStatus: "in_sync", SyncStatus: "synced"}, nil
	}
	if plan.DriftStatus == "conflict" {
		errMsg := "cloudfront sync blocked by unmanaged resource conflict"
		s.updateStatus(cfg.DistributionID, cfg.DistributionDomainName, cfg.Mode, "failed", "conflict", errMsg, plan.Actions)
		return &SyncResult{ActionsApplied: 0, DriftStatus: "conflict", SyncStatus: "failed", Error: errMsg}, nil
	}

	if err := s.client.ApplyDistributionRoutes(ctx, cfg.DistributionID, plan.Actions, plan.RewriteRoutes); err != nil {
		s.updateStatus(cfg.DistributionID, cfg.DistributionDomainName, cfg.Mode, "failed", plan.DriftStatus, err.Error(), plan.Actions)
		return &SyncResult{ActionsApplied: 0, DriftStatus: plan.DriftStatus, SyncStatus: "failed", Error: err.Error()}, nil
	}

	applied := 0
	for _, action := range plan.Actions {
		if action.Action != "noop" {
			applied++
		}
	}
	s.updateStatus(cfg.DistributionID, cfg.DistributionDomainName, cfg.Mode, "synced", "in_sync", "", plan.Actions)
	return &SyncResult{
		ActionsApplied: applied,
		DriftStatus:    "in_sync",
		SyncStatus:     "synced",
	}, nil
}

func (s *SyncService) updateStatus(distributionID, distributionDomainName, mode, syncStatus, driftStatus, syncErr string, actions []RouteAction) {
	planJSON, _ := json.Marshal(actions)
	_ = s.store.UpdateCloudFrontSyncStatus(store.UpdateCloudFrontSyncInput{
		DistributionID:         distributionID,
		DistributionDomainName: distributionDomainName,
		Mode:                   mode,
		PlanJSON:               string(planJSON),
		SyncStatus:             syncStatus,
		DriftStatus:            driftStatus,
		LastSyncError:          syncErr,
	})
}
