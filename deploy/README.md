# Deploying Laminara

The server is a single static Go binary (`laminara-server`) that runs headless and exposes a control
console over a Unix socket. It serves one public TCP listener (launcher API + object bytes + in-game
Yggdrasil auth); put nginx in front of it for TLS.

## Layout
- `Dockerfile` — multi-stage build → `debian:12-slim` runtime (glibc, so the downloaded Mojang JRE can
  run the Forge/NeoForge installer processors).
- `docker-compose.yml` — server + Redis; commented Postgres / S3 (Garage) for scale.
- `systemd/laminara-server.service` — `Type=notify` unit (the daemon sends sd_notify).
- `nginx/laminara.conf` — TLS reverse proxy + zero-copy object serving via `X-Accel-Redirect`.
- `config.example.json` — full config (auth · storage · build · api · yggdrasil).

## Quick start (Docker)
```sh
make generate                     # generate gen/ (protobuf) before building the image
cp deploy/config.example.json deploy/config.json   # then edit
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

## nginx + TLS
Point `nginx/laminara.conf` at your domain, obtain a certificate with certbot, and reload. The
`/internal-objects/` location must `alias` the fs storage root and stays `internal`; the server sets
`X-Accel-Redirect` (enabled by `api.xAccel: true`) so nginx streams object bytes with sendfile.
For S3 storage the server instead redirects to a presigned URL and nginx is not on the byte path.

## Operating the server
Attach to the running daemon's console (same host):
```sh
laminara-server status
laminara-server console          # interactive
laminara-server exec versions 1.21
laminara-server exec loaders 1.21.1
laminara-server exec prepare <name> <mcVersion> loader=<vanilla|fabric|quilt|forge|neoforge> [loaderVersion=..] [platform=windows-x64]
laminara-server exec publish <name>
```
`prepare` builds a ready-to-run client into `build.profilesDir/<name>` (editable files); `publish`
snapshots it into a signed manifest the launcher fetches. New MC/loader versions are picked up from
upstream automatically — no redeploy.

## Branded launcher (baked-in server)

Players should never type a server address — the launcher you hand them already knows your
server(s) and trusts your signing key. Generate the client config from the running server, then
build the launcher with it embedded:

```sh
laminara-server client-config --config /etc/laminara/config.json \
  --endpoint https://eu.play.example.com \
  --endpoint https://us.play.example.com  > laminara.client.json

# build the desktop launcher with your config baked in
LAMINARA_CLIENT_CONFIG=$PWD/laminara.client.json  (cd client && pnpm tauri build)
```

`client-config` emits the ordered endpoint list (players' launchers probe them and pick the
nearest healthy one, with failover) plus `serverPublicKeyHex` (used to verify every signed
manifest). Ship the resulting binary; a first run bootstraps from the embedded config and the
player only logs in. Without `LAMINARA_CLIENT_CONFIG` the build embeds
`client/src-tauri/laminara.client.default.json` (localhost, for development).

## In-game auth (authlib-injector)
Launch the game with authlib-injector pointing at `https://<domain>/yggdrasil/`. Laminara verifies
credentials through the same auth adapter, issues in-game tokens, and signs skin textures (RSA-SHA1)
served by the configured skin adapter (`template` or `json`).
