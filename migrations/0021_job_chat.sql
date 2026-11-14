BEGIN;

CREATE TABLE IF NOT EXISTS job_messages (
  id UUID PRIMARY KEY,
  request_id UUID NOT NULL REFERENCES service_requests(id) ON DELETE CASCADE,
  sender_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  body TEXT NOT NULL DEFAULT '' CHECK(length(body) <= 4000),
  attachment_urls JSONB NOT NULL DEFAULT '[]'::jsonb,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  edited_at TIMESTAMPTZ,
  deleted_at TIMESTAMPTZ,
  CHECK(body <> '' OR jsonb_array_length(attachment_urls) > 0)
);
CREATE INDEX IF NOT EXISTS job_messages_request_idx ON job_messages(request_id,created_at DESC);

CREATE TABLE IF NOT EXISTS job_message_receipts (
  message_id UUID NOT NULL REFERENCES job_messages(id) ON DELETE CASCADE,
  user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  read_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY(message_id,user_id)
);

COMMIT;
