package redis

import (
	"testing"
	"time"

	goredis "github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	miniredis "github.com/alicebob/miniredis/v2"
	"ispilolite/pkg/otp"
)

func TestSetOTPStoresHashNotPlaintext(t *testing.T) {
	server := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: server.Addr()})
	defer client.Close()
	repo := &CacheRepo{client: client}

	require.NoError(t, repo.SetOTP("user-1", "123456", time.Minute))

	raw, err := server.Get("otp:user-1")
	require.NoError(t, err)
	assert.NotEqual(t, "123456", raw, "plaintext OTP must never be persisted in Redis")
	assert.Equal(t, otp.Hash("123456"), raw, "stored value must be the keyed OTP digest")

	stored, err := repo.GetOTP("user-1")
	require.NoError(t, err)
	assert.True(t, otp.Matches("123456", stored), "correct code must verify against the stored digest")
	assert.False(t, otp.Matches("654321", stored), "wrong code must not verify")

	require.NoError(t, repo.DeleteOTP("user-1"))
	assert.False(t, server.Exists("otp:user-1"))
}

func TestGetOTPMissingKey(t *testing.T) {
	server := miniredis.RunT(t)
	client := goredis.NewClient(&goredis.Options{Addr: server.Addr()})
	defer client.Close()
	repo := &CacheRepo{client: client}

	_, err := repo.GetOTP("missing-user")
	assert.Error(t, err, "an absent OTP must surface as an error, not an empty code")
}
