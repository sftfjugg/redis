package redis

import (
	"errors"
	"strconv"
	"time"

	"github.com/coredns/coredns/plugin"

	"github.com/mediocregopher/radix.v2/pool"
	"github.com/miekg/dns"
)

// Redis is plugin that looks up responses in a cache and caches replies.
// It has a success and a denial of existence cache.
type Redis struct {
	Next  plugin.Handler
	Zones []string

	pool *pool.Pool
	nttl time.Duration
	pttl time.Duration

	addr string
	idle int
	// Testing.
	now func() time.Time
}

func New() *Redis {
	return &Redis{
		Zones: []string{"."},
		addr:  "127.0.0.1:6379",
		idle:  10,
		pool:  &pool.Pool{},
		pttl:  maxTTL,
		nttl:  maxNTTL,
		now:   time.Now,
	}
}

func Add(p *pool.Pool, key int, m *dns.Msg, duration time.Duration) error {
	// SETEX key duration m
	conn, err := p.Get()
	if err != nil {
		return err
	}
	defer p.Put(conn)

	resp := conn.Cmd("SETEX", strconv.Itoa(key), int(duration.Seconds()), ToString(m))

	return resp.Err
}

func Get(p *pool.Pool, key int) (*dns.Msg, error) {
	// GET key
	conn, err := p.Get()
	if err != nil {
		return nil, err
	}
	defer p.Put(conn)

	resp := conn.Cmd("GET", strconv.Itoa(key))
	if resp.Err != nil {
		return nil, resp.Err
	}

	ttl := 0 // Item just expired, slap 0 TTL on it.
	respTTL := conn.Cmd("TTL", strconv.Itoa(key))
	if respTTL.Err == nil {
		ttl, err = respTTL.Int()
		if err != nil {
			ttl = 0
		}
	}

	s, _ := resp.Str()
	if s == "" {
		return nil, errors.New("not found")
	}

	m := FromString(s, ttl)

	return m, nil
}

func (r *Redis) get(now time.Time, qname string, qtype uint16, do bool) *dns.Msg {
	k := hash(qname, qtype, do)

	m, err := Get(r.pool, k)
	if err != nil {
		cacheMisses.Inc()
		return nil
	}
	cacheHits.Inc()
	return m
}

func (r *Redis) connect() {
	// Can we ignore err here, i.e. will we try to connect later on?
	r.pool, _ = pool.New("tcp", r.addr, r.idle)
	return
}
