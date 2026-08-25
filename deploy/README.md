# Развёртывание

Сервер — один статический бинарь `laminara-server`. Работает без интерфейса, а управляется
через Unix-сокет: та же команда подключается к запущенному процессу. Наружу он открывает один
TCP-порт, на котором висят API лаунчера, раздача файлов и вход в игре; TLS перед ним держит
nginx.

Подробности по каждому разделу — в [документации](https://docs.laminara.dev).

## Что здесь лежит

| Файл | Зачем |
| --- | --- |
| `Dockerfile` | Многоступенчатая сборка в `debian:12-slim`. Именно glibc, а не distroless: скачанная Java должна запускать процессоры установщика Forge и NeoForge. |
| `docker-compose.yml` | Сервер и Redis; Postgres и своё S3 закомментированы до того момента, когда понадобятся. |
| `systemd/laminara-server.service` | Юнит `Type=notify` — демон сам сообщает systemd о готовности. |
| `config.example.json` | Конфиг со всеми разделами. |

## Docker

```sh
make generate                                        # protobuf перед сборкой образа
cp deploy/config.example.json deploy/config.json     # и отредактировать
docker compose -f deploy/docker-compose.yml up -d --build
```

## systemd

```sh
make build
sudo install -m0755 bin/laminara-server /usr/local/bin/laminara-server
sudo useradd --system --home /var/lib/laminara --shell /usr/sbin/nologin laminara
sudo install -d -o laminara -g laminara /var/lib/laminara /etc/laminara
sudo install -m0640 -o laminara -g laminara deploy/config.example.json /etc/laminara/config.json
sudo install -m0644 deploy/systemd/laminara-server.service /etc/systemd/system/
sudo systemctl enable --now laminara-server
```

## nginx и TLS

Конфиг печатает сам сервер — он знает и порт слушателя, и путь к хранилищу:

```sh
laminara-server nginx-config --config /etc/laminara/config.json --domain launcher.example.com --no-tls \
  | sudo tee /etc/nginx/sites-available/laminara.conf
sudo ln -sf /etc/nginx/sites-available/laminara.conf /etc/nginx/sites-enabled/laminara.conf
sudo nginx -t && sudo systemctl reload nginx
sudo certbot --nginx -d launcher.example.com
```

`--no-tls` печатает один блок на `:80` — сертификата ещё нет; TLS в этот блок добавит certbot.
Без ключа команда сразу печатает вариант с `:443` и путями к сертификатам Let's Encrypt.

Локация `/internal-objects/` должна оставаться `internal` и указывать `alias` на корень
файлового хранилища: сервер отвечает заголовком `X-Accel-Redirect` (включается
`api.xAccel: true`), а байты отдаёт nginx через `sendfile`, не пропуская их через процесс
сервера. С хранилищем S3 всё иначе — сервер выдаёт временную ссылку, и nginx на пути байтов
не стоит.

## Работа с сервером

```sh
laminara-server status
laminara-server console                       # интерактивная консоль с мастерами
laminara-server exec versions 1.21
laminara-server exec loaders 1.21.1
laminara-server exec install <имя> <версия> [loader=neoforge] [loaderVersion=…] [platform=windows-x64]
laminara-server exec publish <имя>
```

`install` собирает готовый клиент в `build.profilesDir/<имя>` — папку можно править руками.
`publish` снимает с неё подписанный манифест, за которым и приходит лаунчер. Новые версии
Minecraft и загрузчиков подхватываются сами, передеплой для этого не нужен.

## Лаунчер под ваш проект

Игрок не вводит адрес сервера: лаунчер, который вы ему даёте, уже знает адреса и доверяет
вашим ключам подписи. Сначала соберите конфигурацию с работающего сервера, потом соберите
с ней лаунчер:

```sh
laminara-server client-config --config /etc/laminara/config.json \
  --endpoint https://eu.launcher.example.com \
  --endpoint https://us.launcher.example.com > laminara.client.json

cd client
./build-launcher.sh ../laminara.client.json --target windows
./build-launcher.sh ../laminara.client.json --target linux
```

`client-config` отдаёт список адресов по порядку (лаунчер проверяет их и берёт ближайший
живой, а при отказе переключается), всё кольцо ключей подписи, соль машинного отпечатка и
оформление с картинками. Готовый файл раздаёте игрокам — при первом запуске он берёт всё из
запечённой конфигурации, игроку остаётся только войти.

Без `LAMINARA_CLIENT_CONFIG` сборка возьмёт `client/src-tauri/laminara.client.default.json` —
это localhost для разработки, не для игроков. Подробности: [сборка
лаунчера](https://docs.laminara.dev/launcher/building/).

## Вход в игре

Игра запускается с authlib-injector, направленным на `https://<домен>/yggdrasil/`. Пароль
проверяется тем же адаптером аккаунтов, что и в лаунчере; Laminara выдаёт игровые токены и
подписывает текстуры скинов (RSA-SHA1). Откуда брать скины, задаёт адаптер — `template`,
`json` или `sql`.
