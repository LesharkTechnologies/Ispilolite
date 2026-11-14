package i18n

import "testing"

func TestTranslateSwahili(t *testing.T) {
	got := Translate("sw-KE", "job.completed")
	if got == "job.completed" || got == catalogs["en"]["job.completed"] {
		t.Fatalf("expected Swahili translation, got %q", got)
	}
}

func TestUnknownLocaleFallsBackToEnglish(t *testing.T) {
	if got := Translate("fr", "job.completed"); got != catalogs["en"]["job.completed"] {
		t.Fatalf("unexpected fallback: %q", got)
	}
}
