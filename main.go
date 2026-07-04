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
	"fmt"
	"os"
	"path/filepath"

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

	posts, err := pikabu.ListPosts(cfg.SourceUser, cfg.PostPrefix, cfg.MaxPostsPerRefill)
	if err != nil {
		return err
	}
	fmt.Printf("Найдено постов автора: %d\n", len(posts))

	newPosts, newImages := 0, 0
	for _, url := range posts {
		if st.PostSeen(url) {
			continue
		}
		pikabu.PoliteSleep(cfg.RequestDelay)
		imgs, err := pikabu.PostImages(url)
		if err != nil {
			fmt.Printf("  ! %s: %v\n", url, err)
			continue
		}
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
		fmt.Printf("  + %s: картинок %d (новых %d)\n", filepath.Base(url), len(imgs), added)
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

	rows := st.NextQueued(cfg.BatchSize)
	if len(rows) == 0 {
		fmt.Println("Нечего постить: очередь пуста и новых картинок нет.")
		return nil
	}

	posted := 0
	for _, im := range rows {
		tmp := filepath.Join(os.TempDir(), fmt.Sprintf("af_%d_%s", im.ID, filepath.Base(im.URL)))
		msgID, err := postOne(cfg, im, tmp)
		os.Remove(tmp)
		if err != nil {
			st.MarkFailed(im.ID, err.Error())
			_ = st.Save()
			fmt.Printf("ОШИБКА id=%d %s: %v\n", im.ID, im.URL, err)
			continue
		}
		st.MarkPosted(im.ID)
		if err := st.Save(); err != nil {
			return err
		}
		posted++
		fmt.Printf("OK опубликовано id=%d msg=%d %s\n", im.ID, msgID, im.URL)
	}
	fmt.Printf("Опубликовано за tick: %d. Осталось в очереди: %d.\n", posted, st.QueuedCount())
	return nil
}

func postOne(cfg config.Config, im store.Image, tmp string) (int64, error) {
	size, err := pikabu.Download(im.URL, tmp)
	if err != nil {
		return 0, err
	}
	if size < 1024 {
		return 0, fmt.Errorf("подозрительно маленький файл (%d байт)", size)
	}
	return telegram.SendPhoto(cfg.TelegramToken, cfg.Channel, tmp, cfg.Caption)
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
