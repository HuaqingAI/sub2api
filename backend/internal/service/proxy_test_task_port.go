package service

import (
	"context"
	"time"
)

// ProxyTestTask stores a schedule and only its most recent diagnostic results.
type ProxyTestTask struct {
	ID               int64                  `json:"id"`
	Name             string                 `json:"name"`
	ProxyIDs         []int64                `json:"proxy_ids"`
	CronExpression   string                 `json:"cron_expression"`
	Timezone         string                 `json:"timezone"`
	Enabled          bool                   `json:"enabled"`
	Recipients       []string               `json:"recipients"`
	FailureThreshold int                    `json:"failure_threshold"`
	TimeoutSeconds   int                    `json:"timeout_seconds"`
	LastRunAt        *time.Time             `json:"last_run_at"`
	NextRunAt        *time.Time             `json:"next_run_at"`
	LastRunStatus    string                 `json:"last_run_status"`
	LastError        string                 `json:"last_error"`
	ConfigVersion    int64                  `json:"config_version"`
	CreatedAt        time.Time              `json:"created_at"`
	UpdatedAt        time.Time              `json:"updated_at"`
	Targets          []*ProxyTestTaskTarget `json:"targets"`
	SMTPConfigured   bool                   `json:"smtp_configured"`
}

type ProxyTestTaskTarget struct {
	TaskID                 int64            `json:"task_id"`
	ProxyID                int64            `json:"proxy_id"`
	ProxyName              string           `json:"proxy_name"`
	MihomoPrimaryNode      string           `json:"mihomo_primary_node,omitempty"`
	LastNodeStatus         string           `json:"last_node_status"`
	LastCheckedAt          *time.Time       `json:"last_checked_at"`
	LastError              string           `json:"last_error"`
	ConsecutiveFailures    int              `json:"consecutive_failures"`
	FailureStartedAt       *time.Time       `json:"failure_started_at"`
	NotifiedRecipients     []string         `json:"notified_recipients"`
	LastProbeConfigVersion string           `json:"last_probe_config_version,omitempty"`
	LastResult             *ProxyTestResult `json:"last_result,omitempty"`
}

type ProxyTestTaskInput struct {
	Name             *string  `json:"name"`
	ProxyIDs         []int64  `json:"proxy_ids"`
	CronExpression   *string  `json:"cron_expression"`
	Timezone         *string  `json:"timezone"`
	Enabled          *bool    `json:"enabled"`
	Recipients       []string `json:"recipients"`
	FailureThreshold *int     `json:"failure_threshold"`
	TimeoutSeconds   *int     `json:"timeout_seconds"`
}

// Lock callbacks receive a context tied to the dedicated PostgreSQL lock session.
// Configuration edits take the association lock before task locks; executions only take a task lock.
type ProxyTestTaskRepository interface {
	List(context.Context) ([]*ProxyTestTask, error)
	Get(context.Context, int64) (*ProxyTestTask, error)
	ListDue(context.Context, time.Time) ([]int64, error)
	Save(context.Context, *ProxyTestTask) (*ProxyTestTask, error)
	Delete(context.Context, int64) error
	SaveRun(context.Context, *ProxyTestTask, bool) error
	WithTaskLock(context.Context, int64, bool, func(context.Context) error) error
	WithProxyConfigLock(context.Context, int64, func(context.Context) error) error
	ResetProxyTargets(context.Context, int64) error
}

type ProxyTaskTester interface {
	TestProxy(context.Context, int64) (*ProxyTestResult, error)
}

type ProxyTaskMailer interface {
	GetSMTPConfig(context.Context) (*SMTPConfig, error)
	SendEmail(context.Context, string, string, string) error
}
