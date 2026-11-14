package redis

import (
	"context"
	"encoding/json"
	"time"

	goredis "github.com/go-redis/redis/v8"
	"ispilolite/pkg/database"
)

type CacheRepo struct{ client *goredis.Client }

func NewCacheRepo() (*CacheRepo, error) {
	client, err := database.MustRedis()
	if err != nil {
		return nil, err
	}
	return &CacheRepo{client: client}, nil
}

func (r *CacheRepo) SetOTP(userID, otp string, expiration time.Duration) error {
	return r.client.Set(context.Background(), "otp:"+userID, otp, expiration).Err()
}

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
