// Scheme labels + display order, shared by getNamingSections (the manual
// scheme list) and the auto-sync scheme picker (which lists ALL schemes for a
// field, unfiltered by the media-server tab, since a binding targets one scheme
// key independent of the active tab).
const NAMING_SCHEME_LABELS = {
  'standard': 'Standard',
  'default': 'Default',
  'original': 'Original Title',
  'p2p-scene': 'P2P / Scene',
  'plex-imdb': 'Plex (IMDb)',
  'plex-tmdb': 'Plex (TMDb)',
  'plex-tvdb': 'Plex (TVDb)',
  'plex-anime-imdb': 'Plex Anime (IMDb)',
  'plex-anime-tmdb': 'Plex Anime (TMDb)',
  'plex-edition-alt-imdb': 'Plex Edition Alternative (IMDb)',
  'plex-edition-alt-tmdb': 'Plex Edition Alternative (TMDb)',
  'emby-imdb': 'Emby (IMDb)',
  'emby-tmdb': 'Emby (TMDb)',
  'emby-tvdb': 'Emby (TVDb)',
  'emby-anime-imdb': 'Emby Anime (IMDb)',
  'emby-anime-tmdb': 'Emby Anime (TMDb)',
  'jellyfin-imdb': 'Jellyfin (IMDb)',
  'jellyfin-tmdb': 'Jellyfin (TMDb)',
  'jellyfin-tvdb': 'Jellyfin (TVDb)',
  'jellyfin-anime-imdb': 'Jellyfin Anime (IMDb)',
  'jellyfin-anime-tmdb': 'Jellyfin Anime (TMDb)',
};
const NAMING_KEY_ORDER = ['standard', 'default', 'plex-imdb', 'plex-tmdb', 'plex-anime-imdb', 'plex-anime-tmdb',
  'plex-edition-alt-imdb', 'plex-edition-alt-tmdb', 'plex-tvdb',
  'emby-imdb', 'emby-tmdb', 'emby-anime-imdb', 'emby-anime-tmdb', 'emby-tvdb',
  'jellyfin-imdb', 'jellyfin-tmdb', 'jellyfin-anime-imdb', 'jellyfin-anime-tmdb', 'jellyfin-tvdb',
  'original', 'p2p-scene'];
const namingSchemeLabel = (key) =>
  NAMING_SCHEME_LABELS[key] || key.replace(/-/g, ' ').replace(/\b\w/g, c => c.toUpperCase());

// Maps a UI section key to the canonical auto-sync field key the backend uses
// (internal/api/naming.go namingArrKey). Auto-syncable fields only — specials
// is intentionally manual-only and absent here.
const NAMING_SECTION_FIELD = {
  'file': 'movieFile',
  'folder': 'movieFolder',
  'episodes-standard': 'standardEpisode',
  'episodes-daily': 'dailyEpisode',
  'episodes-anime': 'animeEpisode',
  'series': 'seriesFolder',
  'season': 'seasonFolder',
};

