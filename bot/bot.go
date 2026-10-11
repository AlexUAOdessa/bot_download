package bot

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
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
	Step  string // "waiting_for_login", "waiting_for_password", или пустая строка (нет регистрации)
	Login string // Временное хранение логина
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
		return "Команды:\n/reg - Зарегистрироваться\n/url <URL> - Скачать видео и аудио\n/users - Показать список пользователей\n/h или /help - Показать это сообщение"
	} else if user.Status == "enable" {
		return "Команды:\n/reg - Зарегистрироваться\n/url <URL> - Скачать видео и аудио\n/h или /help - Показать это сообщение"
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
	user, err := b.db.GetUser(tgUser.ID)
	if err == nil && user.ID != 0 {
		if user.Status == "enable" || user.Status == "admin" {
			return fmt.Sprintf("Вы уже зарегистрированы, %s! Ваш статус: %s", user.FirstName, user.Status)
		}
	}

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
		b.registrationState[tgUser.ID] = RegistrationState{
			Step:  "waiting_for_password",
			Login: text,
		}
		return "Пожалуйста, введите пароль."

	case "waiting_for_password":
		if state.Login != "Ukraine" || text != "Odesa" {
			delete(b.registrationState, tgUser.ID)
			return "Неверный логин или пароль. Логин должен быть 'Ukraine', а пароль — 'Odesa'. Попробуйте снова с /reg."
		}

		user := &models.User{
			ID:            tgUser.ID,
			FirstName:     tgUser.FirstName,
			LastName:      tgUser.LastName,
			Status:        "enable",
			DownloadCount: 0,
		}

		if tgUser.ID == 368154534 {
			user.Status = "admin"
		}

		err := b.db.RegisterUser(user)
		if err != nil {
			log.Printf("Ошибка при регистрации пользователя: %v", err)
			delete(b.registrationState, tgUser.ID)
			return "Ошибка при регистрации. Попробуйте позже."
		}

		delete(b.registrationState, tgUser.ID)
		return fmt.Sprintf("Регистрация успешна, %s! Ваш статус: %s", user.FirstName, user.Status)

	default:
		delete(b.registrationState, tgUser.ID)
		return "Ошибка в процессе регистрации. Попробуйте снова с /reg."
	}
}

