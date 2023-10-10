package a

import "context"

func queryWithCtx(ctx context.Context) {
	_, err := db.Query("SELECT * FROM users WHERE id = ?", 1)
	if err != nil {
		return
	}
}
