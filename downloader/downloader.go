package downloader

import (
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
	exePath, err := os.Executable()
	if err != nil {
		return "", "", fmt.Errorf("не удалось определить путь к исполняемому файлу: %v", err)
	}
	exeDir := filepath.Dir(exePath)
	downloadsDir := filepath.Join(exeDir, "downloads")
	if err := os.MkdirAll(downloadsDir, 0755); err != nil {
		return "", "", fmt.Errorf("не удалось создать папку downloads: %v", err)
	}

	ytDlpName := "yt-dlp"
	if runtime.GOOS == "windows" {
		ytDlpName = "yt-dlp.exe"
	}
	ytDlpPath := filepath.Join(exeDir, ytDlpName)

	if _, err := os.Stat(ytDlpPath); os.IsNotExist(err) {
		return "", "", fmt.Errorf("файл %s не найден в директории %s", ytDlpName, exeDir)
	}

	// Определяем тип ссылки
	isYouTube := strings.Contains(url, "youtube.com") || strings.Contains(url, "youtu.be")
	isTikTok := strings.Contains(url, "tiktok.com") || strings.Contains(url, "vm.tiktok.com") || strings.Contains(url, "vt.tiktok.com")

	if !isYouTube && !isTikTok {
		return "", "", fmt.Errorf("неподдерживаемый URL: %s. Поддерживаются только YouTube и TikTok", url)
	}

	// Если это TikTok, проверяем, укороченная ли ссылка
	originalURL := url
	var fullURL string
	if isTikTok && (strings.Contains(url, "vm.tiktok.com") || strings.Contains(url, "vt.tiktok.com")) {
		fullURL, err = d.getFullTikTokURL(url, ytDlpPath)
		if err != nil {
			return "", "", fmt.Errorf("не удалось получить полный URL TikTok: %v", err)
		}
	} else {
		fullURL = url
	}

	// Извлекаем ID видео
	videoID := extractVideoID(originalURL, isYouTube, ytDlpPath)
	if videoID == "" {
		return "", "", fmt.Errorf("не удалось извлечь ID видео: %s", originalURL)
	}

	// Пользовательский User-Agent
	userAgent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
	// Путь к файлу cookies
	cookiesPath := filepath.Join(exeDir, "tiktok_cookies.txt")
	cookiesArgs := []string{}
	if _, err := os.Stat(cookiesPath); err == nil {
		cookiesArgs = []string{"--cookies", cookiesPath}
	} else {
		fmt.Printf("Файл cookies не найден: %s. Это может вызвать ошибку 403 для TikTok.\n", cookiesPath)
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
		args := append([]string{"-x", "--audio-format", "mp3", "--audio-quality", "0", "--user-agent", userAgent, "-o", audioPath}, cookiesArgs...)
		args = append(args, url)
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
	altURL := fmt.Sprintf("https://www.tiktok.com/@unknown/video/%s", videoID)
	args := append([]string{"-f", "bestvideo+bestaudio/best", "--user-agent", userAgent, "-o", videoOutputTemplate}, cookiesArgs...)
	args = append(args, tikTokArgs...)
	args = append(args, altURL)
	cmd := exec.Command(ytDlpPath, args...)
	cmd.Stderr = os.Stderr
	fmt.Printf("Выполняется команда для TikTok (видео): %v\n", cmd.Args)
	if err := cmd.Run(); err != nil {
		// Пробуем прямой URL, если альтернативный не сработал
		args = append([]string{"-f", "bestvideo+bestaudio/best", "--user-agent", userAgent, "-o", videoOutputTemplate}, cookiesArgs...)
		args = append(args, tikTokArgs...)
		args = append(args, fullURL)
		cmd = exec.Command(ytDlpPath, args...)
		cmd.Stderr = os.Stderr
		fmt.Printf("Выполняется команда для TikTok (прямой URL, видео): %v\n", cmd.Args)
		if err := cmd.Run(); err != nil {
			return "", "", fmt.Errorf("ошибка скачивания видео: %v", err)
		}
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
	args = append(args, altURL)
	cmd = exec.Command(ytDlpPath, args...)
	cmd.Stderr = os.Stderr
	fmt.Printf("Выполняется команда для TikTok (аудио): %v\n", cmd.Args)
	if err := cmd.Run(); err != nil {
		return "", "", fmt.Errorf("ошибка скачивания аудио: %v", err)
	}

	return videoPath, audioPath, nil
}

// getFullTikTokURL получает полный URL TikTok из укороченной ссылки
func (d *Downloader) getFullTikTokURL(shortURL, ytDlpPath string) (string, error) {
	userAgent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36"
	exeDir := filepath.Dir(ytDlpPath)
	cookiesPath := filepath.Join(exeDir, "tiktok_cookies.txt")
	cookiesArgs := []string{}
	if _, err := os.Stat(cookiesPath); err == nil {
		cookiesArgs = []string{"--cookies", cookiesPath}
	} else {
		fmt.Printf("Файл cookies не найден: %s. Это может вызвать ошибку 403 для TikTok.\n", cookiesPath)
	}

	tikTokArgs := []string{
		"--add-header", "Referer:https://www.tiktok.com/",
		"--add-header", "Origin:https://www.tiktok.com",
		"--extractor-args", "tiktok:sys_region=US",
		"--no-check-certificates",
	}

	args := append([]string{"--get-url", "--user-agent", userAgent}, cookiesArgs...)
	args = append(args, tikTokArgs...)
	args = append(args, shortURL)
	cmd := exec.Command(ytDlpPath, args...)
	cmd.Stderr = os.Stderr
	fmt.Printf("Выполняется команда для получения URL TikTok: %v\n", cmd.Args)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ошибка получения полного URL: %v", err)
	}
	fullURL := strings.TrimSpace(string(output))
	if !strings.Contains(fullURL, "tiktok.com") {
		return "", fmt.Errorf("полученный URL не является TikTok URL: %s", fullURL)
	}
	fmt.Printf("Полный URL TikTok: %s\n", fullURL)
	return fullURL, nil
}

// extractVideoID извлекает ID видео из URL YouTube или TikTok
func extractVideoID(url string, isYouTube bool, ytDlpPath string) string {
	if isYouTube {
		if strings.Contains(url, "youtube.com/watch?v=") {
			parts := strings.Split(url, "v=")
			if len(parts) > 1 {
				return strings.Split(parts[1], "&")[0]
			}
		} else if strings.Contains(url, "youtu.be/") {
			parts := strings.Split(url, "youtu.be/")
			if len(parts) > 1 {
				return strings.Split(parts[1], "?")[0]
			}
		}
	} else {
		// Для TikTok извлекаем ID из www.tiktok.com
		if strings.Contains(url, "www.tiktok.com") {
			parts := strings.Split(url, "/")
			for i, part := range parts {
				if part == "video" && i+1 < len(parts) {
					return parts[i+1]
				}
			}
		}
		// Для укороченных или прямых URL TikTok
		args := []string{"--get-id", "--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36", "--no-check-certificates"}
		exeDir := filepath.Dir(ytDlpPath)
		cookiesPath := filepath.Join(exeDir, "tiktok_cookies.txt")
		if _, err := os.Stat(cookiesPath); err == nil {
			args = append(args, "--cookies", cookiesPath)
		}
		args = append(args, url)
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
	fmt.Printf("Не удалось извлечь ID из URL: %s\n", url)
	return ""
}