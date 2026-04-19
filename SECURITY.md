# Security Policy

## Supported versions

| Version | Security updates |
|---------|------------------|
| `v2.0.6` and later | ✅ Yes |
| Earlier `v2.x` releases | ❌ No — please upgrade |

## Reporting a vulnerability

**Please do NOT open a public GitHub issue for security bugs.** Even describing an attack path in a public forum before a fix ships puts other users at risk.

### Preferred channel

Email: **ProphetSe7en@users.noreply.github.com** with subject line `[Clonarr Security] <brief summary>`.

### Fallback

If email fails or you need pseudonymous submission, use GitHub's private **Report a vulnerability** link on the [repository security tab](https://github.com/ProphetSe7en/clonarr/security/advisories/new).

### What to include

- Clonarr version (from Settings → About, or `GET /api/version`)
- Clear reproduction steps (command + request body + expected vs actual response is ideal)
- Impact assessment — what data/access can the attacker obtain?
- Your disclosure timeline preference

### What to expect

- **Acknowledgement within 72 hours** of receipt (usually faster — solo maintainer, best-effort).
- **Triage and severity assessment within 7 days.** I'll confirm whether I accept the finding, classify severity, and propose a fix + disclosure timeline.
- **Fix within 14 days** for Critical/High findings. Medium/Low may take a release cycle.
- **Coordinated disclosure** — I'll ship a patched release first, then credit you in the CHANGELOG and this document (unless you prefer anonymity). Please do not publish details before the patch ships.

### How I handle reports

- Reporter credit in CHANGELOG + this document by default (anonymous on request).
- Honest acknowledgement when a report is valid — including in the CHANGELOG.
- Open to public discussion of a finding after the patch ships.

## Security model

Clonarr is a **local admin tool** for managing Radarr/Sonarr profile data. The design assumes:

- You control the host where it runs.
- You do not expose port 6060 directly to the internet without a reverse proxy.
- You protect `/config/` the same way you protect Radarr/Sonarr's `config.xml` (file permissions, backup encryption, LUKS on the host).

### What Clonarr does

- **Authentication required by default** (Forms mode, bcrypt cost 12). First-run setup wizard forces admin account creation — no default credentials.
- **CSRF protection** on all state-mutating endpoints (double-submit cookie pattern).
- **Security headers**: `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin`.
- **SSRF guards** on Discord + Pushover outbound HTTP (blocklisted internal IP ranges + per-request DNS revalidation).
- **Secret masking** in all API responses — Arr API keys, webhook URLs, Gotify/Pushover tokens. Empty-on-unchanged-edit preserves stored secrets on save.
- **Session persistence** to disk (atomic write, survives container restart).
- **File permissions**: `/config/clonarr.json` written with mode `0600`, `/config/auth.json` `0600` in dir `0700`.
- **X-Forwarded-For hardening**: only trusted when the direct peer IP matches a configured Trusted Proxy. Rightmost-non-trusted algorithm defeats leftmost-spoofing.
- **Env-var override** for trust-boundary config (`TRUSTED_NETWORKS`, `TRUSTED_PROXIES`) — pin at host level to defend against UI-takeover attackers.

### What Clonarr does NOT do (by design)

- **Encryption at rest of Arr API keys.** Stored plaintext in `/config/clonarr.json` (mode 0600). Same trust model as Radarr/Sonarr themselves — both of those also store their API keys plaintext in `config.xml`. If an attacker has read access to `/config/`, the app cannot meaningfully protect the data from them — any local keystore key would be readable from the same filesystem. **Planned for v2.0.7**: opt-in `CLONARR_SECRET_KEY` env-var for AES-GCM encryption of Arr API keys + notification tokens at rest. Backward-compatible (plaintext kept if env var unset). Useful against backup-disk exfiltration and container-escape scenarios where `/config` leaks but process environment doesn't. If you need this before v2.0.7 ships, open a GitHub issue — we'll prioritize.
- **Rate limiting on `/login`.** Delegated to the reverse proxy (fail2ban, CrowdSec, SWAG plugins). Basic-auth mode is a CPU-amplification vector without rate-limit — prefer Forms mode or front Clonarr with a proxy that handles auth.
- **Account lockout.** Same reasoning as rate limiting.
- **Audit log of admin actions.** The Docker event stream + reverse-proxy access logs cover request-level history. If you need per-action audit, a future release can add it — open an issue.
- **TLS termination.** Runs plain HTTP on port 6060. Use a reverse proxy (SWAG, Traefik, Caddy, NPM) for TLS — configure `TRUSTED_PROXIES` so `X-Forwarded-Proto: https` is honored for Secure cookies.

## Security audit trail

Clonarr's security implementation is backed by an internal trap catalogue (T1–T66) — every finding from past code reviews is preserved with the mitigation pattern and the reason it was flagged. This is a living internal document (not published to this repo) covering the full hardening arc: auth primitives, middleware wiring, sensitive-data redaction, CSRF, security headers, race conditions, info leakage, log injection, and supply-chain. Requests for access to specific trap rationale can be made via the disclosure email above.

Current CI: `go test -race ./...` + `govulncheck ./...` run on every push and PR against `main`.

## Changelog of security-relevant changes

See `CHANGELOG.md` — entries flagged **Security** or explicitly reference trap IDs (T1–T66).
