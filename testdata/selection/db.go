package selection

import (
	"context"
	"net/http"
	"sync"

	"github.com/jmoiron/sqlx"
)

var db *sqlx.DB

type Wrapped struct {
	*sqlx.DB
}

type querier interface {
	Get(dest any, query string, args ...any) error
	GetContext(ctx context.Context, dest any, query string, args ...any) error
}

type store interface {
	Get(key string) string
}

type cache struct {
	m sync.Map
}

func (c *cache) Get(key string) string {
	v, _ := c.m.Load(key)
	s, _ := v.(string)
	return s
}

func configurePool(conn *sqlx.DB) {
	conn.SetMaxOpenConns(10)
}

func lookup(c *cache, s store, h http.Header) string {
	return c.Get("a") + s.Get("b") + h.Get("c")
}

func embedded(w Wrapped) error {
	var n int
	return w.Get(&n, "SELECT 1")
}

func viaInterface(q querier) error {
	var n int
	return q.Get(&n, "SELECT 1")
}
