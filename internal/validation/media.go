package validation

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// CloudinaryURLs validates delivery URLs uploaded by the client directly to
// Cloudinary. The API never accepts image bytes and never fetches these URLs.
func CloudinaryURLs(values []string, max int) ([]string, error) {
	if max <= 0 {
		max = 5
	}
	if len(values) > max {
		return nil, fmt.Errorf("at most %d media URLs are allowed", max)
	}
	allowed := []string{"res.cloudinary.com"}
	for _, host := range strings.Split(os.Getenv("CLOUDINARY_ALLOWED_HOSTS"), ",") {
		if host = strings.ToLower(strings.TrimSpace(host)); host != "" {
			allowed = append(allowed, host)
		}
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]bool)
	for _, raw := range values {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if len(raw) > 2048 {
			return nil, fmt.Errorf("media URL is too long")
		}
		u, err := url.ParseRequestURI(raw)
		if err != nil || !strings.EqualFold(u.Scheme, "https") || u.User != nil || u.Hostname() == "" {
			return nil, fmt.Errorf("media URLs must be valid HTTPS Cloudinary URLs")
		}
		host := strings.ToLower(u.Hostname())
		validHost := false
		for _, candidate := range allowed {
			if host == candidate || strings.HasSuffix(host, "."+candidate) {
				validHost = true
				break
			}
		}
		if !validHost {
			return nil, fmt.Errorf("media URL host %q is not an allowed Cloudinary host", host)
		}
		if !seen[raw] {
			seen[raw] = true
			out = append(out, raw)
		}
	}
	return out, nil
}
