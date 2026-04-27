# Clonarr

![GitHub Release](https://img.shields.io/github/v/release/ProphetSe7en/clonarr?label=latest) ![GitHub last commit](https://img.shields.io/github/last-commit/ProphetSe7en/clonarr/main?label=last%20commit)

**Support / questions:** [#clonarr](https://discordapp.com/channels/492590071455940612/1495685561552207913) on the [TRaSH Guides Discord](https://trash-guides.info/discord) (under Community Apps). **Report bugs:** [GitHub issues](https://github.com/prophetse7en/clonarr/issues).

A fully visual TRaSH Guides sync tool for Radarr and Sonarr. Browse, customize, and sync Custom Formats, Quality Profiles, Scores, and Quality Sizes — no YAML configs, no CLI, just a browser.

Build profiles from scratch or start from TRaSH templates, compare your Arr profiles against TRaSH to see what's missing or wrong, test how releases score in the Scoring Sandbox, track every change with sync history and rollback, and sync to multiple Radarr and Sonarr instances. Auto-sync keeps your profiles in sync when TRaSH Guides updates, with Discord and Gotify notifications.

Free, open source, and self-hosted.

## Preview

![Clonarr Preview](docs/images/clonarr-preview.gif)

## Features

### Profile Sync
- Browse all TRaSH Quality Profiles (SQP-1 through SQP-5, HD Bluray, UHD Remux, Anime, language-specific, and more)
- Sync profiles to Radarr/Sonarr — creates quality groups, sets cutoff, applies CF scores
- **Create** new profiles or **Update** existing ones with dry-run preview
- **Sync behavior rules** (Add/Remove/Reset) — control how sync handles missing CFs, score overrides, and removed CFs
- **Override system** — customize language (Radarr), scores, cutoff, quality items, and upgrades per-instance without modifying the TRaSH profile
- **Auto-sync** — automatically sync when TRaSH Guides updates, with Discord and Gotify notifications
- **Sortable sync rules** — sort by TRaSH Profile or Arr Profile name

### Compare
- Compare your Arr profiles against TRaSH Guides side-by-side
- **Table layout** for Required CFs and CF Groups — see current vs TRaSH values at a glance
- **Profile Settings comparison** — Language, Upgrade Allowed, Min/Cutoff scores
- **Filter chips** — All / Only diffs / Wrong score / Missing / Extra / Matching
- **Golden Rule picker** — automatically selects the correct HD/UHD variant based on what's in use
- **Per-card Sync selected** — choose which changes to apply per section (not all-or-nothing)
- **Score override badges** — shows when a score difference is intentional (from your sync rule overrides)
- **Toggle all** per card header for quick select/deselect

### Sync History & Rollback
- **History tab** — dedicated change log for all synced profiles between TRaSH Sync and Compare
- **Ring-buffer storage** — last 10 change events per profile (syncs with no changes don't create entries)
- **CF set-diff tracking** — catches all CF changes including score-0 CFs (group enable/disable)
- **Detailed change log** — CFs added/removed, scores before/after, quality items toggled, settings changed
- **Sortable columns** — TRaSH Profile, Arr Profile, Last Changed, Events
- **Rollback** — restore a profile to a previous state with one click. Auto-disables auto-sync to prevent overwrite

### Custom Formats
- Browse all TRaSH Custom Formats organized by category (Audio, HDR, Streaming, Unwanted, etc.)
- Create and update CFs with spec-level comparison
- **CF Creator** — build custom CFs with regex specs, test patterns, and TRaSH-compatible scoring

### Profile Builder
- Build custom profiles from scratch or start from a TRaSH template
- **Init card with tabs** — choose between TRaSH template or import from Arr instance
- **General + Quality cards** — matching the Edit view's visual language with blue/purple stripes
- **Import from instance** — pulls all CFs including score-0 extras via sync history, resolves custom CFs
- **Shared Quality Items editor** — drag-and-drop quality ordering and grouping (same editor as Edit view)
- **TRaSH group system** — formatItems (mandatory CFs) + CF groups (optional, toggleable)
- **Three-state CF pills** — Req (required in group), Opt (optional in group), Fmt (in formatItems)
- **Golden Rule and Miscellaneous** variant dropdowns as sub-section in Quality card
- **Export** — TRaSH JSON (strict official format) + optional group includes snippets + Recyclarr YAML (v7/v8)
- **Import** — Recyclarr YAML, TRaSH JSON, Clonarr backup, Arr instance profiles

### Scoring Sandbox
- Test how releases score against any profile — paste release names or search via Prowlarr
- Compact table with matched CFs, quality, group, score, and PASS/FAIL per release
- **Custom Prowlarr search categories** — configurable per app type for indexers that don't cascade root IDs
- **Numeric release group fallback** — correctly parses trailing numeric groups (e.g. `-126811`)
- **Per-row selection and filter** — check rows and filter to selected subset
- **Drag reorder** — manually sort the release list
- **Copy-box modal** — shareable plain-text summary per release with title, CFs, and scores
- **Profile comparison** — score the same releases against two profiles side-by-side
- **Score editor** — temporarily modify CF scores and add/remove CFs to test changes
- **Language CFs excluded** — language-aware CFs stripped from scoring (Parse API can't evaluate without TMDB context)
- Sortable columns (score, quality, group, status)

### Profile Detail & Edit
- **General + Quality cards** with per-section override toggles (blue/purple stripe design)
- **Inline Quality Items editor** — expand inside the Quality card with drag-and-drop grouping
- **Override summary bar** — shows active overrides with per-section reset
- **Sonarr language handling** — language field hidden for Sonarr (removed in Sonarr v4)

### Quality Size & File Naming
- Sync TRaSH quality size recommendations to your instance
- Per-quality custom overrides with auto-sync option
- Browse and apply TRaSH naming schemes (movies + series)

### Settings
- **Sidebar layout** — left navigation with sections: Instances, TRaSH Guides, Prowlarr, Notifications, Display, Advanced
- Settings for Prowlarr connection, search categories, auto-sync, Discord/Gotify notifications, and debug logging

### Maintenance
- Instance comparison — see how your instance differs from TRaSH
- Orphaned score cleanup
- Bulk CF deletion with keep-list
- Backup and restore profiles + CFs

### Other
- **Browser navigation** — back/forward buttons work (URL hash routing with History API)
- **TRaSH changelog** — clickable dropdown in header showing recent guide updates
- **Discord notifications** — auto-sync results and TRaSH repo update summaries
- **Gotify notifications** — push notifications with configurable priority levels
- **Developer mode** — TRaSH JSON export, trash_id generation, score set editing
- **Multi-instance** — manage multiple Radarr and Sonarr instances
- **Dynamic language support** — all languages from your Arr instance available in dropdowns

New to Clonarr? See the [Getting Started guide](docs/GETTING-STARTED.md) for a step-by-step walkthrough with screenshots.

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

1. Open `http://your-host:6060` — you'll be redirected to `/setup` on first run to create an admin account (see [Authentication](#authentication) below)
2. After login, go to **Settings** and add your Radarr/Sonarr instance (URL + API key)
3. Click **Pull** in the header to clone the TRaSH Guides repository
4. Browse profiles on the **Radarr** or **Sonarr** tab and click **Sync** to deploy

The TRaSH repository is cloned to `/config/data/trash-guides/` and updated automatically (default: every 24 hours).

## Docker

### Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `TZ` | No | `UTC` | Container timezone |
| `PUID` | No | `99` | User ID for file ownership |
| `PGID` | No | `100` | Group ID for file ownership |
| `PORT` | No | `6060` | Web UI port (inside container) |
| `TRUSTED_NETWORKS` | No | *(empty — uses Radarr-parity defaults)* | Lock **Trusted Networks** at host level. Comma-separated IPs/CIDRs (`192.168.86.0/24, 10.66.0.0/24`). When set, the UI field is disabled and cannot be changed via the web interface — only by editing the template and restarting. Useful for defense-in-depth: prevents a UI-takeover attacker from expanding the trust boundary. |
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

**Install via Community Apps:** Search for **clonarr** in the Apps tab — click Install and configure your settings.

**Or install manually:** Go to **Docker** → **Add Container**, set Repository to `ghcr.io/prophetse7en/clonarr:latest`, and add the required paths and ports (see above).

The Web UI is available at `http://your-unraid-ip:6060`. Config is stored in `/mnt/user/appdata/clonarr` by default.

**Updating:** Click the Clonarr icon in the Docker tab and select **Force Update** to pull the latest image.

## How It Works

Clonarr clones the [TRaSH Guides](https://github.com/TRaSH-Guides/Guides) repository and parses all Custom Format definitions, quality profiles, CF groups, and scoring data. It then provides a web UI to browse, customize, and sync this data to your Radarr/Sonarr instances via their v3 API.

```
TRaSH Guides repo (git clone)
  → Go backend parses CF/profile/group JSON
    → REST API (40+ endpoints)
      → Alpine.js SPA
        → Sync: dry-run plan → apply (CF create/update + profile create/update)
```

Config is stored in `/config/clonarr.json`. Profiles are stored as individual JSON files in `/config/profiles/`.

## Acknowledgments

Clonarr is built on the work of several projects:

- **[TRaSH Guides](https://trash-guides.info/)** — All Custom Format data, quality profiles, scoring systems, and naming schemes. Clonarr is a frontend for TRaSH's guide data.
- **[Recyclarr](https://github.com/recyclarr/recyclarr)** — YAML import/export format compatibility (v7 + v8). Clonarr can import and export Recyclarr-compatible configs.
- **[Notifiarr](https://notifiarr.com/)** — Inspiration for the sync behavior rules (Add/Remove/Reset) and profile sync workflow.
- **[Radarr](https://radarr.video/) / [Sonarr](https://sonarr.tv/)** — API v3 integration for CF management, profile sync, quality sizes, naming, and the Parse API used by the Scoring Sandbox.

## Authentication

As of `v2.0.6`, Clonarr requires a login before you can reach the web UI. The model mirrors Radarr/Sonarr's Security panel:

**Authentication** — how users log in:
- **Forms (login page)** *(default)* — standard username/password form + session cookie (30-day TTL).
- **Basic** — HTTP Basic Auth (browser popup). Use this only when a reverse proxy in front is already handling login.
- **None** — disables auth entirely. **Requires password confirmation to enable** because the blast radius is catastrophic: anyone who reaches the port is admin. Only safe on a host not reachable from other devices.

**Authentication Required** — who must log in:
- **Disabled for Trusted Networks** *(default)* — devices on the "trusted" CIDR list skip the login page. Convenient for LAN-only deployments.
- **Enabled (all traffic)** — every request needs credentials, even from your own LAN.

### First-run setup

1. Open `http://your-host:6060` — you'll be redirected to `/setup`
2. Create an admin username and password (min 10 characters, 2+ of upper/lower/digit/symbol)
3. You're logged in automatically and land in the main UI

Credentials are bcrypt-hashed (cost 12) and stored in `/config/auth.json`. Sessions persist across container restarts via `/config/sessions.json`.

### Trusted Networks

By default "trusted" means all private IPv4 + IPv6 ranges (RFC1918, link-local, ULA, loopback — Radarr-parity). **Anything in this list gets full admin access without a password** — that includes every other container on your Docker host and every device on your home WiFi.

To tighten: go to **Settings → Security** and list specific IPs/CIDRs:
- `192.168.86.0/24` — whole home VLAN
- `10.66.0.0/24` — WireGuard tunnel
- `192.168.86.22/32` — a single device

Loopback (`127.x`) is always trusted so Docker healthchecks work regardless of this list.

**Host-level lockdown:** set the `TRUSTED_NETWORKS` env var in your Unraid template or `docker-compose.yml`. When set, the UI field is disabled — the trust boundary can only be changed by editing the template and restarting the container. Defends against UI-takeover attackers (session hijack, XSS, local-bypass peer). See [Environment Variables](#environment-variables) above.

### API Key

Every install gets an API key (visible in **Settings → Security**, rotatable). Send it on requests as:

```
X-Api-Key: <your-key>
```

or as a query parameter (legacy — leaks to access logs and browser history):

```
?apikey=<your-key>
```

Use this for Homepage widgets, Uptime Kuma, and scripts. API-key auth bypasses both the login requirement and CSRF protection.

### Reverse-proxy deployment

Behind SWAG / Authelia / Traefik / Caddy that terminates TLS:
1. Set **Trusted Proxies** to the proxy's IP (or use `TRUSTED_PROXIES` env var to lock at host level).
2. Ensure the proxy sends `X-Forwarded-For` and `X-Forwarded-Proto: https`.
3. Pick either **Forms** (Clonarr handles login) or **Basic** (reverse proxy handles login upstream).

Clonarr will only trust `X-Forwarded-*` headers when the direct peer IP matches a configured Trusted Proxy — prevents header spoofing from other containers on the same bridge network.

### Lost password recovery

No email reset flow — by design, `/config/auth.json` is authoritative. To recover:

1. Stop the container
2. Delete `/config/auth.json` (credentials only — profiles, sync history, TRaSH data all live elsewhere)
3. Start the container
4. Open the web UI — you'll be redirected to `/setup` again to create new credentials

This is safe on a machine where you have `/config` access. If someone ELSE can delete that file, they can also take over your Clonarr — which is expected behavior for a local admin tool.

## Security Notes

- Radarr/Sonarr instance API keys, Discord webhooks, Gotify tokens, and Pushover credentials are stored in plaintext in `/config/clonarr.json`. Protect that file the same way you protect Radarr/Sonarr's `config.xml`.
- Admin credentials are bcrypt-hashed in `/config/auth.json` — never in plaintext on disk.
- All state-changing API calls are CSRF-protected (double-submit cookie). Scripts using the API key bypass CSRF automatically.
- The app sets `X-Frame-Options: DENY` (prevents clickjacking), `X-Content-Type-Options: nosniff`, and `Referrer-Policy: same-origin`.
- Discord and Pushover outbound calls run through an SSRF-safe HTTP client that refuses internal IP targets with per-request revalidation (defeats DNS rebinding). Gotify uses a plain client since self-hosted Gotify on LAN is legitimate.

## Disclaimer

While Clonarr has been tested extensively, it may contain bugs that could affect your Radarr/Sonarr configuration. Always use **Dry Run** before applying sync changes, and keep backups of your Arr instances.

The authors are not responsible for any unintended changes to your media automation setup. **Use at your own risk.**

## Support

For questions, help, or bug reports:

- **Discord:** [`#prophetse7en-apps`](https://discordapp.com/channels/492590071455940612/1486391669384417300) on the [TRaSH Guides Discord](https://trash-guides.info/discord) (under Community Apps)
- **GitHub:** [prophetse7en/clonarr/issues](https://github.com/prophetse7en/clonarr/issues)

## License

MIT
