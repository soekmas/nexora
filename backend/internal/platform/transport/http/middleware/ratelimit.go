package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"

	"example.com/nexora/backend/internal/platform/apperr"
)

// Limiter defines the rate limiter contract implemented by both in-memory and distributed limiters.
type Limiter interface {
	Allow(key string) (allowed bool, retryAfter time.Duration, remaining int)
	Limit() int
	Window() time.Duration
	ExtractClientIP(r *http.Request) string
	SetTrustedProxies(cidrs []string) error
	Stop()
}

type clientBucket struct {
	count       int
	windowStart time.Time
}

// MemoryRateLimiter implements an in-memory, thread-safe sliding window rate limiter per client IP.
type MemoryRateLimiter struct {
	mu             sync.Mutex
	limit          int
	window         time.Duration
	clients        map[string]*clientBucket
	stopCh         chan struct{}
	trustedProxies []*net.IPNet
}

// NewMemoryRateLimiter constructs a new MemoryRateLimiter.
func NewMemoryRateLimiter(limit int, window time.Duration) *MemoryRateLimiter {
	rl := &MemoryRateLimiter{
		limit:   limit,
		window:  window,
		clients: make(map[string]*clientBucket),
		stopCh:  make(chan struct{}),
	}

	if limit > 0 && window > 0 {
		go rl.cleanupLoop()
	}

	return rl
}

// Limit returns the configured request limit.
func (rl *MemoryRateLimiter) Limit() int {
	return rl.limit
}

// Window returns the configured rate limit window duration.
func (rl *MemoryRateLimiter) Window() time.Duration {
	return rl.window
}

// SetTrustedProxies configures trusted proxy CIDRs.
func (rl *MemoryRateLimiter) SetTrustedProxies(cidrs []string) error {
	nets, err := ParseCIDRs(cidrs)
	if err != nil {
		return err
	}
	rl.mu.Lock()
	rl.trustedProxies = nets
	rl.mu.Unlock()
	return nil
}

func (rl *MemoryRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(rl.window * 2)
	defer ticker.Stop()

	for {
		select {
		case <-rl.stopCh:
			return
		case <-ticker.C:
			rl.mu.Lock()
			now := time.Now()
			for ip, bucket := range rl.clients {
				if now.Sub(bucket.windowStart) > rl.window*2 {
					delete(rl.clients, ip)
				}
			}
			rl.mu.Unlock()
		}
	}
}

// Stop cleanly terminates the background bucket cleanup loop.
func (rl *MemoryRateLimiter) Stop() {
	select {
	case <-rl.stopCh:
		// already stopped
	default:
		close(rl.stopCh)
	}
}

