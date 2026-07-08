// Package pikabu парсит посты автора на Пикабу и картинки внутри поста.
package pikabu

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/120.0 Safari/537.36"

// authCookie — Cookie залогиненной сессии Пикабу (для 18+ контента за возрастным
// гейтом). Пусто = ходим анонимно.
var authCookie string

// SetCookie задаёт Cookie, который будет слаться со всеми запросами к Пикабу.
func SetCookie(c string) { authCookie = c }

var (
	// Ссылки на посты в профиле автора.
	storyRe = regexp.MustCompile(`https://pikabu\.ru/story/[a-z0-9_]+`)
	// Полноразмерные картинки поста лежат в data-large-image="...".
	largeImgRe = regexp.MustCompile(`data-large-image="(https://cs\d+\.pikabu\.ru/[^"]+)"`)
	// Рейтинг поста (плюсы минус минусы) — в атрибуте data-rating.
	ratingRe = regexp.MustCompile(`data-rating="(-?\d+)"`)
	// Текстовые блоки поста (для оценки объёма текста).
	textBlockRe = regexp.MustCompile(`(?s)story-block story-block_type_text">(.*?)</div>`)
	tagRe       = regexp.MustCompile(`<[^>]+>`)
)

func fetch(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "ru,en;q=0.9")
	if authCookie != "" {
		req.Header.Set("Cookie", authCookie)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	// Страница в windows-1251, но нужные нам URL — ASCII, так что читаем как есть.
	return io.ReadAll(resp.Body)
}

// sourceURL превращает источник в URL списка постов. Источник — либо ник автора
// ("BelarusPatriot" → страница профиля), либо готовый URL (страница сообщества,
// поиск с фильтрами и т.п.) — тогда берётся как есть.
func sourceURL(source string) string {
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		return source
	}
	return "https://pikabu.ru/@" + source
}

// ListPosts возвращает URL постов из источника (профиль автора или страница сообщества).
func ListPosts(source, prefix string, limit int) ([]string, error) {
	body, err := fetch(sourceURL(source))
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range storyRe.FindAll(body, -1) {
		url := string(m)
		if seen[url] {
			continue
		}
		seen[url] = true
		slug := url[len("https://pikabu.ru/story/"):]
		if prefix != "" && !hasPrefix(slug, prefix) {
			continue
		}
		out = append(out, url)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// Info — разобранный пост: картинки, рейтинг и объём текста.
type Info struct {
	Images  []string
	Rating  int
	TextLen int // примерная длина текста поста в символах
}

// PostInfo скачивает пост один раз и возвращает его картинки, рейтинг и длину текста.
func PostInfo(postURL string) (Info, error) {
	body, err := fetch(postURL)
	if err != nil {
		return Info{}, err
	}
	return Info{
		Images:  extractImages(body),
		Rating:  extractRating(body),
		TextLen: extractTextLen(body),
	}, nil
}

// extractTextLen суммирует длину текстовых блоков поста (теги убраны).
// Страница в cp1251, где кириллица — 1 байт на символ, так что длина в байтах
// очищенного текста ≈ число символов.
func extractTextLen(body []byte) int {
	total := 0
	for _, m := range textBlockRe.FindAllSubmatch(body, -1) {
		clean := tagRe.ReplaceAll(m[1], nil)
		total += len(strings.TrimSpace(string(clean)))
	}
	return total
}

// PostImages возвращает URL полноразмерных картинок поста (без дублей, по порядку).
func PostImages(postURL string) ([]string, error) {
	info, err := PostInfo(postURL)
	return info.Images, err
}

func extractImages(body []byte) []string {
	seen := map[string]bool{}
	var out []string
	for _, m := range largeImgRe.FindAllSubmatch(body, -1) {
		url := string(m[1])
		if seen[url] {
			continue
		}
		seen[url] = true
		out = append(out, url)
	}
	return out
}

// extractRating возвращает рейтинг поста (0, если не найден).
func extractRating(body []byte) int {
	m := ratingRe.FindSubmatch(body)
	if m == nil {
		return 0
	}
	n, err := strconv.Atoi(string(m[1]))
	if err != nil {
		return 0
	}
	return n
}

// Download скачивает картинку на диск. Возвращает размер в байтах.
func Download(url, dest string) (int, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", "https://pikabu.ru/")
	if authCookie != "" {
		req.Header.Set("Cookie", authCookie)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	if err := os.WriteFile(dest, data, 0o644); err != nil {
		return 0, err
	}
	return len(data), nil
}

// PoliteSleep — вежливая пауза между запросами.
func PoliteSleep(seconds float64) {
	if seconds > 0 {
		time.Sleep(time.Duration(seconds * float64(time.Second)))
	}
}

func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
