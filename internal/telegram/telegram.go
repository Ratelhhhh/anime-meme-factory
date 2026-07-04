// Package telegram — отправка картинок и сообщений в канал через Bot API.
package telegram

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type apiResponse struct {
	OK          bool            `json:"ok"`
	Description string          `json:"description"`
	Result      json.RawMessage `json:"result"`
}

func apiURL(token, method string) string {
	return "https://api.telegram.org/bot" + token + "/" + method
}

var httpClient = &http.Client{Timeout: 120 * time.Second}

func do(req *http.Request) (json.RawMessage, error) {
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var r apiResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("некорректный ответ Telegram: %s", strings.TrimSpace(string(body)))
	}
	if !r.OK {
		return nil, fmt.Errorf("telegram: %s", r.Description)
	}
	return r.Result, nil
}

// Me — данные бота из getMe.
type Me struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}

func GetMe(token string) (Me, error) {
	var me Me
	req, _ := http.NewRequest(http.MethodGet, apiURL(token, "getMe"), nil)
	res, err := do(req)
	if err != nil {
		return me, err
	}
	return me, json.Unmarshal(res, &me)
}

// SendMessage отправляет текстовое сообщение (для проверки связи).
func SendMessage(token, chatID, text string) error {
	form := url.Values{"chat_id": {chatID}, "text": {text}}
	req, _ := http.NewRequest(http.MethodPost, apiURL(token, "sendMessage"),
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, err := do(req)
	return err
}

// SendPhoto загружает локальный файл как фото в канал. Возвращает message_id.
func SendPhoto(token, chatID, filePath, caption string) (int64, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return 0, err
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("chat_id", chatID)
	if caption != "" {
		_ = w.WriteField("caption", caption)
	}
	fw, err := w.CreateFormFile("photo", filepath.Base(filePath))
	if err != nil {
		return 0, err
	}
	if _, err := fw.Write(content); err != nil {
		return 0, err
	}
	if err := w.Close(); err != nil {
		return 0, err
	}

	req, _ := http.NewRequest(http.MethodPost, apiURL(token, "sendPhoto"), &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	res, err := do(req)
	if err != nil {
		return 0, err
	}
	var out struct {
		MessageID int64 `json:"message_id"`
	}
	return out.MessageID, json.Unmarshal(res, &out)
}
