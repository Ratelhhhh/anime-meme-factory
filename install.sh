#!/usr/bin/env bash
# Сборка бинарника и установка таймеров systemd (уровень пользователя, root не нужен).
set -e

DIR="$(cd "$(dirname "$0")" && pwd)"
UNIT_DIR="$HOME/.config/systemd/user"

echo ">> Собираю бинарник factory"
cd "$DIR"
go build -o factory .

echo ">> Копирую unit-файлы в $UNIT_DIR"
mkdir -p "$UNIT_DIR"
cp "$DIR"/systemd/anime-factory-tick.service   "$UNIT_DIR/"
cp "$DIR"/systemd/anime-factory-tick.timer     "$UNIT_DIR/"
cp "$DIR"/systemd/anime-factory-refill.service "$UNIT_DIR/"
cp "$DIR"/systemd/anime-factory-refill.timer   "$UNIT_DIR/"

echo ">> Включаю таймеры"
systemctl --user daemon-reload
systemctl --user enable --now anime-factory-tick.timer
systemctl --user enable --now anime-factory-refill.timer

echo ">> Чтобы таймеры работали без активного логина, включи linger:"
echo "   sudo loginctl enable-linger $USER"
echo
systemctl --user list-timers 'anime-factory-*' --no-pager || true
