package queue

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// liveRedis is a minimal RESP client over one connection: the RedisClient
// methods and [ScriptRunner], enough to run the queue's Lua against a real
// server. GOFASTR_TEST_REDIS names the server (host:port); without it the
// live tests skip. Locally:
//
//	docker run -d --rm -p 127.0.0.1:56379:6379 redis:7.2-alpine
//	GOFASTR_TEST_REDIS=127.0.0.1:56379 go test ./battery/queue -run Live
type liveRedis struct {
	mu   sync.Mutex
	conn net.Conn
	r    *bufio.Reader
}

func dialLiveRedis(t *testing.T) *liveRedis {
	t.Helper()
	addr := os.Getenv("GOFASTR_TEST_REDIS")
	if addr == "" {
		t.Skip("GOFASTR_TEST_REDIS is not set; the live Redis tests need a server")
	}
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.Fatalf("dial %s: %v", addr, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return &liveRedis{conn: conn, r: bufio.NewReader(conn)}
}

func (c *liveRedis) do(args ...any) (any, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	var b strings.Builder
	fmt.Fprintf(&b, "*%d\r\n", len(args))
	for _, a := range args {
		s := fmt.Sprint(a)
		if v, ok := a.([]byte); ok {
			s = string(v)
		}
		fmt.Fprintf(&b, "$%d\r\n%s\r\n", len(s), s)
	}
	if _, err := c.conn.Write([]byte(b.String())); err != nil {
		return nil, err
	}
	return c.read()
}

func (c *liveRedis) read() (any, error) {
	line, err := c.r.ReadString('\n')
	if err != nil {
		return nil, err
	}
	line = strings.TrimSuffix(line, "\r\n")
	if line == "" {
		return nil, errors.New("resp: empty line")
	}
	switch line[0] {
	case '+':
		return line[1:], nil
	case '-':
		return nil, errors.New(line[1:])
	case ':':
		return strconv.ParseInt(line[1:], 10, 64)
	case '$':
		n, err := strconv.Atoi(line[1:])
		if err != nil || n < 0 {
			return nil, err
		}
		buf := make([]byte, n+2)
		if _, err := ioReadFull(c.r, buf); err != nil {
			return nil, err
		}
		return string(buf[:n]), nil
	case '*':
		n, err := strconv.Atoi(line[1:])
		if err != nil || n < 0 {
			return nil, err
		}
		out := make([]any, n)
		for i := range out {
			if out[i], err = c.read(); err != nil {
				return nil, err
			}
		}
		return out, nil
	}
	return nil, fmt.Errorf("resp: unknown reply %q", line)
}

func ioReadFull(r *bufio.Reader, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		m, err := r.Read(buf[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func (c *liveRedis) strs(v any, err error) ([]string, error) {
	if err != nil {
		return nil, err
	}
	arr, _ := v.([]any)
	out := make([]string, len(arr))
	for i, e := range arr {
		out[i] = fmt.Sprint(e)
	}
	return out, nil
}

func (c *liveRedis) LPush(_ context.Context, key string, values ...any) error {
	_, err := c.do(append([]any{"LPUSH", key}, values...)...)
	return err
}

func (c *liveRedis) RPop(_ context.Context, key string) (string, error) {
	v, err := c.do("RPOP", key)
	if err == nil && v == nil {
		return "", ErrRedisEmpty
	}
	s, _ := v.(string)
	return s, err
}

func (c *liveRedis) HGet(_ context.Context, key, field string) (string, error) {
	v, err := c.do("HGET", key, field)
	if err == nil && v == nil {
		return "", ErrRedisEmpty
	}
	s, _ := v.(string)
	return s, err
}

func (c *liveRedis) HSet(_ context.Context, key string, values ...any) error {
	_, err := c.do(append([]any{"HSET", key}, values...)...)
	return err
}

func (c *liveRedis) HGetAll(_ context.Context, key string) (map[string]string, error) {
	l, err := c.strs(c.do("HGETALL", key))
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(l)/2)
	for i := 0; i+1 < len(l); i += 2 {
		out[l[i]] = l[i+1]
	}
	return out, nil
}

func (c *liveRedis) HDel(_ context.Context, key string, fields ...string) error {
	args := []any{"HDEL", key}
	for _, f := range fields {
		args = append(args, f)
	}
	_, err := c.do(args...)
	return err
}

func (c *liveRedis) Del(_ context.Context, keys ...string) error {
	args := []any{"DEL"}
	for _, k := range keys {
		args = append(args, k)
	}
	_, err := c.do(args...)
	return err
}

func (c *liveRedis) LRange(_ context.Context, key string, start, stop int64) ([]string, error) {
	return c.strs(c.do("LRANGE", key, start, stop))
}

func (c *liveRedis) LRem(_ context.Context, key string, count int64, value any) (int64, error) {
	v, err := c.do("LREM", key, count, value)
	n, _ := v.(int64)
	return n, err
}

func (c *liveRedis) Eval(_ context.Context, script string, keys []string, args ...any) (any, error) {
	all := []any{"EVAL", script, len(keys)}
	for _, k := range keys {
		all = append(all, k)
	}
	return c.do(append(all, args...)...)
}

// The erase script, run by a real Redis: it removes the user's jobs from
// all five keys (a duplicate on a list counts each copy), keeps other
// users' jobs and entries that do not decode or name the user some other
// way, and a dry run counts the same entries and removes none.
func TestEraseScriptOnLiveRedis(t *testing.T) {
	ctx := context.Background()
	c := dialLiveRedis(t)
	name := "live-erase-" + redisRandomID()
	q := NewRedisQueue(c, name)
	keys := []string{name, name + ":processing", name + ":dead", name + ":delayed", name + ":popped"}
	t.Cleanup(func() {
		_ = q.Close()
		_ = c.Del(context.Background(), keys...)
	})
	dead := secretJob("d1", "u1")
	dead.MaxAttempts = 1
	if err := q.Enqueue(ctx, dead); err != nil {
		t.Fatal(err)
	}
	claimed, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Nack(ctx, claimed); err != nil {
		t.Fatal(err)
	}
	if err := q.Enqueue(ctx, secretJob("p1", "u1")); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Dequeue(ctx); err != nil {
		t.Fatal(err)
	}
	later := secretJob("l1", "u1")
	later.ScheduledAt = time.Now().Add(time.Hour)
	for _, j := range []Job{secretJob("m1", "u1"), secretJob("m2", "u2"), later} {
		if err := q.Enqueue(ctx, j); err != nil {
			t.Fatal(err)
		}
	}
	dup := string(mustJSON(t, secretJob("m3", "u1")))
	for _, kv := range [][2]string{
		{name, dup}, {name, dup}, {name, "not json"}, {name, `{"user_id":5,"to":"num-secret"}`},
		{name, `{"user_id":"u1x","to":"uix-secret"}`},
	} {
		if err := c.LPush(ctx, kv[0], kv[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := c.HSet(ctx, name+":popped", "1:tok", string(mustJSON(t, secretJob("o1", "u1"))), "2:tok", "{"); err != nil {
		t.Fatal(err)
	}
	holds := func(s string) int {
		n := 0
		for _, k := range []string{name, name + ":dead", name + ":delayed"} {
			l, err := c.LRange(ctx, k, 0, -1)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range l {
				if strings.Contains(v, s) {
					n++
				}
			}
		}
		for _, k := range []string{name + ":processing", name + ":popped"} {
			h, err := c.HGetAll(ctx, k)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range h {
				if strings.Contains(v, s) {
					n++
				}
			}
		}
		return n
	}
	// d1 dead, p1 processing, m1 main, l1 delayed, m3 twice, o1 popped.
	const want = 7
	if got := holds("u1-secret"); got != want {
		t.Fatalf("seeded %d of u1's entries, want %d", got, want)
	}

	dry, err := q.eraseUser(ctx, "u1", true)
	if err != nil || dry != want || holds("u1-secret") != want {
		t.Fatalf("dry run = %d, %v, leaving %d; want %d counted and none removed", dry, err, holds("u1-secret"), want)
	}
	n, err := q.eraseUser(ctx, "u1", false)
	if err != nil || n != want {
		t.Fatalf("erase = %d, %v; want %d", n, err, want)
	}
	if got := holds("u1-secret"); got != 0 {
		t.Errorf("%d of u1's entries survive the erasure", got)
	}
	for _, s := range []string{"u2-secret", "not json", "num-secret", "uix-secret"} {
		if holds(s) != 1 {
			t.Errorf("the entry holding %q was removed; it names nobody or another user", s)
		}
	}
	if h, _ := c.HGetAll(ctx, name+":popped"); h["2:tok"] != "{" {
		t.Errorf("the pop record that does not decode is gone: %v", h)
	}
}
