package config

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/redis/go-redis/v9"
)

var RedisClient *redis.Client

func InitRedis(redisURL string) *redis.Client {
	if redisURL == "" {
		slog.Warn("REDIS_URL not configured, cache invalidation will be disabled")
		return nil
	}

	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		slog.Warn("Failed to parse REDIS_URL", "error", err.Error())
		return nil
	}

	opts.DialTimeout = 5 * time.Second
	opts.ReadTimeout = 5 * time.Second
	opts.WriteTimeout = 5 * time.Second
	opts.MaxRetries = 2

	client := redis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		slog.Warn("Failed to connect to Redis, continuing without cache invalidation", "error", err.Error())
		return nil
	}

	RedisClient = client
	slog.Info("Connected to Redis successfully")
	return RedisClient
}

// DelPattern scans and removes keys matching a wildcard pattern
func DelPattern(ctx context.Context, pattern string) int {
	if RedisClient == nil {
		return 0
	}

	var cursor uint64
	totalDeleted := 0

	for {
		keys, nextCursor, err := RedisClient.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			slog.Warn("Redis Scan error", "pattern", pattern, "error", err.Error())
			break
		}

		if len(keys) > 0 {
			deleted, err := RedisClient.Del(ctx, keys...).Result()
			if err == nil {
				totalDeleted += int(deleted)
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return totalDeleted
}

// InvalidateVideoCaches invalidates both feed caches and individual video caches
func InvalidateVideoCaches(ctx context.Context, videoIDs ...string) {
	if RedisClient == nil {
		return
	}

	// Invalidate feed listings across categories, sorts, and search
	DelPattern(ctx, "videos_*")
	DelPattern(ctx, "suggestions:*")
	DelPattern(ctx, "recommendations:*")
	DelPattern(ctx, "search:*")

	// Invalidate specific video keys
	for _, id := range videoIDs {
		if id != "" {
			RedisClient.Del(ctx, fmt.Sprintf("video:%s", id))
			RedisClient.Del(ctx, fmt.Sprintf("stream:%s", id))
			DelPattern(ctx, fmt.Sprintf("*%s*", id))
		}
	}
}
