// Package config загружает настройки из config.json с переопределением через ENV.
package config

import (
	"encoding/json"
	"fmt"
	"os"
)

type Config struct {
	TelegramToken     string   `json:"telegram_token"`     // токен бота от @BotFather
	Channel           string   `json:"channel"`            // @username канала или числовой id (-100...)
	SourceUser        string   `json:"source_user"`        // автор на Пикабу (устар., если пусто sources)
	Sources           []string `json:"sources"`            // список авторов на Пикабу (приоритетнее source_user)
	PostPrefix        string   `json:"post_prefix"`        // брать только посты с таким префиксом slug ("" = все)
	MinRating         int      `json:"min_rating"`         // брать только посты с рейтингом >= этого (0 = без фильтра)
	SingleImageOnly   bool     `json:"single_image_only"`  // брать только посты ровно с одной картинкой
	MaxTextChars      int      `json:"max_text_chars"`     // пропускать посты с текстом длиннее этого (0 = без лимита)
	Caption           string   `json:"caption"`            // необязательная подпись под картинкой
	CaptionParseMode  string   `json:"caption_parse_mode"` // "" | "HTML" | "MarkdownV2" — для ссылок в подписи
	BatchSize         int      `json:"batch_size"`         // сколько картинок постить за один tick
	MinQueue          int      `json:"min_queue"`          // порог, ниже которого refill докачивает
	MaxPostsPerRefill int      `json:"max_posts_per_refill"`
	RequestDelay      float64  `json:"request_delay"`         // пауза между запросами к Пикабу, сек
	TickIntervalMin   int      `json:"tick_interval_minutes"` // интервал таймера, мин (для наверстывания простоя)
	MaxCatchup        int      `json:"max_catchup"`           // макс. картинок за один tick при наверстывании (0 = без лимита)
	Moderation        bool     `json:"moderation"`            // true = картинки идут в PENDING и публикуются только после ручного одобрения
	StatePath         string   `json:"state_path"`            // путь к файлу состояния (для нескольких каналов — свой на каждый)
	ModeratePort      int      `json:"moderate_port"`         // порт локальной веб-панели модерации
}

// SourceList возвращает список источников: sources, либо [source_user] для совместимости.
func (c Config) SourceList() []string {
	if len(c.Sources) > 0 {
		return c.Sources
	}
	if c.SourceUser != "" {
		return []string{c.SourceUser}
	}
	return nil
}

func defaults() Config {
	return Config{
		SourceUser:        "BelarusPatriot",
		PostPrefix:        "animemyi_",
		BatchSize:         1,
		MinQueue:          15,
		MaxPostsPerRefill: 10,
		RequestDelay:      1.5,
		TickIntervalMin:   60,
		MaxCatchup:        6,
		StatePath:         "data/state.json",
		ModeratePort:      8099,
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
