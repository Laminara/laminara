#!/usr/bin/env bash
set -euo pipefail

REPO="${LAMINARA_REPO:-Laminara/laminara}"
BINARY_OVERRIDE="${LAMINARA_BINARY:-}"

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
  section "$prompt"
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

safe_folder() {
  printf '%s' "$1"     | tr -d '[:cntrl:]'     | tr -d '/\\:*?"<>|'     | sed -E 's/^[[:space:].]+//; s/[[:space:].]+$//'     | cut -c1-48
}

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
  expected=$(awk -v name="$asset" '$2 == name || $2 == "*" name { print $1; exit }' "$sums")
  rm -f "$sums"
  [ -n "$expected" ] || die "в checksums.txt нет строки про $asset"
  actual=$(sha256sum "$dest" | awk '{ print $1 }')
  [ "$actual" = "$expected" ] || die "контрольная сумма не сошлась: скачано $actual, в релизе $expected"
  note "контрольная сумма сошлась"
  chmod +x "$dest"
}

recipe_section() { # recipe_section "<вывод loaders>" — строки раздела рецептов
  printf '%s\n' "$1" | sed -n '/^Рецепты совместимости/,$p' | tail -n +2
}

recipe_for() { # recipe_for "<вывод loaders>" загрузчик — имя подходящего рецепта
  recipe_section "$1" | awk -v want="$2" 'NF >= 3 && $2 == want { print $1; exit }'
}

recipe_summary_for() { # recipe_summary_for "<вывод loaders>" рецепт — его описание
  recipe_section "$1" | awk -v want="$2" '$1 == want { $1=""; $2=""; sub(/^ +/, ""); print; exit }'
}

first_build() { # first_build server config project endpoint
  local server=$1 config=$2 project=$3 endpoint=$4
  local name version loader release loaders_out recipe_name recipe_summary answer
  local loader_version="" recipe=""

  section "Первая сборка"
  release=$("$server" exec "versions" 2>/dev/null | sed 's/\x1b\[[0-9;]*m//g' | sed -n 's/^Последний релиз: \([^ ]*\).*/\1/p' | head -1)
  [ -n "$release" ] && note "последний релиз Minecraft — $release"

  while [ -z "${name:-}" ]; do
    ask name "  Имя сборки (латиницей, без точек и слэшей):" ""
    name=$(printf '%s' "$name" | tr -cd 'A-Za-z0-9_-')
  done
  while [ -z "${version:-}" ]; do
    ask version "  Версия Minecraft:" ""
  done

  say ""
  loaders_out=$("$server" exec "loaders $version" 2>&1 | sed 's/\x1b\[[0-9;]*m//g')
  printf '%s\n' "$loaders_out" | sed 's/^/  /'
  say ""
  while [ -z "${loader:-}" ]; do
    ask loader "  Загрузчик (из списка выше):" ""
  done

  recipe_name=$(recipe_for "$loaders_out" "$loader")
  if [ -n "$recipe_name" ]; then
    recipe_summary=$(recipe_summary_for "$loaders_out" "$recipe_name")
    say ""
    note "$version — старая версия: она рассчитана на Java 8 и LWJGL 2"
    note "рецепт $recipe_name — $recipe_summary"
    ask answer "  Собрать по рецепту $recipe_name? [Д/н]:" "д"
    case "$answer" in
      н|Н|n|N|нет|Нет|НЕТ|no|No|NO) : ;;
      *) recipe=$recipe_name ;;
    esac
  fi

  [ -n "$recipe" ] || ask loader_version "  Версия загрузчика (Enter — последняя):" ""

  local build_cmd="install $name $version loader=$loader"
  [ -n "$loader_version" ] && build_cmd="$build_cmd loaderVersion=$loader_version"
  [ -n "$recipe" ] && build_cmd="$build_cmd compat=$recipe"

  section "Собираю"
  note "это надолго: качается Minecraft, библиотеки и Java под каждую платформу"
  "$server" exec "$build_cmd" || { note "сборка не получилась — поправьте и повторите: laminara-server console"; return 1; }
  "$server" exec "publish $name"  || { note "публикация не прошла — повторите: laminara-server exec \"publish $name\""; return 1; }

  section "Лаунчер"
  "$server" exec "launcher build" || { note "лаунчер собрать не вышло — повторите: laminara-server exec \"launcher build\""; return 1; }

  section "Игрокам"
  say "  Скачать лаунчер:   $endpoint/launcher"
  say "  Windows:           $endpoint/launcher/windows-x64"
  say "  Linux:             $endpoint/launcher/linux"
  say ""
  note "ссылка постоянная: после каждой пересборки по ней лежит свежий лаунчер"
}