// Allow checks if the given key is within the rate limit.
func (rl *MemoryRateLimiter) Allow(key string) (bool, time.Duration, int) {
	if rl.limit <= 0 || rl.window <= 0 {
		return true, 0, rl.limit
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	bucket, exists := rl.clients[key]

	if !exists || now.Sub(bucket.windowStart) >= rl.window {
		rl.clients[key] = &clientBucket{
			count:       1,
			windowStart: now,
		}
		return true, 0, rl.limit - 1
	}

	if bucket.count < rl.limit {
		bucket.count++
		return true, 0, rl.limit - bucket.count
	}

	resetTime := bucket.windowStart.Add(rl.window)
	retryAfter := resetTime.Sub(now)
	if retryAfter < time.Second {
		retryAfter = time.Second
	}

	return false, retryAfter, 0
}

// ExtractClientIP resolves client IP using configured trusted proxy hops.
func (rl *MemoryRateLimiter) ExtractClientIP(r *http.Request) string {
	rl.mu.Lock()
	proxies := rl.trustedProxies
	rl.mu.Unlock()
	return ResolveClientIP(r, proxies)
}

// RedisRateLimiter provides distributed rate limiting backed by Redis.
type RedisRateLimiter struct {
	client         *redis.Client
	limit          int
	window         time.Duration
	keyPrefix      string
	failClosed     bool
	trustedProxies []*net.IPNet
	fallback       *MemoryRateLimiter
	logger         *slog.Logger
	mu             sync.RWMutex
}

// NewRedisRateLimiter constructs a new distributed RedisRateLimiter.
func NewRedisRateLimiter(client *redis.Client, limit int, window time.Duration) *RedisRateLimiter {
	return &RedisRateLimiter{
		client:    client,
		limit:     limit,
		window:    window,
		keyPrefix: "nexora:ratelimit:ip:",
		fallback:  NewMemoryRateLimiter(limit, window),
		logger:    slog.Default(),
	}
}

// SetFailClosed configures whether Redis outages cause requests to be rejected (fail-closed)
// or allowed (fail-open). Default is false (fail-open / local fallback).
func (rl *RedisRateLimiter) SetFailClosed(failClosed bool) {
	rl.mu.Lock()
	rl.failClosed = failClosed
	rl.mu.Unlock()
}

// SetFallback sets an in-memory fallback rate limiter used when Redis is unreachable.
func (rl *RedisRateLimiter) SetFallback(fallback *MemoryRateLimiter) {
	rl.mu.Lock()
	rl.fallback = fallback
	rl.mu.Unlock()
}

// Limit returns the configured request limit.
func (rl *RedisRateLimiter) Limit() int {
	return rl.limit
}

// Window returns the configured rate limit window duration.
func (rl *RedisRateLimiter) Window() time.Duration {
	return rl.window
}

// SetTrustedProxies configures trusted proxy CIDRs.
func (rl *RedisRateLimiter) SetTrustedProxies(cidrs []string) error {
	nets, err := ParseCIDRs(cidrs)
	if err != nil {
		return err
	}
	rl.mu.Lock()
	rl.trustedProxies = nets
	if rl.fallback != nil {
		_ = rl.fallback.SetTrustedProxies(cidrs)
	}
	rl.mu.Unlock()
	return nil
}

// Stop cleanly stops the background fallback limiter if present.
func (rl *RedisRateLimiter) Stop() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	if rl.fallback != nil {
		rl.fallback.Stop()
	}
}

// ExtractClientIP resolves client IP using configured trusted proxy hops.
func (rl *RedisRateLimiter) ExtractClientIP(r *http.Request) string {
	rl.mu.RLock()
	proxies := rl.trustedProxies
	rl.mu.RUnlock()
	return ResolveClientIP(r, proxies)
}

// redisRateLimitScript atomically increments the counter and sets TTL on first hit.
// Returns {allowed (1 or 0), retry_after_seconds, remaining_count}.
const redisRateLimitScript = `
local key = KEYS[1]
local limit = tonumber(ARGV[1])
local window = tonumber(ARGV[2])

local current = redis.call('INCR', key)
if current == 1 then
    redis.call('EXPIRE', key, window)
end

local ttl = redis.call('TTL', key)
if ttl < 0 then
    ttl = window
    redis.call('EXPIRE', key, window)
end

if current > limit then
    return {0, ttl, 0}
else
    local remaining = limit - current
    if remaining < 0 then remaining = 0 end
    return {1, 0, remaining}
end
`

