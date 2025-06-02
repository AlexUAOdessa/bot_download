package downloader

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type Downloader struct{}

func NewDownloader() *Downloader {
	return &Downloader{}
}

func (d *Downloader) Download(url string) (videoPath, audioPath string, err error) {
	// Создаем папку downloads в текущей директории
	downloadsDir := filepath.Join(".", "downloads")
	if err := os.MkdirAll(downloadsDir, 0755); err != nil {
		return "", "", fmt.Errorf("не удалось создать папку downloads: %v", err)
	}

	// Проверка наличия yt-dlp
	ytDlpName := "yt-dlp"
	if runtime.GOOS == "windows" {
		ytDlpName = "yt-dlp.exe"
	}
	ytDlpPath, err := exec.LookPath(ytDlpName)
	if err != nil {
		return "", "", fmt.Errorf("не удалось найти %s в системе: %v", ytDlpName, err)
	}
	if _, err := os.Stat(ytDlpPath); os.IsNotExist(err) {
		return "", "", fmt.Errorf("файл %s не найден: %v", ytDlpName, err)
	}

	// Проверка наличия ffmpeg
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		return "", "", fmt.Errorf("ffmpeg не найден в системе, установите его: %v", err)
	}
	if _, err := os.Stat(ffmpegPath); os.IsNotExist(err) {
		return "", "", fmt.Errorf("файл ffmpeg не найден: %v", err)
	}

	// Очистка URL от параметров
	cleanURL := url
	if idx := strings.Index(url, "?"); idx != -1 {
		cleanURL = url[:idx]
	}

	// Определяем тип ссылки
	isYouTube := strings.Contains(cleanURL, "youtube.com") || strings.Contains(cleanURL, "youtu.be")
	isTikTok := strings.Contains(cleanURL, "tiktok.com") || strings.Contains(cleanURL, "vm.tiktok.com") || strings.Contains(cleanURL, "vt.tiktok.com")
	if !isYouTube && !isTikTok {
		return "", "", fmt.Errorf("неподдерживаемый URL: %s. Поддерживаются только YouTube и TikTok", cleanURL)
	}

	// Если это TikTok, проверяем, укороченная ли ссылка
	originalURL := cleanURL
	var fullURL string
	if isTikTok && (strings.Contains(cleanURL, "vm.tiktok.com") || strings.Contains(cleanURL, "vt.tiktok.com")) {
		fmt.Printf("Обработка укороченной TikTok-ссылки: %s\n", cleanURL)
		fullURL, err = d.getFullTikTokURL(cleanURL, ytDlpPath)
		if err != nil {
			return "", "", fmt.Errorf("не удалось получить полный URL TikTok: %v", err)
		}
	} else {
		fullURL = cleanURL
	}

	// Извлекаем ID видео
	videoID := extractVideoID(originalURL, isYouTube, ytDlpPath)
	if videoID == "" {
		return "", "", fmt.Errorf("не удалось извлечь ID видео: %s", originalURL)
	}

	// Пользовательский User-Agent
	userAgent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
	// Путь к файлу cookies для TikTok
	cookiesArgs := []string{}
	if isTikTok {
		cookiesPath := filepath.Join(".", "tiktok_cookies.txt")
		if _, err := os.Stat(cookiesPath); err == nil {
			cookiesArgs = append(cookiesArgs, "--cookies", cookiesPath)
		} else {
			fmt.Printf("Файл cookies не найден: %s. Продолжаем без cookies.\n", cookiesPath)
		}
	}

	// Дополнительные параметры для TikTok
	tikTokArgs := []string{
		"--add-header", "Referer:https://www.tiktok.com/",
		"--add-header", "Origin:https://www.tiktok.com",
		"--extractor-args", "tiktok:sys_region=US",
		"--no-check-certificates",
	}

	if isYouTube {
		// Для YouTube скачиваем только аудио
		audioPath = filepath.Join(downloadsDir, fmt.Sprintf("youtube_%s_audio.mp3", videoID))
		args := []string{"-x", "--audio-format", "mp3", "--audio-quality", "0", "--user-agent", userAgent, "-o", audioPath}
		args = append(args, cleanURL)
		cmd := exec.Command(ytDlpPath, args...)
		cmd.Stderr = os.Stderr
		fmt.Printf("Выполняется команда для YouTube: %v\n", cmd.Args)
		if err := cmd.Run(); err != nil {
			return "", "", fmt.Errorf("ошибка скачивания аудио: %v", err)
		}
		return "", audioPath, nil
	}

	// Для TikTok скачиваем видео и аудио
	videoOutputTemplate := filepath.Join(downloadsDir, fmt.Sprintf("tiktok_%s.%%(ext)s", videoID))
	args := append([]string{"-f", "bestvideo+bestaudio/best", "--user-agent", userAgent, "-o", videoOutputTemplate}, cookiesArgs...)
	args = append(args, tikTokArgs...)
	args = append(args, fullURL)
	cmd := exec.Command(ytDlpPath, args...)
	cmd.Stderr = os.Stderr
	fmt.Printf("Выполняется команда для TikTok (видео): %v\n", cmd.Args)
	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("ошибка скачивания видео: %v", err)
	}

	// Поиск скачанного видео
	matches, err := filepath.Glob(filepath.Join(downloadsDir, fmt.Sprintf("tiktok_%s.*", videoID)))
	if err != nil || len(matches) == 0 {
		return "", "", fmt.Errorf("не найден файл видео")
	}
	videoPath = matches[0]

	// Скачиваем аудио
	audioPath = filepath.Join(downloadsDir, fmt.Sprintf("tiktok_%s_audio.mp3", videoID))
	args = append([]string{"-x", "--audio-format", "mp3", "--audio-quality", "0", "--user-agent", userAgent, "-o", audioPath}, cookiesArgs...)
	args = append(args, tikTokArgs...)
	args = append(args, fullURL)
	cmd = exec.Command(ytDlpPath, args...)
	cmd.Stderr = os.Stderr
	fmt.Printf("Выполняется команда для TikTok (аудио): %v\n", cmd.Args)
	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("ошибка скачивания аудио: %v", err)
	}

	return videoPath, audioPath, nil
}

