package helper

import (
	"database/sql"
	"time"
)

type Shutdown func()

func InitializeTracerProvider(serviceName string) (Shutdown, error) {
	start := time.Now()
	return func() { _ = time.Since(start) }, nil
}

func WrapDB(db *sql.DB) *sql.DB {
	db.SetConnMaxLifetime(time.Minute)
	return db
}
