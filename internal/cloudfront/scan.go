package cloudfront

import (
	"context"
	"encoding/json"

	"v2ray-platform/internal/domain"
	"v2ray-platform/internal/store"
)

// ScanService fetches CloudFront distribution state from AWS.
type ScanService struct {
	store  store.Store
	client Client
}

// NewScanService creates a new scan service.
func NewScanService(store store.Store, client Client) *ScanService {
	return &ScanService{store: store, client: client}
}

// ScanResult contains the output of a distribution scan.
type ScanResult struct {
	DistributionID string                   `json:"distributionId"`
	DomainName     string                   `json:"domainName"`
	Origins        []domain.CloudFrontOrigin `json:"origins"`
}

// ScanDistribution fetches the current state of the configured distribution from AWS.
// Returns the discovered origins and updates the config's origins_json.
func (s *ScanService) ScanDistribution(ctx context.Context) (*ScanResult, error) {
	cfg, err := s.store.GetCloudFrontConfig()
	if err != nil {
		return nil, err
	}
	if cfg.DistributionID == "" {
		return nil, store.ErrNotFound
	}

	dist, err := s.client.GetDistribution(ctx, cfg.DistributionID)
	if err != nil {
		return nil, err
	}

	// Map AWS origins to domain types
	var origins []domain.CloudFrontOrigin
	for _, o := range dist.Origins {
		routeKey := ""
		if len(o.PathPrefix) > 1 {
			routeKey = o.PathPrefix[1:] // strip leading "/"
		}
		origins = append(origins, domain.CloudFrontOrigin{
			OriginID: o.OriginID,
			Host:     o.DomainName,
			RouteKey: routeKey,
		})
	}

	// Save discovered origins
	originsJSON, _ := json.Marshal(origins)
	if err := s.store.UpdateCloudFrontDistribution(dist.DistributionID, dist.DomainName, cfg.Mode); err != nil {
		return nil, err
	}

	if err := s.store.UpdateCloudFrontOrigins(string(originsJSON)); err != nil {
		return nil, err
	}

	return &ScanResult{
		DistributionID: dist.DistributionID,
		DomainName:     dist.DomainName,
		Origins:        origins,
	}, nil
}