func (d *Downloader) getFullTikTokURL(shortURL, ytDlpPath string) (string, error) {
	userAgent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
	cookiesPath := filepath.Join(".", "tiktok_cookies.txt")
	cookiesArgs := []string{}
	if _, err := os.Stat(cookiesPath); err == nil {
		cookiesArgs = []string{"--cookies", cookiesPath}
	} else {
		fmt.Printf("Файл cookies не найден: %s. Продолжаем без cookies.\n", cookiesPath)
	}

	tikTokArgs := []string{
		"--add-header", "Referer:https://www.tiktok.com/",
		"--add-header", "Origin:https://www.tiktok.com",
		"--extractor-args", "tiktok:sys_region=US",
		"--no-check-certificates",
		"--dump-json",
	}

	cleanURL := shortURL
	if idx := strings.Index(shortURL, "?"); idx != -1 {
		cleanURL = shortURL[:idx]
	}

	args := append([]string{"--user-agent", userAgent}, cookiesArgs...)
	args = append(args, tikTokArgs...)
	args = append(args, cleanURL)
	cmd := exec.Command(ytDlpPath, args...)
	fmt.Printf("Выполняется команда для получения JSON TikTok: %v\n", cmd.Args)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("ошибка выполнения yt-dlp: %v, вывод: %s", err, string(output))
	}

	var result map[string]interface{}
	if err := json.Unmarshal(output, &result); err != nil {
		return "", fmt.Errorf("ошибка разбора JSON: %v", err)
	}

	webURL, ok := result["webpage_url"].(string)
	if !ok || !strings.Contains(webURL, "tiktok.com") {
		return "", fmt.Errorf("не удалось извлечь ссылку из JSON: %v", result)
	}
	fmt.Printf("Полный URL TikTok (из JSON): %s\n", webURL)
	return webURL, nil
}

func extractVideoID(url string, isYouTube bool, ytDlpPath string) string {
	// Очистка URL от параметров
	cleanURL := url
	if idx := strings.Index(url, "?"); idx != -1 {
		cleanURL = url[:idx]
	}

	if isYouTube {
		if strings.Contains(cleanURL, "youtube.com/watch?v=") {
			parts := strings.Split(cleanURL, "v=")
			if len(parts) > 1 {
				return strings.Split(parts[1], "&")[0]
			}
		} else if strings.Contains(cleanURL, "youtu.be/") {
			parts := strings.Split(cleanURL, "youtu.be/")
			if len(parts) > 1 {
				return strings.Split(parts[1], "?")[0]
			}
		}
	} else {
		// Для TikTok извлекаем ID из www.tiktok.com
		if strings.Contains(cleanURL, "www.tiktok.com") {
			parts := strings.Split(cleanURL, "/")
			for i, part := range parts {
				if part == "video" && i+1 < len(parts) {
					return parts[i+1]
				}
			}
		}
		// Для укороченных или прямых URL TikTok
		cookiesPath := filepath.Join(".", "tiktok_cookies.txt")
		args := []string{"--get-id", "--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36", "--no-check-certificates"}
		if _, err := os.Stat(cookiesPath); err == nil {
			args = append(args, "--cookies", cookiesPath)
		}
		args = append(args, cleanURL)
		cmd := exec.Command(ytDlpPath, args...)
		cmd.Stderr = os.Stderr
		fmt.Printf("Попытка извлечь ID через yt-dlp: %v\n", cmd.Args)
		output, err := cmd.Output()
		if err == nil {
			videoID := strings.TrimSpace(string(output))
			if videoID != "" && len(videoID) > 10 { // Проверяем, что ID выглядит как числовой
				fmt.Printf("Извлечённый videoID: %s\n", videoID)
				return videoID
			}
		}
		fmt.Printf("Не удалось извлечь ID через yt-dlp: %v\n", err)
	}
	fmt.Printf("Не удалось извлечь ID из URL: %s\n", cleanURL)
	return ""
}