package tx

import (
	"database/sql"

	"github.com/jmoiron/sqlx"
)

var db *sqlx.DB

type User struct {
	ID   int64  `db:"id"`
	Name string `db:"name"`
}

func sqlxTx(ids []int64) error {
	tx, err := db.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var u User
	if err := tx.Get(&u, tx.Rebind("SELECT * FROM users WHERE id = ?"), 1); err != nil {
		return err
	}
	if _, err := tx.NamedExec("INSERT INTO users (name) VALUES (:name)", u); err != nil {
		return err
	}
	rows, err := tx.NamedQuery("SELECT * FROM users WHERE name = :name", u)
	if err != nil {
		return err
	}
	rows.Close()
	if err := tx.QueryRowx("SELECT * FROM users LIMIT 1").StructScan(&u); err != nil {
		return err
	}
	tx.MustExec("DELETE FROM users WHERE id = ?", u.ID)
	stmt, err := tx.Preparex("SELECT * FROM users WHERE id = ?")
	if err != nil {
		return err
	}
	if err := stmt.Get(&u, 1); err != nil {
		return err
	}
	query, args, err := sqlx.In("SELECT * FROM users WHERE id IN (?)", ids)
	if err != nil {
		return err
	}
	var us []User
	if err := tx.Select(&us, tx.Rebind(query), args...); err != nil {
		return err
	}
	return tx.Commit()
}

func mustBegin() error {
	tx := db.MustBegin()
	tx.MustExec("DELETE FROM users")
	return tx.Commit()
}

func dbCalls() error {
	var u User
	if _, err := db.NamedExec("INSERT INTO users (name) VALUES (:name)", u); err != nil {
		return err
	}
	rows, err := db.NamedQuery("SELECT * FROM users WHERE name = :name", u)
	if err != nil {
		return err
	}
	rows.Close()
	xrows, err := db.Queryx("SELECT * FROM users")
	if err != nil {
		return err
	}
	xrows.Close()
	if err := db.QueryRowx("SELECT * FROM users LIMIT 1").StructScan(&u); err != nil {
		return err
	}
	stmt, err := db.Preparex("SELECT * FROM users WHERE id = ?")
	if err != nil {
		return err
	}
	defer stmt.Close()
	return sqlx.Get(db, &u, "SELECT * FROM users LIMIT 1")
}

func stdTx(sdb *sql.DB) error {
	if err := sdb.Ping(); err != nil {
		return err
	}
	tx, err := sdb.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec("DELETE FROM users"); err != nil {
		_ = tx.Rollback()
		return err
	}
	stmt, err := sdb.Prepare("SELECT COUNT(*) FROM users WHERE id = ?")
	if err != nil {
		return err
	}
	var n int
	if err := stmt.QueryRow(1).Scan(&n); err != nil {
		return err
	}
	return tx.Commit()
}
