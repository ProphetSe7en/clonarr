// Scoring Generator (preview) - back-solve Custom Format scores from a ranked
// list of release titles. Sandbox-style model: ONE shared pool of added titles
// (persisted) + named sets that are orderings/selections of that pool. Paste
// once, see all titles, build several sets (e.g. one per resolution). Backend:
// internal/core/calculator + /api/calculator/* (stateless calc + per-app doc),
// gated in the UI by Advanced Mode.

export default {
  state: {
    genInstanceId: '',      // instance used to parse titles (quality + matched CFs)
    genPasteText: '',
    genPool: [],            // all added titles {id, title, parsedQuality, matchedCFs}
    genSets: [],            // saved sets [{id, name, order, unwanted, cfOverrides, ignoredCFs}]
    genCurrentSetId: '',    // loaded set id ('' = unsaved working view)
    genTitles: [],          // working ordered list: {id(pool), title, parsedQuality, matchedCFs, partition}
    genOverrides: {},       // cfId -> {type:'unwanted', max:-10000} : CF forced strongly negative
    genIgnored: {},         // cfId -> true : CF excluded from the calculation
    genResult: null,
    genPrevResult: null,
    genIssues: [],          // blocking validation issues from the last calc
    genConflict: null,      // IIS conflict explanation when infeasible
    genGrouping: null,      // {groups:[[id,...]], message} : releases that must share a priority
    genBusy: false,
    genError: '',
    genSnapStep: 5,
    genMinScore: '',        // '' = auto-compute minFormatScore; a number pins it
    genDragSrcId: null,
    genDragOverId: null,
    genShowPool: false,     // pool browser (add already-parsed titles to the working list)
  },
  methods: {
    genArrInstances() {
      return (this.instances || []).filter((i) => i.type === this.activeAppType);
    },
    genNewId(prefix) {
      const rnd = (typeof crypto !== 'undefined' && crypto.randomUUID)
        ? crypto.randomUUID().slice(0, 8)
        : Math.random().toString(36).slice(2, 10);
      return prefix + rnd;
    },

    // --- Document (pool + sets) persistence ---
    async genLoadDoc() {
      try {
        const r = await fetch('/api/calculator/' + this.activeAppType + '/doc');
        if (!r.ok) return;
        const doc = await r.json();
        this.genPool = doc.pool || [];
        this.genSets = doc.sets || [];
      } catch (_) { /* keep current */ }
    },
    async genSaveDoc() {
      try {
        await fetch('/api/calculator/' + this.activeAppType + '/doc', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ appType: this.activeAppType, pool: this.genPool, sets: this.genSets }),
        });
      } catch (_) { /* best effort */ }
    },

    // --- Title pool ---
    async genParseTitles() {
      this.genError = '';
      const lines = (this.genPasteText || '').split('\n').map((s) => s.trim()).filter(Boolean);
      if (!lines.length) { this.genError = 'Paste at least one release title.'; return; }
      if (!this.genInstanceId) { this.genError = 'Pick an instance to parse the titles against.'; return; }
      this.genBusy = true;
      try {
        const r = await fetch('/api/scoring/parse/batch', {
          method: 'POST', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ instanceId: this.genInstanceId, titles: lines }),
        });
        if (!r.ok) { this.genError = 'Could not parse the titles.'; return; }
        const results = (await r.json()) || [];
        for (const it of results) {
          let pool = this.genPool.find((p) => p.title === it.title);
          if (!pool) {
            pool = {
              id: this.genNewId('p'),
              title: it.title,
              parsedQuality: it.parsed?.quality || '',
              matchedCFs: (it.matchedCFs || []).map((c) => ({ trashId: c.trashId || c.name, name: c.name })),
            };
            this.genPool.push(pool);
          }
          if (!this.genTitles.some((t) => t.id === pool.id)) {
            this.genTitles.push({ ...pool, partition: 'wanted', tie: false });
          }
        }
        this.genPasteText = '';
        await this.genSaveDoc();
      } catch (e) {
        this.genError = 'Network error while parsing.';
      } finally {
        this.genBusy = false;
      }
    },
    // Pool titles not already in the working list (for the add-from-pool browser).
    genPoolAvailable() {
      const inWork = new Set(this.genTitles.map((t) => t.id));
      return this.genPool.filter((p) => !inWork.has(p.id));
    },
    genAddFromPool(poolId) {
      const p = this.genPool.find((x) => x.id === poolId);
      if (p && !this.genTitles.some((t) => t.id === p.id)) {
        this.genTitles.push({ ...p, partition: 'wanted', tie: false });
      }
    },

    // Priority position for a row: ties share the number of the run above them.
    // Bounded + optional-chained so a transient index past the array during an
    // x-for re-render (after a reorder/load) can't throw.
    genRowPriority(i) {
      let pri = 1;
      const n = Math.min(i, this.genTitles.length - 1);
      for (let k = 1; k <= n; k++) if (!this.genTitles[k]?.tie) pri++;
      return pri;
    },
    // Where a row sits in an equal-priority (tied) group: 'solo' (not tied),
    // 'start' (top of a group), 'mid', or 'end'. Used to bracket the whole group
    // visually so every tied release is marked, not just the followers.
    genTiePos(i) {
      const cur = !!this.genTitles[i]?.tie;
      const next = !!this.genTitles[i + 1]?.tie;
      if (!cur && !next) return 'solo';
      if (!cur && next) return 'start';
      if (cur && next) return 'mid';
      return 'end';
    },
    genInTie(i) { return this.genTiePos(i) !== 'solo'; },
    // Toggle "same priority as the release above" (no effect on the first row).
    genToggleTie(i) {
      if (i > 0) this.genTitles[i].tie = !this.genTitles[i].tie;
    },
    // Pull a set of releases together (below the first one) and tie the rest
    // into its group, so they share one priority. Mutates genTitles in place.
    _genTieGroup(ids) {
      if (!ids || ids.length < 2) return;
      const set = new Set(ids);
      const picked = this.genTitles.filter((t) => set.has(t.id));
      if (picked.length < 2) return;
      const rest = this.genTitles.filter((t) => !set.has(t.id));
      // Insert the group where the first picked title currently sits.
      const firstPos = this.genTitles.findIndex((t) => set.has(t.id));
      let insertAt = 0;
      for (let k = 0; k < firstPos; k++) if (!set.has(this.genTitles[k].id)) insertAt++;
      picked.forEach((t, idx) => { t.tie = idx > 0; });
      rest.splice(insertAt, 0, ...picked);
      this.genTitles = rest;
    },
    // Make a single set of releases equal priority. Used by the conflict fixer.
    genMakeEqual(ids) {
      this._genTieGroup(ids);
      this.genIssues = [];
      this.genConflict = null;
      this.genGrouping = null;
      this.genError = '';
    },
    // Apply everything the solver computed up front (ties + moves), then
    // recalculate. One click instead of resolving conflicts round-by-round.
    genApplyResolution(res) {
      if (!res) return;
      (res.groups || []).forEach((g) => this._genTieGroup(g));
      (res.moves || []).forEach((m) => this._genApplyMove(m));
      this.genGrouping = null;
      this.genCalc();
    },
    // Reposition one release: pull it out and drop it just below afterId (or to
    // the top when afterId is empty), tying it to its new neighbour if asked.
    _genApplyMove(m) {
      const fromIdx = this.genTitles.findIndex((t) => t.id === m.titleId);
      if (fromIdx < 0) return;
      const [moved] = this.genTitles.splice(fromIdx, 1);
      if (!m.afterId) {
        moved.tie = false; // the top row can't tie to a row above it
        this.genTitles.unshift(moved);
        return;
      }
      const afterIdx = this.genTitles.findIndex((t) => t.id === m.afterId);
      moved.tie = !!m.tie;
      this.genTitles.splice(afterIdx < 0 ? this.genTitles.length : afterIdx + 1, 0, moved);
    },
    // Display name for a single pool id (for the resolution panel).
    genTitleName(id) {
      const t = this.genTitles.find((x) => x.id === id);
      return t ? t.title : id;
    },
    // Display names for a group of pool ids.
    genGroupTitles(ids) {
      return (ids || []).map((id) => this.genTitleName(id));
    },
    genRemoveTitle(i) {
      this.genTitles.splice(i, 1); // working list only; stays in the pool
    },
    async genRemoveFromPool(poolId) {
      this.genPool = this.genPool.filter((p) => p.id !== poolId);
      this.genTitles = this.genTitles.filter((t) => t.id !== poolId);
      for (const s of this.genSets) s.order = (s.order || []).filter((id) => id !== poolId);
      await this.genSaveDoc();
    },

    // --- Drag-to-reorder (working list) ---
    genDragStart(id) { this.genDragSrcId = id; },
    genDragOver(id) { this.genDragOverId = id; },
    genDragEnd() { this.genDragSrcId = null; this.genDragOverId = null; },
    genDrop(targetId) {
      const src = this.genDragSrcId;
      this.genDragSrcId = null; this.genDragOverId = null;
      if (!src || src === targetId) return;
      const from = this.genTitles.findIndex((t) => t.id === src);
      const to = this.genTitles.findIndex((t) => t.id === targetId);
      if (from < 0 || to < 0) return;
      const [moved] = this.genTitles.splice(from, 1);
      this.genTitles.splice(to, 0, moved);
    },

    // --- Per-title / per-CF state ---
    genToggleUnwanted(i) {
      const t = this.genTitles[i];
      t.partition = t.partition === 'unwanted' ? 'wanted' : 'unwanted';
    },
    genIsCFUnwanted(cfId) { return this.genOverrides[cfId]?.type === 'unwanted'; },
    genToggleCFUnwanted(cfId) {
      if (this.genIsCFUnwanted(cfId)) {
        delete this.genOverrides[cfId];
      } else {
        this.genOverrides[cfId] = { type: 'unwanted', max: -10000 };
        delete this.genIgnored[cfId];
      }
    },
    genIsIgnored(cfId) { return !!this.genIgnored[cfId]; },
    genToggleIgnore(cfId) {
      if (this.genIgnored[cfId]) {
        delete this.genIgnored[cfId];
      } else {
        this.genIgnored[cfId] = true;
        delete this.genOverrides[cfId];
      }
    },

    genTitleScore(t) {
      if (!this.genResult?.cfScores) return null;
      let total = 0;
      for (const cf of t.matchedCFs || []) total += this.genResult.cfScores[cf.trashId] || 0;
      return Math.round(total);
    },
    genCFsInUse() {
      const seen = {};
      const out = [];
      for (const t of this.genTitles) {
        for (const cf of t.matchedCFs || []) {
          if (seen[cf.trashId]) continue;
          seen[cf.trashId] = true;
          out.push({ trashId: cf.trashId, name: cf.name });
        }
      }
      out.sort((a, b) => a.name.toLowerCase().localeCompare(b.name.toLowerCase()));
      for (const cf of out) {
        cf.ignored = this.genIsIgnored(cf.trashId);
        cf.unwanted = this.genIsCFUnwanted(cf.trashId);
        if (this.genResult?.cfScores && !cf.ignored) cf.score = this.genResult.cfScores[cf.trashId];
      }
      return out;
    },
    genScoreDiff() {
      if (!this.genResult?.cfScores || !this.genPrevResult?.cfScores) return [];
      const prev = this.genPrevResult.cfScores;
      const cur = this.genResult.cfScores;
      const names = {};
      for (const t of this.genTitles) for (const cf of t.matchedCFs || []) names[cf.trashId] = cf.name;
      const diffs = [];
      for (const id of Object.keys(cur)) {
        const before = prev[id];
        if (before === undefined) diffs.push({ name: names[id] || id, before: null, after: cur[id] });
        else if (before !== cur[id]) diffs.push({ name: names[id] || id, before, after: cur[id] });
      }
      return diffs;
    },

    // --- Calculate (stateless) ---
    async genCalc() {
      this.genError = '';
      this.genIssues = [];
      this.genConflict = null;
      this.genGrouping = null;
      if (!this.genTitles.length) { this.genError = 'Add some release titles first.'; return; }
      this.genBusy = true;
      try {
        const quals = [...new Set(this.genTitles.map((t) => t.parsedQuality))];
        const qualityRanksCache = {};
        for (const q of quals) qualityRanksCache[q] = 0;
        const payload = {
          appType: this.activeAppType,
          qualityRanksCache,
          qualityAllowedCache: quals,
          settings: {
            snapStep: this.genSnapStep || 5,
            scoreBound: 10000,
            minScore: (this.genMinScore === '' || this.genMinScore == null) ? null : Number(this.genMinScore),
          },
          cfOverrides: this.genOverrides,
          ignoredCFs: Object.keys(this.genIgnored),
          titles: this.genTitles.map((t, idx) => ({
            id: t.id, title: t.title, priorityGroup: this.genRowPriority(idx),
            partition: t.partition || 'wanted',
            parsedQuality: t.parsedQuality, matchedCFs: t.matchedCFs,
          })),
        };
        const r = await fetch('/api/calculator/calc', {
          method: 'POST', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
        if (!r.ok) { this.genError = 'Calculation failed.'; return; }
        const result = await r.json();
        if (result.feasible) {
          this.genPrevResult = this.genResult;
          this.genResult = result;
        } else if (result.needsGrouping) {
          // The solver worked out exactly what has to change: releases to tie
          // (identical Custom Formats) and releases to move (out of position).
          // One click applies it all.
          this.genGrouping = {
            groups: result.groups || [],
            moves: result.moves || [],
            message: result.message || '',
          };
          this.genResult = null;
        } else if (result.blocked) {
          // Validation caught it before the solver - surface the actionable issues.
          this.genIssues = (result.issues || []).filter((i) => i.severity === 'error');
          this.genResult = null;
        } else {
          this.genConflict = result.conflict || null;
          this.genResult = null;
        }
      } catch (e) {
        this.genError = 'Network error during calculation.';
      } finally {
        this.genBusy = false;
      }
    },

    // --- Sets (named orderings/selections of the pool) ---
    genSetSnapshot(id, name) {
      return {
        id,
        name,
        order: this.genTitles.map((t) => t.id),
        tied: this.genTitles.filter((t) => t.tie).map((t) => t.id),
        unwanted: this.genTitles.filter((t) => t.partition === 'unwanted').map((t) => t.id),
        cfOverrides: this.genOverrides,
        ignoredCFs: Object.keys(this.genIgnored),
        minScore: (this.genMinScore === '' || this.genMinScore == null) ? null : Number(this.genMinScore),
      };
    },
    genPromptName(title, value) {
      return new Promise((resolve) => {
        this.inputModal = {
          show: true, title, message: 'Name this set so you can reload it later.',
          placeholder: 'Set name', value: value || '', confirmLabel: 'Save',
          onConfirm: (v) => resolve((v || '').trim()), onCancel: () => resolve(''),
        };
      });
    },
    async genSaveSetAs() {
      if (!this.genTitles.length) { this.genError = 'Add some release titles first.'; return; }
      const name = await this.genPromptName('Save set', '');
      if (!name) return;
      const set = this.genSetSnapshot(this.genNewId('s'), name);
      this.genSets.push(set);
      this.genCurrentSetId = set.id;
      await this.genSaveDoc();
    },
    async genSaveSet() {
      if (!this.genCurrentSetId) return this.genSaveSetAs();
      const idx = this.genSets.findIndex((s) => s.id === this.genCurrentSetId);
      if (idx < 0) return this.genSaveSetAs();
      this.genSets[idx] = this.genSetSnapshot(this.genCurrentSetId, this.genSets[idx].name);
      await this.genSaveDoc();
    },
    async genRenameSet() {
      if (!this.genCurrentSetId) { this.genError = 'Load a set first to rename it.'; return; }
      const set = this.genSets.find((s) => s.id === this.genCurrentSetId);
      const name = await this.genPromptName('Rename set', set?.name || '');
      if (!name) return;
      set.name = name;
      this.genSets = [...this.genSets];
      await this.genSaveDoc();
    },
    genLoadSet(id) {
      this.genCurrentSetId = id;
      if (!id) { this.genShowAll(); return; }
      const set = this.genSets.find((s) => s.id === id);
      if (!set) return;
      const unwanted = new Set(set.unwanted || []);
      const tied = new Set(set.tied || []);
      this.genTitles = (set.order || [])
        .map((pid) => this.genPool.find((p) => p.id === pid))
        .filter(Boolean)
        .map((p) => ({ ...p, partition: unwanted.has(p.id) ? 'unwanted' : 'wanted', tie: tied.has(p.id) }));
      this.genOverrides = set.cfOverrides || {};
      this.genIgnored = {};
      for (const c of set.ignoredCFs || []) this.genIgnored[c] = true;
      this.genMinScore = (set.minScore == null) ? '' : set.minScore;
      this.genResult = null; this.genPrevResult = null; this.genError = '';
    },
    async genDeleteSet() {
      if (!this.genCurrentSetId) { this.genError = 'Load a set first to delete it.'; return; }
      const set = this.genSets.find((s) => s.id === this.genCurrentSetId);
      const ok = await new Promise((resolve) => {
        this.confirmModal = {
          show: true, title: 'Delete set?',
          message: 'Delete the saved set "' + (set?.name || '') +
            '"? Your release titles stay in the pool - only this saved ordering is removed.',
          confirmLabel: 'Delete set', onConfirm: () => resolve(true), onCancel: () => resolve(false),
        };
      });
      if (!ok) return;
      this.genSets = this.genSets.filter((s) => s.id !== this.genCurrentSetId);
      this.genCurrentSetId = '';
      await this.genSaveDoc();
    },

    // Show the whole pool as a fresh working list (no set, no overrides).
    genShowAll() {
      this.genCurrentSetId = '';
      this.genTitles = this.genPool.map((p) => ({ ...p, partition: 'wanted', tie: false }));
      this.genOverrides = {};
      this.genIgnored = {};
      this.genMinScore = '';
      this.genResult = null; this.genPrevResult = null; this.genError = '';
      this.genIssues = []; this.genConflict = null; this.genGrouping = null;
    },
    // Empty the working list (pool + saved sets are untouched).
    genClearWorking() {
      this.genTitles = [];
      this.genOverrides = {};
      this.genIgnored = {};
      this.genMinScore = '';
      this.genCurrentSetId = '';
      this.genResult = null; this.genPrevResult = null; this.genError = '';
      this.genIssues = []; this.genConflict = null; this.genGrouping = null;
    },
  },
};
