package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Ratelhhhh/anime-meme-factory/internal/config"
	"github.com/Ratelhhhh/anime-meme-factory/internal/pikabu"
	"github.com/Ratelhhhh/anime-meme-factory/internal/store"
	"github.com/Ratelhhhh/anime-meme-factory/internal/telegram"
)

// cmdModerateBot — модерация через бота: рассылает картинки на модерации в чат
// модерации с кнопками «Одобрить/Отклонить» и ловит нажатия. Одобренные уходят
// в очередь публикации, отклонённые отсеиваются. Ничего не публикуется в канал
// без нажатия «Одобрить».
func cmdModerateBot(cfg config.Config) error {
	if cfg.ModerationChat == "" {
		return fmt.Errorf("не задан moderation_chat в конфиге.\n" +
			"Создай приватную группу/канал, добавь бота, узнай id командой:\n" +
			"  factory --config <файл> chatid\n" +
			"и впиши его в moderation_chat.")
	}
	statePath := statePathOf(cfg)
	tmpdir := filepath.Join(filepath.Dir(statePath), "modtmp")
	if err := os.MkdirAll(tmpdir, 0o755); err != nil {
		return err
	}

	fmt.Printf("Бот-модератор запущен. Кандидаты идут в %s (канал %s).\n",
		cfg.ModerationChat, cfg.Channel)
	fmt.Println("Жми кнопки под картинками в чате модерации. Ctrl+C — остановить.")

	var offset int64
	for {
		if err := pushPending(cfg, statePath, tmpdir); err != nil {
			fmt.Printf("push: %v\n", err)
		}
		updates, err := telegram.GetUpdates(cfg.TelegramToken, offset, 25)
		if err != nil {
			fmt.Printf("getUpdates: %v\n", err)
			time.Sleep(3 * time.Second)
			continue
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			handleUpdate(cfg, statePath, u)
		}
	}
}

// pushPending отправляет в чат модерации все картинки на модерации, которые ещё
// не отправляли (по одной карточке с кнопками).
func pushPending(cfg config.Config, statePath, tmpdir string) error {
	st, err := store.Load(statePath)
	if err != nil {
		return err
	}
	for _, im := range st.PendingUnsent() {
		tmp := filepath.Join(tmpdir, strconv.Itoa(im.ID))
		if _, err := pikabu.Download(im.URL, tmp); err != nil {
			// битый/недоступный url — помечаем, чтобы не долбить каждый цикл
			st.MarkFailed(im.ID, "модерация: скачивание не удалось: "+err.Error())
			_ = st.Save()
			fmt.Printf("  ! id=%d скачивание: %v\n", im.ID, err)
			continue
		}
		caption := fmt.Sprintf("id %d · рейтинг≥%d\nисточник: %s", im.ID, cfg.MinRating, im.PostURL)
		buttons := [][]telegram.InlineButton{{
			{Text: "✅ Одобрить", Data: "a:" + strconv.Itoa(im.ID)},
			{Text: "❌ Отклонить", Data: "r:" + strconv.Itoa(im.ID)},
		}}
		msgID, err := telegram.SendPhotoWithButtons(cfg.TelegramToken, cfg.ModerationChat, tmp, caption, "", buttons)
		os.Remove(tmp)
		if err != nil {
			fmt.Printf("  ! id=%d отправка в чат модерации: %v\n", im.ID, err)
			return err // вероятно проблема с доступом к чату — прекращаем пуш до след. цикла
		}
		// перечитываем и сохраняем точечно, чтобы не затирать параллельные правки
		fresh, err := store.Load(statePath)
		if err != nil {
			return err
		}
		fresh.SetModMsg(im.ID, msgID)
		if err := fresh.Save(); err != nil {
			return err
		}
		fmt.Printf("  → на модерацию: id=%d (msg %d)\n", im.ID, msgID)
	}
	return nil
}

// handleUpdate обрабатывает нажатие кнопки: одобрить/отклонить картинку.
func handleUpdate(cfg config.Config, statePath string, u telegram.Update) {
	cq := u.CallbackQuery
	if cq == nil || cq.Data == "" {
		return
	}
	action, idStr, ok := strings.Cut(cq.Data, ":")
	if !ok {
		return
	}
	id, err := strconv.Atoi(idStr)
	if err != nil {
		return
	}

	st, err := store.Load(statePath)
	if err != nil {
		_ = telegram.AnswerCallback(cfg.TelegramToken, cq.ID, "ошибка состояния")
		return
	}
	var toast, mark string
	switch action {
	case "a":
		if st.Approve(id) {
			toast, mark = "✅ Одобрено", "✅ Одобрено"
		}
	case "r":
		if st.Reject(id) {
			toast, mark = "❌ Отклонено", "❌ Отклонено"
		}
	}
	if toast == "" {
		_ = telegram.AnswerCallback(cfg.TelegramToken, cq.ID, "уже обработано")
		return
	}
	if err := st.Save(); err != nil {
		_ = telegram.AnswerCallback(cfg.TelegramToken, cq.ID, "ошибка сохранения")
		return
	}
	_ = telegram.AnswerCallback(cfg.TelegramToken, cq.ID, toast)
	if cq.Message != nil {
		_ = telegram.EditCaption(cfg.TelegramToken, cfg.ModerationChat, cq.Message.MessageID,
			fmt.Sprintf("%s · id %d", mark, id), "")
	}
}

// cmdChatID помогает узнать id чата модерации: слушает обновления и печатает,
// от каких чатов приходят сообщения (добавь бота в группу/канал и напиши туда).
func cmdChatID(cfg config.Config) error {
	fmt.Println("Узнаём id чата. Добавь бота в приватную группу/канал и напиши там любое")
	fmt.Println("сообщение (в канал — сделай пост). Жду 60 сек... Ctrl+C — выход.")
	deadline := time.Now().Add(60 * time.Second)
	var offset int64
	seen := map[int64]bool{}
	for time.Now().Before(deadline) {
		updates, err := telegram.GetUpdates(cfg.TelegramToken, offset, 20)
		if err != nil {
			return err
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			var ch *telegram.Chat
			if u.Message != nil {
				ch = &u.Message.Chat
			} else if u.CallbackQuery != nil && u.CallbackQuery.Message != nil {
				ch = &u.CallbackQuery.Message.Chat
			}
			if ch != nil && !seen[ch.ID] {
				seen[ch.ID] = true
				name := ch.Title
				if name == "" {
					name = "@" + ch.Username
				}
				fmt.Printf("  чат: id=%d  тип=%s  %s\n", ch.ID, ch.Type, name)
				fmt.Printf("       → впиши в moderation_chat: \"%d\"\n", ch.ID)
			}
		}
	}
	if len(seen) == 0 {
		fmt.Println("Ничего не поймал. Проверь, что бот добавлен в чат и там есть сообщение.")
	}
	return nil
}
