package service

import (
	"net"
	"net/url"
	"strconv"
	"time"
)

const (
	FallbackModeNone   = "none"
	FallbackModeProxy  = "proxy"
	FallbackModeDirect = "direct"
)

type Proxy struct {
	ID                int64
	Name              string
	Protocol          string
	Host              string
	Port              int
	Username          string
	Password          string
	Status            string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	ExpiresAt         *time.Time
	FallbackMode      string
	BackupProxyID     *int64
	ExpiryWarnDays    int
	NodeTestPort      *int
	MihomoPrimaryNode string
	MihomoGroup       string
}

func (p *Proxy) IsActive() bool {
	return p.Status == StatusActive
}

// IsExpired 报告代理是否已过期（基于 expires_at，与 status 无关）。
func (p *Proxy) IsExpired(now time.Time) bool {
	return p.ExpiresAt != nil && !p.ExpiresAt.After(now)
}

func (p *Proxy) URL() string {
	u := &url.URL{
		Scheme: p.Protocol,
		Host:   net.JoinHostPort(p.Host, strconv.Itoa(p.Port)),
	}
	if p.Username != "" && p.Password != "" {
		u.User = url.UserPassword(p.Username, p.Password)
	}
	return u.String()
}

type ProxyWithAccountCount struct {
	Proxy
	AccountCount       int64
	LatencyMs          *int64
	LatencyStatus      string
	LatencyMessage     string
	IPAddress          string
	Country            string
	CountryCode        string
	Region             string
	City               string
	QualityStatus      string
	QualityScore       *int
	QualityGrade       string
	QualitySummary     string
	QualityChecked     *int64
	NodeStatus         string
	NodeCheckedAt      *time.Time
	BusinessStatus     string
	BusinessLatencyMs  *int64
	BusinessCheckedAt  *time.Time
	PreferredNode      string
	IsBackup           bool
	RouteStatus        string
	RouteCheckedAt     *time.Time
	RouteError         string
	RouteFixed         bool
	SampleID           string
	SampledPod         string
	ProbeConfigVersion string
}

type ProxyAccountSummary struct {
	ID       int64
	Name     string
	Platform string
	Type     string
	Notes    *string
}
