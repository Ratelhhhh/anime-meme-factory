# 🏭 Anime Meme Factory (Go)

Контент-завод на Go: автоматически берёт аниме-мемы из постов автора на Пикабу и
по расписанию публикует их в Telegram-канал — по одной картинке в час.

- **Без внешних зависимостей** — только стандартная библиотека Go.
- Помнит опубликованные картинки в `data/state.json` — **дублей не будет**.
- `tick` раз в час постит 1 картинку, `refill` раз в 12 часов ищет новые посты.

```
Пикабу (@BelarusPatriot)
   │  refill: посты автора → картинки (data-large-image)
   ▼
JSON-очередь (data/state.json)   ← помнит спарсенное и опубликованное
   │  tick: взять следующую, скачать
   ▼
Telegram Bot API → канал
```

## Сборка

```bash
go build -o factory .
```

Нужен Go 1.21+. Бинарник `factory` самодостаточный.

## 1. Бот и канал

1. **@BotFather** → `/newbot` → скопируй **токен**.
2. Создай канал, получи `@username` (например `@anime_mem_mem`).
3. Добавь бота **администратором** канала с правом **«Публикация сообщений»**.

## 2. Конфиг

Скопируй пример и впиши свои данные:

```bash
cp config.example.json config.json
```

```json
{
  "telegram_token": "123456789:AAE...",
  "channel": "@anime_mem_mem",
  "source_user": "BelarusPatriot",
  "post_prefix": "animemyi_",
  "batch_size": 1,
  "caption": ""
}
```

- `post_prefix` — брать только посты с таким началом адреса (`animemyi_` —
  только «Анимемы»; `""` — все посты автора).
- `batch_size` — картинок за один tick (1 = одна в час).
- `caption` — подпись под каждой картинкой (можно пусто).

> `config.json` в `.gitignore` — токен не попадёт в репозиторий.

## 3. Проверка

```bash
./factory check     # проверит токен и отправит тест в канал
./factory refill    # наполнит очередь картинками с Пикабу
./factory tick       # опубликует первую картинку прямо сейчас
./factory status    # статистика очереди
```

## 4. Автопостинг (systemd)

```bash
bash install.sh                      # соберёт бинарник и включит таймеры
sudo loginctl enable-linger $USER    # чтобы работало без входа в систему
```

- **anime-factory-tick.timer** — каждый час 1 картинка.
- **anime-factory-refill.timer** — каждые 12 часов ищет новые посты.

Логи и расписание:

```bash
systemctl --user list-timers 'anime-factory-*'
journalctl --user -u anime-factory-tick.service -n 50
```

Изменить частоту — правь `OnUnitActiveSec=1h` в
`~/.config/systemd/user/anime-factory-tick.timer`, затем
`systemctl --user daemon-reload && systemctl --user restart anime-factory-tick.timer`.

Остановить:

```bash
systemctl --user disable --now anime-factory-tick.timer anime-factory-refill.timer
```

## Команды

| Команда | Что делает |
|---|---|
| `factory check` | проверить бота и канал |
| `factory refill` | добрать посты (если очередь < min_queue) |
| `factory refill --force` | добрать принудительно |
| `factory tick` | опубликовать следующие картинки |
| `factory status` | статистика очереди |
| `factory parse <URL>` | показать картинки поста (отладка) |

## Заметки

- Контент берётся с чужого аккаунта Пикабу — это репаблишинг. Помни про
  авторские права мемов и правила площадок.
- Если Пикабу поменяет вёрстку — поправь регулярки в `internal/pikabu/pikabu.go`
  (`storyRe`, `largeImgRe`).
