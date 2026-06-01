-- Usage metrics (Option B): an append-only table inside the product database,
-- intentionally decoupled from operational tables. Each row is one occurrence
-- of an auditable/metered Action (see rbac.go action catalog). PHI is not the
-- purpose here; action-specific dimensions live in the jsonb `dims` column.
CREATE TABLE IF NOT EXISTS usage_events (
  id          bigserial PRIMARY KEY,
  action      text NOT NULL,
  actor_kind  text NOT NULL,
  actor_id    text,
  actor_role  text,
  outcome     text NOT NULL,
  status_code integer,
  latency_ms  bigint,
  dims        jsonb NOT NULL DEFAULT '{}'::jsonb,
  occurred_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_usage_events_action_time
  ON usage_events(action, occurred_at DESC);

CREATE INDEX IF NOT EXISTS idx_usage_events_actor
  ON usage_events(actor_kind, actor_id);

CREATE INDEX IF NOT EXISTS idx_usage_events_occurred_at
  ON usage_events(occurred_at DESC);
