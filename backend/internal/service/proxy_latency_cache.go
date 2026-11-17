package service

import (
	"context"
	"time"
)

type ProxyLatencyInfo struct {
	// QualityOnly is an internal write mode, never a persisted/API field. Cache
	// implementations merge diagnostic fields into their latest sampling round.
	QualityOnly        bool       `json:"-"`
	Success            bool       `json:"success"`
	LatencyMs          *int64     `json:"latency_ms,omitempty"`
	Message            string     `json:"message,omitempty"`
	IPAddress          string     `json:"ip_address,omitempty"`
	Country            string     `json:"country,omitempty"`
	CountryCode        string     `json:"country_code,omitempty"`
	Region             string     `json:"region,omitempty"`
	City               string     `json:"city,omitempty"`
	QualityStatus      string     `json:"quality_status,omitempty"`
	QualityScore       *int       `json:"quality_score,omitempty"`
	QualityGrade       string     `json:"quality_grade,omitempty"`
	QualitySummary     string     `json:"quality_summary,omitempty"`
	QualityCheckedAt   *int64     `json:"quality_checked_at,omitempty"`
	QualityCFRay       string     `json:"quality_cf_ray,omitempty"`
	UpdatedAt          time.Time  `json:"updated_at"`
	NodeStatus         string     `json:"node_status,omitempty"`
	NodeCheckedAt      *time.Time `json:"node_checked_at,omitempty"`
	BusinessStatus     string     `json:"business_status,omitempty"`
	BusinessLatencyMs  *int64     `json:"business_latency_ms,omitempty"`
	BusinessCheckedAt  *time.Time `json:"business_checked_at,omitempty"`
	PreferredNode      string     `json:"preferred_node,omitempty"`
	IsBackup           bool       `json:"is_backup"`
	RouteStatus        string     `json:"route_status,omitempty"`
	RouteCheckedAt     *time.Time `json:"route_checked_at,omitempty"`
	RouteError         string     `json:"route_error,omitempty"`
	RouteFixed         bool       `json:"route_fixed"`
	SampleID           string     `json:"sample_id,omitempty"`
	SampledPod         string     `json:"sampled_pod,omitempty"`
	ProbeConfigVersion string     `json:"probe_config_version,omitempty"`
}

// ProxyLatencyInvalidator fences in-flight probes after a configuration update.
// It is optional so existing cache implementations retain their narrow interface.
type ProxyLatencyInvalidator interface {
	InvalidateProxyLatency(ctx context.Context, proxyID int64, version string) error
}

type ProxyLatencyCache interface {
	GetProxyLatencies(ctx context.Context, proxyIDs []int64) (map[int64]*ProxyLatencyInfo, error)
	SetProxyLatency(ctx context.Context, proxyID int64, info *ProxyLatencyInfo) error
}
