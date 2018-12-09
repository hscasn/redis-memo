package memo

import (
	"fmt"
	"github.com/go-redis/redis"
	"math/rand"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"
)

func slowFunc(i int) int {
	time.Sleep(10 * time.Millisecond)
	return (rand.Int() % 10) + i
}

func timeFunc(fn func(string) (string, error)) time.Duration {
	start := time.Now()
	sum := 0
	for i := 0; i < 30; i++ {
		r, _ := fn("0")
		rr, _ := strconv.Atoi(r)
		sum += rr
	}
	// Keep this here, otherwise the compiler may delete the whole test
	fmt.Println(sum)
	elapsed := time.Since(start)
	return elapsed
}

func TestPerformance(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{
		DB:   0,
		Addr: "localhost:6379",
	})
	redisClient.FlushDB()
	defer redisClient.FlushDB()

	exec := func(s string) (string, error) {
		i, _ := strconv.Atoi(s)
		return strconv.Itoa(slowFunc(i)), nil
	}

	m := New("m1", redisClient, exec)

	// Let the memoized run first, to make sure we are not comparing
	// cold x hot cache
	memoizedt := float64(timeFunc(m.Get))
	nmemoizedt := float64(timeFunc(exec))

	maxThreshold := 10.0 // percent
	pct := (float64(memoizedt) / float64(nmemoizedt)) * 100
	if pct > maxThreshold {
		t.Errorf(
			"The performance of the memoizer must be at most "+
				"%.0f%% of the original function, but is "+
				"%.1f%%. Original time: %.0f, memoized "+
				"time: %.0f",
			maxThreshold,
			pct,
			nmemoizedt,
			memoizedt,
		)
	}
	fmt.Printf("Time after memoized: %.1f%%\n", pct)
}

func BenchmarkMemo(b *testing.B) {
	redisClient := redis.NewClient(&redis.Options{
		DB:   0,
		Addr: "localhost:6379",
	})
	redisClient.FlushDB()
	defer redisClient.FlushDB()

	exec := func(s string) (string, error) {
		i, _ := strconv.Atoi(s)
		return strconv.Itoa(slowFunc(i)), nil
	}
	m := New("m2", redisClient, exec)
	for i := 0; i < b.N; i++ {
		m.Get("0")
	}
}

func TestMemo(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{
		DB:   0,
		Addr: "localhost:6379",
	})
	redisClient.FlushDB()
	defer redisClient.FlushDB()

	m := New("m3", redisClient, func(s string) (string, error) {
		if s == "1" {
			return "1", fmt.Errorf("oh no")
		} else if s == "2" {
			return "2", nil
		}
		return "0", nil
	})

	var v interface{}
	var err error
	tests := []struct {
		key     string
		want    string
		wantErr bool
	}{
		{
			key:     "1",
			want:    "1",
			wantErr: true,
		},
		{
			key:     "2",
			want:    "2",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		v, err = m.Get(tt.key)
		if v != tt.want || (err != nil) != tt.wantErr {
			if err != nil {
				t.Logf("Error: %+v", err)
			}
			t.Errorf("Expected '%s'/'%t' but got '%s'/'%t'", tt.want, tt.wantErr, v, err != nil)
		}
	}
}

func TestLock(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{
		DB:   0,
		Addr: "localhost:6379",
	})
	redisClient.FlushDB()
	defer redisClient.FlushDB()

	exec := func(s string) (string, error) {
		return "", nil
	}

	m := New(
		"m3",
		redisClient,
		exec,
		WithMaxLockTries(5),
		WithLockRetryInterval(time.Millisecond),
		WithLockTTL(5*time.Minute))

	lockKey := fmt.Sprintf("lock|%s|call1", m.name)
	redisClient.Set(lockKey, "true", time.Minute)

	r, err := m.Get("call1")
	if r != "" {
		t.Errorf("result should be empty, but is %s", r)
	}
	if err == nil {
		t.Errorf("should have returned an error")
	}
}

func TestConcurrency(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{
		DB:   0,
		Addr: "localhost:6379",
	})
	redisClient.FlushDB()
	defer redisClient.FlushDB()

	conc := runtime.GOMAXPROCS(-1)
	if conc < 2 {
		t.Errorf("GOMAXPROCS needs to be at least 2 for this test, but has %d", conc)
	}

	counter := 0
	exec := func(s string) (string, error) {
		counter++
		time.Sleep(time.Second)
		return "0", nil
	}
	m := New(
		"m4",
		redisClient,
		exec,
		WithCacheTTL(2*time.Minute),
		WithMaxLockTries(20),
		WithLockRetryInterval(200*time.Millisecond),
		WithLockTTL(2*time.Second))
	no := 50
	wg := sync.WaitGroup{}
	for i := 0; i < no; i++ {
		wg.Add(1)
		go func() {
			s, err := m.Get("0")
			if err != nil {
				t.Error(err)
			}
			if s != "0" {
				t.Errorf("value returned should be '0' but is '%s'", s)
			}
			wg.Done()
		}()
	}
	wg.Wait()
	if counter != 1 {
		t.Errorf("number of times called for fn should be exactly 1, but is %d", counter)
	}
}

func TestOptions(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{
		DB:   0,
		Addr: "localhost:6379",
	})
	redisClient.FlushDB()
	defer redisClient.FlushDB()

	exec := func(s string) (string, error) {
		return "", nil
	}

	m := New(
		"m5",
		redisClient,
		exec,
		WithCacheTTL(123),
		WithLockRetryInterval(456),
		WithLockTTL(147),
		WithMaxLockTries(789))

	if m.ttl != 123 {
		t.Errorf("should have set a custom ttl, but is %d", m.ttl)
	}
	if m.lockRetryInterval != 456 {
		t.Errorf("should have set a custom lock retry interval, but is %d", m.lockRetryInterval)
	}
	if m.maxLockTries != 789 {
		t.Errorf("should have set a custom max lock tries, but is %d", m.maxLockTries)
	}
	if m.lockTTL != 147 {
		t.Errorf("should have set a custom lock ttl, but is %d", m.lockTTL)
	}
}

func TestCrossInstance(t *testing.T) {
	redisClient := redis.NewClient(&redis.Options{
		DB:   0,
		Addr: "localhost:6379",
	})
	redisClient.FlushDB()
	defer redisClient.FlushDB()

	// Different returns: this is intentional
	m1 := New("m6", redisClient, func(s string) (string, error) {
		return "m1", nil
	})
	m2 := New("m6", redisClient, func(s string) (string, error) {
		return "m2", nil
	})

	r, err := m1.Get("call1")
	if r != "m1" {
		t.Errorf("result should be m1, but is %s", r)
	}
	if err != nil {
		t.Errorf("should not have returned an error")
	}

	// Different functions but same name. Should share memory
	r, err = m2.Get("call1")
	if r != "m1" {
		t.Errorf("result should be m1, but is %s", r)
	}
	if err != nil {
		t.Errorf("should not have returned an error")
	}
}
