import { copyToClipboard } from '../utils/clipboard.js';

// Module-level debounce timer map for sandbox state persistence.
// Lives outside Alpine's reactive data root so the setTimeout / clearTimeout
// IDs aren't routed through any reactive proxy on each access. Per-app-type
// timer ids so radarr and sonarr debounces are independent.
const _sandboxPersistTimers = {};

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

    // Open the Sandbox Export modal. Generates the initial text body
    // from the current visible+sorted result list with the breakdown
    // toggle off. Toggle changes are watched via x-effect in the
    // template so flipping the checkbox re-runs the formatter.
    //
    // Blurs the active element first so that when x-trap.inert applies
    // aria-hidden + inert to <main> on the next render tick, focus
    // isn't still parked on the trigger button (which would cause
    // Chrome to log "Blocked aria-hidden on an element because its
    // descendant retained focus"). x-trap moves focus into the modal
    // itself once it mounts; blur'ing here just gets it off the
    // trigger before the inert attribute lands.
    openSandboxExport(appType) {
      if (document.activeElement && typeof document.activeElement.blur === 'function') {
        document.activeElement.blur();
      }
      this.sandboxExportModal = {
        show: true,
        appType: appType,
        includeBreakdown: false,
        text: this.formatSandboxExportText(appType, false),
        copied: false,
      };
    },

    // Build the export string. Follows visibleSandboxResults' sort +
    // filter order so what the user sees is what gets exported. Three
    // formatting modes the function picks based on per-release scoring
    // state + the includeBreakdown toggle:
    //   • Release has no scoring             → "<title>"
    //   • Scoring + !includeBreakdown        → "<title>\t<total>"
    //   • Scoring + includeBreakdown         → header line then per-CF
    //     rows (alphabetical by CF name for stable diffs across two
    //     sessions of the same release scored on different profiles)
    //   • Multi-release output joins blocks with a blank line so diff
    //     tools align block boundaries cleanly.
    formatSandboxExportText(appType, includeBreakdown) {
      const rows = (typeof this.visibleSandboxResults === 'function'
        ? this.visibleSandboxResults(appType)
        : (this.sandbox?.[appType]?.results || [])) || [];
      if (rows.length === 0) return '';
      // Format the total score with the explicit sign so a +/- diff
      // shows up cleanly in diff tools' alignment view.
      const fmtScore = (n) => {
        if (typeof n !== 'number') return '0';
        return (n > 0 ? '+' : '') + String(n);
      };
      const blocks = rows.map(res => {
        const title = res.title || '';
        const scoring = res.scoring;
        if (!scoring || typeof scoring.total !== 'number') {
          // No scoring done — just the title. Common when the user
          // pasted a list and exports before clicking Score.
          return title;
        }
        if (!includeBreakdown) {
          // Compact single-line: title + total. Tab separator picks
          // up cleanly in diff tools and in pasted-into-spreadsheet
          // workflows alike.
          return `${title}\t${fmtScore(scoring.total)}`;
        }
        // Breakdown block: header line, then alphabetical CF rows so
        // diff tools align same-CF rows across two sessions even when
        // the scores differ. Unmatched CFs (score==0 from filter) are
        // skipped — they're noise for a comparison view.
        const breakdown = (scoring.breakdown || [])
          .filter(b => b && b.matched && typeof b.score === 'number' && b.score !== 0)
          .slice()
          .sort((a, b) => (a.name || '').localeCompare(b.name || ''));
        const lines = [`${title}\tTOTAL ${fmtScore(scoring.total)}`];
        for (const cf of breakdown) {
          lines.push(`  ${cf.name}\t${fmtScore(cf.score)}`);
        }
        return lines.join('\n');
      });
      return blocks.join('\n\n');
    },

    // Re-run the formatter when the toggle flips. Called from the
    // modal's checkbox @change.
    rerenderSandboxExport() {
      const m = this.sandboxExportModal;
      if (!m || !m.show) return;
      this.sandboxExportModal.text = this.formatSandboxExportText(m.appType, m.includeBreakdown);
    },

    // Copy the modal text to the clipboard. Mirrors copySandboxModalText
    // including the execCommand fallback for non-secure contexts.
    async copySandboxExportText() {
      const text = this.sandboxExportModal.text || '';
      let ok = false;
      if (navigator.clipboard && window.isSecureContext) {
        try { await navigator.clipboard.writeText(text); ok = true; } catch (_) { /* fall through */ }
      }
      if (!ok) {
        const pre = document.getElementById('sandbox-export-pre');
        if (pre) {
          const sel = document.getSelection();
          const saved = [];
          if (sel) {
            for (let i = 0; i < sel.rangeCount; i++) saved.push(sel.getRangeAt(i).cloneRange());
          }
          try {
            const range = document.createRange();
            range.selectNodeContents(pre);
            sel.removeAllRanges();
            sel.addRange(range);
            ok = document.execCommand('copy');
          } catch (_) { /* leave ok=false */ }
          if (sel) {
            sel.removeAllRanges();
            for (const r of saved) sel.addRange(r);
          }
        }
      }
      if (ok) {
        this.sandboxExportModal.copied = true;
        setTimeout(() => { this.sandboxExportModal.copied = false; }, 1500);
      } else if (typeof this.showToast === 'function') {
        this.showToast('Copy failed. Select the text and copy manually.', 'error', 4000);
      }
    },

    async copySandboxModalText() {
      const text = this.sandboxCopyModal.text || '';
      let ok = false;
      // Modern API works on HTTPS + localhost. We try it first because
      // it doesn't depend on DOM selection state.
      if (navigator.clipboard && window.isSecureContext) {
        try { await navigator.clipboard.writeText(text); ok = true; } catch (_) { /* fall through */ }
      }
      // Fallback: select the visible <pre>'s contents and run
      // execCommand('copy'). The pre is already in the modal's DOM and
      // visible, so it survives focus-trap inert ancestors and Chrome's
      // "off-screen textarea looks fake" rejections that broke the
      // generic copyToClipboard helper here.
      if (!ok) {
        const pre = document.getElementById('sandbox-copy-pre');
        if (pre) {
          const sel = document.getSelection();
          const saved = [];
          if (sel) {
            for (let i = 0; i < sel.rangeCount; i++) saved.push(sel.getRangeAt(i).cloneRange());
          }
          try {
            const range = document.createRange();
            range.selectNodeContents(pre);
            sel.removeAllRanges();
            sel.addRange(range);
            ok = document.execCommand('copy');
          } catch (_) { /* leave ok=false */ }
          if (sel) {
            sel.removeAllRanges();
            for (const r of saved) sel.addRange(r);
          }
        }
      }
      if (ok) {
        this.sandboxCopyModal.copied = true;
        setTimeout(() => { this.sandboxCopyModal.copied = false; }, 1500);
      } else if (typeof this.showToast === 'function') {
        this.showToast('Copy failed. Select the text and copy manually.', 'error', 4000);
      }
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
      const raw = (sb.bulkInput || '').trim();
      if (!raw || !sb.instanceId) return;

      // JSON-detect path: when someone pastes a sandbox file dump (the
      // shape produced by GET /api/sandbox/state/{appType}), import
      // both titles AND score sets in one shot so a "here are my test
      // releases plus my SQP-3 set" share round-trips cleanly.
      let lines;
      let importedSets = [];
      if (raw[0] === '{' || raw[0] === '[') {
        try {
          const parsed = JSON.parse(raw);
          if (Array.isArray(parsed)) {
            // Bare JSON array of titles.
            lines = parsed.filter(t => typeof t === 'string' && t.trim()).map(t => t.trim());
          } else if (parsed && typeof parsed === 'object') {
            if (Array.isArray(parsed.titles)) {
              lines = parsed.titles.filter(t => typeof t === 'string' && t.trim()).map(t => t.trim());
            }
            if (Array.isArray(parsed.scoreSets)) {
              importedSets = parsed.scoreSets;
            }
          }
          if (!lines) {
            this.showToast('JSON paste did not contain a titles array. Falling back to line-by-line parsing.', 'warning', 5000);
            lines = raw.split('\n').map(l => l.trim()).filter(Boolean);
          }
        } catch (_) {
          this.showToast('JSON paste failed to parse. Treating each line as a title.', 'warning', 5000);
          lines = raw.split('\n').map(l => l.trim()).filter(Boolean);
        }
      } else {
        lines = raw.split('\n').map(l => l.trim()).filter(Boolean);
      }

      if (lines.length === 0 && importedSets.length === 0) return;

      sb.parsing = true;
      // Each parse is one sequential call against the Arr Parse API. At ~100ms
      // per call, a 200-title batch takes ~20s — surface that to the user
      // instead of leaving them staring at a quiet spinner.
      if (lines.length > 30) {
        this.showToast(`Parsing ${lines.length} titles, this may take a moment...`, 'info', 6000);
      }
      try {
        let scored = [];
        if (lines.length > 0) {
          const r = await fetch('/api/scoring/parse/batch', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ instanceId: sb.instanceId, titles: lines })
          });
          if (!r.ok) { const e = await r.json().catch(() => ({})); this.showToast(e.error || 'Batch parse failed', 'error', 8000); return; }
          const results = await r.json();
          scored = await Promise.all(results.map(result => this.calculateScoring(result, appType)));
        }
        const before = sb.results.length;
        if (scored.length > 0) {
          sb.results = this._sandboxMergeNew(scored, sb.results);
        }
        const replaced = scored.length - (sb.results.length - before);
        // Import score sets after results so set.titles references new
        // result rows immediately. Merge dedupes by id and renames on
        // name-collision so an imported "test" doesn't silently
        // overwrite the local "test".
        let setsAdded = 0;
        if (importedSets.length > 0) {
          const beforeCount = (sb.scoreSets || []).length;
          sb.scoreSets = this._sandboxMergeSets(sb.scoreSets || [], importedSets);
          setsAdded = sb.scoreSets.length - beforeCount;
        }
        this.saveSandboxResults(appType);
        sb.bulkInput = '';
        const parts = [];
        if (scored.length > 0) parts.push(`${scored.length} title${scored.length === 1 ? '' : 's'} parsed`);
        if (setsAdded > 0) parts.push(`${setsAdded} score set${setsAdded === 1 ? '' : 's'} imported`);
        if (replaced > 0) parts.push(`${replaced} duplicate${replaced === 1 ? '' : 's'} re-scored`);
        if (parts.length > 0) {
          this.showToast(parts.join(', ') + '.', 'success', 4500);
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

    // Persist the parsed-results portion of the sandbox state. Routed
    // through the unified persister so both the localStorage cache and
    // the server file stay in sync; existing callers keep their old name
    // and don't need to know about server persistence.
    saveSandboxResults(appType) {
      this._sandboxPersistAll(appType);
    },

    // Unified persister for results + score sets + active set. Writes
    // localStorage immediately (synchronous, survives if the server is
    // unreachable mid-edit) and schedules a debounced PUT to the server
    // file. localStorage is intentionally kept in sync as a read-cache
    // and emergency backup — never deleted on successful server write.
    _sandboxPersistAll(appType) {
      const sb = this.sandbox[appType];
      if (!sb) return;
      // Clean ghost titles out of score sets before any persistence
      // (server file AND localStorage cache). Pre-2026-05-31 row
      // deletes left orphaned title-strings in set.titles that the UI
      // already hid via the visible-count helper but persisted across
      // reloads. Converging in-memory state now keeps every downstream
      // store self-consistent and the user's file shareable as-is.
      this._sandboxCleanGhostsInPlace(appType);
      const resultsCache = (sb.results || []).map(r => ({ title: r.title, parsed: r.parsed, matchedCFs: r.matchedCFs, instanceScore: r.instanceScore }));
      try { localStorage.setItem('clonarr-sandbox-' + appType, JSON.stringify(resultsCache)); } catch (_) {}
      try {
        localStorage.setItem('clonarr-sandbox-sets-' + appType, JSON.stringify(sb.scoreSets || []));
        localStorage.setItem('clonarr-sandbox-active-' + appType, sb.activeScoreSet || '');
      } catch (_) {}
      clearTimeout(_sandboxPersistTimers[appType]);
      _sandboxPersistTimers[appType] = setTimeout(() => {
        this._sandboxPutToServer(appType);
      }, 500);
    },

    // Prune set.titles entries that no longer exist as result rows. Run
    // before every persist so the on-disk file, localStorage cache, and
    // in-memory state all converge to the same self-consistent view.
    // Triggers Alpine reactivity only when something actually changed
    // (avoids a spurious render loop on no-op persists).
    _sandboxCleanGhostsInPlace(appType) {
      const sb = this.sandbox[appType];
      if (!sb || !Array.isArray(sb.scoreSets) || sb.scoreSets.length === 0) return;
      const haveTitles = new Set((sb.results || []).map(r => r?.title).filter(Boolean));
      let mutated = false;
      for (const s of sb.scoreSets) {
        if (!s || !Array.isArray(s.titles)) continue;
        const cleaned = s.titles.filter(t => haveTitles.has(t));
        if (cleaned.length !== s.titles.length) {
          s.titles = cleaned;
          mutated = true;
        }
      }
      if (mutated) sb.scoreSets = [...sb.scoreSets];
    },

    // Push current in-memory state to the server file. Server stores
    // ONLY the stable user-curated data — release-title strings and
    // named score sets. Parsed quality, matched CFs and per-profile
    // scoring all change as soon as the user picks a different profile,
    // so persisting them would be wasteful AND misleading. The file
    // stays small enough to share by paste / email and round-trip back
    // through bulk-import.
    //
    // Network failures swallow silently because localStorage already
    // holds the truth; the next reload retries via the migration path.
    // Non-2xx server responses (500 from disk-full, 502 from container
    // restart, etc.) log a console warning so a debugger can spot
    // server-side persistence drift. 401 is handled by the global fetch
    // interceptor's redirect to /login so we don't double-log it.
    async _sandboxPutToServer(appType) {
      const sb = this.sandbox[appType];
      if (!sb) return;
      const seen = new Set();
      const titles = [];
      for (const r of (sb.results || [])) {
        if (!r || typeof r.title !== 'string' || !r.title) continue;
        if (seen.has(r.title)) continue;
        seen.add(r.title);
        titles.push(r.title);
      }
      // sb.scoreSets has already been ghost-pruned by _sandboxPersistAll
      // (called synchronously before the debounce that fires this PUT),
      // so the payload is consistent with what the UI shows.
      const payload = {
        titles,
        scoreSets: sb.scoreSets || [],
      };
      try {
        const r = await fetch(`/api/sandbox/state/${appType}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
        if (!r.ok && r.status !== 401) {
          // eslint-disable-next-line no-console
          console.warn(`[sandbox:${appType}] server save failed: HTTP ${r.status}. localStorage cache retains the data; next reload will retry.`);
        }
      } catch (_) { /* network down: localStorage stays the source of truth until reachable */ }
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

    // Routed through the unified persister; existing callers keep their
    // old function name to avoid touching every save-site.
    sandboxSaveScoreSets(appType) {
      this._sandboxPersistAll(appType);
    },

    // Legacy entry point — boot called sandboxLoadScoreSets after
    // loadSandboxResults; the unified loader handles both, so this is now
    // a thin guarded delegate. Idempotent: only the first call per
    // app-type does the network work, subsequent calls no-op against the
    // _sandboxLoadedFor flag.
    sandboxLoadScoreSets(appType) {
      return this._sandboxLoadAll(appType);
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

    // Wipe all release rows from the table. Destructive — no undo.
    // Also clears the active score-set selection because keeping it
    // selected would show a count like "5 of 5" while displaying zero
    // rows (set's titles can no longer match anything in empty results).
    // Score sets themselves are kept — only the active selection clears.
    async sandboxClearResults(appType) {
      const sb = this.sandbox[appType];
      const count = (sb.results || []).length;
      if (count === 0) return;
      const ok = await new Promise(resolve => {
        this.confirmModal = {
          show: true,
          title: 'Clear all results?',
          message: `Wipe all ${count} release row${count === 1 ? '' : 's'} from the table. Saved score sets are kept, but any active set will unselect since its releases are gone. This cannot be undone — paste/search again to repopulate.`,
          confirmLabel: 'Clear',
          onConfirm: () => resolve(true),
          onCancel: () => resolve(false)
        };
      });
      if (!ok) return;
      sb.results = [];
      sb.activeScoreSet = '';
      sb.filterToSelected = false;
      this.saveSandboxResults(appType);
      this.sandboxSaveScoreSets(appType);
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

    // Number of titles in a score set that ALSO have a row in the
    // current results table. Titles whose results row was deleted (the
    // "Remove from results" button) stay in set.titles by design so a
    // re-paste re-attaches them, but the dropdown counter would otherwise
    // show stale totals like "test (8)" when the user can only see 4
    // entries. Counting intersection keeps the count truthful to what
    // the user actually sees while preserving set history.
    // Delete a result row AND remove its title from every saved score
    // set that contains it. The previous "delete from results only"
    // behaviour left ghost titles in sets, polluting the dropdown count
    // (test (8) when only 4 rows remained) and forcing users to
    // re-create sets to clean up. Removing from sets at the same time
    // matches what users intuitively expect from a top-level X button
    // on the row.
    sandboxRemoveResultRow(appType, res) {
      const sb = this.sandbox[appType];
      if (!sb || !res) return;
      const idx = (sb.results || []).indexOf(res);
      if (idx === -1) return;
      sb.results.splice(idx, 1);
      // Prune the deleted title from every score set that referenced it.
      // No-op for sets that didn't contain the title.
      const title = res.title;
      let changed = false;
      if (Array.isArray(sb.scoreSets)) {
        for (const s of sb.scoreSets) {
          if (!Array.isArray(s.titles)) continue;
          const next = s.titles.filter(t => t !== title);
          if (next.length !== s.titles.length) {
            s.titles = next;
            changed = true;
          }
        }
        if (changed) sb.scoreSets = [...sb.scoreSets]; // reactivity ping
      }
      this.saveSandboxResults(appType);
    },

    sandboxSetVisibleCount(appType, set) {
      if (!set || !Array.isArray(set.titles) || set.titles.length === 0) return 0;
      const sb = this.sandbox[appType];
      if (!sb || !Array.isArray(sb.results) || sb.results.length === 0) return 0;
      const haveTitles = new Set(sb.results.map(r => r.title));
      let n = 0;
      for (const t of set.titles) {
        if (haveTitles.has(t)) n++;
      }
      return n;
    },

    // Legacy entry point — main.js calls this on boot. Routes through
    // the unified loader which handles both results AND score sets.
    async loadSandboxResults(appType) {
      return this._sandboxLoadAll(appType);
    },

    // Per-app-type idempotency guard. Once the loader has resolved its
    // server-first / localStorage-fallback / migration decision, the
    // app-type is marked complete and subsequent calls no-op so the
    // multi-call boot sequence (loadSandboxResults + sandboxLoadScoreSets
    // for each app type) doesn't trigger duplicate fetches or duplicate
    // migrations.
    _sandboxLoadedFor: {},

    // Unified loader: server first, localStorage second, migration when
    // server is empty + localStorage has data. Designed to NEVER drop
    // data:
    //   - Successful server load + localStorage data → merge by
    //     title-dedupe (server wins on conflict, localStorage-only
    //     entries get appended). One PUT after merge if any new items
    //     came in from localStorage.
    //   - Server empty + localStorage has data → push to server, use as
    //     in-memory state.
    //   - Server unreachable → fall back to localStorage cache. Nothing
    //     is written until the server is reachable again.
    //   - Both empty → genuine first-run; in-memory state stays empty.
    async _sandboxLoadAll(appType) {
      if (this._sandboxLoadedFor[appType]) return;
      this._sandboxLoadedFor[appType] = true;

      const sb = this.sandbox[appType];
      if (!sb) return;

      // Read localStorage up front (cheap, synchronous, never throws).
      const lsResults = this._sandboxReadLocalResults(appType);
      const lsSetsBundle = this._sandboxReadLocalSets(appType);

      // Try the server. A network failure / 5xx leaves serverState null,
      // which we treat as "fall back to localStorage entirely". A 200
      // with empty arrays is the "server is fresh, migrate" signal.
      let serverState = null;
      try {
        const r = await fetch(`/api/sandbox/state/${appType}`);
        if (r.ok) serverState = await r.json();
      } catch (_) { /* serverState stays null → localStorage-only path */ }

      // Snapshot the pre-load in-memory state. main.js dispatches the
      // load WITHOUT await, so a user racing to the Sandbox tab could
      // call sandboxParse / sandboxScoreSelected before the fetch
      // returns. Those handlers mutate sb.results + sb.scoreSets and
      // call saveSandboxResults. Without preserving them, the merge /
      // migration branches below would silently overwrite that fresh
      // work. _sandboxMergeNew puts the first arg first, so any
      // freshly-added title wins on title-collision.
      const inMemoryResultsBefore = Array.isArray(sb.results) ? sb.results.slice() : [];
      const inMemorySetsBefore = Array.isArray(sb.scoreSets) ? sb.scoreSets.slice() : [];

      // The merge branches walk arrays of objects whose shape we only
      // partially trust (server file may have been hand-edited, an old
      // bug may have left malformed entries in localStorage). A single
      // null / string / missing-id entry would otherwise crash the
      // whole load and leave _sandboxLoadedFor true so retries are
      // dead. Wrap the entire decision tree in a try/catch and fall
      // back to "keep the in-memory snapshot" on any throw — the user
      // sees their current session intact and a console warning points
      // a debugger at the cause.
      try {
        if (serverState === null) {
          // Server unreachable. Restore from localStorage only. Do NOT
          // attempt to write anywhere — the user might be offline or
          // the backend is restarting; we shouldn't risk overwriting
          // the server next time it's reachable.
          sb.results = this._sandboxMergeNew(inMemoryResultsBefore, lsResults);
          sb.scoreSets = this._sandboxMergeSets(inMemorySetsBefore, lsSetsBundle.sets);
          sb.activeScoreSet = this._sandboxResolveActiveSet(sb.scoreSets, sb.activeScoreSet || lsSetsBundle.active);
        } else {
          // Server returns the slim shape: { titles: string[], scoreSets: [...] }.
          // We reconstruct full result-records by looking up each title
          // in the in-memory snapshot or the localStorage cache; titles
          // not cached anywhere get a placeholder and are queued for a
          // background batch /parse so the UI surfaces them immediately
          // and refreshes once Arr responds.
          const serverTitles = Array.isArray(serverState.titles) ? serverState.titles.filter(t => typeof t === 'string' && t) : [];
          const serverSets = Array.isArray(serverState.scoreSets) ? serverState.scoreSets : [];

          const serverEmpty = serverTitles.length === 0 && serverSets.length === 0;
          const lsHasData = lsResults.length > 0 || lsSetsBundle.sets.length > 0;

          if (serverEmpty && lsHasData) {
            // Migration path: localStorage holds the user's history in
            // the legacy fat shape. Keep those fat records in memory
            // for instant UI; the next persist downgrades the server
            // file to the new slim shape transparently. In-memory work
            // from the load window stays first so fresh edits survive.
            sb.results = this._sandboxMergeNew(inMemoryResultsBefore, lsResults);
            sb.scoreSets = this._sandboxMergeSets(inMemorySetsBefore, lsSetsBundle.sets);
            sb.activeScoreSet = this._sandboxResolveActiveSet(sb.scoreSets, sb.activeScoreSet || lsSetsBundle.active);
            this._sandboxPersistAll(appType);
            // eslint-disable-next-line no-console
            console.log(`[sandbox:${appType}] migrated ${sb.results.length} title(s) + ${sb.scoreSets.length} score set(s) from localStorage to server file.`);
          } else if (!serverEmpty) {
            // Reconstruct sb.results to match the server's title list.
            // Look up each title in the in-memory snapshot first, then
            // localStorage. Anything not cached anywhere becomes a
            // bare placeholder and joins needParse.
            const byTitle = new Map();
            for (const r of inMemoryResultsBefore) {
              if (r && typeof r.title === 'string') byTitle.set(r.title, r);
            }
            for (const r of lsResults) {
              if (r && typeof r.title === 'string' && !byTitle.has(r.title)) byTitle.set(r.title, r);
            }
            const reconstructed = [];
            const needParse = [];
            for (const title of serverTitles) {
              const cached = byTitle.get(title);
              if (cached) {
                reconstructed.push(cached);
              } else {
                reconstructed.push({ title });
                needParse.push(title);
              }
            }
            // Append any in-memory titles the server hasn't seen yet —
            // race protection for a user who scored a fresh title during
            // the load window. Those get persisted via the dirty path.
            const serverTitleSet = new Set(serverTitles);
            let extraInMemory = 0;
            for (const r of inMemoryResultsBefore) {
              if (r && typeof r.title === 'string' && !serverTitleSet.has(r.title)) {
                reconstructed.push(r);
                extraInMemory++;
              }
            }

            const lsSets = lsSetsBundle.sets || [];
            const mergedSets = this._sandboxMergeSets(serverSets, this._sandboxMergeSets(inMemorySetsBefore, lsSets));
            const dirtySets = mergedSets.length !== serverSets.length;

            sb.results = reconstructed;
            sb.scoreSets = mergedSets;
            // Defer the activeScoreSet write to the next render tick so
            // the score-set <select> has time to render the <option>
            // children produced by the freshly-assigned scoreSets. x-model
            // captures the select's value before the options exist on the
            // initial render; setting active in the same synchronous tick
            // makes the table filter correctly but leaves the dropdown
            // showing "Show all releases" until the next user interaction.
            const _resolvedActive = this._sandboxResolveActiveSet(mergedSets, sb.activeScoreSet || lsSetsBundle.active);
            if (typeof this.$nextTick === 'function') {
              this.$nextTick(() => { sb.activeScoreSet = _resolvedActive; });
            } else {
              sb.activeScoreSet = _resolvedActive;
            }

            // Background re-parse for any title without a cached record
            // so the UI refreshes with parsed quality + matched CFs as
            // Arr responds.
            if (needParse.length > 0 && sb.instanceId) {
              this._sandboxBatchParseAndMerge(appType, needParse).catch(() => {});
            }

            if (extraInMemory > 0 || dirtySets) this._sandboxPersistAll(appType);
          } else {
            // Server empty and localStorage empty. Keep whatever the
            // user produced in-memory (could be empty too); persist it
            // only if non-empty so we don't write an empty file that
            // would later mask a real localStorage migration on a
            // different browser.
            sb.results = inMemoryResultsBefore;
            sb.scoreSets = inMemorySetsBefore;
            sb.activeScoreSet = this._sandboxResolveActiveSet(inMemorySetsBefore, sb.activeScoreSet);
            if (sb.results.length > 0 || sb.scoreSets.length > 0) {
              this._sandboxPersistAll(appType);
            }
          }
        }
      } catch (e) {
        // eslint-disable-next-line no-console
        console.warn(`[sandbox:${appType}] load merge failed; preserving in-memory state. Cause:`, e);
        sb.results = inMemoryResultsBefore;
        sb.scoreSets = inMemorySetsBefore;
        sb.activeScoreSet = this._sandboxResolveActiveSet(inMemorySetsBefore, sb.activeScoreSet);
      }

      // Re-apply scoring if a profile was already selected before load
      // completed (e.g. user landed on the sandbox tab directly).
      if (sb.profileKey && sb.results.length > 0) {
        try {
          const profileData = await this.fetchProfileScores(sb.profileKey, appType);
          sb.results = sb.results.map(res => this.applyScoring(res, profileData));
        } catch (_) { /* leave results unchanged on scoring fetch failure */ }
      }
    },

    _sandboxReadLocalResults(appType) {
      try {
        const raw = localStorage.getItem('clonarr-sandbox-' + appType);
        if (!raw) return [];
        const data = JSON.parse(raw);
        return Array.isArray(data) ? data : [];
      } catch (_) { return []; }
    },

    _sandboxReadLocalSets(appType) {
      let sets = [];
      let active = '';
      try {
        const raw = localStorage.getItem('clonarr-sandbox-sets-' + appType);
        if (raw) {
          const parsed = JSON.parse(raw);
          // Defensive shape filter: a single null/string entry in the
          // array (from any historical bug, browser-sync glitch, or
          // hand-edit) would otherwise crash the merge path that reads
          // s.id / s.name on every entry. Keep only object entries with
          // a string id so downstream code can trust the shape.
          if (Array.isArray(parsed)) sets = parsed.filter(s => s && typeof s === 'object' && typeof s.id === 'string');
        }
        active = localStorage.getItem('clonarr-sandbox-active-' + appType) || '';
      } catch (_) {}
      return { sets, active };
    },

    // Only return an active-set id when the set still exists in the
    // resolved list — prevents a "filter to nothing" UI state when the
    // active set was deleted on another device or never existed in the
    // merged result.
    // Reconstruct parsed quality + matched CFs for the given titles in
    // a single batch /parse call. Runs in the background after the
    // initial UI render so the user sees their title list immediately
    // and the per-row Quality / CFs / Score cells fill in as Arr
    // responds (~100ms per title). Results merge into sb.results by
    // title — placeholders inserted by the loader get replaced; rows
    // already populated stay put.
    async _sandboxBatchParseAndMerge(appType, titles) {
      const sb = this.sandbox[appType];
      if (!sb || !sb.instanceId || !Array.isArray(titles) || titles.length === 0) return;
      let fresh;
      try {
        const r = await fetch('/api/scoring/parse/batch', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ instanceId: sb.instanceId, titles }),
        });
        if (!r.ok) return;
        fresh = await r.json();
      } catch (_) { return; }
      if (!Array.isArray(fresh)) return;
      let profileData = null;
      if (sb.profileKey) {
        try { profileData = await this.fetchProfileScores(sb.profileKey, appType); }
        catch (_) { /* leave un-scored if profile fetch fails */ }
      }
      const byTitle = new Map();
      for (const r of fresh) {
        if (r && typeof r.title === 'string') byTitle.set(r.title, r);
      }
      sb.results = (sb.results || []).map(res => {
        const refreshed = byTitle.get(res.title);
        if (!refreshed) return res;
        return profileData ? this.applyScoring(refreshed, profileData) : refreshed;
      });
      // Persist so the localStorage cache holds the fresh records for
      // the next instant-render. Server file already has the title
      // strings — this also makes the cache useful on next reload.
      this._sandboxPersistAll(appType);
    },

    _sandboxResolveActiveSet(sets, candidate) {
      if (!candidate) return '';
      return (sets || []).some(s => s.id === candidate) ? candidate : '';
    },

    // Merge two score-set arrays. Primary wins on id-collision; entries
    // from secondary that have a NEW id are appended. Name collisions
    // on appended entries get a "(local)" suffix so a user with two
    // browsers that each created a "Test" set sees both retained
    // instead of one silently absorbing the other. Defensive against
    // malformed entries: anything without a usable id is skipped.
    _sandboxMergeSets(primary, secondary) {
      const out = [];
      const seenIds = new Set();
      const usedNames = new Set();
      for (const s of (primary || [])) {
        if (!s || typeof s !== 'object' || typeof s.id !== 'string') continue;
        if (seenIds.has(s.id)) continue;
        seenIds.add(s.id);
        const name = typeof s.name === 'string' ? s.name : 'Untitled';
        usedNames.add(name.toLowerCase());
        out.push(s);
      }
      for (const s of (secondary || [])) {
        if (!s || typeof s !== 'object' || typeof s.id !== 'string') continue;
        if (seenIds.has(s.id)) continue;
        seenIds.add(s.id);
        let name = typeof s.name === 'string' ? s.name : 'Untitled';
        if (usedNames.has(name.toLowerCase())) name = name + ' (local)';
        usedNames.add(name.toLowerCase());
        out.push({ ...s, name });
      }
      return out;
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
      // Mark CFs already in the profile so they can be hidden from the
      // modal (they belong to the main Edit Scores panel, not the "add"
      // flow). Key by both trashId AND name so the lookup works for
      // TRaSH profiles (CFs carry trashId) and Arr instance profiles
      // (CFs only carry name because Arr's API doesn't store trash_id).
      let inProfileCount = 0;
      for (const s of (sb.editOriginal?.scores || [])) {
        if (s.trashId) inProfile[s.trashId] = true;
        if (s.name) inProfile[s.name] = true;
        inProfileCount++;
      }
      // Pre-check CFs the user has already added via earlier modal
      // sessions so they show as selected on re-open.
      for (const key of Object.keys(sb.editToggles)) {
        if (sb.editToggles[key] === 'added') {
          selected[key] = true;
          scores[key] = sb.editScores[key] ?? 0;
        }
      }
      this.sandboxCFBrowser = { open: true, appType, categories: [], customCFs: [], selected, scores, inProfile, inProfileCount, expanded: {}, filter: '' };
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

    // Apply the modal's selection state to the sandbox edit state.
    // Pure state mutation, no network. Used by both the no-unmatched
    // direct path and the post-Add-to-Arr / Continue-without-adding
    // resume paths so the apply logic stays single-source.
    _applySandboxCFBrowserSelection() {
      const br = this.sandboxCFBrowser;
      const sb = this.sandbox[br.appType];
      if (!sb) return;
      if (!sb._addedCFNames) sb._addedCFNames = {};
      // Remove previously-added CFs that the user has now deselected
      // in this modal session.
      for (const key of Object.keys(sb.editToggles)) {
        if (sb.editToggles[key] === 'added' && !br.selected[key]) {
          delete sb.editToggles[key];
          delete sb.editScores[key];
          delete sb._addedCFNames[key];
        }
      }
      // Resolve display names so the sandbox UI can label the added
      // rows. Both TRaSH catalog (by trashId) and Custom CFs (by id).
      const allCFs = {};
      for (const cat of br.categories) {
        for (const g of cat.groups) {
          for (const cf of g.cfs) { allCFs[cf.trashId] = cf.name; }
        }
      }
      for (const cf of br.customCFs || []) { allCFs[cf.id] = cf.name; }
      // Add the user's newly-selected CFs. Skip any that are already
      // part of the editOriginal profile (those live in the main Edit
      // Scores panel, not the "added" set).
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
    },

    // Returns the names + key payloads for selected CFs that don't yet
    // exist on the sandbox's chosen Arr instance. Used to decide whether
    // to prompt the user to push them first (sandbox scoring needs the
    // CF entity in Arr so the /parse endpoint can match against it).
    async _findUnmatchedSelectedCFsInArr(appType) {
      const sb = this.sandbox[appType];
      const br = this.sandboxCFBrowser;
      if (!sb?.instanceId || !br) return { unmatched: [], trashIds: [], customCFIds: [] };
      let arrCFs = [];
      try {
        const r = await fetch(`/api/instances/${sb.instanceId}/cfs`);
        if (!r.ok) return { unmatched: [], trashIds: [], customCFIds: [] };
        arrCFs = await r.json() || [];
      } catch (_) { return { unmatched: [], trashIds: [], customCFIds: [] }; }
      const arrNames = new Set(arrCFs.map(c => (c.name || '').toLowerCase()));
      // Build a TRaSH-CF lookup (key → {name, trashId}) and a Custom-CF
      // lookup (key → {name, customCFId}) from the modal's catalog so we
      // can return the right ID type for the /api/instances/{id}/cfs/add
      // endpoint, which accepts the two arrays separately.
      const trashByKey = {};
      const customByKey = {};
      for (const cat of br.categories || []) {
        for (const g of cat.groups || []) {
          for (const cf of g.cfs || []) trashByKey[cf.trashId] = cf;
        }
      }
      for (const cf of br.customCFs || []) customByKey[cf.id] = cf;
      const unmatched = [];
      const trashIds = [];
      const customCFIds = [];
      for (const [key, on] of Object.entries(br.selected || {})) {
        if (!on) continue;
        const tcf = trashByKey[key];
        const ccf = customByKey[key];
        const name = tcf?.name || ccf?.name || key;
        if (arrNames.has(name.toLowerCase())) continue;
        unmatched.push(name);
        if (tcf) trashIds.push(tcf.trashId);
        else if (ccf) customCFIds.push(ccf.id);
      }
      return { unmatched, trashIds, customCFIds };
    },

    async closeSandboxCFBrowser() {
      const br = this.sandboxCFBrowser;
      const appType = br.appType;
      const sb = this.sandbox[appType];
      if (!sb) { br.open = false; return; }

      // Sandbox scoring needs every "selected" CF to also exist in the
      // chosen Arr instance, because Arr's /parse endpoint is what
      // produces matchedCFs. A CF you selected but never pushed to Arr
      // will show in profileData.scores but never match a release, so
      // the total stays wrong silently. Detect this here, prompt the
      // user with three actions before applying the selection.
      const { unmatched, trashIds, customCFIds } = await this._findUnmatchedSelectedCFsInArr(appType);
      if (unmatched.length === 0) {
        // Happy path: every selected CF already lives in Arr.
        this._applySandboxCFBrowserSelection();
        br.open = false;
        this.applySandboxEdit(appType);
        return;
      }

      const inst = (this.instances || []).find(i => i.id === sb.instanceId);
      const instName = inst?.name || 'Arr';
      const list = unmatched.map(n => '• ' + n).join('\n');
      const message =
        'These custom formats are not in ' + instName + ' yet, so the sandbox cannot score them against your releases:\n\n' +
        list + '\n\n' +
        'Add them to ' + instName + ' now (custom format entities only, no profile changes), or continue and leave the sandbox count incomplete.';

      this.confirmModal = {
        show: true,
        title: 'Add to Arr first?',
        message,
        confirmLabel: 'Add to ' + instName,
        secondaryLabel: 'Continue without adding',
        cancelLabel: 'Cancel',
        onConfirm: async () => {
          // Push the unmatched CFs to the chosen instance, then re-parse
          // the existing sandbox results so the new Arr CFs surface in
          // matchedCFs. We deliberately ignore "skipped" entries (race
          // where another tab added the same CF) — the re-parse picks
          // up whatever is there.
          try {
            const body = {};
            if (trashIds.length > 0) body.trashIds = trashIds;
            if (customCFIds.length > 0) body.customCFIds = customCFIds;
            const r = await fetch(`/api/instances/${sb.instanceId}/cfs/add`, {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify(body),
            });
            if (!r.ok) {
              this.showToast('Failed to add CFs to ' + instName + '.', 'error', 5000);
              return;
            }
            const result = await r.json();
            const addedN = (result.added || []).length;
            const skippedN = (result.skipped || []).length;
            const failedN = (result.failed || []).length;
            let toastMsg = '';
            if (addedN > 0) toastMsg += addedN + ' added';
            if (skippedN > 0) toastMsg += (toastMsg ? ', ' : '') + skippedN + ' already existed';
            if (failedN > 0) toastMsg += (toastMsg ? ', ' : '') + failedN + ' failed';
            this.showToast(toastMsg + ' on ' + instName + '.', failedN > 0 ? 'error' : 'success', 4500);
            this._applySandboxCFBrowserSelection();
            br.open = false;
            await this._reparseSandboxResults(appType);
            this.applySandboxEdit(appType);
          } catch (e) {
            this.showToast('Network error adding CFs.', 'error', 5000);
          }
        },
        onSecondary: () => {
          // Apply selections without pushing to Arr. The added CFs will
          // show in breakdown but contribute 0 since Arr's /parse will
          // not return them as matched against any release.
          this._applySandboxCFBrowserSelection();
          br.open = false;
          this.applySandboxEdit(appType);
        },
        onCancel: () => {
          // Keep the modal open so the user can review their picks. No
          // state mutation, no close.
        },
      };
    },

    // Re-runs the batch parse against the sandbox instance for the
    // existing pasted titles, then applies the current edit state on
    // top. Used after pushing new CFs to Arr so the resulting matched
    // sets include the newly-available CF entities. No-op when there
    // are no titles to re-parse or no instance is selected.
    async _reparseSandboxResults(appType) {
      const sb = this.sandbox[appType];
      if (!sb?.instanceId || !sb.results?.length) return;
      const titles = sb.results.map(r => r.title).filter(Boolean);
      if (titles.length === 0) return;
      try {
        const r = await fetch('/api/scoring/parse/batch', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ instanceId: sb.instanceId, titles })
        });
        if (!r.ok) return;
        const fresh = await r.json();
        if (!Array.isArray(fresh)) return;
        // Preserve any per-row selection flags (e.g. _selected) that
        // live on the existing rows but aren't part of the parse output.
        const byTitle = {};
        for (const old of sb.results) byTitle[old.title] = old;
        sb.results = fresh.map(res => {
          const old = byTitle[res.title];
          if (old) return { ...old, ...res };
          return res;
        });
      } catch (_) { /* leave existing results in place on error */ }
    },

    sandboxCFBrowserCatAddable(cat) {
      // Number of CFs in this category that are NOT already in the
      // selected profile. Used to hide categories where every CF is
      // already covered by the main Edit Scores panel.
      let n = 0;
      for (const g of cat.groups) {
        for (const cf of g.cfs) {
          const inProf = this.sandboxCFBrowser.inProfile[cf.trashId] || this.sandboxCFBrowser.inProfile[cf.name];
          if (!inProf) n++;
        }
      }
      return n;
    },

    sandboxCFBrowserCatCount(cat) {
      let selected = 0;
      let addable = 0;
      for (const g of cat.groups) {
        for (const cf of g.cfs) {
          const inProf = this.sandboxCFBrowser.inProfile[cf.trashId] || this.sandboxCFBrowser.inProfile[cf.name];
          if (inProf) continue;
          addable++;
          if (this.sandboxCFBrowser.selected[cf.trashId]) selected++;
        }
      }
      return selected + '/' + addable;
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

      // Score matched CFs. Only include CFs that exist in the active
      // profile — the Arr Parse API returns ALL CFs that matched the
      // release in the user's Arr instance (TRaSH ones + user customs
      // + release-group CFs + anything else). Non-profile CFs would
      // show up as 0-score "matched" rows that pollute the breakdown
      // and confuse "what would this profile actually score". Filter
      // them out entirely — if a CF isn't in the profile, the profile
      // wouldn't score it.
      for (const cf of result.matchedCFs) {
        const entry = (cf.trashId && byTrashId[cf.trashId]) || byName[cf.name];
        if (!entry) continue;
        const score = entry.score;
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
