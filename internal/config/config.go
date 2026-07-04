// Package config загружает настройки из config.json с переопределением через ENV.
package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	TelegramToken     string  `json:"telegram_token"`     // токен бота от @BotFather
	Channel           string  `json:"channel"`            // @username канала или числовой id (-100...)
	SourceUser        string  `json:"source_user"`        // автор на Пикабу
	PostPrefix        string  `json:"post_prefix"`        // брать только посты с таким префиксом slug ("" = все)
	Caption           string  `json:"caption"`            // необязательная подпись под картинкой
	BatchSize         int     `json:"batch_size"`         // сколько картинок постить за один tick
	MinQueue          int     `json:"min_queue"`          // порог, ниже которого refill докачивает
	MaxPostsPerRefill int     `json:"max_posts_per_refill"`
	RequestDelay      float64 `json:"request_delay"` // пауза между запросами к Пикабу, сек
}

func defaults() Config {
	return Config{
		SourceUser:        "BelarusPatriot",
		PostPrefix:        "animemyi_",
		BatchSize:         1,
		MinQueue:          15,
		MaxPostsPerRefill: 10,
		RequestDelay:      1.5,
	}
}

// Load читает config.json (если есть) и применяет ENV-переопределения.
func Load(path string) (Config, error) {
	cfg := defaults()
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("config.json: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return cfg, err
	}
	if v := os.Getenv("AF_TELEGRAM_TOKEN"); v != "" {
		cfg.TelegramToken = v
	}
	if v := os.Getenv("AF_CHANNEL"); v != "" {
		cfg.Channel = v
	}
	if v := os.Getenv("AF_SOURCE_USER"); v != "" {
		cfg.SourceUser = v
	}
	return cfg, nil
}

// Require проверяет обязательные поля.
func (c Config) Require() error {
	if c.TelegramToken == "" || c.Channel == "" {
		return fmt.Errorf("не заполнены telegram_token и/или channel в config.json " +
			"(см. config.example.json)")
	}
	return nil
}
