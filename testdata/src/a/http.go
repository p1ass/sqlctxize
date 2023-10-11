package a

import "net/http"

func main() {
	http.HandleFunc("/", handler)
}

func handler(w http.ResponseWriter, r *http.Request) {
	subHandler()
}

func subHandler() {
	db.Query("SELECT * FROM users WHERE id = ?", 1)
}
