package cloudfront

import "context"

// DistributionState captures the current state of a CloudFront distribution.
type DistributionState struct {
	DistributionID  string
	DomainName      string
	CallerReference string
	Origins         []OriginState
	// Cache behaviors are not needed for plan computation
}

// OriginState represents a current CloudFront origin.
type OriginState struct {
	OriginID   string
	DomainName string // e.g. "node-abc.example.com"
	PathPrefix string // e.g. "/route-key-1234"
}

// Client is the interface for AWS CloudFront operations.
// Implementations wrap the AWS SDK.
type Client interface {
	GetDistribution(ctx context.Context, distributionID string) (*DistributionState, error)
	UpdateOrigins(ctx context.Context, distributionID string, toAdd []OriginState, toRemove []string, toUpdate []OriginState) error
}
