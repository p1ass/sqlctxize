package a

func exec() {
	_, err := db.Exec("INSERT INTO users (id) VALUES (?)", 1)
	if err != nil {
		return
	}
}

func falsyExec() {
	Exec := func() {}
	Exec()
}