redis_only() {
  local config="" server="" candidate
  section "Laminara — настройка Redis"
  open_answers

  for candidate in /etc/laminara/config.json "$HOME/.config/laminara/config.json"; do
    [ -f "$candidate" ] && { config="$candidate"; break; }
  done
  ask config "Файл настроек:" "${config:-/etc/laminara/config.json}"
  [ -f "$config" ] || die "файл настроек $config не найден — сначала поставьте сервер"

  for candidate in /usr/local/bin/laminara-server "$HOME/.local/bin/laminara-server" /var/lib/laminara/laminara-server; do
    [ -x "$candidate" ] && { server="$candidate"; break; }
  done
  [ -n "$server" ] || die "не нашёл laminara-server — укажите его в PATH и повторите"

  local sudo=""; [ "$(id -u)" = 0 ] || sudo="sudo"
  setup_redis
  if [ -z "$REDIS_ADDR" ]; then
    $sudo "$server" settings --config "$config" auth.sessions.backend memory
    note "сессии оставлены в памяти"
  else
    $sudo "$server" settings --config "$config" auth.sessions.backend redis
    $sudo "$server" settings --config "$config" auth.sessions.redis.addr "$REDIS_ADDR"
    $sudo "$server" settings --config "$config" auth.sessions.redis.password "$REDIS_PASSWORD"
    good "сессии переведены в Redis: $REDIS_ADDR"
  fi

  if systemctl list-unit-files laminara-server.service >/dev/null 2>&1; then
    $sudo systemctl restart laminara-server && note "сервер перезапущен с новыми настройками"
  else
    note "перезапустите сервер, чтобы настройки вступили в силу"
  fi
}

