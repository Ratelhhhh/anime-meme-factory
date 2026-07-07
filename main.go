// Anime Meme Factory — берёт аниме-мемы с Пикабу и постит в Telegram-канал.
//
// Команды:
//
//	factory check        проверить токен бота и доступ к каналу (тест)
//	factory refill        наполнить очередь новыми постами автора
//	factory tick          опубликовать следующие картинки (по расписанию — раз в час)
//	factory status        статистика очереди
//	factory parse <URL>   (отладка) показать картинки конкретного поста
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Ratelhhhh/anime-meme-factory/internal/config"
	"github.com/Ratelhhhh/anime-meme-factory/internal/pikabu"
	"github.com/Ratelhhhh/anime-meme-factory/internal/store"
	"github.com/Ratelhhhh/anime-meme-factory/internal/telegram"
)

const (
	configPath = "config.json"
	statePath  = "data/state.json"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		return
	}
	cmd := os.Args[1]

	cfg, err := config.Load(configPath)
	if err != nil {
		fatal(err)
	}

	switch cmd {
	case "check":
		mustRequire(cfg)
		cmdCheck(cfg)
	case "refill":
		force := len(os.Args) > 2 && os.Args[2] == "--force"
		if err := cmdRefill(cfg, force); err != nil {
			fatal(err)
		}
	case "tick":
		mustRequire(cfg)
		if err := cmdTick(cfg); err != nil {
			fatal(err)
		}
	case "status":
		cmdStatus()
	case "parse":
		if len(os.Args) < 3 {
			fatal(fmt.Errorf("укажи URL поста"))
		}
		cmdParse(os.Args[2])
	default:
		fmt.Printf("Неизвестная команда: %s\n\n", cmd)
		usage()
	}
}

func cmdCheck(cfg config.Config) {
	me, err := telegram.GetMe(cfg.TelegramToken)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("Бот OK: @%s (%s)\n", me.Username, me.FirstName)
	fmt.Printf("Отправляю тест в %s ...\n", cfg.Channel)
	if err := telegram.SendMessage(cfg.TelegramToken, cfg.Channel,
		"✅ Контент-завод подключён. Тест связи с каналом."); err != nil {
		fmt.Printf("Ошибка: %v\n", err)
		fmt.Println("Проверь: бот добавлен админом канала и есть право «Публикация сообщений».")
		os.Exit(1)
	}
	fmt.Println("Успех! Бот пишет в канал.")
}

func cmdRefill(cfg config.Config, force bool) error {
	st, err := store.Load(statePath)
	if err != nil {
		return err
	}
	if !force && st.QueuedCount() >= cfg.MinQueue {
		fmt.Printf("Очередь заполнена (%d >= %d) — refill не нужен.\n",
			st.QueuedCount(), cfg.MinQueue)
		return nil
	}

	sources := cfg.SourceList()
	if len(sources) == 0 {
		return fmt.Errorf("не задан ни один источник (sources / source_user)")
	}

	// Собрать посты со всех источников (без дублей).
	var posts []string
	seenPost := map[string]bool{}
	for _, src := range sources {
		p, err := pikabu.ListPosts(src, cfg.PostPrefix, cfg.MaxPostsPerRefill)
		if err != nil {
			fmt.Printf("  ! источник @%s: %v\n", src, err)
			continue
		}
		fmt.Printf("Источник @%s: постов %d\n", src, len(p))
		for _, u := range p {
			if !seenPost[u] {
				seenPost[u] = true
				posts = append(posts, u)
			}
		}
		pikabu.PoliteSleep(cfg.RequestDelay)
	}

	newPosts, newImages := 0, 0
	for _, url := range posts {
		if st.PostSeen(url) {
			continue
		}
		pikabu.PoliteSleep(cfg.RequestDelay)
		info, err := pikabu.PostInfo(url)
		if err != nil {
			fmt.Printf("  ! %s: %v\n", url, err)
			continue
		}
		// Слишком низкий рейтинг — пропускаем пост, но НЕ помечаем виденным,
		// чтобы при следующем refill перепроверить (рейтинг мог подрасти).
		if cfg.MinRating > 0 && info.Rating < cfg.MinRating {
			fmt.Printf("  - %s: рейтинг %d < %d — пропуск\n",
				filepath.Base(url), info.Rating, cfg.MinRating)
			continue
		}
		imgs := info.Images
		added := 0
		for _, im := range imgs {
			if st.AddImage(im, url) {
				added++
			}
		}
		st.MarkPostSeen(url)
		if err := st.Save(); err != nil {
			return err
		}
		newPosts++
		newImages += added
		fmt.Printf("  + %s: рейтинг %d, картинок %d (новых %d)\n",
			filepath.Base(url), info.Rating, len(imgs), added)
	}

	fmt.Printf("Готово. Новых постов: %d, новых картинок: %d. В очереди: %d.\n",
		newPosts, newImages, st.QueuedCount())
	return nil
}

