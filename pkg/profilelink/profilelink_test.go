package profilelink

import "testing"

func TestDomainChangeKeepsStoredProfilePath(t *testing.T) {
	path := "/u/amina-2026-00112233445566778899aabbccddeeff"
	old, err := New("https://old.example.com", true)
	if err != nil {
		t.Fatal(err)
	}
	next, err := New("https://new.example.com/", true)
	if err != nil {
		t.Fatal(err)
	}
	if got := old.URL(path); got != "https://old.example.com"+path {
		t.Fatal(got)
	}
	if got := next.URL(path); got != "https://new.example.com"+path {
		t.Fatal(got)
	}
}

func TestRejectsInvalidBase(t *testing.T) {
	for _, base := range []string{"https://evil.example/api/v1", "https://user:pass@example.com", "http://example.com", "https://example.com?token=secret", "not-a-url"} {
		if _, err := New(base, true); err == nil {
			t.Fatalf("accepted invalid production base %q", base)
		}
	}
}
