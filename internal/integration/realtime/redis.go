package realtime

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"github.com/redis/go-redis/v9"

	"github.com/NamTranQuoc/shlink-go/internal/config"
)

// RedisOptions ports the `cache.redis` and `redis` config blocks (config/autoload/cache.global.php).
type RedisOptions struct {
	// Servers is the raw REDIS_SERVERS value; nil when redis is not configured.
	Servers         *string
	SentinelService *string
	Username        *string
	Password        *string
	PubSubEnabled   bool
}

// RedisOptionsFromEnv ports config/autoload/cache.global.php.
func RedisOptionsFromEnv() RedisOptions {
	servers := config.RedisServers.Load().StringOrNil()
	return RedisOptions{
		Servers:         servers,
		SentinelService: config.RedisSentinelService.Load().StringOrNil(),
		Username:        config.RedisServersUser.Load().StringOrNil(),
		Password:        config.RedisServersPassword.Load().StringOrNil(),
		PubSubEnabled:   servers != nil && config.RedisPubSubEnabled.Load().AsBool(),
	}
}

// redisServer is one entry of REDIS_SERVERS, normalized like RedisFactory::normalizeServer().
type redisServer struct {
	scheme   string
	host     string
	port     int
	path     string
	username *string
	password *string
	database *int
	tls      bool
}

func (s redisServer) addr() string {
	port := s.port
	if port == 0 {
		port = 6379
	}
	return net.JoinHostPort(s.host, strconv.Itoa(port))
}

// parseRedisServer ports RedisFactory::normalizeServer().
func parseRedisServer(raw string) (redisServer, error) {
	trimmed := strings.TrimSpace(raw)
	toParse := trimmed
	if !strings.Contains(toParse, "://") {
		// parse_url() reads "host:port" as a host and a port
		toParse = "tcp://" + toParse
	}
	u, err := url.Parse(toParse)
	if err != nil || (u.Host == "" && u.Scheme != "unix") {
		return redisServer{}, fmt.Errorf(`Provided server "%s" is not a valid URL with format %s`, raw,
			"schema://[[username]:password@]host:port[/database]")
	}
	s := redisServer{scheme: u.Scheme, host: u.Hostname(), path: u.Path}
	if !strings.Contains(trimmed, "://") {
		s.scheme = ""
	}
	s.port, _ = strconv.Atoi(u.Port())
	s.tls = s.scheme == "tls" || s.scheme == "rediss"
	if u.User != nil {
		if name := u.User.Username(); name != "" {
			s.username = &name
		}
		if pass, ok := u.User.Password(); ok {
			s.password = &pass
		}
	}
	if v := u.Query().Get("database"); u.Query().Has("database") {
		db, err := strconv.Atoi(v)
		if err != nil {
			return redisServer{}, fmt.Errorf("The redis database index should be an integer, %s provided", v)
		}
		s.database = &db
	}
	return s, nil
}

// NewRedisClient ports RedisFactory: one server is a regular connection, several servers a Redis cluster, and a
// sentinel service turns the servers into sentinels.
func NewRedisClient(o RedisOptions) (redis.UniversalClient, error) {
	if o.Servers == nil {
		return nil, fmt.Errorf("redis is not configured")
	}
	var servers []redisServer
	for _, raw := range strings.Split(*o.Servers, ",") {
		s, err := parseRedisServer(raw)
		if err != nil {
			return nil, err
		}
		servers = append(servers, s)
	}
	first := servers[0]
	tlsConfig := func(s redisServer) *tls.Config {
		if !s.tls {
			return nil
		}
		// shlink-common's SSL::OPTIONS allow self-signed certificates and skip peer verification
		return &tls.Config{InsecureSkipVerify: true}
	}
	deref := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	addrs := make([]string, len(servers))
	for i, s := range servers {
		addrs[i] = s.addr()
	}
	if o.SentinelService != nil {
		opts := &redis.FailoverOptions{
			MasterName:       *o.SentinelService,
			SentinelAddrs:    addrs,
			SentinelUsername: deref(first.username),
			SentinelPassword: deref(first.password),
			TLSConfig:        tlsConfig(first),
		}
		if o.Password != nil {
			opts.Username = deref(o.Username)
			opts.Password = *o.Password
		}
		if first.database != nil {
			opts.DB = *first.database
		}
		return redis.NewFailoverClient(opts), nil
	}
	if len(servers) > 1 {
		return redis.NewClusterClient(&redis.ClusterOptions{
			Addrs: addrs, Username: deref(first.username), Password: deref(first.password), TLSConfig: tlsConfig(first),
		}), nil
	}
	opts := &redis.Options{
		Addr: first.addr(), Username: deref(first.username), Password: deref(first.password),
		TLSConfig: tlsConfig(first),
	}
	if first.scheme == "unix" {
		opts.Network, opts.Addr = "unix", first.path
	}
	if first.database != nil {
		opts.DB = *first.database
	}
	return redis.NewClient(opts), nil
}

// Redis ports RedisPublishingHelper.
type Redis struct {
	Client redis.UniversalClient
}

// PublishUpdate publishes the encoded payload on a channel named after the topic.
func (r *Redis) PublishUpdate(ctx context.Context, u Update) error {
	return r.Client.Publish(ctx, u.Topic, u.EncodedPayload()).Err()
}