export default {
  state: {
    namingData: {},
    namingInstanceData: {},
    namingApplyResult: {},
    namingMediaServer: {},
    namingFAQExpanded: false,
    namingIDInfoExpanded: false,
    // Per-section card expansion state. Keyed by sectionKey within each
    // appType. Default: undefined (treated as collapsed). Lets each card
    // open independently so users only see the section they're working on.
    namingSectionExpanded: {},
    // Auto-sync (#5): per-appType map of canonical field key -> binding
    // {scheme, lastFingerprint, lastAppliedAt, lastError}. Opt-in, default
    // empty (nothing auto-syncs). Mirrors the backend NamingAutoSync map.
    namingAutoSync: {},
    // Per-appType map of field -> {scheme, fingerprint, appliedAt}: what clonarr
    // last applied (manual or auto). Source for the scheme label + Sync-now on
    // fields that were synced manually (no auto-sync binding).
    namingApplied: {},
    namingAutoSyncBusy: false,
    namingDriftChecking: false,
    namingSyncing: false,
    // Per-appType map of field -> [change events]; inline per-field history.
    namingChanges: {},
    namingFieldHistoryOpen: {},
  },

  methods: {
    async loadNaming(appType) {
      try {
        const r = await fetch(`/api/trash/${appType}/naming`);
        if (r.ok) {
          const data = await r.json();
          this.namingData = { ...this.namingData, [appType]: data };
        }
      } catch (e) { /* ignore */ }
    },

    getNaming(appType) {
      return this.namingData[appType] || null;
    },

    getNamingSections(appType) {
      const n = this.getNaming(appType);
      if (!n) return [];

      // Labels only — descriptions are sourced from TRaSH JSON if present
      // (currently absent across all keys; TRaSH plans to add descriptions per
      // scheme in a future release). When that lands, makeSchemes below reads
      // them straight from the JSON and the existing UI render-paths pick them
      // up automatically (HTML uses x-show="scheme.description" / "section.description").
      // No "recommended" flag — Clonarr does not editorialize on top of TRaSH's
      // own JSON; users decide which variant suits them.
      // Single source of truth: module-level NAMING_SCHEME_LABELS (also used by
      // the auto-sync picker). Adapted to the {label} shape this code expects.
      const schemeDesc = Object.fromEntries(
        Object.entries(NAMING_SCHEME_LABELS).map(([k, label]) => [k, { label }])
      );

      // Each scheme is tagged with its media-server group. The UI renders ALL
      // schemes once and shows/hides them per the section's own tab via x-show —
      // so switching tabs never changes the section.schemes array, the x-for
      // never tears down, and the scroll position never jumps. Each variant-having
      // section has its OWN tab (section-scoped), so flipping one card's tab does
      // not touch another's. Sections that don't vary always show their schemes.
      const groupOf = (key) =>
        key.startsWith('plex-') ? 'plex' :
          key.startsWith('emby-') ? 'emby' :
            key.startsWith('jellyfin-') ? 'jellyfin' : 'standard';

      // A section "varies per server" if its key map contains any
      // plex-/emby-/jellyfin- prefixed keys. Used to decide whether
      // to render the section-scoped tab bar.
      const sectionVaries = (map) => {
        for (const k of Object.keys(map || {})) {
          if (k.startsWith('plex-') || k.startsWith('emby-') || k.startsWith('jellyfin-')) return true;
        }
        return false;
      };

      const radarrExamples = {
        folder: {
          'default': 'The Movie Title (2010)',
          'plex-imdb': 'The Movie Title (2010) {imdb-tt1520211}',
          'plex-tmdb': 'The Movie Title (2010) {tmdb-345691}',
          'emby-imdb': 'The Movie Title (2010) [imdb-tt1520211]',
          'emby-tmdb': 'The Movie Title (2010) [tmdb-345691]',
          'jellyfin-imdb': 'The Movie Title (2010) [imdbid-tt1520211]',
          'jellyfin-tmdb': 'The Movie Title (2010) [tmdbid-345691]',
        },
        file: {
          'standard': 'The Movie Title (2010) {edition-Ultimate Extended Edition} [IMAX HYBRID][Bluray-1080p Proper][3D][DV HDR10][DTS 5.1][x264]-RlsGrp',
          'original': 'The.Movie.Title.2010.REMASTERED.1080p.BluRay.x264-RlsGrp',
          'p2p-scene': 'The.Movie.Title.2010.Ultimate.Extended.Edition.3D.Hybrid.Remux-2160p.TrueHD.Atmos.7.1.DV.HDR10Plus.HEVC-RlsGrp',
          'plex-imdb': 'The Movie Title (2010) {imdb-tt1520211} - {edition-Ultimate Extended Edition} [IMAX HYBRID][Bluray-1080p Proper][3D][DV HDR10][DTS 5.1][x264]-RlsGrp',
          'plex-tmdb': 'The Movie Title (2010) {tmdb-345691} - {edition-Ultimate Extended Edition} [IMAX HYBRID][Bluray-1080p Proper][3D][DV HDR10][DTS 5.1][x264]-RlsGrp',
          'plex-anime-imdb': 'The Movie Title (2010) {imdb-tt1520211} - {edition-Ultimate Extended Edition} [Surround Sound x264][Bluray-1080p Proper][3D][DTS 5.1][DE][10bit][AVC]-RlsGrp',
          'plex-anime-tmdb': 'The Movie Title (2010) {tmdb-345691} - {edition-Ultimate Extended Edition} [Surround Sound x264][Bluray-1080p Proper][3D][DTS 5.1][DE][10bit][AVC]-RlsGrp',
          'emby-imdb': 'The Movie Title (2010) [imdb-tt0066921] - {edition-Ultimate Extended Edition} [IMAX HYBRID][Bluray-1080p Proper][3D][DV HDR10][DTS 5.1][x264]-RlsGrp',
          'emby-tmdb': 'The Movie Title (2010) [tmdb-345691] - {edition-Ultimate Extended Edition} [IMAX HYBRID][Bluray-1080p Proper][3D][DV HDR10][DTS 5.1][x264]-RlsGrp',
          'emby-anime-imdb': 'The Movie Title (2010) [imdb-tt0066921] - {edition-Ultimate Extended Edition} [Surround Sound x264][Bluray-1080p Proper][3D][DTS 5.1][DE][10bit][AVC]-RlsGrp',
          'emby-anime-tmdb': 'The Movie Title (2010) [tmdb-345691] - {edition-Ultimate Extended Edition} [Surround Sound x264][Bluray-1080p Proper][3D][DTS 5.1][DE][10bit][AVC]-RlsGrp',
          'jellyfin-imdb': 'The Movie Title (2010) [imdbid-tt0106145] - {edition-Ultimate Extended Edition} [IMAX HYBRID][Bluray-1080p Proper][3D][DV HDR10][DTS 5.1][x264]-RlsGrp',
          'jellyfin-tmdb': 'The Movie Title (2010) [tmdbid-345691] - {edition-Ultimate Extended Edition} [IMAX HYBRID][Bluray-1080p Proper][3D][DV HDR10][DTS 5.1][x264]-RlsGrp',
          'jellyfin-anime-imdb': 'The Movie Title (2010) [imdbid-tt0106145] - {edition-Ultimate Extended Edition} [Surround Sound x264][Bluray-1080p Proper][3D][DTS 5.1][DE][10bit][AVC]-RlsGrp',
          'jellyfin-anime-tmdb': 'The Movie Title (2010) [tmdbid-345691] - {edition-Ultimate Extended Edition} [Surround Sound x264][Bluray-1080p Proper][3D][DTS 5.1][DE][10bit][AVC]-RlsGrp',
        }
      };

      const sonarrExamples = {
        series: {
          'default': 'The Series Title! (2010)',
          'plex-imdb': 'The Series Title! (2010) {imdb-tt1520211}',
          'plex-tvdb': 'The Series Title! (2010) {tvdb-1520211}',
          'emby-imdb': 'The Series Title! (2010) [imdb-tt1520211]',
          'emby-tvdb': 'The Series Title! (2010) [tvdb-1520211]',
          'jellyfin-imdb': 'The Series Title! (2010) [imdbid-tt1520211]',
          'jellyfin-tvdb': 'The Series Title! (2010) [tvdbid-1520211]',
        },
        episodes: {
          standard: { 'default': 'The Series Title! (2010) - S01E01 - Episode Title 1 [AMZN WEBDL-1080p Proper][DV HDR10][DTS 5.1][x264]-RlsGrp' },
          daily: { 'default': 'The Series Title! (2010) - 2013-10-30 - Episode Title 1 [AMZN WEBDL-1080p Proper][DV HDR10][DTS 5.1][x264]-RlsGrp' },
          anime: { 'default': 'The Series Title! (2010) - S01E01 - 001 - Episode Title 1 [iNTERNAL HDTV-720p v2][HDR10][10bit][x264][DTS 5.1][JA]-RlsGrp' },
        }
      };

      // Enforce consistent ordering (shared module-level order)
      const keyOrder = NAMING_KEY_ORDER;

      // Pattern values can be plain strings (current TRaSH JSON shape) OR
      // objects with `pattern`/`description` once TRaSH adds per-scheme
      // descriptions. Code handles both shapes so the future migration is
      // a no-op for us.
      const makeSchemes = (map, sectionKey, examplesMap) => {
        // Return ALL schemes (no tab filtering here — the template shows/hides
        // by group via x-show, which keeps the array stable across tab switches).
        const entries = Object.entries(map || {});
        entries.sort((a, b) => {
          const ai = keyOrder.indexOf(a[0]), bi = keyOrder.indexOf(b[0]);
          return (ai === -1 ? 999 : ai) - (bi === -1 ? 999 : bi);
        });
        return entries.map(([key, value]) => {
          const meta = schemeDesc[key] || { label: key.replace(/-/g, ' ').replace(/\b\w/g, c => c.toUpperCase()) };
          const pattern = typeof value === 'string' ? value : (value?.pattern || '');
          const trashDesc = typeof value === 'object' ? (value?.description || '') : '';
          // Tier classifies the scheme on the Standard tab so the UI can
          // insert an "Alternative Naming Options" divider between the
          // recommended scheme (`standard`/`default`) and the alternatives
          // (`original`, `p2p-scene`). Server-prefixed schemes (plex-/emby-/
          // jellyfin-) are always 'recommended' — no alt-naming concept on
          // those tabs, so the divider naturally won't render there.
          const tier = (key === 'original' || key === 'p2p-scene') ? 'alternative' : 'recommended';
          return {
            key,
            label: meta.label || key,
            description: trashDesc,
            pattern,
            example: examplesMap?.[key] || '',
            tier,
            group: groupOf(key),
          };
        });
      };

      const sections = [];

      // No editorial section descriptions — Clonarr does not commentary on top
      // of TRaSH JSON. When TRaSH ships descriptions per scheme/section in JSON,
      // makeSchemes above reads them through automatically.

      if (appType === 'radarr') {
        // File format first, folder second. Both vary per media server,
        // so each gets its own section-scoped tab bar in the UI.
        const fileVaries = sectionVaries(n.file);
        const fileSchemes = makeSchemes(n.file, 'file', radarrExamples.file);
        if (fileSchemes.length > 0) {
          sections.push({
            key: 'file',
            label: 'Standard Movie Format',
            exampleLabel: 'Movie',
            description: '',
            varies: fileVaries,
            schemes: fileSchemes,
          });
        }
        const folderVaries = sectionVaries(n.folder);
        const folderSchemes = makeSchemes(n.folder, 'folder', radarrExamples.folder);
        if (folderSchemes.length > 0) {
          sections.push({
            key: 'folder',
            label: 'Movie Folder Format',
            exampleLabel: 'Folder',
            description: '',
            varies: folderVaries,
            schemes: folderSchemes,
          });
        }
      } else {
        // Sonarr: episode formats don't vary per server (TRaSH JSON only
        // ships default/original/p2p-scene per type, no plex-/emby-/jellyfin-
        // variants). Series folder DOES vary. Season folder doesn't.
        // Display order: Standard → Anime → Daily (per user preference;
        // matches likely-of-use frequency for most libraries).
        const epTypeOrder = ['standard', 'anime', 'daily'];
        for (const epType of epTypeOrder) {
          const schemes = n.episodes?.[epType];
          if (!schemes) continue;
          const epLabel = epType.charAt(0).toUpperCase() + epType.slice(1);
          const epVaries = sectionVaries(schemes);
          const epSchemes = makeSchemes(schemes, epType, sonarrExamples.episodes?.[epType]);
          if (epSchemes.length > 0) {
            sections.push({
              key: 'episodes-' + epType,
              label: 'Episode Format — ' + epLabel,
              exampleLabel: 'Episode',
              description: '',
              varies: epVaries,
              schemes: epSchemes,
            });
          }
        }
        const seriesVaries = sectionVaries(n.series);
        const seriesSchemes = makeSchemes(n.series, 'series', sonarrExamples.series);
        if (seriesSchemes.length > 0) sections.push({
          key: 'series',
          label: 'Series Folder Format',
          exampleLabel: 'Series',
          description: '',
          varies: seriesVaries,
          schemes: seriesSchemes,
        });
        // Season folder: TRaSH ships only `default` for season — single
        // server-agnostic option. No tab bar needed.
        if (n.season) {
          const seasonSchemes = Object.entries(n.season).map(([key, value]) => {
            const meta = schemeDesc[key] || { label: key.replace(/-/g, ' ').replace(/\b\w/g, c => c.toUpperCase()) };
            const pattern = typeof value === 'string' ? value : (value?.pattern || '');
            const trashDesc = typeof value === 'object' ? (value?.description || '') : '';
            return { key, label: meta.label || key, description: trashDesc, pattern, example: key === 'default' ? 'Season 01' : '' };
          });
          sections.push({
            key: 'season',
            label: 'Season Folder Format',
            exampleLabel: 'Season',
            description: '',
            varies: false,
            schemes: seasonSchemes,
          });
        }
      }

      return sections;
    },

    getInstanceName(appType, instId) {
      const inst = this.instances.find(i => i.id === instId);
      return inst ? inst.name : '';
    },

    async loadInstanceNaming(appType) {
      const instId = this.mediaInstanceId[appType];
      if (!instId) {
        this.namingInstanceData = { ...this.namingInstanceData, [appType]: null };
        return;
      }
      try {
        const r = await fetch(`/api/instances/${instId}/naming`);
        if (r.ok) {
          const data = await r.json();
          this.namingInstanceData = { ...this.namingInstanceData, [appType]: data };
        }
      } catch (e) { console.error('Failed to load instance naming:', e); }
    },

    // Maps section keys to the matching field on namingInstanceData so the
    // confirm-modal can show "Currently: X / Change to: Y" before applying.
    namingCurrentFieldFor(appType, sectionKey) {
      if (appType === 'radarr') {
        if (sectionKey === 'file') return 'standardMovieFormat';
        if (sectionKey === 'folder') return 'movieFolderFormat';
      } else {
        if (sectionKey === 'episodes-standard') return 'standardEpisodeFormat';
        if (sectionKey === 'episodes-daily') return 'dailyEpisodeFormat';
        if (sectionKey === 'episodes-anime') return 'animeEpisodeFormat';
        if (sectionKey === 'series') return 'seriesFolderFormat';
        if (sectionKey === 'season') return 'seasonFolderFormat';
      }
      return null;
    },

    // Token-level diff: split both patterns on token boundaries (curly
    // braces, brackets, dots, dashes, spaces preserved as separators) and
    // mark each token as added / removed / unchanged via simple LCS-like
    // pass. Good enough for naming patterns which are usually mostly the
    // same with one or two token swaps. Returns array of {text, type}
    // where type is 'same' / 'add' / 'remove'.
    //
    // Brace regex handles depth-2 nesting (e.g. {edition-{Edition Tags}},
    // {tmdb-{TmdbId}}) which TRaSH patterns use — flat `[^}]*` would
    // truncate at the first `}` and leave the outer closing brace as a
    // stray token. Bracket regex stays flat — TRaSH bracket tokens are
    // never nested.
    diffNamingTokens(oldPattern, newPattern) {
      const tokenize = (s) => (s || '').split(/(\{(?:[^{}]|\{[^{}]*\})*\}|\[[^\]]*\]|[ .\-_])/g).filter(t => t !== '');
      const a = tokenize(oldPattern);
      const b = tokenize(newPattern);
      // LCS table
      const m = a.length, n = b.length;
      const dp = Array.from({ length: m + 1 }, () => new Array(n + 1).fill(0));
      for (let i = 1; i <= m; i++) {
        for (let j = 1; j <= n; j++) {
          dp[i][j] = a[i - 1] === b[j - 1] ? dp[i - 1][j - 1] + 1 : Math.max(dp[i - 1][j], dp[i][j - 1]);
        }
      }
      // Backtrack to build diff
      const out = [];
      let i = m, j = n;
      while (i > 0 || j > 0) {
        if (i > 0 && j > 0 && a[i - 1] === b[j - 1]) {
          out.unshift({ text: a[i - 1], type: 'same' });
          i--; j--;
        } else if (j > 0 && (i === 0 || dp[i][j - 1] >= dp[i - 1][j])) {
          out.unshift({ text: b[j - 1], type: 'add' });
          j--;
        } else {
          out.unshift({ text: a[i - 1], type: 'remove' });
          i--;
        }
      }
      return out;
    },

    // Build the HTML for the current/new pattern comparison. Used by both
    // confirmApplyNamingScheme (with Apply button) and compareNamingScheme
    // (read-only). Centralizes the diff rendering so both flows share the
    // same visualization.
    namingComparisonHTML(appType, sectionKey, scheme) {
      const fieldName = this.namingCurrentFieldFor(appType, sectionKey);
      const rawCurrent = fieldName ? this.namingInstanceData[appType]?.[fieldName] : null;
      const isUnset = !rawCurrent;
      const currentPattern = rawCurrent || '(not set)';
      const newPattern = scheme.pattern;
      const escapeHtml = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');

      // First-time set: instance has no current value. Skip the diff row —
      // showing "(not set)" tokens marked red-strikethrough alongside the
      // entire new pattern marked green is technically correct but reads
      // as if something was removed. Just show "Currently: (not set)" +
      // the new pattern; user understands it's a fresh assignment.
      if (isUnset) {
        return (
          '<div style="font-size:11px;color:var(--text-secondary);margin-top:14px;margin-bottom:4px;text-transform:uppercase;letter-spacing:0.5px">Currently on instance</div>' +
          '<div style="font-family:monospace;font-size:12px;background:var(--bg-page);border:1px solid var(--border-subtle);border-radius:3px;padding:6px 8px;color:var(--text-muted);font-style:italic">(not set)</div>' +
          '<div style="font-size:11px;color:var(--text-secondary);margin-top:10px;margin-bottom:4px;text-transform:uppercase;letter-spacing:0.5px">Would set to</div>' +
          '<div style="font-family:monospace;font-size:12px;background:var(--bg-page);border:1px solid var(--border-subtle);border-left:3px solid var(--app-color);border-radius:3px;padding:6px 8px;white-space:nowrap;overflow-x:auto;color:var(--text-body)">' + escapeHtml(newPattern) + '</div>'
        );
      }

      const diff = this.diffNamingTokens(currentPattern, newPattern);
      const diffHTML = diff.map(t => {
        const text = escapeHtml(t.text);
        // Red/green retained ONLY in the diff itself — that's where they
        // carry direct semantic meaning (added/removed tokens). The
        // current/new pattern boxes above use neutral framing so red
        // doesn't read as "current is bad" and green as "new is good".
        if (t.type === 'add') return `<span style="background:var(--accent-green-bg);color:var(--accent-green);padding:0 2px;border-radius:2px">${text}</span>`;
        if (t.type === 'remove') return `<span style="background:var(--accent-red-bg);color:var(--accent-red);padding:0 2px;border-radius:2px;text-decoration:line-through">${text}</span>`;
        return text;
      }).join('');
      return (
        '<div style="font-size:11px;color:var(--text-secondary);margin-top:14px;margin-bottom:4px;text-transform:uppercase;letter-spacing:0.5px">Currently on instance</div>' +
        '<div style="font-family:monospace;font-size:12px;background:var(--bg-page);border:1px solid var(--border-subtle);border-radius:3px;padding:6px 8px;white-space:nowrap;overflow-x:auto;color:var(--text-body)">' + escapeHtml(currentPattern) + '</div>' +
        '<div style="font-size:11px;color:var(--text-secondary);margin-top:10px;margin-bottom:4px;text-transform:uppercase;letter-spacing:0.5px">Would change to</div>' +
        '<div style="font-family:monospace;font-size:12px;background:var(--bg-page);border:1px solid var(--border-subtle);border-left:3px solid var(--app-color);border-radius:3px;padding:6px 8px;white-space:nowrap;overflow-x:auto;color:var(--text-body)">' + escapeHtml(newPattern) + '</div>' +
        '<div style="font-size:11px;color:var(--text-secondary);margin-top:10px;margin-bottom:4px;text-transform:uppercase;letter-spacing:0.5px">Diff</div>' +
        '<div style="font-family:monospace;font-size:12px;background:var(--bg-page);border:1px solid var(--border-subtle);border-radius:3px;padding:6px 8px;white-space:nowrap;overflow-x:auto;color:var(--text-body)">' + diffHTML + '</div>'
      );
    },

    // Read-only comparison modal — shows the same diff as the Apply confirm
    // modal but without committing the change. Useful for evaluating
    // multiple schemes against the current instance state before deciding
    // which to apply.
    compareNamingScheme(appType, sectionKey, scheme) {
      const instId = this.mediaInstanceId[appType];
      if (!instId) return;
      const instName = this.getInstanceName(appType, instId);
      const escapeHtml = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
      const sectionLabel = sectionKey.startsWith('episodes-')
        ? 'Episode (' + sectionKey.slice(9) + ')'
        : sectionKey.charAt(0).toUpperCase() + sectionKey.slice(1);
      const message =
        '<div style="margin-bottom:6px">Comparing <strong>' + escapeHtml(scheme.label) + '</strong> ' + escapeHtml(sectionLabel) + ' naming against <strong>' + escapeHtml(instName) + '</strong>.</div>' +
        this.namingComparisonHTML(appType, sectionKey, scheme);
      this.confirmModal = {
        show: true,
        title: 'Compare naming scheme',
        message,
        html: true,
        wide: true,  // long monospace patterns need room — bumps modal to 760px
        confirmLabel: 'Apply',
        cancelLabel: 'Close',
        // hideCancel defaults to false — both buttons visible. User can
        // close to back out, or Apply directly if the diff looks good.
        // Compare and Sync now lead to the same end state; difference is
        // framing (Compare = "looking at options", Sync = "I know I want
        // this").
        onConfirm: () => this.applyNamingScheme(appType, sectionKey, scheme),
        onCancel: () => {},
      };
    },

    // Open confirmModal with current → new pattern preview before applying.
    // Destructive-ish UI ops should show a concrete preview of what's
    // about to change, not just an "Are you sure?" prompt.
    confirmApplyNamingScheme(appType, sectionKey, scheme) {
      const instId = this.mediaInstanceId[appType];
      if (!instId) return;
      const instName = this.getInstanceName(appType, instId);
      const escapeHtml = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
      const sectionLabel = sectionKey.startsWith('episodes-')
        ? 'Episode (' + sectionKey.slice(9) + ')'
        : sectionKey.charAt(0).toUpperCase() + sectionKey.slice(1);
      const message =
        '<div style="margin-bottom:6px">Apply <strong>' + escapeHtml(scheme.label) + '</strong> ' + escapeHtml(sectionLabel) + ' naming to <strong>' + escapeHtml(instName) + '</strong>?</div>' +
        this.namingComparisonHTML(appType, sectionKey, scheme) +
        '<div style="font-size:11px;color:var(--text-secondary);margin-top:10px;font-style:italic">Existing files in ' + escapeHtml(instName) + ' will be renamed on next sync if "Rename" is enabled there.</div>';
      this.confirmModal = {
        show: true,
        title: 'Apply naming scheme',
        message,
        html: true,
        wide: true,
        confirmLabel: 'Apply',
        cancelLabel: 'Cancel',
        onConfirm: () => this.applyNamingScheme(appType, sectionKey, scheme),
        onCancel: () => {},
      };
    },

    async applyNamingScheme(appType, sectionKey, scheme) {
      const instId = this.mediaInstanceId[appType];
      if (!instId) return;
      const instName = this.getInstanceName(appType, instId);
      // Maps section keys to the request-body field expected by handleApplyNaming
      // in internal/api/instances.go. Backend reads body.{field} and writes it
      // to the matching Arr setting (req.Daily → animeEpisodeFormat, etc.).
      // Pre-fix bug: all episode types fell through to body.file, so syncing
      // Daily or Anime overwrote standardEpisodeFormat instead of the targeted
      // format. Now explicit per-section mapping.
      const fieldMap = {
        'file': 'file',                  // Radarr movie file format → standardMovieFormat
        'folder': 'folder',              // Radarr movie folder format → movieFolderFormat
        'series': 'series',              // Sonarr series folder → seriesFolderFormat
        'season': 'season',              // Sonarr season folder → seasonFolderFormat
        'episodes-standard': 'file',     // Sonarr standardEpisodeFormat (backend uses "file" key for it)
        'episodes-daily': 'daily',       // Sonarr dailyEpisodeFormat
        'episodes-anime': 'anime',       // Sonarr animeEpisodeFormat
      };
      const field = fieldMap[sectionKey];
      if (!field) {
        this.namingApplyResult = { ...this.namingApplyResult, [appType]: `Unknown section: ${sectionKey}` };
        return;
      }
      // schemeKey is recorded (not used to resolve) so this manual sync is
      // drift/update-tracked like an auto-synced field.
      const body = { [field]: scheme.pattern, schemeKey: scheme.key };
      try {
        const r = await fetch(`/api/instances/${instId}/naming`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body)
        });
        if (r.ok) {
          this.namingApplyResult = { ...this.namingApplyResult, [appType]: `Applied "${scheme.label}" ${sectionKey} naming to ${instName}` };
          this.loadInstanceNaming(appType);
          this.loadNamingApplied(appType); // refresh the recorded scheme/fingerprint
          this.loadNamingChanges(appType);
          setTimeout(() => { this.namingApplyResult = { ...this.namingApplyResult, [appType]: '' }; }, 5000);
        } else {
          const err = await r.json().catch(() => ({}));
          this.namingApplyResult = { ...this.namingApplyResult, [appType]: `Failed: ${err.error || r.statusText}` };
        }
      } catch (e) {
        this.namingApplyResult = { ...this.namingApplyResult, [appType]: `Error: ${e.message}` };
      }
    },

    // ===== Auto-sync (#5): keep a naming field on a TRaSH scheme automatically =====

    namingFieldKey(sectionKey) {
      return NAMING_SECTION_FIELD[sectionKey] || null;
    },

    // The current binding for a section/field, or null when not auto-syncing.
    namingBindingFor(appType, sectionKey) {
      const field = this.namingFieldKey(sectionKey);
      if (!field) return null;
      return this.namingAutoSync[appType]?.[field] || null;
    },

    // Binding looked up directly by canonical field key (the "Currently on
    // instance" card keys rows by their canonical field).
    namingBindingForField(appType, field) {
      return this.namingAutoSync[appType]?.[field] || null;
    },

    // The raw TRaSH scheme map for a canonical field (for reverse-matching the
    // instance's current pattern back to a scheme).
    namingRawMapForField(appType, field) {
      const n = this.getNaming(appType);
      if (!n) return null;
      switch (field) {
        case 'movieFile': return n.file;
        case 'movieFolder': return n.folder;
        case 'seriesFolder': return n.series;
        case 'seasonFolder': return n.season;
        case 'standardEpisode': return n.episodes?.standard;
        case 'dailyEpisode': return n.episodes?.daily;
        case 'animeEpisode': return n.episodes?.anime;
      }
      return null;
    },

    // Reverse-match: which TRaSH scheme does the instance's CURRENT pattern for
    // this field equal? Returns {key, label} or null (custom / hand-edited).
    namingMatchedScheme(appType, arrField, canonField) {
      const cur = this.namingInstanceData[appType]?.[arrField];
      if (!cur) return null;
      const map = this.namingRawMapForField(appType, canonField);
      if (!map) return null;
      for (const [key, val] of Object.entries(map)) {
        const pattern = typeof val === 'string' ? val : (val?.pattern || '');
        if (pattern === cur) return { key, label: namingSchemeLabel(key) };
      }
      return null;
    },

    // Compact drift descriptor for the "Currently on instance" row, so the
    // template can read it without nesting an x-for (which broke the layout).
    namingDrift(appType, row) {
      const st = this.namingFieldStatus(appType, row);
      return { drifted: st.drifted, expected: st.expected || '', label: st.label || '' };
    },

    // The TRaSH pattern a field is bound to (what auto-sync would apply), or '' .
    namingExpectedPattern(appType, field, scheme) {
      const map = this.namingRawMapForField(appType, field);
      if (!map) return '';
      const val = map[scheme];
      return typeof val === 'string' ? val : (val?.pattern || '');
    },

    // Status descriptor for a "Currently on instance" row. kind is one of:
    //  managed — clonarr has synced this field (auto OR manual). `.auto` = auto-sync
    //            on. `.drifted` / `.updatable` come from the last drift/update CHECK
    //            (Instance.namingDrift/UpdateFingerprints) so they never appear
    //            without a check having run. `.expected` = the intended scheme's
    //            current guide pattern (what Sync would re-apply), for the diff row.
    //  matched — not synced by clonarr; current pattern matches a known scheme (info)
    //  custom  — not synced by clonarr; matches no known scheme
    namingFieldStatus(appType, row) {
      const inst = this.instances.find(i => i.id === this.mediaInstanceId[appType]);
      const drifted = !!(inst && inst.namingDriftFingerprints && inst.namingDriftFingerprints[row.field]);
      const updatable = !!(inst && inst.namingUpdateFingerprints && inst.namingUpdateFingerprints[row.field]);
      const binding = this.namingBindingForField(appType, row.field);
      const appliedScheme = this.namingApplied[appType]?.[row.field]?.scheme || '';
      const scheme = binding ? binding.scheme : appliedScheme; // intended scheme
      if (binding || appliedScheme) {
        return {
          kind: 'managed',
          auto: !!binding,
          label: scheme ? namingSchemeLabel(scheme) : '',
          drifted,
          updatable,
          expected: scheme ? this.namingExpectedPattern(appType, row.field, scheme) : '',
        };
      }
      const matched = this.namingMatchedScheme(appType, row.arr, row.field);
      if (matched) return { kind: 'matched', auto: false, label: matched.label, drifted: false, updatable: false, expected: '' };
      return { kind: 'custom', auto: false, label: 'Custom', drifted: false, updatable: false, expected: '' };
    },

    // Rows for the "Currently on instance" card: {label, arr (Arr field on
    // namingInstanceData), field (canonical key for the auto-sync binding)}.
    namingCurrentRows(appType) {
      if (appType === 'radarr') return [
        { label: 'Standard Movie Format', arr: 'standardMovieFormat', field: 'movieFile' },
        { label: 'Movie Folder Format', arr: 'movieFolderFormat', field: 'movieFolder' },
      ];
      return [
        { label: 'Standard Episode Format', arr: 'standardEpisodeFormat', field: 'standardEpisode' },
        { label: 'Anime Episode Format', arr: 'animeEpisodeFormat', field: 'animeEpisode' },
        { label: 'Daily Episode Format', arr: 'dailyEpisodeFormat', field: 'dailyEpisode' },
        { label: 'Series Folder Format', arr: 'seriesFolderFormat', field: 'seriesFolder' },
        { label: 'Season Folder Format', arr: 'seasonFolderFormat', field: 'seasonFolder' },
      ];
    },

    async loadNamingAutoSync(appType) {
      const instId = this.mediaInstanceId[appType];
      if (!instId) {
        this.namingAutoSync = { ...this.namingAutoSync, [appType]: {} };
        this.namingApplied = { ...this.namingApplied, [appType]: {} };
        return;
      }
      try {
        const r = await fetch(`/api/instances/${instId}/naming/auto-sync`);
        if (r.ok) {
          const data = await r.json();
          this.namingAutoSync = { ...this.namingAutoSync, [appType]: data || {} };
        }
      } catch (e) { /* ignore — toggle just shows off */ }
      this.loadNamingApplied(appType);
      this.loadNamingChanges(appType);
    },

    async loadNamingApplied(appType) {
      const instId = this.mediaInstanceId[appType];
      if (!instId) { this.namingApplied = { ...this.namingApplied, [appType]: {} }; return; }
      try {
        const r = await fetch(`/api/instances/${instId}/naming/applied`);
        if (r.ok) {
          const data = await r.json();
          this.namingApplied = { ...this.namingApplied, [appType]: data || {} };
        }
      } catch (e) { /* ignore */ }
    },

    // Build the {field: {scheme}} payload the PUT expects from the current
    // in-memory binding map (server owns fingerprint/appliedAt/error).
    buildNamingAutoSyncPayload(map) {
      const out = {};
      for (const [field, b] of Object.entries(map || {})) {
        if (b && b.scheme) out[field] = { scheme: b.scheme };
      }
      return out;
    },

    async saveNamingAutoSyncMap(appType, nextMap) {
      const instId = this.mediaInstanceId[appType];
      if (!instId) return;
      this.namingAutoSyncBusy = true;
      try {
        const r = await fetch(`/api/instances/${instId}/naming/auto-sync`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(this.buildNamingAutoSyncPayload(nextMap)),
        });
        const data = await r.json().catch(() => ({}));
        if (r.ok) {
          // The backend applies newly-enabled fields immediately (enabling
          // auto-sync brings the field to the scheme now, then follows updates).
          if (data.error) this.showToast(`Auto-sync saved, but applying now failed: ${data.error}`, 'error');
          else if (data.applied > 0) {
            this.showToast('Auto-sync on — scheme applied now and will follow TRaSH updates', 'success');
            this.loadInstanceNaming(appType); // refresh the "currently on instance" card
          }
        } else {
          this.showToast(`Auto-sync save failed: ${data.error || r.statusText}`, 'error');
        }
      } catch (e) {
        this.showToast(`Auto-sync save failed: ${e.message}`, 'error');
      } finally {
        await this.loadNamingAutoSync(appType); // always resync UI to server truth
        this.namingAutoSyncBusy = false;
      }
    },

    // Is this exact scheme the one the field is auto-syncing to?
    isNamingAutoSyncing(appType, sectionKey, schemeKey) {
      return this.namingBindingFor(appType, sectionKey)?.scheme === schemeKey;
    },

    // Per-row toggle: clicking the active scheme turns auto-sync off; clicking
    // any other scheme binds the field to it (replacing any prior scheme — one
    // scheme per field). The backend applies it immediately.
    async toggleNamingAutoSyncScheme(appType, sectionKey, schemeKey) {
      const field = this.namingFieldKey(sectionKey);
      if (!field || !schemeKey) return;
      const cur = { ...(this.namingAutoSync[appType] || {}) };
      if (cur[field]?.scheme === schemeKey) delete cur[field];
      else cur[field] = { scheme: schemeKey };
      await this.saveNamingAutoSyncMap(appType, cur);
    },

    // Turn auto-sync off for a field (from the card summary line).
    async disableNamingAutoSync(appType, sectionKey) {
      const field = this.namingFieldKey(sectionKey);
      if (!field) return;
      const cur = { ...(this.namingAutoSync[appType] || {}) };
      delete cur[field];
      await this.saveNamingAutoSyncMap(appType, cur);
    },

    // Readable label of the scheme a field is bound to (for the summary line).
    namingActiveSchemeLabel(appType, sectionKey) {
      const b = this.namingBindingFor(appType, sectionKey);
      return b ? namingSchemeLabel(b.scheme) : '';
    },

    // ===== Per-field change history + restore =====

    async loadNamingChanges(appType) {
      const instId = this.mediaInstanceId[appType];
      if (!instId) { this.namingChanges = { ...this.namingChanges, [appType]: {} }; return; }
      try {
        const r = await fetch(`/api/instances/${instId}/naming/changes`);
        if (r.ok) {
          const data = await r.json();
          this.namingChanges = { ...this.namingChanges, [appType]: data || {} };
        }
      } catch (e) { /* ignore */ }
    },

    // A field's change events, newest first (the stored array is newest-last).
    namingFieldChanges(appType, field) {
      const ev = (this.namingChanges[appType] || {})[field] || [];
      return ev.slice().reverse();
    },

    // Inline history disclosure per field on the card.
    toggleNamingFieldHistory(appType, field) {
      const key = appType + ':' + field;
      this.namingFieldHistoryOpen = { ...this.namingFieldHistoryOpen, [key]: !this.namingFieldHistoryOpen[key] };
    },
    isNamingFieldHistoryOpen(appType, field) {
      return !!this.namingFieldHistoryOpen[appType + ':' + field];
    },

    // Confirm + restore one field to a prior pattern (from its history). High-stakes
    // (renames files), so it shows exactly what it will set before applying.
    confirmRestoreNamingField(appType, field, fieldLabel, toPattern) {
      const instName = this.getInstanceName(appType, this.mediaInstanceId[appType]);
      const escapeHtml = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
      const message =
        '<div style="margin-bottom:6px">Restore <strong>' + escapeHtml(fieldLabel) + '</strong> on <strong>' + escapeHtml(instName) + '</strong> to:</div>' +
        '<div style="font-family:monospace;font-size:12px;background:var(--bg-page);border:1px solid var(--border-subtle);border-radius:3px;padding:6px 8px;white-space:nowrap;overflow-x:auto;color:var(--text-body)">' + escapeHtml(toPattern) + '</div>' +
        '<div style="font-size:11px;color:var(--text-secondary);margin-top:12px;font-style:italic">Existing files in ' + escapeHtml(instName) + ' are renamed on next sync if Rename is enabled there. This does not turn off auto-sync.</div>';
      this.confirmModal = {
        show: true, title: 'Restore naming', message, html: true, wide: true,
        confirmLabel: 'Restore', cancelLabel: 'Cancel',
        onConfirm: () => this.restoreNamingField(appType, field, toPattern),
        onCancel: () => {},
      };
    },

    async restoreNamingField(appType, field, toPattern) {
      const instId = this.mediaInstanceId[appType];
      if (!instId) return;
      try {
        const r = await fetch(`/api/instances/${instId}/naming/restore-field`, {
          method: 'POST', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ field, to: toPattern }),
        });
        if (r.ok) {
          this.showToast('Naming restored', 'success');
          await this.loadInstances();
          await this.loadInstanceNaming(appType);
          await this.loadNamingApplied(appType);
          await this.loadNamingChanges(appType);
        } else {
          const err = await r.json().catch(() => ({}));
          this.showToast(`Restore failed: ${err.error || r.statusText}`, 'error');
        }
      } catch (e) { this.showToast(`Restore failed: ${e.message}`, 'error'); }
    },

    // Naming-only check: runs ONLY the naming drift+update pass (not CF/profile
    // drift), then refreshes instances (persisted drift/update flags) + current
    // naming + applied records so the markers reflect this check.
    async checkNamingDrift(appType) {
      this.namingDriftChecking = true;
      try {
        const r = await fetch('/api/naming/drift-check', { method: 'POST' });
        if (r.ok) {
          await this.loadInstances();
          await this.loadInstanceNaming(appType);
          await this.loadNamingApplied(appType);
          // Summarise for the selected instance (the page the user is on).
          const inst = this.instances.find(i => i.id === this.mediaInstanceId[appType]);
          const nDrift = Object.keys(inst?.namingDriftFingerprints || {}).length;
          const nUpd = Object.keys(inst?.namingUpdateFingerprints || {}).length;
          if (nDrift === 0 && nUpd === 0) {
            this.showToast('Naming is in sync', 'success');
          } else {
            const parts = [];
            if (nDrift) parts.push(`${nDrift} naming format${nDrift === 1 ? '' : 's'} drifted`);
            if (nUpd) parts.push(`${nUpd} update${nUpd === 1 ? '' : 's'} available`);
            this.showToast('Naming check: ' + parts.join(', '), nDrift ? 'error' : 'info');
          }
        } else {
          const err = await r.json().catch(() => ({}));
          this.showToast(`Naming check failed: ${err.error || r.statusText}`, 'error');
        }
      } catch (e) {
        this.showToast(`Naming check failed: ${e.message}`, 'error');
      } finally {
        this.namingDriftChecking = false;
      }
    },

    // Apply fields' intended schemes now + clear their drift/update flags.
    // fields = array of canonical field keys (per-field Sync); omitted = Update all
    // (every auto-sync field with a drift/update flag).
    async syncNaming(appType, fields) {
      const instId = this.mediaInstanceId[appType];
      if (!instId) return;
      this.namingSyncing = true;
      try {
        const r = await fetch(`/api/instances/${instId}/naming/sync`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(fields ? { fields } : {}),
        });
        const data = await r.json().catch(() => ({}));
        if (r.ok) {
          if (data.applied > 0) this.showToast(`Synced ${data.applied} naming format${data.applied === 1 ? '' : 's'}`, 'success');
          else this.showToast('Nothing to sync', 'info');
          await this.loadInstances();
          await this.loadInstanceNaming(appType);
          await this.loadNamingChanges(appType);
        } else {
          this.showToast(`Sync failed: ${data.error || r.statusText}`, 'error');
        }
      } catch (e) {
        this.showToast(`Sync failed: ${e.message}`, 'error');
      } finally {
        this.namingSyncing = false;
      }
    },

    // True if any auto-sync field on the selected instance currently has a drift
    // or update flag — i.e. "Update all" would do something. Drives that button.
    namingAnyPending(appType) {
      const inst = this.instances.find(i => i.id === this.mediaInstanceId[appType]);
      if (!inst) return false;
      const auto = this.namingAutoSync[appType] || {};
      for (const field of Object.keys(auto)) {
        if ((inst.namingDriftFingerprints && inst.namingDriftFingerprints[field]) ||
            (inst.namingUpdateFingerprints && inst.namingUpdateFingerprints[field])) return true;
      }
      return false;
    },

    // Locale-aware timestamp (server stores UTC/ISO; format client-side).
    namingSnapshotTime(ts) {
      if (!ts) return '';
      try { return new Date(ts).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' }); }
      catch (e) { return ts; }
    },


    // Human label for a change event's origin (the Via field).
    namingChangeVia(via) {
      if (via === 'manual') return 'Manual sync';
      if (via === 'auto-sync') return 'Auto-sync';
      if (via === 'rollback') return 'Restore';
      return via || 'Change';
    },

    // Splits an old/new pattern pair into the shared prefix + suffix and the
    // differing middle of each, so the history "Diff" line can dim what's
    // unchanged and highlight only what changed (removed = red, added = green).
    // Character-level common prefix/suffix — fine for naming patterns, where a
    // change is usually a localized token edit.
    namingDiffParts(from, to) {
      from = from || ''; to = to || '';
      const min = Math.min(from.length, to.length);
      let p = 0;
      while (p < min && from[p] === to[p]) p++;
      let s = 0;
      while (s < (min - p) && from[from.length - 1 - s] === to[to.length - 1 - s]) s++;
      return {
        prefix: from.slice(0, p),
        oldMid: from.slice(p, from.length - s),
        newMid: to.slice(p, to.length - s),
        suffix: s ? from.slice(from.length - s) : '',
      };
    },
  },
};
