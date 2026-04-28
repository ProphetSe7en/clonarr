# Changelog

## v2.2.8

Fixes a bug from v2.2.6/v2.2.7: turning off a custom format group didn't stick — the group came back on after the next sync. v2.2.8 respects your choices again. What you tick or untick is exactly what gets synced.

Sorry for the rough 24 hours. v2.2.6 and v2.2.7 went out too quickly. Thanks for the reports and patience.

## v2.2.7

Follow-up to v2.2.6. v2.2.6 fixed the missing custom formats (BR-DISK, LQ, AV1, etc.) from reappearing on your profiles after the TRaSH update — but their scores were still being reset to 0, so the CFs were technically there but inactive (not blocking or boosting anything).

v2.2.7 finishes the job. After Force Update + a sync, the CFs come back with the correct scores, and the blocking/scoring behavior actually works again.

If you only upgraded to v2.2.6 and noticed your scores still looked off, this is the patch. Update + sync and you're set.

## v2.2.6

Hotfix for a regression caused by a TRaSH guide update on 2026-04-27.

### Fixed — custom formats that disappeared from profiles after the TRaSH update

TRaSH reorganised how a few common custom formats are linked to profiles. The CFs themselves still exist (BR-DISK, LQ, LQ (Release Title), Extras, AV1, HD/UHD Streaming Boost, Generated Dynamic HDR, Upscaled), but Clonarr didn't follow the new structure and dropped them from your profiles on the next sync. Scores got reset to 0, and the blocking/scoring behavior you expected was gone.

This release teaches Clonarr to follow the new structure, the same way Recyclarr does. Affected profiles include WEB-1080p, WEB-2160p, SQP-3 (Audio), SQP-4 (MA Hybrid), and the German variants — basically any profile where TRaSH attaches an "Unwanted Formats" or "HD/UHD Streaming Boost" group.

**What you do:** Update to v2.2.6 in Unraid (Force Update), then run a sync (Pull, or Sync now on the affected rules). The missing CFs come back with their correct scores. No manual rule editing.

If you saw CFs disappear, your existing files in Radarr/Sonarr aren't broken — only future grabs were affected (until the CFs are re-applied).

## v2.2.5

Two bug fixes and a couple of UX improvements around cleaning up after testing.

### New — Restore deleted profiles

When you delete a profile in Radarr or Sonarr, Clonarr no longer drops its saved settings for that profile. The sync rule shows up as **orphaned** in the History tab (amber row, "orphaned" badge, auto-sync turned off). Two actions:

- **Restore** — recreates the profile in Radarr/Sonarr with all the same custom formats, scores, quality settings and overrides you had. If a profile with that name already exists, you get prompted to use a different name.
- **Remove** — permanently delete the saved settings.

The orphaned row goes away by itself once Restore succeeds.

### Fixed — "Remove sync entry" looked broken

If you'd synced the same profile multiple times, clicking the red X to remove its sync history only removed one of the saved entries. The row reappeared, looking like the delete didn't work. Now one click clears the whole row.

### Fixed — toast spam when bulk-deleting profiles in Arr

If you deleted 20+ profiles in Radarr/Sonarr at once, Clonarr would stack 20+ yellow toasts that needed their own scrollbar. Now you get one toast per Arr instance with the first few names and a "+N more" count.

### Improved — Unused Custom Formats scan

The scan used to silently hide custom formats that Radarr/Sonarr can include in filenames (the "Use in renaming" flag — typically streaming-service tags like AMZN, NF, language tags, version tags). That made the scan less useful exactly when you needed it — after deleting profiles and wanting to clean up the leftover CFs.

Now the scan shows everything, with three tabs in the result:

- **All unmanaged** — every CF not used by any sync rule
- **Rename-tagged only** — the subset Radarr/Sonarr can include in filenames (yellow badge)
- **Managed by Clonarr** — read-only list of CFs your sync rules use, with the Arr profile each one belongs to

If your file-naming format includes the `{Custom Formats}` token, a blue info box explains what deleting rename-tagged CFs does to future filenames (existing files on disk are not affected; re-syncing a profile from TRaSH or Profile Builder brings them back). If the token isn't in your format, a green box notes those CFs are safe to delete.

When rename-tagged CFs are present, you get two delete buttons: **Delete safe only** (keeps the rename-tagged ones) or **Delete all** (subtler outline). Both ask for confirmation before running.

## v2.2.4

A bundle of user-reported fixes and small UX improvements.

### Fixed — CF group toggle now respects each CF's "default" flag

When you toggled a TRaSH custom-format group on (e.g. `[Unwanted] Unwanted Formats`), Clonarr used to add every CF in the group to the profile. TRaSH marks some CFs in each group as the recommended defaults — that flag was being ignored. So a group with 15 CFs but 11 marked default added all 15 instead of just the 11.

Now only the marked-default CFs are auto-included. The other ones are still visible and one-click toggleable from the group; you choose which ones to add. Required CFs remain mandatory when the group is enabled. The same logic applies to user-created CF groups in CF Group Builder.

### New — Maintenance → "Unused Custom Formats" cleanup

A new cleanup action under Maintenance that finds custom formats on a Radarr/Sonarr instance which aren't used by any Clonarr sync rule and aren't tagged for use in filenames. You review the list and pick what to delete; the existing Keep List still protects names you want to hold onto.

Heads-up: the scan assumes Clonarr is the only thing managing CFs on the instance. CFs added directly in Radarr/Sonarr's UI, or via Recyclarr / Notifiarr / other tools, will show up as "unused" because Clonarr doesn't know about them.

Two safety checks prevent the worst-case outcome:

- The scan refuses to run if TRaSH guide data hasn't been pulled yet. Without it, every TRaSH-source CF in your sync rules looks unmanaged and you'd be shown "delete all your TRaSH CFs". You get a clear error pointing you to Settings → TRaSH Guides instead.
- If a TRaSH profile in a sync rule was renamed or removed upstream, the scan also checks the last sync history for that rule so previously-required CFs aren't mistakenly flagged.

### Fixed — Custom Format editor no longer wipes field values when you change Type

Typing a regex into a Release Title spec, clicking the Type dropdown by accident, and watching the regex disappear was painful. The editor now remembers field values per Type for the duration of the editing session — switch to another Type and back, your input returns. Compatible Types (like Release Title ↔ Release Group, which both have the same kind of "value" field) carry values forward without you doing anything. Genuinely-different Types still reset, since carrying a numeric value into a textbox doesn't make sense.

### Improved — sync result banner shows what changed

The summary banner after a sync used to read "Created: 0 CFs, Updated: 0 CFs, Scores: 0 updated" even when the sync had changed profile settings or the quality list — nothing in the summary indicated anything had happened. The banner now also shows Settings and Quality change counts, so language switches, min-score adjustments, quality-list edits, etc. are visible at a glance.

### Improved — language changes now show up in sync logs

When only the profile language changed during a sync, the log line said "profile settings changed" with no clue what triggered it (every numeric field would print as unchanged). Language is now included in the log line and in the sync history details panel.

### New — cross-Arr CF JSON import safety check

Importing a Radarr CF JSON into Sonarr (or vice-versa) used to silently misinterpret some specs. Example: a Source spec with value `7` means WEB-DL in Radarr but BlurayRaw in Sonarr — so a Radarr "WEB-DL" CF imported to Sonarr would silently start matching BlurayRaw releases instead.

The import now runs a compatibility check first and surfaces issues in a confirmation dialog:

- **Errors:** spec types that don't exist in the target app, or values that are out of range
- **Warnings:** specs whose canonical name resolves to something different in the target app (the Source value-7 case above falls here)

You can still click **Import anyway** if you know what you're doing — this is a safety check, not a hard block.

### Improved — group sort order is consistent everywhere

Profile Builder, Custom Formats list, Profile Detail, and Compare now all sort CF groups the same way: regular categories alphabetically, then the SQP groups, then "Other", then your own custom groups last.

## v2.2.3

Two small bug fixes from user reports.

### Fixed

- **Scoring Sandbox batch limit raised from 15 to 200 titles.** The previous cap blocked you from testing a profile against a full Prowlarr search worth of release-name variants — which is exactly what the sandbox is for. Clonarr still asks Radarr/Sonarr to parse one title at a time (no indexer hammering), and a "Parsing N titles, this may take a moment..." toast appears on batches over 30 so you know the wait is normal.
- **Custom Format JSON import now honors the "Use in renaming" flag.** Importing a TRaSH JSON like `pcok.json` (where the flag is set) silently landed it as false in the editor — you'd then have to remember to tick the box manually. Imports now bring that setting through correctly. The import dialog also notes that TRaSH-specific fields (`trash_id`, `trash_scores`) aren't imported — your imported CF lives as your own custom format, separate from TRaSH guide data.

## v2.2.2

UX patch — two fixes that protected against silent data loss, plus a few smaller UI improvements.

### Fixed

