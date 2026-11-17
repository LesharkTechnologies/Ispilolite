package models

import (
	"testing"
)

func TestUser_EffectiveDisplayNameAndIdentity(t *testing.T) {
	u1 := &User{
		Name:         "Collins Muthomi",
		DisplayName:  "Leshark",
		VerifiedName: "Collins Muthomi",
	}
	if u1.EffectiveDisplayName() != "Leshark" {
		t.Errorf("expected 'Leshark', got %q", u1.EffectiveDisplayName())
	}
	if u1.IdentityLabel() != "identity [Collins Muthomi]" {
		t.Errorf("expected 'identity [Collins Muthomi]', got %q", u1.IdentityLabel())
	}

	u2 := &User{
		Name: "Alice Wanjiku",
	}
	if u2.EffectiveDisplayName() != "Alice Wanjiku" {
		t.Errorf("expected 'Alice Wanjiku', got %q", u2.EffectiveDisplayName())
	}
	if u2.IdentityLabel() != "identity [Alice Wanjiku]" {
		t.Errorf("expected 'identity [Alice Wanjiku]', got %q", u2.IdentityLabel())
	}
}

func TestTechnician_EffectiveDisplayNameAndIdentity(t *testing.T) {
	tech := &Technician{
		Name:         "Collins Muthomi",
		DisplayName:  "Leshark",
		VerifiedName: "Collins Muthomi",
	}
	if tech.EffectiveDisplayName() != "Leshark" {
		t.Errorf("expected 'Leshark', got %q", tech.EffectiveDisplayName())
	}
	if tech.IdentityLabel() != "identity [Collins Muthomi]" {
		t.Errorf("expected 'identity [Collins Muthomi]', got %q", tech.IdentityLabel())
	}
}
