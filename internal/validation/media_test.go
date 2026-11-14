package validation

import "testing"

func TestCloudinaryURLs(t *testing.T) {
	urls, err := CloudinaryURLs([]string{"https://res.cloudinary.com/demo/image/upload/a.jpg", "https://res.cloudinary.com/demo/image/upload/a.jpg"}, 5)
	if err != nil {
		t.Fatalf("valid Cloudinary URL rejected: %v", err)
	}
	if len(urls) != 1 {
		t.Fatalf("expected de-duplicated URL, got %d", len(urls))
	}
}

func TestCloudinaryURLsRejectsUntrustedHost(t *testing.T) {
	if _, err := CloudinaryURLs([]string{"https://example.com/a.jpg"}, 5); err == nil {
		t.Fatal("untrusted media host was accepted")
	}
}
