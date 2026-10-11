ALTER TABLE health_checks ADD COLUMN current_state TEXT NOT NULL DEFAULT 'unknown'
    CHECK (current_state IN ('checking', 'healthy', 'degraded', 'offline', 'unknown', 'agent_unreachable'));
ALTER TABLE health_checks ADD COLUMN failure_count INTEGER NOT NULL DEFAULT 0 CHECK (failure_count >= 0);
ALTER TABLE health_checks ADD COLUMN recovery_count INTEGER NOT NULL DEFAULT 0 CHECK (recovery_count >= 0);
ALTER TABLE health_checks ADD COLUMN last_checked_at INTEGER;
CREATE INDEX health_checks_due_idx ON health_checks(enabled, last_checked_at);
