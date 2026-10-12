package funclit

import (
	"context"
	"net/http"
	"sync"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
	"golang.org/x/sync/errgroup"
)

var db *sqlx.DB

var once sync.Once

func updateAll(ids []int64) error {
	eg, ctx := errgroup.WithContext(context.Background())
	for _, id := range ids {
		eg.Go(func() error { _, err := db.Exec("UPDATE users SET seen = 1 WHERE id = ?", id); return err })
	}
	<-ctx.Done()
	return eg.Wait()
}

func pingAll() error {
	var eg errgroup.Group
	eg.Go(func() error { return db.Ping() })
	return eg.Wait()
}

func loadMaster() {
	once.Do(func() { db.MustExec("SELECT 1") })
}

func register(e *echo.Echo) {
	e.GET("/health", func(c echo.Context) error { return c.NoContent(http.StatusOK) })
	e.GET("/count", func(c echo.Context) error { var n int; return db.Get(&n, "SELECT COUNT(*) FROM users") })
	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			if err := db.Ping(); err != nil {
				return err
			}
			return next(c)
		}
	})
}
