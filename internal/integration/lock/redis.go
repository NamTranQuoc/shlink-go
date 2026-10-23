package lock

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// redisInitialTTL is RedisStore's default initial TTL.
const redisInitialTTL = 300 * time.Second

// redisStore ports RedisStore behind NamespacedStore: resources are prefixed with "<namespace>:", and the lock is a
// sorted set of token expirations plus the "__write__" member, managed by RedisStore's Lua scripts. Goroutines of this
// process queue on processLocks first.
type redisStore struct {
	client    redis.UniversalClient
	namespace *string
	process   processLocks

	nowOnce sync.Once
	nowCode string
	nowErr  error

	scriptsMu sync.Mutex
	scripts   map[string]*redis.Script
}

const redisSaveScript = `
            local key = KEYS[1]
            local uniqueToken = ARGV[2]
            local ttl = tonumber(ARGV[3])

            -- asserts the KEY is compatible with current version (old Symfony <5.2 BC)
            if redis.call("TYPE", key).ok == "string" then
                return false
            end

            %NOW%

            -- Remove expired values
            redis.call("ZREMRANGEBYSCORE", key, "-inf", now)

            -- is already acquired
            if redis.call("ZSCORE", key, uniqueToken) then
                -- is not WRITE lock and cannot be promoted
                if not redis.call("ZSCORE", key, "__write__") and redis.call("ZCOUNT", key, "-inf", "+inf") > 1  then
                    return false
                end
            elseif redis.call("ZCOUNT", key, "-inf", "+inf") > 0  then
                return false
            end

            redis.call("ZADD", key, now + ttl, uniqueToken)
            redis.call("ZADD", key, now + ttl, "__write__")

            -- Extend the TTL of the key
            local maxExpiration = redis.call("ZREVRANGE", key, 0, 0, "WITHSCORES")[2]
            redis.call("PEXPIREAT", key, maxExpiration)

            return true
        `

const redisPutOffExpirationScript = `
            local key = KEYS[1]
            local uniqueToken = ARGV[2]
            local ttl = tonumber(ARGV[3])

            -- asserts the KEY is compatible with current version (old Symfony <5.2 BC)
            if redis.call("TYPE", key).ok == "string" then
                return false
            end

            %NOW%

            -- lock already acquired acquired?
            if not redis.call("ZSCORE", key, uniqueToken) then
                return false
            end

            redis.call("ZADD", key, now + ttl, uniqueToken)
            -- if the lock is also a WRITE lock, increase the TTL
            if redis.call("ZSCORE", key, "__write__") then
                redis.call("ZADD", key, now + ttl, "__write__")
            end

            -- Extend the TTL of the key
            local maxExpiration = redis.call("ZREVRANGE", key, 0, 0, "WITHSCORES")[2]
            redis.call("PEXPIREAT", key, maxExpiration)

            return true
        `

const redisDeleteScript = `
            local key = KEYS[1]
            local uniqueToken = ARGV[1]

            -- asserts the KEY is compatible with current version (old Symfony <5.2 BC)
            if redis.call("TYPE", key).ok == "string" then
                return false
            end

            -- lock not already acquired
            if not redis.call("ZSCORE", key, uniqueToken) then
                return false
            end

            redis.call("ZREM", key, uniqueToken)
            redis.call("ZREM", key, "__write__")

            local maxExpiration = redis.call("ZREVRANGE", key, 0, 0, "WITHSCORES")[2]
            if nil ~= maxExpiration then
                redis.call("PEXPIREAT", key, maxExpiration)
            end

            return true
        `

const redisSupportTimeScript = `
                local now = redis.call("TIME")
                redis.call("SET", KEYS[1], "1", "PX", 1)

	            return 1
            `

const redisNowFromTime = `
                local now = redis.call("TIME")
                now = now[1] * 1000 + math.floor(now[2] / 1000)
            `

