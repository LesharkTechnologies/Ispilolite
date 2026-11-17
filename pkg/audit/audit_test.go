package audit

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCleanIP(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"192.168.1.1:54321", "192.168.1.1"},
		{"[::1]:8080", "::1"},
		{"10.0.0.1", "10.0.0.1"},
		{"  172.16.0.5  ", "172.16.0.5"},
		{"", ""},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, cleanIP(tt.input))
	}
}

func TestCleanActorID(t *testing.T) {
	assert.Equal(t, "", cleanActorID(""))
	assert.Equal(t, "", cleanActorID("not-a-uuid"))
	validUUID := "a0000000-0000-0000-0000-000000000001"
	assert.Equal(t, validUUID, cleanActorID(validUUID))
}

func TestNormalizeAction(t *testing.T) {
	assert.Equal(t, "LOGIN", normalizeAction("", "user.login"))
	assert.Equal(t, "REGISTER", normalizeAction("", "user.register"))
	assert.Equal(t, "CREATE", normalizeAction("", "job.create"))
	assert.Equal(t, "UPDATE", normalizeAction("", "job.update_status"))
	assert.Equal(t, "DELETE", normalizeAction("", "user.delete"))
	assert.Equal(t, "EXPORT", normalizeAction("", "admin.audit_export"))
	assert.Equal(t, "REQUEST", normalizeAction("", "job.apply"))
	assert.Equal(t, "CREATE", normalizeAction("CREATE", "anything"))
	assert.Equal(t, "SYSTEM", normalizeAction("", "unknown.event"))
}

func TestUserCategory(t *testing.T) {
	assert.Equal(t, "client", UserCategory("customer"))
	assert.Equal(t, "client", UserCategory("client"))
	assert.Equal(t, "isp", UserCategory("isp"))
	assert.Equal(t, "technicians", UserCategory("technician"))
	assert.Equal(t, "technicians", UserCategory("technicians"))
	assert.Equal(t, "admin", UserCategory("admin"))
	assert.Equal(t, "anonymous", UserCategory(""))
}

func TestRecordNilStore(t *testing.T) {
	// Should not panic with unconfigured store
	Configure(nil, "")
	err := Record(context.Background(), Event{
		Event:       "test.event",
		Action:      "CREATE",
		Description: "test event",
		OccurredAt:  time.Now(),
	})
	assert.NoError(t, err)
}