- **Custom-format filenames now keep the `!` prefix.** A common convention is to prefix your own CFs with `!` so they sort to the top in TRaSH-style listings. Clonarr was stripping the `!` when saving to disk, which meant `!FLUX` and `FLUX` collided on the same filename and one silently overwrote the other. Names with `!` now save under their own filename. Existing files migrate on next startup.
- **Toggling auto-sync on/off in the sync modal no longer wipes saved customisations.** If you opened the sync modal via "Save & Sync" on a fresh TRaSH profile and clicked the auto-sync toggle, the rule's saved CFs and score overrides got replaced with an empty state. The toggle now only flips the on/off flag — your saved customisations are only edited through Apply or the Edit pencil in the Sync Rules list.
- **"Showing X Custom Formats" counter** on the Custom Formats tab always rendered 0 due to reading the wrong field. Now shows the real total.

### Improved

- **Sync modal defaults to "Create new profile"** when opened from the profile list. It used to auto-flip to "Update existing profile" if there was matching sync history, putting you in overwrite mode without asking. The Edit pencil on a sync rule still goes straight to update — that's its purpose.
- **Confirmation dialog before overwriting an existing sync rule** via the explicit "Update existing profile" route. The dialog tells you exactly what will be replaced ("the saved rule with N CFs and M score overrides") and offers to cancel and use the Edit pencil instead.

## v2.2.1

Bug fix release. Addresses two filename-collision bugs in Clonarr's local storage that could silently overwrite saved profiles or custom formats.

### Fixed

