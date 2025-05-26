package db

import (
	"telegram-bot/models"

	"github.com/jmoiron/sqlx"
	_ "github.com/mattn/go-sqlite3"
)

type Database struct {
    *sqlx.DB
}

func InitDB(dbPath string) (*Database, error) {
    db, err := sqlx.Connect("sqlite3", dbPath)
    if err != nil {
        return nil, err
    }

    // Создание таблицы пользователей
    const crTable = `
    CREATE TABLE IF NOT EXISTS users (
        id INTEGER PRIMARY KEY,
        first_name TEXT,
        last_name TEXT,
        status TEXT,
        download_count INTEGER DEFAULT 0
    )`

    _, err = db.Exec(crTable)
    if err != nil {
        return nil, err
    }

    return &Database{db}, nil
}

// RegisterUser регистрирует пользователя или обновляет его данные
func (db *Database) RegisterUser(user *models.User) error {
    query := `
    INSERT INTO users (id, first_name, last_name, status, download_count)
    VALUES (:id, :first_name, :last_name, :status, :download_count)
    ON CONFLICT(id) DO UPDATE SET
        first_name = excluded.first_name,
        last_name = excluded.last_name,
        status = excluded.status`

    _, err := db.NamedExec(query, user)
    return err
}

// GetUser получает данные пользователя по ID
func (db *Database) GetUser(id int64) (*models.User, error) {
    user := &models.User{}
    err := db.Get(user, "SELECT * FROM users WHERE id = ?", id)
    return user, err
}

// GetAllUsers возвращает список всех пользователей
func (db *Database) GetAllUsers() ([]models.User, error) {
    var users []models.User
    err := db.Select(&users, "SELECT * FROM users")
    return users, err
}

// IncrementDownloadCount увеличивает счетчик загрузок
func (db *Database) IncrementDownloadCount(userID int64) error {
    _, err := db.Exec("UPDATE users SET download_count = download_count + 1 WHERE id = ?", userID)
    return err
}