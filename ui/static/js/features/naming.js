export default {
  state: {
    namingData: {},
    namingSelectedInstance: {},
    namingInstanceData: {},
    namingApplyResult: {},
    namingMediaServer: {},
    namingFAQExpanded: false,
    namingIDInfoExpanded: false,
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

    getNamingSections(appType, mediaServer) {
      const n = this.getNaming(appType);
      if (!n) return [];
      const ms = mediaServer || 'standard';

      // Labels only — descriptions are sourced from TRaSH JSON if present
      // (currently absent across all keys; TRaSH plans to add descriptions per
      // scheme in a future release). When that lands, makeSchemes below reads
      // them straight from the JSON and the existing UI render-paths pick them
      // up automatically (HTML uses x-show="scheme.description" / "section.description").
      // No "recommended" flag — Clonarr does not editorialize on top of TRaSH's
      // own JSON; users decide which variant suits them.
      const schemeDesc = {
        'standard': { label: 'Standard' },
        'default': { label: 'Default' },
        'original': { label: 'Original Title' },
        'p2p-scene': { label: 'P2P / Scene' },
        'plex-imdb': { label: 'Plex (IMDb)' },
        'plex-tmdb': { label: 'Plex (TMDb)' },
        'plex-tvdb': { label: 'Plex (TVDb)' },
        'plex-anime-imdb': { label: 'Plex Anime (IMDb)' },
        'plex-anime-tmdb': { label: 'Plex Anime (TMDb)' },
        'plex-edition-alt-imdb': { label: 'Plex (IMDb, single entry)' },
        'plex-edition-alt-tmdb': { label: 'Plex (TMDb, single entry)' },
        'emby-imdb': { label: 'Emby (IMDb)' },
        'emby-tmdb': { label: 'Emby (TMDb)' },
        'emby-tvdb': { label: 'Emby (TVDb)' },
        'emby-anime-imdb': { label: 'Emby Anime (IMDb)' },
        'emby-anime-tmdb': { label: 'Emby Anime (TMDb)' },
        'jellyfin-imdb': { label: 'Jellyfin (IMDb)' },
        'jellyfin-tmdb': { label: 'Jellyfin (TMDb)' },
        'jellyfin-tvdb': { label: 'Jellyfin (TVDb)' },
        'jellyfin-anime-imdb': { label: 'Jellyfin Anime (IMDb)' },
        'jellyfin-anime-tmdb': { label: 'Jellyfin Anime (TMDb)' },
      };

      // Media server key filters
      const msFilters = {
        standard: k => !k.includes('-'),  // standard, default, original, p2p-scene have no media server prefix
        plex: k => k.startsWith('plex-'),
        emby: k => k.startsWith('emby-'),
        jellyfin: k => k.startsWith('jellyfin-'),
      };
      const standardKeys = new Set(['standard', 'default', 'original', 'p2p-scene']);
      const filterFn = ms === 'standard'
        ? k => standardKeys.has(k)
        : (msFilters[ms] || (() => true));

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

      // Enforce consistent ordering
      const keyOrder = ['standard', 'default', 'plex-imdb', 'plex-tmdb', 'plex-anime-imdb', 'plex-anime-tmdb',
        'plex-edition-alt-imdb', 'plex-edition-alt-tmdb', 'plex-tvdb',
        'emby-imdb', 'emby-tmdb', 'emby-anime-imdb', 'emby-anime-tmdb', 'emby-tvdb',
        'jellyfin-imdb', 'jellyfin-tmdb', 'jellyfin-anime-imdb', 'jellyfin-anime-tmdb', 'jellyfin-tvdb',
        'original', 'p2p-scene'];

      // Pattern values can be plain strings (current TRaSH JSON shape) OR
      // objects with `pattern`/`description` once TRaSH adds per-scheme
      // descriptions. Code handles both shapes so the future migration is
      // a no-op for us.
      const makeSchemes = (map, sectionKey, examplesMap) => {
        const entries = Object.entries(map || {}).filter(([key]) => filterFn(key));
        entries.sort((a, b) => {
          const ai = keyOrder.indexOf(a[0]), bi = keyOrder.indexOf(b[0]);
          return (ai === -1 ? 999 : ai) - (bi === -1 ? 999 : bi);
        });
        return entries.map(([key, value]) => {
          const meta = schemeDesc[key] || { label: key.replace(/-/g, ' ').replace(/\b\w/g, c => c.toUpperCase()) };
          // Future-compat: TRaSH may ship descriptions per scheme. Accept both
          // string-pattern and {pattern, description} object shapes.
          const pattern = typeof value === 'string' ? value : (value?.pattern || '');
          const trashDesc = typeof value === 'object' ? (value?.description || '') : '';
          return {
            key,
            label: meta.label || key,
            description: trashDesc,
            pattern,
            example: examplesMap?.[key] || '',
          };
        });
      };

      const sections = [];

      // No editorial section descriptions — Clonarr does not commentary on top
      // of TRaSH JSON. When TRaSH ships descriptions per scheme/section in JSON,
      // makeSchemes above reads them through automatically.

      if (appType === 'radarr') {
        // File format first, folder second
        const fileSchemes = makeSchemes(n.file, 'file', radarrExamples.file);
        if (fileSchemes.length > 0) {
          sections.push({
            key: 'file',
            label: 'Standard Movie Format',
            exampleLabel: 'Movie',
            description: '',
            schemes: fileSchemes,
          });
        }
        const folderSchemes = makeSchemes(n.folder, 'folder', radarrExamples.folder);
        if (folderSchemes.length > 0) {
          sections.push({
            key: 'folder',
            label: 'Movie Folder Format',
            exampleLabel: 'Folder',
            description: '',
            schemes: folderSchemes,
          });
        }
      } else {
        // Episodes first (most important)
        for (const [epType, schemes] of Object.entries(n.episodes || {})) {
          const epLabel = epType.charAt(0).toUpperCase() + epType.slice(1);
          const epSchemes = makeSchemes(schemes, epType, sonarrExamples.episodes?.[epType]);
          if (epSchemes.length > 0) {
            sections.push({
              key: 'episodes-' + epType,
              label: 'Episode Format — ' + epLabel,
              exampleLabel: 'Episode',
              description: '',
              schemes: epSchemes,
            });
          }
        }
        const seriesSchemes = makeSchemes(n.series, 'series', sonarrExamples.series);
        if (seriesSchemes.length > 0) sections.push({
          key: 'series',
          label: 'Series Folder Format',
          exampleLabel: 'Series',
          description: '',
          schemes: seriesSchemes,
        });
        // Season folder shows on every media-server tab — pattern doesn't vary
        // by server, and hiding it on Plex/Emby/Jellyfin tabs made users think
        // it had disappeared. Bypass the ms-filter (TRaSH ships only `default`
        // for season; if per-server variants are ever added, makeSchemes can
        // be reintroduced here).
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
      const instId = this.namingSelectedInstance[appType];
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

    // Open confirmModal with current → new pattern preview before applying.
    // Per feedback_dryrun_preview: destructive-ish UI ops should show a
    // concrete preview, not just "Are you sure?".
    confirmApplyNamingScheme(appType, sectionKey, scheme) {
      const instId = this.namingSelectedInstance[appType];
      if (!instId) return;
      const instName = this.getInstanceName(appType, instId);
      const fieldName = this.namingCurrentFieldFor(appType, sectionKey);
      const currentPattern = fieldName ? (this.namingInstanceData[appType]?.[fieldName] || '(not set)') : '(unknown)';
      const escapeHtml = (s) => String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;');
      const sectionLabel = sectionKey.startsWith('episodes-')
        ? 'Episode (' + sectionKey.slice(9) + ')'
        : sectionKey.charAt(0).toUpperCase() + sectionKey.slice(1);
      const message =
        '<div style="margin-bottom:10px">Apply <strong>' + escapeHtml(scheme.label) + '</strong> ' + escapeHtml(sectionLabel) + ' naming to <strong>' + escapeHtml(instName) + '</strong>?</div>' +
        '<div style="font-size:11px;color:var(--text-secondary);margin-top:14px;margin-bottom:4px;text-transform:uppercase;letter-spacing:0.5px">Currently</div>' +
        '<div style="font-family:monospace;font-size:12px;background:var(--bg-page);border:1px solid var(--bg-muted);border-radius:3px;padding:6px 8px;white-space:nowrap;overflow-x:auto;color:var(--text-muted)">' + escapeHtml(currentPattern) + '</div>' +
        '<div style="font-size:11px;color:var(--text-secondary);margin-top:10px;margin-bottom:4px;text-transform:uppercase;letter-spacing:0.5px">Change to</div>' +
        '<div style="font-family:monospace;font-size:12px;background:var(--bg-page);border:1px solid var(--accent-orange);border-radius:3px;padding:6px 8px;white-space:nowrap;overflow-x:auto;color:var(--text-body)">' + escapeHtml(scheme.pattern) + '</div>' +
        '<div style="font-size:11px;color:var(--text-secondary);margin-top:10px;font-style:italic">Existing files in ' + escapeHtml(instName) + ' will be renamed on next sync if "Rename" is enabled there.</div>';
      this.confirmModal = {
        show: true,
        title: 'Apply naming scheme',
        message,
        html: true,
        confirmLabel: 'Apply',
        cancelLabel: 'Cancel',
        onConfirm: () => this.applyNamingScheme(appType, sectionKey, scheme),
        onCancel: () => {},
      };
    },

    async applyNamingScheme(appType, sectionKey, scheme) {
      const instId = this.namingSelectedInstance[appType];
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
      const body = { [field]: scheme.pattern };
      try {
        const r = await fetch(`/api/instances/${instId}/naming`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body)
        });
        if (r.ok) {
          this.namingApplyResult = { ...this.namingApplyResult, [appType]: `Applied "${scheme.label}" ${sectionKey} naming to ${instName}` };
          this.loadInstanceNaming(appType);
          setTimeout(() => { this.namingApplyResult = { ...this.namingApplyResult, [appType]: '' }; }, 5000);
        } else {
          const err = await r.json().catch(() => ({}));
          this.namingApplyResult = { ...this.namingApplyResult, [appType]: `Failed: ${err.error || r.statusText}` };
        }
      } catch (e) {
        this.namingApplyResult = { ...this.namingApplyResult, [appType]: `Error: ${e.message}` };
      }
    },
  },
};
