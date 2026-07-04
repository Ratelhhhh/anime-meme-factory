// Package pikabu парсит посты автора на Пикабу и картинки внутри поста.
package pikabu

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"time"
)

const userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/120.0 Safari/537.36"

var (
	// Ссылки на посты в профиле автора.
	storyRe = regexp.MustCompile(`https://pikabu\.ru/story/[a-z0-9_]+`)
	// Полноразмерные картинки поста лежат в data-large-image="...".
	largeImgRe = regexp.MustCompile(`data-large-image="(https://cs\d+\.pikabu\.ru/[^"]+)"`)
)

func fetch(url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	req.Header.Set("Accept-Language", "ru,en;q=0.9")

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

// ListPosts возвращает URL постов автора (в порядке появления на странице профиля).
func ListPosts(sourceUser, prefix string, limit int) ([]string, error) {
	body, err := fetch("https://pikabu.ru/@" + sourceUser)
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

// PostImages возвращает URL полноразмерных картинок поста (без дублей, по порядку).
func PostImages(postURL string) ([]string, error) {
	body, err := fetch(postURL)
	if err != nil {
		return nil, err
	}
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
	return out, nil
}

// Download скачивает картинку на диск. Возвращает размер в байтах.
func Download(url, dest string) (int, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", "https://pikabu.ru/")

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