main() {
  [ "$(uname -s)" = "Linux" ] || die "сервер работает только на Linux"
  if [ "${1:-}" = "--redis" ]; then
    redis_only
    return
  fi
  open_answers
  command -v curl >/dev/null || die "нужен curl"
  command -v sha256sum >/dev/null || die "нужен sha256sum"

  section "Laminara — установка"
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
  local server="$data_dir/laminara-server"
  install_binary "$server"
  ln -sf "$server" "$bin_dir/laminara-server"

  # --- название проекта ---
  local project folder folder_default
  section "Проект"
  ask project "  Название — его увидят игроки в лаунчере:" "Laminara"
  folder_default=$(safe_folder "$project")
  [ -n "$folder_default" ] || folder_default=laminara
  ask folder "  Папка на компьютере игрока:" "$folder_default"
  folder=$(safe_folder "$folder")
  [ -n "$folder" ] || folder="$folder_default"

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
    local guess; guess=$(hostname -I 2>/dev/null | awk '{print $1}')
    while [ -z "$domain" ]; do
      ask domain "Адрес проекта — по нему лаунчер ходит на сервер (IP или домен):" "$guess"
      [ -n "$domain" ] || note "этот адрес запекается в каждый лаунчер — без него игроки не найдут сервер"
    done
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
    setup_redis
    if [ -z "$REDIS_ADDR" ]; then
      sessions_block='{ "backend": "memory" }'
      note "сессии оставлены в памяти — Redis можно подключить позже: laminara-server settings auth.sessions.backend redis"
    elif [ -n "$REDIS_PASSWORD" ]; then
      sessions_block=$(printf '{ "backend": "redis", "redis": { "addr": "%s", "password": "%s" } }' \
        "$(json_escape "$REDIS_ADDR")" "$(json_escape "$REDIS_PASSWORD")")
    else
      sessions_block=$(printf '{ "backend": "redis", "redis": { "addr": "%s" } }' "$(json_escape "$REDIS_ADDR")")
    fi
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
    ask ygg_name   "  Имя сервера:" "$project"
    local skin_default="https://skins.${domain:-example.com}/%nickname%.png"
    case "$domain" in
      ''|*[!0-9.]*) ;;
      *) skin_default="https://minotar.net/skin/%nickname%" ;;
    esac
    ask skin_url   "  Шаблон ссылки на скин (%nickname% / %uuid%):" "$skin_default"
    skin_domain=$(printf '%s' "$skin_url" | sed -E 's#^https?://##; s#/.*##')
    ygg_tail=$(printf ',\n  "yggdrasil": { "enabled": true, "serverName": "%s", "rsaKeyPath": "%s/yggdrasil-rsa.pem", "skinProvider": "template", "skinConfig": { "skin": "%s" }, "skinDomains": ["%s"] }' \
      "$(json_escape "$ygg_name")" "$data_dir" "$(json_escape "$skin_url")" "$(json_escape "$skin_domain")")
  fi

  # --- запись конфига ---
  mkdir -p "$data_dir/profiles" "$data_dir/objects" "$data_dir/modules" "$data_dir/launcher"
  ( umask 077; cat > "$config" <<EOF
{
  "auth": $auth_block,
  "storage": $storage_block,
  "build": { "profilesDir": "$data_dir/profiles", "signingKeyPath": "$data_dir/signing.key" },
  "api": { "addr": "$api_addr"$xaccel },
  "launcher": { "dir": "$data_dir/launcher", "endpoints": ["$endpoint"] },
  "branding": { "name": "$(json_escape "$project")", "windowTitle": "$(json_escape "$project")", "folderName": "$(json_escape "$folder")" },
  "modules": { "dir": "$data_dir/modules" }$ygg_tail
}
EOF
  )
  chmod 600 "$config"
  section "Конфигурация записана"
  note "  бинарь:  $server"
  note "  конфиг:  $config"
  note "  данные:  $data_dir"

  # --- проверка конфига ---
  section "Проверяю настройки"
  if "$server" doctor --config "$config" --only config,auth,storage,build,api,yggdrasil >/dev/null 2>&1; then
    good "настройки в порядке"
  else
    "$server" doctor --config "$config" --only config,auth,storage,build,api,yggdrasil 2>&1 | tail -20
    note "сервер всё равно поставим — поправить можно потом: laminara-server console"
  fi

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

  section "Сервер готов ✓"
  say "Адрес проекта:      ${bold}${endpoint}${reset}"

  local wait_for_server=25
  while [ "$wait_for_server" -gt 0 ] && ! "$server" status >/dev/null 2>&1; do
    sleep 1
    wait_for_server=$((wait_for_server - 1))
  done

  choose "Что дальше?" \
    "Собрать первую сборку и лаунчер прямо сейчас" \
    "Открыть консоль проекта" \
    "На этом закончить"
  case "$CHOICE" in
    1) first_build "$server" "$config" "$project" "$endpoint" || true ;;
    2) exec 3<&- 2>/dev/null; "$server" console; return ;;
    3) : ;;
  esac

  section "Готово ✓"
  say "Адрес проекта:      ${bold}${endpoint}${reset}"
  say "Консоль проекта:    ${bold}$server console${reset}"
  say "Первая сборка:      ${bold}$server console${reset}  →  install <имя> <версия> loader=neoforge"
  say "Лаунчер для игроков:"
  say "  ${bold}$server console${reset}  →  launcher build"
  note "  соберёт .exe и файл для Linux из готового шаблона — ни Rust, ни pnpm не нужны"
}

redis_answer() { # redis_answer host port [password] — печатает ответ Redis на PING
  local host=$1 port=$2 pass=${3:-} line reply=""
  if ! { exec 4<>"/dev/tcp/$host/$port"; } 2>/dev/null; then
    printf 'нет связи'
    return
  fi
  if [ -n "$pass" ]; then
    printf 'AUTH %s\r\nPING\r\n' "$pass" >&4
  else
    printf 'PING\r\n' >&4
  fi
  while read -r -t 2 line <&4; do
    reply="$reply $line"
    case "$reply" in *PONG*) break ;; esac
  done
  exec 4<&- 2>/dev/null || true
  exec 4>&- 2>/dev/null || true
  printf '%s' "$reply"
}

