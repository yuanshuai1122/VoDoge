# VoDoge

[![Go](https://img.shields.io/badge/Go-1.26%2B-00ADD8?logo=go)](go.mod)
[![License](https://img.shields.io/badge/License-Proprietary-red.svg)](LICENSE)

**English** · [简体中文](README.zh-CN.md)

An **SMS hub** for a rack of USB cellular modules on one machine: domestic and
international lines kept apart, threads and sending in the browser, eSIM profiles
switchable from the same page.

Repository: [github.com/yuanshuai1122/VoDoge](https://github.com/yuanshuai1122/VoDoge)

This is **not** a "one global SIM" product and **not** a proxy pool. Proxying and VoWiFi
are here, but they rank behind SMS. Chinese SIMs are never blocklisted.

## What it does

| | |
|---|---|
| Multiple USB modules | Added and rebound **by IMEI**, never by a hardcoded `cdc-wdmN` — USB renumbers on replug. Up to 5 by default, 1–10 in settings |
| Domestic / international lanes | Each device is tagged `lane=cn` or `lane=intl` **by hand**. Never inferred from MCC |
| SMS | Threaded per ICCID. An international line that is registered sends over cellular and falls back to IMS; a domestic line with VoWiFi on sends over IMS only. Global rolling hourly send cap (default 20) |
| eSIM | List / switch / disable / download profiles on the card in the module. Writing a plan uses a USB reader; the module only ever sees "the USIM currently enabled" |
| Card reader | Discovery and writing via `pcscd`, no CGO. A reader and a module must never hold the same card at once |
| Notifications | New SMS goes out over Telegram / Bark and friends — not web push |
| Web UI | Installable as a PWA (needs HTTPS). The service worker caches the shell only, never `/api`. Dark / light toggle, **English and Chinese** |
| Plugins | Sidebar pages and local backends installed from a zip. They run with backend privileges — install only what you trust |

Optional: per-device SOCKS/HTTP egress, upstream proxies bound per ICCID or per country
(domestic lines still egress straight out of the module), local self-signed HTTPS, and
LAN-only access to the admin UI.

## Supported hardware

The product covers Quectel **EC20 / EC25 / EG25** USB modules. The UFI103S in the lab is
**explicitly out of scope**.

| Hardware | Role | Software | On real hardware |
|---|---|---|---|
| EC25-CN | Domestic SMS | Done | Awaiting the stick, then send/receive acceptance |
| EG25-G | International SMS + IMS | Done | Awaiting the stick |
| EC20 | Spare, cellular SMS | Done | Manage and send is enough |
| USB CCID reader | eSIM writing, plus VoWiFi / AKA | On `pcscd` | Needs a reader + `pcscd` |

The data plane is **QMI**, not RNDIS. One process. See
[docs/hardware-support.md](docs/hardware-support.md).

## Quick start

One-line install — prefers Docker Compose and pulls the GHCR image
`ghcr.io/yuanshuai1122/vodoge`:

```bash
curl -fsSL https://raw.githubusercontent.com/yuanshuai1122/VoDoge/main/scripts/install.sh | bash
```

If you already have the repository checked out:

```bash
cp config/config.example.yaml config/config.yaml
docker compose up -d
```

- Admin UI at `http://127.0.0.1:7575`. Default credentials are in the `web` block of the
  config — change them the moment you log in.
- Compose ships PostgreSQL; the backend connects via `VODOGE_DB_DSN` (`DATABASE_URL` also
  works).
- On first run the installer generates a random PostgreSQL password and stores it `0600`
  at `${VODOGE_DIR:-/opt/vodoge}/.postgres-credentials`. Re-running reuses it.
- Only LAN access is allowed by default. Going public means changing the network policy in
  settings **and** putting HTTPS in front.

Without a reachable PostgreSQL the process exits. There is no SQLite.

### Local development

```bash
docker compose up -d postgres
go run ./cmd/vodoge -c config/config.yaml
npm install --prefix web && npm run dev --prefix web
```

The frontend serves on `:3000` and rewrites `/api/*` to `:7575`. The backend has **no
global CORS** — don't call it cross-origin.

A production binary needs the frontend built and embedded first:

```bash
make frontend-dist
go build -o vodoge ./cmd/vodoge
```

### Verifying

```bash
node scripts/smoke-api.mjs          # login / devices / SMS / proxy
bash scripts/ci.sh                  # local pipeline; tagging v* runs GitHub Actions release
bash scripts/testdb.sh ensure       # throwaway test database
```

**Never** point `TEST_DATABASE_URL` at a database serving real traffic — the tests
truncate every table.

Back up with `pg_dump`:

```bash
docker exec vodoge-postgres pg_dump -U vodoge vodoge > vodoge-$(date +%F).sql
```

## Documentation

| Document | Contents |
|---|---|
| [docs/architecture.md](docs/architecture.md) | How the code is laid out and where to change things |
| [docs/hardware-support.md](docs/hardware-support.md) | Modules, SMS paths, card readers, plugins |
| [docs/frontend-api-matrix.md](docs/frontend-api-matrix.md) | The HTTP contract |
| [docs/pve-lab-deploy.md](docs/pve-lab-deploy.md) | PVE lab deployment |
| [docs/README.md](docs/README.md) | Index of everything else |

> The design documents under `docs/` are written in Chinese. This README and the web UI
> are available in both English and Chinese.

## Disclaimer

- This software drives low-level cellular hardware. Hardware, carrier-billing and network
  risks are yours to carry.
- Not affiliated with or endorsed by Quectel or Qualcomm.
- Follow your local laws and your carrier's terms of service. Unlawful use is prohibited.
- Provided as is, without warranty of any kind.

## License

Copyright (c) 2026 yuanshuai1122. Proprietary software — see [LICENSE](LICENSE).
