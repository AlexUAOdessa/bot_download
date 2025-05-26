package models

type User struct {
	ID            int64  `db:"id"`
	FirstName     string `db:"first_name"`
	LastName      string `db:"last_name"`
	Status        string `db:"status"`
	DownloadCount int    `db:"download_count"`
}