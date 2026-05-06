import { copyToClipboard } from '../utils/clipboard.js';

export default {
  state: {},
  methods: {
    // --- Scoring Sandbox ---

    async loadSandbox(appType) {
      const sb = this.sandbox[appType];
      // Default to first instance of this type
      if (!sb.instanceId) {
        const insts = this.instancesOfType(appType);
        if (insts.length > 0) sb.instanceId = insts[0].id;
      }
      // Load Prowlarr indexers if enabled and not loaded
      if (this.config.prowlarr?.enabled && sb.indexers.length === 0) {
        try {
          const r = await fetch('/api/scoring/prowlarr/indexers');
          if (r.ok) sb.indexers = await r.json();
        } catch (e) { /* ignore */ }
      }
      // Load instance profiles for the "Score against" dropdown.
      // Sort alphabetically so the dropdown is browsable — Arr returns
      // them in id order which feels random to the user.
      if (sb.instanceId && sb.instanceProfiles.length === 0) {
        try {
          const r = await fetch(`/api/instances/${sb.instanceId}/profiles`);
          if (r.ok) {
            const profs = await r.json();
            sb.instanceProfiles = (profs || []).slice().sort((a, b) =>
              (a.name || '').localeCompare(b.name || '', undefined, { sensitivity: 'base' }));
          }
        } catch (e) { /* ignore */ }
      }
    },

    async sandboxInstanceChanged(appType) {
      const sb = this.sandbox[appType];
      sb.instanceProfiles = [];
      if (sb.instanceId) {
        try {
          const r = await fetch(`/api/instances/${sb.instanceId}/profiles`);
          if (r.ok) {
            const profs = await r.json();
            sb.instanceProfiles = (profs || []).slice().sort((a, b) =>
              (a.name || '').localeCompare(b.name || '', undefined, { sensitivity: 'base' }));
          }
        } catch (e) { /* ignore */ }
      }
      // Re-score if using instance profile
      if (sb.profileKey?.startsWith('inst:')) {
        sb.profileKey = '';
      }
      this.rescoreSandbox(appType);
    },

    sandboxTrashProfiles(appType) {
      return (this.trashProfiles[appType] || []).map(p => ({ trashId: p.trashId, name: p.name }));
    },

    sandboxImportedProfiles(appType) {
      return (this.importedProfiles[appType] || []).map(p => ({ id: p.id, name: p.name }));
    },

    // Stamp stable _sid on sandbox results for :key tracking during drag reorder.
    _sbEnsureIds(results) {
      for (const r of results) {
        if (!r._sid) r._sid = ++this._sbIdCounter;
      }
      return results;
    },

    // Quality rank map from the active profile (name → numeric rank,
    // higher = better). Group members share the group's rank so they
    // tie when the sandbox sorts by quality, and the score key inside
    // the sort breaks the tie within a group. Releases with a quality
    // not in the profile rank as -1 (sort below every allowed quality
    // — Radarr wouldn't pick them anyway). Backend builds the map; we
    // just read from the cached profile-scores response.
    _sandboxQualityRank(appType, profileKey) {
      // profileKey override is for the compare row, which scores
      // against sb.compareKey (a different profile). Defaults to the
      // active sb.profileKey for sort + primary status.
      const sb = this.sandbox[appType];
      const key = profileKey ?? sb.profileKey;
      const cacheKey = appType + ':' + key;
      return this._profileScoreCache[cacheKey]?.qualityRanks || {};
    },

    // Tri-state quality-allowed check for visual styling.
    // Returns true if the release's parsed quality is in the active
    // profile's allowed list; false if a profile IS selected but the
    // quality is not allowed (so we can red-line / strike the cell);
    // null when there's no profile loaded or no quality parsed (so the
    // UI falls back to neutral styling instead of falsely flagging).
    sandboxQualityAllowed(res, appType, profileKey) {
      const ranks = this._sandboxQualityRank(appType, profileKey);
      const haveProfile = Object.keys(ranks).length > 0;
      const quality = res?.parsed?.quality || '';
      if (!haveProfile || !quality) return null;
      return quality in ranks;
    },

    // Pass / fail status for a sandbox result, simulating what
    // Sonarr/Radarr would do with the same release in interactive
    // search. TRaSH-confirmed checks (in this order):
    //   1. Quality must be in the profile's allowed list
    //      ("Only checked qualities are wanted").
    //   2. CF score (matched + unmatched penalties) must reach Min
    //      Format Score.
    // The first failing check decides the reason; passing both yields
    // PASS. scoring is the per-release scoring object (sb.results[i]
    // .scoring or .scoringB for the compare profile); profileKey lets
    // the compare row use sb.compareKey instead of the primary key.
    sandboxResultStatus(res, scoring, appType, profileKey) {
      if (!scoring) return { pass: false, reason: 'No score yet', code: 'unscored' };
      const ranks = this._sandboxQualityRank(appType, profileKey);
      const haveProfile = Object.keys(ranks).length > 0;
      const quality = res?.parsed?.quality || '';
      // Quality-allowed gate. Without a loaded profile we skip this so
      // the status doesn't lie when the user hasn't picked one yet.
      if (haveProfile && quality && !(quality in ranks)) {
        return { pass: false, reason: `Quality "${quality}" not allowed in profile`, code: 'quality' };
      }
      const total = scoring.total ?? 0;
      const min = scoring.minScore || 0;
      if (total < min) {
        return { pass: false, reason: `Score ${total} below Min ${min}`, code: 'score' };
      }
      return { pass: true, reason: '', code: 'pass' };
    },

    // Sorted results. sortCol 'manual' (or empty) preserves the underlying sb.results
    // order — set by drag-reorder so manual ordering survives until the user clicks
    // a column header to re-sort.
    //
    // Score and Quality sorts both rank by the active profile's quality
    // first — TRaSH/Radarr's "current logic" rule states a higher
    // quality always trumps score, so a 1080p release outranks a 720p
    // one regardless of score. Group members (e.g. Bluray-1080p +
    // WEBDL-1080p + WEBRip-1080p in a "1080p" group) share rank, so
    // within the group score breaks the tie — that matches Radarr's
    // own behaviour where qualities inside a group are interchangeable.
    sortedSandboxResults(appType) {
      const sb = this.sandbox[appType];
      const results = [...(sb.results || [])];
      const col = sb.sortCol;
      if (!col || col === 'manual') return results;
      const dir = sb.sortDir === 'asc' ? 1 : -1;
      const qRank = (col === 'score' || col === 'quality') ? this._sandboxQualityRank(appType) : null;
      const rankOf = (r) => {
        const q = r.parsed?.quality || '';
        return (qRank && q in qRank) ? qRank[q] : -1;
      };
      // Pass/fail outer key for score + quality sorts. PASS rows always
      // group above FAIL rows regardless of asc/desc on the secondary
      // keys — intermixing passes and fails (e.g. score-too-low rows
      // landing between higher-scoring passes of the same quality)
      // makes the table read as random. FAIL rows still sort by the
      // same quality+score logic within their own block.
      const passOf = (r) => this.sandboxResultStatus(r, r.scoring, appType).pass ? 1 : 0;
      results.sort((a, b) => {
        switch (col) {
          case 'score': {
            // Pass/fail outer, then quality, then score within quality.
            const dp = passOf(b) - passOf(a);
            if (dp !== 0) return dp;
            const dq = rankOf(a) - rankOf(b);
            if (dq !== 0) return dir * dq;
            return dir * ((a.scoring?.total ?? -99999) - (b.scoring?.total ?? -99999));
          }
          case 'quality': {
            // Pass/fail outer, then quality rank, title as final tie-break.
            const dp = passOf(b) - passOf(a);
            if (dp !== 0) return dp;
            const dq = rankOf(a) - rankOf(b);
            if (dq !== 0) return dir * dq;
            return (a.title || '').localeCompare(b.title || '');
          }
          case 'status': {
            // Use the same Sonarr/Radarr-aware status as the display
            // (quality allowed AND score >= min) so sort matches what
            // the user sees in the Status pill.
            const aPass = this.sandboxResultStatus(a, a.scoring, appType).pass ? 1 : 0;
            const bPass = this.sandboxResultStatus(b, b.scoring, appType).pass ? 1 : 0;
            return dir * (aPass - bPass);
          }
          case 'group': return dir * (a.parsed?.releaseGroup || '').localeCompare(b.parsed?.releaseGroup || '');
          case 'title': return dir * a.title.localeCompare(b.title);
        }
        return 0;
      });
      return results;
    },

    // Sort then apply the active score-set filter and the "Show
    // selected only" filter. Table uses this instead of
    // sortedSandboxResults directly so the filter chain lives in one
    // place. Score-set filter narrows by saved title list (Set lookup
    // is O(1) so this stays cheap even with many results); the
    // selected filter then narrows further if active.
    visibleSandboxResults(appType) {
      const sb = this.sandbox[appType];
      this._sbEnsureIds(sb.results || []);
      let results = this.sortedSandboxResults(appType);
      if (sb.activeScoreSet) {
        const set = (sb.scoreSets || []).find(s => s.id === sb.activeScoreSet);
        if (set) {
          const setTitles = new Set(set.titles || []);
          results = results.filter(r => setTitles.has(r.title));
        }
      }
      if (sb.filterToSelected) results = results.filter(r => r._selected === true);
      return results;
    },

    sandboxSelectedCount(appType) {
      return (this.sandbox[appType].results || []).filter(r => r._selected === true).length;
    },

    toggleSandboxSelectAll(appType) {
      const sb = this.sandbox[appType];
      const all = (sb.results || []);
      const allSelected = all.length > 0 && all.every(r => r._selected === true);
      all.forEach(r => { r._selected = !allSelected; });
      // trigger reactivity — mutating props in place isn't always picked up
      sb.results = [...all];
    },

    toggleSandboxSort(appType, col) {
      const sb = this.sandbox[appType];
      if (sb.sortCol === col) {
        sb.sortDir = sb.sortDir === 'asc' ? 'desc' : 'asc';
      } else {
        sb.sortCol = col;
        sb.sortDir = col === 'title' || col === 'group' ? 'asc' : 'desc';
      }
    },

    // Format a single sandbox result as a readable plain-text block for sharing.
    // Includes the full title, parsed metadata, scores (primary profile + compare
    // if active), and the matched/unmatched CF breakdown. Monospace-friendly.
    formatSandboxResultForCopy(appType, res) {
      const sb = this.sandbox[appType];
      const lines = [];
      lines.push(res.title);
      lines.push('');
      const p = res.parsed || {};
      if (p.quality)      lines.push('Quality:      ' + p.quality);
      if (p.releaseGroup) lines.push('Group:        ' + p.releaseGroup);
      if (p.languages?.length) lines.push('Languages:    ' + p.languages.join(', '));
      if (p.edition)      lines.push('Edition:      ' + p.edition);
      const scoreLine = (label, s) => {
        if (!s) return;
        const status = (s.total ?? 0) >= (s.minScore || 0) ? 'PASS' : 'FAIL';
        lines.push(`${label.padEnd(13)} ${s.total} (${status}, min: ${s.minScore || 0})`);
      };
      scoreLine('Score:', res.scoring);
      if (sb.compareKey && res.scoringB) {
        const cmpName = this.sandboxCompareProfileName(appType) || 'Compare';
        scoreLine(cmpName.slice(0, 12) + ':', res.scoringB);
      }
      const breakdown = res.scoring?.breakdown || [];
      const matched = breakdown.filter(b => b.matched);
      const unmatched = breakdown.filter(b => !b.matched && b.score !== 0);
      if (matched.length) {
        lines.push('');
        lines.push('Matched CFs:');
        for (const b of matched) {
          const sgn = b.score > 0 ? '+' : '';
          lines.push(`  ${(sgn + b.score).padStart(6)}  ${b.name}`);
        }
      }
      if (unmatched.length) {
        lines.push('');
        lines.push('Unmatched (in profile, not in release):');
        for (const b of unmatched) {
          const sgn = b.score > 0 ? '+' : '';
          lines.push(`  ${(sgn + b.score).padStart(6)}  ${b.name}`);
        }
      }
      return lines.join('\n');
    },

    openSandboxCopy(appType, res) {
      this.sandboxCopyModal = {
        show: true,
        title: res.title,
        text: this.formatSandboxResultForCopy(appType, res),
        copied: false,
      };
    },

    copySandboxModalText() {
      copyToClipboard(this.sandboxCopyModal.text);
      this.sandboxCopyModal.copied = true;
      setTimeout(() => { this.sandboxCopyModal.copied = false; }, 1500);
    },

    // Drag-reorder rows. Works only when sortCol is 'manual' (or user just dropped —
    // we set it to 'manual' so the drag outcome sticks). Operates on the underlying
    // sb.results array by matching the dragged/target result objects (identity-safe).
    sandboxDragStart(appType, res) {
      this.sandbox[appType].dragSrc = res;
    },
    sandboxDragOver(appType, res) {
      this.sandbox[appType].dragOver = res;
    },
    sandboxDrop(appType, targetRes) {
      const sb = this.sandbox[appType];
      const src = sb.dragSrc;
      sb.dragSrc = null;
      sb.dragOver = null;
      if (!src || src === targetRes) return;
      const arr = [...(sb.results || [])];
      const fromIdx = arr.indexOf(src);
      const toIdx = arr.indexOf(targetRes);
      if (fromIdx < 0 || toIdx < 0) return;
      arr.splice(fromIdx, 1);
      arr.splice(toIdx, 0, src);
      sb.results = arr;
      sb.sortCol = 'manual'; // exit sorted view so the drag order sticks
      this.saveSandboxResults(appType);
    },

    async sandboxParse(appType) {
      const sb = this.sandbox[appType];
      const title = sb.pasteInput?.trim();
      if (!title || !sb.instanceId) return;
      sb.parsing = true;
      try {
        const r = await fetch('/api/scoring/parse', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ instanceId: sb.instanceId, title })
        });
        if (!r.ok) { const e = await r.json().catch(() => ({})); this.showToast(e.error || 'Parse failed', 'error', 8000); return; }
        const result = await r.json();
        const scored = await this.calculateScoring(result, appType);
        sb.results = [scored, ...sb.results];
        this.saveSandboxResults(appType);
        sb.pasteInput = '';
      } catch (e) { this.showToast('Parse error: ' + e.message, 'error', 8000); }
      finally { sb.parsing = false; }
    },

    async sandboxParseBulk(appType) {
      const sb = this.sandbox[appType];
      const lines = (sb.bulkInput || '').split('\n').map(l => l.trim()).filter(Boolean);
      if (lines.length === 0 || !sb.instanceId) return;
      sb.parsing = true;
      // Each parse is one sequential call against the Arr Parse API. At ~100ms
      // per call, a 200-title batch takes ~20s — surface that to the user
      // instead of leaving them staring at a quiet spinner.
      if (lines.length > 30) {
        this.showToast(`Parsing ${lines.length} titles, this may take a moment...`, 'info', 6000);
      }
      try {
        const r = await fetch('/api/scoring/parse/batch', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ instanceId: sb.instanceId, titles: lines })
        });
        if (!r.ok) { const e = await r.json().catch(() => ({})); this.showToast(e.error || 'Batch parse failed', 'error', 8000); return; }
        const results = await r.json();
        const scored = await Promise.all(results.map(result => this.calculateScoring(result, appType)));
        const before = sb.results.length;
        sb.results = this._sandboxMergeNew(scored, sb.results);
        const replaced = scored.length - (sb.results.length - before);
        this.saveSandboxResults(appType);
        sb.bulkInput = '';
        if (replaced > 0) {
          this.showToast(`Re-scored ${replaced} duplicate title${replaced > 1 ? 's' : ''} already in the list.`, 'info', 4000);
        }
      } catch (e) { this.showToast('Batch parse error: ' + e.message, 'error', 8000); }
      finally { sb.parsing = false; }
    },

    sandboxIndexerLabel(appType) {
      const sb = this.sandbox[appType];
      const sel = sb.selectedIndexers || [];
      const all = sb.indexers || [];
      if (sel.length === 0 || sel.length === all.length) return 'All Indexers';
      if (sel.length === 1) {
        const idx = all.find(i => i.id === sel[0]);
        return idx ? idx.name : '1 indexer';
      }
      return sel.length + ' indexers';
    },

    sandboxToggleIndexer(appType, id) {
      const sb = this.sandbox[appType];
      if (!sb.selectedIndexers) sb.selectedIndexers = [];
      const i = sb.selectedIndexers.indexOf(id);
      if (i >= 0) {
        sb.selectedIndexers.splice(i, 1);
      } else {
        sb.selectedIndexers.push(id);
      }
    },

    sandboxToggleAllIndexers(appType) {
      const sb = this.sandbox[appType];
      const all = (sb.indexers || []).map(i => i.id);
      if (sb.selectedIndexers?.length === all.length) {
        sb.selectedIndexers = [];
      } else {
        sb.selectedIndexers = [...all];
      }
    },

    async sandboxSearch(appType) {
      const sb = this.sandbox[appType];
      const query = sb.searchQuery?.trim();
      if (!query) return;
      if (sb.searchCooldownRemaining > 0) return;
      if (sb.searchAbort) sb.searchAbort.abort();
      const abort = new AbortController();
      sb.searchAbort = abort;
      sb.searching = true;
      sb.searchError = '';
      sb.searchResults = [];
      sb.searchFilterText = '';
      sb.searchFilterRes = '';
      sb.indexerDropdown = false;
      try {
        // Categories: use user override from Settings if set, else Newznab defaults
        // (2000 = Movies root, 5000 = TV root). Some private-tracker indexer definitions
        // don't cascade the parent ID to sub-categories, so users may need to specify
        // sub-IDs explicitly (e.g. 2040, 2045) for searches to return results.
        const defaultCats = appType === 'radarr' ? [2000] : [5000];
        const override = appType === 'radarr'
          ? this.config.prowlarr?.radarrCategories
          : this.config.prowlarr?.sonarrCategories;
        const categories = (override && override.length > 0) ? override : defaultCats;
        const indexerIds = sb.selectedIndexers?.length > 0 ? sb.selectedIndexers : [];
        const r = await fetch('/api/scoring/prowlarr/search', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ query, categories, indexerIds }),
          signal: abort.signal
        });
        if (r.status === 429) {
          // Server cooldown active — sync our timer to the server's Retry-After
          // so the button reflects actual time remaining. Defends against
          // multiple browser tabs / lost-state edge cases.
          const retryAfter = parseInt(r.headers.get('Retry-After'), 10) || 120;
          this.startSearchCooldown(appType, retryAfter);
          const e = await r.json().catch(() => ({}));
          sb.searchError = e.error || `Search rate limited — wait ${retryAfter}s`;
          return;
        }
        if (!r.ok) { const e = await r.json().catch(() => ({})); sb.searchError = e.error || 'Search failed'; return; }
        const results = await r.json();
        sb.searchResults = results.map(r => ({ ...r, _selected: false }));
        // Successful search → start 120s cooldown to match server.
        this.startSearchCooldown(appType, 120);
      } catch (e) {
        if (e.name === 'AbortError') { sb.searchError = ''; return; }
        sb.searchError = 'Search error: ' + e.message;
      }
      finally { sb.searching = false; sb.searchAbort = null; }
    },

    // Per-app-type cooldown ticker. setInterval lives only while cooldown
    // is active — cleaned up when remaining hits 0 or another search starts.
    // No global timer, no leaked intervals.
    startSearchCooldown(appType, seconds) {
      const sb = this.sandbox[appType];
      sb.searchCooldownRemaining = seconds;
      if (sb._searchCooldownTimer) clearInterval(sb._searchCooldownTimer);
      sb._searchCooldownTimer = setInterval(() => {
        sb.searchCooldownRemaining = Math.max(0, sb.searchCooldownRemaining - 1);
        if (sb.searchCooldownRemaining === 0) {
          clearInterval(sb._searchCooldownTimer);
          sb._searchCooldownTimer = null;
        }
      }, 1000);
    },

    sandboxCancelSearch(appType) {
      const sb = this.sandbox[appType];
      if (sb.searchAbort) { sb.searchAbort.abort(); sb.searchAbort = null; }
      sb.searching = false;
    },

    filteredSearchResults(appType) {
      const sb = this.sandbox[appType];
      let results = sb.searchResults || [];
      const text = sb.searchFilterText?.trim().toLowerCase();
      if (text) results = results.filter(r => r.title.toLowerCase().includes(text));
      const res = sb.searchFilterRes;
      if (res) {
        // Match exact resolution token — not source descriptors like "UHD BluRay"
        const patterns = {
          '2160p': /\b2160p\b/i,
          '1080p': /\b1080p\b/i,
          '720p': /\b720p\b/i,
          '480p': /\b480p\b/i,
        };
        const pat = patterns[res];
        if (pat) results = results.filter(r => pat.test(r.title));
      }
      return results;
    },

    saveSandboxResults(appType) {
      const sb = this.sandbox[appType];
      const data = (sb.results || []).map(r => ({ title: r.title, parsed: r.parsed, matchedCFs: r.matchedCFs, instanceScore: r.instanceScore }));
      try { localStorage.setItem('clonarr-sandbox-' + appType, JSON.stringify(data)); } catch (e) {}
    },

    // Merge freshly scored items into the existing results list, with
    // title-based dedupe — fresh items take precedence so re-scoring the
    // same title overwrites the old entry instead of stacking duplicates
    // (the prior behaviour produced "12 releases" lists where 4 were the
    // same title from earlier Score Selected runs).
    _sandboxMergeNew(newItems, existing) {
      const seen = new Set();
      const out = [];
      for (const r of (newItems || [])) {
        if (!r || !r.title || seen.has(r.title)) continue;
        seen.add(r.title);
        out.push(r);
      }
      for (const r of (existing || [])) {
        if (!r || !r.title || seen.has(r.title)) continue;
        seen.add(r.title);
        out.push(r);
      }
      return out;
    },

    // --- Score Sets ---
    // A score set is a named collection of release titles that the user
    // wants to test repeatedly against profile changes. Implemented as a
    // saved title-list filter on top of the normal results list, so:
    //   - Adding new releases to a set is just append-titles.
    //   - Activating a set filters visibleSandboxResults to those titles.
    //   - Score Selected still adds to the unfiltered main results — set
    //     contents are explicitly curated, never auto-grown.
    // Persisted to localStorage per app-type so sets survive reloads
    // alongside the existing results storage.

    sandboxSaveScoreSets(appType) {
      const sb = this.sandbox[appType];
      try {
        localStorage.setItem('clonarr-sandbox-sets-' + appType, JSON.stringify(sb.scoreSets || []));
        localStorage.setItem('clonarr-sandbox-active-' + appType, sb.activeScoreSet || '');
      } catch (e) { /* quota — best-effort */ }
    },

    sandboxLoadScoreSets(appType) {
      const sb = this.sandbox[appType];
      try {
        const raw = localStorage.getItem('clonarr-sandbox-sets-' + appType);
        if (raw) {
          const sets = JSON.parse(raw);
          if (Array.isArray(sets)) sb.scoreSets = sets;
        }
        const active = localStorage.getItem('clonarr-sandbox-active-' + appType) || '';
        // Only restore active if the set still exists — avoids ghost
        // filter that hides everything because the set was deleted.
        if (active && (sb.scoreSets || []).some(s => s.id === active)) {
          sb.activeScoreSet = active;
        } else {
          sb.activeScoreSet = '';
        }
      } catch (e) { /* corrupt JSON — ignore, start fresh */ }
    },

    _sandboxNewSetId() {
      return (typeof crypto !== 'undefined' && crypto.randomUUID)
        ? crypto.randomUUID()
        : 'set-' + Date.now() + '-' + Math.random().toString(36).slice(2, 10);
    },

    async sandboxCreateScoreSetFromSelected(appType) {
      const sb = this.sandbox[appType];
      // Dedupe selected titles up-front. Score Selected can produce
      // multiple identical-title rows over multiple sessions; saving
      // those into a set as duplicates wastes storage and clutters the
      // count display ("12 releases" when only 9 are unique).
      const selectedTitles = [...new Set(
        (sb.results || []).filter(r => r._selected === true).map(r => r.title)
      )];
      if (selectedTitles.length === 0) {
        this.showToast('Select one or more releases first.', 'info', 4000);
        return;
      }
      const name = await new Promise(resolve => {
        this.inputModal = {
          show: true,
          title: 'Save Score Set',
          message: `Save ${selectedTitles.length} selected release${selectedTitles.length > 1 ? 's' : ''} as a new score set. Use the score sets dropdown later to filter the results to just this group.`,
          placeholder: 'e.g. SQP test 1080p',
          value: '',
          confirmLabel: 'Save',
          onConfirm: (val) => resolve((val || '').trim()),
          onCancel: () => resolve('')
        };
      });
      if (!name) return;
      const set = { id: this._sandboxNewSetId(), name, titles: selectedTitles };
      sb.scoreSets = [...(sb.scoreSets || []), set];
      // Switch to the new set and clear the row selection so the user
      // sees exactly what they just saved.
      sb.activeScoreSet = set.id;
      sb.results.forEach(r => r._selected = false);
      sb.filterToSelected = false;
      this.sandboxSaveScoreSets(appType);
      this.showToast(`Saved score set "${name}" (${selectedTitles.length} release${selectedTitles.length > 1 ? 's' : ''}).`, 'info', 4000);
    },

    sandboxAddSelectedToScoreSet(appType, scoreSetId) {
      const sb = this.sandbox[appType];
      const selectedTitles = (sb.results || []).filter(r => r._selected === true).map(r => r.title);
      if (selectedTitles.length === 0) {
        this.showToast('Select one or more releases first.', 'info', 4000);
        return;
      }
      const set = (sb.scoreSets || []).find(s => s.id === scoreSetId);
      if (!set) return;
      const existing = new Set(set.titles || []);
      let added = 0;
      for (const t of selectedTitles) {
        if (!existing.has(t)) {
          existing.add(t);
          added++;
        }
      }
      set.titles = [...existing];
      sb.scoreSets = [...sb.scoreSets]; // reactivity
      sb.results.forEach(r => r._selected = false);
      this.sandboxSaveScoreSets(appType);
      const skipped = selectedTitles.length - added;
      const msg = added > 0
        ? `Added ${added} release${added > 1 ? 's' : ''} to "${set.name}"${skipped > 0 ? ` (${skipped} already in set)` : ''}.`
        : `All ${selectedTitles.length} already in "${set.name}".`;
      this.showToast(msg, 'info', 4000);
    },

    sandboxSetActiveScoreSet(appType, id) {
      const sb = this.sandbox[appType];
      sb.activeScoreSet = id || '';
      this.sandboxSaveScoreSets(appType);
    },

    // Remove every checkbox-selected release from the active score set.
    // Releases stay in sb.results — only their membership in the set is
    // dropped. Selection-driven so the user controls scope by check vs
    // un-check, and the same checkbox UX that drives Add-to-existing /
    // New set drives this too.
    sandboxRemoveSelectedFromScoreSet(appType) {
      const sb = this.sandbox[appType];
      if (!sb.activeScoreSet) return;
      const set = (sb.scoreSets || []).find(s => s.id === sb.activeScoreSet);
      if (!set) return;
      const selectedTitles = new Set(
        (sb.results || []).filter(r => r._selected === true).map(r => r.title)
      );
      if (selectedTitles.size === 0) {
        this.showToast('Select one or more releases first.', 'info', 4000);
        return;
      }
      const before = (set.titles || []).length;
      set.titles = (set.titles || []).filter(t => !selectedTitles.has(t));
      const removed = before - set.titles.length;
      if (removed === 0) return;
      sb.scoreSets = [...sb.scoreSets];
      sb.results.forEach(r => r._selected = false);
      this.sandboxSaveScoreSets(appType);
      this.showToast(`Removed ${removed} release${removed > 1 ? 's' : ''} from "${set.name}" (still in results).`, 'info', 4000);
    },

    async sandboxDeleteScoreSet(appType, id) {
      const sb = this.sandbox[appType];
      const set = (sb.scoreSets || []).find(s => s.id === id);
      if (!set) return;
      const ok = await new Promise(resolve => {
        this.confirmModal = {
          show: true,
          title: 'Delete score set?',
          message: `Delete "${set.name}" (${(set.titles || []).length} release${(set.titles || []).length === 1 ? '' : 's'})? This only removes the saved set — the underlying releases stay in your results.`,
          confirmLabel: 'Delete',
          onConfirm: () => resolve(true),
          onCancel: () => resolve(false)
        };
      });
      if (!ok) return;
      sb.scoreSets = (sb.scoreSets || []).filter(s => s.id !== id);
      if (sb.activeScoreSet === id) sb.activeScoreSet = '';
      this.sandboxSaveScoreSets(appType);
    },

    async sandboxRenameScoreSet(appType, id) {
      const sb = this.sandbox[appType];
      const set = (sb.scoreSets || []).find(s => s.id === id);
      if (!set) return;
      const name = await new Promise(resolve => {
        this.inputModal = {
          show: true,
          title: 'Rename Score Set',
          message: 'Choose a new name for this score set.',
          placeholder: 'Score set name',
          value: set.name,
          confirmLabel: 'Rename',
          onConfirm: (val) => resolve((val || '').trim()),
          onCancel: () => resolve('')
        };
      });
      if (!name || name === set.name) return;
      set.name = name;
      sb.scoreSets = [...sb.scoreSets];
      this.sandboxSaveScoreSets(appType);
    },

    sandboxActiveScoreSetName(appType) {
      const sb = this.sandbox[appType];
      const set = (sb.scoreSets || []).find(s => s.id === sb.activeScoreSet);
      return set ? set.name : '';
    },

    async loadSandboxResults(appType) {
      try {
        const raw = localStorage.getItem('clonarr-sandbox-' + appType);
        if (!raw) return;
        const data = JSON.parse(raw);
        if (!Array.isArray(data) || data.length === 0) return;
        const sb = this.sandbox[appType];
        // One-time dedupe of historical duplicates: users who hit Score
        // Selected on the same title across multiple sessions before the
        // dedupe landed have stacks of identical rows. Re-merging the
        // restored array against an empty existing list collapses them.
        sb.results = this._sandboxMergeNew(data, []);
        if (sb.results.length !== data.length) {
          this.saveSandboxResults(appType); // persist the cleanup
        }
        // Re-apply scoring if profile is selected
        if (sb.profileKey) {
          const profileData = await this.fetchProfileScores(sb.profileKey, appType);
          sb.results = sb.results.map(res => this.applyScoring(res, profileData));
        }
      } catch (e) {}
    },

    async sandboxScoreSelected(appType) {
      const sb = this.sandbox[appType];
      const selected = (sb.searchResults || []).filter(r => r._selected);
      if (selected.length === 0) return;
      // Defensive auto-init: Score Selected used to silently no-op when
      // sb.instanceId was empty (loadSandbox hadn't run yet on this
      // page-load). Auto-pick the first instance of this type and load
      // its profiles so the dropdown + scoring start working
      // immediately. Toast + return only if the user genuinely has no
      // matching instance.
      if (!sb.instanceId) {
        const insts = this.instancesOfType(appType);
        if (insts.length === 0) {
          this.showToast(`Configure a ${appType} instance in Settings before scoring.`, 'error', 6000);
          return;
        }
        sb.instanceId = insts[0].id;
        await this.sandboxInstanceChanged(appType);
      }
      sb.parsing = true;
      if (selected.length > 30) {
        this.showToast(`Parsing ${selected.length} titles, this may take a moment...`, 'info', 6000);
      }
      try {
        const titles = selected.map(r => r.title);
        const r = await fetch('/api/scoring/parse/batch', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ instanceId: sb.instanceId, titles })
        });
        if (!r.ok) { const e = await r.json().catch(() => ({})); this.showToast(e.error || 'Parse failed', 'error', 8000); return; }
        const results = await r.json();
        const scored = await Promise.all(results.map(result => this.calculateScoring(result, appType)));
        const before = sb.results.length;
        sb.results = this._sandboxMergeNew(scored, sb.results);
        const replaced = scored.length - (sb.results.length - before);
        this.saveSandboxResults(appType);
        // Clear selections
        sb.searchResults.forEach(r => r._selected = false);
        if (replaced > 0) {
          this.showToast(`Re-scored ${replaced} duplicate title${replaced > 1 ? 's' : ''} already in the list.`, 'info', 4000);
        }
      } catch (e) { this.showToast('Score error: ' + e.message, 'error', 8000); }
      finally { sb.parsing = false; }
    },

    // Profile score cache: { "radarr:trash:abc123": { scores: [{trashId, name, score}], minScore: 0 } }
    _profileScoreCache: {},

    async fetchProfileScores(profileKey, appType) {
      const cacheKey = appType + ':' + profileKey;
      if (this._profileScoreCache[cacheKey]) return this._profileScoreCache[cacheKey];
      const sb = this.sandbox[appType];
      const params = new URLSearchParams({ profileKey, appType });
      if (profileKey.startsWith('inst:')) params.set('instanceId', sb.instanceId);
      try {
        const r = await fetch('/api/scoring/profile-scores?' + params);
        if (!r.ok) return { scores: [], minScore: 0 };
        const data = await r.json();
        this._profileScoreCache[cacheKey] = data;
        return data;
      } catch (e) { return { scores: [], minScore: 0 }; }
    },

    async rescoreSandbox(appType) {
      const sb = this.sandbox[appType];
      if (!sb.results?.length || !sb.profileKey) return;
      const cacheKey = appType + ':' + sb.profileKey;
      delete this._profileScoreCache[cacheKey];
      const profileData = await this.fetchProfileScores(sb.profileKey, appType);
      sb.results = sb.results.map(res => this.applyScoring(res, profileData));
      // Re-score compare profile too
      if (sb.compareKey) this.rescoreCompare(appType);
    },

    async rescoreCompare(appType) {
      const sb = this.sandbox[appType];
      if (!sb.results?.length || !sb.compareKey) {
        sb.results = sb.results.map(res => { const r = {...res}; delete r.scoringB; return r; });
        return;
      }
      const cacheKey = appType + ':' + sb.compareKey;
      delete this._profileScoreCache[cacheKey];
      const profileData = await this.fetchProfileScores(sb.compareKey, appType);
      sb.results = sb.results.map(res => {
        const scored = this.applyScoring(res, profileData);
        return { ...res, scoringB: scored.scoring };
      });
    },

    async toggleSandboxEdit(appType) {
      const sb = this.sandbox[appType];
      if (sb.editOpen) {
        sb.editOpen = false;
        // Re-score with original profile to undo edits
        await this.rescoreSandbox(appType);
        return;
      }
      if (!sb.profileKey) return;
      const profileData = await this.fetchProfileScores(sb.profileKey, appType);
      sb.editOriginal = JSON.parse(JSON.stringify(profileData));
      sb.editScores = {};
      sb.editToggles = {};
      sb.editMinScore = null;
      sb.editOpen = true;
    },

    resetSandboxEdit(appType) {
      const sb = this.sandbox[appType];
      sb.editScores = {};
      sb.editToggles = {};
      sb.editMinScore = null;
      this.applySandboxEdit(appType);
    },

    _sandboxEditTimer: null,
    debounceSandboxEdit(appType) {
      clearTimeout(this._sandboxEditTimer);
      this._sandboxEditTimer = setTimeout(() => this.applySandboxEdit(appType), 200);
    },

    applySandboxEdit(appType) {
      const sb = this.sandbox[appType];
      if (!sb.editOriginal || !sb.results?.length) return;
      // Build modified profile data from original + edits
      const modified = {
        scores: sb.editOriginal.scores
          .filter(s => sb.editToggles[s.trashId || s.name] !== false)
          .map(s => ({
            ...s,
            score: sb.editScores[s.trashId || s.name] ?? s.score
          })),
        minScore: sb.editMinScore ?? sb.editOriginal.minScore ?? 0
      };
      // Add any extra CFs added by user
      for (const key of Object.keys(sb.editToggles)) {
        if (sb.editToggles[key] === 'added') {
          modified.scores.push({ trashId: key, name: sb._addedCFNames?.[key] || key, score: sb.editScores[key] ?? 0 });
        }
      }
      sb.results = sb.results.map(res => this.applyScoring(res, modified));
    },

    _sandboxCFCache: {},
    _trashScoreContextCache: {},
    async openSandboxCFBrowser(appType) {
      const sb = this.sandbox[appType];
      const selected = {};
      const scores = {};
      const inProfile = {};
      // Mark CFs already in the profile (show as ON + disabled)
      for (const s of (sb.editOriginal?.scores || [])) {
        const key = s.trashId || s.name;
        selected[key] = true;
        scores[key] = sb.editScores[key] ?? s.score;
        inProfile[key] = true;
      }
      // Also mark CFs added via editToggles
      for (const key of Object.keys(sb.editToggles)) {
        if (sb.editToggles[key] === 'added') {
          selected[key] = true;
          scores[key] = sb.editScores[key] ?? 0;
        }
      }
      this.sandboxCFBrowser = { open: true, appType, categories: [], customCFs: [], selected, scores, inProfile, expanded: {}, filter: '' };
      // Fetch categories + custom CFs
      try {
        const [cfRes, customRes] = await Promise.all([
          fetch(`/api/trash/${appType}/all-cfs`),
          fetch(`/api/custom-cfs/${appType}`)
        ]);
        if (cfRes.ok) {
          const data = await cfRes.json();
          this.sandboxCFBrowser.categories = data.categories || [];
        }
        if (customRes.ok) {
          this.sandboxCFBrowser.customCFs = await customRes.json() || [];
        }
      } catch (e) { console.error('openSandboxCFBrowser:', e); }
    },

    closeSandboxCFBrowser() {
      const br = this.sandboxCFBrowser;
      const sb = this.sandbox[br.appType];
      if (!sb) { br.open = false; return; }
      // Apply selected CFs to edit state
      if (!sb._addedCFNames) sb._addedCFNames = {};
      // Remove previously added CFs that are now deselected
      for (const key of Object.keys(sb.editToggles)) {
        if (sb.editToggles[key] === 'added' && !br.selected[key]) {
          delete sb.editToggles[key];
          delete sb.editScores[key];
          delete sb._addedCFNames[key];
        }
      }
      // Add newly selected CFs
      const allCFs = {};
      for (const cat of br.categories) {
        for (const g of cat.groups) {
          for (const cf of g.cfs) { allCFs[cf.trashId] = cf.name; }
        }
      }
      for (const cf of br.customCFs || []) { allCFs[cf.id] = cf.name; }
      for (const [key, on] of Object.entries(br.selected)) {
        if (on) {
          const existing = (sb.editOriginal?.scores || []).find(s => s.trashId === key);
          if (!existing) {
            sb.editToggles[key] = 'added';
            sb.editScores[key] = br.scores[key] ?? 0;
            sb._addedCFNames[key] = allCFs[key] || key;
          }
        }
      }
      br.open = false;
      this.applySandboxEdit(br.appType);
    },

    sandboxCFBrowserCatCount(cat) {
      let count = 0;
      for (const g of cat.groups) {
        for (const cf of g.cfs) {
          if (this.sandboxCFBrowser.selected[cf.trashId]) count++;
        }
      }
      const total = cat.groups.reduce((sum, g) => sum + g.cfs.length, 0);
      return count + '/' + total;
    },

    async sandboxSearchCFs(appType, query) {
      if (!query || query.length < 2) return [];
      // Cache TRaSH + custom CFs per appType
      if (!this._sandboxCFCache[appType]) {
        try {
          const [trashRes, customRes] = await Promise.all([
            fetch(`/api/trash/${appType}/cfs`),
            fetch(`/api/custom-cfs/${appType}`)
          ]);
          const trashCFs = trashRes.ok ? await trashRes.json() : [];
          const customCFs = customRes.ok ? await customRes.json() : [];
          // Merge: custom CFs use their id as trashId, marked with isCustom
          const merged = [...(trashCFs || [])];
          for (const cf of (customCFs || [])) {
            merged.push({ trashId: cf.id, name: cf.name, isCustom: true });
          }
          this._sandboxCFCache[appType] = merged;
        } catch { this._sandboxCFCache[appType] = []; }
      }
      const q = query.toLowerCase();
      const existing = new Set((this.sandbox[appType].editOriginal?.scores || []).map(s => s.trashId));
      const added = this.sandbox[appType].editToggles || {};
      return this._sandboxCFCache[appType].filter(cf => cf.name.toLowerCase().includes(q) && !existing.has(cf.trashId) && added[cf.trashId] !== 'added').slice(0, 15);
    },

    addSandboxEditCF(appType, cf) {
      const sb = this.sandbox[appType];
      if (!sb._addedCFNames) sb._addedCFNames = {};
      sb._addedCFNames[cf.trashId] = cf.name;
      sb.editToggles[cf.trashId] = 'added';
      sb.editScores[cf.trashId] = 0;
      this.debounceSandboxEdit(appType);
    },

    sandboxCompareProfileName(appType) {
      const key = this.sandbox[appType].compareKey;
      if (!key) return '';
      if (key.startsWith('trash:')) {
        const tid = key.replace('trash:', '');
        const p = (this.trashProfiles[appType] || []).find(p => p.trashId === tid);
        return p?.name || tid;
      }
      if (key.startsWith('imported:')) {
        const id = key.replace('imported:', '');
        const p = (this.importedProfiles[appType] || []).find(p => p.id === id);
        return p?.name || id;
      }
      if (key.startsWith('inst:')) {
        const id = parseInt(key.replace('inst:', ''));
        const p = (this.sandbox[appType].instanceProfiles || []).find(p => p.id === id);
        return p?.name || key;
      }
      return key;
    },

    async calculateScoring(result, appType) {
      const sb = this.sandbox[appType];
      const profileKey = sb.profileKey;
      if (!profileKey || !result.matchedCFs) return result;
      const profileData = await this.fetchProfileScores(profileKey, appType);
      let scored = this.applyScoring(result, profileData);
      // Also score against compare profile if active
      if (sb.compareKey) {
        const compareData = await this.fetchProfileScores(sb.compareKey, appType);
        const compScored = this.applyScoring(result, compareData);
        scored = { ...scored, scoringB: compScored.scoring };
      }
      return scored;
    },

    applyScoring(result, profileData) {
      if (!result.matchedCFs || !profileData?.scores?.length) return result;

      // Build lookup maps: by trashId and by name
      const byTrashId = {};
      const byName = {};
      for (const s of profileData.scores) {
        if (s.trashId) byTrashId[s.trashId] = s;
        if (s.name) byName[s.name] = s;
      }

      let total = 0;
      const breakdown = [];
      const matchedKeys = new Set();

      // Score matched CFs
      for (const cf of result.matchedCFs) {
        const entry = (cf.trashId && byTrashId[cf.trashId]) || byName[cf.name];
        const score = entry?.score ?? 0;
        total += score;
        breakdown.push({ name: cf.name, trashId: cf.trashId, score, matched: true });
        if (cf.trashId) matchedKeys.add(cf.trashId);
        matchedKeys.add(cf.name);
      }

      // Unmatched CFs from profile
      for (const s of profileData.scores) {
        if (matchedKeys.has(s.trashId) || matchedKeys.has(s.name)) continue;
        breakdown.push({ name: s.name, trashId: s.trashId, score: s.score, matched: false });
        if (s.trashId) matchedKeys.add(s.trashId);
        matchedKeys.add(s.name);
      }

      // Sort: matched first (by |score| desc), then unmatched
      breakdown.sort((a, b) => {
        if (a.matched !== b.matched) return a.matched ? -1 : 1;
        return Math.abs(b.score) - Math.abs(a.score);
      });

      return { ...result, scoring: { total, breakdown, minScore: profileData.minScore || 0 } };
    },

    formatBytes(bytes) {
      if (!bytes || bytes === 0) return '0 B';
      const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
      const i = Math.floor(Math.log(bytes) / Math.log(1024));
      return (bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0) + ' ' + units[i];
    },

    async testProwlarr() {
      this.prowlarrTesting = true;
      this.prowlarrTestResult = null;
      try {
        const r = await fetch('/api/prowlarr/test', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ url: this.config.prowlarr?.url, apiKey: this.config.prowlarr?.apiKey })
        });
        const data = await r.json();
        if (data.connected) {
          this.prowlarrTestResult = { ok: true, message: 'Connected', version: data.version };
        } else {
          this.prowlarrTestResult = { ok: false, message: data.error || 'Connection failed' };
        }
      } catch (e) {
        this.prowlarrTestResult = { ok: false, message: 'Network error: ' + e.message };
      }
      finally { this.prowlarrTesting = false; }
    },

  },
};
