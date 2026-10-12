package main

import (
	"log"

	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
)

var db *sqlx.DB

var userCount = countUsers()

func init() {
	db = sqlx.MustOpen("mysql", "isucon:isucon@/isucon")
	db.MustExec("SET time_zone = '+00:00'")
}

func main() {
	if err := db.Ping(); err != nil {
		log.Fatal(err)
	}
	if err := initData(); err != nil {
		log.Fatal(err)
	}
	go func() {
		_ = refresh()
	}()
	log.Println(userCount)
}

func initData() error {
	_, err := db.Exec("TRUNCATE users")
	return err
}

func refresh() error {
	return db.Ping()
}

func countUsers() int {
	var n int
	_ = db.Get(&n, "SELECT COUNT(*) FROM users")
	return n
}
