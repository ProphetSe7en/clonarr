# Clonarr Profile Builder — Specification

**Last Updated:** 2026-03-23
**Based on:** TRaSH team feedback + TRaSH Guides JSON analysis

---

## Purpose

The Profile Builder is a tool for **TRaSH guide contributors** to create and edit quality profiles for the TRaSH Guides repository. The exported JSON must match TRaSH's official format exactly so it can be used by sync tools (Recyclarr, Notifiarr) and the guide itself.

---

## TRaSH's Profile System — How It Works

### Two separate data structures:

**1. Quality Profile JSON** (`quality-profiles/*.json`)
- Contains `formatItems` — a flat map of CF name → trash_id
- These CFs are **mandatory** — sync tools always apply them, users cannot disable them
- Also contains: quality items, cutoff, scores, language, upgrade settings
- Does NOT contain group references

**2. CF Group JSON files** (`cf-groups/*.json`)
- Each file is one independent group (e.g. `[Audio] Audio Formats`, `[Required] Golden Rule HD`)
- Contains `quality_profiles.include` — maps profile names to their trash_ids (which profiles use this group)
- Contains `custom_formats[]` — list of CFs with per-CF flags:
  - `required: true` — when group is active, this CF must be included (user can't deselect it within the group)
  - `required: false` — when group is active, this CF is optional (user can choose)
  - `default: true/false` — TRaSH recommendation for optional CFs
- Contains `default: "true"/"false"` — whether the group itself is on by default

### How sync tools use this:
1. Read profile JSON → apply all `formatItems` (locked, always synced)
2. Read all group files → find groups that `include` this profile
3. Present groups to user → user toggles groups on/off
4. For enabled groups: `required: true` CFs are always added, `required: false` CFs are user's choice

### Key insight:
- `formatItems` = CFs the end user CANNOT change
- Groups = CFs the end user CAN choose
- `required` in a group ≠ "goes in formatItems" — it means "mandatory within the group when active"
- The same CF can be either in formatItems OR in a group, depending on the profile variant

### Example: SQP-3 vs SQP-3 Audio
- SQP-3: Audio is a **group** (14 CFs). User chooses to enable Audio or not.
- SQP-3 Audio: Same 14 Audio CFs are in **formatItems**. User always has them. Audio group does NOT include this profile.
- Only other difference: `minFormatScore` is 550 vs 3350.

---

## Profile Builder UI

### Section 1: Required CFs (formatItems)

- Shows CFs that are in the profile's `formatItems`
- These are CFs **not in any TRaSH group** (Tiers, Repack, LQ, BR-DISK, Unwanted, etc.)
- Each has a toggle (on = in formatItems, off = removed)
- Score input per CF
- "Add more CFs..." expandable section for adding additional ungrouped CFs
- When loading a template: populated from `detail.coreCFs` (profile JSON's formatItems)

### Section 2: CF Groups

- Each TRaSH CF group shown as a **separate card** (not nested under categories)
- Category prefix used only for color-coding, not for grouping
- Sorted by category order: Golden Rule → Audio → HDR → Streaming → Optional → etc.

**Per group:**
- Header with group name, `default` badge, `pick one` badge (exclusive), CF count
- Group toggle = "this profile includes this group" (maps to `quality_profiles.include`)
- Expandable: shows CFs inside the group

**Per CF in a group — three states (clickable pills):**
- `required` (green) — CF is required within the group (from TRaSH `required: true`). Click → moves to formatItems.
- `optional` (gray) — CF is optional within the group (from TRaSH `required: false`). Click → moves to formatItems.
- `formatItems` (blue) — CF has been moved to formatItems (mandatory for profile). Click → moves back to group state.

### Golden Rule Dropdown
- HD / UHD / None
- Only the **selected variant** group is enabled — NOT both
- This is an exclusive choice — the two Golden Rule groups share CFs

### Miscellaneous Dropdown
- Standard / SQP / None
- Enables the matching variant group, disables the other

### Profile Settings
- Name, score set, quality preset, cutoff, scores, language, upgrade settings
- Import from template or instance

---

## Export

### TRaSH Export (for guide / 3rd-party sync tools)
Strict TRaSH format — no Clonarr-specific fields.

```json
{
  "trash_id": "uuid",
  "name": "Profile Name",
  "trash_score_set": "sqp-3",
  "trash_description": "HTML description",
  "group": 99,
  "upgradeAllowed": true,
  "cutoff": "Quality Name",
  "minFormatScore": 550,
  "cutoffFormatScore": 10000,
  "minUpgradeFormatScore": 1,
  "language": "Original",
  "items": [ ... quality items ... ],
  "formatItems": {
    "CF Name": "trash_id",
    "CF Name": "trash_id"
  }
}
```

**Rules:**
- `formatItems` contains ONLY CFs marked as formatItems in the builder
- NO `cfGroupIncludes` field
- NO `requiredCFs`, `defaultOnCFs`, or other Clonarr fields
- `formatItems` is `{ "CF Name": "trash_id" }` — name as key, trash_id as value

### Group Includes Export (optional checkbox)
Generates snippets for each enabled group's `quality_profiles.include` section:

```
"Profile Name": "profile-trash-id"
```

These need to be added to each group's JSON file to link the profile.

### Clonarr Backup (internal)
Can contain any Clonarr-specific fields for restore: `formatItemCFs`, `enabledGroups`, variant choices, scores, etc.

---

## Data Flow

### Loading a template:
1. Fetch profile detail from API
2. `detail.coreCFs` → populate `formatItemCFs` (these are the profile's formatItems)
3. Iterate `pbCategories` → check each group's `includeProfiles` for this profile name
4. Matching groups → set `enabledGroups[groupTrashId] = true`
5. All CFs from enabled groups → add to `selectedCFs`
6. Group CFs do NOT go into `formatItemCFs` — they stay in groups

### Building SQP-3 Audio from SQP-3:
1. Load SQP-3 as template → 20 formatItems + groups (Audio, HDR, Streaming, etc.)
2. Open Audio group → click each CF pill from `required` → `formatItems` (turns blue)
3. Disable Audio group toggle (now CFs are in formatItems, not group)
4. Change minFormatScore to 3350
5. Export → formatItems now has 34 CFs (20 original + 14 Audio)

---

## Internal State (pb object)

```javascript
pb: {
  // Profile settings
  name, appType, scoreSet, upgradeAllowed, cutoff, cutoffScore,
  minFormatScore, minUpgradeFormatScore, language,
  qualityPresetId, qualityItems, cutoff,

  // CF selection
  formatItemCFs: { [trashId]: true },  // CFs in formatItems (mandatory)
  enabledGroups: { [groupTrashId]: true },  // which groups include this profile
  selectedCFs: { [trashId]: true },  // all selected CFs (formatItems + group CFs)
  scoreOverrides: { [trashId]: score },  // custom score overrides

  // Variant dropdowns
  variantGoldenRule: 'HD' | 'UHD' | 'none' | '',
  variantMisc: 'Standard' | 'SQP' | 'none' | '',

  // Template tracking
  templateId, trashProfileId, trashProfileName,
  baselineCFs, coreCFIds,

  // Dev mode
  trashScoreSet, trashDescription, groupNum,
}
```

---

## Rules

1. **formatItems in export = ONLY formatItemCFs** — never include group CFs
2. **No cfGroupIncludes in export** — TRaSH format doesn't have it
3. **Groups are separate entities** — each JSON file = one group = one card in UI
4. **required in group ≠ formatItems** — required means "mandatory within group when active"
5. **Golden Rule: pick one** — only selected variant group enabled
6. **Group order**: Golden Rule → Audio → HDR → HQ Release Groups → Resolution → Streaming → Misc/Optional → SQP → Release Groups → Unwanted → Movie Versions → Anime → French → German → Language → Other
7. **Template loading**: coreCFs → formatItemCFs, groups via include → enabledGroups
8. **Export format must match TRaSH exactly** — compare with existing profiles in repo
