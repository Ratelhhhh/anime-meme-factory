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

// cmdModerateBot — интерактивный бот-модератор. Ты пишешь ему в личку команды
// (refill / status), он присылает картинки на модерацию с кнопками
// «Одобрить/Отклонить». Пользоваться может ТОЛЬКО owner_id — остальным отказ.
// Ничего не публикуется в канал без нажатия «Одобрить».
func cmdModerateBot(cfg config.Config) error {
	if cfg.OwnerID == 0 {
		return fmt.Errorf("не задан owner_id в конфиге.\n" +
			"Узнай свой user_id: `factory --config <файл> chatid`, напиши боту в личку,\n" +
			"и впиши число в owner_id.")
	}
	statePath := statePathOf(cfg)
	tmpdir := filepath.Join(filepath.Dir(statePath), "modtmp")
	if err := os.MkdirAll(tmpdir, 0o755); err != nil {
		return err
	}
	// Куда слать карточки: явный moderation_chat или личка владельца.
	modChat := cfg.ModerationChat
	if modChat == "" {
		modChat = strconv.FormatInt(cfg.OwnerID, 10)
	}

	fmt.Printf("Бот-модератор запущен. Владелец: %d, канал: %s.\n", cfg.OwnerID, cfg.Channel)
	fmt.Println("Напиши боту в личку: refill (собрать) / status. Ctrl+C — остановить.")

	var offset int64
	for {
		updates, err := telegram.GetUpdates(cfg.TelegramToken, offset, 25)
		if err != nil {
			fmt.Printf("getUpdates: %v\n", err)
			time.Sleep(3 * time.Second)
			continue
		}
		for _, u := range updates {
			offset = u.UpdateID + 1
			switch {
			case u.CallbackQuery != nil:
				handleCallback(cfg, statePath, modChat, u.CallbackQuery)
			case u.Message != nil && u.Message.Text != "":
				handleCommand(cfg, statePath, tmpdir, modChat, u.Message)
			}
		}
	}
}

// authorized проверяет, что действие исходит от владельца.
func authorized(from *telegram.User, ownerID int64) bool {
	return from != nil && from.ID == ownerID
}

// handleCommand обрабатывает текстовые команды владельца в личке.
func handleCommand(cfg config.Config, statePath, tmpdir, modChat string, m *telegram.Message) {
	chat := strconv.FormatInt(m.Chat.ID, 10)
	if !authorized(m.From, cfg.OwnerID) {
		_ = telegram.SendMessage(cfg.TelegramToken, chat, "⛔ Доступ запрещён.")
		return
	}
	cmd := strings.ToLower(strings.TrimPrefix(strings.Fields(m.Text)[0], "/"))
	switch cmd {
	case "refill":
		_ = telegram.SendMessage(cfg.TelegramToken, chat, "🔎 Собираю кандидатов, подожди…")
		if err := cmdRefill(cfg, true); err != nil {
			_ = telegram.SendMessage(cfg.TelegramToken, chat, "Ошибка refill: "+err.Error())
			return
		}
		sent, err := pushPending(cfg, statePath, tmpdir, modChat)
		if err != nil {
			_ = telegram.SendMessage(cfg.TelegramToken, chat, "Прислал часть, потом сбой: "+err.Error())
		}
		st, _ := store.Load(statePath)
		s := st.Stats()
		_ = telegram.SendMessage(cfg.TelegramToken, chat,
			fmt.Sprintf("Готово. Прислал карточек: %d. Всего на модерации: %d. Одобрено в очереди: %d.",
				sent, s.Pending, s.Queued))
	case "status":
		st, _ := store.Load(statePath)
		s := st.Stats()
		_ = telegram.SendMessage(cfg.TelegramToken, chat, fmt.Sprintf(
			"Канал: %s\nНа модерации: %d\nОдобрено (в очереди): %d\nОпубликовано: %d\nОтклонено: %d",
			cfg.Channel, s.Pending, s.Queued, s.Posted, s.Rejected))
	case "start", "help":
		_ = telegram.SendMessage(cfg.TelegramToken, chat,
			"Я бот-модератор канала "+cfg.Channel+".\n\n"+
				"refill — собрать новых кандидатов и прислать карточки\n"+
				"status — статистика\n\n"+
				"Под каждой картинкой — кнопки ✅ Одобрить / ❌ Отклонить. "+
				"Одобренные публикуются в канал по одной в час.")
	default:
		_ = telegram.SendMessage(cfg.TelegramToken, chat, "Не понял. Команды: refill, status.")
	}
}

