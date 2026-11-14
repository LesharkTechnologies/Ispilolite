-- Social follows are user-to-user relationships. Employment is separate and
-- continues to live in isp_technicians: following an ISP does not join it.
CREATE TABLE IF NOT EXISTS user_follows (
  follower_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  followee_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (follower_id, followee_id),
  CONSTRAINT user_follows_no_self CHECK (follower_id <> followee_id)
);

CREATE INDEX IF NOT EXISTS user_follows_followee_time_idx
  ON user_follows (followee_id, created_at DESC, follower_id DESC);
CREATE INDEX IF NOT EXISTS user_follows_follower_time_idx
  ON user_follows (follower_id, created_at DESC, followee_id DESC);
