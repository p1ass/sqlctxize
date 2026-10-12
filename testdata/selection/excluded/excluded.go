package excluded

import "database/sql"

func Count(db *sql.DB) error {
	return db.QueryRow("SELECT COUNT(*) FROM users").Err()
}