func cmdTick(cfg config.Config) error {
	st, err := store.Load(statePath)
	if err != nil {
		return err
	}
	// Пустая очередь — попробовать докинуть постов.
	if st.QueuedCount() == 0 {
		fmt.Println("Очередь пуста — запускаю refill...")
		if err := cmdRefill(cfg, true); err != nil {
			return err
		}
		st, err = store.Load(statePath)
		if err != nil {
			return err
		}
	}

	// Сколько картинок постить за этот tick. По умолчанию batch_size, но если ПК
	// был выключен и таймер пропустил N интервалов — наверстаем пропущенное разом.
	want := catchupCount(cfg, st.LastTick())

	// Кандидаты — вся очередь по порядку; постим, пропуская дубликаты по хешу,
	// пока не наберём want уникальных картинок.
	candidates := st.NextQueued(0)
	if len(candidates) == 0 {
		fmt.Println("Нечего постить: очередь пуста и новых картинок нет.")
		return nil
	}
	if want > 1 {
		fmt.Printf("Наверстываю простой: постим до %d картинок за этот запуск.\n", want)
	}

	posted, skipped := 0, 0
	for _, im := range candidates {
		if posted >= want {
			break
		}
		tmp := filepath.Join(os.TempDir(), fmt.Sprintf("af_%d_%s", im.ID, filepath.Base(im.URL)))

		// Скачать и посчитать хеш содержимого.
		size, err := pikabu.Download(im.URL, tmp)
		if err == nil && size < 1024 {
			err = fmt.Errorf("подозрительно маленький файл (%d байт)", size)
		}
		if err != nil {
			os.Remove(tmp)
			st.MarkFailed(im.ID, err.Error())
			_ = st.Save()
			fmt.Printf("ОШИБКА id=%d %s: %v\n", im.ID, im.URL, err)
			continue
		}

		hash, herr := fileSHA256(tmp)
		if herr == nil && st.HashSeen(hash) {
			os.Remove(tmp)
			st.MarkSkipped(im.ID, hash, "дубликат по хешу картинки")
			_ = st.Save()
			skipped++
			fmt.Printf("ПРОПУСК id=%d (дубликат) %s\n", im.ID, im.URL)
			continue
		}

		msgID, err := telegram.SendPhoto(cfg.TelegramToken, cfg.Channel, tmp,
			cfg.Caption, cfg.CaptionParseMode)
		os.Remove(tmp)
		if err != nil {
			st.MarkFailed(im.ID, err.Error())
			_ = st.Save()
			fmt.Printf("ОШИБКА id=%d %s: %v\n", im.ID, im.URL, err)
			continue
		}
		st.MarkPosted(im.ID, hash)
		if err := st.Save(); err != nil {
			return err
		}
		posted++
		fmt.Printf("OK опубликовано id=%d msg=%d %s\n", im.ID, msgID, im.URL)
	}
	st.TouchTick()
	if err := st.Save(); err != nil {
		return err
	}
	fmt.Printf("Опубликовано за tick: %d (пропущено дублей: %d). Осталось в очереди: %d.\n",
		posted, skipped, st.QueuedCount())
	return nil
}

// catchupCount считает, сколько картинок постить за один tick: обычно batch_size,
// но если с прошлого tick прошло несколько интервалов таймера (ПК был выключен) —
// возвращает столько картинок, сколько слотов пропущено, с учётом max_catchup.
func catchupCount(cfg config.Config, lastTick int64) int {
	batch := max(cfg.BatchSize, 1)
	interval := cfg.TickIntervalMin
	if lastTick == 0 || interval <= 0 {
		return batch // первый запуск или наверстывание отключено
	}
	elapsedMin := (time.Now().Unix() - lastTick) / 60
	slots := max(int(elapsedMin)/interval, 1)
	want := slots * batch
	if cfg.MaxCatchup > 0 && want > cfg.MaxCatchup {
		want = cfg.MaxCatchup
	}
	return want
}

// fileSHA256 считает sha256 содержимого файла (hex).
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func cmdStatus() {
	st, err := store.Load(statePath)
	if err != nil {
		fatal(err)
	}
	s := st.Stats()
	fmt.Printf("Постов просмотрено: %d\n", s.PostsSeen)
	fmt.Printf("В очереди:          %d\n", s.Queued)
	fmt.Printf("Опубликовано:       %d\n", s.Posted)
	fmt.Printf("Пропущено дублей:   %d\n", s.Skipped)
	fmt.Printf("Ошибок:             %d\n", s.Failed)
}

func cmdParse(url string) {
	imgs, err := pikabu.PostImages(url)
	if err != nil {
		fatal(err)
	}
	fmt.Printf("Картинок в посте: %d\n", len(imgs))
	for _, i := range imgs {
		fmt.Println("  ", i)
	}
}

func mustRequire(cfg config.Config) {
	if err := cfg.Require(); err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "Ошибка:", err)
	os.Exit(1)
}

func usage() {
	fmt.Print(`Anime Meme Factory

Использование:
  factory check         проверить бота и канал (тест)
  factory refill        наполнить очередь новыми постами Пикабу
  factory refill --force  наполнить принудительно
  factory tick           опубликовать следующие картинки
  factory status         статистика очереди
  factory parse <URL>    показать картинки поста (отладка)
`)
}