redis_state() { # redis_state host port [password] -> ok | пароль | нет связи
  local reply; reply=$(redis_answer "$@")
  case "$reply" in
    *PONG*)                       printf 'ok' ;;
    *NOAUTH*|*WRONGPASS*|*"without any password"*) printf 'пароль' ;;
    *)                            printf 'нет связи' ;;
  esac
}

random_secret() {
  if command -v openssl >/dev/null; then
    openssl rand -hex 24
  else
    tr -dc 'A-Za-z0-9' < /dev/urandom | head -c 40
  fi
}

redis_conf_path() {
  local candidate
  for candidate in /etc/redis/redis.conf /etc/redis.conf /etc/valkey/valkey.conf; do
    [ -f "$candidate" ] && { printf '%s' "$candidate"; return; }
  done
}

REDIS_MARK='# Laminara: настройки от установщика'

start_redis_service() { # start_redis_service password
  local pass=$1 conf unit
  local sudo=""; [ "$(id -u)" = 0 ] || sudo="sudo"
  install_package redis-server >/dev/null 2>&1 || install_package redis >/dev/null 2>&1 || return 1

  conf=$(redis_conf_path)
  if [ -n "$conf" ] && [ -n "$pass" ]; then
    $sudo sed -i "\\|^$REDIS_MARK\$|,+2d" "$conf" 2>/dev/null || true
    printf '%s\nbind 127.0.0.1 ::1\nrequirepass %s\n' "$REDIS_MARK" "$pass" | $sudo tee -a "$conf" >/dev/null
  elif [ -n "$pass" ]; then
    note "конфиг redis не нашёлся — оставляю Redis без пароля, доступ только с этой машины"
    return 2
  fi

  for unit in redis-server redis valkey; do
    if $sudo systemctl enable --now "$unit" >/dev/null 2>&1; then
      $sudo systemctl restart "$unit" >/dev/null 2>&1 || true
      return 0
    fi
  done
  return 1
}

start_redis_container() { # start_redis_container password
  local pass=$1
  local sudo=""; [ "$(id -u)" = 0 ] || sudo="sudo"
  if $sudo docker ps -a --format '{{.Names}}' 2>/dev/null | grep -qx laminara-redis; then
    note "контейнер laminara-redis уже есть — пересоздаю его с новым паролем"
    $sudo docker rm -f laminara-redis >/dev/null 2>&1 || true
  fi
  $sudo docker run -d --name laminara-redis --restart unless-stopped \
    -p 127.0.0.1:6379:6379 redis:alpine \
    redis-server --requirepass "$pass" --appendonly yes >/dev/null 2>&1
}

wait_for_redis() { # wait_for_redis host port [password]
  local i
  for i in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15; do
    [ "$(redis_state "$@")" = "ok" ] && return 0
    sleep 1
  done
  return 1
}

settle_redis() { # settle_redis host port — ждёт наш свежепоставленный Redis и уточняет, взялся ли пароль
  local host=$1 port=$2
  if [ "$(redis_state "$host" "$port")" = "ok" ]; then
    REDIS_PASSWORD=""
    note "Redis отвечает без пароля — он слушает только эту машину"
    return 0
  fi
  if wait_for_redis "$host" "$port" "$REDIS_PASSWORD"; then
    good "Redis поднят, пароль задан и записан в конфиг"
    return 0
  fi
  note "Redis поставлен, но не отвечает на $host:$port"
  REDIS_PASSWORD=""
  return 1
}

