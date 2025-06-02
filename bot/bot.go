package bot

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"telegram-bot/db"
	"telegram-bot/downloader"
	"telegram-bot/models"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// RegistrationState определяет состояние регистрации пользователя
type RegistrationState struct {
	Step   string // "waiting_for_login", "waiting_for_password", или пустая строка (нет регистрации)
	Login  string // Временное хранение логина
}

// Bot структура с добавлением карты для отслеживания состояния регистрации
type Bot struct {
	bot               *tgbotapi.BotAPI
	db                *db.Database
	downloader        *downloader.Downloader
	registrationState map[int64]RegistrationState // Хранит состояние регистрации по userID
	stateMutex        sync.Mutex                  // Для безопасного доступа к registrationState
}

func StartBot(token string, database *db.Database, downloader *downloader.Downloader) error {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return err
	}

	b := &Bot{
		bot:               bot,
		db:                database,
		downloader:        downloader,
		registrationState: make(map[int64]RegistrationState),
	}

	// Загрузка часового пояса Киева (Europe/Kyiv)
	loc, err := time.LoadLocation("Europe/Kyiv")
	if err != nil {
		return fmt.Errorf("ошибка загрузки часового пояса Europe/Kyiv: %v", err)
	}

	// Вывод сообщения о запуске бота на экран
	fmt.Println("Бот запущен:", time.Now().In(loc).Format("02-01-2006 15:04:05 EEST"))

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)
	b.handleUpdates(updates)
	return nil
}

func (b *Bot) handleUpdates(updates tgbotapi.UpdatesChannel) {
	for update := range updates {
		if update.Message == nil {
			continue
		}

		user := b.registerUser(update.Message.From)
		msg := tgbotapi.NewMessage(update.Message.Chat.ID, "")

		// Проверяем, находится ли пользователь в процессе регистрации
		b.stateMutex.Lock()
		state, exists := b.registrationState[update.Message.From.ID]
		b.stateMutex.Unlock()

		if exists && state.Step != "" {
			// Обрабатываем ввод логина или пароля
			msg.Text = b.handleRegistrationStep(update.Message.From, update.Message.Text, state)
		} else if update.Message.IsCommand() {
			// Обрабатываем команды
			switch update.Message.Command() {
			case "start":
				msg.Text = b.handleStart(user)
			case "h", "help":
				msg.Text = b.handleHelp(user)
			case "users":
				msg.Text = b.handleUsers(user)
			case "reg":
				msg.Text = b.handleRegister(update.Message.From)
			default:
				if strings.HasPrefix(update.Message.Text, "/url") {
					msg.Text = b.handleURL(user, update.Message.Text)
				} else {
					msg.Text = "Неизвестная команда. Используйте /h или /help для справки."
				}
			}
		} else {
			// Обрабатываем обычный текст
			msg.Text = b.handleText(user, update.Message.Text)
		}

		b.bot.Send(msg)
	}
}

func (b *Bot) registerUser(tgUser *tgbotapi.User) *models.User {
	// Проверяем, существует ли пользователь в базе
	existingUser, err := b.db.GetUser(tgUser.ID)
	if err == nil && existingUser.ID != 0 {
		// Пользователь уже существует, возвращаем его данные
		return existingUser
	}

	// Создаём нового пользователя
	user := &models.User{
		ID:            tgUser.ID,
		FirstName:     tgUser.FirstName,
		LastName:      tgUser.LastName,
		Status:        "guest",
		DownloadCount: 0,
	}

	err = b.db.RegisterUser(user)
	if err != nil {
		log.Printf("Ошибка регистрации пользователя: %v", err)
	}
	return user
}

func (b *Bot) handleStart(user *models.User) string {
	return fmt.Sprintf("Добро пожаловать, %s! Ваш статус: %s", user.FirstName, user.Status)
}

func (b *Bot) handleHelp(user *models.User) string {
	if user.Status == "admin" {
		return "Команды:\n/reg - Зарегистрироваться\n/url <YouTube URL> - Скачать видео и аудио\n/users - Показать список пользователей\n/h или /help - Показать это сообщение"
	} else if user.Status == "enable" {
		return "Команды:\n/reg - Зарегистрироваться\n/url <YouTube URL> - Скачать видео и аудио\n/h или /help - Показать это сообщение"
	}
	return "Ваш статус: guest. Используйте /reg для регистрации."
}

