export default {
  state: {
    namingData: {},
    namingSelectedInstance: {},
    namingInstanceData: {},
    namingApplyResult: {},
    namingMediaServer: {},
    namingPlexSingleEntry: {},
    namingFAQExpanded: false,
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

    getNamingSections(appType, mediaServer, plexSingleEntry) {
      const n = this.getNaming(appType);
      if (!n) return [];
      const ms = mediaServer || 'standard';

      // Descriptions sourced verbatim from TRaSH-Guides where available.
      // Schemes without TRaSH-authored descriptions have no desc field.
      // The "recommended" flag is set per-app below (perAppRecommended) — TRaSH
      // recommends TMDb for Radarr (movies) and TVDb for Sonarr (TV) because
      // those guarantee a metadata match. IMDb is only matched indirectly via
      // the TMDb/TVDb entry's IMDb-ID association. See
      // includes/{radarr,sonarr}/{tmdb,tvdb}-imdb-info.md in the TRaSH repo.
      const schemeDesc = {
        // 'standard' (file) — TRaSH lists this as the first tab in "Standard Movie/Series Format"
        // section but does NOT explicitly call this specific variant "recommended". The whole
        // section title implies recommendation, but per-variant text is silent. No badge.
        'standard': { label: 'Standard' },
        // 'default' (folder) — TRaSH MD says verbatim "The minimum needed and recommended format"
        // for the "Standard Folder" tab (which maps to this key). Explicit endorsement → badge.
        'default': { label: 'Default', recommended: true },
        'original': { label: 'Original Title', desc: 'Another option is to use {Original Title} instead of the recommended naming scheme above. {Original Title} uses the title of the release, which includes all the information from the release itself. The benefit of this naming scheme is that it prevents download loops that can happen during import when there\'s a mismatch between the release title and the file contents (for example, if the release title says DTS-ES but the contents are actually DTS). The downside is that you have less control over how the files are named.' },
        'p2p-scene': { label: 'P2P / Scene', desc: 'Use P2P/Scene naming if you don\'t like spaces and brackets in the filename. It\'s the closest to the P2P/scene naming scheme, except it uses the exact audio and HDR formats from the media file, where the original release or filename might be unclear.' },
        'plex-imdb': { label: 'Plex (IMDb)' },
        'plex-tmdb': { label: 'Plex (TMDb)' },
        'plex-tvdb': { label: 'Plex (TVDb)' },
        'plex-anime-imdb': { label: 'Plex Anime (IMDb)' },
        'plex-anime-tmdb': { label: 'Plex Anime (TMDb)' },
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

      // Per-app "Recommended" badge. TRaSH's tmdb-imdb-info.md (Radarr) and
      // tvdb-imdb-info.md (Sonarr) state that TMDb/TVDb are usually better
      // because they guarantee a metadata match, whereas IMDb only matches
      // indirectly via the TMDb/TVDb entry's IMDb-ID association.
      // Anime variants are NOT in this set: TRaSH endorses TMDb/TVDb over IMDb
      // generally, but does not explicitly call out the anime variants as
      // "recommended" — the prose targets the regular media-server schemes.
      // Sonarr's naming JSON also has no anime variants in series naming.
      const perAppRecommended = {
        radarr: new Set(['plex-tmdb', 'emby-tmdb', 'jellyfin-tmdb']),
        sonarr: new Set(['plex-tvdb', 'emby-tvdb', 'jellyfin-tvdb']),
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

      const applyEditionToggle = (pattern, example) => {
        if (!plexSingleEntry || ms !== 'plex') return { pattern, example };
        return {
          pattern: pattern.replace(/\{edition-\{Edition Tags\}\}/g, '{Edition Tags}'),
          example: example ? example.replace(/\{edition-([^}]+)\}/g, '$1') : example,
        };
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

      // Enforce consistent ordering
      const keyOrder = ['standard', 'default', 'plex-imdb', 'plex-tmdb', 'plex-anime-imdb', 'plex-anime-tmdb', 'plex-tvdb',
        'emby-imdb', 'emby-tmdb', 'emby-anime-imdb', 'emby-anime-tmdb', 'emby-tvdb',
        'jellyfin-imdb', 'jellyfin-tmdb', 'jellyfin-anime-imdb', 'jellyfin-anime-tmdb', 'jellyfin-tvdb',
        'original', 'p2p-scene'];

      const makeSchemes = (map, sectionKey, examplesMap) => {
        const entries = Object.entries(map || {}).filter(([key]) => filterFn(key));
        entries.sort((a, b) => {
          const ai = keyOrder.indexOf(a[0]), bi = keyOrder.indexOf(b[0]);
          return (ai === -1 ? 999 : ai) - (bi === -1 ? 999 : bi);
        });
        return entries.map(([key, pattern]) => {
          const meta = schemeDesc[key] || { label: key.replace(/-/g, ' ').replace(/\b\w/g, c => c.toUpperCase()) };
          const ed = applyEditionToggle(pattern, examplesMap?.[key] || '');
          return {
            key,
            label: meta.label || key,
            recommended: meta.recommended || perAppRecommended[appType]?.has(key) || false,
            description: meta.desc || '',
            pattern: ed.pattern,
            example: ed.example,
          };
        });
      };

      const sections = [];

      // Section descriptions sourced verbatim from TRaSH-Guides where available.
      // Sonarr: docs/Sonarr/Sonarr-recommended-naming-scheme.md
      // Radarr: docs/Radarr/Radarr-recommended-naming-scheme.md (+ includes/radarr/radarr-folder-name-after-year-info.md)
      const radarrFileDesc = {
        standard: '',
        plex: 'This naming scheme is designed to work with the New Plex Agent.',
        emby: 'Source: Emby Wiki/Docs',
        jellyfin: 'Source: Jellyfin Wiki/Docs',
      };
      const radarrFolderDesc = {
        standard: 'The minimum needed and recommended format',
        plex: 'Keep in mind adding anything additional after the release year could give issues during a fresh import into Radarr, but it can help for movies that have the same release name and year',
        emby: 'Keep in mind adding anything additional after the release year could give issues during a fresh import into Radarr, but it can help for movies that have the same release name and year',
        jellyfin: 'Keep in mind adding anything additional after the release year could give issues during a fresh import into Radarr, but it can help for movies that have the same release name and year',
      };
      const sonarrSeriesDesc = {
        standard: '',
        plex: 'This naming scheme is made to be used with the New Plex TV Series Scanner.',
        emby: 'Source: Emby Wiki/Docs',
        jellyfin: 'Source: Jellyfin Wiki/Docs — Jellyfin doesn\'t support IMDb IDs for shows.',
      };

      if (appType === 'radarr') {
        // File format first, folder second
        const fileSchemes = makeSchemes(n.file, 'file', radarrExamples.file);
        if (fileSchemes.length > 0) {
          sections.push({
            key: 'file',
            label: 'Standard Movie Format',
            exampleLabel: 'Movie',
            description: radarrFileDesc[ms] || '',
            schemes: fileSchemes,
            showEditionToggle: ms === 'plex',
          });
        }
        const folderSchemes = makeSchemes(n.folder, 'folder', radarrExamples.folder);
        if (folderSchemes.length > 0) {
          sections.push({
            key: 'folder',
            label: 'Movie Folder Format',
            exampleLabel: 'Folder',
            description: radarrFolderDesc[ms] || '',
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
          description: sonarrSeriesDesc[ms] || '',
          schemes: seriesSchemes,
        });
        if (n.season && ms === 'standard') {
          sections.push({
            key: 'season',
            label: 'Season Folder Format',
            exampleLabel: 'Season',
            description: 'For this, there\'s only one real option to use in our opinion.',
            schemes: makeSchemes(n.season, 'season', { 'default': 'Season 01' }),
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

    async applyNamingScheme(appType, sectionKey, scheme) {
      const instId = this.namingSelectedInstance[appType];
      if (!instId) return;
      const instName = this.getInstanceName(appType, instId);
      const body = {};
      if (sectionKey === 'folder' || sectionKey === 'series' || sectionKey === 'season') {
        body[sectionKey] = scheme.pattern;
        if (sectionKey === 'series') body.series = scheme.pattern;
        if (sectionKey === 'season') body.season = scheme.pattern;
        if (sectionKey === 'folder') body.folder = scheme.pattern;
      } else {
        // file/episodes section
        body.file = scheme.pattern;
      }
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
