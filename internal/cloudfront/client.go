package cloudfront

import "context"

type DistributionSummary struct {
	DistributionID           string   `json:"distributionId"`
	DomainName               string   `json:"domainName"`
	Status                   string   `json:"status"`
	Comment                  string   `json:"comment,omitempty"`
	Aliases                  []string `json:"aliases,omitempty"`
	ManagedResourcesDetected bool     `json:"managedResourcesDetected,omitempty"`
}

type CreateDistributionInput struct {
	Comment string
	Nodes   []CreateDistributionNode
}

type CreateDistributionNode struct {
	NodeID      string
	RouteKey    string
	Host        string
	RewritePath string
}

// DistributionState captures the current CloudFront routing state needed by the planner.
type DistributionState struct {
	DistributionID string
	DomainName     string
	Status         string
	Comment        string
	Aliases        []string
	Origins        []OriginState
	Behaviors      []BehaviorState
}

// OriginState represents a current CloudFront origin.
type OriginState struct {
	OriginID   string
	DomainName string
}

// BehaviorState represents one cache behavior routing rule.
type BehaviorState struct {
	PathPattern string
	OriginID    string
}

// Client is the interface for AWS CloudFront operations.
type Client interface {
	ListDistributions(ctx context.Context) ([]DistributionSummary, error)
	GetDistribution(ctx context.Context, distributionID string) (*DistributionState, error)
	ApplyDistributionRoutes(ctx context.Context, distributionID string, actions []RouteAction, rewrites []RewriteRoute) error
}

// RouteAction is the planner-friendly representation of one CloudFront path mutation.
type RouteAction struct {
	Action    string
	RouteKey  string
	OriginID  string
	Host      string
	GroupName string
	Reason    string
}

// RewriteRoute maps the stable CloudFront-facing path to the node's actual
// websocket path. CloudFront path behaviors choose the origin before functions
// run, so this rewrite does not change which node receives the request.
type RewriteRoute struct {
	RouteKey string `json:"routeKey"`
	Path     string `json:"path"`
}
