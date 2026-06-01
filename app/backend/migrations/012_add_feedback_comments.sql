-- Free-text feedback left by portal users (patients, physicians or anonymous
-- visitors). The message is stored verbatim (after server-side sanitization:
-- control-char stripping + length cap) and is always HTML-escaped on render, so
-- the column never participates in SQL string building (parameterized inserts
-- only) nor in HTML without escaping. Actor context is derived server-side from
-- the resolved session, not from client-supplied fields.
CREATE TABLE IF NOT EXISTS feedback_comments (
  id          bigserial PRIMARY KEY,
  message     text NOT NULL,
  actor_kind  text NOT NULL,
  actor_id    text,
  actor_role  text,
  actor_name  text,
  client_ip   text,
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_feedback_comments_created_at
  ON feedback_comments(created_at DESC);
