# redis-memo

Memoize Go functions with Redis.

The locking mechanism is simple, not using Redlock.

[Documentation](https://godoc.org/github.com/hscasn/redis-memo/pkg/memo)

*License:* Apache 2.0

## Example

```
package main

import (
	"fmt"
	"github.com/go-redis/redis"
	"github.com/hscasn/redis-memo/pkg/memo"
	"time"
)

func sayWorld(s string) (string, error) {
	return "world", nil
}

func main() {
        // Not memoized
	result1, err := sayWorld("hello")
	fmt.Println(result1, err) // "world", nil

        // Memoizing it...
	redisClient := redis.NewClient(&redis.Options{
		DB:   0,
		Addr: "localhost:6379",
	})

	m := memo.New(
		"sayworld",
		redisClient,
		sayWorld,
		memo.WithCacheTTL(2*time.Minute),
		memo.WithMaxLockTries(20),
		memo.WithLockRetryInterval(200*time.Millisecond),
		memo.WithLockTTL(5*time.Minute))

	result2, err := m.Get("hello")
	fmt.Println(result2, err) // "world", nil
}
```

## Options

### WithCacheTTL(value time.Duration)
TTL for entries in the cache

### WithMaxLockTries(value int)
Maximum retries for obtaining a lock. If the limit is exceeded, the *Get* function will return an error

### WithLockRetryInterval(value time.Duration)
Interval for retrying a lock, in case it doesn't succeed

### WithLockTTL(value time.Duration)
TTL for a lock. Adjust it so it matches the expected duration of the function call. For example: if the function takes 5 minutes to run, you could adjust it to be around 7 minutes
