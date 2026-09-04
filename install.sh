#!/usr/bin/env bash
set -euo pipefail

REPO="${LAMINARA_REPO:-Laminara/laminara}"
BINARY_OVERRIDE="${LAMINARA_BINARY:-}"

bold=$'\e[1m'; dim=$'\e[2m'; accent=$'\e[38;5;99m'; ok=$'\e[38;5;42m'; warn=$'\e[38;5;214m'; reset=$'\e[0m'

say()  { printf '%s\n' "$*"; }
head() { printf '\n%s%s%s\n' "$bold$accent" "$*" "$reset"; }
note() { printf '%s%s%s\n' "$dim" "$*" "$reset"; }
die()  { printf '%serror:%s %s\n' "$warn" "$reset" "$*" >&2; exit 1; }

open_answers() {
  if [ -t 0 ]; then
    exec 3<&0
    return
  fi
  if { exec 3</dev/tty; } 2>/dev/null; then
    return
  fi
  die "установщику нужен терминал: скачайте скрипт и запустите его — curl -fsSL https://raw.githubusercontent.com/${REPO}/main/install.sh -o install.sh && bash install.sh"
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

ask_secret() { # ask_secret VAR "prompt"
  local __var=$1 prompt=$2 reply
  printf '%s ' "$prompt"
  read -rs reply <&3 || true
  echo
  printf -v "$__var" '%s' "$reply"
}

choose() { # choose "prompt" "opt1" "opt2" ... -> sets CHOICE to 1-based index
  local prompt=$1; shift
  local options=("$@") i reply
  head "$prompt"
  for i in "${!options[@]}"; do
    printf '  %s%d%s  %s\n' "$accent" "$((i + 1))" "$reset" "${options[$i]}"
  done
  while :; do
    printf '%s› %s' "$dim" "$reset"
    read -r reply <&3 || true
    if [[ "$reply" =~ ^[0-9]+$ ]] && [ "$reply" -ge 1 ] && [ "$reply" -le "${#options[@]}" ]; then
      CHOICE=$reply
      return
    fi
    note "введите число от 1 до ${#options[@]}"
  done
}

json_escape() { printf '%s' "$1" | sed 's/\\/\\\\/g; s/"/\\"/g'; }

detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64) echo amd64 ;;
    aarch64|arm64) echo arm64 ;;
    *) die "неподдерживаемая архитектура: $(uname -m)" ;;
  esac
}

install_binary() {
  local dest=$1
  if [ -n "$BINARY_OVERRIDE" ]; then
    note "использую локальный бинарь: $BINARY_OVERRIDE"
    install -m0755 "$BINARY_OVERRIDE" "$dest"
    return
  fi
  local arch base asset sums expected actual
  arch=$(detect_arch)
  asset="laminara-server-linux-${arch}"
  base="https://github.com/${REPO}/releases/latest/download"
  note "скачиваю сервер ($arch) из релизов GitHub…"
  curl -fSL --progress-bar "$base/$asset" -o "$dest" || die "не удалось скачать $base/$asset"

  sums=$(mktemp)
  curl -fsSL "$base/checksums.txt" -o "$sums" || die "не удалось скачать checksums.txt — без него скачанное не проверить"
  expected=$(awk -v name="$asset" '$2 == name || $2 == "*" name { print $1 }' "$sums" | head -1)
  rm -f "$sums"
  [ -n "$expected" ] || die "в checksums.txt нет строки про $asset"
  actual=$(sha256sum "$dest" | awk '{ print $1 }')
  [ "$actual" = "$expected" ] || die "контрольная сумма не сошлась: скачано $actual, в релизе $expected"
  note "контрольная сумма сошлась"
  chmod +x "$dest"
}