- **Same-name profiles on Radarr and Sonarr no longer overwrite each other.** If you imported the same TRaSH profile to both apps, or built a custom profile with the same name on both via Profile Builder, saving the second one wrote over the first. Filenames now include an app-type suffix (`-radarr` / `-sonarr`) so each app gets its own file. Credit [@ColeSpringer](https://github.com/ColeSpringer) via [PR #28](https://github.com/prophetse7en/clonarr/pull/28).
- **Existing profile files now migrate to the new naming on startup.** PR #28's auto-rename was wired up for custom formats and CF groups but missed for profiles — without this, existing files kept their old names and only new saves used the suffix. Now everything migrates in one pass on first launch.
- **Collision protection during migration.** If two existing files would migrate to the same name (e.g. `HD` and `HD?` both clean to `hd-sonarr.json`), the alphabetically-first source wins; the rest keep their original names and you get a log warning telling you which one to rename. Before this guard, the second one silently overwrote the first during upgrade.

### What still might be unrelated

These fixes solve two specific collision cases. If you continue to see profile "reset to stock" symptoms, hex-named ghost CFs in your sync rules, or truncated sync history after upgrading, file a GitHub issue with details — those may be different root causes still under investigation.

### Recovery for already-affected installs

These fixes prevent future loss but don't recover data already overwritten. If you saw a profile reset or custom CFs disappear before upgrading:

1. **Re-import or rebuild** the affected profile and re-add personal CFs + score overrides manually. Going forward, each app gets its own file.
2. **Restore from a host-level backup** of `/config/profiles/` and `/config/custom/json/` from before v2.0.4 if you have one.

## v2.2.0

CF Group Builder redesign, a startup-pull fix, and a responsive top navigation bar from the community. What was going to ship as v2.1.2 grew enough that it became v2.2.0.

### Fixed

- **Pull interval "Disabled" now actually disables the startup pull.** Settings → TRaSH Guides → Pull interval set to Disabled was honored by the scheduled-pull loop but ignored at startup — so every container restart still did a fresh `git fetch`. Startup now respects the setting and loads the existing on-disk data without git ops if you have it set to Disabled. First-time launches still clone since Clonarr needs the data to work.
- **Status panel now correctly shows pull errors** when a pull fails on parsing or commit-hash lookup. Used to keep showing a clean state on top of a stale/corrupted snapshot.
- **Row layouts in CF Group Builder no longer collapse** in two places (Card A selected-highlight + the saved cf-groups list). Checkboxes used to wrap above CF names, and Edit/Delete buttons dropped to a third line on the only saved group.

### New

- **CF Group Builder — Selected CFs card.** A live preview at the top of the builder shows every CF currently in the group being built. Reorder via drag-and-drop in manual mode, set required / default per CF, or remove with the ×, all without scrolling back to the Custom Formats list. Works for new groups, local edits, and TRaSH copies.
- **CF Group Builder — hash lock toggle.** When editing or copying a group, a visible 🔒/🔓 toggle replaces the old save-time "keep vs regenerate" prompt. Locked (the default when editing) means typo fixes and minor rewording don't invalidate the group's identity — existing profile includes, prior exports, and synced Arr profiles stay valid. Unlocked means the identity changes as you type the name.
- **CF Group Builder — copy a TRaSH cf-group into the local builder.** A "TRaSH cf-groups" section above the builder lists every upstream group. Click Edit on any row to seed the form with its contents and save as your own local copy. The TRaSH repo is never written to.
- **Manual-order CF reorder via drag-and-drop.** Replaces the old ▲/▼ arrows. Same pattern as Scoring Sandbox and the Quality Structure editor.

### Changed

- **CF Group Builder is now three cards** — Selected CFs (live preview, where order lives), Custom Formats (browse + add), and Quality Profiles (include). Selected CFs in the Custom Formats list show with a blue background and green "IN GROUP" pill so you can see at a glance which ones you've already added.
- **Custom Formats list packs into 2+ columns on wide viewports** so short names like AMZN, 10bit, ATV don't waste a whole row each.
- **Responsive top navigation bar.** Tabs wrap on narrow viewports, the "TRaSH synced" label collapses gracefully, and the Changelog dropdown scales on mobile. Icon moved to `ui/static/icons/clonarr.png`. Credit [@ColeSpringer](https://github.com/ColeSpringer) via [PR #26](https://github.com/prophetse7en/clonarr/pull/26).

## v2.1.1

UX patch release. Profile Builder's "save without syncing" flow was always there — v2.1.0 just made it hard to discover and blocked for YAML-imported profiles. This release fixes both.

### Fixed

- **Imported profiles can now be opened in the Profile Builder for editing.** Previously, profiles created via YAML import (`Advanced → Import profile`) were rendered read-only — clicking the profile name sent you to a detail view with only "Save & Sync" / "Create New" buttons, and the Edit button was hidden. That made the "start a profile, come back to it later, finish it before pushing to Arr" workflow impossible for imported profiles. The backend always allowed edits; the frontend was gating on `source === 'custom'` in three places (`profile name click`, `Edit button`, `Edit button in detail view`). All three now accept any user-owned profile (`custom` / `import` / legacy empty source).
- **TRaSH-imported profiles now render their required CFs in the Builder.** Opening a TRaSH profile import (e.g. `base-profile.json` with 4 mandatory blocking CFs) in the Builder showed an empty Required CFs section — the CFs existed in `formatItems` with correct scores but weren't mirrored into `formatItemCFs`, which is what the Builder UI reads to render the Required section. The TRaSH convention is that every CF in `formatItems` is a mandatory CF of the profile; `parseTrashProfileJSON` now populates `FormatItemCFs` accordingly. Profiles imported before v2.1.1 get the same treatment via a frontend fallback in `openProfileBuilder` — no re-import needed.

### Changed

- **Profile Builder save-button labels clarified.** `Create Profile` → `Save Profile`, `Update Profile` → `Save Changes`. The existing Save-only action always saved locally without syncing to Arr (distinct from `Save & Sync` / `Create New` which push to an Arr instance), but the old labels didn't make that separation obvious when both options were visible side-by-side. Tooltips ("Save profile changes without syncing") unchanged.

### For existing users — how the save flow works

Profile Builder has two-tier saving:

1. **`Save Profile` / `Save Changes`** — saves locally in Clonarr only. Nothing touches Radarr/Sonarr. Perfect for drafts: start a profile, come back tomorrow, finish it, push it later.
2. **`Save & Sync`** or **`Create New`** — saves locally AND pushes to an existing or new Arr quality profile. Only shown when editing an existing profile.

This separation always existed — v2.1.1 just makes it more obvious and fixes the imported-profile gap.

## v2.1.0

### Added

- **CF Group Builder** — client-side generator for `cf-groups/*.json` files under Settings → CF Group Builder. Loads TRaSH's real cf-groups as starting points, filters by app type (Radarr/Sonarr), supports manual and alpha CF ordering, per-CF `required` / `default` toggles, category filter, multi-term search, scoped Select-all, bulk Mark-all / Clear-required / Clear-CFs / Clear-profiles, custom CFs with MD5 trash_id scoped by app-type, deduplication across cf-groups with accumulated group memberships, and export to downloadable JSON named with category prefix. Persists locally in browser storage. Profile cards reuse the Profiles-tab styling, collapsed by default, per-card select-all, reloads on appType switch. Makes it practical to ship custom exclusivity groups without editing JSON by hand.
- **Advanced Mode split into two toggles** — Settings now exposes "Show advanced Clonarr options" (existing) and a separate "Show TRaSH schema fields" toggle. Lets you see raw TRaSH fields (trash_id, includeCustomFormatWhenRenaming, etc.) without enabling the rest of Clonarr's advanced UI. Either, both, or neither — fully independent.

### Changed

- **Architecture refactor** — backend restructured from flat `ui/*.go` to standard Go layout: `internal/api/` (HTTP handlers split by domain — instances, cleanup, sync, autosync, trash, custom_cfs, custom_profiles, import, scoring, notifications, config, auth_handlers, routes, server, utils), `internal/core/` (models, config store, sync engine, TRaSH integration), `internal/arr/` (Radarr/Sonarr API clients), `internal/auth/` + `internal/netsec/` (security primitives unchanged), `internal/utils/` (`SafeGo`). `ui/` is now only the `//go:embed static` wrapper. Contributed by @ColeSpringer via revived PR #14. No user-facing behavior change — pure reorganization for maintainability.
- **Background panic recovery everywhere** — every goroutine wrapped via `utils.SafeGo`. One bad notifier/poller can no longer crash the whole process.
- **Golden Rule is now optional everywhere.** TRaSH renamed `[Required] Golden Rule` → `[Optional] Golden Rule` in PR #2711 upstream — both Golden Rule CFs (`Golden Rule UHD` and `Golden Rule HD`) are `required: false` in the schema and always were, but the group naming implied otherwise. Clonarr now treats the group as optional in the TRaSH-profile flow (profile detail, compare, builder) — picking zero is allowed, picking one is allowed, picking both is still forbidden (the exclusivity rule). Works with both the old and the renamed TRaSH repo name so existing installs don't break when TRaSH's PR merges upstream.
- **CF Group Builder for CF categorization** — formerly only TRaSH's own cf-groups drove the dropdown; now the UI surfaces the user's locally-built cf-groups alongside upstream ones and splits the "Ungrouped" bucket so it's obvious what hasn't been categorized yet.
- **Profile card sorting** — profiles within each card on the Profiles tab now alpha-sort, and cards themselves sort by their group integer (not a hardcoded name order) so new groups slot in correctly.
- **Sync history "Last Changed" time is frozen on apply.** Previously the column could drift as entries aged; now it's backfilled for existing entries at load and preserved via a frozen `AppliedAt` field going forward. Empty-state placeholder shown when entries exist but have no changes.

### Fixed

- **Stale git-lock files no longer permanently break the TRaSH pull.** A container kill during `git fetch --deepen=1` (or any other git op) can leave a `.lock` file behind. Next start failed with "fatal: Unable to create lock" until manual deletion. Reported by @fiservedpi in issue #23 with a one-line `shallow.lock` patch in PR #24. Broadened the fix to cover the full catalogue of locks that the same class of interrupt can leave behind: `HEAD.lock`, `index.lock`, `config.lock`, `packed-refs.lock`, `FETCH_HEAD.lock`, `shallow.lock`, and any `refs/**/*.lock`. Runs at the top of the existing-clone branch in `CloneOrPull`, before any git invocation. Safe — Clonarr is the only writer to `/data/trash-guides/.git` (single-process `pullMu` serializes all callers), so any lock found at startup is by definition stale from our own interrupted previous run. Credit @fiservedpi for the clean reproducer + patch that started the investigation.
- **C3 — config save no longer clobbers env-locked trust-boundary fields on no-change edits.** Unrelated setting saves could silently empty `TrustedNetworks` / `TrustedProxies` when the UI didn't touch them. Now guarded at every call site.
- **H3 — unauthenticated `/api/*` requests redirect to `/login`** instead of returning raw 401 JSON for browser-initiated navigation. API-key paths still return JSON 401. Centralized in the fetch wrapper so every handler inherits it.
- **H4 — `handleUpdateConfig` serialized to close a lost-update race.** Two parallel config edits could land in the wrong order; one lock per handler eliminates the interleave.
- **H5 — password-complexity UX on the setup wizard** gives progressive hints instead of a single rejection at submit. Matches Radarr/Sonarr feel.
- **Profile export omits the `language` field from Sonarr TRaSH JSON.** Sonarr schema doesn't include it at the profile level; previous exports added noise that round-tripped back as a dirty diff. Radarr exports unchanged (language is valid there).
- **Profile detail — cutoff override now syncs after an auto-correct.** If the chosen cutoff was invalid and the UI auto-corrected it, the override state stayed pointing at the old value until a manual change. Now auto-correct writes through.
- **Profile builder — auto-selected cutoff syncs to `pb.cutoff` immediately.** Same class of desync as above; the builder flow was independently affected.
- **Sync-history display** — backfills `AppliedAt` on load for entries that pre-date the field (so they don't all show the same placeholder time).
- **CF Group Builder** — scopes MD5 trash_id generation by app type (a Radarr CF and a Sonarr CF with identical names now get different trash_ids and don't collide when imported side-by-side). `cfgbDelete` guarded against overlapping clicks. Styled confirm modals instead of the browser-native `confirm()` dialog. Paste artefacts stripped from pasted descriptions.
- **Notification webhook validation** restored after refactor, with migration tests covering the v2.0.x-flat → v2.0.8-agents path.
- **UI polish** — readable placeholder text on dark inputs; renamed undefined `.config-input` class to the actual `.input` style.

### Notes for upgraders

Upgrading from v2.0.x is transparent — no config migration needed. If you have `cf-groups/*.json` files in `/config/custom-cfs/`, they're picked up automatically by the CF Group Builder dropdown alongside TRaSH's upstream groups. The v2.0.6 security baseline (authentication, trusted networks, API key) is unchanged.

Users who were manually deleting `.git/shallow.lock` after container restarts can stop — that's now handled automatically.

## v2.0.8

### Added

- **Notification Agents** — replaces the flat per-provider toggles under Auto-Sync → Notifications with an Instances-style list. Each notification channel (Discord, Gotify, Pushover) is now an independent agent with its own enable flag, credentials, severity routing, and optional `Name` field so you can run multiple agents of the same type (e.g. "Discord #main" + "Discord #trash" to separate sync alerts from TRaSH repo updates). Per-agent inline test button verifies credentials end-to-end. Migration auto-converts existing v2.0.x flat config on first startup — nothing to do manually. Contributed by @xFlawless11x via PR #15.

### Security

- Notification agent credentials masked in all `/api/config` responses (Discord webhooks, Gotify token, Pushover user key + app token). `preserveIfMasked` on update restores stored values when the UI round-trips the placeholder.
- `dispatchNotification` wraps `sendGotify` / `sendPushover` goroutines via `safeGo` — a panic in one notifier cannot kill the process.
- Inline notification-agent test endpoint hardened: `MaxBytesReader` 4096, unknown agent types return 400, `Cache-Control: no-store` on all responses.
- **T70 fix:** the session-persistence goroutine in `ui/auth/auth.go` is now wrapped in a panic-recovery helper. A theoretical panic inside `writeSessionsSnapshot` (e.g. an unexpected `os.WriteFile` error path) would previously have crashed the container. No known impact in production — defense in depth.

### CI

- `.github/workflows/ci.yml` gains `workflow_dispatch` trigger so the test matrix can be re-run manually from the Actions tab.
- `.github/workflows/docker.yml` now supports forks and self-hosted setups without Docker Hub credentials: Docker Hub login step is conditional on `DOCKERHUB_USERNAME` secret being set. `setup-qemu-action` pinned to v4.0.0. From PR #16.

## v2.0.7

### Fixed

- **Golden Rule (and other exclusive CF groups) can now be disabled at the group level.** Previously, groups that TRaSH marks with a "pick one" exclusivity hint in their description (like `[Required] Golden Rule UHD`) had their group-level toggle hidden in the profile detail / TRaSH-sync view — users had no way to say "I don't want this group at all", only "enable / disable each CF individually". That was inconsistent with how equivalent optional groups (HDR Formats, Optional Movie Versions, Audio Formats) behave, and stricter than what TRaSH's own schema supports (both Golden Rule CFs are `required: false`). The group toggle is now shown for every group including exclusive ones. Behavior:
  - Group ON + not exclusive → all non-required CFs auto-enabled (unchanged).
  - Group ON + exclusive → no CFs auto-enabled; user picks one via pick-one logic.
  - Group OFF → all CFs in the group cleared regardless.
  - The "only enable one" warning still shows when the group is expanded.

## v2.0.6

**⚠️ Breaking change:** Authentication is now enabled by default (Forms + "Disabled for Trusted Networks", matching the Radarr/Sonarr pattern). On first run after upgrade, Clonarr will redirect to `/setup` to create an admin username and password. Existing sessions are invalidated (cookie name changed from `constat_session` to `clonarr_session` as part of branding cleanup). Homepage widgets and external scripts hitting `/api/*` now need the API key (Settings → Security) — send as `X-Api-Key` header.

### Added

- **Authentication (Radarr/Sonarr pattern)** — `/config/auth.json` stores the bcrypt-hashed password + API key. Three modes:
  - `forms` (default): login page + session cookie, 30-day TTL.
  - `basic`: HTTP Basic behind a reverse proxy.
  - `none`: auth disabled (requires password-confirm to enable — catastrophic blast radius).
- **Authentication Required** — `enabled` (every request needs auth) or `disabled_for_local_addresses` (default — LAN bypasses).
- **Trusted Networks** — user-configurable CIDR list of what counts as "local". Empty = Radarr-parity defaults (10/8, 172.16/12, 192.168/16, link-local, IPv6 ULA, loopback). Narrow the list (`192.168.86.0/24`, `192.168.86.22/32`) for tighter control.
- **Trusted Proxies** — required when Clonarr sits behind a reverse proxy (SWAG, Authelia, etc.) so `X-Forwarded-For` is trusted.
- **Env-var override for trust-boundary config** — set `TRUSTED_NETWORKS` and/or `TRUSTED_PROXIES` in the Unraid template or `docker-compose.yml` to pin the values at host level. When set, the UI shows the field as locked and rejects edits — the trust boundary can only be changed by editing the template and restarting.
- **API key** — auto-generated on first setup, rotatable from the Security panel. Send as `X-Api-Key: <key>` header (preferred) or `?apikey=<key>` query param (legacy — leaks to access logs and browser history). For Homepage widgets, scripts, Uptime Kuma.
- **Change password** — from the Security panel. Requires current password. Invalidates all other sessions.
- **CSRF protection** — double-submit cookie pattern on all state-mutating requests. Transparent to browser users; scripts using the API key bypass (verified key required, not just presence).
- **Security headers** — `X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff`, `Referrer-Policy: same-origin`. Radarr-parity scope.
- **SSRF-safe notification client** — Discord and Pushover (both always external) now use a blocklisted HTTP client that refuses RFC1918/loopback/link-local/ULA/NAT64/CGN/doc-range targets with per-request IP revalidation (defeats DNS rebinding). Gotify stays on a plain client (LAN targets are legitimate for self-hosted Gotify).
- **Webhook and notification secret masking** — Discord webhook URLs, Gotify token, Pushover user key + app token, and Arr instance API keys are masked in API responses. Empty-on-unchanged-edit preserves the stored value on save (so editing unrelated fields doesn't clobber secrets).

### Fixed

- **T64 — live-reload no longer clobbers env-locked trust-boundary fields.** Previously any unrelated config save (session TTL, auth mode) could silently empty the env-derived trusted-networks slice. Now guarded at every call site.
- **T65 — `UpdateConfig` preserves all deployment-level fields.** Previously only `AuthFilePath` was preserved; `SessionsFilePath`, `MaxSessions`, and env-lock state could be silently dropped by a future caller building config from scratch. Defense-in-depth: also force-restores locked values from the internal state.
- **T66 — data races eliminated from `Middleware` / `TrustedProxies()` / `IsRequestFromTrustedProxy()`.** Config snapshot taken via `RLock` at the top; all downstream reads use the local value. Passes `go test -race`.

### Changed

- **Cookie rename** — `constat_csrf` → `clonarr_csrf`, `constat_session` → `clonarr_session`. Avoids browser-scope collision when both apps sit behind the same parent domain. Existing sessions won't survive the upgrade.
- **Basic realm** — `WWW-Authenticate: Basic realm="Clonarr"` (was `"Constat"` from initial port).
- **Setup page footer** — GitHub link points to `prophetse7en/clonarr` (was `/constat`).

### Security

- First-run forces the `/setup` wizard — no default credentials.
- bcrypt cost 12; password verify is timing-equalized (prevents user-enumeration via response timing).
- Session persistence via atomic write to `/config/sessions.json` (survives container restart).
- CIDR min-mask enforced (`/8` IPv4, `/16` IPv6) to reject mis-typed host bits masking as subnets.
- See `docs/security-implementation-baseline.md` in the repo for the full trap catalogue (T1–T66) behind the implementation.

### Notes for upgraders

- First boot redirects to `/setup`. Choose a strong password (≥10 chars, 2+ of upper/lower/digit/symbol).
- If you access Clonarr from the same LAN the host is on, the default "Disabled for Trusted Networks" mode will skip login for you — no change in day-to-day UX.
- Homepage / Uptime Kuma: use the API key from Security panel, send as `X-Api-Key` header.
- Lost your password: stop the container, delete `/config/auth.json` (credentials only — no profile data), restart. The setup wizard will run again.

## v2.0.5

### Fixed

- **Extra CFs showed hex IDs instead of names in Overridden Scores** — Score overrides on Extra CFs (CFs added to a profile but not part of the base TRaSH profile) displayed their trash ID (e.g. `82cc7396f79a`) instead of the CF name after Save & Sync. The display helpers only looked at CFs belonging to the base profile; they now fall back to the Extra CFs list so the correct name and default score are shown. Sort order in the panel also now uses real names. Same fix covers both TRaSH Extra CFs and user-created custom CFs added as extras.

## v2.0.4

### Fixed

- **Quality Definitions null values** — Sonarr/Radarr "Unlimited" (null) for preferred/max size showed as 0.0. Now uses `*float64` to distinguish null from explicit zero.
- **Sync All score oscillation** — Ring-buffer entries with different selectedCFs caused scores to flip-flop on every Sync All. Now deduplicates to latest entry per profile.
- **CF Editor dropdowns lost on edit** — Language, Resolution, and other select-type specs showed raw numeric values instead of dropdown. Three-part fix: schema matching, string coercion, and programmatic option population (replaces `<template x-for>` inside `<select>`).
- **Cutoff dropdown showing deleted group** — When quality structure override removed the TRaSH default cutoff group, dropdown showed the deleted name. Now auto-picks first allowed quality. Also fixed same `x-for`-in-`select` timing bug.
- **Language dropdown in Edit view** — Same programmatic population fix applied.
- **Custom CF filenames** — Regression from path traversal fix: files saved as `custom:hex.json` instead of readable names. Now uses sanitized CF name. Auto-migrates on startup.
- **GitHub #10** — Unknown quality names (group names without sub-items, cross-type names) now skipped with log warning instead of failing entire sync.
- **pprof debug endpoints removed** — `/debug/pprof/*` endpoints removed from release builds.

### Improved

- **Score Override UX** — Summary panel shows all overridden CFs when toggle is active, editable inline with per-CF ↻ reset button. Override count badge per CF group header.
- **Toggle labels** — "Override" → "Hide Overrides" when active (General, Quality, CF Scores, Extra CFs).
- **Extra CFs layout** — Fixed-width columns (toggle | name 180px | score 65px), sorted A→Z.
- **Keep List redesign** — Side-by-side layout: search + Add/Add all on left, 3-column CF list on right. Batch "Add all (N)" matching, "Remove all" button.
- **Sync Rules default sort** — A→Z by Arr Profile name instead of ring-buffer insertion order.
- **Per-webhook Discord test** — Sync and Updates webhooks each have independent Test buttons.

## v2.0.3

### Added

- **Docker Hub mirror** — Image now published to both GHCR (`ghcr.io/prophetse7en/clonarr`) and Docker Hub (`prophetse7en/clonarr`). Use Docker Hub if your platform can't pull from GHCR (e.g. Synology DSM with older Docker).
- **Per-webhook Discord test buttons** — Sync webhook and Updates webhook each have their own Test button.

## v2.0.2

### Added

- **Pushover notifications** — Third notification provider alongside Discord and Gotify. Collapsible provider sections with status indicators and test buttons. Discord can now be toggled on/off. (Community contribution by @xFlawless11x, PR #12)

### Fixed

- **GHCR pull fails on older Docker clients (Synology, DSM)** — Multi-arch builds produced OCI image indexes that older Docker versions can't parse. Added `provenance: false` to CI workflow to force Docker manifest list v2 format.

## v2.0.1

### Fixed

- **Dry-run Apply button shows wrong instance** — When selecting a non-default instance in the sync modal's Target Instance dropdown, the dry-run results banner showed "Apply to [default instance]" instead of the selected instance. Now uses `syncPlan.instanceName` from the backend.

## v2.0.0

### Compare — Redesigned

- **Table layout** for Required CFs and CF Groups — current vs TRaSH values side-by-side with checkboxes per row
- **Profile Settings table** — compares Language, Upgrade Allowed, Min/Cutoff/Upgrade scores against TRaSH defaults
- **Filter chips** — All / Only diffs / Wrong score / Missing / Extra / Matching to focus on what matters
- **Golden Rule picker** — auto-selects HD or UHD variant based on what's in use, with cascade logic (inUse → default+required → default → first)
- **Per-card Sync selected** — sync changes per section (Required CFs, each CF Group, Settings) instead of all-or-nothing
- **Toggle all** link per card header for quick select/deselect
- **Score override badges** — blue "OR" badge when a score difference is intentional (from your sync rule overrides)
- **Score-0 extras via sync history** — CFs added via "Add Extra CFs" with score=0 now correctly appear in Compare instead of being silently dropped
- **Exclusive group radio behavior** — "pick one" groups work correctly with proper counting

### Sync History & Rollback — New

- **History tab** between TRaSH Sync and Compare — dedicated change log for all synced profiles
- **Ring-buffer storage** — last 10 change events per profile (no-change syncs only update the timestamp)
- **CF set-diff tracking** — catches all CF changes including score-0 CFs from group enable/disable
- **Detailed change log** — CFs added/removed, scores before→after, quality items toggled, settings changed
- **Sortable columns** — TRaSH Profile, Arr Profile, Last Changed, Events
- **Rollback** — restore a profile to any previous state with one click. Confirmation shows what will be reversed. Auto-disables auto-sync to prevent overwrite
- **Auto-refresh** — History tab updates in real-time after sync operations

### Profile Detail — Redesigned

- **General + Quality cards** with blue/purple stripe design and per-section override toggles
- **Inline Quality Items editor** — expands inside the Quality card (same as Builder) with drag-and-drop grouping
- **Quality card spans full width** when editor is open (prevents CSS column overflow)
- **Override summary bar** — shows active overrides with per-section and "Reset all" controls

### Profile Builder — Redesigned

- **Init card with tabs** — TRaSH template / Instance profile (replaces cluttered "Start from" row)
- **General + Quality cards** matching the Edit view's visual language
- **Golden Rule + Miscellaneous variants** as sub-section inside Quality card
- **Collapsible Advanced Mode** behind devMode flag
- **Shared Quality Items editor** — Builder and Edit view share the same drag-drop editor code (parameterized with target='edit'|'builder')
- **Import from instance improved** — consults sync history for score-0 extras, resolves custom CFs, surfaces all CFs in Required CFs section
- **Button label** — "Editing Items" → "Done" (describes action, not state)

### Settings — Redesigned

- **Sidebar + content panel** layout matching vpn-gateway and PurgeBot
- Six sections: Instances, TRaSH Guides, Prowlarr, Notifications, Display, Advanced
- **Prowlarr gets its own section** (split from Advanced) with custom search categories per app type
- Green left-border active indicator, centered layout (1100px max-width)

### Scoring Sandbox — Improved

- **Custom Prowlarr search categories** — configurable Radarr/Sonarr category IDs for indexers that don't cascade root IDs
- **Numeric release group fallback** — trailing numeric groups like `-126811` now parsed correctly when Arr returns empty
- **Per-row selection + filter** — checkbox per row, "Filter to selected" toggle, "Reset filter"
- **Drag reorder** — manual sorting with drag handles (disabled during filter to prevent confusion)
- **Copy-box modal** — shareable plain-text summary per release (title, parsed metadata, matched CFs, scores)
- **Language CFs stripped** — "Wrong Language" and "Language: *" CFs excluded from scoring (Parse API can't evaluate without TMDB context)
- **Stable drag keys** — `_sid` identity tracking prevents DOM glitches during reorder

### Browser Navigation — New

- **Back/forward works** — `pushState` on every section/tab change, `popstate` listener restores state
- **URL hash routing** — e.g. `#radarr/profiles/compare`, `#settings/prowlarr`, `#sonarr/advanced/scoring`
- **Hash validation** — invalid hashes fall back to defaults (no blank page)
- **Initial entry seeded** — `replaceState` ensures the first Back click has somewhere to go

### Other Improvements

- **Sonarr language** — language field hidden everywhere for Sonarr (removed in Sonarr v4, not in TRaSH Sonarr profiles)
- **Sortable Sync Rules columns** — TRaSH Profile and Arr Profile headers clickable to sort A→Z / Z→A
- **Sync Rules renamed** from "Sync Rules & History" (History has its own tab now)

### Fixed

- **GitHub #10** — "WEB 2160p not found in definitions" when syncing. Quality names not in definitions are now skipped with a log warning instead of failing the entire sync
- **XSS sanitization** — all `x-html` bindings now wrapped in `sanitizeHTML()` (3 were missing)
- **Path traversal** in custom CF create endpoint
- **Shared quality editor state leak** — `qualityStructureEditMode` no longer leaks between Builder and Edit view
- **`pb.qualityItems` identity tracking** — `$watch` auto-assigns stable `_id` on every reassignment
- **Sonarr Language "Unknown" diff** — no longer shows false Language diff in Compare for Sonarr profiles
- **`alert()` → toast** — all browser alerts replaced with toast notifications

### Security

- All `x-html` bindings sanitized via `sanitizeHTML()`
- `GetLatestSyncEntry` returns defensive copy (not pointer into config slice)
- Path traversal prevention in custom CF file operations
- API key masking on all config responses

## v1.9.0

### Added

- **Clone profile** — Clone button on sync history row creates a copy of a synced profile with a new name, including all overrides, quality structure, and behavior settings.
- **Inline rename** — Click the Arr profile name in sync history to rename it directly. Changes are applied to the Arr instance and local sync history. Duplicate name detection prevents accidental overwrites.
- **Dry-run settings/quality preview** — Dry-run now shows settings changes (min score, cutoff, language, upgrade until) and quality item changes (enabled/disabled) — same detail level as the apply result.
- **Arr profile name in Edit header** — When editing a synced profile, the header shows which Arr profile it syncs to (e.g. "Sonarr → WEB-2160p").

### Fixed

- **"Delete CFs & Scores" cleanup now respects Keep List** — Score reset previously zeroed ALL scores across every profile, even for CFs in the Keep List. Now only scores for the actually deleted CFs are reset.
- **Safer cleanup order** — "Delete CFs & Scores" now deletes CFs first, then resets scores. If CF deletion fails partway through, orphaned scores are harmless. Previously scores were zeroed first, which was unrecoverable if CF deletion then failed.
- **Button text invisible in several modals** — Pull, Preview, Apply, Download Backup, and Create/Update Profile buttons appeared as empty green/colored rectangles. Caused by `<template x-if>` inside `<button>`, which browsers handle inconsistently. Replaced with `<span x-show>` across 9 buttons.
- **Cleanup descriptions clarified** — "Delete All CFs" and "Delete All CFs & Scores" descriptions now state "(respects Keep List)" so the relationship with the Keep List above is clear.
- **Auto-sync checkbox in sync modal** — "Auto-sync this profile" checkbox couldn't be unticked after Save & Sync. The binding checked if a rule *existed* rather than if it was *enabled*.
- **Auto-sync rule not updated on profile change** — Changing target Arr profile in sync modal dropdown didn't update the auto-sync rule reference, causing stale checkbox state.
- **CF score overrides lost after Done** — Static score display always showed TRaSH default after closing the override panel. Now shows overridden values in yellow.
- **Alpine errors on quality structure** — `item.items.length` crashed on non-group items (undefined), cascading into reactive state corruption that affected CF score overrides.
- **Custom CF false "update" on every sync** — Custom CFs with numeric field values (e.g. resolution "2160") were always reported as changed because the stored string didn't match Arr's integer. Values are now normalized before comparison.
- **Profile Builder label clarity** — "Create New Profile" → "New Profile", "Import" → "Import JSON", builder "Import" row → "Start from" to distinguish file import from Arr instance import.
- **Extra CFs score-0 visibility** — CFs with score 0 stayed visible in "Other" after being added to extras because `!0` is `true` in JavaScript. Fixed with explicit `undefined` check.

### Improved

- **Extra CFs Added list** — Multi-column layout (2 cols >10, 3 cols >20) matching the Other list, preventing long single-column scrolling.

### Changed

- **Icon buttons** — Sync history action buttons (Edit, Sync, Clone, Remove) replaced with compact icons + tooltips for a cleaner layout.

## v1.8.8

### Fixed

- **Custom CF storage — eliminate cross-app name collisions** — Imported custom formats with identical names in Radarr and Sonarr (e.g. `!LQ`) no longer get a `(2)` suffix. CFs are now stored in app-scoped directories (`/config/custom/json/{radarr,sonarr}/cf/`). Existing installations migrate automatically on startup — old files are moved to the correct subdirectory and collision suffixes are stripped.
- **CF editor Type dropdown empty on first open** — The "Type" dropdown in the Custom Format editor showed "Select type..." instead of the actual type (e.g. Source, Release Group) when opening a CF for the first time. Root cause: `<template x-for>` inside `<select>` is invalid HTML and the browser silently removes it. Replaced with programmatic option creation via `x-effect`.
- **Export TRaSH JSON broken over HTTP** — The "Export TRaSH JSON" button in the CF editor silently failed on non-HTTPS connections (e.g. LAN access). Replaced with a proper export modal showing formatted JSON with a Copy button, matching the profile builder export style.

## v1.8.7

### Fixed

- **Custom Format editor — context dropdown showed wrong app types** — When editing a user-created CF, the "Trash Scores → Context" dropdown listed all contexts regardless of app type. A Sonarr CF's dropdown showed Radarr-only SQP tiers (`sqp-1-1080p`, `sqp-2`, etc.) and `anime-radarr`. The list is now derived dynamically from the actual TRaSH-Guides CF JSONs on disk via a new `/api/trash/{app}/score-contexts` endpoint, so Sonarr CFs only show Sonarr contexts (including `anime-sonarr`) and Radarr CFs only show Radarr contexts (with all SQP tiers). New contexts added by TRaSH upstream are picked up automatically without code changes.

### Improved

- **Sync Profile modal — clearer dropdown labels and descriptions** — All three dropdowns (Add / Scores / Reset) had labels and descriptions that either implied the wrong behavior or hid important details. Rewritten against the actual `BuildSyncPlan` / `ExecuteSyncPlan` logic so each option states exactly what it does:
  - **Scores:** "Enforce TRaSH scores" / "Allow custom scores" suggested TRaSH defaults override everything and that "custom scores" meant Clonarr-side overrides. Both misleading — Clonarr score overrides apply in *both* modes, and the real distinction is how Clonarr handles manual edits made directly in Arr's UI. Renamed to "Overwrite all scores in Arr" / "Preserve manual edits in Arr" with descriptions that spell out the behavior precisely.
  - **Add:** "Automatically add new formats" didn't mention that this mode respects manual CF removals in Arr (the actual reason to pick it over "add missing"). Renamed to "Respect manual removals — only add new ones" and the description now explains the `lastSyncedSet` comparison and the first-sync edge case.
  - **Reset:** "Reset unsynced scores to 0" didn't clarify that only non-zero scores are touched, or what "unsynced" means. Renamed to "Zero out orphaned scores" and the description spells out that it targets CFs in the target Arr profile that are no longer part of this sync.
  No logic change — pure text and label rewrite.
- **File Naming tab — verbatim TRaSH-Guides text** — All descriptions on the File Naming tab now quote TRaSH-Guides directly instead of paraphrasing. Clonarr is a TRaSH sync tool; it should use the wording the guide maintainers have crafted. Replaced the "Why use a naming scheme?" and "IMDb vs TVDb / TMDb" info cards, per-scheme descriptions (Original Title, P2P/Scene), section descriptions for Movie File/Folder Format, Episode/Series/Season Folder Format, and the Plex "Edition Tags" warning with their TRaSH-Guides source text. Source file paths documented in the UI markup.

## v1.8.6

### Added

- **Quality Group editor in TRaSH sync overrides** — Edit quality groups directly from the Customize Overrides dialog without opening Profile Builder. Drag-and-drop to reorder, drop on a row to merge, click a group name to rename. Create / rename / merge / ungroup / delete / reorder groups inline.
- **Multi-arch GHCR builds** — `linux/amd64` + `linux/arm64` (Apple Silicon support).

### Fixed

- **Memory leak** — Every API call created a new `http.Client` with its own connection pool, accumulating ~2-3 MiB/hour of unreclaimable transport state. Replaced with two shared clients (one for Arr/Prowlarr API, one for notifications). Also fixed event slice reslicing to release old backing arrays.
- **Five sync diff blindspots** — Sync previously missed Radarr-side changes that kept the same set of allowed qualities: reorder items, reorder groups, extracting a quality from a group, cutoff change, and upgradeUntil change. The diff was set-based and silently ignored ordering and structure. Replaced with a structure-aware fingerprint that captures ordering, group structure, and allowed-state. Covers Auto-Sync, manual Sync, and Sync All.
- **Sync result banner hiding change details** — After Save & Sync, the profile detail banner only showed `cfsCreated` / `cfsUpdated` / `scoresUpdated` counts. Quality flips, cutoff changes, and per-CF changes were in the backend response but never rendered. Banner now lists the full details.
- **Imported profile toast hiding change details** — Same blindspot in the `startApply` toast path. Now renders the full details list like `Sync` / `Sync All` already did.
- **Quality structure override loss on auto-sync** — Enabled structure overrides now survive every sync regardless of upstream TRaSH quality/CF/score changes.
- **Cutoff handling with structure override** — Cutoff dropdown reads from the override structure when set (so renamed/created groups appear). "Reset to TRaSH" properly clears the structure override.

## v1.8.5

### Fixed

- **Zombie process leak** — `git auto-gc` was detaching as an orphan subprocess and getting reparented to the Go binary running as PID 1, which the Go runtime does not reap. Accumulated ~79 zombies in 6 hours under normal load. Fix: `tini` as PID 1 in the Dockerfile (`ENTRYPOINT ["/sbin/tini", "--", "/entrypoint.sh"]`), plus `git config gc.auto=0` on the TRaSH data dir in `ui/trash.go` (both the fresh-clone and migration code paths). Verified zero zombies after 3+ hours in production.

## v1.8.4

### Fixed

- **CF tooltip showing raw markdown** — Descriptions with Wikipedia links (e.g. streaming service CFs) now display as clean text instead of raw markdown syntax

## v1.8.3

### Fixed

- **Browser autofill popup on Settings** — URL and token fields no longer trigger browser password save/fill dialogs

## v1.8.2

### Improved

- **Sync Rules column headers** — TRaSH Profile, Arr Profile, Auto-Sync, Details, and Actions columns with consistent alignment across all rows
- **Arr Profile ID** — Profile ID shown next to Arr profile name (e.g. `ID 23`) for easy identification
- **Builder Synced Profiles** — Same column layout as TRaSH sync (Your Profile, Arr Profile, Details, Actions)
- **Text readability** — All secondary text lightened from `#484f58` to `#6e7681` across all tabs (quality sizes, scoring sandbox, settings, compare, builder)
- **Healthcheck suggestion UI** — Suggestion box hidden when no Extra Parameters command is available (e.g. distroless images)

### Fixed

- **conflicts.json parser** — Updated to match the TRaSH Guides PR #2681 schema where trash_ids are object keys, not fields. Ready for when the PR merges.

## v1.8.1

First stable release — all previous beta versions consolidated.

### Features
- **Gotify push notifications** — Configurable Gotify support for all notification types (auto-sync, cleanup, repo updates, changelog). Per-level priority toggles (Critical/Warning/Info) with customizable priority values.
- **Second Discord webhook** — Separate webhook for TRaSH Guides updates (repo changes, weekly changelog), keeping sync notifications on the main webhook.
- **Settings reorganized** — Collapsible accordion sections: Instances, Notifications, Auto-Sync, Advanced. Cleaner layout as settings grew.

### Bug fixes
- **Gotify fires independently of Discord** — Notifications no longer require a Discord webhook to be set. Gotify and Discord send independently.
- **Priority value 0 preserved** — Gotify priority value of 0 (silent) now persists correctly through restarts instead of being reset to defaults.

## v1.8.0-beta

### Features
- **Auto-sync GUI toasts** — When scheduled or manual pull triggers auto-sync, toast notifications show detailed results (CF names, score changes, quality items) with staggered 3s delay between multiple profiles.
- **Detailed sync toasts** — quickSync, Sync All, and toggle auto-sync now show specific changes (e.g. "Repack/Proper: 5 → 6") instead of just counts.
- **Sync All respects auto-sync** — Only syncs profiles with auto-sync enabled. Shows warning if no profiles qualify.
- **Scheduled pull diff toast** — Scheduled pulls show "TRaSH updated: ..." toast in GUI automatically.
- **Instance version display** — Settings shows "Connected · vX.Y.Z" for Radarr, Sonarr, and Prowlarr consistently.
- **Prowlarr auto-test** — Prowlarr tested on init and every 60s alongside Radarr/Sonarr.

### UI improvements
- **Sync rules layout** — Fixed min-widths for profile names, arrow, Arr name, and auto-sync toggle for vertical alignment across all rules.
- **Larger arrow** — Profile → Arr arrow more visible (15px, lighter color, centered margins).
- **Settings layout** — Instance URL inline after name, version on same line as Connected.

## v1.7.9-beta

### Features
- **Compare overhaul** — Compare tab now shows profile settings (min score, cutoff, language, upgrade allowed, quality items) alongside CF comparison. All sections in collapsible cards with summary badges and status icons.
- **Settings sync from Compare** — Checkboxes on each setting/quality diff: checked syncs to TRaSH value, unchecked keeps current value as override. Overrides passed to sync modal automatically.
- **Override and custom CF badges on sync rules** — TRaSH Sync tab shows separate pills: blue "X custom CFs" for user-created formats, amber "X overrides" for score/quality/settings overrides. Tooltips explain each.
- **Auto-sync immediate run** — Enabling auto-sync toggle now runs sync immediately instead of waiting for next TRaSH pull.
- **Pull toast notification** — Manual pull shows toast with result: "TRaSH data up to date" or diff summary.
- **conflicts.json support** — Auto-deselect conflicting CFs when TRaSH merges conflicts.json. Activates automatically on pull.

### Bug fixes
- **Optional exclusive groups (SDR)** — Can now deselect all toggles. Golden Rule still requires at least one active.
- **Sync All Fixes** — Confirm dialog with profile names. Correct profile pre-selection via resyncTargetArrProfileId.
- **Required CFs counts** — Compare badges now show section-specific counts (not global totals that included grouped CFs).
- **Auto-sync hidden in Compare sync** — Sync modal from Compare hides auto-sync toggle.
- **Select option type mismatch** — Fixed String vs number comparison for Arr profile dropdown pre-selection.
- **Shallow clone diff detection** — Pull diff works reliably with shallow clones (fetch uses `--deepen=1`).

### Internal
- Prepared conflicts.json parsing (ConflictsData structs, API endpoint, frontend loading). Zero-downtime activation when TRaSH merges PR #2681.

## v1.7.7-beta

### Bug fixes
- **Profile Builder buttons missing** — `_resyncReturnSubTab` and `_resyncNavigating` were not declared in Alpine data, causing console errors and hiding Create/Save/Sync buttons entirely.
- **Top action bar in Profile Builder** — Save/Sync buttons now shown at top of builder (not just in sticky bottom bar), matching user expectation.
- **Auto-sync hidden for builder profiles** — Sync modal no longer shows auto-sync toggle for builder profiles (manual sync only, prevents TRaSH/builder conflicts).

## v1.7.6-beta

### Features
- **Git diff Discord notifications** — "TRaSH Guides Updated" now shows actual file changes (Added/Updated/Removed per CF, profile, group) via git diff instead of stale updates.txt entries.
- **Separate weekly changelog notification** — "TRaSH Weekly Changelog" Discord notification sent only when TRaSH updates their changelog (amber embed, distinct from per-pull blue notifications).
- **Latest Update in GUI dropdown** — Changelog dropdown now shows last pull's actual changes at the top, with timestamp and commit range. TRaSH Changelog (updates.txt) shown below.
- **Next pull countdown** — Header bar shows time until next scheduled pull (auto-updates every 30s).
- **Arr profile name in Discord** — Auto-sync Discord notifications show Arr profile name when different from TRaSH profile name.
- **CF tab uses TRaSH groups** — Custom Formats tab now uses actual TRaSH CF group files as categories instead of hardcoded fake categorization. Each group file is its own collapsible section with color-coded borders.
- **Multi-column CF lists** — CF lists with 10+ items use 2 columns, 30+ use 3 columns for compact display.

### Bug fixes
- **CF description duplicate name** — TRaSH markdown descriptions started with a bold title line repeating the CF name. Now stripped automatically.
- **Pull remote URL sync** — Changing repo URL in settings now updates the git remote before fetching. Previously the old remote was used until re-clone.
- **Quality override flip-flop** — Quality overrides (user-toggled resolutions) are now applied before comparing with Arr state, preventing false Enabled/Disabled changes on every sync.
- **Discord "no changes" spam** — Auto-sync no longer sends Discord notifications for profiles that are already in sync.
- **Discord bullet point formatting** — Fixed indented bullet points rendering incorrectly in Discord embeds.
- **Manual pull sends Discord notification** — Manual pull button now triggers "TRaSH Guides Updated" notification (previously only scheduled pulls did).
- **timeAgo auto-updates** — Sync timestamps in UI now update automatically every 30s without manual refresh.
- **Sync history auto-reload** — Frontend detects when scheduled pull completes and reloads sync data automatically.
- **Last diff persisted to disk** — Latest Update diff survives container restarts.
- **Unique category colors** — Fixed duplicate colors for Streaming Services, Optional, Resolution, and HQ Release Groups categories.
- **Improved text contrast** — Fixed dark-on-dark text for commit hash, changelog counts, and PR links in UI.
- **Dockerfile version** — Updated from 1.7.2-beta to 1.7.6-beta.

## v1.7.5-beta

### Bug fixes
- **Builder/TRaSH sync rule separation** — Auto-sync disabled for builder profiles (manual sync only). Prevents builder rules from overwriting TRaSH sync history on pull.
- **Auto-sync rule updated on source change** — Syncing a TRaSH profile to an Arr profile with a builder rule now converts the rule permanently. No merge-back possible.
- **Confirm dialog on source change** — Warning shown when syncing overwrites a rule of different type (Builder→TRaSH or TRaSH→Builder).
- **Startup cleanup safety** — Cleanup skips instances returning 0 profiles (race condition when Arr is still starting).
- **Reset Non-Synced Scores** — Now includes extra CFs, custom CFs, and all CFs from sync history. Previously only checked standard TRaSH profile CFs, causing user-synced CFs to be falsely flagged.

## v1.7.4-beta

### Features
- **Instance health check every 60s** — Connection status now updates automatically within a minute when instances go up or down (was 5 minutes).
- **Comprehensive debug logging** — Cleanup, auto-sync, TRaSH pull, and sync errors now all logged to debug.log for easier troubleshooting.
- **Profile Builder description** — Clarified as "For advanced users" with amber warning, pointing users to TRaSH Sync tab.

### Bug fixes
- **Sync errors shown as "no changes"** — Backend returns `{"error":"..."}` but frontend only checked `result.errors` (array). Connection failures now correctly show red error toast.
- **Error badge persists through toggle** — Toggling auto-sync no longer clears the error badge. Error clears only when a sync succeeds.
- **Sync All/quickSync sets error badge** — Manual sync failures now set lastSyncError on auto-sync rules, not just auto-sync failures.
- **Sync All toast type** — All failures = red, some = amber, none = blue (was always amber or blue).

## v1.7.3-beta

### Features
- **Builder sync rules in Builder tab** — Builder synced profiles now shown in Profile Builder tab instead of TRaSH Sync, with distinct tooltips and "Sync All" per tab.
- **Discord notifications for settings changes** — Auto-sync notifications now show profile settings changes (Min Score, Cutoff Score, etc.) and zeroed scores with CF names.

### Bug fixes
- **Create-mode cutoff override preserved** — Cutoff override no longer replaced by first allowed quality when user's chosen cutoff is still valid.
- **Update-mode settings-only changes detected** — HasChanges() now always executes for updates, catching min score and cutoff changes that were previously skipped.
- **Cutoff read-only display shows override** — After Done, cutoff override now shown in amber instead of always showing TRaSH default.

## v1.7.2-beta

### Features
- **Add Extra CFs** — Add any TRaSH CF to a profile via Customize overrides. CFs organized in real TRaSH groups with collapsible headers, toggles, and search. Default scores from profile's score set.
- **Quality overrides redesign** — Dynamic columns, toggle switches, amber override indicator.
- **UI polish** — Column layout for Profile section, toggle switches for override panel, number input spinners removed globally.

### Bug fixes
- **quickSync fallback for importedProfileId** — Pre-v1.7.1 sync history entries now check auto-sync rule as fallback, preventing builder profiles from zeroing on upgrade.
- **Extra CFs persisted** — Restored on resync, included in auto-sync rules and quickSync.
- **Extra CF browser wrong type** — Reset on profile switch to prevent showing radarr CFs for sonarr.
- **Resync loads grouped browser** — extraCFGroups populated after resync (was empty).
- **Reset to TRaSH clears Extra CFs** — Toggle, search, and selections all cleared.
- **CF name casing auto-corrected** — CFs with wrong casing (e.g. HULU vs Hulu) are now updated to match TRaSH's canonical name on sync.
- **Orphaned scores case-insensitive** — Maintenance Reset Non-Synced Scores no longer flags CFs with different casing as out of sync.
- **Tooltip links clickable** — SQP description tooltips now have styled, clickable links. Tooltip stays visible when hovering over it.
- **CF info icon more readable** — Info icon and trash ID in builder now use lighter color for better visibility.

## v1.7.1-beta

### Features
- **Per-CF score overrides on ALL CFs** — Score overrides now work on required CFs and core formatItems, not just optional. Enables overriding scores on CFs like Anime Dual Audio while keeping everything else synced with TRaSH.
- **Create New button** — Duplicate a synced profile as a new Arr profile with different settings. Available on both TRaSH and builder profiles.
- **Builder badge in Sync Rules** — Blue "Builder" tag identifies profiles from Profile Builder.
- **Info banner for builder edits** — Warning when editing builder profiles from Sync Rules that changes affect the profile itself.
- **Sync behavior in create mode** — Add/Scores dropdowns with dynamic descriptions.
- **Edit/Sync/Sync All** — Sync Rules buttons for quick actions with toast result summaries.
- **Custom CF amber grouping** — Custom CFs in dedicated amber-styled category.
- **Toast notifications** — Centered, progress bar, multiline for Sync All breakdown.
- **Profile group sorting** — Standard → Anime → French → German → SQP.

### Bug fixes
- **Builder profile resync zeroed scores** — Resync/quickSync from TRaSH Sync tab fell back to TRaSH base profile instead of imported profile. Now correctly sends importedProfileId.
- **Edit from Sync Rules opened wrong view** — Builder profiles now open in builder editor with correct values.
- **Dry-run/apply reset to TRaSH profile** — After dry-run on imported profiles, code opened TRaSH base profile detail, losing all builder settings.
- **Instance data survives delete+recreate** — Orphan migration now checks instance type to prevent cross-type contamination.
- **Multi-instance support** — Builder sync functions find correct instance from sync history instead of assuming first.
- **API key field appeared empty** — Edit mode shows "Leave empty to keep current key".
- **Stale _resyncReturnSubTab** — Cleared on manual tab switch to prevent stale navigation state.
- **History matching for imported profiles** — Also checks importedProfileId for profiles without trashProfileId.
- **Prowlarr test connection** — Fixed "authentication failed (HTTP 401)" when testing Prowlarr after page refresh.

### Refactoring
- **Generic FileStore[T]** — profileStore 239→14 lines, customCFStore 248→76 lines.
- **Handler helpers** — decodeJSON/requireInstance reduce boilerplate across 10+ handlers.
- **22 unit tests** — sync behavior, field conversion, score resolution, FileStore.

## v1.7.0-beta

### Features
- **Per-CF score overrides** — Override individual CF scores in sync mode. Enable "CF scores" in Customize overrides to edit scores on optional CFs. Overrides persist through auto-sync and resync.
- **Edit/Sync/Sync All buttons** — Sync Rules now has Edit (open profile), Sync (one-click resync), and Sync All (resync all profiles on instance) with toast result summary.
- **Custom CF amber grouping** — Custom CFs displayed in a dedicated amber-styled "Custom" category in CF browser.
- **Sync behavior in create mode** — Add and Scores dropdowns now visible when creating new profiles. Dynamic descriptions explain each option.
- **Profile group sorting** — Standard → Anime → French → German → SQP. New TRaSH groups appear before SQP.
- **Toast notifications** — Centered top, progress bar, auto-dismiss. Used for sync results, cleanup events, and errors.
- **Auto-sync rule on every sync** — Syncing a profile always creates an auto-sync rule (disabled by default). Toggle on/off directly from Sync Rules.
- **Multiple profiles from same TRaSH source** — Same TRaSH profile synced to multiple Arr profiles with different overrides and CF selections.
- **Discord cleanup notifications** — Amber embed when synced profiles are auto-removed because the Arr profile was deleted.
- **Friendly connection errors** — User-friendly messages instead of raw TCP errors in Discord and Settings.
- **Instance data survives delete+recreate** — Sync history and rules preserved when instance is removed and re-added.

### Refactoring
- **Generic FileStore[T]** — Replaced duplicated CRUD in profileStore (239→14 lines) and customCFStore (248→76 lines).
- **Handler helpers** — `decodeJSON` and `requireInstance` reduce boilerplate across 10+ handlers.
- **22 unit tests** — Coverage for sync behavior, field conversion, score resolution, and FileStore operations.

### Bug fixes
- **Cutoff error on resync** — Cutoff resolved against stale quality items. Now resolved after rebuild.
- **Min Score / overrides not syncing** — Overrides not applied in create mode, not saved in auto-sync rules, not sent when only profile settings changed.
- **Resync didn't restore settings** — Optional CFs, overrides, behavior, target profile, and score overrides now fully restored.
- **SnapshotAppData missing Naming deep-copy** — Shared pointer could cause data corruption on concurrent access.
- **Custom CF field format** — TRaSH `{"value":X}` now converted to Arr array format on write, preventing HTTP 400 errors.
- **Deleted auto-sync rule still running** — Race condition fix with fresh config re-check before execution.
- **Same TRaSH profile overwrote sync history** — Rekeyed from trashProfileId to arrProfileId throughout.
- **Stale sync history after profile deletion** — Auto-cleaned on pull, page load, with Discord notification.
- **Create mode contaminated existing profile** — syncForm.arrProfileId now reset when switching to create mode.
- **Keep List search, File Naming feedback, confirm modals** — Various UI fixes from user reports.
- **Connection errors spammed Discord** — Friendly message, only on startup or new TRaSH changes.
- **API key field appeared empty on edit** — Now shows "Leave empty to keep current key".

## v1.6.1-beta

(Superseded by v1.7.0-beta — not released separately)

## v1.6.0-beta

### Features
- **Quality items sync** — Auto-sync now detects and updates quality item changes (allowed/disallowed qualities). Previously only CFs and scores were synced.
- **Detailed Discord notifications** — Auto-sync notifications now show exactly what changed: CF names created/updated, score changes (old → new), and quality item changes (Enabled → Disabled)
- **Startup auto-repair** — On container start, resets auto-sync commit hashes (ensures all rules re-evaluate) and removes broken rules with arrProfileId=0

### Bug fixes
- **Quality items not applied** — Quality item rebuild was running before the `updated` flag, so changes were never sent to Arr
- **Quality items reversed** — Update mode now correctly reverses item order to match Arr API expectations (same as create mode)
- **Spurious quality notification** — "Quality items updated" no longer shown when nothing actually changed

## v1.5.0-beta

### Features
- **Debug logging** — Enable in Settings to write detailed operations to `/config/debug.log`. Logs sync, compare, auto-sync, and UI actions. Download button for easy sharing when reporting issues.
- **Compare: sync history awareness** — Compare uses Clonarr sync history to accurately identify which score-0 CFs were deliberately synced vs unused defaults. Works best with profiles synced via Clonarr.
- **Auto-sync per-profile toggle** — Enable/disable auto-sync individually for each profile directly from Sync Rules & History. Global toggle removed from Settings.
- **Auto-sync error visibility** — Failed auto-sync rules show error badge with tooltip in Sync Rules

### Improvements
- **Settings: auto-sync clarification** — Description explains that auto-sync triggers on TRaSH pull changes, not on a fixed schedule
- **Settings: active rules moved** — Auto-sync rules managed under Profiles → TRaSH Sync instead of Settings
- **Compare: info note** — Visible warning about score-0 limitations for profiles not synced via Clonarr

### Bug fixes
- **Compare: score-0 CFs** — CFs synced with score 0 via Clonarr now correctly shown as "in use"
- **Sync: case-insensitive BuildArrProfile** — Score assignment no longer fails for mixed-case CF names

## v1.4.0-beta

### Features
- **Profiles tab reorganized** — Three sub-tabs: TRaSH Sync, Profile Builder, and Compare
- **Compare Profiles redesigned** — Uses TRaSH CF groups with per-group status badges, only flags actual errors (wrong scores on active CFs, missing required CFs)
- **Compare: auto-sync from Compare** — Sync fixes and enable auto-sync directly from comparison results
- **Auto-select instance** — When only one instance per type exists, automatically selected across all functions
- **Auto-sync rule auto-update** — Existing auto-sync rules automatically updated with new selections when you re-sync

### Improvements
- **Compare: smart verification** — Optional CFs with score 0 are not flagged as errors, exclusive groups (Golden Rule, SDR) verified correctly
- **Compare: "Extra in Arr"** — CFs not in the TRaSH profile shown with removal option
- **Sync Rules & History** — Visible in TRaSH Sync tab with auto-sync badges and re-sync/remove buttons
- **Profile Builder** — Moved to dedicated tab with description and prominent Create/Import buttons
- **Consistent status display** — All instance selectors show Connected/Failed/Not tested uniformly
- **Descriptions** — Added tab descriptions for TRaSH Sync, Profile Builder, and Compare

### Bug fixes
- **Compare: HTML rendering** — TRaSH descriptions now render HTML correctly (was showing raw tags)
- **Compare: category colors** — Group cards show colored left borders matching TRaSH categories
- **Maintenance cleaned up** — Only Cleanup and Backup/Restore remain (Compare moved to Profiles)

## v1.3.0-beta

### Features
- **TRaSH JSON export sort order** — Matches TRaSH convention (grouped CFs by score, Tiers, Repack, Unwanted, Resolution)
- **Case-insensitive CF matching** — Handles name mismatches like HULU/Hulu across sync, compare, and single-CF operations
- **Builder: formatItems group display** — CFs in formatItems shown in their TRaSH group with Fmt state (e.g. Audio in SQP-3 Audio)
- **Variant dropdowns with templates** — Golden Rule and Misc variants auto-detected and visible when loading templates

### Bug fixes
- **syncSingleCF updates CF specs** — Not just score, also corrects name and specifications
- **pdHasOverrides tautology** — Copy-paste error causing override banner to always show
- **SelectedCFs deep copy** — Fixed concurrency bug in config store
- **Resync restore** — Correctly sets deselected CFs to false (not just selected to true)
- **Resync loads sync history** — Synced Profiles section now appears immediately in Maintenance

## v1.2.0-beta

### Features
- **Sync view refactored to TRaSH groups** — Replaced custom category grouping with TRaSH CF groups (matches Notifiarr's approach)
- **Group toggles** — Include/exclude groups from sync, required CFs shown with lock icon
- **"All" toggle** — Bulk toggle for optional groups with 3+ CFs
- **Group descriptions** — TRaSH descriptions visible when expanded, bold amber warnings
- **Cutoff override dropdown** — Select from allowed quality items, TRaSH default, or "Don't sync cutoff"
- **Profile Builder: "Add more CFs"** — Search field with live filtering and "Clear All" button
- **Instance connection status** — Quality Size, File Naming, Maintenance tabs show actual connection status
- **Tab persistence** — Last selected tab saved to localStorage
- **Resync from Maintenance** — Opens profile detail with previously synced optional CFs restored from sync history

### Bug fixes
- **Sync engine fix** — Group toggles now actually affect dry-run/sync (required CFs from disabled groups properly excluded)
- **Custom cutoff values** — Now correctly sent to backend (was broken before)
- **CI hardening** — GitHub Actions pinned to commit SHAs, removed redundant lowercase step

## v1.1.0-beta

### Features
- **Profile Builder refactored to TRaSH group system** — Group-based model replacing per-CF Req/Opt/Opt★ categories
- **Three-state CF pills** — Req (green), Opt (yellow), Fmt (blue) with click-to-cycle
- **Group-level state controls** — Set all CFs in a group at once via header pills
- **Golden Rule fix** — Only selected variant enabled (HD or UHD), not both
- **TRaSH JSON export** — Strict format matching TRaSH sync expectations
- **Group includes export** — Optional checkbox shows `quality_profiles.include` snippets
- **File Naming redesign** — Media server tabs (Standard/Plex/Emby/Jellyfin), instance selector, combined info boxes
- **Profile Builder spec** — Complete specification document for the group system

## v1.0.0-beta

### Features
- **Profile sync** — Sync quality profiles from TRaSH Guides to Radarr/Sonarr instances
- **Profile Builder** — Create custom quality profiles with CF selection and scoring
- **Quality Size sync** — Sync quality size limits from TRaSH Guides
- **File Naming sync** — Apply TRaSH recommended naming conventions
- **Multi-instance support** — Manage multiple Radarr/Sonarr instances
- **Custom CFs** — Create and manage custom format definitions
- **Maintenance tab** — View synced profiles, resync, and manage sync history
- **API key security** — Keys masked in all API responses, git flag injection prevention
- **Docker-native** — Go + Alpine.js, port 6060, Alpine-based
