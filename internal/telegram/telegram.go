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
	"strconv"
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
// parseMode: "" | "HTML" | "MarkdownV2" — как трактовать разметку в подписи.
func SendPhoto(token, chatID, filePath, caption, parseMode string) (int64, error) {
	return SendPhotoWithButtons(token, chatID, filePath, caption, parseMode, nil)
}

// InlineButton — одна кнопка inline-клавиатуры под сообщением.
type InlineButton struct {
	Text string `json:"text"`
	Data string `json:"callback_data"`
}

// SendPhotoWithButtons — как SendPhoto, но с inline-кнопками (для модерации).
// rows — строки кнопок; nil = без кнопок.
func SendPhotoWithButtons(token, chatID, filePath, caption, parseMode string, rows [][]InlineButton) (int64, error) {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return 0, err
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("chat_id", chatID)
	if caption != "" {
		_ = w.WriteField("caption", caption)
		if parseMode != "" {
			_ = w.WriteField("parse_mode", parseMode)
		}
	}
	if len(rows) > 0 {
		markup, _ := json.Marshal(map[string]any{"inline_keyboard": rows})
		_ = w.WriteField("reply_markup", string(markup))
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

// --- Приём нажатий кнопок (модерация через бота) ---

// Update — одно обновление из getUpdates.
type Update struct {
	UpdateID      int64          `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

type Message struct {
	MessageID int64  `json:"message_id"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
}

type Chat struct {
	ID       int64  `json:"id"`
	Type     string `json:"type"`
	Title    string `json:"title"`
	Username string `json:"username"`
}

type CallbackQuery struct {
	ID      string   `json:"id"`
	Data    string   `json:"data"`
	Message *Message `json:"message"`
}

// GetUpdates — long-polling: ждёт до timeoutSec секунд новые обновления начиная с offset.
func GetUpdates(token string, offset int64, timeoutSec int) ([]Update, error) {
	u := fmt.Sprintf("%s?offset=%d&timeout=%d", apiURL(token, "getUpdates"), offset, timeoutSec)
	req, _ := http.NewRequest(http.MethodGet, u, nil)
	client := &http.Client{Timeout: time.Duration(timeoutSec+15) * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var r struct {
		OK          bool     `json:"ok"`
		Description string   `json:"description"`
		Result      []Update `json:"result"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("некорректный ответ getUpdates: %s", strings.TrimSpace(string(body)))
	}
	if !r.OK {
		return nil, fmt.Errorf("telegram: %s", r.Description)
	}
	return r.Result, nil
}

// AnswerCallback подтверждает нажатие кнопки (убирает «часики» и показывает toast).
func AnswerCallback(token, callbackID, text string) error {
	form := url.Values{"callback_query_id": {callbackID}, "text": {text}}
	req, _ := http.NewRequest(http.MethodPost, apiURL(token, "answerCallbackQuery"),
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, err := do(req)
	return err
}

// EditCaption меняет подпись сообщения и убирает кнопки (после решения модератора).
func EditCaption(token, chatID string, messageID int64, caption, parseMode string) error {
	form := url.Values{
		"chat_id":      {chatID},
		"message_id":   {strconv.FormatInt(messageID, 10)},
		"caption":      {caption},
		"reply_markup": {`{"inline_keyboard":[]}`},
	}
	if parseMode != "" {
		form.Set("parse_mode", parseMode)
	}
	req, _ := http.NewRequest(http.MethodPost, apiURL(token, "editMessageCaption"),
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	_, err := do(req)
	return err
}
