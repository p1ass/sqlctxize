package a

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
