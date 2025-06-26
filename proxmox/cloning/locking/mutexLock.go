package locking

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/bsm/redislock"
	"github.com/redis/go-redis/v9"
)

var (
	rdb = redis.NewClient(&redis.Options{
		Addr:     os.Getenv("REDIS_ADDR"),
		Password: os.Getenv("REDIS_PASSWORD"),
		DB:       0,
	})
)

// try to acquire a redis mutex lock for an allotted amount of time with specified backoff
func TryAcquireLockWithBackoff(ctx context.Context, lockKey string, ttl time.Duration, maxAttempts int, initialBackoff time.Duration) (*redislock.Lock, error) {
	locker := redislock.New(rdb)
	backoff := initialBackoff

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		lock, err := locker.Obtain(ctx, lockKey, ttl, nil)
		if err == nil {
			return lock, nil
		}

		if err == redislock.ErrNotObtained {
			if attempt == maxAttempts {
				break
			}
			time.Sleep(backoff)
			backoff *= 2
			continue
		}

		return nil, fmt.Errorf("unexpected error while acquiring lock: %v", err)
	}

	return nil, fmt.Errorf("could not obtain lock %q after %d attempts", lockKey, maxAttempts)
}