const redisNowFromArg = `
                local now = tonumber(ARGV[1])
                now = math.floor(now * 1000)
            `

// resource ports NamespacedStore::namespaceKey().
func (s *redisStore) resource(k *key) string {
	if s.namespace == nil {
		return k.resource
	}
	prefix := *s.namespace + ":"
	if strings.HasPrefix(k.resource, prefix) {
		return k.resource
	}
	return prefix + k.resource
}

// uniqueToken ports RedisStore::getUniqueToken().
func uniqueToken(k *key) string {
	if k.token == "" || k.token == "__write__" {
		var b [32]byte
		_, _ = rand.Read(b[:])
		k.token = base64.StdEncoding.EncodeToString(b[:])
	}
	return k.token
}

// now ports getNowCode(): Redis TIME inside scripts when the server allows it (Redis 5+), the client clock otherwise.
func (s *redisStore) now(ctx context.Context) (string, error) {
	s.nowOnce.Do(func() {
		res, err := redis.NewScript(redisSupportTimeScript).Run(ctx, s.client, []string{"symfony_check_support_time"}).Int()
		switch {
		case err == nil:
			s.nowCode = redisNowFromArg
			if res == 1 {
				s.nowCode = redisNowFromTime
			}
		case strings.Contains(err.Error(), "commands not allowed after non deterministic") ||
			strings.Contains(err.Error(), "is not allowed from script script"):
			s.nowCode = redisNowFromArg
		default:
			s.nowErr = err
		}
	})
	return s.nowCode, s.nowErr
}

func (s *redisStore) script(ctx context.Context, src string) (*redis.Script, error) {
	now, err := s.now(ctx)
	if err != nil {
		return nil, err
	}
	s.scriptsMu.Lock()
	defer s.scriptsMu.Unlock()
	if s.scripts == nil {
		s.scripts = map[string]*redis.Script{}
	}
	sc, ok := s.scripts[src]
	if !ok {
		sc = redis.NewScript(strings.Replace(src, "%NOW%", now, 1))
		s.scripts[src] = sc
	}
	return sc, nil
}

// evaluate runs a script and reports whether it returned true.
func (s *redisStore) evaluate(ctx context.Context, src string, k *key, args ...any) (bool, error) {
	sc, err := s.script(ctx, src)
	if err != nil {
		return false, err
	}
	res, err := sc.Run(ctx, s.client, []string{s.resource(k)}, args...).Int()
	if errors.Is(err, redis.Nil) {
		return false, nil
	}
	return err == nil && res == 1, err
}

func microtime() float64 { return float64(time.Now().UnixMicro()) / 1e6 }

func millis(d time.Duration) int64 { return int64(math.Ceil(d.Seconds() * 1000)) }

func (s *redisStore) save(ctx context.Context, k *key) error {
	if k.release == nil {
		releaseProcess, err := s.process.lock(ctx, k.resource, false)
		if err != nil {
			return err
		}
		if releaseProcess == nil {
			return errConflicted
		}
		k.release = releaseProcess
	}
	ok, err := s.evaluate(ctx, redisSaveScript, k, microtime(), uniqueToken(k), millis(redisInitialTTL))
	if err != nil || !ok {
		k.release()
		k.release = nil
		if err != nil {
			return err
		}
		return errConflicted
	}
	return nil
}

func (s *redisStore) waitAndSave(context.Context, *key) (bool, error) { return false, nil }

func (s *redisStore) putOffExpiration(ctx context.Context, k *key, ttl time.Duration) error {
	ok, err := s.evaluate(ctx, redisPutOffExpirationScript, k, microtime(), uniqueToken(k), millis(ttl))
	if err == nil && !ok {
		return errConflicted
	}
	return err
}

func (s *redisStore) delete(ctx context.Context, k *key) error {
	_, err := s.evaluate(ctx, redisDeleteScript, k, uniqueToken(k))
	if k.release != nil {
		k.release()
		k.release = nil
	}
	return err
}
