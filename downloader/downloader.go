package downloader

import (
	"archive/zip"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Downloader отвечает за скачивание медиафайлов через утилиту yt-dlp.
type Downloader struct{}

// NewDownloader создает новый экземпляр структуры Downloader.
func NewDownloader() *Downloader {
	return &Downloader{}
}

// Download выполняет скачивание видео/аудио по переданному URL.
// Возвращает слайс путей к скачанным файлам или ошибку.
func (d *Downloader) Download(userID int64, rawURL string) ([]string, error) {

	// Определяем директорию, в которой находится исполняемый файл бота
	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("ошибка получения пути exe: %v", err)
	}

	exeDir := filepath.Dir(exePath)

	// ======================================================
	// СОЗДАНИЕ УНИКАЛЬНОЙ ПАПКИ ДЛЯ СКАЧИВАНИЯ
	// ======================================================

	userDir := filepath.Join(
		exeDir,
		"downloads",
		fmt.Sprintf("%d", userID),
		fmt.Sprintf("%d", time.Now().UnixNano()),
	)

	err = os.MkdirAll(userDir, 0755)
	if err != nil {
		return nil, fmt.Errorf("ошибка создания папки: %v", err)
	}

	// ======================================================
	// ПОИСК YT-DLP (ЛОКАЛЬНО ИЛИ В СИСТЕМЕ)
	// ======================================================

	ytDlpName := "yt-dlp"
	if runtime.GOOS == "windows" {
		ytDlpName = "yt-dlp.exe"
	}

	// 1. Проверяем наличие yt-dlp в папке с ботом
	ytDlpPath := filepath.Join(exeDir, ytDlpName)
	if _, err := os.Stat(ytDlpPath); err != nil {
		// 2. Если локальный файл не найден, ищем yt-dlp в системном PATH
		pathFromEnv, lookErr := exec.LookPath(ytDlpName)
		if lookErr != nil {
			return nil, fmt.Errorf("yt-dlp не найден ни в директории бота (%s), ни в системном PATH: %v", ytDlpPath, lookErr)
		}
		ytDlpPath = pathFromEnv
	}

	// ======================================================
	// ПОДГОТОВКА URL И ИДЕНТИФИКАТОРОВ
	// ======================================================

	url := cleanURL(rawURL)
	secUserID := extractSecUserID(rawURL)

	// ======================================================
	// ОБЩИЕ АРГУМЕНТЫ ДЛЯ YT-DLP
	// ======================================================

	commonArgs := []string{
		"--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36",
		"--referer", "https://www.tiktok.com/",
		"--add-header", "Accept-Language:en-US,en;q=0.9",
		"--no-check-certificates",
		"--extractor-retries", "5",
		"--fragment-retries", "5",
		"--retry-sleep", "10",
		"--sleep-requests", "10",
		"--sleep-interval", "5",
		"--max-sleep-interval", "15",
		"--force-ipv4",
		"--no-warning",
		"--yes-playlist",
		"--no-playlist-reverse",
		// "--impersonate", "chrome", // отключено — нет поддержки в текущей версии yt-dlp
	}

	// Добавляем аргументы куки (если файлы cookies.txt присутствуют)
	commonArgs = append(commonArgs, buildCookiesArgs(exeDir)...)

	// ======================================================
	// СКАЧИВАНИЕ С YOUTUBE (ИЗВЛЕЧЕНИЕ АУДИО)
	// ======================================================

	if isYouTubeURL(url) {
		fmt.Println("⬇️ Загрузка с YouTube...")

		outputTemplate := filepath.Join(
			userDir,
			"youtube_%(playlist_index)s_%(id)s.%(ext)s",
		)

		args := []string{
			"-x",
			"--audio-format", "mp3",
			"--audio-quality", "0",
			"-o", outputTemplate,
		}

		args = append(args, commonArgs...)
		args = append(args, url)

		err := runYtDlp(ytDlpPath, args)
		if err != nil {
			return nil, fmt.Errorf("ошибка YouTube:\n%v", err)
		}

		return collectFiles(userDir)
	}

	// ======================================================
	// СКАЧИВАНИЕ С TIKTOK / ДРУГИХ СЕРВИСОВ (ВИДЕО)
	// ======================================================

	fmt.Println("⬇️ Загрузка TikTok / Обычная загрузка...")

	outputTemplate := filepath.Join(
		userDir,
		"video_%(playlist_index)s_%(id)s.%(ext)s",
	)

	// Ограничиваем видео до 720p
	args := []string{
		"-f", "bv*[height<=720]+ba/b[height<=720]/best[height<=720]/best",
		"--merge-output-format", "mp4",
		"-o", outputTemplate,
	}

	args = append(args, commonArgs...)
	args = append(args, url)

	err = runYtDlp(ytDlpPath, args)

	// ======================================================
	// ОБРАБОТКА ОШИБОК И FALLBACK СЦЕНАРИИ ДЛЯ TIKTOK
	// ======================================================

	if err != nil {
		errText := err.Error()

		if strings.Contains(errText, "/playlist/") || strings.Contains(errText, "Unsupported URL") {
			profileURL := extractTikTokProfileFromText(errText)

			if profileURL != "" {
				fmt.Println("⚠️ Плейлист TikTok не поддерживается")
				fmt.Println("➡️ Резервный вариант (профиль):", profileURL)

				profileArgs := []string{
					"-f", "bv*[height<=720]+ba/b[height<=720]/best[height<=720]/best",
					"--merge-output-format", "mp4",
					"-o", outputTemplate,
				}

				profileArgs = append(profileArgs, commonArgs...)
				profileArgs = append(profileArgs, profileURL)

				err2 := runYtDlp(ytDlpPath, profileArgs)

				// ==============================================
				// ВТОРОЙ FALLBACK (ПО SEC_USER_ID)
				// ==============================================

				if err2 != nil {
					if secUserID == "" {
						secUserID = extractSecUserID(errText)
					}

					if secUserID == "" {
						secUserID = extractSecUserID(err2.Error())
					}

					if secUserID != "" {
						fmt.Println("⚠️ Не удалось загрузить по профилю")
						fmt.Println("➡️ Резервный вариант (tiktokuser):", secUserID)

						userIDArgs := []string{
							"-f", "bv*[height<=720]+ba/b[height<=720]/best[height<=720]/best",
							"--merge-output-format", "mp4",
							"-o", outputTemplate,
						}

						userIDArgs = append(userIDArgs, commonArgs...)
						userIDArgs = append(userIDArgs, "tiktokuser:"+secUserID)

						err3 := runYtDlp(ytDlpPath, userIDArgs)

						if err3 != nil {
							return nil, fmt.Errorf(
								"ошибка yt-dlp резервной загрузки по tiktokuser:\n%v",
								err3,
							)
						}

					} else {
						return nil, fmt.Errorf(
							"ошибка yt-dlp резервной загрузки профиля:\n%v",
							err2,
						)
					}
				}

			} else {
				return nil, fmt.Errorf(
					"ошибка yt-dlp:\n%v",
					err,
				)
			}

		} else {
			return nil, fmt.Errorf(
				"ошибка yt-dlp:\n%v",
				err,
			)
		}
	}

	return collectFiles(userDir)
}

