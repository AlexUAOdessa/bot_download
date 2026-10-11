package downloader

import (
	"fmt"
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

// Download выполняет скачивание видео или аудио по переданному URL.
// Возвращает слайс путей к скачанным файлам во временной директории или ошибку.
func (d *Downloader) Download(userID int64, rawURL string) ([]string, error) {

	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("ошибка получения пути exe: %v", err)
	}

	exeDir := filepath.Dir(exePath)

	// Создание уникальной папки для скачивания пользователя
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

	ytDlpName := "yt-dlp"
	if runtime.GOOS == "windows" {
		ytDlpName = "yt-dlp.exe"
	}

	ytDlpPath := filepath.Join(exeDir, ytDlpName)
	if _, err := os.Stat(ytDlpPath); err != nil {
		pathFromEnv, lookErr := exec.LookPath(ytDlpName)
		if lookErr != nil {
			return nil, fmt.Errorf("yt-dlp не найден ни в директории бота (%s), ни в системном PATH: %v", ytDlpPath, lookErr)
		}
		ytDlpPath = pathFromEnv
	}

	url := cleanURL(rawURL)
	secUserID := extractSecUserID(rawURL)

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
	}

	commonArgs = append(commonArgs, buildCookiesArgs(exeDir)...)

	// Скачивание с YouTube (извлечение аудио MP3)
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

	// Скачивание с TikTok / других сервисов (видео до 720p)
	fmt.Println("⬇️ Загрузка TikTok / Обычная загрузка...")

	outputTemplate := filepath.Join(
		userDir,
		"video_%(playlist_index)s_%(id)s.%(ext)s",
	)

	args := []string{
		"-f", "bv*[height<=720]+ba/b[height<=720]/best[height<=720]/best",
		"--merge-output-format", "mp4",
		"-o", outputTemplate,
	}

	args = append(args, commonArgs...)
	args = append(args, url)

	err = runYtDlp(ytDlpPath, args)

	if err != nil {
		errText := err.Error()

		if strings.Contains(errText, "/playlist/") || strings.Contains(errText, "Unsupported URL") {
			profileURL := extractTikTokProfileFromText(errText)

			if profileURL != "" {
				fmt.Println("⚠️ Плейлист TikTok не поддерживается напрямую, пробуем профиль...")

				profileArgs := []string{
					"-f", "bv*[height<=720]+ba/b[height<=720]/best[height<=720]/best",
					"--merge-output-format", "mp4",
					"-o", outputTemplate,
				}

				profileArgs = append(profileArgs, commonArgs...)
				profileArgs = append(profileArgs, profileURL)

				err2 := runYtDlp(ytDlpPath, profileArgs)

				if err2 != nil {
					if secUserID == "" {
						secUserID = extractSecUserID(errText)
					}
					if secUserID == "" {
						secUserID = extractSecUserID(err2.Error())
					}

					if secUserID != "" {
						fmt.Println("➡️ Резервный вариант загрузки (tiktokuser)...")

						userIDArgs := []string{
							"-f", "bv*[height<=720]+ba/b[height<=720]/best[height<=720]/best",
							"--merge-output-format", "mp4",
							"-o", outputTemplate,
						}

						userIDArgs = append(userIDArgs, commonArgs...)
						userIDArgs = append(userIDArgs, "tiktokuser:"+secUserID)

						err3 := runYtDlp(ytDlpPath, userIDArgs)
						if err3 != nil {
							return nil, fmt.Errorf("ошибка резервной загрузки по tiktokuser:\n%v", err3)
						}
					} else {
						return nil, fmt.Errorf("ошибка резервной загрузки профиля:\n%v", err2)
					}
				}
			} else {
				return nil, fmt.Errorf("ошибка yt-dlp:\n%v", err)
			}
		} else {
			return nil, fmt.Errorf("ошибка yt-dlp:\n%v", err)
		}
	}

	return collectFiles(userDir)
}

func runYtDlp(ytDlpPath string, args []string) error {
	cmd := exec.Command(ytDlpPath, args...)
	output, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(output))

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return fmt.Errorf("код завершения %d:\n%s", exitErr.ExitCode(), text)
		}
		return fmt.Errorf("%v:\n%s", err, text)
	}

	return nil
}

func buildCookiesArgs(exeDir string) []string {
	pathTikTok := filepath.Join(exeDir, "tiktok_cookies.txt")
	pathGeneric := filepath.Join(exeDir, "cookies.txt")

	if _, err := os.Stat(pathTikTok); err == nil {
		return []string{"--cookies", pathTikTok}
	}
	if _, err := os.Stat(pathGeneric); err == nil {
		return []string{"--cookies", pathGeneric}
	}
	return []string{}
}

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
