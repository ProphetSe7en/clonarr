# Changelog

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