setup_redis() { # заполняет REDIS_ADDR и REDIS_PASSWORD; пустой REDIS_ADDR = отказались
  REDIS_ADDR=""; REDIS_PASSWORD=""
  local addr host port state tries
  ask addr "  Адрес Redis:" "127.0.0.1:6379"
  while :; do
    host=${addr%:*}; port=${addr##*:}
    [ "$host" = "$port" ] && port=6379
    state=$(redis_state "$host" "$port")

    if [ "$state" = "пароль" ]; then
      tries=0
      while [ "$tries" -lt 3 ]; do
        ask_secret REDIS_PASSWORD "  Пароль Redis:"
        state=$(redis_state "$host" "$port" "$REDIS_PASSWORD")
        [ "$state" = "ok" ] && break
        note "Redis не принял этот пароль"
        REDIS_PASSWORD=""
        tries=$((tries + 1))
      done
    fi

    if [ "$state" = "ok" ]; then
      REDIS_ADDR="$host:$port"
      good "Redis отвечает: $REDIS_ADDR"
      return
    fi

    note "Redis по адресу $host:$port не отвечает."
    local options=() local_host=0
    case "$host" in 127.0.0.1|localhost|::1|"") local_host=1 ;; esac
    if [ "$local_host" = 1 ]; then
      command -v systemctl >/dev/null && options+=("Поставить Redis на этот сервер (пакет + служба)")
      command -v docker >/dev/null && options+=("Запустить Redis в Docker (контейнер laminara-redis)")
    fi
    options+=("Указать другой адрес")
    options+=("Обойтись без Redis — держать сессии в памяти")

    choose "Что делаем с Redis?" "${options[@]}"
    case "${options[$((CHOICE - 1))]}" in
      "Поставить Redis"*)
        REDIS_PASSWORD=$(random_secret)
        note "ставлю redis, задаю пароль и закрываю его от сети…"
        start_redis_service "$REDIS_PASSWORD"
        case $? in
          0) ;;
          2) REDIS_PASSWORD="" ;;
          *) note "поставить не вышло — поставьте Redis сами или выберите другой вариант"; REDIS_PASSWORD=""; continue ;;
        esac
        settle_redis "$host" "$port" || continue
        ;;
      "Запустить Redis в Docker"*)
        REDIS_PASSWORD=$(random_secret)
        note "поднимаю контейнер laminara-redis с паролем…"
        start_redis_container "$REDIS_PASSWORD" || { note "контейнер не поднялся — посмотрите docker logs laminara-redis"; REDIS_PASSWORD=""; continue; }
        settle_redis "$host" "$port" || continue
        ;;
      "Указать другой адрес")
        ask addr "  Адрес Redis:" "$addr"
        REDIS_PASSWORD=""
        ;;
      *)
        return
        ;;
    esac
  done
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
  section "nginx и сертификат"

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
  if ! id laminara >/dev/null 2>&1; then
    $sudo groupadd --system laminara 2>/dev/null || true
    $sudo useradd --system --gid laminara --home-dir "$data_dir" --shell /usr/sbin/nologin laminara 2>/dev/null \
      || $sudo useradd --system --home-dir "$data_dir" --shell /usr/sbin/nologin laminara 2>/dev/null || true
  fi
  id laminara >/dev/null 2>&1 || die "не удалось создать пользователя laminara. Создайте его вручную: sudo useradd --system --home-dir $data_dir --shell /usr/sbin/nologin laminara — и запустите установку снова"
  local run_group; run_group=$(id -gn laminara)
  $sudo chown -R laminara:"$run_group" "$data_dir" 2>/dev/null || true
  $sudo chown laminara:"$run_group" "$(dirname "$config")" "$config" 2>/dev/null || true
  $sudo chmod 750 "$(dirname "$config")" 2>/dev/null || true
  local unit; unit=$(mktemp)
  $sudo "$server" systemd-config --config "$config" --binary "$server" --user laminara --group "$run_group" > "$unit" \
    || die "версия сервера не умеет печатать unit systemd — обновите install.sh или сервер"
  $sudo tee /etc/systemd/system/laminara-server.service >/dev/null < "$unit"
  rm -f "$unit"
  $sudo systemctl daemon-reload
  $sudo systemctl enable --now laminara-server || die "служба не запустилась — что случилось, покажет: journalctl -u laminara-server -n 50"
  note "сервис запущен: systemctl status laminara-server"
}

main "$@"
