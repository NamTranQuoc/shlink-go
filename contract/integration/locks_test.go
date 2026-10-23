package integration

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"

	h "github.com/NamTranQuoc/shlink-go/contract/internal/harness"
)

// With REDIS_SERVERS set, upstream's LockFactory uses symfony/lock's RedisStore behind shlink-common's
// NamespacedStore (prefix "<CACHE_NAMESPACE>:", "Shlink" by default). These tests hold a lock the way another Shlink
// instance would, as RedisStore stores it: a sorted set with a token and the "__write__" member scored with their
// expiration in milliseconds.
//
// When the locks of new tags and domains are released is not checked: upstream releases them in the relation
// resolver's postFlush listener, which only runs during the first request a RoadRunner worker serves (the entity
// manager it is registered on is closed and replaced after each request); later locks expire after their 5s TTL.

const heldLock = 2 * time.Second

func holdRedisLock(t *testing.T, resource string) {
	t.Helper()
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	t.Cleanup(func() { _ = client.Close() })
	now, err := client.Time(ctx).Result()
	if err != nil {
		t.Fatalf("redis time: %v", err)
	}
	expiration := float64(now.Add(heldLock).UnixMilli())
	key := "Shlink:" + resource
	_, err = client.TxPipelined(ctx, func(p redis.Pipeliner) error {
		p.Del(ctx, key)
		p.ZAdd(ctx, key, redis.Z{Score: expiration, Member: "contract-test-token"}, redis.Z{Score: expiration, Member: "__write__"})
		p.PExpireAt(ctx, key, time.UnixMilli(int64(expiration)))
		return nil
	})
	if err != nil {
		t.Fatalf("hold lock %s: %v", key, err)
	}
	t.Cleanup(func() { client.Del(ctx, key) })
}

func redisKeyExists(t *testing.T, resource string) bool {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: redisAddr})
	defer client.Close()
	n, err := client.Exists(context.Background(), "Shlink:"+resource).Result()
	if err != nil {
		t.Fatalf("redis exists: %v", err)
	}
	return n > 0
}

// assertWaitedForLock checks that an operation started while the lock was held only finished after it expired.
func assertWaitedForLock(t *testing.T, started time.Time) {
	t.Helper()
	if elapsed := time.Since(started); elapsed < heldLock-500*time.Millisecond || elapsed > heldLock+8*time.Second {
		t.Fatalf("operation took %s; it should wait for the %s lock to expire", elapsed, heldLock)
	}
}

func TestLocateVisitsIsSkippedWhileLockedInRedis(t *testing.T) {
	requireProfile(t)
	h.Seed(t)
	holdRedisLock(t, "visit:locate")

	out, code := h.Exec(t, []string{"visit:locate"})

	h.AssertEquals(t, 2, code)
	h.AssertEquals(t, "Command \"visit:locate\" is already in progress. Skipping.\n", out)
}

func TestCreateDatabaseWaitsForRedisLock(t *testing.T) {
	requireProfile(t)
	h.Seed(t)
	holdRedisLock(t, "db:create")

	started := time.Now()
	out, code := h.Exec(t, []string{"db:create"})

	assertWaitedForLock(t, started)
	h.AssertEquals(t, 0, code)
	h.AssertEquals(t, true, strings.Contains(out, "Database already exists."), out)
	h.AssertEquals(t, false, redisKeyExists(t, "db:create"))
}

func TestNewTagWaitsForRedisLock(t *testing.T) {
	requireProfile(t)
	h.Seed(t)
	holdRedisLock(t, "tag_locked-tag")

	started := time.Now()
	resp := h.CallAPIWithKey(t, http.MethodPost, "/rest/v3/short-urls", h.Options{JSON: map[string]any{
		"longUrl": "https://shlink.io/locked-tag",
		"tags":    []string{"locked-tag"},
	}})

	assertWaitedForLock(t, started)
	h.AssertStatus(t, resp, http.StatusOK)
}

func TestNewDomainWaitsForRedisLock(t *testing.T) {
	requireProfile(t)
	h.Seed(t)
	holdRedisLock(t, "domain_locked.example.com")

	started := time.Now()
	resp := h.CallAPIWithKey(t, http.MethodPost, "/rest/v3/short-urls", h.Options{JSON: map[string]any{
		"longUrl": "https://shlink.io/locked-domain",
		"domain":  "locked.example.com",
	}})

	assertWaitedForLock(t, started)
	h.AssertStatus(t, resp, http.StatusOK)
}