main() {
  [ "$(uname -s)" = "Linux" ] || die "сервер работает только на Linux"
  open_answers
  command -v curl >/dev/null || die "нужен curl"
  command -v sha256sum >/dev/null || die "нужен sha256sum"

  head "Laminara — установка"
  note "Соберём конфигурацию и запустим сервер. Ничего качать руками не придётся."

  # --- каталоги и бинарь ---
  local bin_dir data_dir config
  if [ -w /usr/local/bin ] || [ "$(id -u)" = 0 ]; then
    bin_dir=/usr/local/bin; data_dir=/var/lib/laminara; config=/etc/laminara/config.json
  else
    bin_dir="$HOME/.local/bin"; data_dir="$HOME/.local/share/laminara"; config="$HOME/.config/laminara/config.json"
    note "нет прав на системные каталоги — ставлю в домашний ($bin_dir)"
  fi
  mkdir -p "$bin_dir" "$data_dir" "$(dirname "$config")"
  local server="$bin_dir/laminara-server"
  install_binary "$server"

  # --- как игроки приходят ---
  local front domain="" email="" api_addr endpoint
  choose "Как лаунчер будет ходить на проект?" "По домену через nginx, с сертификатом Let's Encrypt" "Напрямую по адресу и порту"
  if [ "$CHOICE" = 1 ]; then
    front=nginx
    while [ -z "$domain" ]; do ask domain "  Домен проекта — по нему лаунчер ходит на сервер (например launcher.example.com):" ""; done
    ask email "  Почта для Let's Encrypt:" ""
    api_addr="127.0.0.1:8099"
    endpoint="https://$domain"
  else
    front=direct
    ask api_addr "Адрес публичного слушателя:" "0.0.0.0:8099"
    ask domain "Адрес проекта — по нему лаунчер ходит на сервер (IP или домен):" "$(hostname -I 2>/dev/null | awk '{print $1}')"
    endpoint="http://${domain}:${api_addr##*:}"
  fi

  # --- хранилище ---
  local storage_block xaccel=""
  choose "Где хранить файлы сборок?" "Локальный диск (просто, когда сервер один)" "S3-совместимое (Garage/SeaweedFS/B2/облако)"
  if [ "$CHOICE" = 1 ]; then
    storage_block=$(printf '{ "backend": "fs", "config": { "root": "%s/objects", "xaccelPrefix": "/internal-objects/" } }' "$data_dir")
    [ "$front" = nginx ] && xaccel=', "xAccel": true'
  else
    local s3_endpoint s3_bucket s3_region s3_key s3_secret
    ask s3_endpoint "  Endpoint (например s3.eu-central-1.amazonaws.com):" ""
    ask s3_region   "  Region:" "us-east-1"
    ask s3_bucket   "  Bucket:" "laminara"
    ask s3_key      "  Access key ID:" ""
    ask_secret s3_secret "  Secret access key:"
    storage_block=$(printf '{ "backend": "s3", "config": { "endpoint": "%s", "region": "%s", "bucket": "%s", "accessKeyId": "%s", "secretAccessKey": "%s", "pathStyle": true } }' \
      "$(json_escape "$s3_endpoint")" "$(json_escape "$s3_region")" "$(json_escape "$s3_bucket")" "$(json_escape "$s3_key")" "$(json_escape "$s3_secret")")
  fi

  # --- сессии ---
  local sessions_block
  choose "Где хранить сессии?" "В памяти (просто, один экземпляр)" "Redis (переживает перезапуск, несколько экземпляров)"
  if [ "$CHOICE" = 1 ]; then
    sessions_block='{ "backend": "memory" }'
  else
    local redis_addr
    ask redis_addr "  Адрес Redis:" "127.0.0.1:6379"
    sessions_block=$(printf '{ "backend": "redis", "redisAddr": "%s" }' "$(json_escape "$redis_addr")")
  fi

  # --- аутентификация ---
  local auth_block
  choose "Откуда брать учётные записи?" "JSON-файл (создам первого пользователя)" "SQL-база (Postgres/MySQL)" "HTTP-API"
  case "$CHOICE" in
    1)
      local admin_user admin_pass admin_pass2 users="$data_dir/users.json" hashed
      ask admin_user "  Логин администратора:" "admin"
      while :; do
        ask_secret admin_pass "  Пароль:"
        ask_secret admin_pass2 "  Повторите пароль:"
        [ "$admin_pass" = "$admin_pass2" ] && [ -n "$admin_pass" ] && break
        note "пароли не совпали или пусты — ещё раз"
      done
      hashed=$(printf '%s\n' "$admin_pass" | "$server" hash --algo argon2id)
      printf '[{ "username": "%s", "password": "%s", "uuid": "" }]\n' "$(json_escape "$admin_user")" "$hashed" > "$users"
      chmod 600 "$users"
      auth_block=$(printf '{ "provider": "jsonfile", "config": { "path": "%s", "hash": "argon2id", "fields": { "username": "username", "password": "password", "uuid": "uuid" } }, "sessions": %s }' \
        "$users" "$sessions_block")
      ;;
    2)
      local driver dsn table fcol_user fcol_pass fcol_uuid halgo
      choose "  СУБД" "postgres" "mysql"; [ "$CHOICE" = 1 ] && driver=postgres || driver=mysql
      ask dsn       "  DSN подключения:" ""
      ask table     "  Таблица пользователей:" "users"
      ask fcol_user "  Колонка логина:" "username"
      ask fcol_pass "  Колонка пароля (хеша):" "password"
      ask fcol_uuid "  Колонка UUID (Enter — нет):" ""
      ask halgo     "  Хеш пароля в базе (argon2id/bcrypt/sha256/…):" "bcrypt"
      auth_block=$(printf '{ "provider": "sql", "config": { "driver": "%s", "dsn": "%s", "table": "%s", "hash": "%s", "fields": { "username": "%s", "password": "%s", "uuid": "%s" } }, "sessions": %s }' \
        "$driver" "$(json_escape "$dsn")" "$(json_escape "$table")" "$halgo" "$fcol_user" "$fcol_pass" "$fcol_uuid" "$sessions_block")
      ;;
    3)
      local url f_user f_pass f_uuid f_ok
      ask url    "  URL проверки логина:" ""
      ask f_user "  Поле логина в запросе:" "username"
      ask f_pass "  Поле пароля в запросе:" "password"
      ask f_uuid "  Поле UUID в ответе:" "uuid"
      ask f_ok   "  Поле успеха в ответе:" "ok"
      auth_block=$(printf '{ "provider": "http", "config": { "url": "%s", "usernameField": "%s", "passwordField": "%s", "uuidField": "%s", "successField": "%s" }, "sessions": %s }' \
        "$(json_escape "$url")" "$f_user" "$f_pass" "$f_uuid" "$f_ok" "$sessions_block")
      ;;
  esac

  # --- yggdrasil ---
  local ygg_tail=""
  choose "Включить вход в игре (authlib-injector) и скины?" "Да" "Нет"
  if [ "$CHOICE" = 1 ]; then
    local ygg_name skin_url skin_domain
    ask ygg_name   "  Имя сервера:" "Laminara"
    ask skin_url   "  Шаблон ссылки на скин (%nickname% / %uuid%):" "https://skins.${domain:-example.com}/%nickname%.png"
    skin_domain=$(printf '%s' "$skin_url" | sed -E 's#^https?://##; s#/.*##')
    ygg_tail=$(printf ',\n  "yggdrasil": { "enabled": true, "serverName": "%s", "rsaKeyPath": "%s/yggdrasil-rsa.pem", "skinProvider": "template", "skinConfig": { "skin": "%s" }, "skinDomains": ["%s"] }' \
      "$(json_escape "$ygg_name")" "$data_dir" "$(json_escape "$skin_url")" "$(json_escape "$skin_domain")")
  fi

  # --- запись конфига ---
  mkdir -p "$data_dir/profiles" "$data_dir/objects"
  cat > "$config" <<EOF
{
  "auth": $auth_block,
  "storage": $storage_block,
  "build": { "profilesDir": "$data_dir/profiles", "signingKeyPath": "$data_dir/signing.key" },
  "api": { "addr": "$api_addr"$xaccel }$ygg_tail
}
EOF
  head "Конфигурация записана"
  note "  бинарь:  $server"
  note "  конфиг:  $config"
  note "  данные:  $data_dir"

  # --- запуск ---
  local run_opts=("Только конфиг — запущу сам")
  command -v systemctl >/dev/null && run_opts+=("systemd-сервис (автозапуск)")
  command -v docker >/dev/null && run_opts+=("Docker (compose)")
  choose "Как запускать сервер?" "${run_opts[@]}"
  local picked=${run_opts[$((CHOICE - 1))]}
  case "$picked" in
    systemd*)
      setup_systemd "$server" "$config" "$data_dir" ;;
    Docker*)
      note "Пример docker-compose — в deploy/docker-compose.yml репозитория." ;;
    *)
      say; say "Запуск: ${bold}$server start --config $config${reset}" ;;
  esac

  [ "$front" = nginx ] && setup_nginx "$server" "$config" "$domain" "$email"

  head "Готово ✓"
  say "Адрес проекта:      ${bold}${endpoint}${reset}"
  say "Первая сборка:      ${bold}$server console${reset}  →  install <имя> <версия> loader=neoforge"
  say "Лаунчер для игроков:"
  say "  ${bold}$server console${reset}  →  launcher build"
  note "  соберёт .exe и файл для Linux из готового шаблона — ни Rust, ни pnpm не нужны"
}