// pushPending отправляет владельцу все картинки на модерации, ещё не отправленные.
// Возвращает число отправленных карточек.
func pushPending(cfg config.Config, statePath, tmpdir, modChat string) (int, error) {
	st, err := store.Load(statePath)
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, im := range st.PendingUnsent() {
		tmp := filepath.Join(tmpdir, strconv.Itoa(im.ID))
		if _, err := pikabu.Download(im.URL, tmp); err != nil {
			fresh, _ := store.Load(statePath)
			fresh.MarkFailed(im.ID, "модерация: скачивание не удалось: "+err.Error())
			_ = fresh.Save()
			continue
		}
		caption := fmt.Sprintf("id %d\nисточник: %s", im.ID, im.PostURL)
		buttons := [][]telegram.InlineButton{{
			{Text: "✅ Одобрить", Data: "a:" + strconv.Itoa(im.ID)},
			{Text: "❌ Отклонить", Data: "r:" + strconv.Itoa(im.ID)},
		}}
		msgID, err := telegram.SendPhotoWithButtons(cfg.TelegramToken, modChat, tmp, caption, "", buttons)
		os.Remove(tmp)
		if err != nil {
			return sent, err
		}
		fresh, err := store.Load(statePath)
		if err != nil {
			return sent, err
		}
		fresh.SetModMsg(im.ID, msgID)
		if err := fresh.Save(); err != nil {
			return sent, err
		}
		sent++
	}
	return sent, nil
}

// handleCallback обрабатывает нажатие кнопки: одобрить/отклонить (только владелец).
func handleCallback(cfg config.Config, statePath, modChat string, cq *telegram.CallbackQuery) {
	if !authorized(cq.From, cfg.OwnerID) {
		_ = telegram.AnswerCallback(cfg.TelegramToken, cq.ID, "⛔ не для вас")
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
	var mark string
	switch action {
	case "a":
		if st.Approve(id) {
			mark = "✅ Одобрено"
		}
	case "r":
		if st.Reject(id) {
			mark = "❌ Отклонено"
		}
	}
	if mark == "" {
		_ = telegram.AnswerCallback(cfg.TelegramToken, cq.ID, "уже обработано")
		return
	}
	if err := st.Save(); err != nil {
		_ = telegram.AnswerCallback(cfg.TelegramToken, cq.ID, "ошибка сохранения")
		return
	}
	_ = telegram.AnswerCallback(cfg.TelegramToken, cq.ID, mark)
	if cq.Message != nil {
		_ = telegram.EditCaption(cfg.TelegramToken, modChat, cq.Message.MessageID,
			fmt.Sprintf("%s · id %d", mark, id), "")
	}
}

// cmdChatID помогает узнать свой user_id: напиши боту в личку любое сообщение.
func cmdChatID(cfg config.Config) error {
	fmt.Println("Напиши боту в личку любое сообщение. Жду 60 сек… Ctrl+C — выход.")
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
			var from *telegram.User
			if u.Message != nil {
				from = u.Message.From
			} else if u.CallbackQuery != nil {
				from = u.CallbackQuery.From
			}
			if from != nil && !seen[from.ID] {
				seen[from.ID] = true
				fmt.Printf("  user_id=%d  @%s  %s\n", from.ID, from.Username, from.FirstName)
				fmt.Printf("       → впиши в owner_id: %d\n", from.ID)
			}
		}
	}
	if len(seen) == 0 {
		fmt.Println("Ничего не поймал. Напиши боту в личку и попробуй снова.")
	}
	return nil
}
