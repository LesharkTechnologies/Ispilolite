package middleware

import (
	"context"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	goredis "github.com/go-redis/redis/v8"
)

// RateLimiter is a Redis-backed fixed-window rate limiter keyed by IP + route,
// with an in-memory sliding bucket fallback when Redis is absent.
type RateLimiter struct {
	client  *goredis.Client
	limit   int64
	window  time.Duration
	prefix  string
	mu      sync.Mutex
	buckets map[string]*memBucket
}

type memBucket struct {
	count     int64
	expiresAt time.Time
}

func NewRateLimiter(client *goredis.Client, limit int, window time.Duration) *RateLimiter {
	if limit <= 0 {
		limit = 60
	}
	if window <= 0 {
		window = time.Minute
	}
	return &RateLimiter{
		client:  client,
		limit:   int64(limit),
		window:  window,
		prefix:  "ratelimit:",
		buckets: make(map[string]*memBucket),
	}
}

var rateScript = goredis.NewScript(`local current=redis.call('INCR',KEYS[1]);if current==1 then redis.call('PEXPIRE',KEYS[1],ARGV[1]) end;local ttl=redis.call('PTTL',KEYS[1]);return {current,ttl}`)

func (r *RateLimiter) Allow(ctx context.Context, key string) (bool, time.Duration, error) {
	if r.client == nil {
		r.mu.Lock()
		defer r.mu.Unlock()
		now := time.Now()
		b, exists := r.buckets[key]
		if !exists || now.After(b.expiresAt) {
			r.buckets[key] = &memBucket{
				count:     1,
				expiresAt: now.Add(r.window),
			}
			return true, 0, nil
		}
		b.count++
		if b.count > r.limit {
			return false, b.expiresAt.Sub(now), nil
		}
		return true, 0, nil
	}
	result, err := rateScript.Run(ctx, r.client, []string{r.prefix + key}, r.window.Milliseconds()).Int64Slice()
	if err != nil {
		return false, 0, err
	}
	return result[0] <= r.limit, time.Duration(result[1]) * time.Millisecond, nil
}

// Middleware applies the rate limiter to the next handler.
func (r *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		key := clientIP(req) + ":" + routeBucket(req.URL.Path)
		allowed, retry, err := r.Allow(req.Context(), key)
		if err != nil {
			http.Error(w, "rate limiter unavailable", http.StatusServiceUnavailable)
			return
		}
		if !allowed {
			seconds := int(retry.Seconds())
			if seconds < 1 {
				seconds = 1
			}
			w.Header().Set("Retry-After", strconv.Itoa(seconds))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"success":false,"message":"too many login attempts. Please wait 5 minutes before trying again.","retry_after":` + strconv.Itoa(seconds) + `}`))
			return
		}
		next.ServeHTTP(w, req)
	})
}

// proxyNets is the set of reverse proxies whose X-Forwarded-For header is
// trusted. A request that does not arrive from one of these peers cannot
// spoof its client IP.
type proxyNets struct {
	nets []*net.IPNet
}

func parseProxyNets(spec string) *proxyNets {
	p := &proxyNets{}
	for _, raw := range strings.Split(spec, ",") {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		// Accept bare IPs as well as CIDR blocks.
		if !strings.Contains(entry, "/") {
			ip := net.ParseIP(entry)
			if ip == nil {
				continue
			}
			bits := 32
			if ip.To4() == nil {
				bits = 128
			}
			entry = ip.String() + "/" + strconv.Itoa(bits)
		}
		if _, network, err := net.ParseCIDR(entry); err == nil {
			p.nets = append(p.nets, network)
		}
	}
	return p
}

func (p *proxyNets) contains(ip net.IP) bool {
	if p == nil || ip == nil {
		return false
	}
	for _, network := range p.nets {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

// trustedProxies defaults to loopback only, which matches the documented
// single-VPS deployment where Nginx runs on the same host. Set TRUSTED_PROXIES
// to the proxy CIDRs (comma-separated) when Nginx lives elsewhere.
var trustedProxies = sync.OnceValue(func() *proxyNets {
	spec := strings.TrimSpace(os.Getenv("TRUSTED_PROXIES"))
	if spec == "" {
		spec = "127.0.0.1/32,::1/128"
	}
	return parseProxyNets(spec)
})

func clientIP(r *http.Request) string {
	return clientIPWithProxies(r, trustedProxies())
}

// ClientIP resolves the caller address using the same trusted-proxy rules as
// the rate limiter. Callers may hash the result for privacy-safe analytics, but
// must never persist it in raw form.
func ClientIP(r *http.Request) string {
	return clientIP(r)
}

// clientIPWithProxies resolves the client address, only reading
// X-Forwarded-For when the immediate peer is a trusted proxy. It walks the
// header from right to left and returns the first address that is not itself a
// trusted proxy, which defeats client-supplied header spoofing.
func clientIPWithProxies(r *http.Request, proxies *proxyNets) string {
	remote := r.RemoteAddr
	if host, _, err := net.SplitHostPort(remote); err == nil {
		remote = host
	}

	if !proxies.contains(net.ParseIP(remote)) {
		return remote
	}

	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		candidate := strings.TrimSpace(parts[i])
		ip := net.ParseIP(candidate)
		if ip == nil {
			continue
		}
		if !proxies.contains(ip) {
			return ip.String()
		}
	}
	return remote
}

func routeBucket(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) > 3 {
		return strings.Join(parts[:3], "/")
	}
	return strings.Join(parts, "/")
}
