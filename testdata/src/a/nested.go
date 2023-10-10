package a

func inner() {
	_, err := db.Query("SELECT * FROM users WHERE id = ?", 1)
	if err != nil {
		return
	}
}

func outer() {
	inner()
}
