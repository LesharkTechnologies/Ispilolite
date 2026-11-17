package redis

import (
	"context"
	"encoding/json"
	"time"

	goredis "github.com/go-redis/redis/v8"
	"ispilolite/pkg/database"
	otphash "ispilolite/pkg/otp"
)

type CacheRepo struct{ client *goredis.Client }

func NewCacheRepo() (*CacheRepo, error) {
	client, err := database.MustRedis()
	if err != nil {
		return nil, err
	}
	return &CacheRepo{client: client}, nil
}

// SetOTP stores only a keyed hash of the verification code, never the
// plaintext code, so a Redis snapshot or memory dump cannot expose codes.
func (r *CacheRepo) SetOTP(userID, code string, expiration time.Duration) error {
	return r.client.Set(context.Background(), "otp:"+userID, otphash.Hash(code), expiration).Err()
}

// GetOTP returns the stored OTP digest for constant-time comparison through
// otp.Matches. It errors when no active code exists.
func (r *CacheRepo) GetOTP(userID string) (string, error) {
	return r.client.Get(context.Background(), "otp:"+userID).Result()
}

func (r *CacheRepo) DeleteOTP(userID string) error {
	return r.client.Del(context.Background(), "otp:"+userID).Err()
}

func (r *CacheRepo) Set(key string, value interface{}, expiration time.Duration) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return r.client.Set(context.Background(), key, encoded, expiration).Err()
}

func (r *CacheRepo) SetIfAbsent(key string, value interface{}, expiration time.Duration) (bool, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return false, err
	}
	return r.client.SetNX(context.Background(), key, encoded, expiration).Result()
}

func (r *CacheRepo) Get(key string) (string, error) {
	return r.client.Get(context.Background(), key).Result()
}

func (r *CacheRepo) SetRevokedToken(token string, expiration time.Duration) error {
	return r.client.Set(context.Background(), "revoked:"+token, "1", expiration).Err()
}

func (r *CacheRepo) IsTokenRevoked(token string) bool {
	exists, err := r.client.Exists(context.Background(), "revoked:"+token).Result()
	return err == nil && exists > 0
}

// IncrementOTPAttempts increments a per-phone OTP attempt counter and returns
// the current count. Returns false if the phone has exceeded the limit.
func (r *CacheRepo) IncrementOTPAttempts(phone string, limit int, window time.Duration) (int, bool) {
	key := "otp_attempts:" + phone
	current, err := r.client.Incr(context.Background(), key).Result()
	if err != nil {
		return 0, false
	}
	if current == 1 {
		_ = r.client.Expire(context.Background(), key, window)
	}
	return int(current), current <= int64(limit)
}

// ResetOTPAttempts clears the OTP attempt counter for a phone after a
// successful verification.
func (r *CacheRepo) ResetOTPAttempts(phone string) {
	_ = r.client.Del(context.Background(), "otp_attempts:"+phone).Err()
}