// handleURL обрабатывает команду скачивания по ссылке
func (b *Bot) handleURL(user *models.User, text string) string {
	if user.Status != "enable" && user.Status != "admin" {
		return "Команда /url доступна только для пользователей со статусом enable или admin."
	}

	parts := strings.SplitN(text, " ", 2)
	if len(parts) < 2 {
		return "Пожалуйста, укажите URL после команды /url."
	}

	url := strings.TrimSpace(parts[1])

	// 1. Скачиваем файлы на сервер
	files, err := b.downloader.Download(user.ID, url)
	if err != nil {
		return fmt.Sprintf("Ошибка скачивания: %v", err)
	}

	if len(files) == 0 {
		return "Скачивание завершилось, но файлы не найдены."
	}

	// Лимит Telegram для отправки файлов напрямую (50 МБ, берем 48 МБ для безопасности)
	const maxTelegramSize = 48 * 1024 * 1024

	// 2. Обрабатываем каждый скачанный файл
	for _, file := range files {
		fileInfo, err := os.Stat(file)
		if err != nil {
			log.Printf("❌ Ошибка получения информации о файле %s: %v", file, err)
			continue
		}

		fileSize := fileInfo.Size()
		fileName := filepath.Base(file)
		fileSizeMB := float64(fileSize) / (1024 * 1024)

		log.Printf("📁 Проверка файла: %s (%.2f МБ)", fileName, fileSizeMB)

		// Если файл больше 50 МБ — отправляем на Google Диск в папку music
		if fileSize > maxTelegramSize {
			log.Printf("⚠️ Файл превышает 50 МБ. Загружаем на Google Диск в папку 'music'...")

			// Проверяем наличие папки music на диске и создаем при необходимости
			err = ensureGoogleDriveMusicFolder()
			if err != nil {
				log.Printf("❌ Ошибка создания/проверки папки music: %v", err)
				b.bot.Send(tgbotapi.NewMessage(user.ID, fmt.Sprintf("Файл %s более 50 МБ, но не удалось создать папку music на Google Диске.", fileName)))
				continue
			}

			// Загружаем файл и получаем публичную ссылку
			driveLink, err := uploadToGoogleDriveMusic(file, fileName)
			if err != nil {
				log.Printf("❌ Ошибка выгрузки на Google Диск: %v", err)
				b.bot.Send(tgbotapi.NewMessage(user.ID, fmt.Sprintf("Ошибка загрузки файла %s на Google Диск.", fileName)))
				continue
			}

			// Отправляем пользователю ссылку на Google Диск
			msgText := fmt.Sprintf("📦 Файл *%s* (%.2f МБ) превышает лимит Telegram.\n\n🔗 Скачать с Google Диска (папка music):\n%s", fileName, fileSizeMB, driveLink)
			msg := tgbotapi.NewMessage(user.ID, msgText)
			msg.ParseMode = "Markdown"
			b.bot.Send(msg)

		} else {
			// Если файл меньше или равен 50 МБ — отправляем напрямую в Telegram
			log.Printf("✅ Файл в пределах лимита. Отправляем в Telegram...")
			lower := strings.ToLower(file)
			if strings.HasSuffix(lower, ".mp4") ||
				strings.HasSuffix(lower, ".webm") ||
				strings.HasSuffix(lower, ".mkv") ||
				strings.HasSuffix(lower, ".mov") {
				b.sendFileWithCaption(user.ID, file, "video", "")
			} else {
				b.sendFileWithCaption(user.ID, file, "document", "")
			}
		}
	}

	// 3. Очищаем временные локальные файлы
	downloader.DeleteFiles(files)

	// 4. Увеличиваем счетчик загрузок пользователя
	b.db.IncrementDownloadCount(user.ID)

	return "Обработка и отправка файлов завершена!"
}

// ensureGoogleDriveMusicFolder проверяет наличие папки music и создает её при отсутствии
func ensureGoogleDriveMusicFolder() error {
	cmd := exec.Command("rclone", "lsf", "disk_bot:music", "--dirs-only")
	output, err := cmd.CombinedOutput()

	outputStr := string(output)
	if err != nil || !strings.Contains(outputStr, "music") {
		log.Printf("📁 Папка 'music' не найдена на Google Диске. Создаем...")
		mkdirCmd := exec.Command("rclone", "mkdir", "disk_bot:music")
		if mkdirErr := mkdirCmd.Run(); mkdirErr != nil {
			return fmt.Errorf("не удалось создать папку music: %v", mkdirErr)
		}
		log.Println("✅ Папка 'music' успешно создана на Google Диске.")
	}
	return nil
}

// uploadToGoogleDriveMusic загружает файл в папку music и возвращает публичную ссылку
func uploadToGoogleDriveMusic(filePath, fileName string) (string, error) {
	cmd := exec.Command("rclone", "copyto", filePath, "disk_bot:music/"+fileName)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("ошибка копирования rclone: %v", err)
	}

	linkCmd := exec.Command("rclone", "link", "disk_bot:music/"+fileName)
	linkOutput, err := linkCmd.Output()
	if err != nil {
		return "https://drive.google.com (файл загружен в папку music)", nil
	}

	return strings.TrimSpace(string(linkOutput)), nil
}

func (b *Bot) sendFileWithCaption(chatID int64, filePath, fileType, caption string) {
	var file tgbotapi.Chattable
	if fileType == "video" {
		vid := tgbotapi.NewVideo(chatID, tgbotapi.FilePath(filePath))
		vid.Caption = caption
		file = vid
	} else {
		doc := tgbotapi.NewDocument(chatID, tgbotapi.FilePath(filePath))
		doc.Caption = caption
		file = doc
	}
	_, err := b.bot.Send(file)
	if err != nil {
		log.Printf("Ошибка отправки файла %s: %v", filePath, err)
	}
}

func (b *Bot) handleText(user *models.User, text string) string {
	return "Пожалуйста, используйте команды. Для справки введите /h или /help."
}
