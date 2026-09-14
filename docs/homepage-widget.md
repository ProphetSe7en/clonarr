# Homepage widget

Show Clonarr stats on your [gethomepage](https://gethomepage.dev) dashboard.

> **Available on `:dev` and `:preview` only.** Will ship to `:latest` with the next release.

![Clonarr widget in homepage](images/homepage-widget.png)

## What you need

1. Your Clonarr address (e.g. `http://192.168.1.10:6060`)
2. Your Clonarr **API key**, copied from **Settings → Security**

## Add to your `services.yaml`

Replace `CLONARR_URL` and `YOUR_API_KEY` with your own values:

```yaml
- TRaSH Automation:
    - Clonarr:
        icon: https://raw.githubusercontent.com/ProphetSe7en/clonarr/main/ui/static/icons/clonarr.png
        href: CLONARR_URL
        widget:
            type: customapi
            url: CLONARR_URL/api/widget/summary
            method: GET
            headers:
              X-Api-Key: YOUR_API_KEY
            refreshInterval: 30000
            display: block
            mappings:
              - field: { instances: total }
                label: Instances
              - field: { rules: total }
                label: Profiles
              - field: { trash: nextPull }
                label: Next pull
                format: relativeDate
                defaultValue: "Off"
              - field: { autoSync: nextSync }
                label: Next sync
                format: relativeDate
                defaultValue: "Off"

    - Radarr Profiles:
        widget:
            type: customapi
            url: CLONARR_URL/api/widget/summary
            headers:
              X-Api-Key: YOUR_API_KEY
            refreshInterval: 30000
            display: block
            mappings:
              - field: { rules: radarrTotal }
                label: Total
              # One row per profile you want to show. Add or remove rows as needed.
              - field: { rules: { radarrList: { 0: arrProfileName } } }
                label: " "
              - field: { rules: { radarrList: { 1: arrProfileName } } }
                label: " "

    - Sonarr Profiles:
        widget:
            type: customapi
            url: CLONARR_URL/api/widget/summary
            headers:
              X-Api-Key: YOUR_API_KEY
            refreshInterval: 30000
            display: block
            mappings:
              - field: { rules: sonarrTotal }
                label: Total
              # One row per profile you want to show. Add or remove rows as needed.
              - field: { rules: { sonarrList: { 0: arrProfileName } } }
                label: " "
              - field: { rules: { sonarrList: { 1: arrProfileName } } }
                label: " "
```

## Showing more (or fewer) profiles

Each `radarrList`/`sonarrList` row shows **one** profile. Counting starts at `0`:

- 1 profile  → keep just `0`
- 2 profiles → `0` and `1`
- 3 profiles → `0`, `1`, `2`
- and so on

If you list a number higher than how many profiles you actually have, that row just stays empty — nothing breaks.

## What else you can show

The YAML above is a starter set. You can swap in or add any of these:

| What it shows | Field to put in `field: { ... }` |
|---|---|
| Number of Arr instances configured | `{ instances: total }` |
| Number of Radarr instances | `{ instances: radarr }` |
| Number of Sonarr instances | `{ instances: sonarr }` |
| Number of paused instances | `{ instances: paused }` |
| Total number of sync rules | `{ rules: total }` |
| Number of active sync rules | `{ rules: active }` |
| Number of Radarr profiles synced | `{ rules: radarrTotal }` |
| Number of Sonarr profiles synced | `{ rules: sonarrTotal }` |
| Number of rules with an error | `{ rules: withErrors }` |
| When TRaSH-Guides last pulled | `{ trash: lastPull }` |
| When TRaSH-Guides will pull next | `{ trash: nextPull }` |
| When any rule last synced | `{ autoSync: lastSync }` |
| When the force-sync schedule fires next | `{ autoSync: nextSync }` |
| First error message from any rule | `{ autoSync: lastError }` |
| Name of a specific Radarr profile | `{ rules: { radarrList: { 0: arrProfileName } } }` |
| Name of a specific Sonarr profile | `{ rules: { sonarrList: { 0: arrProfileName } } }` |

For time fields (`lastPull`, `nextPull`, `lastSync`, `nextSync`), add `format: relativeDate` so homepage shows "in 12 minutes" instead of the raw timestamp, and `defaultValue: "Off"` so the tile reads "Off" when that schedule isn't set.

## Keep your API key private

The Clonarr API key gives **full read and write access** to your install. Treat it like a password — never commit `services.yaml` to a public git repo with your real key in it.

## Verify it works

From a terminal:

```bash
curl -H "X-Api-Key: YOUR_API_KEY" CLONARR_URL/api/widget/summary
```

You should get a long JSON response. If you get:

- `401 Unauthorized` — wrong API key
- `404` — your Clonarr is on `:latest`. Upgrade to `:dev` or `:preview`.
- Connection refused — wrong URL or port.
