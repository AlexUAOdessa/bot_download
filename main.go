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
	// Загрузка переменных окружения
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Ошибка загрузки файла .env: %v", err)
	}

	// Инициализация базы данных
	database, err := db.InitDB("users.db")
	if err != nil {
		log.Fatalf("Ошибка инициализации базы данных: %v", err)
	}
	defer database.Close()

	// Инициализация загрузчика
	downloader := downloader.NewDownloader()

	// Инициализация и запуск бота
	botToken := os.Getenv("TELEGRAM_BOT_TOKEN")
	if botToken == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN не установлен")
	}

	err = bot.StartBot(botToken, database, downloader)
	if err != nil {
		log.Fatalf("Ошибка запуска бота: %v", err)
	}
}