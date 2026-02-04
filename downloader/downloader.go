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
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	downloadsDir := filepath.Join(exeDir, "downloads")
	_ = os.MkdirAll(downloadsDir, 0755)

	ytDlpName := "yt-dlp"
	if runtime.GOOS == "windows" {
		ytDlpName = "yt-dlp.exe"
	}
	ytDlpPath := filepath.Join(exeDir, ytDlpName)

	// === НАСТРОЙКА ===
	
	// 1. ВСТАВЬТЕ СЮДА ВАШ НАСТОЯЩИЙ USER-AGENT ИЗ БРАУЗЕРА
	// Если куки от Chrome 132, а тут написано 120 - работать НЕ БУДЕТ.
	userAgent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36"

	// 2. COOKIES
	cookiesArgs := []string{}
	pathTikTok := filepath.Join(exeDir, "tiktok_cookies.txt")
	pathGeneric := filepath.Join(exeDir, "cookies.txt")

	if _, err := os.Stat(pathTikTok); err == nil {
		fmt.Println("🍪 Используем tiktok_cookies.txt")
		cookiesArgs = []string{"--cookies", pathTikTok}
	} else if _, err := os.Stat(pathGeneric); err == nil {
		fmt.Println("🍪 Используем cookies.txt")
		cookiesArgs = []string{"--cookies", pathGeneric}
	} else {
		// Если файла нет, пробуем взять из системы (работает только если Chrome закрыт)
		fmt.Println("⚠️ Файл cookies не найден, пробуем --cookies-from-browser chrome")
		cookiesArgs = []string{"--cookies-from-browser", "chrome"}
	}

	// === ПОЛУЧЕНИЕ ДАННЫХ ===
	fmt.Println("🔄 Анализ ссылки...")

	// Добавляем Referer, чтобы имитировать переход с главной страницы TikTok
	commonArgs := []string{
		"--user-agent", userAgent,
		"--referer", "https://www.tiktok.com/",
		"--add-header", "Accept-Language:en-US,en;q=0.9",
		"--no-check-certificates",
	}
	commonArgs = append(commonArgs, cookiesArgs...)

	// Получаем ID и URL
	metaArgs := append([]string{"--print", "%(id)s", "--print", "%(webpage_url)s"}, commonArgs...)
	metaArgs = append(metaArgs, url)

	cmd := exec.Command(ytDlpPath, metaArgs...)
	output, err := cmd.Output()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			// Если ошибка, пробуем очистить кеш и выйти
			_ = exec.Command(ytDlpPath, "--rm-cache-dir").Run()
			return "", "", fmt.Errorf("Ошибка TikTok (код %d):\n%s\n👉 Попробуйте обновить User-Agent в коде.", exitErr.ExitCode(), string(exitErr.Stderr))
		}
		return "", "", err
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return "", "", fmt.Errorf("пустой ответ от yt-dlp")
	}
	videoID := strings.TrimSpace(lines[0])
	finalURL := url
	if len(lines) > 1 {
		finalURL = strings.TrimSpace(lines[1])
	}
	fmt.Printf("✅ ID: %s\n", videoID)

	// === СКАЧИВАНИЕ ===
	
	prefix := "tiktok"
	if strings.Contains(url, "youtu") { prefix = "youtube" }
	
	videoOut := filepath.Join(downloadsDir, fmt.Sprintf("%s_%s.%%(ext)s", prefix, videoID))
	audioOut := filepath.Join(downloadsDir, fmt.Sprintf("%s_%s_audio.mp3", prefix, videoID))

	// Если YouTube -> только аудио
	if prefix == "youtube" {
		args := append([]string{"-x", "--audio-format", "mp3", "-o", audioOut}, commonArgs...)
		args = append(args, finalURL)
		if err := exec.Command(ytDlpPath, args...).Run(); err != nil { return "", "", err }
		return "", audioOut, nil
	}

	// Скачивание ВИДЕО
	videoArgs := append([]string{"-f", "bestvideo+bestaudio/best", "-o", videoOut}, commonArgs...)
	videoArgs = append(videoArgs, finalURL)
	
	fmt.Println("⬇️ Скачивание видео...")
	if err := exec.Command(ytDlpPath, videoArgs...).Run(); err != nil {
		return "", "", fmt.Errorf("сбой скачивания: %v", err)
	}

	// Проверка результата
	matches, _ := filepath.Glob(filepath.Join(downloadsDir, fmt.Sprintf("%s_%s.*", prefix, videoID)))
	if len(matches) > 0 {
		videoPath = matches[0]
	} else {
		return "", "", fmt.Errorf("файл не записался на диск")
	}

	// Скачивание АУДИО
	audioArgs := append([]string{"-x", "--audio-format", "mp3", "--audio-quality", "0", "-o", audioOut}, commonArgs...)
	audioArgs = append(audioArgs, finalURL)
	
	fmt.Println("🎵 Извлечение аудио...")
	_ = exec.Command(ytDlpPath, audioArgs...).Run()
	audioPath = audioOut

	return videoPath, audioPath, nil
}