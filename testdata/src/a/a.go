package a

import "database/sql"

var db *sql.DB

func query() {
	_, err := db.Query("SELECT * FROM users WHERE id = ?", 1)
	if err != nil {
		return
	}
}

func falsyQuery() {
	Query := func() {}
	Query()
}
