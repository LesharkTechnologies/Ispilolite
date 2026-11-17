package bootstrap

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEnsureAdminAccountSkippedWhenUnset(t *testing.T) {
	_ = os.Unsetenv("ADMIN_USERNAME")
	_ = os.Unsetenv("ADMIN_PASSWORD")

	err := EnsureAdminAccount(context.Background(), nil, log.Default())
	assert.NoError(t, err)
}

func TestEnsureAdminAccountNilDB(t *testing.T) {
	_ = os.Setenv("ADMIN_USERNAME", "admin")
	_ = os.Setenv("ADMIN_PASSWORD", "SuperPassword123!")
	defer os.Unsetenv("ADMIN_USERNAME")
	defer os.Unsetenv("ADMIN_PASSWORD")

	err := EnsureAdminAccount(context.Background(), nil, log.Default())
	assert.NoError(t, err)
}
