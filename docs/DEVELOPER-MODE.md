# Advanced Mode And TRaSH Schema Fields

Clonarr has two separate switches for power-user and contributor tooling:

- **Advanced Mode** is available in Settings → Advanced. It enables the Advanced tab, including Profile Builder, Scoring Sandbox, CF Group Builder, and Prowlarr settings.
- **Show TRaSH schema fields** appears only when the container is started with `CLONARR_DEV_FEATURES=true`. It exposes TRaSH Guides internal fields used by guide contributors who author or export Custom Formats, CF groups, and profiles in official TRaSH JSON formats.

For regular users building custom CFs for personal use, TRaSH schema fields are **not required**. Scores can be set directly in the Profile Builder without needing trash IDs or trash scores.

**Enable contributor fields:**

1. Add `CLONARR_DEV_FEATURES=true` to the container environment.
2. Restart Clonarr.
3. Open Settings → Advanced.
4. Enable **Show TRaSH schema fields**.

---

## Custom Format Editor

When TRaSH schema fields are enabled, the CF Create/Edit modal shows an extra section with three fields:

### Trash ID

A UUID that uniquely identifies this Custom Format in the TRaSH Guides ecosystem. TRaSH uses these IDs to track CFs across updates — tools like Recyclarr and Clonarr match CFs by this ID.

- Click **Generate** to create a new random UUID
- Leave empty if you don't plan to share the CF with the TRaSH community
- If you're editing a CF imported from TRaSH, this field is pre-filled

### Trash Scores

Score values per profile context. A single CF can have different scores depending on which quality profile type it's used in.

Each entry has:
- **Context** — a dropdown with all TRaSH profile contexts (e.g. `default`, `sqp-1-2160p`, `sqp-5`, `anime-radarr`, `german`, etc.)
- **Score** — the numeric score for that context

**Example:** A "DV (WEBDL)" CF might have:
| Context | Score | Meaning |
|---------|-------|---------|
| default | 0 | Neutral in most profiles |
| sqp-1-2160p | 1500 | Highly preferred in SQP-1 4K |
| sqp-5 | -10000 | Blocked in SQP-5 |

**How it works during sync:**
When Clonarr syncs a profile that uses a specific score context (e.g. `sqp-1-2160p`), it looks up the CF's `trash_scores` for that context and applies the matching score. If no match is found, it falls back to `default`.

**Do you need this?** Only if you want your custom CFs to behave like official TRaSH CFs — with context-dependent scoring across different profile types. For simple use cases (e.g. one CF with one score), just set the score directly in the profile builder instead.

### Description

A markdown-formatted description of the CF. Used by TRaSH Guides to display information about what the CF does and when to use it.

---

## Custom Format Editor — Export TRaSH JSON

When TRaSH schema fields are enabled, an **Export TRaSH JSON** button appears in the CF editor footer. Clicking it copies the CF to your clipboard in the official TRaSH JSON format:

```json
{
  "trash_id": "a1b2c3d4-...",
  "trash_scores": {
    "default": 100,
    "sqp-1-2160p": 1500
  },
  "name": "My Custom Format",
  "includeCustomFormatWhenRenaming": false,
  "specifications": [
    {
      "name": "Resolution",
      "implementation": "ResolutionSpecification",
      "negate": false,
      "required": false,
      "fields": {
        "value": 1080
      }
    }
  ]
}
```

This format is what TRaSH Guides uses in their repository (`docs/json/radarr/cf/*.json`). Use this if you want to contribute a CF back to TRaSH or share it with others using TRaSH-compatible tools.

**Note:** The export format differs from Arr's API format — field arrays are converted to objects (`{name: value}` instead of `[{name, value}]`) and uses snake_case keys (`trash_id`, `trash_scores`).

---

## Custom Format Browser — Trash ID Preview

In the profile builder's CF selection list, each CF shows the first 8 characters of its Trash ID next to the name. This helps identify CFs when debugging or cross-referencing with TRaSH's guide repository.

---

## Profile Builder

When TRaSH schema fields are enabled, the profile Create/Edit modal shows four extra fields:

### Trash ID

A UUID that identifies this profile in the TRaSH Guides ecosystem. Used by TRaSH to track profiles across guide updates.

### Trash Score Set

The score context this profile uses (e.g. `sqp-1-2160p`, `sqp-5`). When syncing, Clonarr uses this value to look up the correct score from each CF's `trash_scores` map. This is what links a profile to its scoring context.

### Group Number

Determines how this profile is grouped in TRaSH's guide structure. Profiles with the same group number appear together.

### Description

HTML-formatted description displayed in TRaSH Guides for this profile.

---

## Summary

| Feature | Location | What it does |
|---------|----------|-------------|
| Trash ID | CF Editor | UUID for TRaSH ecosystem identification |
| Trash Scores | CF Editor | Context-dependent scoring (per profile type) |
| Description | CF Editor | Markdown description for TRaSH Guides |
| Export TRaSH JSON | CF Editor footer | Copy CF in official TRaSH format to clipboard |
| Trash ID preview | CF Browser | Show 8-char ID next to CF names |
| Trash ID | Profile Builder | UUID for TRaSH profile identification |
| Trash Score Set | Profile Builder | Link profile to its scoring context |
| Group Number | Profile Builder | TRaSH guide grouping |
| Description | Profile Builder | HTML description for TRaSH Guides |
