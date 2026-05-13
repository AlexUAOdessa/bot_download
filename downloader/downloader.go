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

type Downloader struct{}

func NewDownloader() *Downloader {
	return &Downloader{}
}

func (d *Downloader) Download(userID int64, rawURL string) ([]string, error) {

	exePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("ошибка получения пути exe: %v", err)
	}

	exeDir := filepath.Dir(exePath)

	// ======================================================
	// UNIQUE DOWNLOAD FOLDER
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
	// YT-DLP
	// ======================================================

	ytDlpName := "yt-dlp"

	if runtime.GOOS == "windows" {
		ytDlpName = "yt-dlp.exe"
	}

	ytDlpPath := filepath.Join(exeDir, ytDlpName)

	if _, err := os.Stat(ytDlpPath); err != nil {
		return nil, fmt.Errorf("yt-dlp не найден: %s", ytDlpPath)
	}

	// ======================================================
	// URL + USER ID
	// ======================================================

	url := cleanURL(rawURL)

	secUserID := extractSecUserID(rawURL)

	// ======================================================
	// USER AGENT
	// ======================================================

	userAgent := "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 Safari/537.36"

	// ======================================================
	// COMMON ARGS
	// ======================================================

    commonArgs := []string{
    	"--user-agent", userAgent,
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

	// ======================================================
	// YOUTUBE
	// ======================================================

	if isYouTubeURL(url) {

		fmt.Println("⬇️ YouTube download...")

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
	// TIKTOK + GENERIC
	// ======================================================

	fmt.Println("⬇️ TikTok / Generic download...")

	outputTemplate := filepath.Join(
		userDir,
		"video_%(playlist_index)s_%(id)s.%(ext)s",
	)

	args := []string{
		"-f", "bestvideo+bestaudio/best",
		"--merge-output-format", "mp4",
		"-o", outputTemplate,
	}

	args = append(args, commonArgs...)
	args = append(args, url)

	err = runYtDlp(ytDlpPath, args)

	// ======================================================
	// FALLBACK
	// ======================================================

	if err != nil {

		errText := err.Error()

		if strings.Contains(errText, "/playlist/") ||
			strings.Contains(errText, "Unsupported URL") {

			profileURL := extractTikTokProfileFromText(errText)

			if profileURL != "" {

				fmt.Println("⚠️ TikTok playlist не поддерживается")
				fmt.Println("➡️ fallback profile:", profileURL)

				profileArgs := []string{
					"-f", "bestvideo+bestaudio/best",
					"--merge-output-format", "mp4",
					"-o", outputTemplate,
				}

				profileArgs = append(profileArgs, commonArgs...)
				profileArgs = append(profileArgs, profileURL)

				err2 := runYtDlp(
					ytDlpPath,
					profileArgs,
				)

				// ==============================================
				// SECOND FALLBACK
				// ==============================================

				if err2 != nil {

					if secUserID == "" {
						secUserID = extractSecUserID(errText)
					}

					if secUserID == "" {
						secUserID = extractSecUserID(err2.Error())
					}

					if secUserID != "" {

						fmt.Println("⚠️ profile fallback failed")
						fmt.Println("➡️ fallback tiktokuser:", secUserID)

						userIDArgs := []string{
							"-f", "bestvideo+bestaudio/best",
							"--merge-output-format", "mp4",
							"-o", outputTemplate,
						}

						userIDArgs = append(userIDArgs, commonArgs...)
						userIDArgs = append(userIDArgs, "tiktokuser:"+secUserID)

						err3 := runYtDlp(
							ytDlpPath,
							userIDArgs,
						)

						if err3 != nil {
							return nil, fmt.Errorf(
								"ошибка yt-dlp fallback tiktokuser:\n%v",
								err3,
							)
						}

					} else {

						return nil, fmt.Errorf(
							"ошибка yt-dlp fallback:\n%v",
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
// RUN YT-DLP
// ======================================================

func runYtDlp(ytDlpPath string, args []string) error {

	fmt.Println("==================================================")
	fmt.Println("yt-dlp args:")
	fmt.Println(strings.Join(args, " "))
	fmt.Println("==================================================")

	cmd := exec.Command(ytDlpPath, args...)

	output, err := cmd.CombinedOutput()

	text := string(output)

	fmt.Println(text)

	if err != nil {

		if exitErr, ok := err.(*exec.ExitError); ok {

			return fmt.Errorf(
				"код %d:\n%s",
				exitErr.ExitCode(),
				text,
			)
		}

		return fmt.Errorf("%v:\n%s", err, text)
	}

	return nil
}

// ======================================================
// COOKIES
// ======================================================

func buildCookiesArgs(exeDir string) []string {

	pathTikTok := filepath.Join(exeDir, "tiktok_cookies.txt")
	pathGeneric := filepath.Join(exeDir, "cookies.txt")

	if _, err := os.Stat(pathTikTok); err == nil {

		fmt.Println("🍪 Используем tiktok_cookies.txt")

		return []string{
			"--cookies",
			pathTikTok,
		}
	}

	if _, err := os.Stat(pathGeneric); err == nil {

		fmt.Println("🍪 Используем cookies.txt")

		return []string{
			"--cookies",
			pathGeneric,
		}
	}

	fmt.Println("⚠️ cookies не найдены")

	return []string{}
}

// ======================================================
// HELPERS
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

	return strings.Contains(u, "youtube.com") ||
		strings.Contains(u, "youtu.be")
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
// FILES
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

// ======================================================
// DELETE FILES
// ======================================================

func DeleteFiles(files []string) {

	for _, file := range files {

		err := os.Remove(file)

		if err != nil {
			fmt.Println("Ошибка удаления:", err)
		}
	}
}