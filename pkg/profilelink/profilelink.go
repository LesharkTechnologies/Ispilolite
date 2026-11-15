// Package profilelink builds public profile URLs from persisted, domain-free
// paths. Changing the origin never changes stored user records or old slugs.
package profilelink

import (
	"errors"
	"net/url"
	"os"
	"strings"
)

type Builder struct{ base string }

func FromEnv() (*Builder, error) {
	production := strings.EqualFold(os.Getenv("ENVIRONMENT"), "production")
	base := os.Getenv("PROFILE_PUBLIC_BASE_URL")
	if strings.TrimSpace(base) == "" {
		if production {
			base = "https://lite.ispilo.co.ke"
		} else {
			base = "http://localhost:8001"
		}
	}
	return New(base, production)
}

// New accepts an origin, not an API prefix or a path. Production links must
// use HTTPS. An explicit origin avoids Host-header-based link spoofing.
func New(base string, production bool) (*Builder, error) {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u == nil || u.Host == "" || u.User != nil || u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || (u.Scheme != "https" && u.Scheme != "http") || (production && u.Scheme != "https") {
		return nil, errors.New("PROFILE_PUBLIC_BASE_URL must be an HTTP(S) origin (HTTPS in production), without a path or query")
	}
	return &Builder{base: strings.TrimRight(u.String(), "/")}, nil
}

func (b *Builder) URL(profilePath string) string {
	if b == nil || !strings.HasPrefix(profilePath, "/u/") || strings.ContainsAny(profilePath, "?#") {
		return ""
	}
	return b.base + profilePath
}

// Origin returns the configured public origin without a trailing slash. It is
// used for non-profile share links such as public coverage pages, which must
// also be stable even when the API is served from a different host.
func (b *Builder) Origin() string {
	if b == nil {
		return ""
	}
	return b.base
}
