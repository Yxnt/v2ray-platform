package cloudfront

import (
	"context"
	"encoding/json"
	"strings"

	"v2ray-platform/internal/domain"
	"v2ray-platform/internal/store"
)

// ScanService fetches CloudFront distribution state from AWS.
type ScanService struct {
	store  store.Store
	client Client
}

func NewScanService(store store.Store, client Client) *ScanService {
	return &ScanService{store: store, client: client}
}

type ScanResult struct {
	DistributionID      string                     `json:"distributionId"`
	DomainName          string                     `json:"domainName"`
	Origins             []domain.CloudFrontOrigin  `json:"origins"`
	DistributionOrigins []OriginState              `json:"distributionOrigins"`
	CacheBehaviors      []BehaviorState            `json:"cacheBehaviors"`
	Parameters          []ParameterDefinitionState `json:"parameters"`
}

func (s *ScanService) ListDistributions(ctx context.Context) ([]DistributionSummary, error) {
	return s.client.ListDistributions(ctx)
}

// ScanDistribution fetches the current bound distribution state from AWS and
// persists the reconstructed route table as origins_json.
func (s *ScanService) ScanDistribution(ctx context.Context) (*ScanResult, error) {
	cfg, err := s.store.GetCloudFrontConfig()
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.DistributionID) == "" {
		return nil, store.ErrNotFound
	}

	dist, err := s.client.GetDistribution(ctx, cfg.DistributionID)
	if err != nil {
		return nil, err
	}
	return s.persistDistributionState(cfg.Mode, dist)
}

// ScanDistributionByID fetches a specific distribution and persists it as the current target.
func (s *ScanService) ScanDistributionByID(ctx context.Context, distributionID, mode string) (*ScanResult, error) {
	dist, err := s.client.GetDistribution(ctx, distributionID)
	if err != nil {
		return nil, err
	}
	return s.persistDistributionState(firstNonEmpty(mode, "adopted"), dist)
}

func (s *ScanService) persistDistributionState(mode string, dist *DistributionState) (*ScanResult, error) {
	originByID := make(map[string]OriginState, len(dist.Origins))
	for _, origin := range dist.Origins {
		originByID[origin.OriginID] = origin
	}

	origins := make([]domain.CloudFrontOrigin, 0, len(dist.Behaviors))
	for _, behavior := range dist.Behaviors {
		routeKey := strings.TrimPrefix(strings.TrimSpace(behavior.PathPattern), "/")
		if routeKey == "" {
			continue
		}
		origin, ok := originByID[behavior.OriginID]
		if !ok {
			continue
		}
		origins = append(origins, domain.CloudFrontOrigin{
			OriginID: behavior.OriginID,
			Host:     origin.DomainName,
			RouteKey: routeKey,
		})
	}

	originsJSON, _ := json.Marshal(origins)
	if err := s.store.UpdateCloudFrontDistribution(dist.DistributionID, dist.DomainName, mode); err != nil {
		return nil, err
	}
	if err := s.store.UpdateCloudFrontOrigins(string(originsJSON)); err != nil {
		return nil, err
	}

	return &ScanResult{
		DistributionID:      dist.DistributionID,
		DomainName:          dist.DomainName,
		Origins:             origins,
		DistributionOrigins: append([]OriginState(nil), dist.Origins...),
		CacheBehaviors:      append([]BehaviorState(nil), dist.Behaviors...),
		Parameters:          append([]ParameterDefinitionState(nil), dist.Parameters...),
	}, nil
}

func firstNonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