func (b *Bot) handleUsers(user *models.User) string {
	if user.Status != "admin" {
		return "Команда /users доступна только администраторам."
	}

	users, err := b.db.GetAllUsers()
	if err != nil {
		return "Ошибка получения списка пользователей."
	}

	var response strings.Builder
	response.WriteString("Список пользователей:\n")
	for _, u := range users {
		response.WriteString(fmt.Sprintf("ID: %d, Имя: %s %s, Статус: %s, Загрузок: %d\n",
			u.ID, u.FirstName, u.LastName, u.Status, u.DownloadCount))
	}
	return response.String()
}

func (b *Bot) handleRegister(tgUser *tgbotapi.User) string {
	// Проверяем, существует ли пользователь в базе
	user, err := b.db.GetUser(tgUser.ID)
	if err == nil && user.ID != 0 {
		if user.Status == "enable" || user.Status == "admin" {
			return fmt.Sprintf("Вы уже зарегистрированы, %s! Ваш статус: %s", user.FirstName, user.Status)
		}
	}

	// Инициируем процесс регистрации
	b.stateMutex.Lock()
	b.registrationState[tgUser.ID] = RegistrationState{Step: "waiting_for_login"}
	b.stateMutex.Unlock()

	return "Пожалуйста, введите логин."
}

func (b *Bot) handleRegistrationStep(tgUser *tgbotapi.User, text string, state RegistrationState) string {
	b.stateMutex.Lock()
	defer b.stateMutex.Unlock()

	switch state.Step {
	case "waiting_for_login":
		// Сохраняем логин и запрашиваем пароль
		b.registrationState[tgUser.ID] = RegistrationState{
			Step:  "waiting_for_password",
			Login: text,
		}
		return "Пожалуйста, введите пароль."

	case "waiting_for_password":
		// Проверяем логин и пароль
		if state.Login != "Ukraine" || text != "Odesa" {
			// Очищаем состояние
			delete(b.registrationState, tgUser.ID)
			return "Ваш статус гость"
		}

		// Создаём или обновляем пользователя
		user := &models.User{
			ID:            tgUser.ID,
			FirstName:     tgUser.FirstName,
			LastName:      tgUser.LastName,
			Status:        "enable",
			DownloadCount: 0,
		}

		// Устанавливаем статус admin для определённого ID
		if tgUser.ID == 11111111 {
			user.Status = "admin"
		}

		err := b.db.RegisterUser(user)
		if err != nil {
			log.Printf("Ошибка при регистрации пользователя: %v", err)
			// Очищаем состояние
			delete(b.registrationState, tgUser.ID)
			return "Ошибка при регистрации. Попробуйте позже."
		}

		// Очищаем состояние после успешной регистрации
		delete(b.registrationState, tgUser.ID)
		return fmt.Sprintf("Регистрация успешна, %s! Ваш статус: %s", user.FirstName, user.Status)

	default:
		// На случай ошибки в состоянии
		delete(b.registrationState, tgUser.ID)
		return "Ошибка в процессе регистрации. Попробуйте снова с /reg."
	}
}

func (b *Bot) handleURL(user *models.User, text string) string {
	if user.Status != "enable" && user.Status != "admin" {
		return "Здравствуй гость"
	}

	parts := strings.SplitN(text, " ", 2)
	if len(parts) < 2 {
		return "Пожалуйста, укажите URL после команды /url."
	}

	url := parts[1]
	videoPath, audioPath, err := b.downloader.Download(url)
	if err != nil {
		return fmt.Sprintf("Ошибка скачивания: %v", err)
	}

	// Отправка файлов пользователю
	chatID := user.ID
	isYouTube := strings.Contains(url, "youtube.com") || strings.Contains(url, "youtu.be")

	if isYouTube {
		// Для YouTube отправляем только аудио
		if audioPath != "" {
			b.sendFile(chatID, audioPath, "audio")
		}
	} else {
		// Для TikTok отправляем видео и аудио
		if videoPath != "" {
			b.sendFile(chatID, videoPath, "video")
		}
		if audioPath != "" {
			b.sendFile(chatID, audioPath, "audio")
		}
	}

	// Увеличение счетчика загрузок
	b.db.IncrementDownloadCount(user.ID)
	return "Файлы успешно скачаны и отправлены!"
}

func (b *Bot) sendFile(chatID int64, filePath, fileType string) {
	var file tgbotapi.Chattable
	if fileType == "video" {
		file = tgbotapi.NewVideo(chatID, tgbotapi.FilePath(filePath))
	} else {
		file = tgbotapi.NewDocument(chatID, tgbotapi.FilePath(filePath))
	}
	_, err := b.bot.Send(file)
	if err != nil {
		log.Printf("Ошибка отправки файла %s: %v", filePath, err)
	}
}

func (b *Bot) handleText(user *models.User, text string) string {
	return "Пожалуйста, используйте команды. Для справки введите /h или /help."
}