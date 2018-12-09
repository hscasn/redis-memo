// Package memo provides a simple way to memoize Go functions with Redis
package memo

import (
	"fmt"
	"time"

	"github.com/go-redis/redis"
)

// Fn is the signature of a function that can be memoized
type Fn func(string) (string, error)

type request struct {
	params   string
	response chan<- result
}

type result struct {
	value string
	err   error
}

// Memo is an instantiated memoizer
type Memo struct {
	name              string
	redis             *redis.Client
	requests          chan request
	ttl               time.Duration
	lockTTL           time.Duration
	fn                Fn
	maxLockTries      int
	lockRetryInterval time.Duration
	frozen            bool
}

// New memoization for a function. Give the memoizer a  name, a redis client,
// and the function to be memoized. If no options are used, the default ttl
// will be 20 minutes, with 10 lock retries every 100 milliseconds, and lock
// ttl of 1 minute.
// The name of the memoizer is important: memoizers with the same name
// will share the same memory on redis (useful, for example, when you have
// several pods of the same service on Kubernetes)
func New(name string, redisClient *redis.Client, fn Fn, options ...Option) *Memo {
	m := &Memo{
		name:              name,
		redis:             redisClient,
		requests:          make(chan request),
		ttl:               20 * time.Minute,
		lockTTL:           1 * time.Minute,
		fn:                fn,
		maxLockTries:      10,
		lockRetryInterval: 100 * time.Millisecond,
	}
	for _, option := range options {
		option(m)
	}
	m.frozen = true
	go m.listen()
	return m
}

// Get the memoized value for an input. If the call is not yet memoized, it
// will run the function
func (m *Memo) Get(key string) (string, error) {
	response := make(chan result)
	m.requests <- request{key, response}
	res := <-response
	return res.value, res.err
}

func (m *Memo) listen() {
	for req := range m.requests {
		go m.retrieve(req, 0)
	}
}

func (m *Memo) retrieve(req request, tryNo int) {
	redisKey := fmt.Sprintf("%s|%s", m.name, req.params)
	value, err := m.redis.Get(redisKey).Result()
	res := result{
		value: value,
	}
	if err != nil && err != redis.Nil {
		res.err = err
	}
	if err == redis.Nil {
		lockKey := "lock|" + redisKey
		didSet, err := m.redis.SetNX(lockKey, "locked", m.lockTTL).Result()
		if err != nil || !didSet {
			if err != nil && err != redis.Nil {
				go m.deliver(req.response, result{
					err: err,
				})
				return
			}
			if tryNo > m.maxLockTries {
				go m.deliver(req.response, result{
					err: fmt.Errorf("could not obtain lock"),
				})
				return
			}
			time.Sleep(m.lockRetryInterval)
			m.retrieve(req, tryNo+1)
			return
		}
		res.value, res.err = m.fn(req.params)
		if res.err == nil {
			res.err = m.redis.SetNX(redisKey, res.value, m.ttl).Err()
		}
		m.redis.Del(lockKey)
	}
	go m.deliver(req.response, res)
}

func (m *Memo) deliver(response chan<- result, res result) {
	response <- res
}

// Option is a custom setting that can be applied to the New Memo constructor
type Option func(m *Memo)

// WithCacheTTL will set a custom TTL for the entries in the cache
func WithCacheTTL(value time.Duration) Option {
	return func(m *Memo) {
		if m.frozen {
			panic("Trying to modify a memoizer after it has been instantiated")
		}
		m.ttl = value
	}
}

// WithMaxLockTries will set a custom limit for retries when obtaining a lock
func WithMaxLockTries(value int) Option {
	return func(m *Memo) {
		if m.frozen {
			panic("Trying to modify a memoizer after it has been instantiated")
		}
		m.maxLockTries = value
	}
}

// WithLockRetryInterval will set a custom interval for the lock to be retried
func WithLockRetryInterval(value time.Duration) Option {
	return func(m *Memo) {
		if m.frozen {
			panic("Trying to modify a memoizer after it has been instantiated")
		}
		m.lockRetryInterval = value
	}
}

// WithLockTTL will set a custom TTL for a lock
func WithLockTTL(value time.Duration) Option {
	return func(m *Memo) {
		if m.frozen {
			panic("Trying to modify a memoizer after it has been instantiated")
		}
		m.lockTTL = value
	}
}
