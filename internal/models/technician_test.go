package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPublicTechnicianJSONDoesNotExposeContactFields(t *testing.T) {
	payload, err := json.Marshal(Technician{ID: "tech-1", UserID: "user-1", Name: "Technician", Phone: "+254700000000", Email: "private@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	if strings.Contains(text, "phone") || strings.Contains(text, "email") || strings.Contains(text, "+254") || strings.Contains(text, "private@example.com") {
		t.Fatalf("private technician contact leaked: %s", text)
	}
}
