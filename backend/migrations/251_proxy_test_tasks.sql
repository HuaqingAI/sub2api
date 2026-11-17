-- Mihomo metadata is only used for original-node diagnostics. Business proxy URLs remain unchanged.
ALTER TABLE proxies ADD COLUMN IF NOT EXISTS node_test_port INTEGER;
ALTER TABLE proxies ADD COLUMN IF NOT EXISTS mihomo_primary_node VARCHAR(100);
ALTER TABLE proxies ADD COLUMN IF NOT EXISTS mihomo_group VARCHAR(100);

CREATE TABLE IF NOT EXISTS proxy_test_tasks (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    cron_expression VARCHAR(100) NOT NULL,
    timezone VARCHAR(100) NOT NULL DEFAULT 'Asia/Shanghai',
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    recipients JSONB NOT NULL DEFAULT '[]'::jsonb,
    failure_threshold INTEGER NOT NULL DEFAULT 1 CHECK (failure_threshold BETWEEN 1 AND 20),
    timeout_seconds INTEGER NOT NULL DEFAULT 10 CHECK (timeout_seconds BETWEEN 3 AND 30),
    last_run_at TIMESTAMPTZ,
    next_run_at TIMESTAMPTZ,
    last_run_status VARCHAR(30) NOT NULL DEFAULT 'pending',
    last_error TEXT NOT NULL DEFAULT '',
    config_version BIGINT NOT NULL DEFAULT 1,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_proxy_test_tasks_due ON proxy_test_tasks (next_run_at) WHERE enabled = TRUE;

CREATE TABLE IF NOT EXISTS proxy_test_task_targets (
    task_id BIGINT NOT NULL REFERENCES proxy_test_tasks(id) ON DELETE CASCADE,
    proxy_id BIGINT NOT NULL REFERENCES proxies(id) ON DELETE CASCADE,
    last_node_status VARCHAR(20) NOT NULL DEFAULT 'unknown',
    last_checked_at TIMESTAMPTZ,
    last_error TEXT NOT NULL DEFAULT '',
    consecutive_failures INTEGER NOT NULL DEFAULT 0,
    failure_started_at TIMESTAMPTZ,
    notified_recipients JSONB NOT NULL DEFAULT '[]'::jsonb,
    last_probe_config_version TEXT NOT NULL DEFAULT '',
    last_result JSONB,
    PRIMARY KEY (task_id, proxy_id)
);
CREATE INDEX IF NOT EXISTS idx_proxy_test_task_targets_proxy ON proxy_test_task_targets (proxy_id);