// ======================================================
// ПОДГОТОВКА ФАЙЛОВ К ОТПРАВКЕ (ZIP + РАЗБИЕНИЕ)
// ======================================================

// PrepareFilesForSending подготавливает файл к отправке:
// - если ≤ maxBytes → возвращает оригинал
// - если больше → упаковывает в ZIP
//   - ZIP ≤ maxBytes → возвращает ZIP
//   - ZIP больше → режет ZIP на части
func PrepareFilesForSending(filePath string, maxBytes int64) ([]string, error) {
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		log.Printf("❌ Ошибка получения информации о файле %s: %v", filePath, err)
		return nil, err
	}

	fileSizeMB := float64(fileInfo.Size()) / (1024 * 1024)
	log.Printf("🔍 Проверка файла: %s (%.2f МБ)", filePath, fileSizeMB)

	// Файл уже подходит по размеру
	if fileInfo.Size() <= maxBytes {
		log.Printf("✅ Файл в пределах лимита")
		return []string{filePath}, nil
	}

	log.Printf("⚠️ Файл %.2f МБ > лимита. Создаём ZIP...", fileSizeMB)

	// Создаём ZIP
	zipPath := filePath + ".zip"
	if err := createZip(filePath, zipPath); err != nil {
		return nil, fmt.Errorf("ошибка создания ZIP: %v", err)
	}

	// Удаляем оригинал сразу после упаковки
	_ = os.Remove(filePath)

	zipInfo, err := os.Stat(zipPath)
	if err != nil {
		return nil, err
	}

	zipSizeMB := float64(zipInfo.Size()) / (1024 * 1024)
	log.Printf("📦 ZIP создан: %.2f МБ", zipSizeMB)

	// Если ZIP помещается — отдаём его
	if zipInfo.Size() <= maxBytes {
		log.Printf("✅ ZIP в пределах лимита")
		return []string{zipPath}, nil
	}

	// ZIP тоже большой — режем его на части
	log.Printf("⚠️ ZIP всё ещё большой (%.2f МБ). Режем на части...", zipSizeMB)
	parts, err := splitFile(zipPath, maxBytes)
	if err != nil {
		return nil, err
	}

	// Удаляем целый ZIP после разрезания
	_ = os.Remove(zipPath)

	return parts, nil
}

// createZip создаёт ZIP-архив с одним файлом внутри
func createZip(srcPath, zipPath string) error {
	zipFile, err := os.Create(zipPath)
	if err != nil {
		return err
	}
	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	info, err := src.Stat()
	if err != nil {
		return err
	}

	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}
	header.Name = filepath.Base(srcPath)
	header.Method = zip.Deflate

	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return err
	}

	_, err = io.Copy(writer, src)
	return err
}

