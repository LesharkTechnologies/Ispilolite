package validation

import "testing"

func TestMediaURLsAcceptsConfiguredR2AndDeduplicates(t *testing.T) {
	t.Setenv("R2_PUBLIC_BASE_URL", "https://media.example.test")
	value := "https://media.example.test/ispilolite/profiles/user/photo.jpg"
	urls, err := MediaURLs([]string{value, value}, 5)
	if err != nil || len(urls) != 1 {
		t.Fatalf("urls=%v err=%v", urls, err)
	}
}

func TestMediaURLsRejectsUntrustedAndPresignedURLs(t *testing.T) {
	t.Setenv("R2_PUBLIC_BASE_URL", "https://media.example.test")
	for _, value := range []string{
		"https://example.com/a.jpg",
		"https://media.example.test/a.jpg?X-Amz-Signature=secret",
	} {
		if _, err := MediaURLs([]string{value}, 5); err == nil {
			t.Fatalf("untrusted URL accepted: %s", value)
		}
	}
}
