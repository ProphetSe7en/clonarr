# Clonarr

![GitHub Release](https://img.shields.io/github/v/release/ProphetSe7en/clonarr?label=latest) ![GitHub last commit](https://img.shields.io/github/last-commit/ProphetSe7en/clonarr/main?label=last%20commit)

**Support / questions:** [#clonarr](https://discordapp.com/channels/492590071455940612/1495685561552207913) on the [TRaSH Guides Discord](https://trash-guides.info/discord) (under Community Apps). **Report bugs:** [GitHub issues](https://github.com/prophetse7en/clonarr/issues).

A fully visual TRaSH-Guides sync tool for Radarr and Sonarr. Browse, customize, and sync Custom Formats, Quality Profiles, Scores, and Quality Sizes. No YAML configs, no CLI, just a browser.

Build profiles from scratch or start from TRaSH-Guides templates. Compare your Arr profiles against TRaSH-Guides to spot what's missing or incorrect. Test how releases score in the Scoring Sandbox. Track every change with sync history and rollback. Sync to multiple Radarr and Sonarr instances.

Auto-sync keeps your profiles up to date when TRaSH-Guides updates or when a profile is edited directly in Radarr/Sonarr. You choose what happens when a change is detected: apply automatically, just get notified so you can review and apply manually, or wait a set delay before applying. Notifications via Discord, Gotify, Pushover, ntfy, and Apprise.

Free, open source, and self-hosted.

## Preview

![Clonarr Preview](docs/images/clonarr-preview.gif)

## Features

Navigation is grouped into **Library**, **Tools**, and **App** (shown as a left sidebar by default, or a top bar if you prefer; set it under Settings > Display). Here is what each part does, in plain English.

### Library

**Profiles**
- **Sync a profile** - push a TRaSH quality profile into Radarr or Sonarr. Create a new profile or update an existing one, with a dry-run preview that shows exactly what will change before anything is written.
- **Customize a profile** - before syncing, add or remove custom formats, change scores, and adjust quality items, language, cutoff, and upgrade settings. Your changes live on the sync rule; the underlying guide profile is never modified.
- **Auto-sync** - keep profiles up to date automatically. You pick what happens when a change is detected: **Apply automatically** writes to Arr right away, **Just notify me** sends a notification and shows an "updates available" badge so you review and apply on your own, **Wait before applying** detects now and applies after a delay you set. Detection covers both guide updates and "drift" when someone edits a profile directly in Radarr/Sonarr. Auto-sync can be paused per instance, and individual rules with auto-sync off still get the updates flag.
- **Clone a sync rule** - copy a configured profile, with all its customizations, under a new name, into the same instance or another one of the same type.
- **Sync history and rollback** - a change log per profile (custom formats added/removed, scores before and after, quality and setting changes), with one-click rollback to a previous state.
- **Compare** - view your Arr profile against the guide profile side by side, so you can see what is missing, wrong, or extra.

**Custom Formats**
- Browse every custom format in the guide, grouped by category, with conditions and descriptions.
- Create or import your own custom formats, clone existing ones, and organize them into your own categories.
- Use any custom format, guide or your own, in a profile via **Customize this profile > Additional CF**.

**Media Management**
- **Quality Definitions** - sync the guide's recommended quality sizes (min/preferred/max per quality) to your instance, or set your own per-quality values.
- **File Naming** - browse and apply the guide's recommended folder and file naming schemes, including Plex, Emby, and Jellyfin variants.

### Tools

- **Maintenance** - back up and restore your profiles and custom formats, and clean up: remove orphaned scores, bulk-delete custom formats with a keep-list, find unused quality profiles, and compare an instance against the guide.
- **Advanced** (developer options, off by default) - a **Profile Builder** to build a profile from scratch or import one from Recyclarr, an Arr instance, or a backup; a **Scoring Sandbox** to test how releases score against a profile (paste release names or search through Prowlarr); and a **CF Group Builder**. These are power-user and developer tools, hidden behind a toggle.

### App

- **Notifications** - send events to Discord, Gotify, Pushover, ntfy, and Apprise. For each agent you pick which events it receives: sync succeeded, sync failed, cleanup, repository updated, new changelog, TRaSH updates available, drift detected, and drift reconciled.
- **Display** - theme (system/light/dark), UI scale (compact/default/large), content alignment (centered or left-aligned), and navigation style (left sidebar or top bar). Times are shown in your own locale.
- **Security** - configurable: require login always or only from outside your local network, mark trusted networks and proxies, use an API key, host behind a reverse-proxy subpath, and a login brute-force rate limit. See [Authentication](#authentication) below for details.
- **Multiple instances** - manage as many Radarr and Sonarr instances as you like, switching between them from the top of the app.

Browser back and forward navigation work throughout (each section and tab has its own URL).

New to Clonarr? See the [Getting Started guide](docs/GETTING-STARTED.md) for a quick step-by-step walkthrough.

## Quick Start

### 1. Run with Docker

```bash
docker run -d \
  --name clonarr \
  --restart unless-stopped \
  -p 6060:6060 \
  -v /path/to/config:/config \
  -e TZ=Europe/Oslo \
  ghcr.io/prophetse7en/clonarr:latest
```

Open the Web UI at `http://your-host:6060`.

### 2. Initial Setup

1. Open `http://your-host:6060` - you'll be redirected to `/setup` on first run to create an admin account (see [Authentication](#authentication) below)
2. After login, go to **Settings** and add your Radarr/Sonarr instance (URL + API key)
3. Click **Pull** in the header to clone the TRaSH Guides repository
4. Go to **Profiles > TRaSH Profiles**, pick a profile, and click **Use profile** to configure and sync it

The TRaSH repository is cloned to `/config/data/trash-guides/` and updated automatically (default: every 24 hours).

## Docker

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `TZ` | No | `UTC` | Container timezone |
| `PUID` | No | `99` | User ID for file ownership |
| `PGID` | No | `100` | Group ID for file ownership |
| `PORT` | No | `6060` | Web UI port (inside container) |
| `CONFIG_DIR` | No | `/config` | Persistent config root. If changed, mount the same path as a volume. |
| `DATA_DIR` | No | `${CONFIG_DIR}/data` | TRaSH Guides cache/data directory. Usually leave this under `CONFIG_DIR`. |
| `URL_BASE` | No | *(empty)* | URL path prefix for reverse proxy subpath hosting, such as `/clonarr`. Can also be set in **Settings → URL Base**. When set to a path, it overrides and locks the Settings field. Empty or `/` means no override. |
| `TRUSTED_NETWORKS` | No | *(empty - uses Radarr-parity defaults)* | Lock **Trusted Networks** at host level. Comma-separated IPs/CIDRs (`192.168.0.0/24, 10.0.0.0/24`). When set, the UI field is disabled and cannot be changed via the web interface - only by editing the template and restarting. Useful for defense-in-depth: prevents a UI-takeover attacker from expanding the trust boundary. |
| `TRUSTED_PROXIES` | No | *(empty)* | Lock **Trusted Proxies** at host level. Comma-separated IPs. Same UI-disabled behavior as `TRUSTED_NETWORKS`. Only needed when Clonarr sits behind a reverse proxy that terminates TLS (SWAG, Authelia, Traefik). |

### Volumes

| Container Path | Purpose |
|---------------|---------|
| `/config` | Configuration, profiles, sync history, and TRaSH Guides cache |

### Ports

| Port | Purpose |
|------|---------|
| `6060` | Web UI |

### Docker Compose

```yaml
services:
  clonarr:
    image: ghcr.io/prophetse7en/clonarr:latest
    container_name: clonarr
    restart: unless-stopped
    ports:
      - "6060:6060"
    environment:
      - TZ=Europe/Oslo
      - PUID=99
      - PGID=100
    volumes:
      - ./clonarr-config:/config
```

### Building from Source

```bash
git clone https://github.com/prophetse7en/clonarr.git
cd clonarr
docker build -t clonarr .
docker run -d --name clonarr -p 6060:6060 \
  -v ./config:/config clonarr
```

### Healthcheck

The container includes a built-in healthcheck that verifies the web UI and TRaSH data status. Docker (and platforms like Unraid/Portainer) will show the container as healthy when the API is responsive.

### Unraid

**Install via Community Apps:** Search for **clonarr** in the Apps tab - click Install and configure your settings.

**Or install manually:** Go to **Docker** → **Add Container**, set Repository to `ghcr.io/prophetse7en/clonarr:latest`, and add the required paths and ports (see above).

The Web UI is available at `http://your-unraid-ip:6060`. Config is stored in `/mnt/user/appdata/clonarr` by default.

**Updating:** Click the Clonarr icon in the Docker tab and select **Force Update** to pull the latest image.

## How It Works

Clonarr clones the [TRaSH Guides](https://github.com/TRaSH-Guides/Guides) repository and parses all Custom Format definitions, quality profiles, CF groups, and scoring data. It then provides a web UI to browse, customize, and sync this data to your Radarr/Sonarr instances via their v3 API.

```
TRaSH Guides repo (git clone)
  → Go backend parses CF/profile/group JSON
    → REST API (40+ endpoints)
      → Alpine.js SPA assembled from embedded partials
        → Sync: dry-run plan → apply (CF create/update + profile create/update)
```

By default, config is stored in `/config/clonarr.json`. Profiles are stored as individual JSON files in `/config/profiles/`.

## Acknowledgments

Clonarr is built on the work of several projects:

- **[TRaSH Guides](https://trash-guides.info/)** - All Custom Format data, quality profiles, scoring systems, and naming schemes. Clonarr is a frontend for TRaSH's guide data.
- **[Recyclarr](https://github.com/recyclarr/recyclarr)** - YAML import compatibility. Recyclarr YAML import works (including v7 configs for migration) and include files are supported. Recyclarr YAML export is currently paused while we verify the output against current Recyclarr docs (v8-only - v7 generation has been dropped).
- **[Notifiarr](https://notifiarr.com/)** - Inspiration for the sync behavior rules (Add/Remove/Reset) and profile sync workflow.
- **[Radarr](https://radarr.video/) / [Sonarr](https://sonarr.tv/)** - API v3 integration for CF management, profile sync, quality sizes, naming, and the Parse API used by the Scoring Sandbox.

## Authentication

As of `v2.0.6`, Clonarr requires a login before you can reach the web UI. The model mirrors Radarr/Sonarr's Security panel:

**Authentication** - how users log in:
- **Forms (login page)** *(default)* - standard username/password form + session cookie (30-day TTL).
- **Basic** - HTTP Basic Auth (browser popup). Use this only when a reverse proxy in front is already handling login.
- **None** - disables auth entirely. **Requires password confirmation to enable** because the blast radius is catastrophic: anyone who reaches the port is admin. Only safe on a host not reachable from other devices.

**Authentication Required** - who must log in:
- **Disabled for Trusted Networks** *(default)* - devices on the "trusted" CIDR list skip the login page. Convenient for LAN-only deployments.
- **Enabled (all traffic)** - every request needs credentials, even from your own LAN.

### First-run setup

1. Open `http://your-host:6060` - you'll be redirected to `/setup`
2. Create an admin username and password (min 10 characters, 2+ of upper/lower/digit/symbol)
3. You're logged in automatically and land in the main UI

Credentials are bcrypt-hashed (cost 12) and stored in `${CONFIG_DIR}/auth.json` (default `/config/auth.json`). Sessions persist across container restarts via `${CONFIG_DIR}/sessions.json`.

### Trusted Networks

By default "trusted" means all private IPv4 + IPv6 ranges (RFC1918, link-local, ULA, loopback - Radarr-parity). **Anything in this list gets full admin access without a password** - that includes every other container on your Docker host and every device on your home WiFi.

To tighten: go to **Settings → Security** and list specific IPs/CIDRs:
- `192.168.0.0/24` - whole home VLAN
- `10.0.0.0/24` - WireGuard tunnel
- `192.168.0.5/32` - a single device

Loopback (`127.x`) is always trusted so Docker healthchecks work regardless of this list.

**Host-level lockdown:** set the `TRUSTED_NETWORKS` env var in your Unraid template or `docker-compose.yml`. When set, the UI field is disabled - the trust boundary can only be changed by editing the template and restarting the container. Defends against UI-takeover attackers (session hijack, XSS, local-bypass peer). See [Environment Variables](#environment-variables) above.

### API Key

Every install gets an API key (visible in **Settings → Security**, rotatable). Send it on requests as:

```
X-Api-Key: <your-key>
```

or as a query parameter (legacy - leaks to access logs and browser history):

```
?apikey=<your-key>
```

Use this for Homepage widgets, Uptime Kuma, and scripts. API-key auth bypasses both the login requirement and CSRF protection.

See [docs/homepage-widget.md](docs/homepage-widget.md) for a ready-to-paste [gethomepage](https://gethomepage.dev) config (`:dev` / `:preview` only - ships to `:latest` next release).

### Reverse-proxy deployment

Behind SWAG / Authelia / Traefik / Caddy that terminates TLS:
1. Set **Trusted Proxies** to the proxy's IP (or use `TRUSTED_PROXIES` env var to lock at host level).
2. Ensure the proxy sends `X-Forwarded-For` and `X-Forwarded-Proto: https`.
3. Pick either **Forms** (Clonarr handles login) or **Basic** (reverse proxy handles login upstream).

Clonarr will only trust `X-Forwarded-*` headers when the direct peer IP matches a configured Trusted Proxy - prevents header spoofing from other containers on the same bridge network.

#### Subpath hosting (e.g. `https://example.com/clonarr`)

If you want Clonarr at a path prefix instead of a subdomain, set **Settings → URL Base** to match the prefix your proxy uses and restart Clonarr. Then point the proxy at port 6060. This works the same way as Radarr/Sonarr's URL Base setting and needs no container changes, so it also works on platforms that pin `URL_BASE` in their app template.

To set it from the container instead (this locks the Settings field):

```yaml
environment:
  - URL_BASE=/clonarr
```

Example NGINX block:

```nginx
location /clonarr/ {
    proxy_pass http://clonarr-host:6060/clonarr/;
    proxy_set_header Host $host;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
}
```

Leading slash required, no trailing slash. Leave it empty for root-path access (default).

When a URL base is active, requests to `/` redirect to the base, and `/api/health` answers both at the root and under the base. Healthchecks and "open Web UI" links that don't know the base keep working.

### Lost password recovery

No email reset flow - by design, `${CONFIG_DIR}/auth.json` is authoritative. To recover:

1. Stop the container
2. Delete `${CONFIG_DIR}/auth.json` (default `/config/auth.json`; credentials only - profiles, sync history, TRaSH data all live elsewhere)
3. Start the container
4. Open the web UI - you'll be redirected to `/setup` again to create new credentials

This is safe on a machine where you have `/config` access. If someone ELSE can delete that file, they can also take over your Clonarr - which is expected behavior for a local admin tool.

## Security Notes

- **Login is required by default.** Admin password is bcrypt-hashed in `${CONFIG_DIR}/auth.json` - never plaintext.
- **Brute-force protection.** 5 failed logins from the same IP within a minute → blocked until a minute has passed since the oldest of those 5 attempts (HTTP 429 + `Retry-After`). Same on the setup and change-password endpoints. Failed attempts are logged with the source IP so fail2ban can pick them up if you want firewall-level bans.
- **Long passwords are welcome.** Minimum 10 characters; 16+ skips the upper/lower/digit/symbol class rule, so passphrases like `correct horse battery staple` pass.
- **Custom format names can't collide with TRaSH CFs.** Saving a custom CF with a name that matches a TRaSH guides CF (or another custom for the same app) is refused - sharing a name caused syncs to flip-flop the score back and forth. Names are case-sensitive (`PCOK` and `Pcok` are distinct), matching Radarr/Sonarr's own rule.
- **Secrets stay on disk.** Arr API keys, Discord/Gotify/Pushover/NTFY/Apprise tokens are stored in plaintext in `${CONFIG_DIR}/clonarr.json` (mode 0600). Protect that file the same way you protect Radarr/Sonarr's `config.xml`.
- **CSRF protection** on every state-changing request. Scripts authenticating with `X-Api-Key` bypass CSRF - that's intentional, key-holders are trusted.
- **Outbound webhooks check destinations.** Discord and Pushover (the two where users paste a URL from a third party) go through an HTTP client that refuses internal IPs and re-checks on every request - defeats DNS-rebinding attacks where a malicious webhook hostname secretly resolves to a LAN address. Self-hosted Gotify / ntfy / Apprise use a standard client (you control the destination).
- **Security headers** set on every response: `X-Frame-Options: DENY` (no embedding in iframes), `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin`.

See `SECURITY.md` for the full posture, supported-version policy, and how to report a vulnerability.

## Disclaimer

While Clonarr has been tested extensively, it may contain bugs that could affect your Radarr/Sonarr configuration. Always use **Dry Run** before applying sync changes, and keep backups of your Arr instances.

The authors are not responsible for any unintended changes to your media automation setup. **Use at your own risk.**

## Support

For questions, help, or bug reports:

- **Discord:** [`#prophetse7en-apps`](https://discordapp.com/channels/492590071455940612/1486391669384417300) on the [TRaSH Guides Discord](https://trash-guides.info/discord) (under Community Apps)
- **GitHub:** [prophetse7en/clonarr/issues](https://github.com/prophetse7en/clonarr/issues)

## Development

Clonarr is developed with active AI assistance (Claude, Anthropic) under human direction. Architectural decisions, code review, testing against real Radarr/Sonarr instances, and releases are done by ProphetSe7en. Issues and PRs go through a human review.

For maintainers working on the modular UI, manifest-driven settings, or Docker-first local testing flow, see [Development Notes](docs/DEVELOPMENT.md).

## License

MIT
