package validation

import (
	"fmt"
	"net/url"
	"os"
	"strings"
)

// MediaURLs validates Cloudflare R2 delivery URLs returned by the media API.
// Callers store only delivery URLs; R2 access keys and presigned upload URLs
// must never be persisted in business records.
func MediaURLs(values []string, max int) ([]string, error) {
	if max <= 0 {
		max = 5
	}
	if len(values) > max {
		return nil, fmt.Errorf("at most %d media URLs are allowed", max)
	}
	allowed := []string{"media.ispilo.co.ke"}
	if publicBase := strings.TrimSpace(os.Getenv("R2_PUBLIC_BASE_URL")); publicBase != "" {
		if parsed, err := url.Parse(publicBase); err == nil && parsed.Hostname() != "" {
			allowed = append(allowed, strings.ToLower(parsed.Hostname()))
		}
	}
	for _, host := range strings.Split(os.Getenv("R2_ALLOWED_HOSTS"), ",") {
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
		if err != nil || !strings.EqualFold(u.Scheme, "https") || u.User != nil || u.Hostname() == "" || u.RawQuery != "" || u.Fragment != "" {
			return nil, fmt.Errorf("media URLs must be R2 HTTPS delivery URLs")
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
			return nil, fmt.Errorf("media URL host %q is not an allowed R2 host", host)
		}
		if !seen[raw] {
			seen[raw] = true
			out = append(out, raw)
		}
	}
	return out, nil
}
