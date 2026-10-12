package a

import "github.com/jmoiron/sqlx"

var DB *sqlx.DB

type User struct {
	ID int64
}

func (u User) Name() string {
	var name string
	_ = DB.Get(&name, "SELECT name FROM users WHERE id = ?", u.ID)
	return name
}
