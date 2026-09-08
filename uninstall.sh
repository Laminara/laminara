#!/usr/bin/env bash
set -euo pipefail

bold=$'\e[1m'; dim=$'\e[2m'; accent=$'\e[38;5;99m'; ok=$'\e[38;5;42m'; warn=$'\e[38;5;214m'; reset=$'\e[0m'

say()  { printf '%s\n' "$*"; }
section() { printf '\n%s%s%s\n' "$bold$accent" "$*" "$reset"; }
note() { printf '%s%s%s\n' "$dim" "$*" "$reset"; }
good() { printf '%s%s%s\n' "$ok" "$*" "$reset"; }
die()  { printf '%serror:%s %s\n' "$warn" "$reset" "$*" >&2; exit 1; }

open_answers() {
  if [ -t 0 ]; then
    exec 3<&0
    return
  fi
  if { exec 3</dev/tty; } 2>/dev/null; then
    return
  fi
  die "удалению нужен терминал: скачайте скрипт и запустите его — curl -fsSL https://raw.githubusercontent.com/Laminara/laminara/main/uninstall.sh -o uninstall.sh && bash uninstall.sh"
}

ask() { # ask VAR "prompt" "default"
  local __var=$1 prompt=$2 default=${3:-} reply
  if [ -n "$default" ]; then
    printf '%s %s[%s]%s ' "$prompt" "$dim" "$default" "$reset"
  else
    printf '%s ' "$prompt"
  fi
  read -r reply <&3 || true
  printf -v "$__var" '%s' "${reply:-$default}"
}

confirm() { # confirm "prompt" -> 0 если да
  local reply
  printf '%s %s[да/нет]%s ' "$1" "$dim" "$reset"
  read -r reply <&3 || true
  case "${reply,,}" in
    д|да|y|yes) return 0 ;;
    *) return 1 ;;
  esac
}

sudo=""
if [ "$(id -u)" -ne 0 ]; then
  command -v sudo >/dev/null || die "нужны права root: запустите под sudo"
  sudo=sudo
fi

human_size() {
  local path=$1
  [ -e "$path" ] || { printf '—'; return; }
  du -sh "$path" 2>/dev/null | cut -f1 || printf '?'
}

section "Удаление Laminara"

config=""
for candidate in /etc/laminara/config.json "$HOME/.config/laminara/config.json"; do
  [ -f "$candidate" ] && { config="$candidate"; break; }
done
open_answers
ask config "Файл настроек:" "${config:-/etc/laminara/config.json}"

data_dir=""
binary=""
if [ -f "$config" ]; then
  data_dir=$(sed -n 's/.*"profilesDir"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "$config" | head -1)
  data_dir=${data_dir%/profiles}
fi
[ -n "$data_dir" ] || data_dir=/var/lib/laminara
ask data_dir "Каталог данных:" "$data_dir"

for candidate in "$data_dir/laminara-server" /usr/local/bin/laminara-server "$HOME/.local/bin/laminara-server"; do
  [ -e "$candidate" ] && { binary="$candidate"; break; }
done

section "Что будет удалено"
printf '  %-28s %s\n' "настройки" "$config"
printf '  %-28s %s  (%s)\n' "данные, ключи, сборки" "$data_dir" "$(human_size "$data_dir")"
[ -n "$binary" ] && printf '  %-28s %s\n' "программа" "$binary"
if systemctl list-unit-files laminara-server.service >/dev/null 2>&1; then
  printf '  %-28s %s\n' "служба systemd" "laminara-server.service"
fi
if id laminara >/dev/null 2>&1; then
  printf '  %-28s %s\n' "системный пользователь" "laminara"
fi
if [ -f /etc/nginx/sites-enabled/laminara.conf ] || [ -f /etc/nginx/conf.d/laminara.conf ]; then
  printf '  %-28s %s\n' "сайт nginx" "laminara.conf"
fi

note ""
note "В каталоге данных лежит ключ подписи. Без него лаунчеры, которые уже стоят у игроков,"
note "перестанут принимать ваши сборки — восстановить его можно только из резервной копии."
note ""

keep_data=1
if confirm "Удалить каталог данных вместе с ключами и сборками?"; then
  keep_data=0
fi

backup=""
if [ "$keep_data" -eq 0 ] && [ -n "$binary" ] && [ -x "$binary" ] && [ -f "$config" ]; then
  if confirm "Сначала сохранить ключи и настройки в архив?"; then
    backup="${TMPDIR:-/tmp}/laminara-backup-$(date +%Y%m%d-%H%M%S).tar.gz"
    if "$binary" backup --config "$config" --out "$backup" >/dev/null 2>&1; then
      good "Архив: $backup"
    else
      note "Архив сделать не вышло — продолжаю без него."
      backup=""
    fi
  fi
fi

confirm "Точно удалить Laminara?" || { say "Отменено, ничего не тронуто."; exit 0; }

section "Останавливаю"
if systemctl list-unit-files laminara-server.service >/dev/null 2>&1; then
  $sudo systemctl disable --now laminara-server.service >/dev/null 2>&1 || true
  $sudo rm -f /etc/systemd/system/laminara-server.service
  $sudo systemctl daemon-reload || true
  good "служба остановлена и снята с автозапуска"
elif [ -n "$binary" ] && [ -x "$binary" ]; then
  "$binary" stop >/dev/null 2>&1 || true
  good "сервер остановлен"
fi

if [ -f /etc/nginx/sites-enabled/laminara.conf ] || [ -f /etc/nginx/conf.d/laminara.conf ]; then
  $sudo rm -f /etc/nginx/sites-enabled/laminara.conf /etc/nginx/sites-available/laminara.conf /etc/nginx/conf.d/laminara.conf
  $sudo nginx -t >/dev/null 2>&1 && $sudo systemctl reload nginx >/dev/null 2>&1 || true
  good "сайт nginx убран"
fi

section "Удаляю"
[ -n "$binary" ] && { $sudo rm -f "$binary"; good "программа удалена"; }
for link in /usr/local/bin/laminara-server "$HOME/.local/bin/laminara-server"; do
  [ -L "$link" ] && $sudo rm -f "$link"
done
$sudo rm -rf /run/laminara

if [ "$keep_data" -eq 0 ]; then
  $sudo rm -rf "$data_dir"
  $sudo rm -f "$config"
  $sudo rmdir "$(dirname "$config")" 2>/dev/null || true
  good "данные и настройки удалены"
else
  note "Данные оставлены: $data_dir"
  note "Настройки оставлены: $config"
fi

if id laminara >/dev/null 2>&1 && confirm "Удалить системного пользователя laminara?"; then
  $sudo userdel laminara >/dev/null 2>&1 || true
  $sudo groupdel laminara >/dev/null 2>&1 || true
  good "пользователь удалён"
fi

section "Готово"
say "Laminara удалена."
[ -n "$backup" ] && say "Резервная копия ключей: $backup"
if [ "$keep_data" -eq 1 ]; then
  say "Данные остались в $data_dir — удалите вручную, когда они больше не нужны."
fi
