package handlers

import (
	"fmt"
	"net/http"

	"github.com/jmoiron/sqlx"
	"github.com/labstack/echo/v4"
)

var dbConn *sqlx.DB

type Livestream struct {
	ID    int64 `db:"id"`
	Owner int64 `db:"owner_id"`
}

type LivestreamResponse struct {
	ID        int64
	OwnerName string
}

func Register(e *echo.Echo, mux *http.ServeMux) {
	e.GET("/api/livestream", getLivestreamsHandler)
	e.GET("/api/ping", pingHandler)
	mux.HandleFunc("/api/count", countHandler)
	mux.HandleFunc("/api/inline", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, countUsers())
	})
	mux.Handle("/api/server", &server{})
	run(cleanup)
}

func getLivestreamsHandler(c echo.Context) error {
	tx, err := dbConn.Beginx()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var ls []Livestream
	if err := tx.Select(&ls, "SELECT * FROM livestreams"); err != nil {
		return err
	}
	resps := make([]LivestreamResponse, 0, len(ls))
	for _, l := range ls {
		resp, err := fillLivestreamResponse(tx, l)
		if err != nil {
			return err
		}
		resps = append(resps, resp)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return c.JSON(http.StatusOK, resps)
}

func pingHandler(ctx echo.Context) error {
	return dbConn.Ping()
}

func fillLivestreamResponse(tx *sqlx.Tx, l Livestream) (LivestreamResponse, error) {
	var name string
	if err := tx.Get(&name, "SELECT name FROM users WHERE id = ?", l.Owner); err != nil {
		return LivestreamResponse{}, err
	}
	return LivestreamResponse{ID: l.ID, OwnerName: name}, nil
}

func countHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, countUsers())
}

type server struct{}

func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fmt.Fprint(w, countUsers())
}

type report struct{}

func (report) String() string {
	return fmt.Sprint(countUsers())
}

func countUsers() int {
	var n int
	_ = dbConn.Get(&n, "SELECT COUNT(*) FROM users")
	return n
}

func run(f func()) {
	f()
}

func cleanup() {
	dbConn.MustExec("DELETE FROM sessions")
}