install_package() {
  local sudo=""; [ "$(id -u)" = 0 ] || sudo="sudo"
  if command -v apt-get >/dev/null; then
    $sudo apt-get update -qq && $sudo apt-get install -y "$@"
  elif command -v dnf >/dev/null; then
    $sudo dnf install -y "$@"
  elif command -v pacman >/dev/null; then
    $sudo pacman -Sy --noconfirm "$@"
  elif command -v zypper >/dev/null; then
    $sudo zypper install -y "$@"
  else
    return 1
  fi
}

setup_nginx() {
  local server=$1 config=$2 domain=$3 email=$4
  local sudo=""; [ "$(id -u)" = 0 ] || sudo="sudo"
  head "nginx и сертификат"

  command -v nginx >/dev/null || install_package nginx || {
    note "не смог поставить nginx — конфиг напечатан ниже, поставьте руками"
    "$server" nginx-config --config "$config" --domain "$domain"
    return
  }

  local site link
  if [ -d /etc/nginx/sites-available ]; then
    site=/etc/nginx/sites-available/laminara.conf
    link=/etc/nginx/sites-enabled/laminara.conf
  else
    site=/etc/nginx/conf.d/laminara.conf
    link=""
  fi
  "$server" nginx-config --config "$config" --domain "$domain" --no-tls | $sudo tee "$site" >/dev/null
  [ -n "$link" ] && $sudo ln -sf "$site" "$link"
  [ -e /etc/nginx/sites-enabled/default ] && $sudo rm -f /etc/nginx/sites-enabled/default
  $sudo nginx -t || die "nginx не принял конфиг $site"
  $sudo systemctl reload nginx 2>/dev/null || $sudo nginx -s reload
  note "  сайт: $site"

  if ! command -v certbot >/dev/null; then
    install_package certbot python3-certbot-nginx || {
      note "не смог поставить certbot — сервер работает по HTTP, сертификат получите сами"
      return
    }
  fi
  local certbot_args=(--nginx -d "$domain" --agree-tos --non-interactive --redirect)
  if [ -n "$email" ]; then certbot_args+=(-m "$email"); else certbot_args+=(--register-unsafely-without-email); fi
  if $sudo certbot "${certbot_args[@]}"; then
    note "  сертификат получен, обновляется сам"
  else
    note "не вышло получить сертификат — проверьте, что домен $domain смотрит на этот сервер, и повторите: sudo certbot --nginx -d $domain"
  fi
}

setup_systemd() {
  local server=$1 config=$2 data_dir=$3
  local sudo=""; [ "$(id -u)" = 0 ] || sudo="sudo"
  note "ставлю systemd-сервис (нужны права)…"
  id laminara >/dev/null 2>&1 || $sudo useradd --system --home "$data_dir" --shell /usr/sbin/nologin laminara || true
  $sudo chown -R laminara:laminara "$data_dir" 2>/dev/null || true
  $sudo tee /etc/systemd/system/laminara-server.service >/dev/null <<EOF
[Unit]
Description=Laminara launcher server
After=network-online.target
Wants=network-online.target

[Service]
Type=notify
User=laminara
Group=laminara
ExecStart=$server start --config $config
Restart=on-failure
RestartSec=5
RuntimeDirectory=laminara
Environment=XDG_RUNTIME_DIR=/run/laminara
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=$data_dir /run/laminara

[Install]
WantedBy=multi-user.target
EOF
  $sudo systemctl daemon-reload
  $sudo systemctl enable --now laminara-server
  note "сервис запущен: systemctl status laminara-server"
}

main "$@"