// splitFile режет файл на части размером maxBytes
func splitFile(filePath string, maxBytes int64) ([]string, error) {
	src, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer src.Close()

	dir := filepath.Dir(filePath)
	baseName := filepath.Base(filePath)
	var parts []string
	partNum := 0
	buf := make([]byte, 32*1024*1024) // 32 МБ буфер

	for {
		toRead := maxBytes
		if toRead > int64(len(buf)) {
			toRead = int64(len(buf))
		}

		n, readErr := src.Read(buf[:toRead])
		if n > 0 {
			partPath := filepath.Join(dir, fmt.Sprintf("%s_part_%03d", baseName, partNum))
			if err := os.WriteFile(partPath, buf[:n], 0644); err != nil {
				// чистим уже созданные части
				for _, p := range parts {
					_ = os.Remove(p)
				}
				return nil, fmt.Errorf("ошибка записи части: %v", err)
			}
			parts = append(parts, partPath)
			partNum++
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			for _, p := range parts {
				_ = os.Remove(p)
			}
			return nil, readErr
		}
	}

	log.Printf("📦 Создано частей: %d", len(parts))
	return parts, nil
}

// ======================================================
// ВСПОМОГАТЕЛЬНАЯ ФУНКЦИЯ ВЫЗОВА YT-DLP
// ======================================================

func runYtDlp(ytDlpPath string, args []string) error {
	fmt.Println("==================================================")
	fmt.Println("Аргументы yt-dlp:")
	fmt.Println(strings.Join(args, " "))
	fmt.Println("==================================================")

	cmd := exec.Command(ytDlpPath, args...)

	output, err := cmd.CombinedOutput()
	text := string(output)

	fmt.Println(text)

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf(
				"код завершения %d:\n%s",
				exitErr.ExitCode(),
				text,
			)
		}
		return fmt.Errorf("%v:\n%s", err, text)
	}

	return nil
}

// ======================================================
// НАСТРОЙКА КУКИ (COOKIES)
// ======================================================

func buildCookiesArgs(exeDir string) []string {
	pathTikTok := filepath.Join(exeDir, "tiktok_cookies.txt")
	pathGeneric := filepath.Join(exeDir, "cookies.txt")

	if _, err := os.Stat(pathTikTok); err == nil {
		fmt.Println("🍪 Используется файл tiktok_cookies.txt")
		return []string{
			"--cookies",
			pathTikTok,
		}
	}

	if _, err := os.Stat(pathGeneric); err == nil {
		fmt.Println("🍪 Используется файл cookies.txt")
		return []string{
			"--cookies",
			pathGeneric,
		}
	}

	fmt.Println("⚠️ Файлы куки (cookies) не найдены")
	return []string{}
}

// ======================================================
// ВСПОМОГАТЕЛЬНЫЕ ФУНКЦИИ
// ======================================================

func cleanURL(raw string) string {
	u := strings.TrimSpace(raw)
	if strings.Contains(u, "?") {
		parts := strings.Split(u, "?")
		if len(parts) > 0 {
			u = parts[0]
		}
	}
	return u
}

func isYouTubeURL(u string) bool {
	u = strings.ToLower(u)
	return strings.Contains(u, "youtube.com") || strings.Contains(u, "youtu.be")
}

func extractTikTokProfileFromText(text string) string {
	marker := "https://www.tiktok.com/@"
	idx := strings.Index(text, marker)
	if idx == -1 {
		return ""
	}
	part := text[idx:]
	end := strings.IndexAny(part, " \n\r\t")
	if end != -1 {
		part = part[:end]
	}
	if q := strings.Index(part, "?"); q != -1 {
		part = part[:q]
	}
	playlistIdx := strings.Index(part, "/playlist/")
	if playlistIdx != -1 {
		part = part[:playlistIdx]
	}
	return strings.TrimSpace(part)
}

func extractSecUserID(raw string) string {
	marker := "sec_user_id="
	idx := strings.Index(raw, marker)
	if idx == -1 {
		return ""
	}
	part := raw[idx+len(marker):]
	end := strings.IndexAny(part, "& \n\r\t")
	if end != -1 {
		part = part[:end]
	}
	part = strings.ReplaceAll(part, "%2B", "+")
	part = strings.ReplaceAll(part, "%2F", "/")
	part = strings.ReplaceAll(part, "%3D", "=")
	return strings.TrimSpace(part)
}

// ======================================================
// РАБОТА С ФАЙЛАМИ
// ======================================================

func collectFiles(dir string) ([]string, error) {
	var result []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		fullPath := filepath.Join(dir, entry.Name())
		result = append(result, fullPath)
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("файлы не найдены")
	}
	return result, nil
}

func DeleteFiles(files []string) {
	for _, file := range files {
		err := os.Remove(file)
		if err != nil {
			fmt.Println("Ошибка удаления файла:", err)
		}
	}
}