// Allow checks the rate limit in Redis atomically using Lua script.
func (rl *RedisRateLimiter) Allow(key string) (bool, time.Duration, int) {
	if rl.limit <= 0 || rl.window <= 0 {
		return true, 0, rl.limit
	}

	if rl.client == nil {
		return rl.handleRedisFailure(key, fmt.Errorf("redis client is nil"))
	}

	redisKey := rl.keyPrefix + key
	windowSeconds := int(rl.window.Seconds())
	if windowSeconds <= 0 {
		windowSeconds = 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	res, err := rl.client.Eval(ctx, redisRateLimitScript, []string{redisKey}, rl.limit, windowSeconds).Result()
	if err != nil {
		return rl.handleRedisFailure(key, err)
	}

	slice, ok := res.([]any)
	if !ok || len(slice) < 3 {
		return rl.handleRedisFailure(key, fmt.Errorf("unexpected redis script result format: %v", res))
	}

	allowedInt, _ := slice[0].(int64)
	ttlInt, _ := slice[1].(int64)
	remainingInt, _ := slice[2].(int64)

	allowed := allowedInt == 1
	var retryAfter time.Duration
	if !allowed {
		if ttlInt <= 0 {
			ttlInt = 1
		}
		retryAfter = time.Duration(ttlInt) * time.Second
	}

	return allowed, retryAfter, int(remainingInt)
}

func (rl *RedisRateLimiter) handleRedisFailure(key string, err error) (bool, time.Duration, int) {
	rl.mu.RLock()
	failClosed := rl.failClosed
	fallback := rl.fallback
	logger := rl.logger
	rl.mu.RUnlock()

	if logger != nil {
		logger.Warn("Redis rate limiter error, applying failure policy",
			slog.String("key", key),
			slog.String("error", err.Error()),
			slog.Bool("fail_closed", failClosed),
		)
	}

	// 1. If local in-memory fallback is configured, use it for defense-in-depth
	if fallback != nil {
		return fallback.Allow(key)
	}

	// 2. Otherwise apply explicit fail-closed / fail-open policy
	if failClosed {
		return false, rl.window, 0
	}

	// Fail-open: allow request
	return true, 0, rl.limit
}

// RateLimit returns an HTTP middleware enforcing rate limits per client IP address.
func RateLimit(limiter Limiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if limiter == nil || limiter.Limit() <= 0 {
				next.ServeHTTP(w, r)
				return
			}

			ip := limiter.ExtractClientIP(r)
			allowed, retryAfter, remaining := limiter.Allow(ip)

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limiter.Limit()))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))

			if !allowed {
				retryAfterSeconds := int(retryAfter.Seconds())
				if retryAfterSeconds <= 0 {
					retryAfterSeconds = 1
				}

				w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds))

				reqID := GetRequestID(r.Context())
				apperr.RespondError(w, reqID, apperr.NewRateLimitError(
					fmt.Sprintf("Rate limit exceeded (%d requests per %v). Please retry after %d seconds.", limiter.Limit(), limiter.Window(), retryAfterSeconds),
					retryAfterSeconds,
				))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ParseCIDRs parses and validates a slice of CIDR string representations.
func ParseCIDRs(cidrs []string) ([]*net.IPNet, error) {
	var nets []*net.IPNet
	for _, cidr := range cidrs {
		trimmed := strings.TrimSpace(cidr)
		if trimmed == "" {
			continue
		}
		_, ipNet, err := net.ParseCIDR(trimmed)
		if err != nil {
			return nil, fmt.Errorf("invalid trusted proxy CIDR %q: %w", cidr, err)
		}
		nets = append(nets, ipNet)
	}
	return nets, nil
}

// ResolveClientIP extracts the authentic client IP from the request.
// If trusted proxies are configured, forwarded headers are only trusted
// when the direct connection originates from a trusted proxy CIDR.
// Traversal is evaluated right-to-left: the first untrusted IP in X-Forwarded-For is returned.
func ResolveClientIP(r *http.Request, trustedProxies []*net.IPNet) string {
	remoteHost := extractRemoteHost(r.RemoteAddr)

	if len(trustedProxies) == 0 || !isTrustedProxy(remoteHost, trustedProxies) {
		return remoteHost
	}

	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		// Traverse right-to-left
		for i := len(parts) - 1; i >= 0; i-- {
			hop := strings.TrimSpace(parts[i])
			if hop == "" {
				continue
			}
			parsed := net.ParseIP(hop)
			if parsed == nil {
				continue
			}
			if !isTrustedProxy(hop, trustedProxies) {
				return hop
			}
		}
		// If all hops were trusted proxies, return leftmost valid IP
		for _, part := range parts {
			hop := strings.TrimSpace(part)
			if net.ParseIP(hop) != nil {
				return hop
			}
		}
	}

	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" {
		if net.ParseIP(xri) != nil {
			return xri
		}
	}

	return remoteHost
}

func extractRemoteHost(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	return remoteAddr
}

func isTrustedProxy(ip string, trusted []*net.IPNet) bool {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return false
	}
	for _, cidr := range trusted {
		if cidr.Contains(parsed) {
			return true
		}
	}
	return false
}
