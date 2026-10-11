package main

import (
	"log"
	"os"
	"telegram-bot/bot"
	"telegram-bot/db"
	"telegram-bot/downloader"

	"github.com/joho/godotenv"
)

func main() {
	// Загрузка переменных окружения из файла .env
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Ошибка загрузки файла .env: %v", err)
	}

	// Инициализация базы данных SQLite
	database, err := db.InitDB("users.db")
	if err != nil {
		log.Fatalf("Ошибка инициализации базы данных: %v", err)
	}
	defer database.Close()

	// Инициализация загрузчика медиафайлов
	dl := downloader.NewDownloader()

	// Получение токена Telegram-бота из переменных окружения
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN не установлен в переменных окружения")
	}

	// Запуск Telegram-бота
	err = bot.StartBot(botToken, database, dl)
	if err != nil {
		log.Fatalf("Ошибка запуска бота: %v", err)
	}
}
