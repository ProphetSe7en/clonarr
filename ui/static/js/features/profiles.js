import { sanitizeHTML } from '../utils/csrf.js';

export default {
  state: {},
  methods: {
    trashProfileCount(type) {
      return (this.trashProfiles[type] || []).length;
    },

    groupedProfiles(type) {
      // TRaSH convention: sort cards by the `group` integer from profile.json
      // (ascending), then alpha by card name as tiebreak. A card can contain
      // profiles with different group ints (Standard has 1 and 2); use the
      // minimum as the card's sort key so it still lands in the right slot.
      // User-created "Other" profiles without a group int drift to the end.
      const profiles = this.trashProfiles[type] || [];
      const groups = {};
      for (const p of profiles) {
        const g = p.groupName || 'Other';
        if (!groups[g]) groups[g] = { name: g, profiles: [], minGroup: Infinity };
        groups[g].profiles.push(p);
        const gnum = typeof p.group === 'number' ? p.group : Infinity;
        if (gnum < groups[g].minGroup) groups[g].minGroup = gnum;
      }
      // Alpha-sort profiles within each card so order doesn't depend on
      // whatever filesystem read order /api/trash/{app}/profiles happened
      // to return. Matches the CF Group Builder's within-card sort.
      for (const g of Object.values(groups)) {
        g.profiles.sort((a, b) => a.name.localeCompare(b.name));
      }
      return Object.values(groups).sort((a, b) => {
        if (a.minGroup !== b.minGroup) return a.minGroup - b.minGroup;
        return a.name.localeCompare(b.name);
      });
    },

    toggleInstance(id) {
      const opening = !this.expandedInstances[id];
      this.expandedInstances = { ...this.expandedInstances, [id]: opening };
      if (opening) {
        this.loadSyncHistory(id);
        // Auto-load profiles on expand if not already loaded
        if (!this.instProfiles[id]) {
          const inst = this.instances.find(i => i.id === id);
          if (inst) this.loadInstanceProfiles(inst);
        }
      }
    },

    // Profile group collapse/expand
    toggleProfileGroup(instId, groupName) {
      const key = instId + ':' + groupName;
      this.expandedProfileGroups = { ...this.expandedProfileGroups, [key]: !this.expandedProfileGroups[key] };
    },

    isProfileGroupExpanded(instId, groupName) {
      const key = instId + ':' + groupName;
      return !!this.expandedProfileGroups[key];
    },

    toggleQSExpanded(instId) {
      this.qsExpanded = { ...this.qsExpanded, [instId]: !this.qsExpanded[instId] };
    },

    // --- Notifications ---

    getAllSelectedCFIds() {
      const ids = this.getSelectedCFIds();
      const sel = this.selectedOptionalCFs || {};
      // Skip extras the user has explicitly toggled off (sel[tid]===false).
      // Without this filter, toggling off an Additional CF that was opted
      // in earlier (and so carries a score in extraCFs) silently re-adds
      // it to selectedCFs via the unconditional append — visible as the
      // CF "reactivating itself" on every reopen. Mirror filter lives in
      // buildSyncBody's scoreOverrides loop so the rule's scoreOverride
      // entry for the same CF also drops out of the persisted payload.
      const extraIds = Object.keys(this.extraCFs).filter(tid =>
        !this._isOrphanCustomTrashId(tid) && sel[tid] !== false
      );
      return extraIds.length > 0 ? [...ids, ...extraIds] : ids;
    },

    // Trash defaults: the set of CF trash_ids the TRaSH-Guides repository
    // currently considers part of this profile. Sum of:
    //   - profile.formatItems CFs not categorized into any group (these
    //     ship to the frontend as detail.formatItemNames)
    //   - CFs in default-on groups (group.defaultEnabled === true) that are
    //     either required (cf.required) or default-on within the group
    //     (cf.default === true)
    //
    // Mirrors backend's ComputeTrashDefaults so editor-side decisions
    // (which list a deselected CF goes into: selectedCFs as opt-in or
    // excludedCFs as opt-out) line up with the sync engine's view of the
    // same profile.
    computeTrashDefaults() {
      const defaults = new Set();
      const detail = this.profileDetail?.detail;
      if (!detail) return defaults;
      for (const fi of (detail.formatItemNames || [])) {
        if (fi.trashId) defaults.add(fi.trashId);
      }
      for (const group of (detail.trashGroups || [])) {
        if (!group.defaultEnabled) continue;
        for (const cf of (group.cfs || [])) {
          if (cf.required || cf.default) defaults.add(cf.trashId);
        }
      }
      return defaults;
    },

    // Excluded CFs: trash_ids the user has opted OUT of from the TRaSH
    // defaults set. Sent alongside selectedCFs in the sync payload so
    // the engine zeroes them in the Arr profile even when they were
    // previously synced.
    //
    // Two sources:
    //   1. Phase 2c lock-clicks — sel[tid] === false on a CF that's in
    //      TRaSH defaults (spToggleRequiredCF writes this).
    //   2. Group-level opt-out — when the user disables a default-on
    //      group via pdToggleGroup, the group flag flips to false but
    //      per-CF state stays untouched (preserves Phase 2c
    //      exclusions). Without this branch, backend would see the
    //      group's required+default CFs missing from selectedCFs but
    //      not in excludedCFs either — it treats that as "still
    //      default-on" and never zeroes them in the Arr profile.
    //
    // Mirror in restoreFromSyncHistory (~line 803): the rehydrate
    // heuristic "all required+default CFs of a default-on group are
    // excluded → group flag is false" must stay in sync with what we
    // emit here.
    getExcludedCFIds() {
      const sel = this.selectedOptionalCFs || {};
      const excluded = new Set();
      // FormatItem-level Phase 2c locks (CFs not in any group).
      const formatItems = this.profileDetail?.detail?.formatItemNames || [];
      for (const fi of formatItems) {
        if (sel[fi.trashId] === false) excluded.add(fi.trashId);
      }
      // Group-level handling — branch on whether the group is currently
      // active in the user's selection (defaultEnabled if no override).
      const groups = this.profileDetail?.detail?.trashGroups || [];
      for (const group of groups) {
        const flag = sel['__grp_' + group.name];
        const grpOn = flag === undefined ? group.defaultEnabled : flag;
        if (!grpOn) {
          // Group off. For default-on groups we MUST emit required+
          // default CFs so backend zeros them out of the existing Arr
          // profile (otherwise trash_defaults brings them back and the
          // disable signal is lost). Default-off groups that are off
          // are the default state — defaults don't include them, so
          // nothing to subtract.
          if (!group.defaultEnabled) continue;
          for (const cf of (group.cfs || [])) {
            if (cf.required || cf.default) excluded.add(cf.trashId);
          }
        } else {
          // Group on. Surface Phase 2c lock-clicks on required+default
          // CFs (sel[cf]===false). Covers both default-on lock-clicks
          // AND default-off opt-in lock-clicks (the latter aren't in
          // computeTrashDefaults so the formatItems loop misses them).
          // Set dedupes if the CF is already added via the defaults
          // first-loop or the formatItems loop above.
          for (const cf of (group.cfs || [])) {
            if (!(cf.required || cf.default)) continue;
            if (sel[cf.trashId] === false) excluded.add(cf.trashId);
          }
        }
      }
      return [...excluded];
    },

    // True when tid is a custom: ID that no longer resolves to a live
    // custom CF. Used to strip dead refs from sync payloads + post-sync
    // rule persistence so backend doesn't have to clean up after us.
    //
    // GUARDED: only returns true when extraCFAllCFs is populated. Empty
    // means /api/trash/{app}/all-cfs returned nothing (TRaSH cache empty
    // during Reset window — backend short-circuits and drops customs from
    // the response). We can't distinguish "deleted" from "not loaded yet"
    // in that state, so we leave the IDs alone and let backend cleanup
    // catch them post-sync. Worst case in the Reset window: orphan ref
    // round-trips one extra time — same as before this fix.
    _isOrphanCustomTrashId(tid) {
      if (!tid || !tid.startsWith('custom:')) return false;
      const all = this.extraCFAllCFs || [];
      if (all.length === 0) return false; // unknown — don't strip
      for (const cf of all) {
        if (cf.trashId === tid) return false; // resolved — alive
      }
      return true; // custom: prefix + customs loaded + no match = dead
    },

    resolveCFName(tid) {
      const detail = this.profileDetail?.detail;
      if (!detail) return tid.substring(0, 12);
      for (const fi of (detail.formatItemNames || [])) {
        if (fi.trashId === tid) return fi.name;
      }
      for (const g of (detail.trashGroups || [])) {
        for (const cf of g.cfs) {
          if (cf.trashId === tid) return cf.name;
        }
      }
      // Fallback: extras and other CFs loaded via /all-cfs (Extra CFs picker list)
      for (const cf of (this.extraCFAllCFs || [])) {
        if (cf.trashId === tid) return cf.name;
      }
      return tid.replace(/^custom:/, '').substring(0, 12);
    },

    resolveCFDefaultScore(tid) {
      const detail = this.profileDetail?.detail;
      if (!detail) return '?';
      for (const fi of (detail.formatItemNames || [])) {
        if (fi.trashId === tid) return fi.score ?? 0;
      }
      for (const g of (detail.trashGroups || [])) {
        for (const cf of g.cfs) {
          if (cf.trashId === tid) return cf.score ?? 0;
        }
      }
      // Fallback: extras — resolve default score from CF's trashScores map using current score set
      const scoreSet = detail.scoreCtx || detail.profile?.trashScoreSet || 'default';
      for (const cf of (this.extraCFAllCFs || [])) {
        if (cf.trashId === tid) {
          return cf.trashScores?.[scoreSet] ?? cf.trashScores?.default ?? 0;
        }
      }
      return '?';
    },

    resolveExtraCFName(tid) {
      // Fallback name resolver for extra CFs not found in extraCFAllCFs.
      // Checks instance CF names (for CFs synced to Arr but not in TRaSH groups).
      if (this._extraCFNameCache?.[tid]) return this._extraCFNameCache[tid];
      const instCFs = this.cleanupCFNames || [];
      // Custom CFs: try to find by partial ID match in extraCFAllCFs or instance
      for (const cf of (this.extraCFAllCFs || [])) {
        if (cf.trashId === tid) { return cf.name; }
      }
      return tid.replace(/^custom:/, '').substring(0, 12);
    },

    async loadExtraCFList(appType) {
      // Default to the open editor's app type so existing callers keep
      // working (most flows have profileDetail already set when they
      // reach here). Callers that run BEFORE openProfileDetail — like
      // Compare → Edit & Sync's pre-flight check for Arr-only CFs —
      // pass the appType explicitly.
      const t = appType || this.profileDetail?.instance?.type;
      if (!t) return;
      // Cache hit: same Arr type as the populated catalog → skip the
      // network round-trip. extraCFGroups is the heavy /all-cfs payload
      // (every TRaSH CF organised into picker groups); without this
      // gate every profile-detail open refetches the same data.
      // Invalidated by loadCFBrowse (hit by save/delete/import of any
      // custom CF + by pullTrash), and by clearTrashDerivedCaches
      // (hit by resetTrashData). Together those cover every path that
      // could change the underlying catalog.
      if (this._extraCFGroupsCachedType === t && Array.isArray(this.extraCFGroups) && this.extraCFGroups.length > 0) {
        return;
      }
      try {
        const r = await fetch(`/api/trash/${t}/all-cfs`);
        if (!r.ok) return;
        const d = await r.json();
        // Build grouped + ungrouped lists. Three buckets:
        //   1. TRaSH cf-groups (have groupTrashId) → push verbatim
        //   2. TRaSH CFs not in any group → roll into a single "Other"
        //      group (pushed before Custom so OTHER section sorts above
        //      CUSTOM in Sync Preview's sub-nav)
        //   3. Custom-category groups (g.isCustom + parent category
        //      "Custom") → push each user-cat as its own group, named
        //      "[Custom] <user-cat>" so spGroupBySection clusters them
        //      under a CUSTOM section in Sync Preview's sub-nav. This
        //      matches the Custom Formats browse view's sidebar nesting.
        const groups = []; // { name, category, cfs[] }
        const ungrouped = [];
        const customGroups = [];
        for (const c of (d.categories || [])) for (const g of c.groups) {
          if (g.groupTrashId) {
            groups.push({ name: g.name, category: c.category, trashDescription: g.trashDescription, cfs: g.cfs });
          } else if (g.isCustom && c.category === 'Custom') {
            if ((g.cfs || []).length > 0) {
              customGroups.push({
                name: `[Custom] ${g.name}`,
                category: 'Custom',
                cfs: g.cfs,
              });
            }
          } else {
            for (const cf of g.cfs) ungrouped.push(cf);
          }
        }
        // Order matters — spGroupBySection preserves input order and
        // the sub-nav renders sections top-to-bottom in that order.
        // Other above Custom by request.
        if (ungrouped.length > 0) groups.push({ name: 'Other', category: 'Other', cfs: ungrouped });
        groups.push(...customGroups);
        this.extraCFGroups = groups;
        // Ensure all groups start collapsed. Alpine's reactive proxy treats
        // missing keys as truthy in some cases AND direct keyed mutation
        // doesn't reliably re-trigger every dependent expression — use the
        // object-spread pattern so x-show/:class re-evaluate uniformly.
        const updatedSections = { ...this.detailSections };
        for (const g of groups) updatedSections['extra_' + g.name] = false;
        this.detailSections = updatedSections;
        // Flat list for filtering
        const all = [];
        for (const g of groups) for (const cf of g.cfs) all.push(cf);
        this.extraCFAllCFs = all;
        // Record which Arr type this catalog is for so the next
        // openProfileDetail can hit the cache. Reset alongside the
        // groups in pdResetDetailState / clearTrashDerivedCaches.
        this._extraCFGroupsCachedType = t;
      } catch (e) { console.error('loadExtraCFs:', e); }
    },

    _extraInProfile(trashId) {
      if (!this._extraInProfileSet) {
        this._extraInProfileSet = new Set();
        for (const fi of (this.profileDetail?.detail?.formatItemNames || [])) this._extraInProfileSet.add(fi.trashId);
        for (const g of (this.profileDetail?.detail?.trashGroups || [])) for (const cf of g.cfs) this._extraInProfileSet.add(cf.trashId);
      }
      return this._extraInProfileSet.has(trashId);
    },

    extraCFAvailable() {
      const q = (this.extraCFSearch || '').toLowerCase();
      const inProfile = new Set();
      for (const fi of (this.profileDetail?.detail?.formatItemNames || [])) inProfile.add(fi.trashId);
      for (const g of (this.profileDetail?.detail?.trashGroups || [])) {
        for (const cf of g.cfs) inProfile.add(cf.trashId);
      }
      return this.extraCFAllCFs.filter(cf =>
        !inProfile.has(cf.trashId) && !this.extraCFs[cf.trashId] && (!q || cf.name.toLowerCase().includes(q))
      );
    },

    // Counter for the Additional CFs picker group header — inline IIFE on
    // x-text didn't reliably re-trigger Alpine reactivity when extraCFs
    // mutated (counter stayed at 0/N after toggling). Method-form forces
    // a re-evaluation per render. Returns { total, added, overridden } —
    // overridden counts added CFs whose current score differs from the
    // profile's score-set default.
    pdExtraGroupCount(group) {
      const cfs = (group?.cfs || []).filter(cf => !this._extraInProfile(cf.trashId));
      let added = 0, overridden = 0;
      for (const cf of cfs) {
        const cur = this.extraCFs[cf.trashId];
        if (cur === undefined) continue;
        added++;
        if (cur !== this.pdExtraCFScore(cf)) overridden++;
      }
      return { total: cfs.length, added, overridden };
    },

    // Resolve the score that this CF would be added at — current profile's
    // score-set falling back to default. Used by picker rows and the
    // pdToggleAdditionalGroup helper. Returns 0 when no score is defined.
    pdExtraCFScore(cf) {
      const scoreSet = this.profileDetail?.detail?.profile?.trashScoreSet || 'default';
      return cf?.trashScores?.[scoreSet] ?? cf?.trashScores?.default ?? 0;
    },

    // Group-level toggle for the Additional CFs picker. Mirrors the per-group
    // toggle in the main Groups section: when checked, every selectable CF in
    // the group is added to extraCFs at its current-score-set default; when
    // unchecked, every CF in the group is removed from extraCFs. CFs already
    // in the underlying Arr profile are skipped both ways since they aren't
    // addable as extras to begin with.
    pdToggleAdditionalGroup(group, on) {
      const scoreSet = this.profileDetail?.detail?.profile?.trashScoreSet || 'default';
      const next = { ...this.extraCFs };
      for (const cf of (group.cfs || [])) {
        if (this._extraInProfile(cf.trashId)) continue;
        if (on) {
          next[cf.trashId] = cf.trashScores?.[scoreSet] ?? cf.trashScores?.default ?? 0;
        } else {
          delete next[cf.trashId];
        }
      }
      this.extraCFs = next;
    },

    async toggleAdvancedMode(enable) {
      if (enable) {
        const ok = await new Promise(resolve => {
          this.confirmModal = {
            show: true,
            title: 'Enable Advanced Mode',
            html: true,
            message: 'Advanced Mode enables tools for power users and guide contributors:<br><br>• Profile Builder — create custom profiles with fixed scores <span style="color:var(--accent-red);font-weight:600">(no auto-sync — scores will NOT update when TRaSH Guides change)</span><br>• Scoring Sandbox — test how releases score against profiles<br>• TRaSH JSON export — for contributing to TRaSH Guides<br><br><strong style="color:var(--accent-orange);font-size:14px">Most users don\'t need this.</strong> TRaSH Sync handles profiles, scores, and updates automatically. Only enable Advanced Mode if you have a specific need that TRaSH Sync doesn\'t cover.<br><br>Enable Advanced Mode?',
            onConfirm: () => resolve(true),
            onCancel: () => resolve(false)
          };
        });
        if (!ok) return;
      }
      this.config.devMode = enable;
      this.saveConfig(['devMode']);
    },

    setUIScale(value) {
      this.uiScale = value;
      localStorage.setItem('clonarr-ui-scale', value);
      if (CSS.supports('zoom', '1')) document.documentElement.style.zoom = value;
    },

    setTheme(value) {
      this.theme = value;
      localStorage.setItem('clonarr-theme', value);
      this.applyTheme();
    },

    applyTheme() {
      const resolved = this.theme === 'system'
        ? (matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark')
        : this.theme;
      document.documentElement.setAttribute('data-theme', resolved);
    },

    // v3 content-alignment toggle — applies immediately, persists to
    // localStorage. CSS rule in layout.css reads [data-content-align="left"]
    // on the x-data wrapper and removes the centering margin.
    setContentAlign(value) {
      this.contentAlign = (value === 'left') ? 'left' : 'center';
      localStorage.setItem('clonarr-content-align', this.contentAlign);
    },

    // v3 navigation-style toggle — applies immediately, persists to
    // localStorage. Switches between sidebar (default) and topnav (classic
    // horizontal bar with v3 color treatment). The x-if templates in
    // index.html swap the layout in/out; CSS rule in layout.css reads
    // [data-nav-style="topnav"] on the x-data wrapper to suppress the
    // sidebar grid + collapse it back to a vertical stack.
    setNavStyle(value) {
      this.navStyle = (value === 'topnav') ? 'topnav' : 'sidebar';
      localStorage.setItem('clonarr-nav-style', this.navStyle);
      // Close the sub-nav popup if it was open from the sidebar.
      this.sidebarSubnavPopup = '';
      // Reset sidebar to expanded on every nav-style switch. Without
      // this, a user who collapses the sidebar, switches to topnav,
      // then switches back finds an unexpectedly-collapsed sidebar
      // (the collapsed flag persists in localStorage).
      if (this.sidebarCollapsed) {
        this.sidebarCollapsed = false;
        localStorage.setItem('clonarr-sidebar-collapsed', '0');
      }
    },

    async checkCleanupEvents() {
      try {
        const r = await fetch('/api/cleanup-events');
        if (!r.ok) return;
        const events = await r.json();
        if (events.length === 0) return;
        // Coalesce per instance: bulk deletions in Arr would otherwise spawn
        // one toast per affected profile. The toast manager keeps the compact
        // preview small and lets the full profile list expand inline.
        const byInstance = {};
        for (const ev of events) {
          (byInstance[ev.instanceName] = byInstance[ev.instanceName] || []).push(ev.profileName);
        }
        for (const [instanceName, names] of Object.entries(byInstance)) {
          if (names.length === 1) {
            this.showToast({
              title: `"${names[0]}" deleted`,
              message: `Sync rule removed from ${instanceName}.`,
              type: 'warning',
              duration: 6000,
              key: this.toastKey('cleanup', instanceName, names[0]),
            });
          } else {
            this.showToast({
              title: `${names.length} profiles deleted in ${instanceName}`,
              message: 'Sync rules removed.',
              details: names.map(n => `"${n}"`),
              type: 'warning',
              duration: 8000,
              key: this.toastKey(
                'cleanup', instanceName, names.length, names[0], names[names.length - 1], names
              ),
            });
          }
        }
      } catch (e) { /* ignore */ }
    },

    async checkAutoSyncEvents() {
      try {
        const r = await fetch('/api/auto-sync/events');
        if (!r.ok) return;
        const events = await r.json();
        const profileLabel = (ev) => ev.arrProfileName && ev.arrProfileName !== ev.profileName
          ? `${ev.profileName} -> ${ev.arrProfileName}` : ev.profileName;
        const groups = {};
        for (const ev of events) {
          const severity = ev.error ? 'error' : 'info';
          const key = `${severity}:${ev.instanceName}`;
          (groups[key] = groups[key] || { severity, instanceName: ev.instanceName, events: [] }).events.push(ev);
        }
        for (const group of Object.values(groups)) {
          if (group.severity === 'error') {
            if (group.events.length === 1) {
              const ev = group.events[0];
              this.showToast({
                title: `Auto-sync failed: ${group.instanceName}`,
                message: `${profileLabel(ev)}: ${ev.error}`,
                type: 'error',
                duration: 8000,
                key: this.toastKey(
                  'autosync', 'error', group.instanceName, ev.profileName, ev.timestamp || ev.error
                ),
              });
            } else {
              const latestTimestamp = group.events.reduce((latest, ev) => ev.timestamp && ev.timestamp > latest ? ev.timestamp : latest, '');
              this.showToast({
                title: `Auto-sync failed: ${group.instanceName}`,
                message: `${group.events.length} profiles failed.`,
                details: group.events.map(ev => `${profileLabel(ev)}: ${ev.error}`),
                type: 'error',
                duration: 10000,
                key: this.toastKey(
                  'autosync',
                  'error',
                  group.instanceName,
                  group.events.length,
                  latestTimestamp,
                  group.events.map(ev => [ev.profileName, ev.error])
                ),
              });
            }
          } else if (group.events.length === 1) {
            const ev = group.events[0];
            this.showToast({
              title: `Auto-sync: ${group.instanceName}`,
              message: `"${profileLabel(ev)}"`,
              details: ev.details || [],
              type: 'info',
              duration: 8000,
              key: this.toastKey(
                'autosync', 'info', group.instanceName, ev.profileName, ev.timestamp || ''
              ),
            });
          } else {
            const latestTimestamp = group.events.reduce((latest, ev) => ev.timestamp && ev.timestamp > latest ? ev.timestamp : latest, '');
            const details = group.events.flatMap(ev => [
              `"${profileLabel(ev)}"`,
              ...((ev.details || []).map(detail => `  ${detail}`)),
            ]);
            this.showToast({
              title: `Auto-sync: ${group.instanceName}`,
              message: `${group.events.length} profiles updated.`,
              details,
              type: 'info',
              duration: 10000,
              key: this.toastKey(
                'autosync',
                'info',
                group.instanceName,
                group.events.length,
                latestTimestamp,
                group.events.map(ev => ev.profileName)
              ),
            });
          }
        }
        // Reload sync history if any events came through
        if (events.length > 0) {
          for (const inst of this.instances) {
            await this.loadSyncHistory(inst.id);
          }
        }
      } catch (e) { /* ignore */ }
    },

    // Find the instance that has sync history for an imported profile
    findSyncedInstance(appType, importedProfileId) {
      for (const inst of this.instancesOfType(appType)) {
        const history = (this.syncHistory[inst.id] || []).find(h => h.importedProfileId === importedProfileId);
        if (history) return { inst, history };
      }
      // Fallback to first instance
      const inst = this.instancesOfType(appType)[0];
      return inst ? { inst, history: null } : null;
    },

    // --- TRaSH ---

    async pullTrash() {
      this.pulling = true;
      const prevCommit = this.trashStatus?.commitHash || '';
      try {
        const r = await fetch('/api/trash/pull', { method: 'POST' });
        if (!r.ok) { this.pulling = false; this.showToast('Pull failed', 'error'); return; }
        const poll = setInterval(async () => {
          await this.loadTrashStatus();
          if (!this.trashStatus.pulling) {
            clearInterval(poll);
            this.pulling = false;
            // Show toast based on pull result
            const radarrCFs = this.trashStatus.radarrCFs || 0;
            const sonarrCFs = this.trashStatus.sonarrCFs || 0;
            const radarrProfs = this.trashStatus.radarrProfiles || 0;
            const sonarrProfs = this.trashStatus.sonarrProfiles || 0;
            const totalLoaded = radarrCFs + sonarrCFs + radarrProfs + sonarrProfs;
            if (this.trashStatus.pullError) {
              this.showToast('Pull failed: ' + this.trashStatus.pullError, 'error');
            } else if (totalLoaded === 0) {
              // Git fetch succeeded but the parser found nothing — typically a
              // path/permissions issue with /config/data/trash-guides. Surfacing
              // this as a warning instead of a misleading "success" toast.
              this.showToast('Pull completed but no TRaSH data was loaded (0 CFs, 0 profiles). Check container logs and that /config/data/trash-guides/docs/json/ exists and is readable by the container user.', 'warning', 12000);
            } else if (this.trashStatus.commitHash !== prevCommit && this.trashStatus.lastDiff?.summary) {
              // Keep the diff's line structure — toast-content is pre-line so
              // each app + CF renders on its own row. Only strip markdown bold.
              const summary = this.trashStatus.lastDiff.summary.replace(/\*\*/g, '').trim();
              this.showToast('TRaSH updated:\n' + summary, 'info', 10000);
            } else {
              this.showToast('TRaSH data up to date', 'info', 3000);
            }
            this.loadTrashProfiles('radarr');
            this.loadTrashProfiles('sonarr');
            this.loadTrashProfileDescriptions('radarr');
            this.loadTrashProfileDescriptions('sonarr');
            this.loadQualitySizes('radarr');
            this.loadQualitySizes('sonarr');
            this.loadCFBrowse('radarr');
            this.loadCFBrowse('sonarr');
            this.loadConflicts('radarr');
            this.loadConflicts('sonarr');
            this.loadNaming('radarr');
            this.loadNaming('sonarr');
            // Reload sync data and check for cleanup events
            await this.loadAutoSyncRules();
            for (const inst of this.instances) {
              await this.loadSyncHistory(inst.id);
            }
            this.checkCleanupEvents();
            // Delay auto-sync event check — auto-sync runs async after pull completes
            setTimeout(() => this.checkAutoSyncEvents(), 5000);
          }
        }, 2000);
        setTimeout(() => { clearInterval(poll); this.pulling = false; }, 120000);
      } catch (e) {
        this.pulling = false;
        this.showToast('Pull failed: ' + e.message, 'error');
      }
    },

    resetTrashData() {
      if (this.trashResetting || this.trashStatus?.pulling || this.pulling || this.syncing) return;
      this.confirmModal = {
        show: true,
        title: 'Reset TRaSH Data',
        message: 'Delete the local TRaSH Guides cache and pull metadata?\n\nUser settings, instances, sync rules, custom profiles, and custom CFs are not deleted.\n\nAfter reset, Pull downloads a fresh TRaSH cache. If it lands on the same TRaSH commit your rules already synced, Pull will not force Arr profiles to resync. Use Update all after Pull if you want to force an Arr refresh.',
        confirmLabel: 'Reset data',
        onConfirm: () => this._resetTrashDataConfirmed(),
      };
    },

    async _resetTrashDataConfirmed() {
      if (this.trashResetting) return;
      this.trashResetting = true;
      try {
        const r = await fetch('/api/trash/reset', { method: 'POST' });
        if (!r.ok) {
          let msg = 'Failed to reset TRaSH data';
          try {
            const data = await r.json();
            if (data?.error) msg = data.error;
          } catch {}
          this.showToast(msg, 'error', 6000);
          return;
        }
        await this.loadTrashStatus();
        this.clearTrashDerivedCaches();
        this.showToast('TRaSH data reset. Pull downloads fresh data; use Update all after Pull for a forced Arr refresh.', 'info', 7000);
      } catch (e) {
        this.showToast('Failed to reset TRaSH data: ' + e.message, 'error', 6000);
      } finally {
        this.trashResetting = false;
      }
    },

    clearTrashDerivedCaches() {
      this.trashProfiles = { radarr: [], sonarr: [] };
      this.qualitySizesPerApp = {};
      this.cfBrowseData = {};
      this.conflictsData = {};
      this.namingData = {};

      this.pbCategories = [];
      this.pbScoreSets = [];
      this.pbQualityPresets = [];

      this.extraCFAllCFs = [];
      this.extraCFGroups = [];
      // Invalidate /all-cfs cache marker so the next openProfileDetail
      // refetches fresh after Pull/Reset.
      this._extraCFGroupsCachedType = null;
      this._extraInProfileSet = null;

      this.cfgbCFs = [];
      this.cfgbGroups = [];
      this.cfgbProfiles = [];
      this.cfgbTrashCFGroups = [];
      this.cfgbHasCustom = false;
      this.cfgbUngroupedTrashCount = 0;
      this.cfgbUngroupedRemainingCount = 0;
      this.cfgbLoadError = '';
      this._cfgbTrashFlat = [];
      this._cfgbTrashGroupMap = new Map();
      this._cfgbTrashHasCustom = false;
      this._cfgbLoadFor = '';

      this._sandboxCFCache = {};
      this._trashScoreContextCache = {};
      this.profileDetail = null;
      this.profileBuilder = false;
      this.sandboxCFBrowser = {
        open: false,
        appType: '',
        categories: [],
        customCFs: [],
        selected: {},
        scores: {},
        expanded: {},
        filter: '',
      };
      this.showChangelog = false;
    },

    // --- Profile Detail ---

    // restoreFromRule controls auto-restore from existing sync rules:
    //   false (default) — fresh TRaSH defaults. Use when user clicks a TRaSH
    //     guide profile from Standard/German/French/Anime/SQP cards. Browse
    //     mode: profile detail shows what TRaSH spec defines, not what an
    //     existing rule may have customized.
    //   true            — auto-restore from matching rule (selectedOptionalCFs
    //     reconstructed, scoreOverrides + qualityStructure + overrides loaded,
    //     edit-session lock set to that Arr profile). Use when user explicitly
    //     opens an existing rule (Edit pencil from Sync rules card,
    //     resyncProfile, Compare → Edit & Sync, post-sync land-on-profile).
    //
    // Earlier versions auto-restored unconditionally when matchingRules.length
    // === 1. That broke the "create another sibling profile from the same
    // TRaSH guide" workflow — clicking a guide profile would silently load
    // the existing rule's customizations and lock Save & Sync to the
    // existing Arr profile, instead of starting fresh.
    async openProfileDetail(inst, profile, restoreFromRule = false) {
      this.debugLog('UI', `Profile opened: "${profile.name}" on ${inst.name} (restoreFromRule=${restoreFromRule})`);
      this.syncPlan = null;
      this.syncResult = null;
      this.selectedOptionalCFs = {};
      // Reset the CF search filter so a new profile doesn't open looking
      // empty because a previous edit-session left a filter active.
      // Without this, opening profile B after searching "flux" on profile
      // A renders B's editor with the search still applied, hiding rows
      // that don't match.
      this.spSearchFilter = '';

      this.profileDetail = { instance: inst, profile: profile, detail: null };
      // Pre-load languages and quality presets for this instance (for override dropdowns)
      this.getLanguagesForInstance(inst.id);
      if (!this.pbQualityPresets.length) {
        fetch(`/api/trash/${inst.type}/quality-presets`).then(r => r.ok ? r.json() : []).then(d => this.pbQualityPresets = d || []).catch(() => {});
      }
      // Fire /all-cfs in parallel with the profile fetch. It's the
      // heavy catalog the Additional CF picker + Diffs view rely on,
      // and there's no dependency between the two requests — the
      // previous sequential ordering (profile → applyRuleStateToEditor
      // → loadExtraCFList) just cost a full round-trip's worth of
      // latency. loadExtraCFList is idempotent + cache-gated by Arr
      // type so the subsequent call from resyncProfile / applyRuleState
      // becomes a no-op when the catalog already arrived.
      this.loadExtraCFList();
      try {
        const r = await fetch(`/api/trash/${inst.type}/profiles/${profile.trashId}`);
        if (!r.ok) {
          // Surface the backend error so the user knows what to do
          // (TRaSH data empty → run Pull, profile missing → upstream
          // change). Silent console.error left the click feeling broken.
          let msg = 'Could not open profile';
          try { const data = await r.json(); if (data?.error) msg = data.error; } catch (_) {}
          this.showToast(msg, 'error', 8000);
          // Reset the half-loaded state so the overlay doesn't show a
          // stale shell — return to the profile list.
          this.profileDetail = null;
          console.error('loadProfileDetail: HTTP', r.status);
          return;
        }
        const detail = await r.json();
        this.profileDetail = { ...this.profileDetail, detail: detail };
        this.initDetailSections(detail);
        this.initSelectedCFs(detail);
        // Reset profile-detail override state on every load so stale state from a prior
        // profile doesn't leak. pdInitOverrides then seeds pdOverrides from the new profile's
        // defaults; the rule-restore branch below re-enables overrides if persisted.
        this.pdResetDetailState();
        this.pdInitOverrides(detail.profile || null);
        // Auto-restore is opt-in via restoreFromRule. Only fires when the
        // caller explicitly asked AND there's exactly one matching rule.
        // Multiple rules for the same TRaSH profile (different Arr
        // profiles) are not auto-restored — ambiguous which to load. User
        // reaches the specific rule via Sync rules card's Edit button.
        if (restoreFromRule) {
          const matchingRules = (this.autoSyncRules || []).filter(rl =>
            rl.instanceId === inst.id &&
            rl.trashProfileId === profile.trashId &&
            !rl.orphanedAt
          );
          if (matchingRules.length === 1) {
            this.applyRuleStateToEditor(matchingRules[0], detail);
          }
        }
        // Issue #52 — initial baseline. resyncProfile re-captures this
        // after its own restore pass; for the create-new path (TPD Use
        // this profile, no resyncProfile follow-up) this is the only
        // capture point.
        this._captureProfileBaseline();
      } catch (e) { console.error('loadProfileDetail:', e); }
    },

    // Mirror the rule's persisted state into the profile-detail editor's
    // working maps. Called from openProfileDetail when exactly one rule
    // exists for the (instance, TRaSH profile) pair, so the editor opens
    // showing the user's actual current sync configuration rather than
    // TRaSH defaults. Same restoration shape as resyncProfile's inline
    // logic but operates directly on rule data (no sync-history hop).
    applyRuleStateToEditor(rule, detail) {
      if (!rule || !detail) return;
      // Lock subsequent Save & Sync to this Arr profile.
      this.profileDetail._arrProfileName = this.resolveArrProfileName(rule.instanceId, rule.arrProfileId) || null;
      this.profileDetail._editLockedArrProfileId = rule.arrProfileId;
      // Build the in-profile trashID set so we can split scoreOverrides
      // into base-profile overrides vs Additional CFs (extras).
      const inProfile = new Set();
      for (const fi of (detail.formatItemNames || [])) inProfile.add(fi.trashId);
      for (const g of (detail.trashGroups || [])) {
        for (const cf of (g.cfs || [])) inProfile.add(cf.trashId);
      }
      // selectedCFs → selectedOptionalCFs map. v2.5.8 split: selectedCFs
      // are PURE opt-ins (CFs not in TRaSH defaults), excludedCFs are
      // explicit opt-outs from TRaSH defaults. CFs not in either map
      // fall back to cf.default at render time.
      //
      // Ordering matters: infer group flags FIRST, then apply per-CF
      // entries. This lets the per-CF loop skip writing sel=false for
      // CFs that are already covered by a group-off flag — without that,
      // a rule saved on v2.5.9 (where group-disable bulk-wrote false
      // for every CF in the group) would rehydrate with sel=false
      // entries that LOOK like Phase 2c locks. The user toggling the
      // group back on then wouldn't restore defaults because the per-CF
      // locks would shadow the group flag's "back to defaults" intent.
      const ruleSelSet = new Set(rule.selectedCFs || []);
      const ruleExcSet = new Set(rule.excludedCFs || []);
      const selOpt = { ...(this.selectedOptionalCFs || {}) };

      // Step 1: group flag inference.
      //   - default-off group + any CF in selectedCFs → user opted in → on
      //   - default-on group + every required+default CF in excludedCFs
      //     → user opted out of the group's defaults → off
      //   - otherwise leave undefined → render falls back to defaultEnabled
      // "required+default" (not "every CF") matches what getExcludedCFIds
      // emits when disabling a default-on group: optional members the
      // user never opted into aren't in excludedCFs even when group off.
      for (const g of (detail.trashGroups || [])) {
        const cfTids = (g.cfs || []).map(cf => cf.trashId);
        const anyInSelected = cfTids.some(tid => ruleSelSet.has(tid));
        const defaultsInGroup = (g.cfs || []).filter(cf => cf.required || cf.default).map(cf => cf.trashId);
        const allDefaultsExcluded = defaultsInGroup.length > 0 && defaultsInGroup.every(tid => ruleExcSet.has(tid));
        const grpKey = '__grp_' + g.name;
        if (!g.defaultEnabled && anyInSelected) {
          selOpt[grpKey] = true;
        } else if (g.defaultEnabled && allDefaultsExcluded) {
          selOpt[grpKey] = false;
        }
      }

      // Step 2: build the set of CFs covered by a group-off flag —
      // their excludedCFs entry is redundant (group flag carries the
      // signal) and writing sel=false on them would inhibit re-enable.
      const groupOffCFs = new Set();
      for (const g of (detail.trashGroups || [])) {
        if (selOpt['__grp_' + g.name] === false) {
          for (const cf of (g.cfs || [])) {
            if (cf.required || cf.default) groupOffCFs.add(cf.trashId);
          }
        }
      }

      // Step 3: apply per-CF state. Skip the group-off-covered CFs in
      // the excludedCFs loop so they end up sel=undefined (group flag
      // cascades) instead of sel=false (looks like a Phase 2c lock).
      for (const tid of (rule.selectedCFs || [])) selOpt[tid] = true;
      for (const tid of (rule.excludedCFs || [])) {
        if (groupOffCFs.has(tid)) continue;
        selOpt[tid] = false;
      }
      this.selectedOptionalCFs = selOpt;
      // scoreOverrides split: in-profile → cfScoreOverrides, out-of-profile → extraCFs.
      const extras = {};
      const baseOv = { ...(this.cfScoreOverrides || {}) };
      if (rule.scoreOverrides) {
        for (const [tid, v] of Object.entries(rule.scoreOverrides)) {
          if (inProfile.has(tid)) {
            baseOv[tid] = v;
          } else {
            extras[tid] = v;
          }
        }
      }
      this.cfScoreOverrides = baseOv;
      if (Object.keys(extras).length > 0) {
        this.extraCFs = { ...(this.extraCFs || {}), ...extras };
        if (this.profileDetail?.instance?.type) this.loadExtraCFList();
      }
      // qualityOverrides + qualityStructure passthrough.
      if (rule.qualityStructure && rule.qualityStructure.length > 0) {
        this.qualityStructure = rule.qualityStructure.map(it => ({
          _id: ++this._qsIdCounter,
          name: it.name,
          allowed: !!it.allowed,
          ...(it.items && it.items.length > 0 ? { items: [...it.items] } : {}),
        }));
      } else if (rule.qualityOverrides && Object.keys(rule.qualityOverrides).length > 0) {
        this.qualityOverrides = { ...(this.qualityOverrides || {}), ...rule.qualityOverrides };
      }
      // Settings overrides → pdOverrides + pdOverridesEnabled flag.
      let anyOverride = Object.keys(extras).length > 0
        || (rule.qualityStructure && rule.qualityStructure.length > 0)
        || (rule.qualityOverrides && Object.keys(rule.qualityOverrides).length > 0);
      if (rule.overrides) {
        const ov = rule.overrides;
        if (ov.language !== undefined)              { this.pdOverrides.language.enabled = false; this.pdOverrides.language.value = ov.language; anyOverride = true; }
        if (ov.minFormatScore !== undefined)        { this.pdOverrides.minFormatScore.enabled = false; this.pdOverrides.minFormatScore.value = ov.minFormatScore; anyOverride = true; }
        if (ov.minUpgradeFormatScore !== undefined) { this.pdOverrides.minUpgradeFormatScore.enabled = false; this.pdOverrides.minUpgradeFormatScore.value = ov.minUpgradeFormatScore; anyOverride = true; }
        if (ov.cutoffFormatScore !== undefined)     { this.pdOverrides.cutoffFormatScore.enabled = false; this.pdOverrides.cutoffFormatScore.value = ov.cutoffFormatScore; anyOverride = true; }
        if (ov.upgradeAllowed !== undefined)        { this.pdOverrides.upgradeAllowed.enabled = false; this.pdOverrides.upgradeAllowed.value = ov.upgradeAllowed; anyOverride = true; }
        if (ov.cutoffQuality !== undefined)         { this.pdOverrides.cutoffQuality = ov.cutoffQuality; anyOverride = true; }
      }
      if (rule.behavior) {
        this.syncForm.behavior = { ...(this.syncForm.behavior || {}), ...rule.behavior };
      }
      // Restore the user's free-form notes for this rule. Empty by
      // default — the Notes editor renders an "Add notes" CTA when
      // pdDescription is blank. Auto-expand the Notes card on
      // reopen when notes exist so the user instantly sees they're
      // still there (collapsed default + small snippet otherwise
      // reads as "my notes are gone" — first reported bug).
      this.pdDescription = rule.description || '';
      this.pdNotesExpanded = !!(rule.description || '').trim();
      if (anyOverride || (rule.selectedCFs && rule.selectedCFs.length > 0)) {
        this.pdOverridesEnabled = true;
      }
    },

    initDetailSections(detail) {
      const sections = { core: false };
      const groups = {};
      for (const cat of (detail.cfCategories || [])) {
        sections[cat.category] = false;
      }
      this.detailSections = sections;
      this.groupExpanded = groups;
      this.cfDescExpanded = {};
    },

    initSelectedCFs(detail) {
      const selected = {};
      // Use trashGroups (new system) if available, fall back to cfCategories (legacy)
      if (detail.trashGroups?.length) {
        for (const group of detail.trashGroups) {
          if (group.defaultEnabled) {
            if (group.exclusive) {
              // Exclusive group: only enable the default CF
              for (const cf of (group.cfs || [])) {
                if (!cf.required) selected[cf.trashId] = !!cf.default;
              }
            } else {
              // Non-exclusive group: optional CFs respect their per-CF default
              // flag. Required CFs are handled by group state. Previously this
              // unconditionally set all optional CFs to true, ignoring
              // cf.default — defeating TRaSH's whole reason for marking some
              // group members default-on and others default-off.
              for (const cf of (group.cfs || [])) {
                if (!cf.required) selected[cf.trashId] = !!cf.default;
              }
            }
          }
        }
      } else {
        for (const cat of (detail.cfCategories || [])) {
          for (const group of cat.groups) {
            if (group.defaultEnabled) {
              if (group.exclusive) {
                for (const cf of (group.cfs || [])) {
                  selected[cf.trashId] = !!cf.default;
                }
              } else {
                // Same fix as the trashGroups branch above — respect cf.default
                // even when the legacy cfCategories shape is used.
                for (const cf of (group.cfs || [])) {
                  selected[cf.trashId] = !!cf.default;
                }
              }
            }
          }
        }
      }
      this.selectedOptionalCFs = selected;
    },

    toggleDetailSection(section) {
      this.detailSections = { ...this.detailSections, [section]: !this.detailSections[section] };
    },

    toggleGroupExpanded(category, groupName) {
      const key = category + '/' + groupName;
      this.groupExpanded = { ...this.groupExpanded, [key]: !this.groupExpanded[key] };
    },

    // Returns conflicting CF trash_ids that should be deactivated when activating trashId
    getConflictingCFs(appType, trashId) {
      const conflicts = this.conflictsData[appType]?.custom_formats;
      if (!conflicts) return [];
      const conflicting = [];
      for (const group of conflicts) {
        if (group.some(cf => cf.trash_id === trashId)) {
          for (const cf of group) {
            if (cf.trash_id !== trashId) conflicting.push(cf.trash_id);
          }
        }
      }
      return conflicting;
    },

    toggleExclusiveCF(trashId, groupCFs, mustHaveOne = false) {
      const updated = { ...this.selectedOptionalCFs };
      const enabling = !updated[trashId];
      if (enabling) {
        // Radio behavior: activate this one, deactivate all others in group
        for (const cf of groupCFs) {
          updated[cf.trashId] = (cf.trashId === trashId);
        }
        // Also deactivate cross-group conflicts from conflicts.json
        const appType = this.profileDetail?.instance?.type || this.currentTab;
        for (const conflictId of this.getConflictingCFs(appType, trashId)) {
          updated[conflictId] = false;
        }
      } else if (mustHaveOne) {
        // Golden Rule: cannot deactivate the last active one
        updated[trashId] = true;
      } else {
        // Optional exclusive (e.g. SDR): allow deactivating all
        updated[trashId] = false;
      }
      this.selectedOptionalCFs = updated;
    },

    showCFTooltip(event, text) {
      clearTimeout(window._tooltipHideTimer);
      const el = document.getElementById('cf-tooltip-portal');
      if (!el) return;
      // Pre-process pymdownx-extra "caret" syntax (^^text^^) used in
      // TRaSH descriptions for underline emphasis — without this the
      // raw "^^NOT^^" leaks through to the tooltip text. The <u> tag
      // is already on sanitizeHTML's allow-list, so the rendered
      // underline survives sanitization.
      const md = (text || '').replace(/\^\^([^^\n]+?)\^\^/g, '<u>$1</u>');
      el.innerHTML = sanitizeHTML(md);
      el.style.display = 'block';
      const rect = event.target.getBoundingClientRect();
      // UI Scale fix: getBoundingClientRect() returns post-zoom actual
      // pixels, but style.left/top is interpreted as CSS pixels and then
      // zoom-scaled by the browser. Divide every actual-pixel measurement
      // by the computed zoom so values round-trip back to the same
      // physical position. Mirrors main.js's showTooltip x-tt directive.
      const zoom = parseFloat(getComputedStyle(document.documentElement).zoom) || 1;
      // Group-fixed X anchoring: find the rightmost .cf-info icon in the
      // SAME COLUMN as the hovered icon. Without this, tooltip X shifts
      // per CF (icons sit inline after CF name → X varies with name
      // length) and the user can't slide the mouse straight down to the
      // next ⓘ because the wider tooltip overlaps shorter rows' icons.
      //
      // Multi-column awareness: groups with 10+ CFs render in 2 columns,
      // 30+ in 3 columns (custom-formats.html grid layout). Anchoring to
      // the rightmost icon across ALL columns would push the tooltip for
      // a left-column CF all the way over to the right column. Filter
      // icons by "same column" using rect.left clustering (~80px band
      // around the hovered icon's left edge).
      //
      // Walk up parents until we find the first ancestor containing >1
      // .cf-info — that's the group container.
      let anchorRight = rect.right;
      // Only walk ancestors when the trigger itself IS a .cf-info icon
      // (Sync Preview / classic editor / etc.). The new CF browse hover
      // moved off .cf-info onto .cf-name-text, where the X-anchor logic
      // does nothing useful — short-circuit to avoid an unnecessary
      // tree walk on every hover.
      if (event.target.classList && event.target.classList.contains('cf-info')) {
        let parent = event.target.parentElement;
        while (parent) {
          const siblings = parent.querySelectorAll('.cf-info');
          if (siblings.length > 1) {
            const hoverLeft = rect.left;
            let maxRight = 0;
            for (const s of siblings) {
              const r = s.getBoundingClientRect();
              // Same-column filter: ⓘ rects vary in left because name
              // length varies, but column boundaries are ~hundreds of
              // pixels apart in grid layouts. 80px band is loose
              // enough to cover varied names within a column, tight
              // enough to exclude other columns.
              if (Math.abs(r.left - hoverLeft) > 80) continue;
              if (r.right > maxRight) maxRight = r.right;
            }
            if (maxRight > 0) anchorRight = maxRight;
            break;
          }
          parent = parent.parentElement;
        }
      }
      const rTop = rect.top / zoom;
      const rLeft = rect.left / zoom;
      const aRight = anchorRight / zoom;
      const viewportW = window.innerWidth / zoom;
      const viewportH = window.innerHeight / zoom;
      // Position to the right of the rightmost icon in the group, vertically
      // aligned to the hovered row's top.
      const w = el.offsetWidth || 340;
      const h = el.offsetHeight;
      let x = aRight + 12;
      if (x + w > viewportW - 8) x = rLeft - w - 12;
      x = Math.max(8, x);
      let y = rTop - 8;
      if (y + h > viewportH - 8) y = Math.max(8, viewportH - h - 8);
      el.style.left = x + 'px';
      el.style.top = y + 'px';
    },

    hideCFTooltip() {
      window._tooltipHideTimer = setTimeout(() => {
        const el = document.getElementById('cf-tooltip-portal');
        if (el) el.style.display = 'none';
      }, 200);
    },

    toggleOptionalCF(trashId) {
      const updated = { ...this.selectedOptionalCFs, [trashId]: !this.selectedOptionalCFs[trashId] };
      // If enabling, deactivate any conflicting CFs (cross-group conflicts from conflicts.json)
      if (updated[trashId]) {
        const appType = this.profileDetail?.instance?.type || this.currentTab;
        for (const conflictId of this.getConflictingCFs(appType, trashId)) {
          updated[conflictId] = false;
        }
      }
      this.selectedOptionalCFs = updated;
    },

    // --- Category helpers ---

    // Resolves a TRaSH category label to its CSS class suffix (cat-anime, …).
    // Backed by the UI manifest: internal/core/categories.go owns the
    // label → ID mapping (with aliases for near-duplicates like "French
    // Audio Version" + "French HQ Source Groups"), and manifest.js builds
    // the lookup table at load time.
    getCategoryClass(category) {
      return this.manifestCategoryClass(category);
    },

    countCategoryCFs(cat) {
      let count = 0;
      for (const g of (cat.groups || [])) count += (g.cfs || []).length;
      return count;
    },

    countSelectedCategoryCFs(cat) {
      let count = 0;
      for (const g of (cat.groups || [])) {
        for (const cf of (g.cfs || [])) {
          if (this.selectedOptionalCFs[cf.trashId]) count++;
        }
      }
      return count;
    },

    countSelectedGroupCFs(catName, group) {
      let count = 0;
      for (const cf of (group.cfs || [])) {
        if (this.selectedOptionalCFs[cf.trashId]) count++;
      }
      return count;
    },

    countAllCategoryCFs() {
      // Use trashGroups if available
      const groups = this.profileDetail?.detail?.trashGroups || [];
      if (groups.length) {
        let count = 0;
        for (const group of groups) {
          const grpOn = this.selectedOptionalCFs['__grp_' + group.name] !== undefined
            ? this.selectedOptionalCFs['__grp_' + group.name] : group.defaultEnabled;
          if (grpOn) count += group.cfs.length;
        }
        return count;
      }
      // Legacy fallback
      let count = 0;
      for (const cat of (this.profileDetail?.detail?.cfCategories || [])) {
        count += this.countCategoryCFs(cat);
      }
      return count;
    },

    countGroupCFs(groups) {
      if (!groups) return 0;
      let count = 0;
      for (const g of groups) count += (g.cfs || []).length;
      return count;
    },

    // --- Instance Profile Compare ---

    // Resolve the human-readable name of an Arr profile for the given
    // instance + profile ID. Used by toasts / debug-log lines that
    // would otherwise show the raw ID — meaningless to users who
    // identify their profiles by name. Lookup order:
    //   1. instProfiles cache (live data fetched at startup, refreshed
    //      via loadInstanceProfiles whenever the user navigates near
    //      a profile flow)
    //   2. sync-history entries — every successful sync writes
    //      arrProfileName, so this catches profiles that weren't in
    //      the live cache yet
    //   3. empty string if neither has it (caller should fall back to
    //      "Arr profile #N" formatting)
    resolveArrProfileName(instId, arrProfileId) {
      if (!instId || !arrProfileId) return '';
      const profs = this.instProfiles?.[instId];
      if (Array.isArray(profs)) {
        const p = profs.find(x => x.id === arrProfileId);
        if (p?.name) return p.name;
      }
      const hist = this.syncHistory?.[instId] || [];
      const h = hist.find(x => x.arrProfileId === arrProfileId && x.arrProfileName);
      return h?.arrProfileName || '';
    },

    async loadInstanceProfiles(inst) {
      this.instProfilesLoading = {...this.instProfilesLoading, [inst.id]: true};
      try {
        const r = await fetch(`/api/instances/${inst.id}/profiles`);
        if (!r.ok) return;
        const profiles = await r.json();
        this.instProfiles = {...this.instProfiles, [inst.id]: profiles};
      } catch (e) {
        console.error('loadInstanceProfiles:', e);
      } finally {
        this.instProfilesLoading = {...this.instProfilesLoading, [inst.id]: false};
      }
    },

    selectInstProfile(inst, arrProfile) {
      const current = this.instCompareProfile[inst.id];
      if (current === arrProfile.id) {
        // Toggle off
        this.instCompareProfile = {...this.instCompareProfile, [inst.id]: null};
        this.instCompareResult = {...this.instCompareResult, [inst.id]: null};
        this.instCompareTrashId = {...this.instCompareTrashId, [inst.id]: ''};
      } else {
        this.instCompareProfile = {...this.instCompareProfile, [inst.id]: arrProfile.id};
        this.instCompareResult = {...this.instCompareResult, [inst.id]: null};
        this.instCompareTrashId = {...this.instCompareTrashId, [inst.id]: ''};
      }
    },

    async runProfileCompare(inst, arrProfileId, trashProfileId) {
      this.debugLog('UI', `Compare: arr profile ${arrProfileId} vs TRaSH "${trashProfileId}" on ${inst.name}`);
      this.instCompareTrashId = {...this.instCompareTrashId, [inst.id]: trashProfileId};
      if (!trashProfileId) {
        this.instCompareResult = {...this.instCompareResult, [inst.id]: null};
        return;
      }
      this.instCompareLoading = {...this.instCompareLoading, [inst.id]: true};
      try {
        const r = await fetch(`/api/instances/${inst.id}/compare?arrProfileId=${arrProfileId}&trashProfileId=${encodeURIComponent(trashProfileId)}`);
        if (!r.ok) {
          this.instCompareResult = {...this.instCompareResult, [inst.id]: {error: 'Failed to compare'}};
          return;
        }
        const result = await r.json();
        this.instCompareResult = {...this.instCompareResult, [inst.id]: result};
      } catch (e) {
        console.error('runProfileCompare:', e);
        this.instCompareResult = {...this.instCompareResult, [inst.id]: {error: e.message}};
      } finally {
        this.instCompareLoading = {...this.instCompareLoading, [inst.id]: false};
      }
    },

    getTrashProfileGroups(appType) {
      const profiles = this.trashProfiles[appType] || [];
      const order = { 'Standard': 0, 'Anime': 1, 'French': 2, 'German': 3, 'SQP': 99 };
      const seen = new Set();
      const groups = [];
      for (const p of profiles) {
        const gn = p.groupName || 'Other';
        if (!seen.has(gn)) { seen.add(gn); groups.push(gn); }
      }
      return groups.sort((a, b) => (order[a] ?? 50) - (order[b] ?? 50));
    },

    getTrashProfilesByGroup(appType, groupName) {
      return (this.trashProfiles[appType] || []).filter(p => (p.groupName || 'Other') === groupName);
    },

    getSyncOptionalBreakdown() {
      const sel = this.selectedOptionalCFs || {};
      const groups = this.profileDetail?.detail?.trashGroups || [];
      const breakdown = [];
      if (groups.length) {
        // New: use trashGroups
        for (const group of groups) {
          const grpOn = sel['__grp_' + group.name] !== undefined ? sel['__grp_' + group.name] : group.defaultEnabled;
          if (!grpOn) continue;
          const count = group.cfs.filter(cf => cf.required || sel[cf.trashId]).length;
          if (count > 0) breakdown.push({ category: group.name, count });
        }
      } else {
        // Legacy: use cfCategories
        for (const cat of (this.profileDetail?.detail?.cfCategories || [])) {
          let count = 0;
          for (const g of cat.groups) {
            for (const cf of g.cfs) { if (sel[cf.trashId]) count++; }
          }
          if (count > 0) breakdown.push({ category: cat.category, count });
        }
      }
      return breakdown;
    },

    getSelectedCFIds() {
      const idSet = new Set();
      const sel = this.selectedOptionalCFs || {};
      const groups = this.profileDetail?.detail?.trashGroups || [];
      const isGroupOn = (g) => {
        const flag = sel['__grp_' + g.name];
        return flag === undefined ? g.defaultEnabled : flag;
      };
      // Individually toggled optional CFs — honoured regardless of
      // group state. User explicit sel=true is a hard signal ("include
      // this specific CF"). When the user disables a group via
      // pdToggleGroup, that handler clears per-CF=true entries for
      // the group's members + their score overrides, so this loop
      // doesn't see stale opt-ins from a disabled group.
      for (const [k, v] of Object.entries(sel)) {
        if (v && !k.startsWith('__grp_')) idSet.add(k);
      }
      // Required CFs from active TRaSH groups — skip Phase 2c locks
      // (sel===false) so a lock on a required CF inside an opted-in
      // default-off group is honoured.
      for (const group of groups) {
        if (!isGroupOn(group)) continue;
        for (const cf of group.cfs) {
          if (!cf.required) continue;
          if (sel[cf.trashId] === false) continue;
          idSet.add(cf.trashId);
        }
      }
      // Score-override safety net. CFs carrying an override that
      // aren't otherwise in scope still need to make it into
      // selectedCFs so backend's BuildArrProfile applies the override.
      // Skip Phase 2c locks (sel===false). pdToggleGroup-off clears
      // cfScoreOverrides for the group's CFs, so an override on a CF
      // in a disabled group never reaches this loop.
      if (this.cfScoreOverrides) {
        for (const trashId of Object.keys(this.cfScoreOverrides)) {
          if (idSet.has(trashId)) continue;
          if (sel[trashId] === false) continue;
          idSet.add(trashId);
        }
      }
      return [...idSet];
    },

    // --- Sync ---

    async openSyncModal(inst, profile) {
      this.syncForm = {
        instanceId: inst.id,
        instanceName: inst.name,
        appType: inst.type,
        profileTrashId: profile.trashId,
        importedProfileId: '',
        profileName: profile.name,
        arrProfileId: '0',
        newProfileName: profile.name,
        behavior: { addMode: 'add_missing', removeMode: 'remove_custom', resetMode: 'reset_to_zero' }
      };
      // Re-arm the edit-session lock on every Save & Sync click. Without
      // this, after the first sync attempt the modal would flip back to
      // 'create' mode on a second click (Dry Run / Cancel had cleared
      // resyncTargetArrProfileId in _loadSyncInstanceData).
      if (this.profileDetail?._editLockedArrProfileId) {
        this.resyncTargetArrProfileId = this.profileDetail._editLockedArrProfileId;
      }
      await this._loadSyncInstanceData(inst.id, profile.trashId);
      this.showSyncModal = true;
    },

    async openSyncModalAsNew(inst, profile) {
      this.resyncTargetArrProfileId = null;
      this.syncForm = {
        instanceId: inst.id,
        instanceName: inst.name,
        appType: inst.type,
        profileTrashId: profile.trashId,
        importedProfileId: '',
        profileName: profile.name,
        arrProfileId: '0',
        newProfileName: profile.name + ' (Copy)',
        behavior: { addMode: 'add_missing', removeMode: 'remove_custom', resetMode: 'reset_to_zero' }
      };
      try {
        const r = await fetch(`/api/instances/${inst.id}/profiles`);
        this.arrProfiles = r.ok ? await r.json() : [];
      } catch (e) { this.arrProfiles = []; }
      this.getLanguagesForInstance(inst.id);
      this.syncMode = 'create';
      this.autoSyncRuleForSync = null;
      this.syncPreview = null;
      this.showSyncModal = true;
    },

    async openImportedSyncModalFromList(appType, profile) {
      const inst = this.instancesOfType(appType)[0];
      if (!inst) { this.showToast('No ' + appType + ' instance configured', 'error', 8000); return; }
      this.syncForm = {
        instanceId: inst.id,
        instanceName: inst.name,
        appType: appType,
        profileTrashId: profile.trashProfileId || '',
        importedProfileId: profile.id,
        profileName: profile.name,
        arrProfileId: '0',
        newProfileName: profile.name,
        behavior: { addMode: 'add_missing', removeMode: 'remove_custom', resetMode: 'reset_to_zero' }
      };
      await this._loadSyncInstanceData(inst.id, profile.trashProfileId || profile.id);
      this.showSyncModal = true;
    },

    async openImportedSyncModalAsNew() {
      const raw = this.profileDetail?.detail?.importedRaw;
      if (!raw) return;
      const appType = raw.appType || this.profileDetail?.profile?.appType;
      const inst = this.instancesOfType(appType)[0];
      if (!inst) { this.showToast('No ' + appType + ' instance configured', 'error', 8000); return; }
      this.resyncTargetArrProfileId = null;
      this.syncForm = {
        instanceId: inst.id,
        instanceName: inst.name,
        appType: appType,
        profileTrashId: raw.trashProfileId || '',
        importedProfileId: raw.id,
        profileName: raw.name,
        arrProfileId: '0',
        newProfileName: raw.name + ' (Copy)',
        behavior: { addMode: 'add_missing', removeMode: 'remove_custom', resetMode: 'reset_to_zero' }
      };
      try {
        const r = await fetch(`/api/instances/${inst.id}/profiles`);
        this.arrProfiles = r.ok ? await r.json() : [];
      } catch (e) { this.arrProfiles = []; }
      this.getLanguagesForInstance(inst.id);
      this.syncMode = 'create';
      this.autoSyncRuleForSync = null;
      this.syncPreview = null;
      this.showSyncModal = true;
    },

    async saveAndSyncBuilderAsNew() {
      const editId = this.pb.editId;
      const appType = this.pb.appType;
      await this.saveCustomProfile();
      const allImported = this.importedProfiles[appType] || [];
      const imported = allImported.find(p => p.id === editId);
      if (!imported) return;
      const found = this.findSyncedInstance(appType, imported.id);
      if (!found) { this.showToast('No ' + appType + ' instance configured', 'error', 8000); return; }
      const { inst } = found;
      this.resyncTargetArrProfileId = null;
      this.syncForm = {
        instanceId: inst.id, instanceName: inst.name, appType: appType,
        profileTrashId: imported.trashProfileId || '', importedProfileId: imported.id,
        profileName: imported.name, arrProfileId: '0', newProfileName: imported.name + ' (Copy)',
        behavior: { addMode: 'add_missing', removeMode: 'remove_custom', resetMode: 'reset_to_zero' }
      };
      try {
        const r = await fetch(`/api/instances/${inst.id}/profiles`);
        this.arrProfiles = r.ok ? await r.json() : [];
      } catch (e) { this.arrProfiles = []; }
      this.getLanguagesForInstance(inst.id);
      this.syncMode = 'create';
      this.autoSyncRuleForSync = null;
      this.syncPreview = null;
      this.showSyncModal = true;
    },

    async saveAndSyncBuilder() {
      const editId = this.pb.editId;
      const appType = this.pb.appType;
      await this.saveCustomProfile();
      const allImported = this.importedProfiles[appType] || [];
      const imported = allImported.find(p => p.id === editId);
      if (!imported) return;
      const found = this.findSyncedInstance(appType, imported.id);
      if (!found) { this.showToast('No ' + appType + ' instance configured', 'error', 8000); return; }
      const { inst, history } = found;
      if (history) this.resyncTargetArrProfileId = history.arrProfileId;
      this.syncForm = {
        instanceId: inst.id, instanceName: inst.name, appType: appType,
        profileTrashId: imported.trashProfileId || '', importedProfileId: imported.id,
        profileName: imported.name, arrProfileId: '0', newProfileName: imported.name,
        behavior: history?.behavior || { addMode: 'add_missing', removeMode: 'remove_custom', resetMode: 'reset_to_zero' }
      };
      await this._loadSyncInstanceData(inst.id, imported.trashProfileId || imported.id);
      this.showSyncModal = true;
    },

    async openBuilderSyncModal() {
      if (!this.pb.editId || !this.pb.appType) return;
      const appType = this.pb.appType;
      const allImported = this.importedProfiles[appType] || [];
      const imported = allImported.find(p => p.id === this.pb.editId);
      if (!imported) { this.showToast('Profile not found', 'error', 8000); return; }
      const found = this.findSyncedInstance(appType, imported.id);
      if (!found) { this.showToast('No ' + appType + ' instance configured', 'error', 8000); return; }
      const { inst, history } = found;
      if (history) this.resyncTargetArrProfileId = history.arrProfileId;
      this.syncForm = {
        instanceId: inst.id,
        instanceName: inst.name,
        appType: appType,
        profileTrashId: imported.trashProfileId || '',
        importedProfileId: imported.id,
        profileName: imported.name,
        arrProfileId: '0',
        newProfileName: imported.name,
        behavior: history?.behavior || { addMode: 'add_missing', removeMode: 'remove_custom', resetMode: 'reset_to_zero' }
      };
      await this._loadSyncInstanceData(inst.id, imported.trashProfileId || imported.id);
      this.showSyncModal = true;
    },

    async openImportedSyncModal() {
      const raw = this.profileDetail?.detail?.importedRaw;
      if (!raw) return;
      const appType = raw.appType || this.profileDetail?.profile?.appType;
      const inst = this.instancesOfType(appType)[0];
      if (!inst) { this.showToast('No ' + appType + ' instance configured', 'error', 8000); return; }
      this.syncForm = {
        instanceId: inst.id,
        instanceName: inst.name,
        appType: appType,
        profileTrashId: raw.trashProfileId || '',
        importedProfileId: raw.id,
        profileName: raw.name,
        arrProfileId: '0',
        newProfileName: raw.name,
        behavior: { addMode: 'add_missing', removeMode: 'remove_custom', resetMode: 'reset_to_zero' }
      };
      await this._loadSyncInstanceData(inst.id, raw.trashProfileId || raw.id);
      this.showSyncModal = true;
    },

    async switchSyncInstance(newInstId) {
      const inst = this.instancesOfType(this.syncForm.appType).find(i => i.id === newInstId);
      if (!inst) return;
      this.syncForm.instanceId = inst.id;
      this.syncForm.instanceName = inst.name;
      this.syncForm.arrProfileId = '0';
      this.syncPreview = null;
      await this._loadSyncInstanceData(inst.id, this.syncForm.profileTrashId || this.syncForm.importedProfileId);
    },

    // Returns sorted language list for an instance (Original first, then alphabetical). Uses cache.
    async getLanguagesForInstance(instId) {
      if (this.instanceLanguages[instId]) return this.instanceLanguages[instId];
      try {
        const r = await fetch(`/api/instances/${instId}/languages`);
        if (r.ok) {
          const langs = await r.json();
          // Sort: Original first, Any second, then alphabetical
          langs.sort((a, b) => {
            if (a.name === 'Original') return -1;
            if (b.name === 'Original') return 1;
            if (a.name === 'Any') return -1;
            if (b.name === 'Any') return 1;
            return a.name.localeCompare(b.name);
          });
          this.instanceLanguages[instId] = langs;
          return langs;
        }
      } catch (e) { /* ignore */ }
      return [{ id: -1, name: 'Original' }, { id: 0, name: 'Any' }];
    },

    // Shorthand: languages for current sync form instance (or fallback)
    get syncLanguages() {
      return this.instanceLanguages[this.syncForm.instanceId] || [{ id: -1, name: 'Original' }, { id: 0, name: 'Any' }];
    },

    // Max possible Min Score for the profile-as-currently-configured.
    // Mirrors backend's BuildSyncPlan computation but runs client-side
    // off the data already loaded into the editor — no dry-run / HTTP
    // round-trip needed. The value is the post-sync sum of every
    // positive Custom Format score that will be in the profile after
    // Save & Sync, which is exactly what Sonarr/Radarr validates Min
    // Score against.
    //
    // Sources walked (matches backend's allCFTrashIDs ∪ overrides):
    //   1. detail.formatItemNames        — TRaSH profile.formatItems (always synced)
    //   2. detail.trashGroups[].cfs      — group CFs that are toggled on
    //   3. selectedOptionalCFs[trashId]  — individually toggled optional CFs
    //   4. extraCFs                      — user-added extras (their explicit score)
    //   5. cfScoreOverrides              — per-CF score overrides (when active)
    //
    // Returns null when no profile is loaded so the tooltip / red
    // border can hide. Caveat: do_not_adjust removeMode keeps existing
    // Arr scores for CFs not in the managed set; those aren't visible
    // here. Backend has the same caveat in its plan computation.
    get pdMaxPossibleScore() {
      const detail = this.profileDetail?.detail;
      if (!detail) return null;
      const cfMap = new Map(); // trashId → score
      // 1. profile.formatItems
      for (const fi of (detail.formatItemNames || [])) {
        cfMap.set(fi.trashId, fi.score ?? 0);
      }
      // 2 + 3. Active groups: required CFs + individually toggled optionals
      const groups = detail.trashGroups || [];
      for (const group of groups) {
        const grpKey = '__grp_' + group.name;
        const grpOn = (this.selectedOptionalCFs[grpKey] !== undefined)
          ? this.selectedOptionalCFs[grpKey]
          : group.defaultEnabled;
        if (!grpOn) continue;
        for (const cf of group.cfs) {
          const indOn = this.selectedOptionalCFs[cf.trashId];
          if (cf.required || indOn) {
            if (!cfMap.has(cf.trashId)) cfMap.set(cf.trashId, cf.score ?? 0);
          }
        }
      }
      // 4. Extra CFs — user-added with their explicit scores
      for (const [tid, score] of Object.entries(this.extraCFs || {})) {
        cfMap.set(tid, score);
      }
      // 5. Score overrides — apply to every CF in the override map. CFs
      // already in cfMap get their score replaced; CFs not yet present get
      // pulled in (matching the backend, which uses body.scoreOverrides
      // regardless of selectedCFs). Symmetric with buildSyncBody:1283 where
      // the same override IDs are added to selectedCFs.
      if (this.cfScoreOverrides) {
        for (const [tid, score] of Object.entries(this.cfScoreOverrides)) {
          cfMap.set(tid, score);
        }
      }
      // Sum positives — matches backend's `if sa.NewScore > 0` filter
      let sum = 0;
      for (const score of cfMap.values()) {
        if (typeof score === 'number' && score > 0) sum += score;
      }
      return sum;
    },

    // True when the user's Min Format Score override exceeds the max
    // computable from the profile-as-configured. Drives the red border
    // + the pre-flight gate in applySync.
    get pdMinScoreInvalid() {
      const max = this.pdMaxPossibleScore;
      if (max === null) return false;
      const min = this.pdOverrides?.minFormatScore?.value ?? 0;
      return min > max;
    },

    async _loadSyncInstanceData(instId, profileTrashId) {
      try {
        const r = await fetch(`/api/instances/${instId}/profiles`);
        this.arrProfiles = r.ok ? await r.json() : [];
      } catch (e) {
        this.arrProfiles = [];
      }
      // Load languages for this instance
      this.getLanguagesForInstance(instId);
      // Mode default: 'create'. Only the explicit Edit-pencil flow
      // (resyncProfile → resyncTargetArrProfileId) auto-flips to 'update'.
      // Previously we also auto-flipped on any sync-history match, which
      // silently put users in overwrite mode when they re-opened a TRaSH
      // profile from the list with no intent to edit the saved rule. The
      // user must now explicitly pick "Update existing" via the radio for
      // any non-pencil flow, and applySync() shows a confirm-overwrite
      // dialog when they do (gated on _editFlow).
      const arrProfileIds = new Set((this.arrProfiles || []).map(p => p.id));
      this.syncForm._editFlow = false;
      if (this.resyncTargetArrProfileId && arrProfileIds.has(this.resyncTargetArrProfileId)) {
        this.syncMode = 'update';
        this.syncForm.arrProfileId = String(this.resyncTargetArrProfileId);
        this.syncForm._editFlow = true;
        this.resyncTargetArrProfileId = null;
      } else {
        this.syncMode = 'create';
      }
      this.syncPreview = null;
      // Auto-fetch preview if update mode with pre-selected profile
      if (this.syncMode === 'update' && this.syncForm.arrProfileId && this.syncForm.arrProfileId !== '0') {
        this.fetchSyncPreview();
      }
      // Check for existing auto-sync rule
      this.updateAutoSyncRuleForSync();
    },

    buildSyncBody() {
      const body = {
        instanceId: this.syncForm.instanceId,
        profileTrashId: this.syncForm.profileTrashId,
        arrProfileId: this.syncMode === 'create' ? 0 : parseInt(this.syncForm.arrProfileId),
        selectedCFs: this.getAllSelectedCFIds(),
        // Explicit opt-outs — trash_ids the user has toggled OFF among
        // CFs that ARE in current TRaSH defaults for this profile. Backend
        // subtracts these from `ComputeTrashDefaults ∪ selectedCFs` so the
        // resulting Arr profile reflects the user's opt-out choices even
        // when TRaSH later moves the CFs between formatItems and cf-groups.
        excludedCFs: this.getExcludedCFIds()
      };
      if (this.syncForm.importedProfileId) {
        body.importedProfileId = this.syncForm.importedProfileId;
      }
      if (this.syncMode === 'create') {
        body.profileName = this.syncForm.newProfileName;
      }
      // Build overrides from pdOverrides values. Persistence is data-driven —
      // values that match the profile default are filtered out below, so the
      // saved sync rule only carries true overrides. The pdOverridesEnabled
      // toggle gates the EDITOR UI (whether the override cards render at all),
      // not the payload — when the user disables the toggle, pdDisableOverrides
      // explicitly clears the maps so the next sync sends a clean body.
      const ov = this.pdOverrides;
      const p = this.profileDetail?.detail?.profile || {};
      const overrides = {};
      let hasOverrides = false;
      if (this.activeAppType === 'radarr' && ov.language.value !== (p.language || 'Original')) { overrides.language = ov.language.value; hasOverrides = true; }
      const upVal = ov.upgradeAllowed.value === true || ov.upgradeAllowed.value === 'true';
      if (upVal !== (p.upgradeAllowed ?? true)) { overrides.upgradeAllowed = upVal; hasOverrides = true; }
      if (ov.minFormatScore.value !== (p.minFormatScore ?? 0)) { overrides.minFormatScore = ov.minFormatScore.value; hasOverrides = true; }
      if (ov.minUpgradeFormatScore.value !== (p.minUpgradeFormatScore ?? 1)) { overrides.minUpgradeFormatScore = ov.minUpgradeFormatScore.value; hasOverrides = true; }
      if (ov.cutoffFormatScore.value !== (p.cutoffFormatScore || p.cutoffScore || 10000)) { overrides.cutoffFormatScore = ov.cutoffFormatScore.value; hasOverrides = true; }
      const defaultCutoff = p.cutoff || '';
      if (ov.cutoffQuality && ov.cutoffQuality !== defaultCutoff) { overrides.cutoffQuality = ov.cutoffQuality; hasOverrides = true; }
      if (hasOverrides) body.overrides = overrides;
      // Carry the rule's persisted KeepArrCFIDs through. Without this echo,
      // opening an existing rule via Profile Detail and clicking Save & Sync
      // would silently zero every Arr-only custom CF the user previously
      // pinned (rule has the list, but body didn't, so backend's
      // reset_to_zero treated them as unsynced).
      const arrIdForRule = parseInt(this.syncForm.arrProfileId) || 0;
      if (arrIdForRule > 0) {
        const existingRule = this.autoSyncRules.find(r => r.instanceId === this.syncForm.instanceId && r.arrProfileId === arrIdForRule);
        if (existingRule && Array.isArray(existingRule.keepArrCFIDs) && existingRule.keepArrCFIDs.length > 0) {
          body.keepArrCFIDs = existingRule.keepArrCFIDs;
        }
      }
      // Per-CF score overrides + extra CFs scores. Strip orphan custom:
      // refs (deleted CFs still in state) so backend doesn't have to
      // clean up after us — keeps payloads + persisted rule data clean.
      // See _isOrphanCustomTrashId for the guard that handles the Reset
      // window safely.
      const allScoreOverrides = {};
      const selForExtras = this.selectedOptionalCFs || {};
      for (const [tid, score] of Object.entries(this.cfScoreOverrides)) {
        if (this._isOrphanCustomTrashId(tid)) continue;
        allScoreOverrides[tid] = score;
      }
      for (const [tid, score] of Object.entries(this.extraCFs)) {
        if (this._isOrphanCustomTrashId(tid)) continue;
        // Mirror the sel-filter from getAllSelectedCFIds: an extra the
        // user toggled off must NOT survive in scoreOverrides either,
        // or applyRuleStateToEditor on reopen re-populates extraCFs
        // from the persisted scoreOverride and the toggle springs back.
        if (selForExtras[tid] === false) continue;
        allScoreOverrides[tid] = score;
      }
      if (Object.keys(allScoreOverrides).length > 0) body.scoreOverrides = allScoreOverrides;
      // Quality overrides: structure (new) trumps flat map (legacy). Skip
      // sending qualityStructure when it exactly mirrors profile defaults —
      // otherwise just OPENING the Quality Items editor (which auto-inits
      // qualityStructure from defaults so drag-drop works) would persist a
      // phantom override on Save & Sync.
      if (this.qualityStructure.length > 0 && !this.qualityStructureMatchesDefaults()) {
        body.qualityStructure = this.qsForBackend();
      } else if (Object.keys(this.qualityOverrides).length > 0) {
        body.qualityOverrides = this.qualityOverrides;
      }
      // Sync behavior rules
      if (this.syncForm.behavior) body.behavior = this.syncForm.behavior;
      // Free-form user notes about this rule — persists as
      // AutoSyncRule.Description. Trim to avoid whitespace-only saves.
      const desc = (this.pdDescription || '').trim();
      if (desc) body.description = desc;
      return body;
    },

    async fetchSyncPreview() {
      this.syncPreview = null;
      if (!this.syncForm.arrProfileId || this.syncForm.arrProfileId === '0') return;
      this.syncPreviewLoading = true;
      try {
        const r = await fetch('/api/sync/dry-run', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(this.buildSyncBody())
        });
        if (r.ok) {
          this.syncPreview = await r.json();
        } else {
          const e = await r.json();
          this.syncPreview = { error: e.error || 'Preview failed' };
        }
      } catch (e) {
        this.syncPreview = { error: e.message };
      } finally {
        this.syncPreviewLoading = false;
      }
    },

    async startDryRun() {
      // Check for name collision in Create mode — prevent silent overwrite
      if (this.syncMode === 'create') {
        const newName = this.syncForm.newProfileName.trim().toLowerCase();
        const existing = this.arrProfiles.find(p => p.name.toLowerCase() === newName);
        if (existing) {
          this.showToast(`Profile "${this.syncForm.newProfileName.trim()}" already exists in ${this.syncForm.instanceName}. Choose a different name or use Update mode.`, 'error', 10000);
          return;
        }
      }

      this.syncing = true;
      this.debugLog('UI', `Dry-run: "${this.syncForm.profileName}" → ${this.syncForm.instanceName} | ${this.getSelectedCFIds().length} selected CFs`);
      try {
        const body = this.buildSyncBody();
        const r = await fetch('/api/sync/dry-run', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body)
        });
        const data = await r.json();
        if (!r.ok) {
          this.showToast(data.error || 'Dry-run failed', 'error', 8000);
          return;
        }
        this.showSyncModal = false;
        if (!this.profileDetail && !this.syncForm.importedProfileId) {
          const inst = this.instances.find(i => i.id === this.syncForm.instanceId);
          const profile = (this.trashProfiles[inst.type] || []).find(p => p.trashId === this.syncForm.profileTrashId);
          // Post-dryrun land-on-profile: restore the rule whose dryrun we
          // just ran so the editor reflects what was previewed.
          if (inst && profile) await this.openProfileDetail(inst, profile, true);
        }
        this.syncPlan = data;
        this.dryrunDetailsOpen = false;
      } catch (e) {
        console.error('dryRun:', e);
      } finally {
        this.syncing = false;
      }
    },

    async startApply() {
      // Pre-flight reachability check — bail out with a friendly toast
      // before the confirm modals + body building if the Arr instance is
      // unreachable. Avoids the user clicking through an "overwrite?"
      // confirm only to get a generic "failed to build sync plan" toast
      // when the real cause is just that Sonarr/Radarr is down.
      const instId = this.syncForm.instanceId;
      const instName = this.syncForm.instanceName || this.profileDetail?.instance?.name || 'instance';
      if (instId) {
        try {
          const probe = await fetch(`/api/instances/${instId}/test`, { method: 'POST' });
          const probeBody = await probe.json().catch(() => ({}));
          if (!probe.ok || probeBody.connected === false) {
            const detail = probeBody.error || `${instName} is not reachable.`;
            this.showToast(`Save & Sync skipped — ${detail}`, 'error', 8000);
            return;
          }
        } catch (e) {
          this.showToast(`Save & Sync skipped — ${instName} unreachable: ${e.message}`, 'error', 8000);
          return;
        }
      }

      // Overwrite-confirmation guard. The "Edit sync rule" pencil flow sets
      // _editFlow=true in _loadSyncInstanceData; that path expects to update
      // the existing rule and shows no popup. Any other path (Save & Sync
      // from Profile Detail, Compare, etc.) reaching Update mode means the
      // user explicitly clicked the "Update existing profile" radio — warn
      // before we replace their saved customizations with current UI state.
      if (this.syncMode === 'update' && !this.syncForm._editFlow) {
        const arrId = parseInt(this.syncForm.arrProfileId) || 0;
        const targetProfile = this.arrProfiles.find(p => p.id === arrId);
        const profileName = targetProfile?.name || `Arr profile ${arrId}`;
        const existingRule = this.autoSyncRules.find(r =>
          r.instanceId === this.syncForm.instanceId && r.arrProfileId === arrId
        );
        const ruleSummary = existingRule
          ? `${existingRule.selectedCFs?.length || 0} CFs and ${Object.keys(existingRule.scoreOverrides || {}).length} score overrides`
          : 'its existing settings';
        const ok = await new Promise(resolve => {
          this.confirmModal = {
            show: true,
            title: 'Overwrite existing sync rule?',
            message: `You're about to overwrite the sync rule for "${profileName}" with the current sync form state.\n\nThe saved rule (${ruleSummary}) will be replaced. To edit the saved rule without losing customizations, cancel and use the pencil (edit) icon next to the rule in the Sync Rules list.\n\nProceed with overwrite?`,
            confirmLabel: 'Overwrite',
            onConfirm: () => resolve(true),
            onCancel: () => resolve(false)
          };
        });
        if (!ok) return;
      }

      // Check for name collision in Create mode — prevent silent overwrite
      if (this.syncMode === 'create') {
        const newName = this.syncForm.newProfileName.trim().toLowerCase();
        const existing = this.arrProfiles.find(p => p.name.toLowerCase() === newName);
        if (existing) {
          this.showToast(`Profile "${this.syncForm.newProfileName.trim()}" already exists in ${this.syncForm.instanceName}. Choose a different name or use Update mode.`, 'error', 10000);
          return;
        }
      }

      // Check for source type conflict (builder↔TRaSH)
      const arrId = parseInt(this.syncForm.arrProfileId) || 0;
      if (arrId > 0) {
        const instId = this.syncForm.instanceId;
        const existing = (this.syncHistory[instId] || []).find(sh => sh.arrProfileId === arrId);
        if (existing) {
          const isBuilder = !!this.syncForm.importedProfileId;
          const wasBuilder = !!existing.importedProfileId;
          if (wasBuilder && !isBuilder) {
            // Builder → TRaSH: OK with warning
            const ok = await new Promise(resolve => {
              this.confirmModal = { show: true, title: 'Convert to TRaSH Sync',
                message: 'This profile is currently synced via Profile Builder with fixed scores.\n\nConverting to TRaSH Sync means CFs and scores will follow TRaSH Guide updates automatically. You can set overrides in Customize.\n\nThis will replace the builder sync rule.',
                onConfirm: () => resolve(true), onCancel: () => resolve(false) };
            });
            if (!ok) return;
          } else if (!wasBuilder && isBuilder) {
            // TRaSH → Builder: warning about losing auto-sync
            const ok = await new Promise(resolve => {
              this.confirmModal = { show: true, title: 'Replace TRaSH Sync Rule',
                message: 'This profile is currently synced with TRaSH Guides and receives automatic updates.\n\nSwitching to a builder profile will stop all TRaSH Guide sync — scores become fixed and will no longer update automatically.\n\nAre you sure?',
                onConfirm: () => resolve(true), onCancel: () => resolve(false) };
            });
            if (!ok) return;
          }
        }
      }
      // Auto-coerce Min Upgrade Score to 1 if the user typed 0 or
      // negative. Sonarr/Radarr's own UI does the same client-side,
      // so matching that behaviour avoids a confusing round-trip
      // through Arr's 400. The blur handler on the input already
      // does this when the user moves focus; this is the safety net
      // for the "user edits then immediately clicks Apply without
      // blurring the field" case.
      if ((this.pdOverrides?.minUpgradeFormatScore?.value ?? 1) < 1) {
        this.pdOverrides.minUpgradeFormatScore.value = 1;
      }
      // Block Apply when Min Score exceeds the authoritative max from
      // the latest dry-run plan. The plan computes maxPossibleScore by
      // walking exactly the CF set ExecuteSyncPlan will push, so this
      // matches what Sonarr/Radarr would validate — no edge cases.
      if (this.pdMinScoreInvalid) {
        const min = this.pdOverrides?.minFormatScore?.value ?? 0;
        const max = this.pdMaxPossibleScore;
        this.showToast(`Min Score ${min} can't be reached — max for this profile after sync is ${max} (sum of every positive Custom Format score). Lower Min Score, raise CF scores, or add CFs with positive scores before saving.`, 'error', 9000);
        return;
      }
      this.syncing = true;
      this.debugLog('UI', `Apply: "${this.syncForm.profileName}" → ${this.syncForm.instanceName}`);
      try {
        const r = await fetch('/api/sync/apply', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(this.buildSyncBody())
        });
        const result = await r.json();
        this.showSyncModal = false;
        if (!this.profileDetail && !this.syncForm.importedProfileId) {
          const inst = this.instances.find(i => i.id === this.syncForm.instanceId);
          const profile = (this.trashProfiles[inst.type] || []).find(p => p.trashId === this.syncForm.profileTrashId);
          // Post-apply land-on-profile: restore the rule we just synced so
          // the editor reflects the persisted state.
          if (inst && profile) await this.openProfileDetail(inst, profile, true);
        }
        // Show toast for imported profiles (no profile detail view to show results)
        if (this.syncForm.importedProfileId) {
          if (result.error) {
            this.showToast(`Sync failed: ${result.error}`, 'error', 8000);
          } else if (result.errors?.length) {
            this.showToast(`Sync failed: ${result.errors[0]}`, 'error', 8000);
          } else {
            const details = [
              ...(result.cfDetails || []),
              ...(result.scoreDetails || []),
              ...(result.qualityDetails || []),
              ...(result.settingsDetails || [])
            ];
            this.showToast({
              title: `"${this.syncForm.profileName}" synced`,
              message: details.length > 0 ? `${details.length} change${details.length === 1 ? '' : 's'} applied.` : 'No changes.',
              details,
              type: 'info',
              duration: details.length > 0 ? 8000 : 4000,
              key: this.toastKey(
                'sync-imported', this.syncForm.instanceId, this.syncForm.profileName, details.length, details
              ),
            });
          }
        }
        this.syncResult = result;
        this.syncResultDetailsOpen = false;
        this.syncPlan = null;
        // Issue #52 — Save & Sync success: re-snapshot baseline so the
        // editor doesn't show as dirty after a clean save.
        this._captureProfileBaseline();
        // Reload Arr profiles first (new profile may have been created), then sync history + rules
        const inst = this.instances.find(i => i.id === this.syncForm.instanceId);
        if (inst) await this.loadInstanceProfiles(inst);
        await this.loadAutoSyncRules();
        await this.loadSyncHistory(this.syncForm.instanceId);
        // Skip the rule auto-update PUT when the sync had errors. The
        // backend already chose not to persist the bad overrides
        // (handleApply early-returns past the rule upsert on errors)
        // and may have auto-disabled the rule. Re-PUT'ing the form's
        // current overrides here would silently overwrite the rule's
        // selectedCFs / overrides / scoreOverrides with the same bad
        // data the backend just rejected, defeating the safeguard.
        const hadErrors = !!result?.error || (Array.isArray(result?.errors) && result.errors.length > 0);
        // Auto-update existing auto-sync rule with current settings
        const syncBody = this.buildSyncBody();
        const arrId = parseInt(this.syncForm.arrProfileId) || 0;
        const existingRule = arrId > 0
          ? this.autoSyncRules.find(r => r.instanceId === this.syncForm.instanceId && r.arrProfileId === arrId)
          : null;
        // Native profile sync success — surface a result toast and
        // close the editor (Apply & Sync UX: one-click commit, return
        // to where you came from). Imported profile flow already has
        // its own toast above; this branch handles the rule-attached
        // flow that previously left the user staring at a result
        // banner they had to dismiss via Close. Errors leave the
        // editor open so the user can see what failed + re-attempt.
        const closeEditorAfterApply = !this.syncForm.importedProfileId && !hadErrors;
        if (closeEditorAfterApply) {
          const details = [
            ...(result.cfDetails || []),
            ...(result.scoreDetails || []),
            ...(result.qualityDetails || []),
            ...(result.settingsDetails || [])
          ];
          this.showToast({
            title: `"${this.syncForm.profileName}" synced`,
            message: details.length > 0 ? `${details.length} change${details.length === 1 ? '' : 's'} applied.` : 'No changes — profile already in sync.',
            // Pass the full details list so the toast can render the
            // per-change breakdown inline (matches the imported-sync
            // toast). Without this, auto-closing the editor on Apply
            // & Sync left the user with no visibility of what
            // actually changed — they'd have to navigate to History
            // to see the breakdown.
            details,
            type: 'info',
            // Longer duration when there are details so the user has
            // time to read the list before it auto-dismisses.
            duration: details.length > 0 ? 12000 : 4000,
            // Dedupe key so rapid Apply & Sync clicks don't stack
            // duplicate "synced" toasts.
            key: this.toastKey('sync-native', this.syncForm.instanceId, this.syncForm.profileName, details.length),
          });
        }
        if (existingRule && !hadErrors) {
          const updated = {
            ...existingRule,
            selectedCFs: this.getAllSelectedCFIds(),
            // Echo whatever the sync body just sent — without this, the spread
            // of `...existingRule` (loaded BEFORE this sync ran) silently
            // overwrites the excludedCFs that /api/sync/apply just persisted
            // to the rule. Any opt-out the user made in the editor would be
            // erased on the very next post-sync PUT.
            excludedCFs: syncBody.excludedCFs || [],
            arrProfileId: arrId,
            behavior: this.syncForm.behavior || existingRule.behavior,
            overrides: syncBody.overrides || null,
            scoreOverrides: syncBody.scoreOverrides || null,
            qualityOverrides: syncBody.qualityOverrides || null,
            qualityStructure: syncBody.qualityStructure || null,
            // Echo whatever the sync body just sent (which is what the
            // backend actually applied + persisted to the rule). Falls
            // back to existingRule when buildSyncBody omitted the field
            // (today: same value, since buildSyncBody itself reads from
            // existingRule.keepArrCFIDs). Reading from syncBody first
            // future-proofs the path: if Profile Detail ever gains UI to
            // edit keepArrCFIDs, the user's change won't be silently
            // dropped here.
            keepArrCFIDs: (Array.isArray(syncBody.keepArrCFIDs) ? syncBody.keepArrCFIDs : existingRule.keepArrCFIDs) || null,
            // Description: echo whatever the sync body just sent.
            // buildSyncBody only includes the field when trimmed
            // pdDescription is non-empty, so explicit empty-string
            // fallback persists "user cleared their notes" correctly
            // — without this the spread of ...existingRule would
            // resurrect the old description.
            description: syncBody.description !== undefined ? syncBody.description : '',
          };
          try {
            await fetch(`/api/auto-sync/rules/${existingRule.id}`, {
              method: 'PUT',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify(updated)
            });
            await this.loadAutoSyncRules();
          } catch (e) { console.error('updateAutoSyncRule:', e); }
        }
        // Close editor after all post-sync work (rule PUT + reloads)
        // completes — guarantees the back-list reflects the persisted
        // state by the time the user sees it. Gated on the same
        // condition as the success toast above so error paths leave
        // the editor open for re-attempt. Clear the dirty-tracking
        // baseline first so closeProfileEditor's unsaved-changes guard
        // doesn't fire — Apply IS the save, by definition there's
        // nothing unsaved to warn about.
        if (closeEditorAfterApply) {
          this._clearProfileBaseline();
          this.closeProfileEditor();
        }
      } catch (e) {
        console.error('apply:', e);
      } finally {
        this.syncing = false;
      }
    },

    async applySync() {
      if (!this.profileDetail || !this.syncForm.instanceId) return;
      await this.startApply();
    },

    // Persist Profile Detail editor state to the existing rule WITHOUT
    // triggering a sync. Mirrors the rule-update half of startApply
    // (lines 1789-1822) but skips /api/sync/apply entirely. Backend bumps
    // UpdatedAt on the ?save_only=1 PUT so the Profiles tab can render
    // "● Unsynced changes" on the rule card until the next Sync All /
    // Sync Now / Auto-Sync run equalizes UpdatedAt with LastSyncTime.
    // Gated on _editLockedArrProfileId (only the edit-existing-rule flow
    // — Create New has no rule to save against, must Save & Sync first).
    async saveRuleOnly() {
      if (!this.profileDetail || !this.profileDetail.instance) return;
      const inst = this.profileDetail.instance;
      const profile = this.profileDetail.profile;
      const arrId = this.profileDetail._editLockedArrProfileId || 0;
      if (!arrId) {
        this.showToast('No existing rule to save — use Save & Sync or Create New first.', 'error', 8000);
        return;
      }
      const existingRule = this.autoSyncRules.find(r => r.instanceId === inst.id && r.arrProfileId === arrId);
      if (!existingRule) {
        this.showToast('Could not find rule to update. Refresh and try again.', 'error', 8000);
        return;
      }
      // Populate syncForm minimally so buildSyncBody can read its fields,
      // then restore — saveRuleOnly is independent of the Sync Profile
      // modal, no need to leave syncForm primed for an accidental
      // Save & Sync click.
      const prevSyncForm = this.syncForm;
      const prevSyncMode = this.syncMode;
      this.syncForm = {
        instanceId: inst.id,
        instanceName: inst.name,
        appType: inst.type,
        profileTrashId: profile.trashId,
        importedProfileId: existingRule.importedProfileId || '',
        profileName: profile.name,
        arrProfileId: String(arrId),
        newProfileName: profile.name,
        behavior: existingRule.behavior || { addMode: 'add_missing', removeMode: 'remove_custom', resetMode: 'reset_to_zero' }
      };
      this.syncMode = 'update';
      let syncBody;
      try {
        syncBody = this.buildSyncBody();
      } finally {
        this.syncForm = prevSyncForm;
        this.syncMode = prevSyncMode;
      }
      const updated = {
        ...existingRule,
        selectedCFs: syncBody.selectedCFs || [],
        excludedCFs: syncBody.excludedCFs || [],
        arrProfileId: arrId,
        behavior: syncBody.behavior || existingRule.behavior,
        overrides: syncBody.overrides || null,
        scoreOverrides: syncBody.scoreOverrides || null,
        qualityOverrides: syncBody.qualityOverrides || null,
        qualityStructure: syncBody.qualityStructure || null,
        keepArrCFIDs: (Array.isArray(syncBody.keepArrCFIDs) ? syncBody.keepArrCFIDs : existingRule.keepArrCFIDs) || null,
        description: syncBody.description !== undefined ? syncBody.description : '',
      };
      this.debugLog('UI', `Save (rule-only): "${profile.name}" → ${inst.name}`);
      this.savingRule = true;
      try {
        const r = await fetch(`/api/auto-sync/rules/${existingRule.id}?save_only=1`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(updated)
        });
        if (!r.ok) {
          let msg = 'Save failed';
          try { const data = await r.json(); if (data?.error) msg = data.error; } catch (_) {}
          if (r.status === 409) msg = 'Sync running on this instance — try again in a moment.';
          this.showToast(msg, 'error', 8000);
          return;
        }
        await this.loadAutoSyncRules();
        this.showToast('Changes applied. Will sync on next Update all / Update profile / Auto-sync.', 'success', 6000);
        // Auto-close the editor after a clean Apply — leaves the user
        // back where they came from (Sync Rules tab / TRaSH Profiles
        // grid). Pre-fix the user had to manually click Cancel to leave
        // even though they'd already committed their changes, which
        // also made "Cancel" read as "discard" right after a save.
        // Clear the dirty-tracking baseline first so closeProfileEditor's
        // unsaved-changes guard doesn't fire — Apply IS the save, the
        // editor state matches what was just persisted.
        this._clearProfileBaseline();
        this.closeProfileEditor();
      } catch (e) {
        console.error('saveRuleOnly:', e);
        this.showToast('Apply failed: ' + e.message, 'error', 8000);
      } finally {
        this.savingRule = false;
      }
    },

    // --- Quick Sync ---

    async quickSync(inst, sh, silent = false, useHistoryOnly = false) {
      // Pre-flight reachability check for interactive paths (per-rule "Sync
      // now" + rollback). Skipped when silent=true because Sync All already
      // pre-flights once before iterating, and we don't want to spam N
      // probes for N rules on the same instance.
      if (!silent) {
        try {
          const probe = await fetch(`/api/instances/${inst.id}/test`, { method: 'POST' });
          const probeBody = await probe.json().catch(() => ({}));
          if (!probe.ok || probeBody.connected === false) {
            const detail = probeBody.error || `${inst.name} is not reachable.`;
            this.showToast(`"${sh.profileName}" sync skipped — ${detail}`, 'error', 8000);
            this.setRuleSyncError(inst.id, sh.arrProfileId, detail);
            return { ok: false, name: sh.profileName, error: detail };
          }
        } catch (e) {
          const msg = `${inst.name} unreachable — ${e.message}`;
          this.showToast(`"${sh.profileName}" sync skipped — ${msg}`, 'error', 8000);
          this.setRuleSyncError(inst.id, sh.arrProfileId, msg);
          return { ok: false, name: sh.profileName, error: msg };
        }
      }
      // Look up the rule once — used for importedProfileId fallback AND
      // for the SelectedCFs source-of-truth lookup below.
      // useHistoryOnly bypasses rule lookup entirely: required for rollback
      // so prevEntry is the strict source of truth. Without this, rollback
      // would build the body from the (already-modified-by-the-just-undone-
      // sync) rule, push the current state to Arr, and produce a no-op —
      // user sees rollback succeed but next reset-to-default has nothing
      // to change because the profile was never actually rolled back.
      const rule = useHistoryOnly
        ? null
        : this.autoSyncRules.find(r => r.instanceId === inst.id && r.arrProfileId === sh.arrProfileId);
      // Fallback: check auto-sync rule for importedProfileId if missing from history (pre-1.7.1 migration)
      let importedProfileId = sh.importedProfileId || '';
      if (!importedProfileId && rule?.importedProfileId) {
        importedProfileId = rule.importedProfileId;
      }
      // Prefer the rule's SelectedCFs as the authoritative source. Sync
      // history's stored SelectedCFs may lag if the auto-sync no-changes
      // path skipped the history refresh, or if the rule was modified
      // outside the sync history flow. Fall back to sh.selectedCFs only
      // for orphaned profiles where no rule mapping exists.
      const selectedCFs = (rule && Array.isArray(rule.selectedCFs))
        ? rule.selectedCFs.slice()
        : Object.keys(sh.selectedCFs || {}).filter(k => sh.selectedCFs[k]);
      // Opt-outs follow the same prefer-rule-then-history pattern as selectedCFs
      // and keepArrCFIDs. Rollback (useHistoryOnly=true) sets rule=null, so the
      // history snapshot's excludedCFs is the only source — without this, every
      // rolled-back sync would silently re-include CFs the user had opted out of
      // at the time of the original sync.
      const excludedCFs = (rule && Array.isArray(rule.excludedCFs))
        ? rule.excludedCFs.slice()
        : (Array.isArray(sh.excludedCFs) ? sh.excludedCFs.slice() : []);
      const body = {
        instanceId: inst.id,
        profileTrashId: sh.profileTrashId,
        importedProfileId,
        arrProfileId: sh.arrProfileId,
        selectedCFs,
        excludedCFs,
        // Prefer the live rule when present; fall back to the sync-history
        // snapshot for orphaned-profile reruns (rule may have been deleted
        // since the original sync, but the snapshot still holds the keep
        // list so reset_to_zero doesn't wipe pinned customs). Empty / nil
        // for rules and entries created before this field existed →
        // omitempty on backend → reset_to_zero behaves exactly as before.
        keepArrCFIDs: (rule && rule.keepArrCFIDs) || sh.keepArrCFIDs || null,
        // expandRule: true tells the backend to run brand-new-group
        // expansion (ExpandSelectedCFsForBrandNewGroups) before building
        // the plan. Set when this is a Sync All path with a real rule
        // backing it, so manual Sync All matches scheduled auto-sync's
        // TRaSH-restructure handling. Skip when no rule (orphaned
        // sync-history rerun) or imported-profile case.
        expandRule: !!(rule && rule.id && !rule.importedProfileId),
        scoreOverrides: (rule && rule.scoreOverrides) || sh.scoreOverrides || null,
        qualityOverrides: (rule && rule.qualityOverrides) || sh.qualityOverrides || null,
        qualityStructure: (rule && rule.qualityStructure) || sh.qualityStructure || null,
        overrides: (rule && rule.overrides) || sh.overrides || null,
        behavior: (rule && rule.behavior) || sh.behavior || null
      };
      try {
        const r = await fetch('/api/sync/apply', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body)
        });
        const result = await r.json();
        if (result.error) {
          if (!silent) this.showToast(`"${sh.profileName}" sync failed: ${result.error}`, 'error', 8000);
          this.setRuleSyncError(inst.id, sh.arrProfileId, result.error);
          return { ok: false, name: sh.profileName, error: result.error };
        }
        if (result.errors?.length) {
          if (!silent) this.showToast(`"${sh.profileName}" sync failed: ${result.errors[0]}`, 'error', 8000);
          this.setRuleSyncError(inst.id, sh.arrProfileId, result.errors[0]);
          return { ok: false, name: sh.profileName, error: result.errors[0] };
        }
        const details = [
          ...(result.cfDetails || []),
          ...(result.scoreDetails || []),
          ...(result.qualityDetails || []),
          ...(result.settingsDetails || [])
        ];
        if (!silent) {
          this.showToast({
            title: `${inst.name} - "${sh.profileName}" synced`,
            message: details.length > 0 ? `${details.length} change${details.length === 1 ? '' : 's'} applied.` : 'No changes.',
            details,
            type: 'info',
            duration: details.length > 0 ? 8000 : 4000,
            key: this.toastKey('sync', inst.id, sh.arrProfileId, details.length, details),
          });
        }
        this.setRuleSyncError(inst.id, sh.arrProfileId, '');
        await this.loadSyncHistory(inst.id);
        // Reload rules so the "● unsynced" chip clears once the server has
        // equalized UpdatedAt with LastSyncTime on success.
        await this.loadAutoSyncRules();
        const summary = details.length > 0 ? details.slice(0, 3).join(', ') : 'no changes';
        return { ok: true, name: sh.profileName, summary, details };
      } catch (e) {
        if (!silent) this.showToast(`Sync error: ${e.message}`, 'error', 8000);
        this.setRuleSyncError(inst.id, sh.arrProfileId, e.message);
        return { ok: false, name: sh.profileName, error: e.message };
      }
    },

    async renameArrProfile(inst, sh, newName) {
      try {
        const r = await fetch(`/api/instances/${inst.id}/profiles/${sh.arrProfileId}/rename`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ name: newName })
        });
        if (!r.ok) {
          const err = await r.json().catch(() => ({}));
          this.showToast(`Rename failed: ${err.error || 'Unknown error'}`, 'error', 6000);
          return;
        }
        sh.arrProfileName = newName;
        this.showToast(`Renamed → "${newName}"`, 'info', 3000);
        await this.loadSyncHistory(inst.id);
      } catch (e) {
        this.showToast(`Rename error: ${e.message}`, 'error', 6000);
      }
    },

    cloneProfile(inst, sh) {
      // Open the clone-profile modal. Target instance defaults to the source
      // but can be changed to any other instance of the same app-type, so a
      // configured profile can be replicated to e.g. Radarr 4K in one step.
      this.cloneProfileModal = {
        open: true,
        sh,
        sourceInstanceId: inst.id,
        appType: inst.type,
        sourceName: sh.arrProfileName,
        name: sh.arrProfileName + ' (Copy)',
        targetInstanceId: inst.id,
        saving: false,
        error: ''
      };
    },

    cancelCloneProfile() {
      this.cloneProfileModal.open = false;
    },

    async confirmCloneProfile() {
      const m = this.cloneProfileModal;
      const name = (m.name || '').trim();
      if (!name) { m.error = 'Name is required'; return; }
      // Read the live <select> value as the source of truth. Alpine-binding the
      // selected value on a <select> with x-for-generated <option>s is
      // unreliable: the initial value can mismatch the displayed option, so a
      // user who keeps the default selection would otherwise submit a stale
      // value. Reading the chosen option directly at submit is WYSIWYG.
      const sel = document.getElementById('clone-profile-instance');
      const targetInstanceId = (sel && sel.value) || m.targetInstanceId;
      if (!targetInstanceId) { m.error = 'Pick a target instance'; return; }
      const sh = m.sh;
      m.saving = true;
      m.error = '';
      // Resolve importedProfileId from the SOURCE rule if the history entry
      // doesn't carry it. importedProfileId is app-type scoped, so it stays
      // valid on a same-app-type target.
      let importedProfileId = sh.importedProfileId || '';
      if (!importedProfileId) {
        const rule = this.autoSyncRules.find(r => r.instanceId === m.sourceInstanceId && r.arrProfileId === sh.arrProfileId);
        if (rule?.importedProfileId) importedProfileId = rule.importedProfileId;
      }
      const body = {
        instanceId: targetInstanceId,
        profileTrashId: sh.profileTrashId,
        importedProfileId,
        arrProfileId: 0, // create mode
        profileName: name,
        selectedCFs: Object.keys(sh.selectedCFs || {}).filter(k => sh.selectedCFs[k]),
        // Clone preserves the historical state including user opt-outs so the
        // copied profile reflects exactly what the source synced.
        excludedCFs: Array.isArray(sh.excludedCFs) ? sh.excludedCFs.slice() : [],
        scoreOverrides: sh.scoreOverrides || null,
        qualityOverrides: sh.qualityOverrides || null,
        qualityStructure: sh.qualityStructure || null,
        overrides: sh.overrides || null,
        behavior: sh.behavior || null
      };
      try {
        const r = await fetch('/api/sync/apply', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body)
        });
        const result = await r.json();
        if (result.error || result.errors?.length) {
          m.error = `Clone failed: ${result.error || result.errors[0]}`;
          m.saving = false;
          return;
        }
        const targetInst = this.instances.find(i => i.id === targetInstanceId);
        const targetLabel = targetInst ? targetInst.name : 'instance';
        m.open = false;
        m.saving = false;
        this.showToast(`Cloned "${sh.arrProfileName}" → "${name}" on ${targetLabel}`, 'info', 5000);
        await this.loadSyncHistory(targetInstanceId);
        await this.loadAutoSyncRules();
      } catch (e) {
        m.error = `Clone error: ${e.message}`;
        m.saving = false;
      }
    },

    async syncAllForInstance(inst, builderOnly = false) {
      // Iterate auto-sync RULES, not sync-history entries. Earlier this
      // function pulled the latest sync-history entry per arrProfileId
      // and re-played its overrides — but a failed sync's bad overrides
      // (e.g. unsatisfiable Min Score) were ALSO recorded in history,
      // so any subsequent Sync All would re-attempt the same bad data
      // and produce yet another bad-data entry, locking the user in a
      // loop even after they'd corrected the rule.
      //
      // The auto-sync rule is the user's saved intent (overrides,
      // selectedCFs, etc.). History is just the record. Sync All should
      // execute current intent, not replay broken attempts.
      const rules = this.autoSyncRules.filter(r => {
        if (r.instanceId !== inst.id || !r.enabled) return false;
        // Skip soft-tombstoned rules — their target Arr profile no longer
        // resolves, so the sync would 404/500. Restore-flow gating uses
        // the same predicate; Sync All shouldn't try harder than Restore.
        if (r.orphanedAt) return false;
        return builderOnly ? r.profileSource === 'imported' : r.profileSource !== 'imported';
      });
      if (!rules.length) {
        this.showToast(`Update all (${inst.name}): no profiles with auto-sync enabled`, 'warning', 4000);
        return;
      }
      // Pre-flight reachability check — bail out with ONE friendly toast
      // instead of iterating N rules and producing N copies of the same
      // "FAILED: ..." line in a single aggregate toast. Uses the existing
      // /api/instances/{id}/test endpoint which already returns a clear
      // "<instance> is not reachable" message for connection errors.
      try {
        const probe = await fetch(`/api/instances/${inst.id}/test`, { method: 'POST' });
        const probeBody = await probe.json().catch(() => ({}));
        if (!probe.ok || probeBody.connected === false) {
          const detail = probeBody.error || `${inst.name} is not reachable — check that the instance is running.`;
          this.showToast(`Update all (${inst.name}): ${detail}`, 'error', 8000);
          return;
        }
      } catch (e) {
        this.showToast(`Update all (${inst.name}): could not reach ${inst.name} — ${e.message}`, 'error', 8000);
        return;
      }
      // quickSync expects a sync-history-entry shape. Adapt each rule
      // into one without using the actual history. selectedCFs flips
      // from string[] (rule) to {[id]: bool} (history shape) so
      // quickSync's existing Object.keys(...).filter(...) call still
      // works without modification.
      const ruleToHistoryShape = (r) => {
        const cfMap = {};
        for (const id of (r.selectedCFs || [])) cfMap[id] = true;
        // Rules don't carry the Arr profile name (storage is by ID),
        // so resolve it from the live instance profile cache or the
        // sync-history fallback. Toast / Discord / log all read from
        // profileName so this lookup decides what users see.
        const arrName = this.resolveArrProfileName(r.instanceId, r.arrProfileId);
        const profileName = arrName
          ? `${arrName} (#${r.arrProfileId})`
          : `Arr profile #${r.arrProfileId}`;
        return {
          profileTrashId: r.trashProfileId || '',
          importedProfileId: r.importedProfileId || '',
          arrProfileId: r.arrProfileId,
          arrProfileName: arrName,
          profileName: profileName,
          selectedCFs: cfMap,
          scoreOverrides: r.scoreOverrides || null,
          qualityOverrides: r.qualityOverrides || null,
          qualityStructure: r.qualityStructure || null,
          overrides: r.overrides || null,
          behavior: r.behavior || null,
        };
      };
      const results = [];
      for (const rule of rules) {
        results.push(await this.quickSync(inst, ruleToHistoryShape(rule), true));
      }
      const details = results.flatMap(r => {
        if (!r.ok) return [`${r.name} - FAILED: ${r.error}`];
        if (r.details?.length > 0) return [r.name, ...r.details.map(detail => `  ${detail}`)];
        return [`${r.name} - no changes`];
      });
      const errors = results.filter(r => !r.ok).length;
      const toastType = errors === results.length ? 'error' : errors > 0 ? 'warning' : 'info';
      this.showToast({
        title: `Update all (${inst.name})`,
        message: errors > 0
          ? `${results.length - errors} succeeded, ${errors} failed.`
          : `${results.length} profile${results.length === 1 ? '' : 's'} synced.`,
        details,
        type: toastType,
        duration: 10000,
        key: this.toastKey(
          'sync-all',
          inst.id,
          results.length,
          errors,
          results.map(r => [r.name, r.ok, r.error || r.summary || ''])
        ),
      });
    },

    // --- Sync History ---

    async loadSyncHistory(instanceId) {
      try {
        const r = await fetch(`/api/instances/${instanceId}/sync-history`);
        if (r.ok) {
          const data = await r.json();
          this.syncHistory = { ...this.syncHistory, [instanceId]: data };
          // If the History tab has a profile expanded for this instance, refresh its entries too
          if (this.historyExpanded && this.historyExpanded.startsWith(instanceId + ':')) {
            const arrId = parseInt(this.historyExpanded.split(':')[1], 10);
            if (!isNaN(arrId)) this.loadProfileHistory(instanceId, arrId);
          }
        }
      } catch (e) { console.error('loadSyncHistory:', e); }
    },

    // Quick action modal — driven by status pill click. Lets the user
    // review what changed for THIS rule and act (Apply updates / Re-sync /
    // Sync now) without opening the full editor. Escape hatch button
    // opens the editor at the right tab for users who need more context.
    openStatusReview(inst, sh) {
      const rule = this.autoSyncRules.find(r => r.instanceId === inst.id && r.arrProfileId === sh.arrProfileId);
      const status = this.v3RuleStatus(rule);
      // Only the three actionable statuses get a modal; 'ok' / 'failed' / ''
      // already convey everything in the tooltip + sync history.
      if (!['updates', 'out-of-sync', 'pending'].includes(status)) return;
      this.statusReviewInst = inst;
      this.statusReviewSh = sh;
      this.statusReviewKind = status === 'out-of-sync' ? 'drift' : status;
      this.statusReviewOpen = true;
      // Pending needs a backend round-trip — the change list comes from
      // dry-run, not from pre-computed PendingChanges.
      if (this.statusReviewKind === 'pending') {
        this.fetchPendingSyncPlan(rule, inst);
      }
    },
    closeStatusReview() {
      this.statusReviewOpen = false;
      this.statusReviewApplying = false;
      this.statusReviewInst = null;
      this.statusReviewSh = null;
      this.statusReviewKind = '';
      this.statusReviewPendingPlan = null;
      this.statusReviewPendingLoading = false;
      this.statusReviewPendingError = '';
    },
    // Categorised change-list for the modal body. Shape is identical to
    // pdPendingByCategoryDrift/Trash so the modal template renders the
    // same section blocks regardless of kind.
    statusReviewChangesByCategory() {
      if (!this.statusReviewInst || !this.statusReviewSh) return { general: [], quality: [], cfs: [] };
      const rule = this.autoSyncRules.find(r => r.instanceId === this.statusReviewInst.id && r.arrProfileId === this.statusReviewSh.arrProfileId);
      if (!rule) return { general: [], quality: [], cfs: [] };
      if (this.statusReviewKind === 'drift') {
        return this._pdCategorizePending((rule.pendingChanges || []).filter(c => c.source === 'drift'));
      }
      if (this.statusReviewKind === 'updates') {
        return this._pdCategorizePending((rule.pendingChanges || []).filter(c => c.source !== 'drift'));
      }
      // pending — uses dry-run output instead of frontend diff because the
      // rule's persisted fields don't align with what SyncHistory stores
      // (opt-ins vs resolved set, score overrides routing differences).
      return this.syncPlanByCategory(this.statusReviewPendingPlan);
    },
    statusReviewTotalChanges() {
      const c = this.statusReviewChangesByCategory();
      return c.general.length + c.quality.length + c.cfs.length;
    },
    statusReviewTitle() {
      switch (this.statusReviewKind) {
        case 'drift':   return 'Arr drift detected';
        case 'updates': return 'TRaSH updates pending';
        case 'pending': return 'Unsynced changes';
        default:        return '';
      }
    },
    statusReviewSubtitle() {
      const inst = this.statusReviewInst;
      const sh = this.statusReviewSh;
      const appLabel = (inst?.type === 'sonarr') ? 'Sonarr' : 'Radarr';
      const profileName = sh?.arrProfileName || sh?.profileName || 'this profile';
      switch (this.statusReviewKind) {
        case 'drift':
          return `Someone edited ${profileName} directly in ${inst?.name || appLabel}. Re-syncing pushes Clonarr's saved state back, overwriting these edits.`;
        case 'updates':
          return `TRaSH-Guides has new commits affecting ${profileName}. Apply pulls the new data and syncs this rule.`;
        case 'pending':
          return `You saved changes to ${profileName} that haven't been pushed to ${inst?.name || appLabel} yet.`;
        default: return '';
      }
    },
    statusReviewActionLabel() {
      switch (this.statusReviewKind) {
        case 'drift':   return 'Re-sync now';
        case 'updates': return 'Apply updates';
        case 'pending': return 'Sync now';
        default:        return 'Apply';
      }
    },
    async statusReviewApply() {
      if (this.statusReviewApplying) return;
      const inst = this.statusReviewInst;
      const sh = this.statusReviewSh;
      const kind = this.statusReviewKind;
      if (!inst || !sh) return;
      this.statusReviewApplying = true;
      try {
        if (kind === 'updates') {
          // Pull TRaSH then sync this one rule. Same as row Update profile.
          await this.updateThisProfile(inst, sh);
        } else {
          // drift OR pending: push Clonarr's current target state to Arr.
          // No pull needed — neither case requires upstream data.
          await this.quickSync(inst, sh);
          await this.loadAutoSyncRules();
        }
        this.closeStatusReview();
      } catch (e) {
        // updateThisProfile/quickSync already surface their own error toasts.
        this.statusReviewApplying = false;
      }
    },
    async statusReviewOpenEditor() {
      const inst = this.statusReviewInst;
      const sh = this.statusReviewSh;
      const kind = this.statusReviewKind;
      this.closeStatusReview();
      if (!inst || !sh) return;
      await this.resyncProfile(inst, sh);
      // Jump to the tab carrying the status reason once editor renders.
      this.$nextTick(() => {
        if (kind === 'drift') this.spActiveTab = 'drift';
        else if (kind === 'updates') this.spActiveTab = 'updates';
        else this.spActiveTab = 'overview';
      });
    },

    async resyncProfile(inst, shArg) {
      // Always use the latest sync history entry for this profile — after rollback
      // or other changes, the passed-in sh may be stale (Alpine template reference).
      const freshHistory = (this.syncHistory[inst.id] || []).filter(h => h.arrProfileId === shArg.arrProfileId);
      const sh = freshHistory[0] || shArg;
      // Set target Arr profile for sync modal to pick up
      this.resyncTargetArrProfileId = sh.arrProfileId;
      // Imported/builder profile: open in Profile Builder editor
      if (sh.importedProfileId) {
        const allImported = this.importedProfiles[inst.type] || [];
        const imported = allImported.find(p => p.id === sh.importedProfileId);
        if (!imported) {
          this.showToast('Imported profile no longer available', 'error', 8000);
          return;
        }
        this.activeAppType = inst.type;
        this.currentSection = 'advanced';
        this.advancedTab = 'builder';
        this.editCustomProfile(inst.type, imported);
        return;
      }
      // TRaSH profile: find and open profile detail
      const profile = (this.trashProfiles[inst.type] || []).find(p => p.trashId === sh.profileTrashId);
      if (!profile) {
        this.showToast('Profile no longer available in TRaSH data', 'error', 8000);
        return;
      }
      // Navigate to profile detail with defaults
      this.activeAppType = inst.type;
      // restoreFromRule=true: this is the Edit-existing-rule entry point
      // (sync rules card → Edit pencil), so auto-restore from matching rule.
      await this.openProfileDetail(inst, profile, true);
      // Show which Arr profile this is synced to
      this.profileDetail._arrProfileName = sh.arrProfileName || null;
      // Lock the edit session to this Arr profile. resyncTargetArrProfileId
      // is consumed once per modal open in _loadSyncInstanceData, so on a
      // second Save & Sync (after Dry Run / Cancel) it would fall back to
      // 'create' mode. Persist the target on profileDetail (cleared when
      // user leaves the detail view) so every subsequent openSyncModal in
      // this edit session re-arms the lock. Bypass: openSyncModalAsNew.
      this.profileDetail._editLockedArrProfileId = sh.arrProfileId;
      // Look up the rule once. ruleData (declared just below) is the
      // authoritative source for the editor's current state: the user's
      // saved sync intent, which Save-only updates without touching sync
      // history. Without this preference, reopening the editor after
      // Save-only would show stale pre-save values pulled from sh.
      const ruleForRestore = this.autoSyncRules.find(r => r.instanceId === inst.id && r.arrProfileId === sh.arrProfileId);
      const ruleData = ruleForRestore || sh;
      // Restore optional CF selections from sync history
      if (sh.selectedCFs && Object.keys(sh.selectedCFs).length > 0) {
        const groups = this.profileDetail?.detail?.trashGroups || [];
        // priorAvailableGroups: per-group snapshot from last successful
        // sync. Distinguishes "user opted out of an existing group" from
        // "group is brand new since last sync (TRaSH restructure)".
        const priorAvailable = (ruleForRestore && ruleForRestore.priorAvailableGroups) || {};
        // priorSyncedCFs: per-CF snapshot from last successful sync — the
        // trash_ids that ended up in Arr regardless of how they got there
        // (SelectedCFs, formatItems direct path, or group expansion).
        // Used to distinguish "CF is new in this group since last sync
        // (TRaSH restructure — follow default)" from "CF existed and user
        // chose to leave it off (preserve opt-out)". Without this, NEW
        // default-on CFs inside KNOWN groups get silently flipped off when
        // the rule reopens.
        const priorSyncedCFs = (ruleForRestore && Array.isArray(ruleForRestore.priorSyncedCFs))
          ? new Set(ruleForRestore.priorSyncedCFs)
          : new Set();
        // Explicit opt-outs from the rule. Highest-priority signal — a
        // CF here is always off regardless of TRaSH defaults or prior
        // synced state. Persists across syncs and TRaSH restructures.
        const excludedCFs = (ruleForRestore && Array.isArray(ruleForRestore.excludedCFs))
          ? new Set(ruleForRestore.excludedCFs)
          : new Set();
        // The rule's SelectedCFs is the authoritative source of "what's
        // currently in the rule's sync set". sh.selectedCFs may lag if
        // the auto-sync no-changes path skipped the history refresh
        // (pre-fix data) or if the rule was modified outside the sync
        // history flow. When a rule is found, prefer its SelectedCFs;
        // fall back to sh.selectedCFs for orphaned / unmapped profiles.
        const effectiveSelectedCFs = (ruleForRestore && Array.isArray(ruleForRestore.selectedCFs))
          ? Object.fromEntries(ruleForRestore.selectedCFs.map(id => [id, true]))
          : sh.selectedCFs;
        for (const group of groups) {
          const groupExistedAtLastSync = group.trashId && (group.trashId in priorAvailable);
          // Group is considered "synced" if either:
          //   - any of its CFs are in the current rule.selectedCFs, OR
          //   - any of its CFs were in priorSyncedCFs (covers CFs that
          //     reached Arr via profile.formatItems instead of group
          //     opt-in, e.g. unwanted CFs before TRaSH restructure)
          const groupWasSynced = group.cfs.some(cf =>
            effectiveSelectedCFs[cf.trashId] || priorSyncedCFs.has(cf.trashId));
          // If the WHOLE group's defaults are excluded, the user
          // disabled the group — its excludedCFs entries are bulk
          // group-off signals, NOT per-CF Phase 2c locks. Treating
          // them as locks would leave the CFs frozen as opt-outs
          // even after the user re-enables the group, which is the
          // exact bug pdToggleGroup-off + the rehydrate's groupOffCFs
          // path are designed to avoid. Mirror that pass here.
          const defaultsInThisGroup = group.cfs.filter(cf => !cf.required && cf.default).map(cf => cf.trashId);
          const groupOffViaExcluded = group.defaultEnabled
            && defaultsInThisGroup.length > 0
            && defaultsInThisGroup.every(tid => excludedCFs.has(tid));
          for (const cf of group.cfs) {
            if (cf.required) continue;
            if (groupOffViaExcluded) {
              // Skip the per-CF=false write — the group flag below
              // expresses the off state. Per-CF stays undefined →
              // render falls back to cf.default on re-enable.
              continue;
            }
            if (excludedCFs.has(cf.trashId)) {
              // Explicit opt-out persists — always off, no exceptions.
              this.selectedOptionalCFs[cf.trashId] = false;
            } else if (effectiveSelectedCFs[cf.trashId]) {
              // CF is currently in the rule's selection → restore as on.
              this.selectedOptionalCFs[cf.trashId] = true;
            } else if (priorSyncedCFs.has(cf.trashId)) {
              // CF was synced last time (via formatItems, or as a group
              // default) but isn't in selectedCFs today. Restore as on
              // so a TRaSH structural restructure that moves CFs around
              // doesn't silently flip the user's effective state.
              this.selectedOptionalCFs[cf.trashId] = true;
            } else if (groupExistedAtLastSync || !group.defaultEnabled) {
              // Existing default-on group + CF wasn't synced before AND
              // isn't in selectedCFs → user has either explicitly opted
              // out OR has never engaged with this CF. Either way, off.
              //
              // Important: we deliberately do NOT auto-enable CFs whose
              // default flipped from false/null to true since last sync
              // (e.g. Black and White Editions becoming default=true in
              // unwanted-formats-german post-PR-2733). If we did, every
              // editor reopen would re-tick CFs the user just deselected.
              // New TRaSH defaults are surfaced as available but not
              // pre-checked — user opts in explicitly.
              this.selectedOptionalCFs[cf.trashId] = false;
            } else if (cf.default) {
              // Brand-new default-on group + CF is default-on within → on.
              this.selectedOptionalCFs[cf.trashId] = true;
            } else {
              // Brand-new default-on group but CF is opt-in within → off.
              this.selectedOptionalCFs[cf.trashId] = false;
            }
          }
          // Group toggle
          if (groupWasSynced) {
            this.selectedOptionalCFs['__grp_' + group.name] = true;
          } else if (group.defaultEnabled && groupExistedAtLastSync) {
            // Existing default-on group with no activity → user opted out → off.
            this.selectedOptionalCFs['__grp_' + group.name] = false;
          }
          // else: brand-new default-on group → leave undefined →
          // render fallback uses group.defaultEnabled (= true).
        }
        this.selectedOptionalCFs = { ...this.selectedOptionalCFs };
      }
      // Restore Additional CF opt-ins — CFs in rule.selectedCFs that
      // DON'T live in any profile-default trashGroup (e.g. HDR, DV,
      // Flux, DD+/DD+ ATMOS for a profile whose default scope doesn't
      // include those groups). The optional-CF loop above only walks
      // profile-default groups, so these opt-ins were silently
      // dropped on every Edit-from-Sync-Rules → Apply & Sync cycle
      // (rule was rewritten without them → backend's reset_to_zero
      // pass zeroed the corresponding Arr profile entries).
      // Source: rule.selectedCFs first (saved sync intent), fall back
      // to sh.selectedCFs for orphaned rules.
      const additionalCFsSource = (ruleForRestore && Array.isArray(ruleForRestore.selectedCFs))
        ? Object.fromEntries(ruleForRestore.selectedCFs.map(id => [id, true]))
        : (sh.selectedCFs || {});
      if (additionalCFsSource && Object.keys(additionalCFsSource).length > 0) {
        const inProfileGroups = new Set();
        for (const g of (this.profileDetail?.detail?.trashGroups || [])) {
          for (const cf of (g.cfs || [])) inProfileGroups.add(cf.trashId);
        }
        const selWithExtras = { ...this.selectedOptionalCFs };
        for (const tid of Object.keys(additionalCFsSource)) {
          if (inProfileGroups.has(tid)) continue;
          // Don't overwrite a Phase 2c lock or other explicit false.
          if (selWithExtras[tid] === false) continue;
          selWithExtras[tid] = true;
        }
        this.selectedOptionalCFs = selWithExtras;
      }
      // Phase 2c — restore required-CF exclusions from rule.ExcludedCFs.
      // The optional-CF restore loop above intentionally skips required
      // CFs (their inclusion is driven by group state). But Phase 2c
      // lets users opt out of required CFs via the lock-icon UI, so the
      // rule can carry excludedCFs entries that target required CFs
      // (both formatItemNames and required-in-group). Without this
      // restore step, the editor reopens with the exclusion silently
      // dropped from sel — the Diffs view + lock-icon visuals show the
      // CF as included even though the rule on disk still excludes it.
      // applyRuleStateToEditor handles this for the single-matching-
      // rule case via openProfileDetail; this is the resyncProfile path
      // that runs after that and must not undo it. Belt-and-braces: we
      // re-apply the rule's excludedCFs here so the state is consistent
      // regardless of which load path ran.
      if (ruleForRestore && Array.isArray(ruleForRestore.excludedCFs)) {
        // Mirror the groupOffViaExcluded skip from the per-group
        // loop above — when a default-on group is wholly disabled
        // its required+default CFs land in excludedCFs as the bulk
        // group-off signal. Writing sel=false here would freeze them
        // as Phase 2c locks (shadowing cf.default on re-enable),
        // exactly the bug pdToggleGroup-off avoids.
        const groupOffCFs = new Set();
        for (const g of (this.profileDetail?.detail?.trashGroups || [])) {
          if (!g.defaultEnabled) continue;
          const defaultsInGroup = (g.cfs || []).filter(cf => cf.required || cf.default).map(cf => cf.trashId);
          if (defaultsInGroup.length === 0) continue;
          if (defaultsInGroup.every(tid => ruleForRestore.excludedCFs.includes(tid))) {
            for (const tid of defaultsInGroup) groupOffCFs.add(tid);
          }
        }
        const sel = { ...this.selectedOptionalCFs };
        for (const tid of ruleForRestore.excludedCFs) {
          if (groupOffCFs.has(tid)) continue;
          sel[tid] = false;
        }
        this.selectedOptionalCFs = sel;
      }
      // Restore overrides. ruleData prefers the rule (saved intent) over
      // sync history, so Save-only edits are visible on reopen. Values are
      // written to pdOverrides; pdOverridesEnabled flips on at the end if
      // ANY override was found.
      let anyOverride = false;
      if (ruleData.overrides) {
        const ov = ruleData.overrides;
        if (ov.language !== undefined) { this.pdOverrides.language.enabled = false; this.pdOverrides.language.value = ov.language; anyOverride = true; }
        if (ov.minFormatScore !== undefined) { this.pdOverrides.minFormatScore.enabled = false; this.pdOverrides.minFormatScore.value = ov.minFormatScore; anyOverride = true; }
        if (ov.minUpgradeFormatScore !== undefined) { this.pdOverrides.minUpgradeFormatScore.enabled = false; this.pdOverrides.minUpgradeFormatScore.value = ov.minUpgradeFormatScore; anyOverride = true; }
        if (ov.cutoffFormatScore !== undefined) { this.pdOverrides.cutoffFormatScore.enabled = false; this.pdOverrides.cutoffFormatScore.value = ov.cutoffFormatScore; anyOverride = true; }
        if (ov.upgradeAllowed !== undefined) { this.pdOverrides.upgradeAllowed.enabled = false; this.pdOverrides.upgradeAllowed.value = ov.upgradeAllowed; anyOverride = true; }
        if (ov.cutoffQuality !== undefined) { this.pdOverrides.cutoffQuality = ov.cutoffQuality; anyOverride = true; }
      }
      // Determine which trashIDs are part of the TRaSH base profile. Used by
      // both the Extra-CF split below AND the Overridden-Scores filter.
      // Without this set, we can't tell "user overrode this profile CF's
      // score" from "user added this CF as Extra with its default score".
      const inProfile = new Set();
      for (const fi of (this.profileDetail?.detail?.formatItemNames || [])) inProfile.add(fi.trashId);
      for (const g of (this.profileDetail?.detail?.trashGroups || [])) {
        for (const cf of g.cfs) inProfile.add(cf.trashId);
      }

      // Split ruleData.scoreOverrides into (Extra CF) vs (base-profile override).
      // Rule: if trashID is NOT in the base profile, it's an Extra — belongs
      // in extraCFs, NOT cfScoreOverrides. Otherwise it's a base-profile
      // override, and only kept if score differs from TRaSH default (prevents
      // "false-positive" overrides with `default → default` rows that
      // reappear after every refresh). "Also selected" guard uses
      // effectiveSelectedCFs (rule-preferring) defined earlier — fall back
      // to sh.selectedCFs when no rule (orphaned).
      const effectiveSelOpt = (ruleForRestore && Array.isArray(ruleForRestore.selectedCFs))
        ? Object.fromEntries(ruleForRestore.selectedCFs.map(id => [id, true]))
        : (sh.selectedCFs || {});
      const extras = {};
      const baseOverrides = {};
      if (ruleData.scoreOverrides) {
        for (const [tid, v] of Object.entries(ruleData.scoreOverrides)) {
          if (!inProfile.has(tid)) {
            // Only add to extras if also selected.
            if (effectiveSelOpt[tid]) {
              extras[tid] = v;
            }
            continue;
          }
          const def = this.resolveCFDefaultScore(tid);
          // Keep only if score differs from TRaSH default. When default can't
          // be resolved (older data, missing profile context), keep the entry
          // — we can't prove it's redundant.
          if (def === '?' || v !== def) {
            baseOverrides[tid] = v;
          }
        }
      }
      this.cfScoreOverrides = baseOverrides;
      if (Object.keys(baseOverrides).length > 0) anyOverride = true;

      // Restore quality overrides — prefer structure override over legacy flat map.
      // qualityOverrideActive is the Quality Items editor modal-open flag and
      // must NOT be set here, otherwise the editor would auto-open on every
      // Profile Detail open for any rule with Quality overrides.
      if (ruleData.qualityStructure && ruleData.qualityStructure.length > 0) {
        this.qualityStructure = ruleData.qualityStructure.map(it => {
          const out = { _id: ++this._qsIdCounter, name: it.name, allowed: !!it.allowed };
          if (it.items && it.items.length > 0) out.items = [...it.items];
          return out;
        });
        anyOverride = true;
        // If profile-default cutoff is not in the overridden structure, pick first allowed
        const defaultCutoff = this.profileDetail?.detail?.profile?.cutoff || '';
        if (!this.pdOverrides.cutoffQuality && defaultCutoff) {
          const inStructure = this.qualityStructure.some(it => it.name === defaultCutoff && it.allowed !== false);
          if (!inStructure) {
            const firstAllowed = this.qualityStructure.find(it => it.allowed !== false);
            if (firstAllowed) this.pdOverrides.cutoffQuality = firstAllowed.name;
          }
        }
      } else if (ruleData.qualityOverrides && Object.keys(ruleData.qualityOverrides).length > 0) {
        this.qualityOverrides = { ...ruleData.qualityOverrides };
        anyOverride = true;
      }
      // Apply the Extra CFs computed above.
      if (Object.keys(extras).length > 0) {
        this.extraCFs = extras;
        anyOverride = true;
        // Sync Preview's Additional CF picker + Profile overview's
        // Additional CF + Diffs bucket 3 all read selectedOptionalCFs
        // (NOT extraCFs) to decide whether an extra is activated.
        // applyRuleStateToEditor sets sel[extras] = true via
        // rule.selectedCFs, but only fires for the single-matching-
        // rule case (multi-Arr profiles sharing a TRaSH profile skip
        // it). The optional-CF restore loop above only visits
        // profile.trashGroups, never extras. Result: extras land in
        // extraCFs but NOT in sel, so Sync Preview's UI sees them as
        // "not activated". Belt-and-braces: write sel[id] = true for
        // every extra so both Classic (extraCFs) and Sync Preview
        // (sel) agree.
        const selWithExtras = { ...this.selectedOptionalCFs };
        for (const tid of Object.keys(extras)) {
          selWithExtras[tid] = true;
        }
        this.selectedOptionalCFs = selWithExtras;
        // Load all CFs for the browser
        const appType = this.profileDetail?.instance?.type;
        if (appType) this.loadExtraCFList();
      }
      // Phase 2c — excludedCFs (required opt-outs + default-on opt-outs)
      // counts as customization. Without this, a rule whose only edit is
      // an excluded required CF reopens with Customize off and the
      // "Customize this profile" CTA — even though the rule clearly IS
      // customized (Sync Rules pill correctly shows N excluded). Mirror
      // backend ComputeRuleCustomizations bucket 4 / frontend
      // pdExcludedCFCount: count entries that resolve to default CFs.
      //
      // Defensive: if profileDetail.detail hasn't loaded yet (async race
      // — shouldn't happen since resyncProfile awaits openProfileDetail,
      // but cheap to guard), conservatively flip anyOverride=true so the
      // rule's saved-customization state isn't lost. The downstream
      // diff/badge counters will reconcile correctly once detail loads.
      if (Array.isArray(ruleData.excludedCFs) && ruleData.excludedCFs.length > 0) {
        const detail = this.profileDetail?.detail;
        if (!detail) {
          anyOverride = true;
        } else {
          const defaults = this.computeTrashDefaults();
          for (const tid of ruleData.excludedCFs) {
            if (defaults.has(tid)) { anyOverride = true; break; }
          }
        }
      }
      // Restore behavior — prefer rule's behavior (current intent) over sync
      // history (last applied).
      if (ruleData.behavior) {
        this.syncForm.behavior = { ...this.syncForm.behavior, ...ruleData.behavior };
      }
      // Restore the rule's free-form notes. ruleForRestore is preferred
      // (current rule state) over sh (last-synced history snapshot) so
      // Save-only notes edits are visible on reopen. Auto-expand the
      // Notes card when notes exist (same UX rationale as
      // applyRuleStateToEditor).
      this.pdDescription = (ruleForRestore?.description || ruleData.description || '');
      this.pdNotesExpanded = !!(this.pdDescription || '').trim();
      // Auto-enable the Profile Detail overrides toggle if ANY override was
      // restored, so the UI reflects the saved state of the rule (no "All
      // values follow profile defaults" lie when there are real overrides).
      // Also flip on when the rule carries selectedCFs OR excludedCFs —
      // Additional CF opt-ins (Flux/DD+/HDR for profiles whose default
      // scope excludes them) live in selectedCFs, and Phase 2c lock-clicks
      // live in excludedCFs. Both count as user customizations even when
      // basics/quality/score overrides are absent. Mirrors the equivalent
      // gate in applyRuleStateToEditor (~line 933).
      const hasSelectedCFs = ruleForRestore && Array.isArray(ruleForRestore.selectedCFs) && ruleForRestore.selectedCFs.length > 0;
      const hasExcludedCFs = ruleForRestore && Array.isArray(ruleForRestore.excludedCFs) && ruleForRestore.excludedCFs.length > 0;
      if (anyOverride || hasSelectedCFs || hasExcludedCFs) this.pdOverridesEnabled = true;
      // Issue #52 — snapshot the just-restored state as the dirty-check
      // baseline. Anything the user changes after this point is an
      // unsaved edit that warrants a Stay/Discard prompt on navigation.
      this._captureProfileBaseline();
    },

    async removeSyncHistory(instanceId, arrProfileId) {
      // Build a descriptive message — name the instance + Arr profile + ID
      // so the user can tell which rule they're about to delete (especially
      // important when multiple rules exist for the same TRaSH profile
      // across instances).
      const inst = this.instances.find(i => i.id === instanceId);
      const instName = (inst && inst.name) || 'instance';
      const arrName = this.resolveArrProfileName(instanceId, arrProfileId) || `profile #${arrProfileId}`;
      const rule = this.autoSyncRules.find(r => r.instanceId === instanceId && r.arrProfileId === arrProfileId);
      const ruleSuffix = rule ? '' : ' (no active rule — only the history entry will be deleted)';
      const message = `Delete the sync rule for "${arrName}" (#${arrProfileId}) on ${instName}, plus its sync history entry?${ruleSuffix}\n\nThis does NOT delete the profile from ${instName}.`;
      const confirmed = await new Promise(resolve => {
        this.confirmModal = { show: true, title: 'Remove Sync Rule', message, confirmLabel: 'Remove', onConfirm: () => resolve(true), onCancel: () => resolve(false) };
      });
      if (!confirmed) return;
      try {
        const resp = await fetch(`/api/instances/${instanceId}/sync-history/${arrProfileId}`, { method: 'DELETE' });
        if (!resp.ok) { console.error('removeSyncHistory: HTTP', resp.status); }
        // Also remove associated auto-sync rule (looked up above for the
        // confirm-message; reuse it here so we don't double-find).
        if (rule) {
          await fetch(`/api/auto-sync/rules/${rule.id}`, { method: 'DELETE' });
          await this.loadAutoSyncRules();
        }
        await this.loadSyncHistory(instanceId);
      } catch (e) { console.error('removeSyncHistory:', e); }
    },

    // Restore an orphaned sync rule: re-create the profile in Arr from the
    // last synced state. Surfaces 409 name-collision errors via a rename
    // prompt loop until the user provides a unique name or cancels.
    async restoreOrphanedRule(inst, sh) {
      const rule = this.autoSyncRules.find(r => r.instanceId === inst.id && r.arrProfileId === sh.arrProfileId);
      if (!rule) {
        this.showToast('Cannot restore: sync rule no longer exists', 'error', 6000);
        return;
      }
      const cfCount = (sh.syncedCFs || []).length;
      const scoreCount = Object.keys(sh.scoreOverrides || {}).length;
      const baseMessage = `Recreate "${sh.profileName}" in ${inst.name} from the last synced state?\n\n${cfCount} custom format${cfCount === 1 ? '' : 's'}, ${scoreCount} score override${scoreCount === 1 ? '' : 's'}, plus saved quality items and settings will be re-pushed. A new ArrProfileID will be assigned.`;
      const confirmed = await new Promise(resolve => {
        this.confirmModal = { show: true, title: 'Restore Profile', message: baseMessage, confirmLabel: 'Restore', onConfirm: () => resolve(true), onCancel: () => resolve(false) };
      });
      if (!confirmed) return;

      let newName = '';
      // Loop on 409 collision until success or user cancels.
      for (;;) {
        const body = newName ? JSON.stringify({ newName }) : '';
        const resp = await fetch(`/api/auto-sync/rules/${rule.id}/restore`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body,
        });
        if (resp.status === 409) {
          const conflict = await resp.json().catch(() => ({}));
          const promptMsg = `${conflict.error || 'Name conflict'}\n\nProvide a different name to use for the restored profile.`;
          newName = await new Promise(resolve => {
            this.inputModal = {
              show: true,
              title: 'Profile Name Conflict',
              message: promptMsg,
              value: conflict.suggested || '',
              placeholder: 'New profile name',
              confirmLabel: 'Restore with this name',
              onConfirm: (val) => resolve((val || '').trim()),
              onCancel: () => resolve(null),
            };
          });
          if (!newName) return; // user cancelled
          continue; // retry with new name
        }
        if (!resp.ok) {
          const err = await resp.json().catch(() => ({}));
          this.showToast('Restore failed: ' + (err.error || resp.status), 'error', 8000);
          return;
        }
        const result = await resp.json();
        this.showToast(`Restored "${result.arrProfileName}" — ${result.cfsCreated} CF${result.cfsCreated === 1 ? '' : 's'} created, ${result.scoresUpdated} score${result.scoresUpdated === 1 ? '' : 's'} set`, 'info', 6000);
        await this.loadAutoSyncRules();
        await this.loadSyncHistory(inst.id);
        return;
      }
    },

    // Deduplicate sync history to latest entry per arrProfileId (entries are newest-first
    // from backend). Then apply optional column sort.
    sortedSyncRules(instId) {
      const all = (this.syncHistory[instId] || []).filter(sh => !sh.importedProfileId);
      const seen = new Set();
      const rules = [];
      for (const sh of all) {
        if (!seen.has(sh.arrProfileId)) {
          seen.add(sh.arrProfileId);
          rules.push(sh);
        }
      }
      const col = this.syncRulesSort.col || 'arr';
      const dir = this.syncRulesSort.dir === 'desc' ? -1 : 1;
      // Status sort order: failed > drift > pending > ok (most-urgent first
      // when sorting ascending — matches "show me what needs attention").
      const statusOrder = { failed: 0, pending: 1, updates: 2, 'out-of-sync': 3, ok: 4 };
      // Pre-index this instance's rules by arrProfileId so the comparator
      // is O(1) per call. Without this, lastSync / status sorts do
      // .find() twice per compare → O(N² log N) on each sort.
      let ruleByArrID = null;
      if (col === 'lastSync' || col === 'status') {
        ruleByArrID = new Map();
        for (const r of this.autoSyncRules) {
          if (r.instanceId === instId) ruleByArrID.set(r.arrProfileId, r);
        }
      }
      return [...rules].sort((a, b) => {
        switch (col) {
          case 'trash': return dir * (a.profileName || '').localeCompare(b.profileName || '');
          case 'arr':   return dir * (a.arrProfileName || '').localeCompare(b.arrProfileName || '');
          case 'lastSync': {
            const ar = ruleByArrID.get(a.arrProfileId);
            const br = ruleByArrID.get(b.arrProfileId);
            const at = ar?.lastSyncTime ? new Date(ar.lastSyncTime).getTime() : 0;
            const bt = br?.lastSyncTime ? new Date(br.lastSyncTime).getTime() : 0;
            return dir * (at - bt);
          }
          case 'status': {
            const ar = ruleByArrID.get(a.arrProfileId);
            const br = ruleByArrID.get(b.arrProfileId);
            const av = statusOrder[this.v3RuleStatus(ar)] ?? 99;
            const bv = statusOrder[this.v3RuleStatus(br)] ?? 99;
            return dir * (av - bv);
          }
          default: return dir * (a.arrProfileName || '').localeCompare(b.arrProfileName || '');
        }
      });
    },

    // Per-rule status badge. Pending and out-of-sync share the orange
    // colour (same urgency family) but stay separately labelled so the
    // user knows at a glance what action resolves the state.
    //   'failed'      — lastSyncError set. Red. Wins over everything else
    //                   so a broken sync doesn't get buried; tooltip
    //                   exposes the error.
    //   'pending'     — user saved local overrides without syncing
    //                   (updatedAt > lastSyncTime). Orange. Resolved by
    //                   clicking Sync.
    //   'out-of-sync' — upstream TRaSH changes affect this rule's CFs
    //                   (pendingChanges populated by Profile Sync
    //                   detection). Orange. Resolved by clicking Pull.
    //                   Future Arr-side drift (Phase D) joins this state.
    //   'ok'          — last sync succeeded and nothing pending.
    //   ''            — no rule object (orphaned display path).
    v3RuleStatus(rule) {
      if (!rule) return '';
      if (rule.lastSyncError) return 'failed';
      // Four distinct "needs attention" states share the orange colour
      // (same urgency family) but stay separately labelled so the user
      // can tell at a glance what CAUSE drives the state and what
      // action resolves it:
      //   pending      — YOU edited locally without syncing
      //   out-of-sync  — ARR-side drift: someone edited the profile
      //                  directly in Radarr/Sonarr, current state no
      //                  longer matches what clonarr would push
      //   updates      — TRaSH-Guides UPSTREAM has changes pending
      // Precedence (most-actionable first):
      //   pending > out-of-sync > updates
      // Pending wins because the user already committed to a change
      // but hasn't pushed it yet. Out-of-sync wins over updates
      // because external interference with the Arr profile is more
      // urgent than upstream TRaSH publishing new content the user
      // can apply at leisure. Tooltips surface secondary state so
      // nothing is hidden.
      const hasUnpushedEdits = rule.updatedAt && (!rule.lastSyncTime || rule.updatedAt > rule.lastSyncTime);
      if (hasUnpushedEdits) return 'pending';
      const hasDrift = !!(rule.watchState && rule.watchState.lastDriftFingerprint);
      if (hasDrift) return 'out-of-sync';
      // Updates: pendingChanges with TRaSH source (filter out drift
      // entries so a reconciled-but-still-tracked-via-PendingChanges
      // rule doesn't masquerade as having upstream updates).
      const trashPending = (rule.pendingChanges || []).filter(pc => pc.source !== 'drift');
      if (trashPending.length > 0) return 'updates';
      return 'ok';
    },
    v3RuleStatusLabel(rule) {
      switch (this.v3RuleStatus(rule)) {
        case 'ok':           return 'In sync';
        case 'pending':      return 'Pending';
        case 'updates':      return 'Updates';
        case 'out-of-sync':  return 'Arr drift';
        case 'failed':       return 'Failed';
        default:             return '—';
      }
    },
    v3RuleStatusTip(rule) {
      switch (this.v3RuleStatus(rule)) {
        case 'ok':     return 'Last sync matched the target Arr profile';
        case 'failed': return rule?.lastSyncError || 'Last sync attempt failed';
        case 'out-of-sync': {
          // Drift: someone edited the Arr profile directly. Tooltip
          // names the affected CFs / settings / quality items so the
          // user can see WHY the row is out of sync without having
          // to open the editor.
          const driftPC = (rule.pendingChanges || []).filter(pc => pc.source === 'drift');
          if (driftPC.length === 0) {
            return 'Arr-side drift detected. Something changed in the Arr profile since the last sync. Click to review and re-sync.';
          }
          const cfs = driftPC.filter(pc => pc.changeType === 'cf-modified').map(pc => pc.affectedName || pc.affectedId);
          const settings = driftPC.filter(pc => pc.changeType === 'profile-modified').map(pc => pc.affectedName || pc.affectedId);
          const qualities = driftPC.filter(pc => pc.changeType === 'qs-modified').map(pc => pc.affectedName || pc.affectedId);
          const fmt = (items, label, max = 6) => {
            if (items.length === 0) return null;
            const shown = items.slice(0, max);
            const more = items.length - shown.length;
            const tail = more > 0 ? `, +${more} more` : '';
            return `${label}: ${shown.join(', ')}${tail}`;
          };
          const lines = [`Someone edited this profile in ${rule.instanceType === 'sonarr' ? 'Sonarr' : 'Radarr'} directly:`];
          const cfLine = fmt(cfs, 'CFs');
          const setLine = fmt(settings, 'Settings');
          const qLine = fmt(qualities, 'Quality');
          if (cfLine) lines.push(cfLine);
          if (setLine) lines.push(setLine);
          if (qLine) lines.push(qLine);
          lines.push('', 'Click to review and re-sync.');
          // Mention TRaSH updates too if they exist alongside drift.
          const trashCount = (rule.pendingChanges || []).filter(pc => pc.source !== 'drift').length;
          if (trashCount > 0) {
            lines.push('', `Also: TRaSH updated ${trashCount} CF${trashCount === 1 ? '' : 's'} affecting this profile.`);
          }
          return lines.join('\n');
        }
        case 'pending': {
          // Tooltip can't run an async dry-run, so it stays generic — the
          // modal (opened via click) loads the actual change list from
          // /api/sync/dry-run. Earlier attempt to diff client-side here
          // produced false positives (rule.selectedCFs is opt-ins only,
          // SyncHistory.selectedCFs is the resolved set).
          const lines = [
            'You saved changes to this rule that haven\'t been synced to Arr yet.',
            '',
            'Click to review what will change and sync.',
          ];
          const trashCount = (rule.pendingChanges || []).filter(pc => pc.source !== 'drift').length;
          const driftCount = (rule.pendingChanges || []).filter(pc => pc.source === 'drift').length;
          if (trashCount > 0) lines.push('', `Also: TRaSH updated ${trashCount} CF${trashCount === 1 ? '' : 's'} affecting this profile.`);
          if (driftCount > 0) lines.push((trashCount > 0 ? '' : ''), `Also: ${driftCount} Arr-side change${driftCount === 1 ? '' : 's'} detected.`);
          return lines.join('\n');
        }
        case 'updates': {
          const pc = rule.pendingChanges || [];
          // Resolve CF names from local CF cache (backend populates
          // AffectedName at detection-time, but legacy entries written
          // before that fix have it empty — this fallback covers them).
          const appType = rule.instanceType || '';
          const cfs = (this.cfBrowseData && this.cfBrowseData[appType] && this.cfBrowseData[appType].cfs) || [];
          const nameByID = new Map();
          for (const cf of cfs) nameByID.set(cf.trash_id, cf.name);
          const resolveName = c => c.affectedName || nameByID.get(c.affectedId) || c.affectedId || 'unknown';
          const sorted = [...pc].sort((a, b) => (b.detectedAt || '').localeCompare(a.detectedAt || ''));
          const shown = sorted.slice(0, 8);
          const lines = [`TRaSH updated ${pc.length} CF${pc.length === 1 ? '' : 's'} in this profile since last sync:`, ''];
          for (const c of shown) lines.push('• ' + resolveName(c));
          if (sorted.length > shown.length) lines.push(`...and ${sorted.length - shown.length} more`);
          lines.push('', 'Click to review and apply.');
          return lines.join('\n');
        }
        default: return '';
      }
    },

    // Customizations pill — reads from the ruleCustomizations cache
    // populated by loadRuleCustomizations() on Sync Rules tab mount.
    // Backend (core.ComputeRuleCustomizations) does the actual diff
    // against TRaSH defaults; the frontend just reads the result.
    //
    // Returns an empty (all-zero) object until the cache loads, which
    // the markup renders as "—". Once loaded, the breakdown matches
    // what the detail view's "Override mode · N changes" header shows
    // for the same rule.
    v3RuleCustomizations(rule) {
      const empty = { total: 0, quality: 0, extraCFs: 0, customScores: 0, general: 0, excludedCFs: 0 };
      if (!rule || !this.ruleCustomizationsLoaded) return empty;
      return this.ruleCustomizations[rule.id] || empty;
    },

    // Load per-rule customization counts from the backend. Called when
    // the user navigates to the Sync Rules tab; results cache in
    // ruleCustomizations until the next call. Refresh after Save+Sync
    // is wired separately via the existing autoSyncRules reload chain.
    async loadRuleCustomizations() {
      try {
        const r = await fetch('/api/auto-sync/rules/customizations');
        if (!r.ok) return;
        const data = await r.json();
        this.ruleCustomizations = data || {};
        this.ruleCustomizationsLoaded = true;
      } catch (e) {
        // Network error — keep whatever's in the cache. Pill renders "—"
        // for entries we don't have.
        console.error('loadRuleCustomizations:', e);
      }
    },

    // Fetch the per-CF view that drives the Sync Rules → Custom Formats
    // sub-tab. Lazy-loaded: only fires on first click of the Custom
    // Formats sub-tab (not on tab mount), so users who never visit the
    // sub-tab don't pay the per-instance ArrCF list cost. Re-runs after
    // an Apply succeeds so the row's status pill refreshes without
    // requiring the user to navigate away and back.
    async loadCFSyncRules(appType) {
      if (!appType) return;
      try {
        const r = await fetch(`/api/cf-sync-rules/${appType}`);
        if (!r.ok) return;
        const data = await r.json();
        this.cfSyncRules[appType] = data.items || [];
        this.cfSyncRulesLoaded[appType] = true;
      } catch (e) {
        console.error('loadCFSyncRules:', e);
      }
    },

    // Number of instances on which the row is currently drifted. Drives
    // both the per-row pill ("Arr drift (2)") and the per-instance Apply
    // button cluster (one button per drifted instance).
    cfRowDriftCount(row) {
      if (!row || !Array.isArray(row.instances)) return 0;
      let n = 0;
      for (const inst of row.instances) {
        if (inst.drift) n++;
      }
      return n;
    },

    // Number of CFs currently drifted on at least one instance, for the
    // card-header summary line ("3 drifted" / "All in sync").
    // Number of instances on this row that have a TRaSH upstream
    // update pending. Symmetric with cfRowDriftCount — feeds the
    // per-row status pill + "X updates" badges.
    cfRowUpdateCount(row) {
      if (!row || !Array.isArray(row.instances)) return 0;
      let n = 0;
      for (const inst of row.instances) {
        if (inst && inst.updateAvailable) n++;
      }
      return n;
    },

    cfDriftedCount(appType) {
      // Reflect what's actually in the current view — when the user
      // scopes to one instance via the picker, "drifted" should mean
      // "drifted on THIS instance", not the global cross-instance
      // total. cfSyncRulesFiltered already trims row.instances to
      // just the picked instance so cfRowDriftCount counts correctly.
      const rows = this.cfSyncRulesFiltered(appType) || [];
      let n = 0;
      for (const row of rows) {
        if (this.cfRowDriftCount(row) > 0) n++;
      }
      return n;
    },

    // Update-available row count for the currently-visible scope.
    // Symmetric with cfDriftedCount.
    cfUpdatesCount(appType) {
      const rows = this.cfSyncRulesFiltered(appType) || [];
      let n = 0;
      for (const row of rows) {
        if (this.cfRowUpdateCount(row) > 0) n++;
      }
      return n;
    },

    // Hierarchical category tree for the CF sub-tab sidebar. Groups
    // managed CFs by their parent bracket-prefix ("Audio Formats",
    // "HDR Formats", "Unwanted") with children = unique subcategories
    // ("Default", "Streaming", "Anime"). Mirrors the Browse tab's
    // sidebar shape so users carry the same mental model across both
    // surfaces. Counts respect the active instance scope; search is
    // NOT applied (sidebar is "jump to", not "narrow further").
    cfSyncRulesCategories(appType) {
      let rows = this.cfSyncRules[appType] || [];
      const instId = this.cfSyncRulesActiveInstance || '';
      if (instId) {
        rows = rows
          .map(r => {
            const filtered = (r.instances || []).filter(i => i.id === instId);
            if (filtered.length === 0) return null;
            return { ...r, instances: filtered };
          })
          .filter(Boolean);
      }
      const byParent = new Map();
      for (const row of rows) {
        const parent = row.category || 'Other';
        const child = row.subcategory || '';
        if (!byParent.has(parent)) {
          byParent.set(parent, { name: parent, total: 0, drifted: 0, children: new Map() });
        }
        const parentEntry = byParent.get(parent);
        parentEntry.total++;
        if (this.cfRowDriftCount(row) > 0) parentEntry.drifted++;
        if (child) {
          if (!parentEntry.children.has(child)) {
            parentEntry.children.set(child, { name: child, total: 0, drifted: 0 });
          }
          const childEntry = parentEntry.children.get(child);
          childEntry.total++;
          if (this.cfRowDriftCount(row) > 0) childEntry.drifted++;
        }
      }
      const out = Array.from(byParent.values()).map(p => ({
        ...p,
        children: Array.from(p.children.values()).sort((a, b) => a.name.localeCompare(b.name)),
      }));
      // Fixed-order tail:
      //   1. Custom at the very bottom (user-managed CFs aren't in
      //      the TRaSH catalog so they read as "other" relative to
      //      it).
      //   2. ANY group whose name starts with "SQP" just above Custom
      //      (TRaSH's SQP-N + SQP MA Hybrid + SQP Disable If One...
      //      are profile-targeted quality presets, not content
      //      categories — they belong as a group at the bottom).
      //   3. Everything else alphabetical.
      out.sort((a, b) => {
        if (a.name === 'Custom') return 1;
        if (b.name === 'Custom') return -1;
        const aSQP = a.name.startsWith('SQP');
        const bSQP = b.name.startsWith('SQP');
        if (aSQP && !bSQP) return 1;
        if (bSQP && !aSQP) return -1;
        return a.name.localeCompare(b.name);
      });
      return out;
    },

    // Toggle the chevron expansion state of a sidebar parent. Mirrors
    // the Browse tab's pattern: explicit state per-parent kept in
    // localStorage so re-opening the sub-tab restores what the user
    // had. Auto-expanded when the active filter targets a child of
    // this parent (handled in cfSRSidebarParentOpen).
    cfSRToggleSidebarParent(parentName) {
      const visible = this.cfSRSidebarParentOpen(parentName);
      this.cfSyncRulesSidebarExpanded = {
        ...(this.cfSyncRulesSidebarExpanded || {}),
        [parentName]: !visible,
      };
      try { localStorage.setItem('clonarr_cfSRSidebarExpanded', JSON.stringify(this.cfSyncRulesSidebarExpanded)); } catch (_) {}
    },

    // True when a parent's children should render visible. Three
    // states, in priority order:
    //   1. Explicit chevron state (locked open / locked closed)
    //   2. Auto-open because the active filter targets this parent
    //      OR one of its children
    //   3. Default closed
    // Matches Browse's isCFBrowseParentExpanded exactly so users
    // carry the same interaction model across both sidebars: clicking
    // a parent label expands it and auto-collapses the previously
    // active one; clicking the chevron locks open.
    cfSRSidebarParentOpen(parentName) {
      const explicit = (this.cfSyncRulesSidebarExpanded || {})[parentName];
      if (explicit !== undefined) return explicit;
      const active = this.cfSyncRulesActiveCat || 'all';
      if (active === 'cat:' + parentName) return true;
      if (active.startsWith('sub:' + parentName + '|')) return true;
      return false;
    },

    // Click on a sidebar parent label — sets it as the active
    // category AND clears any explicit chevron state so auto-expand
    // (via the active-match check above) takes over. Without the
    // clear, a previously-collapsed parent would stay closed even
    // when re-selected, surprising the user.
    cfSRPickParent(parentName) {
      this.cfSyncRulesActiveCat = 'cat:' + parentName;
      const stored = this.cfSyncRulesSidebarExpanded || {};
      if (Object.prototype.hasOwnProperty.call(stored, parentName)) {
        const updated = { ...stored };
        delete updated[parentName];
        this.cfSyncRulesSidebarExpanded = updated;
        try { localStorage.setItem('clonarr_cfSRSidebarExpanded', JSON.stringify(updated)); } catch (_) {}
      }
    },

    // Click on a child sub-category — narrows to that sub AND
    // clears the parent's explicit state for the same reason
    // (auto-match opens the parent so the picked child is visible).
    cfSRPickSubcategory(parentName, childName) {
      this.cfSyncRulesActiveCat = 'sub:' + parentName + '|' + childName;
      const stored = this.cfSyncRulesSidebarExpanded || {};
      if (Object.prototype.hasOwnProperty.call(stored, parentName)) {
        const updated = { ...stored };
        delete updated[parentName];
        this.cfSyncRulesSidebarExpanded = updated;
        try { localStorage.setItem('clonarr_cfSRSidebarExpanded', JSON.stringify(updated)); } catch (_) {}
      }
    },

    // True when AT LEAST ONE sidebar parent is currently shown
    // (either via auto-match, explicit lock-open, or the active
    // filter). Drives the Expand all / Collapse all toggle label —
    // if any are open, the button reads "Collapse all"; if all are
    // closed, "Expand all".
    cfSRAnyParentExpanded(appType) {
      const cats = this.cfSyncRulesCategories(appType);
      for (const p of cats) {
        if (this.cfSRSidebarParentOpen(p.name)) return true;
      }
      return false;
    },

    // Bulk-toggle every sidebar parent. Writes explicit state for
    // each one so the choice survives auto-match transitions (the
    // user wants "ALL open" or "ALL closed" regardless of what the
    // active filter would auto-expand).
    cfSRToggleAllParents(appType) {
      const cats = this.cfSyncRulesCategories(appType);
      const next = !this.cfSRAnyParentExpanded(appType);
      const updated = { ...(this.cfSyncRulesSidebarExpanded || {}) };
      for (const p of cats) {
        updated[p.name] = next;
      }
      this.cfSyncRulesSidebarExpanded = updated;
      try { localStorage.setItem('clonarr_cfSRSidebarExpanded', JSON.stringify(updated)); } catch (_) {}
    },

    // Apply every active filter (category / subcategory / view /
    // instance / search) to the row list. Filters AND together.
    // Filter shapes:
    //   'all'                                — no filter
    //   'cat:<parent>'                       — narrow to parent bracket prefix
    //   'sub:<parent>|<child>'               — narrow to a specific child
    //   'view:drifted'                       — only rows with drift
    // The instance filter on top trims row.instances to just the
    // picked instance so per-row counts scope correctly. Search
    // matches CF name OR any profile name (TRaSH or Arr) inside any
    // instance block.
    cfSyncRulesFiltered(appType) {
      let rows = this.cfSyncRules[appType] || [];
      const cat = this.cfSyncRulesActiveCat || 'all';
      if (cat.startsWith('cat:')) {
        const target = cat.slice('cat:'.length);
        rows = rows.filter(r => (r.category || 'Other') === target);
      } else if (cat.startsWith('sub:')) {
        const rest = cat.slice('sub:'.length);
        const pipe = rest.indexOf('|');
        if (pipe > -1) {
          const parent = rest.slice(0, pipe);
          const child = rest.slice(pipe + 1);
          rows = rows.filter(r =>
            (r.category || 'Other') === parent
            && (r.subcategory || '') === child);
        }
      }
      const instId = this.cfSyncRulesActiveInstance || '';
      if (instId) {
        rows = rows
          .map(r => {
            const filtered = (r.instances || []).filter(i => i.id === instId);
            if (filtered.length === 0) return null;
            return { ...r, instances: filtered };
          })
          .filter(Boolean);
      }
      if (cat === 'view:drifted') {
        // Trim each row's instances to only the drifted ones, and
        // drop rows that end up with zero. Without the per-instance
        // trim, a CF drifted on Radarr (main) but in-sync on Radarr
        // (4K) would still render an "In sync" row in the 4K card —
        // surprising when the user explicitly asked for "drifted".
        rows = rows
          .map(r => {
            const driftedOnly = (r.instances || []).filter(i => i.drift);
            if (driftedOnly.length === 0) return null;
            return { ...r, instances: driftedOnly };
          })
          .filter(Boolean);
      }
      if (cat === 'view:updates') {
        rows = rows
          .map(r => {
            const updOnly = (r.instances || []).filter(i => i.updateAvailable);
            if (updOnly.length === 0) return null;
            return { ...r, instances: updOnly };
          })
          .filter(Boolean);
      }
      const q = (this.cfSyncRulesSearch || '').trim().toLowerCase();
      if (q) {
        rows = rows.filter(r => {
          if ((r.name || '').toLowerCase().includes(q)) return true;
          for (const inst of (r.instances || [])) {
            for (const p of (inst.profiles || [])) {
              if ((p.trashProfileName || '').toLowerCase().includes(q)) return true;
              if ((p.arrProfileName || '').toLowerCase().includes(q)) return true;
            }
          }
          return false;
        });
      }
      return rows;
    },

    // Pivot rows by instance so the main pane can render one
    // v3-inst-card per instance, mirroring Profile Sync Rules. Result
    // is sorted by instance name and only includes instances that
    // actually have managed CFs after filters. Each entry carries
    // the instance metadata + its slice of filtered rows.
    cfSyncRulesByInstance(appType) {
      const rows = this.cfSyncRulesFiltered(appType) || [];
      const byInstId = new Map();
      for (const row of rows) {
        for (const inst of (row.instances || [])) {
          if (!byInstId.has(inst.id)) {
            byInstId.set(inst.id, { id: inst.id, name: inst.name, rows: [] });
          }
          // Each rendered row shows the CF + this instance's slice
          // (one inst block) so a CF on two instances renders twice,
          // once per card. Keeps each card a coherent per-instance view.
          byInstId.get(inst.id).rows.push({
            trashId: row.trashId,
            name: row.name,
            category: row.category,
            subcategory: row.subcategory,
            instance: inst,
          });
        }
      }
      const out = Array.from(byInstId.values());
      out.sort((a, b) => a.name.localeCompare(b.name));
      return out;
    },

    // Drift count for a per-instance card. Walks the rows in the
    // card and counts CFs that are drifted on this instance.
    cfSRInstanceCardDriftCount(card) {
      let n = 0;
      for (const r of (card.rows || [])) {
        if (r.instance && r.instance.drift) n++;
      }
      return n;
    },

    // Update-available count for the per-instance card.
    cfSRInstanceCardUpdateCount(card) {
      let n = 0;
      for (const r of (card.rows || [])) {
        if (r.instance && r.instance.updateAvailable) n++;
      }
      return n;
    },

    // Pull TRaSH then sync the rule(s) on this instance that pull
    // this CF in. The pull step is shared (a single Update click on
    // any CF gets the whole TRaSH-Guides advance), but only the
    // specific rules tied to this CF run their per-Arr sync — other
    // rules on the instance stay at their previous lastSyncCommit
    // so the user can opt in CF-by-CF instead of having to commit
    // to "sync everything".
    async updateCFForInstance(instanceId, trashId, cfName, instanceName, ruleIds) {
      if (!instanceId || !trashId || !Array.isArray(ruleIds) || ruleIds.length === 0) return;
      const key = instanceId + ':' + trashId;
      if (this.cfApplyingKey === key) return;
      // updatingRuleId intentionally NOT guarded — see comment on the
      // button's :disabled binding. updatingInstance is still checked
      // since a card-level Update all on the same instance is a real
      // mutex (both flows pull TRaSH).
      if (this.updatingInstance === instanceId) return;
      this.cfApplyingKey = key;
      try {
        const inst = this.instances.find(i => i.id === instanceId);
        if (!inst) return;
        await fetch('/api/trash/pull', { method: 'POST' });
        await this._waitForPullDone();
        if (this.trashStatus?.pullError) {
          this.showToast(`Update CF: pull failed — ${this.trashStatus.pullError}`, 'error', 8000);
          return;
        }
        await this.loadCFBrowse(inst.type);
        await this.loadTrashProfiles(inst.type);
        // Narrow the candidate rule set to ONLY rules that actually
        // have a TRaSH-side pending change matching this trashId.
        // Without this filter, every rule that pulls the CF into its
        // profile would re-sync — bumping their LastSyncTime to "just
        // now" and burning Arr API calls even when those rules had no
        // upstream change to act on. The split-on-first-colon mirrors
        // the backend's affectedId → trash_id recovery in cf_sync_rules.go.
        const ruleHasPendingForCF = (rule) => {
          for (const pc of (rule.pendingChanges || [])) {
            if (pc.source !== 'trash') continue;
            if (!pc.changeType || !pc.changeType.startsWith('cf-')) continue;
            const aid = pc.affectedId || '';
            const ci = aid.indexOf(':');
            const pcTid = ci > 0 ? aid.slice(0, ci) : aid;
            if (pcTid === trashId) return true;
          }
          return false;
        };
        const filteredRuleIds = ruleIds.filter(rid => {
          const rule = this.autoSyncRules.find(r => r.id === rid);
          return rule && ruleHasPendingForCF(rule);
        });
        // Defensive fallback: if nothing matches (stale state, dedup
        // edge case), fall back to the full list so the user's click
        // does SOMETHING rather than silently no-op'ing.
        const effectiveRuleIds = filteredRuleIds.length > 0 ? filteredRuleIds : ruleIds;
        let succeeded = 0;
        const failures = [];
        // Reuse syncAllForInstance's ruleToHistoryShape adapter so the
        // sh body sent to quickSync is byte-identical to the working
        // Update all path. Earlier attempt at hand-rolling the shape
        // missed selectedCFs map + override fields, which quickSync
        // would then read as undefined and the backend would reject
        // silently → "Update button does nothing".
        const ruleToShape = (r) => {
          const cfMap = {};
          for (const id of (r.selectedCFs || [])) cfMap[id] = true;
          const arrName = (typeof this.resolveArrProfileName === 'function')
            ? this.resolveArrProfileName(r.instanceId, r.arrProfileId)
            : '';
          const profileName = arrName
            ? `${arrName} (#${r.arrProfileId})`
            : `Arr profile #${r.arrProfileId}`;
          return {
            profileTrashId: r.trashProfileId || '',
            importedProfileId: r.importedProfileId || '',
            arrProfileId: r.arrProfileId,
            arrProfileName: arrName,
            profileName: profileName,
            selectedCFs: cfMap,
            scoreOverrides: r.scoreOverrides || null,
            qualityOverrides: r.qualityOverrides || null,
            qualityStructure: r.qualityStructure || null,
            overrides: r.overrides || null,
            behavior: r.behavior || null,
          };
        };
        for (const rid of effectiveRuleIds) {
          const rule = this.autoSyncRules.find(r => r.id === rid);
          if (!rule) continue;
          const sh = ruleToShape(rule);
          try {
            const res = await this.quickSync(inst, sh, /* silent */ true);
            if (res && res.ok) {
              succeeded++;
            } else {
              failures.push(`${sh.profileName}: ${res?.error || 'unknown'}`);
            }
          } catch (e) {
            failures.push(`${sh.profileName || rule.id}: ${e.message}`);
          }
        }
        await this.loadAutoSyncRules();
        await this.loadCFSyncRules(this.activeAppType);
        if (failures.length === 0) {
          this.showToast(`"${cfName}" updated on ${instanceName}.`, 'success', 4000);
        } else {
          this.showToast(`"${cfName}" partial update on ${instanceName}: ${succeeded} ok, ${failures.length} failed.`, 'warning', 6000);
        }
      } finally {
        this.cfApplyingKey = '';
      }
    },

    // Most recent sync timestamp across all profiles pulling this CF
    // into this instance. Used for the collapsed row's "Last sync"
    // column — gives the user a single representative value when a
    // CF is in multiple profiles on the same instance. ISO strings
    // compare lexicographically so a plain string max works.
    cfSRInstanceLastSync(instance) {
      if (!instance || !instance.profiles) return '';
      let latest = '';
      for (const p of instance.profiles) {
        if (p.lastSync && p.lastSync > latest) latest = p.lastSync;
      }
      return latest;
    },

    // Update all for a single instance card — pushes the saved spec
    // for every drifted CF on that instance. Replaces the old
    // global Update all (which used the cross-instance filter).
    // Sequential with concurrency cap to avoid hammering Arr.
    async applyAllCFDriftForInstance(card) {
      if (this.cfApplyAllProgress.running) return;
      const queue = (card.rows || [])
        .filter(r => r.instance && r.instance.drift)
        .map(r => ({
          instanceId: card.id,
          instanceName: card.name,
          trashId: r.trashId,
          cfName: r.name,
        }));
      if (queue.length === 0) {
        this.showToast('Nothing to update on ' + card.name + '.', 'info', 3000);
        return;
      }
      this.cfApplyAllProgress = { running: true, total: queue.length, done: 0, label: '' };
      let succeeded = 0;
      const failures = [];
      const concurrency = 2;
      let next = 0;
      const worker = async () => {
        while (next < queue.length) {
          const idx = next++;
          const item = queue[idx];
          this.cfApplyAllProgress.label = `${item.cfName} → ${item.instanceName}`;
          try {
            const r = await fetch('/api/cf-drift/apply', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ instanceId: item.instanceId, trashId: item.trashId }),
            });
            if (r.ok) {
              succeeded++;
            } else {
              let msg = 'unknown error';
              try { const j = await r.json(); if (j?.error) msg = j.error; } catch (_) {}
              failures.push(`${item.cfName}: ${msg}`);
            }
          } catch (e) {
            failures.push(`${item.cfName}: ${e.message}`);
          }
          this.cfApplyAllProgress.done++;
        }
      };
      const workers = [];
      for (let i = 0; i < Math.min(concurrency, queue.length); i++) workers.push(worker());
      await Promise.all(workers);
      this.cfApplyAllProgress = { running: false, total: 0, done: 0, label: '' };
      await this.loadCFSyncRules(this.activeAppType);
      if (failures.length === 0) {
        this.showToast(`Updated ${succeeded} drifted CF${succeeded === 1 ? '' : 's'} on ${card.name}.`, 'success', 4000);
      } else if (succeeded === 0) {
        this.showToast(`Update all failed on ${card.name} (${failures.length} error${failures.length === 1 ? '' : 's'}):\n${failures.slice(0, 4).join('\n')}${failures.length > 4 ? `\n+${failures.length - 4} more` : ''}`, 'error', 8000);
      } else {
        this.showToast(`Updated ${succeeded} of ${queue.length} on ${card.name}. ${failures.length} failed:\n${failures.slice(0, 3).join('\n')}${failures.length > 3 ? `\n+${failures.length - 3} more` : ''}`, 'warning', 8000);
      }
    },

    // Drifted-row total for the sidebar's "Drifted (N)" pin. Respects
    // instance picker so the count matches what the user would see if
    // they activate the view filter, but ignores category + search so
    // the badge advertises a real "jump to" affordance regardless of
    // where the user currently is.
    cfSyncRulesDriftedTotal(appType) {
      let rows = this.cfSyncRules[appType] || [];
      const instId = this.cfSyncRulesActiveInstance || '';
      if (instId) {
        rows = rows
          .map(r => {
            const filtered = (r.instances || []).filter(i => i.id === instId);
            if (filtered.length === 0) return null;
            return { ...r, instances: filtered };
          })
          .filter(Boolean);
      }
      let n = 0;
      for (const row of rows) {
        if (this.cfRowDriftCount(row) > 0) n++;
      }
      return n;
    },

    // Mirror of cfSyncRulesDriftedTotal for upstream-update view-pin.
    cfSyncRulesUpdatesTotal(appType) {
      let rows = this.cfSyncRules[appType] || [];
      const instId = this.cfSyncRulesActiveInstance || '';
      if (instId) {
        rows = rows
          .map(r => {
            const filtered = (r.instances || []).filter(i => i.id === instId);
            if (filtered.length === 0) return null;
            return { ...r, instances: filtered };
          })
          .filter(Boolean);
      }
      let n = 0;
      for (const row of rows) {
        if (this.cfRowUpdateCount(row) > 0) n++;
      }
      return n;
    },

    // List of instance options for the picker. Sorted by name for
    // a predictable dropdown. Empty when the app type has only one
    // instance — the picker is then redundant and gets hidden.
    cfSyncRulesInstanceOptions(appType) {
      const list = this.instancesOfType(appType) || [];
      return list.slice().sort((a, b) => a.name.localeCompare(b.name));
    },

    // Toggle the per-(instance, CF) expand row. Single-open: opening
    // row B closes row A. Re-clicking the open row collapses it.
    // Compound key "<instanceId>:<trashId>" — trashId may itself
    // contain colons (custom: prefix) so we split on the FIRST colon
    // only, not via String.split which would mis-segment custom CFs.
    // When opening a drifted row, auto-load the diff for that
    // specific (instance, CF) pair.
    cfSyncRulesToggleExpand(key) {
      if (this.cfSyncRulesExpandedRow === key) {
        this.cfSyncRulesExpandedRow = '';
        return;
      }
      this.cfSyncRulesExpandedRow = key;
      const colonIdx = key.indexOf(':');
      if (colonIdx === -1) return;
      const instanceId = key.slice(0, colonIdx);
      const trashId = key.slice(colonIdx + 1);
      const row = (this.cfSyncRules[this.activeAppType] || []).find(r => r.trashId === trashId);
      if (!row) return;
      const inst = (row.instances || []).find(i => i.id === instanceId);
      if (!inst || !inst.drift) return;
      if (this.cfDriftDiffCache[key]
          && (this.cfDriftDiffCache[key].diff || this.cfDriftDiffCache[key].error)) {
        return;
      }
      this.loadCFDriftDiff(instanceId, trashId);
    },

    // Fetch the per-CF drift diff for a single (instance, trashId)
    // pair. Stores into cfDriftDiffCache. Surfaces a loading
    // placeholder while in-flight and an error string on failure so
    // the UI can render either state cleanly.
    async loadCFDriftDiff(instanceId, trashId) {
      const key = instanceId + ':' + trashId;
      this.cfDriftDiffCache[key] = { loading: true, diff: null, error: '' };
      try {
        const r = await fetch(`/api/cf-drift/diff?instanceId=${encodeURIComponent(instanceId)}&trashId=${encodeURIComponent(trashId)}`);
        if (!r.ok) {
          let msg = `HTTP ${r.status}`;
          try { const j = await r.json(); if (j?.error) msg = j.error; } catch (_) {}
          this.cfDriftDiffCache[key] = { loading: false, diff: null, error: msg };
          return;
        }
        const data = await r.json();
        this.cfDriftDiffCache[key] = {
          loading: false,
          diff: data.diff || null,
          liveMissing: !!data.liveMissing,
          error: '',
        };
      } catch (e) {
        this.cfDriftDiffCache[key] = { loading: false, diff: null, error: e.message };
      }
    },

    // Helper for the expand panel — returns true when the diff for
    // a given (instance, trashId) pair carries any reportable change.
    // Used to gate "Drift was already resolved" vs "Drift detail" sub-states.
    cfDriftDiffHasChanges(diff) {
      if (!diff) return false;
      return (diff.addedConditions || []).length > 0
        || (diff.removedConditions || []).length > 0
        || (diff.changedConditions || []).length > 0
        || (diff.settingsChanges || []).length > 0;
    },

    // Apply every (instance, trashId) drifted pair currently visible
    // through the active filter. Sequential with a 2-at-a-time
    // concurrency cap so a 50-CF drift batch doesn't fan-out 50
    // simultaneous PUTs at one Arr instance. Progress surface visible
    // in the header bar (X / Y); per-failure log in the post-batch
    // toast. Stops cleanly when the user navigates away (in-flight
    // POSTs complete; queued ones are abandoned on the cfApplyingKey
    // check at start).
    async applyAllCFDrift(appType) {
      if (this.cfApplyAllProgress.running) return;
      const rows = this.cfSyncRulesFiltered(appType);
      const queue = [];
      for (const row of rows) {
        for (const inst of (row.instances || [])) {
          if (!inst.drift) continue;
          queue.push({ instanceId: inst.id, instanceName: inst.name, trashId: row.trashId, cfName: row.name });
        }
      }
      if (queue.length === 0) {
        this.showToast('Nothing to apply — no drift detected.', 'info', 3000);
        return;
      }
      this.cfApplyAllProgress = { running: true, total: queue.length, done: 0, label: '' };
      let succeeded = 0;
      const failures = [];
      // Worker pool — 2 parallel Apply calls feels safe on Arr. The
      // body is tiny and the bottleneck is Arr-side validation.
      const concurrency = 2;
      let next = 0;
      const worker = async () => {
        while (next < queue.length) {
          const idx = next++;
          const item = queue[idx];
          this.cfApplyAllProgress.label = `${item.cfName} → ${item.instanceName}`;
          try {
            const r = await fetch('/api/cf-drift/apply', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ instanceId: item.instanceId, trashId: item.trashId }),
            });
            if (r.ok) {
              succeeded++;
            } else {
              let msg = 'unknown error';
              try { const j = await r.json(); if (j?.error) msg = j.error; } catch (_) {}
              failures.push(`${item.cfName} → ${item.instanceName}: ${msg}`);
            }
          } catch (e) {
            failures.push(`${item.cfName} → ${item.instanceName}: ${e.message}`);
          }
          this.cfApplyAllProgress.done++;
        }
      };
      const workers = [];
      for (let i = 0; i < Math.min(concurrency, queue.length); i++) workers.push(worker());
      await Promise.all(workers);
      this.cfApplyAllProgress = { running: false, total: 0, done: 0, label: '' };
      await this.loadCFSyncRules(appType);
      if (failures.length === 0) {
        this.showToast(`Updated ${succeeded} drifted custom format${succeeded === 1 ? '' : 's'}.`, 'success', 4000);
      } else if (succeeded === 0) {
        this.showToast(`Update all failed (${failures.length} error${failures.length === 1 ? '' : 's'}):\n${failures.slice(0, 4).join('\n')}${failures.length > 4 ? `\n+${failures.length - 4} more` : ''}`, 'error', 8000);
      } else {
        this.showToast(`Updated ${succeeded} of ${queue.length}. ${failures.length} failed:\n${failures.slice(0, 3).join('\n')}${failures.length > 3 ? `\n+${failures.length - 3} more` : ''}`, 'warning', 8000);
      }
    },

    // Push clonarr's saved spec for one CF back to one instance. The
    // backend writes a CF-tagged history entry, clears the fingerprint,
    // and fires NotifyCFDriftReconciled. We refresh the local data so
    // the row's pill flips to "In sync" without a page reload.
    async applyCFDrift(instanceId, trashId, cfName, instanceName) {
      if (!instanceId || !trashId) return;
      const key = instanceId + ':' + trashId;
      if (this.cfApplyingKey === key) return;
      this.cfApplyingKey = key;
      try {
        const r = await fetch('/api/cf-drift/apply', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ instanceId, trashId }),
        });
        if (!r.ok) {
          let msg = 'Failed to apply drift';
          try { const j = await r.json(); if (j?.error) msg = j.error; } catch (_) {}
          this.showToast(msg, 'error', 5000);
          return;
        }
        await this.loadCFSyncRules(this.activeAppType);
        this.showToast(`"${cfName}" updated on ${instanceName}.`, 'success', 4000);
      } catch (e) {
        this.showToast('Network error during apply: ' + e.message, 'error', 5000);
      } finally {
        this.cfApplyingKey = '';
      }
    },

    historyEventCount(instId, arrProfileId) {
      return (this.syncHistory[instId] || []).filter(sh => sh.arrProfileId === arrProfileId && sh.changes).length;
    },

    sortedHistoryProfiles(instId) {
      const rules = this.sortedSyncRules(instId);
      const col = this.historySort.col;
      if (!col) return rules;
      const dir = this.historySort.dir === 'asc' ? 1 : -1;
      return [...rules].sort((a, b) => {
        switch (col) {
          case 'trash': return dir * (a.profileName || '').localeCompare(b.profileName || '');
          case 'arr': return dir * (a.arrProfileName || '').localeCompare(b.arrProfileName || '');
          case 'changed': {
            const at = a.changes ? new Date(a.appliedAt || a.lastSync).getTime() : 0;
            const bt = b.changes ? new Date(b.appliedAt || b.lastSync).getTime() : 0;
            return dir * (at - bt);
          }
          case 'events': return dir * (this.historyEventCount(instId, a.arrProfileId) - this.historyEventCount(instId, b.arrProfileId));
        }
        return 0;
      });
    },

    toggleHistorySort(col) {
      if (this.historySort.col === col) {
        this.historySort.dir = this.historySort.dir === 'asc' ? 'desc' : 'asc';
      } else {
        this.historySort.col = col;
        this.historySort.dir = col === 'changed' || col === 'events' ? 'desc' : 'asc';
      }
    },

    async rollbackSync(inst, entry, entryIdx) {
      // To undo the changes shown in this entry, we sync with the PREVIOUS entry's
      // settings (the state before this sync ran). The previous entry is the next one
      // in the array (newest-first ordering).
      const allEntries = this.historyEntries;
      const changeEntries = allEntries.filter(e => e.changes);
      const prevEntry = changeEntries[entryIdx + 1] || allEntries[allEntries.length - 1];
      if (!prevEntry || prevEntry === entry) {
        this.showToast('No previous state to rollback to — this is the earliest recorded sync.', 'warning', 6000);
        return;
      }
      const date = new Date(entry.appliedAt || entry.lastSync).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false });
      const prevDate = new Date(prevEntry.appliedAt || prevEntry.lastSync).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false });
      // Full list of changes to be reversed, grouped by category so the
      // user sees the real magnitude of the rollback. Rendered in the
      // confirm modal's scrollable details box (no truncation).
      const changes = entry.changes || {};
      const details = [
        ...(changes.cfDetails || []),
        ...(changes.scoreDetails || []),
        ...(changes.qualityDetails || []),
        ...(changes.settingsDetails || []),
      ];
      const headline = details.length > 0
        ? `\n\n${details.length} change${details.length === 1 ? '' : 's'} will be reversed:`
        : '';
      const confirmed = await new Promise(resolve => {
        this.confirmModal = {
          show: true,
          title: 'Rollback Profile',
          message: `Undo the changes from ${date} and restore "${entry.arrProfileName}" to the state from ${prevDate}?\n\nAuto-sync will be disabled to prevent it from overwriting the rollback.${headline}`,
          details,
          confirmLabel: 'Rollback',
          onConfirm: () => resolve(true),
          onCancel: () => resolve(false),
        };
      });
      if (!confirmed) return;
      this.showToast(`Rolling back "${entry.arrProfileName}" to ${prevDate}...`, 'info', 3000);
      // useHistoryOnly=true: rebuild the request strictly from prevEntry's
      // snapshot. The rule's current state reflects the just-completed sync
      // we're trying to undo — falling through to it would no-op the
      // rollback (push current rule state == post-undo state).
      const result = await this.quickSync(inst, prevEntry, true, true);
      if (result.ok) {
        const rule = this.autoSyncRules.find(r => r.instanceId === inst.id && r.arrProfileId === entry.arrProfileId);
        if (rule && rule.enabled) {
          await this.toggleAutoSyncRule(rule);
        }
        this.showToast({
          title: `Rolled back "${entry.arrProfileName}"`,
          message: `Restored ${prevDate}. Auto-sync disabled.`,
          details: result.details || [],
          type: 'info',
          duration: 8000,
          key: this.toastKey(
            'rollback', inst.id, entry.arrProfileId, prevEntry.appliedAt || prevEntry.lastSync || entry.arrProfileId
          ),
        });
        await this.loadProfileHistory(inst.id, entry.arrProfileId);
        await this.loadSyncHistory(inst.id);
      } else {
        this.showToast(`Rollback failed: ${result.error}`, 'error', 8000);
      }
    },

    async loadProfileHistory(instanceId, arrProfileId) {
      this.historyEntries = [];
      this.historyDetailIdx = -1;
      this.historyLoading = true;
      try {
        const r = await fetch(`/api/instances/${instanceId}/sync-history/${arrProfileId}/changes`);
        if (r.ok) this.historyEntries = await r.json();
      } catch (e) { console.error('loadProfileHistory:', e); }
      finally { this.historyLoading = false; }
    },

    toggleSyncRulesSort(col) {
      if (this.syncRulesSort.col === col) {
        this.syncRulesSort.dir = this.syncRulesSort.dir === 'asc' ? 'desc' : 'asc';
      } else {
        this.syncRulesSort = { col, dir: 'asc' };
      }
    },

    // Per-rule optional-CF count for the sync rules table.
    //
    // Walks rule.selectedCFs and matches each entry against cf-groups data
    // to determine if it's a TRaSH customization (non-default
    // activation). Returns 0 when cf-groups data isn't loaded yet — badge
    // hidden in that case until user visits Custom Formats tab and the
    // browse data populates. Approximation note: a single trash_id can
    // theoretically appear in multiple cf-groups with different default
    // flags; this picks the first-found and accepts that limitation.
    ruleOptionalCount(rule, appType) {
      if (!rule || !appType || !Array.isArray(rule.selectedCFs)) return 0;
      const groupsData = this.cfBrowseData?.[appType]?.groups || [];
      if (groupsData.length === 0) return 0;
      // Build trash_id → { groupDefault, cfDefault, cfRequired, hasOptionalMembers }
      const cfInfo = {};
      for (const g of groupsData) {
        const cfList = g.custom_formats || g.cfs || [];
        const hasOptionalMembers = cfList.some(c => !c.required);
        const groupDefault = !!(g.default ?? g.defaultEnabled);
        for (const cf of cfList) {
          const tid = cf.trash_id || cf.trashId;
          if (!tid || cfInfo[tid]) continue;
          cfInfo[tid] = {
            cfDefault: !!cf.default,
            cfRequired: !!cf.required,
            groupDefault,
            hasOptionalMembers,
            groupName: g.name,
          };
        }
      }
      const ruleSet = new Set(rule.selectedCFs);
      let n = 0;
      const groupCountedOnce = new Set();
      for (const tid of ruleSet) {
        const info = cfInfo[tid];
        if (!info) continue;
        if (info.cfRequired) {
          // Required CFs don't count individually. But if their group has
          // no optional members AND group is default-OFF, the group toggle
          // is the only signal — count once per such group.
          if (!info.groupDefault && !info.hasOptionalMembers && !groupCountedOnce.has(info.groupName)) {
            n++;
            groupCountedOnce.add(info.groupName);
          }
          continue;
        }
        // Non-required CF: count if its activation differs from default.
        const def = info.groupDefault ? info.cfDefault : false;
        if (true !== def) n++;
      }
      return n;
    },

    // Count how many General fields differ from TRaSH defaults.
    pdGeneralChangeCount() {
      const p = this.profileDetail?.detail?.profile || {};
      const ov = this.pdOverrides;
      let n = 0;
      if (this.activeAppType === 'radarr' && ov.language.value !== (p.language || 'Original')) n++;
      const upVal = ov.upgradeAllowed.value === true || ov.upgradeAllowed.value === 'true';
      if (upVal !== (p.upgradeAllowed ?? true)) n++;
      if (ov.minFormatScore.value !== (p.minFormatScore ?? 0)) n++;
      if (ov.minUpgradeFormatScore.value !== (p.minUpgradeFormatScore ?? 1)) n++;
      if (ov.cutoffFormatScore.value !== (p.cutoffFormatScore || p.cutoffScore || 10000)) n++;
      return n;
    },

    // Count how many Quality fields differ (currently: cutoffQuality only).
    // Any value that isn't the TRaSH default counts — including "__skip__" (don't sync).
    pdQualityChangeCount() {
      const p = this.profileDetail?.detail?.profile || {};
      const cq = this.pdOverrides.cutoffQuality || '';
      const def = p.cutoff || '';
      return cq !== def ? 1 : 0;
    },

    // Per-section override counts + total. Drives the "N changes · General: X /
    // Quality: Y / Overridden Scores: Z / Extras: W" breakdown shown in the
    // Profile Detail toggle header when pdOverridesEnabled is true. All counts
    // are data-driven from pdOverrides / cfScoreOverrides / extraCFs values
    // compared against profile defaults.
    pdOverrideSummary() {
      const general = this.pdGeneralChangeCount();
      const quality = this.pdQualityChangeCount() + this.pdQualityItemsChangeCount();
      // Customizations: union of cfScoreOverrides (TRaSH-base CFs whose
      // score the user changed) + extraCFs (CFs added beyond profile
      // defaults). A CF can be in both maps (added + score-changed) — count
      // once via Set union. Replaces the old separate cfScores + extraCFs
      // counts for the unified CF Customizations section.
      // Walk pdAllCustomizations(). added and overridden OVERLAP — a CF that
      // was added beyond profile defaults AND has a custom score (≠ TRaSH
      // default) counts in BOTH. Visual row already paints both states
      // (orange name from isAdded + orange score from .overridden class);
      // the badge counts now mirror that. Sum (added + overridden) may
      // therefore exceed customizations — that's intentional, the union
      // total stays accurate via customizations.
      const all = this.pdAllCustomizations();
      const added = all.filter(it => it.isAdded).length;
      const overridden = all.filter(it => it.isOverridden).length;
      const customizations = all.length;
      // Legacy fields — mirror the filtered counts above so any binding
      // that still references them stays consistent with the unified view
      // (orphan custom: refs are excluded). Will be removed once confirmed
      // unused.
      const cfScores = overridden;
      const extraCFs = added;
      const optional = this.pdOptionalCount();
      // Phase 2c — excluded CFs (lock-icon opt-outs + default-on
      // optional opt-outs) count toward total. Without this the
      // header reads "0 changes" for a rule that materially excludes
      // CFs from sync (the user's "no changes" confusion).
      const excludedCFs = this.pdExcludedCFCount();
      // total = overrides only (profile-level settings the user changed).
      // optional = separate count of TRaSH optional CFs / groups
      //   activated outside the profile's defaults.
      return {
        general, quality, cfScores, extraCFs, customizations, added, overridden, optional, excludedCFs,
        total: general + quality + customizations + excludedCFs,
      };
    },

    // Compact "X added · Y overridden" formatter used by every
    // CF Customizations badge (status bar, card header, group cards).
    // Empty string when both are zero (caller decides what to show then).
    formatCustomizationCounts(added, overridden) {
      if (added === 0 && overridden === 0) return '';
      if (added === 0) return overridden + ' custom score' + (overridden === 1 ? '' : 's');
      if (overridden === 0) return added + ' extra CF' + (added === 1 ? '' : 's');
      return added + ' extra CF' + (added === 1 ? '' : 's') + ' · ' + overridden + ' custom score' + (overridden === 1 ? '' : 's');
    },

    // Build the unified CF Customizations list. Walks both maps
    // (cfScoreOverrides for TRaSH-base CFs and extraCFs for added CFs)
    // and returns one entry per unique trashId, annotated with everything
    // the renderer needs to color-code by role:
    //
    //   isAdded     — CF is in extraCFs (beyond profile's TRaSH-base set)
    //   isOverridden — score differs from the CF's TRaSH default
    //   defaultScore — the CF's TRaSH default for the active score-set
    //   currentScore — what the user has set
    //   groupName    — which TRaSH cf-group the CF belongs to (display label)
    //
    // Sorted by groupName then CF name so customizations group visually
    // by their source. CFs that are in both maps render once (isAdded
    // wins for the toggle/remove control, score override still applies).
    pdAllCustomizations() {
      const items = [];
      const seen = new Set();
      const scoreSet = this.profileDetail?.detail?.profile?.trashScoreSet || 'default';
      // Lookup helper: find name + group for a tid via extraCFGroups
      // (covers any CF that exists in TRaSH-data) then fall through to
      // the active profile's trashGroups (covers TRaSH-base CFs the
      // picker doesn't surface).
      const lookup = (tid) => {
        for (const g of (this.extraCFGroups || [])) {
          const cf = g.cfs?.find(c => c.trashId === tid);
          if (cf) {
            const def = cf.trashScores?.[scoreSet] ?? cf.trashScores?.default ?? 0;
            return { name: cf.name, category: g.category || 'Other', groupName: g.name, defaultScore: def, description: cf.description, isDangling: false };
          }
        }
        const groups = this.profileDetail?.detail?.trashGroups || [];
        for (const g of groups) {
          const cf = (g.cfs || []).find(c => c.trashId === tid);
          if (cf) return { name: cf.name, category: g.category || 'Other', groupName: g.name, defaultScore: this.resolveCFDefaultScore(tid), description: cf.description, isDangling: false };
        }
        // formatItemNames covers the "Required CFs" surface — TRaSH-base CFs
        // that live directly in profile.FormatItems (no source cf-group).
        // Base Profile especially has many of these (Wrong Language etc.),
        // and without this lookup the CF Customizations row falls back to a
        // truncated trashId. Description isn't on formatItemNames; pull it
        // from extraCFAllCFs when available.
        for (const fi of (this.profileDetail?.detail?.formatItemNames || [])) {
          if (fi.trashId === tid) {
            let desc = '';
            for (const cf of (this.extraCFAllCFs || [])) {
              if (cf.trashId === tid) { desc = cf.description || ''; break; }
            }
            return { name: fi.name, category: 'Required', groupName: '', defaultScore: this.resolveCFDefaultScore(tid), description: desc, isDangling: false };
          }
        }
        // Last-chance live lookup via extraCFAllCFs (covers user-created customs
        // that don't appear in any TRaSH/profile group).
        for (const cf of (this.extraCFAllCFs || [])) {
          if (cf.trashId === tid) {
            return { name: cf.name, category: 'Custom', groupName: '', defaultScore: cf.score ?? 0, description: cf.description || '', isDangling: false };
          }
        }
        // Dangling — rule references a CF that no longer exists. Two cases:
        //
        //   custom:<id>  — user deleted the custom CF. Permanent: it will
        //     never resolve again. Hide entirely; sync's cfSetDetails diff
        //     emits "Removed: <id>" once on the next sync (same UX as a
        //     TRaSH-upstream removal). Backend's
        //     CleanupDanglingCustomCFsOnRule strips it from rule data on
        //     the same successful sync so the diff fires exactly once.
        //
        //   TRaSH ids    — usually transient: TRaSH cache empty right
        //     after Reset (until Pull), or upstream restructure moved the
        //     CF. Will resolve again after the next Pull. Render as a
        //     placeholder row so the user keeps visibility of their full
        //     customizations list even while data is mid-refresh.
        if (tid.startsWith('custom:')) {
          return null;
        }
        const shortId = tid.substring(0, 12);
        return { name: 'Unknown CF (' + shortId + '…)', category: 'Unknown', groupName: '', defaultScore: '?', description: 'This Custom Format is referenced by the profile but not currently in TRaSH data. It usually means TRaSH cache is empty (Pull pending) or upstream moved the CF.', isDangling: true };
      };
      // Additional CF opt-ins from selectedOptionalCFs (sel[tid]=true
      // for a CF NOT in profile-default trashGroups). These are pure
      // opt-ins without a score override; extraCFs only carries them
      // when the user also changed the score. Without this loop, a
      // user who toggled on Flux/DD+/etc via the Additional CF tab
      // saw "0 customizations" in the editor header even though the
      // opt-ins are visible in Profile overview and persist in
      // rule.selectedCFs.
      const inProfileGroups = new Set();
      for (const fi of (this.profileDetail?.detail?.formatItemNames || [])) inProfileGroups.add(fi.trashId);
      for (const g of (this.profileDetail?.detail?.trashGroups || [])) {
        for (const cf of (g.cfs || [])) inProfileGroups.add(cf.trashId);
      }
      for (const [tid, v] of Object.entries(this.selectedOptionalCFs || {})) {
        if (!v || tid.startsWith('__grp_')) continue;
        if (inProfileGroups.has(tid)) continue;
        if (seen.has(tid)) continue;
        const meta = lookup(tid);
        if (meta === null) { seen.add(tid); continue; }
        const def = meta.defaultScore;
        // Pull saved score from either store so customizations row
        // matches what's rendered everywhere else.
        const extraScore = this.extraCFs?.[tid];
        const overrideScore = this.cfScoreOverrides?.[tid];
        const currentScore = extraScore !== undefined ? extraScore : (overrideScore !== undefined ? overrideScore : def);
        items.push({
          trashId: tid,
          name: meta.name,
          category: meta.category,
          groupName: meta.groupName,
          description: meta.description,
          isAdded: true,
          defaultScore: def,
          currentScore: currentScore,
          isOverridden: def !== '?' && currentScore !== def,
          isDangling: !!meta.isDangling,
        });
        seen.add(tid);
      }
      for (const [tid, score] of Object.entries(this.extraCFs)) {
        if (seen.has(tid)) continue;
        const meta = lookup(tid);
        if (meta === null) { seen.add(tid); continue; } // permanent orphan (deleted custom CF) — hide
        const def = meta.defaultScore;
        items.push({
          trashId: tid,
          name: meta.name,
          category: meta.category,
          groupName: meta.groupName,
          description: meta.description,
          isAdded: true,
          defaultScore: def,
          currentScore: score,
          isOverridden: def !== '?' && score !== def,
          isDangling: !!meta.isDangling,
        });
        seen.add(tid);
      }
      for (const [tid, score] of Object.entries(this.cfScoreOverrides)) {
        if (seen.has(tid)) continue;
        const def = this.resolveCFDefaultScore(tid);
        if (def !== '?' && score === def) continue; // not actually overridden
        const meta = lookup(tid);
        if (meta === null) { seen.add(tid); continue; } // permanent orphan (deleted custom CF) — hide
        items.push({
          trashId: tid,
          name: meta.name,
          category: meta.category,
          groupName: meta.groupName,
          description: meta.description,
          isAdded: false,
          defaultScore: def,
          currentScore: score,
          isOverridden: true,
          isDangling: !!meta.isDangling,
        });
        seen.add(tid);
      }
      items.sort((a, b) => (a.groupName || '').localeCompare(b.groupName || '') || (a.name || '').localeCompare(b.name || ''));
      return items;
    },

    // Phase 2c — count of excluded CFs that affect the synced output
    // (i.e. the CF would normally sync as part of the profile's
    // defaults). Mirrors backend ComputeRuleCustomizations.ExcludedCFs
    // so frontend + Sync Rules pill agree. Kept as a separate count
    // (rather than folded into pdAllCustomizations) so the existing
    // CF Customizations card render — which only knows how to show
    // added / score-overridden CFs — stays unchanged. Sync Preview's
    // Diffs view surfaces the detail via spOverviewDiffs bucket 5.
    pdExcludedCFCount() {
      const sel = this.selectedOptionalCFs || {};
      const defaults = this.computeTrashDefaults();
      let n = 0;
      for (const tid of defaults) {
        if (sel[tid] === false) n++;
      }
      return n;
    },

    // Hybrid layout for the CF Customizations card: split pdAllCustomizations()
    // into flat rows (CFs whose source group has only one customized entry, or
    // no group at all) and group cards (groups with 2+ customized entries —
    // collapsible to keep the section compact when many CFs are tweaked).
    // Returns { flat: [...], groups: [{name, category, items: []}] }.
    // Flat list preserves the parent sort (by groupName then name).
    // Group list sorts alphabetically by groupName.
    pdGroupedCustomizations() {
      const all = this.pdAllCustomizations();
      const counts = {};
      for (const it of all) {
        const k = it.groupName || '';
        counts[k] = (counts[k] || 0) + 1;
      }
      const flat = [];
      const groupMap = {};
      for (const it of all) {
        const k = it.groupName || '';
        if (k === '' || counts[k] === 1) {
          flat.push(it);
        } else {
          if (!groupMap[k]) {
            groupMap[k] = { name: k, category: it.category, items: [] };
          }
          groupMap[k].items.push(it);
        }
      }
      const groups = Object.values(groupMap).sort((a, b) => a.name.localeCompare(b.name));
      return { flat, groups };
    },

    // Count of TRaSH optional CFs and groups activated outside the
    // profile's defaults. A group counts when its effective on/off state
    // differs from defaultEnabled. A non-required CF counts when its
    // selected state differs from cf.default (or differs from false when
    // the parent group is default-OFF). Required CFs are owned by the
    // group toggle and don't add to the count separately.
    pdOptionalCount() {
      const sel = this.selectedOptionalCFs || {};
      const groups = this.profileDetail?.detail?.trashGroups || [];
      let n = 0;
      for (const group of groups) {
        n += this.pdGroupOptionalCount(group, sel);
      }
      return n;
    },

    // Per-group optional-customization count.
    //
    // Counts non-required CFs whose selected state diverges from cf.default
    // (or from false when the group is default-OFF). For groups that have
    // optional members (any cf.required === false), the group toggle is NOT
    // counted separately — toggling the group on/off is implicit in the
    // per-CF count, so adding it would double-count. For groups whose only
    // members are REQUIRED (e.g. HDR Formats HDR / HDR Formats DV Boost,
    // single-CF groups marked required), the per-CF loop skips everything,
    // so the group toggle becomes the only signal: count it when divergent
    // from defaultEnabled. This makes HDR Formats DV Boost → 1 instead of 0
    // when activated, while keeping Optional Movie Versions at 11 (not 12).
    pdGroupOptionalCount(group, sel) {
      sel = sel || this.selectedOptionalCFs || {};
      let n = 0;
      for (const cf of (group.cfs || [])) {
        if (cf.required) continue;
        const cur = !!sel[cf.trashId];
        const def = group.defaultEnabled ? !!cf.default : false;
        if (cur !== def) n++;
      }
      const hasOptionalMembers = (group.cfs || []).some(cf => !cf.required);
      if (!hasOptionalMembers) {
        const grpKey = '__grp_' + group.name;
        const grpOn = sel[grpKey] !== undefined ? sel[grpKey] : group.defaultEnabled;
        if (grpOn !== group.defaultEnabled) n++;
      }
      return n;
    },

    // Reset all per-group expand states in the Extra CFs picker. Called
    // eagerly on the activate-toggle so re-activation always renders with
    // all groups collapsed; otherwise stale detailSections keys from a
    // prior activation paint as expanded with chevrons that don't match
    // (chevron/content desync). Iterates the full detailSections key set
    // so the reset works even when extraCFGroups isn't populated yet.
    resetExtraCFGroupCollapse() {
      const updated = { ...this.detailSections };
      for (const k of Object.keys(updated)) {
        if (k.startsWith('extra_')) updated[k] = false;
      }
      this.detailSections = updated;
    },

    // ---------------------------------------------------------------
    // Compare classification — taxonomy + status + sub-tab visibility
    // ---------------------------------------------------------------
    // Taxonomy answers: where does this CF live in the guide profile?
    //   'required'   = formatItem, OR cf.required:true inside a
    //                  default-on group (locked-on by guide default).
    //   'default-on' = default-on group + cf.default:true (on by
    //                  default but unlocked — user can opt out).
    //   'optional'   = everything else: optional cf-group (regardless
    //                  of internal flags), OR a non-required/non-
    //                  default CF inside a default-on group (e.g. MP3
    //                  inside Audio Formats).
    // Status overlays the diff vs Arr profile on top of taxonomy.
    //   'match'       = exists + scoreMatch + in active scope
    //   'wrong'       = exists + !scoreMatch + in active scope
    //   'missing'     = guide expects on (taxonomy required/default-on)
    //                   but Arr has score=0 or CF doesn't exist
    //   'optional-on' = optional taxonomy + user opted in (scored)
    //   'optional-off'= optional taxonomy + user has NOT opted in
    //   'na'          = hidden everywhere (exclusive-group losers)
    // ---------------------------------------------------------------
    cfTaxonomy(cf, group) {
      if (!group) return 'required';
      if (group.defaultEnabled && cf.required) return 'required';
      if (group.defaultEnabled && cf.default) return 'default-on';
      return 'optional';
    },
    cfRowStatus(cf, group) {
      if (group?.exclusive) {
        // Exclusive groups (Golden Rule HD/UHD, HDR Formats SDR):
        // user must pick at most one. Unchosen variants at score=0
        // are correct-per-exclusivity, not a diff — hide via 'na'.
        const scoredCount = (group.cfs || []).filter(c => c.exists && c.currentScore !== 0).length;
        if (scoredCount === 0) {
          // No variant picked. For default-OFF exclusive groups the
          // user has nothing to fix — they opted out. For default-ON
          // ones the guide expects ONE pick; surface only the
          // TRaSH-recommended variant (cf.default) as missing so the
          // user gets a single actionable signal (not all variants
          // flashing red, which would over-promote the alternative).
          if (group.defaultEnabled && cf.default) return 'missing';
          return 'na';
        }
        if (cf.currentScore === 0) return 'na'; // unchosen variant when another is picked
        if (scoredCount > 1) return 'wrong'; // exclusive violation flag the scored ones
        return cf.scoreMatch ? 'match' : 'wrong';
      }
      const tax = this.cfTaxonomy(cf, group);
      if (tax === 'required' || tax === 'default-on') {
        // Guide expects this on. Score=0 or missing-from-Arr is missing.
        if (!cf.exists) return 'missing';
        if (cf.currentScore === 0 && cf.desiredScore !== 0) return 'missing';
        return cf.scoreMatch ? 'match' : 'wrong';
      }
      // Optional taxonomy
      if (cf.exists && cf.currentScore !== 0) {
        // User opted in — compare vs guide score (which is the
        // recommended score when on)
        return cf.scoreMatch ? 'match' : 'wrong';
      }
      return 'optional-off';
    },
    // Is this CF visible in the current sub-tab pane?
    cfVisibleIn(cf, group, subTab) {
      const st = this.cfRowStatus(cf, group);
      if (st === 'na') return false;
      const tax = this.cfTaxonomy(cf, group);
      const inDefault = (tax === 'required' || tax === 'default-on');
      const optedIn = (tax === 'optional' && st !== 'optional-off');
      switch (subTab || this.compareFilter) {
        case 'overview':
          // Required-by-default rows only (no optional, no opted-in optional)
          return inDefault;
        case 'optional':
          // All optional offerings — opted-in or not (showing the
          // available menu of TRaSH optionals)
          return tax === 'optional';
        case 'all-diffs':
          // All meaningful diffs across all taxonomies.
          // 'missing' on default-on/required, 'wrong' anywhere in scope,
          // and opted-in optionals that are wrong.
          if (st === 'missing' || st === 'wrong') return true;
          return false;
        case 'wrong':
          // Score mismatches only, scoped to default-on + opted-in.
          return st === 'wrong';
        case 'missing':
          // Missing only — strictly required/default-on bucket.
          return st === 'missing';
        case 'all-active':
          // Arr-perspective: anything actually scored in Arr right now.
          return !!(cf.exists && cf.currentScore !== 0);
        case 'extra':
        case 'general':
        case 'quality':
          // These tabs don't render CF rows at all.
          return false;
        default:
          return inDefault;
      }
    },
    // Section-block visibility — show the group header if at least one
    // of its CFs is visible in the current sub-tab.
    cmpGroupVisibleIn(group, subTab) {
      return (group?.cfs || []).some(cf => this.cfVisibleIn(cf, group, subTab));
    },
    // Required-CFs section (formatItems) visibility — same idea but
    // for the synthetic Required CFs block. Synthesise a status using
    // the always-required taxonomy.
    cmpRequiredVisibleIn(cr, subTab) {
      return (cr?.formatItems || []).some(fi => {
        // formatItems behave like required taxonomy; reuse the same
        // status logic by treating each fi as a CF with required=true
        // in a synthetic default-on group context.
        const st = this.cfRowStatus(fi, null);
        if (st === 'na') return false;
        const tab = subTab || this.compareFilter;
        if (tab === 'overview' || tab === 'all-diffs' || tab === 'wrong' || tab === 'missing') {
          if (tab === 'wrong') return st === 'wrong';
          if (tab === 'missing') return st === 'missing';
          if (tab === 'all-diffs') return st === 'missing' || st === 'wrong';
          return true; // overview
        }
        if (tab === 'all-active') return !!(fi.exists && fi.currentScore !== 0);
        return false;
      });
    },
    // Group taxonomy badge — 'default-on' or 'optional'.
    cmpGroupBadge(group) {
      return group?.defaultEnabled ? 'default-on' : 'optional';
    },
    // CF taxonomy short label (for per-row badge).
    cmpTaxonomyLabel(cf, group) {
      const t = this.cfTaxonomy(cf, group);
      if (t === 'required') return 'Required';
      if (t === 'default-on') return 'Default on';
      return 'Optional';
    },
    // Plain-language pane description shown at the top of each sub-tab.
    cmpPaneDescription(subTab) {
      switch (subTab || this.compareFilter) {
        case 'overview':
          return 'What the guide turns on by default — Required CFs and the CFs in groups the guide enables out of the box. Shows match, wrong score, or missing for each.';
        case 'optional':
          return 'CFs the guide makes available but does not turn on by default. Opt-in only.';
        case 'general':
          return 'Top-level profile settings: language, upgrades allowed, min/cutoff scores.';
        case 'quality':
          return 'Quality items and cutoff. Shows what is enabled in your profile vs what the guide enables.';
        case 'all-diffs':
          return 'Everything that differs from the guide — grouped by Required → Default on → Optional. Optional CFs only appear here if you have enabled them.';
        case 'wrong':
          return 'CFs where your Arr score does not match the guide score. Includes optional CFs you have opted into.';
        case 'extra':
          return 'CFs scored in your Arr profile that are not part of this guide profile at all.';
        case 'missing':
          return 'CFs the guide turns on by default but your Arr profile does not have on (score is 0 or CF is missing).';
        case 'all-active':
          return 'Every CF that has a score in your Arr profile, whether or not the guide includes it.';
        default:
          return '';
      }
    },
    // Compare filter visibility predicates. Called from x-show on CF rows in every diff section.
    // Legacy helper kept for backwards-compat in other tables (extras,
    // settings rows). For CF rows, prefer cfVisibleIn(cf, group).
    compareRowVisible(status) {
      // status: 'match' | 'wrong' | 'missing' | 'extra' | 'optional-on' | 'optional-off' | 'na'
      if (status === 'na') return false;
      switch (this.compareFilter) {
        case 'overview': return status === 'match' || status === 'wrong' || status === 'missing';
        case 'optional': return status === 'optional-on' || status === 'optional-off';
        case 'all-diffs': return status === 'wrong' || status === 'missing';
        case 'wrong': return status === 'wrong';
        case 'missing': return status === 'missing';
        case 'extra': return status === 'extra';
        case 'all-active': return status !== 'optional-off' && status !== 'na';
        // Defensive fallback for unrecognised filter values (e.g. an
        // old persisted state value like 'all' / 'diff' / 'match').
        // Render the row instead of swallowing it — better to over-
        // show than to display an empty Compare view silently.
        default: return true;
      }
    },
    // Determine status class for a format-item row (required CF or group CF)
    // A CF with score=0 when the guide expects non-zero is functionally
    // identical to "not in the profile" — Arr's scoring engine ignores
    // 0-score entries. Two thin delegations to the unified classifier
    // so legacy callsites (compareRowVisible, ad-hoc x-show conditions
    // in older templates) keep working without churn.
    compareFormatItemStatus(fi) {
      return this.cfRowStatus(fi, null);
    },
    compareGroupCFStatus(cf, group) {
      return this.cfRowStatus(cf, group);
    },

    // Strip HTML tags for use as a plain-text tooltip (TRaSH descriptions are HTML fragments)
    stripHtml(html) {
      if (!html) return '';
      const tmp = document.createElement('div');
      tmp.innerHTML = html;
      return (tmp.textContent || tmp.innerText || '').trim();
    },

    // Returns compare summary counts for the sub-nav. Backend totals
    // (s.missing / s.wrongScore) classify by name-match only — a CF
    // with score=0 when guide expects non-zero counts as "wrong score"
    // there. Frontend reclassifies these as "missing" (they're
    // functionally absent — Arr ignores 0-score entries), so we
    // recompute missing + wrong client-side using the per-row helpers
    // for consistency with what the user sees in the tables.
    compareAdjustedCounts(cr) {
      const s = cr?.summary || {};
      const extra = s.extra || 0;
      const settings = s.settingsDiffs || 0;
      const quality = s.qualityDiffs || 0;
      // Walk both data sources (Required formatItems + Group CFs) and
      // tally per the helper-driven classification. Mirrors what the
      // filter renders so sub-nav counts and visible rows agree.
      let missing = 0, wrong = 0, overview = 0, optional = 0, allActive = 0;
      for (const fi of (cr?.formatItems || [])) {
        const st = this.cfRowStatus(fi, null);
        if (st === 'na') continue;
        overview++;
        if (st === 'missing') missing++;
        else if (st === 'wrong') wrong++;
        if (fi.exists && fi.currentScore !== 0) allActive++;
      }
      for (const g of (cr?.groups || [])) {
        for (const cf of (g.cfs || [])) {
          const st = this.cfRowStatus(cf, g);
          if (st === 'na') continue;
          const tax = this.cfTaxonomy(cf, g);
          if (tax === 'optional') optional++;
          else overview++;
          if (st === 'missing') missing++;
          else if (st === 'wrong') wrong++;
          if (cf.exists && cf.currentScore !== 0) allActive++;
        }
      }
      const allDiffs = wrong + missing;
      // Legacy aliases (`diffs`, `all`) kept so older callsites that
      // haven't been migrated to the new sub-nav names still work.
      // `optional` here = TOTAL optional offerings (count rendered in
      // the Optional sub-nav), not "user-activated optional" like
      // the previous semantics. Summary-bar code that wants the old
      // meaning needs updating, not this helper.
      return {
        overview, optional, missing, wrong, extra, settings, quality,
        allDiffs, allActive,
        diffs: allDiffs + extra + settings + quality, // legacy alias
        all: overview + optional, // legacy alias (rough)
      };
    },

    // Effective diff: how many leaf resolutions end up with a different `allowed` state than the
    // TRaSH original after the user's edits. Grouping/rename/reorder alone don't count — only
    // changes that actually affect the sync outcome (which resolutions Arr will see as enabled).
    pdQualityItemsChangeCount() {
      // Flatten a structure to a {leafName → allowed} map. Groups push their allowed down to members.
      const leafMap = (items) => {
        const m = new Map();
        for (const it of items || []) {
          if (it.items && it.items.length > 0) {
            for (const leaf of it.items) m.set(leaf, !!it.allowed);
          } else {
            m.set(it.name, !!it.allowed);
          }
        }
        return m;
      };
      const orig = leafMap(this.profileDetail?.detail?.profile?.items);
      if (this.qualityStructure.length > 0) {
        const cur = leafMap(this.qualityStructure);
        let n = 0;
        for (const [name, allowed] of cur) {
          if (orig.get(name) !== allowed) n++;
        }
        // Leaves dropped entirely from structure (rare) count too
        for (const name of orig.keys()) if (!cur.has(name)) n++;
        return n;
      }
      return Object.keys(this.qualityOverrides).length;
    },

    // Reset General values to TRaSH defaults (keeps override toggle ON so user can re-edit).
    pdResetGeneral() {
      const p = this.profileDetail?.detail?.profile || {};
      this.pdOverrides.language.value = p.language || 'Original';
      this.pdOverrides.upgradeAllowed.value = p.upgradeAllowed ?? true;
      this.pdOverrides.minFormatScore.value = p.minFormatScore ?? 0;
      this.pdOverrides.minUpgradeFormatScore.value = p.minUpgradeFormatScore ?? 1;
      this.pdOverrides.cutoffFormatScore.value = p.cutoffFormatScore || p.cutoffScore || 10000;
    },

    // Reset Quality cutoff to TRaSH default (keeps override toggle ON).
    pdResetQuality() {
      const p = this.profileDetail?.detail?.profile || {};
      this.pdOverrides.cutoffQuality = p.cutoff || '';
    },

    // Full reset: all overrides back to TRaSH, toggles off, editor state cleared.
    // Values stored in pdOverrides are reset to the current profile's defaults.
    pdResetAllOverrides() {
      this.pdResetGeneral();
      this.pdResetQuality();
      this.pdResetDetailState();
    },

    // Clear all profile-detail override flags and transient editor state.
    // Does NOT touch pdOverrides values — caller handles that via pdInitOverrides() if needed.
    // Used by: loadProfileDetail (fresh load), Back-link (leaving the view), pdResetAllOverrides.
    pdResetDetailState() {
      this.pdOverridesEnabled = false;
      this.pdDescription = '';
      this.pdDescriptionPreview = false;
      this.pdNotesExpanded = false;
      this.pdGeneralCollapsed = false;
      this.pdQualityCollapsed = false;
      this.pdCFScoresCollapsed = true;
      this.pdExtraCFsCollapsed = true;
      this.cfScoreOverrides = {};
      this.qualityOverrides = {};
      this.qualityOverrideActive = false;
      this.qualityStructure = [];
      this.qualityStructureEditMode = false;
      this.qualityStructureExpanded = {};
      this.qualityStructureRenaming = null;
      this.extraCFs = {};
      this.extraCFSearch = '';
      // NOTE: extraCFAllCFs + extraCFGroups deliberately NOT cleared.
      // They hold the heavy /all-cfs catalog which is identical for
      // every profile of the same Arr type. loadExtraCFList() checks
      // _extraCFGroupsCachedType and skips the network fetch when the
      // cache still matches — wiping it here would force a refetch on
      // every profile-detail open. Invalidated by Pull/Reset via
      // clearTrashDerivedCaches.
      this._extraInProfileSet = null;
    },

    // Toggle handler for "Reset to profile defaults" button. Shows a confirm
    // modal listing what will be cleared (per-section breakdown), then either
    // calls pdDisableOverrides() on confirm or no-ops on cancel. When there are
    // no actual overrides yet (user just enabled the toggle), skips the modal
    // and disables silently — nothing to lose.
    pdConfirmDisable() {
      const s = this.pdOverrideSummary();
      if (s.total === 0 && s.optional === 0) {
        this.pdDisableOverrides();
        return;
      }
      const overridesParts = [];
      if (s.general > 0) overridesParts.push(`General: ${s.general}`);
      if (s.quality > 0) overridesParts.push(`Quality: ${s.quality}`);
      if (s.customizations > 0) overridesParts.push(`CF Customizations: ${s.customizations}`);
      const lines = [];
      if (s.total > 0) {
        lines.push(`${s.total} override${s.total === 1 ? '' : 's'}: ${overridesParts.join(', ')}`);
      }
      if (s.optional > 0) {
        lines.push(`${s.optional} optional CF activation${s.optional === 1 ? '' : 's'} across the TRaSH groups`);
      }
      const breakdown = lines.join('\n');
      this.confirmModal = {
        show: true,
        title: 'Reset profile to defaults?',
        message: `The following will be cleared:\n\n${breakdown}\n\nOn the next Save & Sync, this profile will revert to profile defaults in your Arr instance.\n\n⚠ This action is permanent. The only way to recover is to roll back from this profile's sync history.`,
        confirmLabel: 'Reset to profile defaults',
        onConfirm: () => this.pdDisableOverrides(),
        onCancel: () => {},
      };
    },

    // Disable the Profile Detail overrides toggle. Clears all override state
    // (general, quality, scores, extras) AND resets CF-group state
    // (selectedOptionalCFs) so the next Save & Sync sends a clean body that
    // reverts the rule to profile defaults — equivalent to creating a fresh
    // sync from scratch. Caller (pdConfirmDisable) is responsible for
    // showing the confirm modal first.
    pdDisableOverrides() {
      this.pdOverridesEnabled = false;
      // Re-seed pdOverrides from profile defaults so input fields show clean
      // values if the user immediately re-enables the toggle.
      this.pdInitOverrides(this.profileDetail?.detail?.profile || null);
      this.cfScoreOverrides = {};
      this.qualityOverrides = {};
      this.qualityOverrideActive = false;
      this.qualityStructure = [];
      this.qualityStructureEditMode = false;
      this.qualityStructureExpanded = {};
      this.qualityStructureRenaming = null;
      this.extraCFs = {};
      // Re-seed CF-group state from TRaSH defaults: every default-enabled
      // group is on (with its default-on optional CFs selected), every
      // default-disabled group is off (no per-CF toggles set). Without this
      // call, a user who'd toggled "Optional Movie Versions" on or
      // "Unwanted Formats" off would see those decisions persist past
      // Reset — exactly the divergence the button is meant to undo.
      const detail = this.profileDetail?.detail;
      if (detail) this.initSelectedCFs(detail);
      else this.selectedOptionalCFs = {};
    },

    // Seed pdOverrides from a profile's TRaSH defaults (or global defaults if no profile).
    pdInitOverrides(p) {
      p = p || {};
      this.pdOverrides = {
        language: { enabled: true, value: p.language || 'Original' },
        upgradeAllowed: { enabled: true, value: p.upgradeAllowed ?? true },
        minFormatScore: { enabled: true, value: p.minFormatScore ?? 0 },
        minUpgradeFormatScore: { enabled: true, value: p.minUpgradeFormatScore ?? 1 },
        cutoffFormatScore: { enabled: true, value: p.cutoffFormatScore || p.cutoffScore || 10000 },
        cutoffQuality: p.cutoff || '',
      };
    },

    // --- Profile-editor unsaved-changes tracking (Issue #52) ---

    // Snapshot the profile-editor state as a JSON string. Cheap diff
    // primitive: compare the snapshot vs a re-snapshot to detect any
    // change. Called after openProfileDetail + resyncProfile finish
    // restoring state, so the baseline reflects "what the rule looks
    // like right now on disk". Future edits diverge from this.
    _snapshotProfileEditor() {
      // Quality structure: when it equals the profile's defaults the
      // user hasn't actually changed anything, even if qsInitFromProfile
      // happened to seed it (the Qualities edit button does this just
      // to populate the modal). Normalise that case to `null` so
      // opening the editor doesn't flip the dirty flag and trigger
      // beforeunload prompts.
      const qStruct = (this.qualityStructure && this.qualityStructure.length > 0
                       && !this.qualityStructureMatchesDefaults())
        ? this.qualityStructure
        : null;
      return JSON.stringify({
        sel: this.selectedOptionalCFs,
        cfScore: this.cfScoreOverrides,
        extras: this.extraCFs,
        pdOv: this.pdOverrides,
        qStruct,
        qOver: this.qualityOverrides,
        desc: (this.pdDescription || '').trim(),
      });
    },

    // Capture the current state as the dirty-tracking baseline.
    // Invoked by openProfileDetail at the end of state restoration.
    _captureProfileBaseline() {
      this._profileBaseline = this._snapshotProfileEditor();
    },

    // Clear the baseline so subsequent dirty-checks return false until
    // a new editor session opens. Called on save-success + Discard.
    _clearProfileBaseline() {
      this._profileBaseline = null;
    },

    // True when the editor has unsaved changes vs the baseline taken
    // at load time. Returns false when no editor is open (no
    // profileDetail) or no baseline was captured yet (race window
    // during initial load — better to say "not dirty" than to gate
    // navigation on a snapshot that doesn't exist).
    profileDetailIsDirty() {
      if (!this.profileDetail || !this._profileBaseline) return false;
      return this._snapshotProfileEditor() !== this._profileBaseline;
    },

    // Centralised editor-close handler. Called by the Cancel button +
    // sidebar navigation guards. When dirty, shows a Stay / Discard
    // modal. The done callback fires after Discard (or immediately if
    // not dirty) — caller passes a function that performs whatever
    // destination action they wanted (route change, app switch,
    // open-other-rule, etc.).
    closeProfileEditor(done) {
      const finish = () => {
        this._clearProfileBaseline();
        this.profileDetail = null;
        this.syncPlan = null;
        this.syncResult = null;
        this.selectedOptionalCFs = {};
        this.groupExpanded = {};
        this.detailSections = { core: true };
        this.pdResetDetailState();
        this.pdInitOverrides(null);
        if (typeof done === 'function') done();
      };
      if (!this.profileDetailIsDirty()) {
        finish();
        return;
      }
      this.confirmModal = {
        show: true,
        title: 'Unsaved changes',
        message: 'You have unsaved changes in the profile editor. Stay to keep editing, or discard the changes to leave.',
        cancelLabel: 'Stay on this page',
        confirmLabel: 'Discard changes',
        confirmStyle: 'border-color:var(--accent-red);color:var(--accent-red)',
        onConfirm: finish,
        onCancel: () => {},
      };
    },

    // Compare convergence entry point. Opens Profile Detail overlay with the
    // user's saved sync rule pre-loaded (via applyRuleStateToEditor) AND
    // comparison-derived overrides layered on top (via
    // prefillOverridesFromCompare). The user then edits in the same UI as
    // a normal Profile Sync — buildSyncBody / startApply / handleApply all
    // run identical code to Profile Sync's Save & Sync, so the rule and
    // Sync All stay consistent.
    async openCompareEditor(inst, arrProfileId, trashProfileId) {
      const arrIdNum = parseInt(arrProfileId, 10);
      if (!arrIdNum) { this.showToast('Pick an Arr profile first', 'error', 5000); return; }
      const trashProfile = (this.trashProfiles[inst.type] || []).find(p => p.trashId === trashProfileId);
      if (!trashProfile) { this.showToast('TRaSH profile not found', 'error', 5000); return; }
      const comparison = this.instCompareResult[inst.id];
      if (!comparison || comparison.trashProfileId !== trashProfileId || comparison.arrProfileId !== arrIdNum) {
        this.showToast('Run compare first', 'error', 5000);
        return;
      }
      this.activeAppType = inst.type;

      // Pre-flight: detect CFs that exist in the Arr profile but not in
      // Clonarr (neither TRaSH guide nor user custom). Without warning,
      // they silently drop from the editor and get removed from the Arr
      // profile on the next sync — Clonarr only manages what it knows
      // about. Load the catalog first, then surface a modal with the
      // user's three options: cancel, continue without importing, or
      // import them as Clonarr custom formats and then continue.
      //
      // Explicit appType arg because profileDetail isn't set yet — the
      // default loadExtraCFList() path reads it from profileDetail and
      // would no-op here.
      await this.loadExtraCFList(inst.type);
      const nameToTrashId = {};
      for (const cf of (this.extraCFAllCFs || [])) {
        if (cf && cf.name && cf.trashId) nameToTrashId[cf.name] = cf.trashId;
      }
      const arrOnlyNames = [];
      for (const ecf of (comparison.extraCFs || [])) {
        if (!nameToTrashId[ecf.name]) arrOnlyNames.push(ecf.name);
      }
      if (arrOnlyNames.length > 0) {
        const choice = await this._promptArrOnlyImport(inst, arrOnlyNames);
        if (choice === 'cancel') return;
        if (choice === 'import') {
          const ok = await this._importArrOnlyCFs(inst, arrOnlyNames);
          if (!ok) return;
          // Refresh the catalog so the freshly-imported customs resolve
          // in prefillOverridesFromCompare below — without this, they'd
          // still land in _compareArrOnlyExtras as if never imported.
          // Explicit appType again since profileDetail still isn't set.
          this._extraCFGroupsCachedType = null;
          this.extraCFAllCFs = [];
          await this.loadExtraCFList(inst.type);
        }
        // 'continue' path falls through with arrOnlyNames left unresolved;
        // they land in _compareArrOnlyExtras and the sync drops them.
      }

      // restoreFromRule=true: Compare → Edit & Sync is editing an existing
      // rule for the Arr profile we ran compare against. Auto-restore loads
      // the rule's saved state; prefillOverridesFromCompare below layers
      // Arr-side drift on top.
      await this.openProfileDetail(inst, trashProfile, true);
      // Lock subsequent Save & Sync to the Arr profile we're editing.
      this.profileDetail._arrProfileName = comparison.arrProfileName || this.resolveArrProfileName(inst.id, arrIdNum) || null;
      this.profileDetail._editLockedArrProfileId = arrIdNum;
      this.resyncTargetArrProfileId = arrIdNum;
      // loadExtraCFList is idempotent + cache-gated, so this is a no-op
      // when the pre-flight already loaded it (covers the rare case where
      // openProfileDetail invalidated the cache during reset).
      await this.loadExtraCFList();
      // applyRuleStateToEditor already ran via openProfileDetail's auto-restore
      // path if exactly one rule matches (instance + trashProfile). Layer the
      // compare-derived overrides on top so Arr-state-vs-TRaSH-default deltas
      // surface in the editor too.
      this.prefillOverridesFromCompare(comparison);
    },

    // Returns 'cancel' | 'continue' | 'import' depending on which button
    // the user clicks in the pre-flight Arr-only-CFs prompt. Uses the
    // shared confirmModal with its three-button shape (cancel +
    // secondary + primary) so we don't need a dedicated modal partial.
    _promptArrOnlyImport(inst, arrOnlyNames) {
      const appLabel = inst.type === 'radarr' ? 'Radarr' : 'Sonarr';
      const countLabel = arrOnlyNames.length === 1 ? 'custom format' : 'custom formats';
      return new Promise(resolve => {
        this.confirmModal = {
          show: true,
          wide: true,
          title: `${arrOnlyNames.length} ${countLabel} in ${appLabel} not in Clonarr`,
          message: `These ${countLabel} exist in ${inst.name} but Clonarr doesn't know about them yet. To keep them on this sync rule, import them as Clonarr custom formats first. Otherwise they will be removed from ${appLabel} the next time this rule syncs (Clonarr only manages what it knows about).`,
          details: arrOnlyNames,
          cancelLabel: 'Cancel',
          secondaryLabel: 'Continue without importing',
          confirmLabel: 'Import and continue',
          onCancel: () => resolve('cancel'),
          onSecondary: () => resolve('continue'),
          onConfirm: () => resolve('import'),
        };
      });
    },

    // POSTs to /api/custom-cfs/import-from-instance with the list of CF
    // names to pull from the source Arr. Returns true on success, false
    // when the import fails or returns no added rows (in which case the
    // caller aborts before opening the editor). Skipped name collisions
    // get surfaced in the success toast so the user knows some weren't
    // imported and why.
    async _importArrOnlyCFs(inst, cfNames) {
      try {
        const r = await fetch('/api/custom-cfs/import-from-instance', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            instanceId: inst.id,
            cfNames,
            category: 'Custom',
            appType: inst.type,
          }),
        });
        if (!r.ok) {
          let msg = `Import failed (HTTP ${r.status})`;
          try { const err = await r.json(); if (err?.error) msg = err.error; } catch (_) {}
          this.showToast(msg, 'error', 8000);
          return false;
        }
        const result = await r.json().catch(() => ({}));
        const added = Number(result.added) || 0;
        const skipped = Array.isArray(result.skippedCollisions) ? result.skippedCollisions.length : 0;
        if (added === 0) {
          this.showToast('No custom formats were imported (all skipped due to name collisions).', 'warning', 8000);
          return false;
        }
        let msg = `Imported ${added} custom format${added === 1 ? '' : 's'} from ${inst.name}.`;
        if (skipped > 0) {
          msg += ` Skipped ${skipped} due to name collisions — those stay in ${inst.type === 'radarr' ? 'Radarr' : 'Sonarr'} unmanaged.`;
        }
        this.showToast(msg, 'success', 6000);
        // Refresh the Custom Formats tab data so the newly-imported CFs
        // show up there too without requiring a page reload.
        await this.loadCFBrowse(inst.type);
        return true;
      } catch (e) {
        this.showToast(`Import failed: ${e.message}`, 'error', 8000);
        return false;
      }
    },


    // Compare → Override-editor translation. Walks a /api/instances/X/compare
    // response and populates the same state tree the profile-detail override
    // editor uses (pdOverrides, qualityOverrides, cfScoreOverrides,
    // selectedOptionalCFs, plus a transient list of Arr-only extras).
    //
    // Caller pattern:
    //   await this.openProfileDetail(inst, profile);  // seeds profile defaults
    //   this.prefillOverridesFromCompare(comparison); // overlays Arr's current
    //                                                 // state as overrides
    //
    // Arr-only extras (CFs in Arr but not in any TRaSH cf-group, e.g. user-
    // imported release-group customs) live in this._compareArrOnlyExtras —
    // they can't live in pdOverrides.extraCFs (trash-id-keyed) until those
    // CFs are imported as clonarr custom CFs first.
    prefillOverridesFromCompare(comparison) {
      if (!comparison) return false;
      let anyOverride = false;

      // --- General settings overrides ---
      for (const sd of (comparison.settingsDiffs || [])) {
        if (sd.match) continue;
        switch (sd.name) {
          case 'Language':
            this.pdOverrides.language.enabled = false;
            this.pdOverrides.language.value = sd.current;
            anyOverride = true;
            break;
          case 'Upgrade Allowed':
            this.pdOverrides.upgradeAllowed.enabled = false;
            this.pdOverrides.upgradeAllowed.value = sd.current === 'true' || sd.current === true;
            anyOverride = true;
            break;
          case 'Min Format Score':
            this.pdOverrides.minFormatScore.enabled = false;
            this.pdOverrides.minFormatScore.value = parseInt(sd.current) || 0;
            anyOverride = true;
            break;
          case 'Min Upgrade Format Score':
            this.pdOverrides.minUpgradeFormatScore.enabled = false;
            this.pdOverrides.minUpgradeFormatScore.value = parseInt(sd.current) || 1;
            anyOverride = true;
            break;
          case 'Cutoff Format Score':
            this.pdOverrides.cutoffFormatScore.enabled = false;
            this.pdOverrides.cutoffFormatScore.value = parseInt(sd.current) || 10000;
            anyOverride = true;
            break;
          case 'Cutoff':
            this.pdOverrides.cutoffQuality = sd.current;
            anyOverride = true;
            break;
        }
      }

      // --- Quality items overrides (Cutoff lives in settingsDiffs above) ---
      // Uses the legacy flat qualityOverrides map; profile-detail's editor
      // can promote to qualityStructure later if user reorders.
      for (const qd of (comparison.qualityDiffs || [])) {
        if (qd.match) continue;
        this.qualityOverrides[qd.name] = qd.currentAllowed;
        anyOverride = true;
      }

      // --- CF score overrides (formatItems + group CFs) ---
      // For every diff CF that exists in Arr but with a non-TRaSH score,
      // record current score as an override. The override editor's
      // "Overridden Scores" card surfaces these.
      for (const fi of (comparison.formatItems || [])) {
        if (!fi.exists || fi.scoreMatch) continue;
        this.cfScoreOverrides[fi.trashId] = fi.currentScore;
        anyOverride = true;
      }
      for (const group of (comparison.groups || [])) {
        for (const cf of group.cfs) {
          if (!cf.exists || cf.scoreMatch) continue;
          this.cfScoreOverrides[cf.trashId] = cf.currentScore;
          anyOverride = true;
        }
      }

      // --- Group activation state ---
      // Whatever's inUse on Arr right now becomes the set we're syncing.
      // Required CFs in default-enabled groups are implicitly tracked via
      // group.defaultEnabled and don't need explicit selectedOptionalCFs
      // entries unless the user opted out.
      for (const group of (comparison.groups || [])) {
        let anyInUse = false;
        for (const cf of group.cfs) {
          if (cf.inUse) {
            this.selectedOptionalCFs[cf.trashId] = true;
            anyInUse = true;
          }
        }
        // Track group on/off state when it diverges from default.
        if (anyInUse && !group.defaultEnabled) {
          this.selectedOptionalCFs['__grp_' + group.name] = true;
        } else if (!anyInUse && group.defaultEnabled) {
          this.selectedOptionalCFs['__grp_' + group.name] = false;
        }
      }

      // --- Additional CFs (CFs in Arr profile but not in any TRaSH cf-group
      // for this profile). These carry name + arrCFID + score in the
      // comparison response — no trashID. To surface them in the editor's
      // Additional CFs card (which is keyed by trashID), look up name in
      // extraCFAllCFs (loaded by loadExtraCFList in openCompareEditor) and
      // populate this.extraCFs by the resolved trashID. Includes:
      //   - User-created clonarr custom CFs (synthetic trashID like
      //     custom:abc123, e.g. "!PL Tier 02", "Dubs Only", "2.0 Stereo").
      //   - TRaSH CFs the user added to their profile but that aren't in
      //     a cf-group included by this profile (e.g. user manually
      //     scored "Hybrid" via direct API edit on a profile that doesn't
      //     include the [Optional] Movie Versions group).
      // Anything we can't resolve by name (truly Arr-only, no TRaSH or
      // custom CF backing it) drops into _compareArrOnlyExtras for
      // possible future Arr-only-customs UI.
      const nameToTrashId = {};
      for (const cf of (this.extraCFAllCFs || [])) {
        if (cf && cf.name && cf.trashId) nameToTrashId[cf.name] = cf.trashId;
      }
      const extras = { ...(this.extraCFs || {}) };
      const selOpt = { ...(this.selectedOptionalCFs || {}) };
      const unresolved = [];
      let resolvedExtras = false;
      for (const ecf of (comparison.extraCFs || [])) {
        const tid = nameToTrashId[ecf.name];
        if (tid) {
          extras[tid] = ecf.score;
          // Mark the CF as explicitly enabled so the per-CF toggle in
          // the editor renders ON (read by `:checked="selectedOptionalCFs[cf.trashId]"`).
          // Without this, the toggle visually reads false even though
          // the CF is in extras and would sync correctly — the "X of Y
          // enabled" counter also reads from this map, so the user
          // sees "0 of N enabled" while the CF is actually active.
          selOpt[tid] = true;
          resolvedExtras = true;
        } else {
          unresolved.push({ arrCFID: ecf.format, name: ecf.name, currentScore: ecf.score });
        }
      }
      this.extraCFs = extras;
      this.selectedOptionalCFs = selOpt;
      this._compareArrOnlyExtras = unresolved;

      // Customize-this-profile must flip on whenever the comparison
      // surfaced any divergence the user can act on — that includes
      // resolved Additional CFs (TRaSH-guide or freshly-imported
      // customs) and unresolved Arr-only CFs alike. Without this,
      // the editor opens in non-customize mode and the Additional CFs
      // section stays hidden, so the user thinks the import / extra
      // didn't take. The save engine still emits extras regardless of
      // the toggle, but the editor UI gates visibility on it.
      if (anyOverride || resolvedExtras || (this._compareArrOnlyExtras && this._compareArrOnlyExtras.length > 0)) {
        this.pdOverridesEnabled = true;
      }
      return anyOverride;
    },

    // ======================================================================
    // Quality Structure Override (full structure replacing TRaSH items)
    // ======================================================================

    // Initialize qualityStructure from the current profile's items.
    // Bakes any legacy flat overrides into the structure, then clears them.
    qsInitFromProfile() {
      const items = this.profileDetail?.detail?.profile?.items || [];
      if (items.length === 0) return;
      this.qualityStructure = items.map(it => {
        // Apply legacy flat override if present
        const legacy = this.qualityOverrides[it.name];
        const allowed = (legacy !== undefined) ? legacy : !!it.allowed;
        const out = { _id: ++this._qsIdCounter, name: it.name, allowed };
        if (it.items && it.items.length > 0) {
          out.items = [...it.items];
        }
        return out;
      });
      // Now that legacy is migrated, clear it (we only want one source of truth)
      this.qualityOverrides = {};
    },

    // Trash default lookup helper (used for "is overridden" indicator)
    qsTrashDefaultFor(name) {
      const items = this.profileDetail?.detail?.profile?.items || [];
      return items.find(i => i.name === name);
    },

    qsIsOverridden(item) {
      const def = this.qsTrashDefaultFor(item.name);
      if (!def) return true; // user-created group
      if (!!def.allowed !== !!item.allowed) return true;
      const a = (def.items || []).slice().sort().join('|');
      const b = (item.items || []).slice().sort().join('|');
      return a !== b;
    },

    // Returns true if the TRaSH default cutoff name is a valid allowed entry given the current state.
    // When no structure override is active, the TRaSH default is always valid (sourced from profile.items).
    // When structure override is active, it's only valid if a top-level allowed item with that exact name exists.
    qsTrashDefaultCutoffValid() {
      const trashCutoff = this.profileDetail?.detail?.profile?.cutoff || '';
      if (!trashCutoff) return false;
      if (this.qualityStructure.length === 0) return true;
      return this.qualityStructure.some(it => it.name === trashCutoff && it.allowed);
    },

    // Validate pdOverrides.cutoffQuality against the current source-of-truth.
    // If invalid (name no longer exists or is disabled), reset to first allowed entry from
    // the structure (or back to TRaSH default if structure is empty).
    // Triggered reactively whenever qualityStructure changes (rename, delete, merge, etc.).
    qsValidateCutoff() {
      const cutoff = this.pdOverrides?.cutoffQuality;
      if (cutoff === undefined || cutoff === '__skip__' || cutoff === '') return;

      // When no structure override is active, fall back to profile.items as source
      const source = this.qualityStructure.length > 0
        ? this.qualityStructure
        : (this.profileDetail?.detail?.profile?.items || []);
      if (source.length === 0) return;

      // Check if current cutoff is a valid allowed entry
      const valid = source.some(it => it.name === cutoff && it.allowed);
      if (valid) return;

      // Not valid — pick first allowed as fallback
      const firstAllowed = source.find(it => it.allowed);
      this.pdOverrides.cutoffQuality = firstAllowed ? firstAllowed.name : '';
    },

    // Toggle Edit Groups mode. On first activation, lazy-init structure from TRaSH default.
    qsToggleEditMode() {
      if (!this.qualityStructureEditMode && this.qualityStructure.length === 0) {
        this.qsInitFromProfile();
      }
      this.qualityStructureEditMode = !this.qualityStructureEditMode;
      if (!this.qualityStructureEditMode) {
        this.qualityStructureExpanded = {};
        this.qualityStructureRenaming = null;
        this.qsResetDrag();
      }
    },

    qsStartRename(item) {
      this.qualityStructureRenaming = item._id;
    },

    qsResetDrag() {
      this.qualityStructureDrag = { kind: null, src: null, srcGroup: null, srcMember: null, dropGap: null, dropMerge: null, dropMemberGroup: null, dropMemberGap: null };
    },

    // Shared qs editor state (editMode / expanded / renaming) is used by BOTH the Builder's
    // inline editor and the Edit view's inline editor. Must be cleared whenever either editor
    // closes — otherwise re-opening the other one lands mid-edit with drag handles visible.
    qsCloseSharedState() {
      this.qualityStructureEditMode = false;
      this.qualityStructureExpanded = {};
      this.qualityStructureRenaming = null;
      this.qsResetDrag();
      // Defensive: reset qualityEditorTarget so a future opener that
      // forgets to set it lands on a safe default ('builder') rather
      // than a stale 'edit' that would mis-route writes.
      this.qualityEditorTarget = 'builder';
    },

    // Drag-drop on a gap → reorder (or ungroup-and-insert if dragging a member)
    // Resolve the target array for quality-editor helpers. 'edit' = profile-detail's qualityStructure,
    // 'builder' = Profile Builder's pb.qualityItems. Both share the same shape { name, allowed, items? }
    // and the same editor UI state (qualityStructureEditMode/Expanded/Renaming/Drag) — only one
    // editor is open at a time so shared state is safe.
    _qsArr(target) { return target === 'builder' ? this.pb.qualityItems : this.qualityStructure; },
    _qsSetArr(target, v) {
      if (target === 'builder') this.pb.qualityItems = v;
      else this.qualityStructure = v;
    },

    qsHandleDropOnGap(gapIdx, target = 'edit') {
      const d = this.qualityStructureDrag;
      const arr = this._qsArr(target);
      if (d.kind === 'top') {
        const src = d.src;
        if (src === gapIdx || src === gapIdx - 1) { this.qsResetDrag(); return; }
        const moved = arr.splice(src, 1)[0];
        const insertAt = src < gapIdx ? gapIdx - 1 : gapIdx;
        arr.splice(insertAt, 0, moved);
      } else if (d.kind === 'member') {
        const grp = arr[d.srcGroup];
        if (!grp || !grp.items) { this.qsResetDrag(); return; }
        const memberName = grp.items.splice(d.srcMember, 1)[0];
        const newSingle = { _id: ++this._qsIdCounter, name: memberName, allowed: false };
        let insertAt = gapIdx;
        if (grp.items.length === 0) {
          arr.splice(d.srcGroup, 1);
          if (d.srcGroup < gapIdx) insertAt -= 1;
        }
        arr.splice(insertAt, 0, newSingle);
      }
      this.qsResetDrag();
    },

    // Drag-drop on a row → merge (create group if both singles, add to group otherwise)
    qsHandleDropOnRow(targetIdx, target = 'edit') {
      const d = this.qualityStructureDrag;
      const arr = this._qsArr(target);
      if (d.kind === 'top') {
        const src = d.src;
        if (src === targetIdx) { this.qsResetDrag(); return; }
        const srcItem = arr[src];
        const tgtItem = arr[targetIdx];
        if (tgtItem.items) {
          const newMembers = srcItem.items ? srcItem.items : [srcItem.name];
          tgtItem.items.push(...newMembers);
          arr.splice(src, 1);
        } else if (srcItem.items) {
          srcItem.items.push(tgtItem.name);
          arr.splice(targetIdx, 1);
        } else {
          const defaultName = `${srcItem.name} | ${tgtItem.name}`;
          this.inputModal = {
            show: true,
            title: 'New Quality Group',
            message: 'Both qualities will be merged into a single group. Arr will treat them as equal — CF scores decide the winner.',
            value: defaultName,
            placeholder: 'Group name',
            confirmLabel: 'Create',
            onConfirm: (groupName) => {
              if (!groupName) return;
              const newGroup = {
                _id: ++this._qsIdCounter,
                name: groupName,
                allowed: true,
                items: [srcItem.name, tgtItem.name],
              };
              const indices = [src, targetIdx].sort((a, b) => b - a);
              indices.forEach(i => arr.splice(i, 1));
              const insertAt = Math.min(src, targetIdx);
              arr.splice(insertAt, 0, newGroup);
              this.qualityStructureExpanded[newGroup._id] = true;
            },
            onCancel: null,
          };
        }
      } else if (d.kind === 'member') {
        const oldGroup = arr[d.srcGroup];
        if (!oldGroup || !oldGroup.items) { this.qsResetDrag(); return; }
        const memberName = oldGroup.items.splice(d.srcMember, 1)[0];
        let tIdx = targetIdx;
        if (oldGroup.items.length === 0) {
          arr.splice(d.srcGroup, 1);
          if (d.srcGroup < tIdx) tIdx -= 1;
        }
        const tgtItem = arr[tIdx];
        if (!tgtItem) { this.qsResetDrag(); return; }
        if (tgtItem.items) {
          tgtItem.items.push(memberName);
        } else {
          const defaultName = `${memberName} | ${tgtItem.name}`;
          this.inputModal = {
            show: true,
            title: 'New Quality Group',
            message: 'Both qualities will be merged into a single group.',
            value: defaultName,
            placeholder: 'Group name',
            confirmLabel: 'Create',
            onConfirm: (groupName) => {
              if (!groupName) return;
              const newGroup = {
                _id: ++this._qsIdCounter,
                name: groupName,
                allowed: true,
                items: [memberName, tgtItem.name],
              };
              arr.splice(tIdx, 1, newGroup);
              this.qualityStructureExpanded[newGroup._id] = true;
            },
            onCancel: null,
          };
        }
      }
      this.qsResetDrag();
    },

    // Drop on a gap BETWEEN members inside a group → reorder member position,
    // move from another group, or insert a top-level single as a member at
    // that position. groupIdx = group's top-level index; gapIdx = position
    // in group.items (0 = before first, items.length = after last).
    qsHandleDropOnMemberGap(groupIdx, gapIdx, target = 'edit') {
      const d = this.qualityStructureDrag;
      const arr = this._qsArr(target);
      const grp = arr[groupIdx];
      if (!grp || !grp.items) { this.qsResetDrag(); return; }
      if (d.kind === 'member' && d.srcGroup === groupIdx) {
        // Reorder within same group
        if (d.srcMember === gapIdx || d.srcMember === gapIdx - 1) { this.qsResetDrag(); return; }
        const memberName = grp.items.splice(d.srcMember, 1)[0];
        const insertAt = d.srcMember < gapIdx ? gapIdx - 1 : gapIdx;
        grp.items.splice(insertAt, 0, memberName);
      } else if (d.kind === 'member') {
        // Move member from a different group into this group at position
        const oldGroup = arr[d.srcGroup];
        if (!oldGroup || !oldGroup.items) { this.qsResetDrag(); return; }
        const memberName = oldGroup.items.splice(d.srcMember, 1)[0];
        let realGroupIdx = groupIdx;
        if (oldGroup.items.length === 0) {
          arr.splice(d.srcGroup, 1);
          if (d.srcGroup < realGroupIdx) realGroupIdx -= 1;
        }
        if (arr[realGroupIdx] && arr[realGroupIdx].items) {
          arr[realGroupIdx].items.splice(gapIdx, 0, memberName);
        }
      } else if (d.kind === 'top') {
        // Top-level row → add as member at position (singles only; group→group
        // member-merge is ambiguous so we delegate to drop-on-row instead).
        const srcItem = arr[d.src];
        if (!srcItem || srcItem.items) { this.qsResetDrag(); return; }
        arr.splice(d.src, 1);
        let realGroupIdx = groupIdx;
        if (d.src < realGroupIdx) realGroupIdx -= 1;
        if (arr[realGroupIdx] && arr[realGroupIdx].items) {
          arr[realGroupIdx].items.splice(gapIdx, 0, srcItem.name);
        }
      }
      this.qsResetDrag();
    },

    qsDeleteGroup(idx, target = 'edit') {
      const arr = this._qsArr(target);
      const grp = arr[idx];
      if (!grp || !grp.items) return;
      const singles = grp.items.map(name => ({
        _id: ++this._qsIdCounter,
        name,
        allowed: false,
      }));
      arr.splice(idx, 1, ...singles);
    },

    qsUngroupMember(groupIdx, memberIdx, target = 'edit') {
      const arr = this._qsArr(target);
      const grp = arr[groupIdx];
      if (!grp || !grp.items) return;
      const removed = grp.items.splice(memberIdx, 1)[0];
      arr.splice(groupIdx + 1, 0, {
        _id: ++this._qsIdCounter,
        name: removed,
        allowed: false,
      });
      if (grp.items.length === 0) {
        arr.splice(groupIdx, 1);
      }
    },

    // Reset all quality overrides to TRaSH default. Clears both legacy and structure overrides.
    // Target 'edit' resets qualityStructure + qualityOverrides. Target 'builder' clears
    // pb.qualityItems (user will need to re-apply template to repopulate).
    qsResetAll(target = 'edit') {
      this.confirmModal = {
        show: true,
        title: target === 'builder' ? 'Reset Quality Items' : 'Reset Quality Overrides',
        message: target === 'builder'
          ? 'Clear all quality items?\n\nThis removes the current qualities and groups. Re-apply a template or preset to repopulate.'
          : 'Reset to profile defaults?\n\nAll override structure changes (toggles, groups, ordering, renames) will be discarded. This cannot be undone.',
        confirmLabel: 'Reset',
        onConfirm: () => {
          if (target === 'builder') {
            this.pb.qualityItems = [];
          } else {
            this.qualityStructure = [];
            this.qualityOverrides = {};
          }
          this.qualityStructureExpanded = {};
          this.qualityStructureRenaming = null;
          this.qsResetDrag();
          // Close the editor after reset — the modal is now empty
          // (no structure overrides → profile defaults flow through
          // the underlying Profile editor without the modal). Without
          // this close, the user lands on an empty editor with only
          // a Reset button, which reads as "broken".
          if (this.pb) this.pb.qualityEditorOpen = false;
          if (typeof this.qsCloseSharedState === 'function') this.qsCloseSharedState();
        },
        onCancel: null,
      };
    },

    // Strip _id before sending to backend (backend doesn't need it)
    qsForBackend() {
      return this.qualityStructure.map(it => {
        const out = { name: it.name, allowed: it.allowed };
        if (it.items && it.items.length > 0) out.items = [...it.items];
        return out;
      });
    },

    // Deep structural equality: does qualityStructure exactly match the
    // profile's default items (same order, same names, same allowed flags,
    // same group nesting)? Used by buildSyncBody to skip persisting a
    // qualityStructure that's identical to defaults — prevents phantom
    // overrides when the user just opened the editor without making changes.
    // Considers ordering significant (reorder is a real override).
    qualityStructureMatchesDefaults() {
      const defaults = this.profileDetail?.detail?.profile?.items || [];
      const current = this.qualityStructure;
      if (current.length !== defaults.length) return false;
      for (let i = 0; i < defaults.length; i++) {
        const a = current[i];
        const b = defaults[i];
        if (a.name !== b.name) return false;
        if (!!a.allowed !== !!b.allowed) return false;
        const aItems = a.items || [];
        const bItems = b.items || [];
        if (aItems.length !== bItems.length) return false;
        for (let j = 0; j < aItems.length; j++) {
          if (aItems[j] !== bItems[j]) return false;
        }
      }
      return true;
    },

    // Debug logging helper — fire-and-forget POST to backend
    debugLog(category, message) {
      if (!this.config?.debugLogging) return;
      fetch('/api/debug/log', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ category, message })
      }).catch(() => {});
    },

    timeAgo(isoString) {
      if (!isoString) return 'never';
      void this._nowTick; // reactive dependency — triggers re-render every 30s
      const diff = Date.now() - new Date(isoString).getTime();
      const mins = Math.floor(diff / 60000);
      if (mins < 1) return 'just now';
      if (mins < 60) return mins + 'm ago';
      const hours = Math.floor(mins / 60);
      if (hours < 24) return hours + 'h ago';
      const days = Math.floor(hours / 24);
      return days + 'd ago';
    },

    nextPullTime() {
      void this._nowTick;
      const interval = this.config.pullInterval;
      if (!interval || interval === '0') return '';
      const nextPull = this.trashStatus?.nextPull;
      if (!nextPull) return '';
      const nextPullMs = new Date(nextPull).getTime();
      const serverNowMs = new Date(this.trashStatus?.serverNow || '').getTime();
      const fetchedAt = this._trashStatusFetchedAt || Date.now();
      const elapsed = Math.max(0, Date.now() - fetchedAt);
      const nowMs = Number.isFinite(serverNowMs) ? serverNowMs + elapsed : Date.now();
      const diff = nextPullMs - nowMs;
      if (diff <= 0) return 'soon';
      const mins = Math.ceil(diff / 60000);
      if (mins < 60) return mins + 'm';
      const hours = Math.floor(mins / 60);
      const remMins = mins % 60;
      return remMins > 0 ? hours + 'h ' + remMins + 'm' : hours + 'h';
    },

    nextPullClockLabel() {
      const serverClock = this.formatScheduleClockValue(this.trashStatus?.nextPullClock);
      if (!serverClock) return '';
      const serverLabel = this.config.serverTimeZone || '';
      const serverText = serverLabel ? serverClock + ' ' + serverLabel : serverClock;
      if (!this.scheduleTimeZoneMismatch()) return serverText;
      const localClock = this.formatLocalClock(this.trashStatus?.nextPull);
      return localClock ? serverText + ' / ' + localClock + ' local' : serverText;
    },

    // --- Profile editor Updates section helpers ---
    // pdCurrentRule resolves the rule the editor is currently editing.
    // Returns null when the editor is in Create-New flow (no rule yet)
    // or when autoSyncRules hasn't loaded. Same lookup pattern as
    // saveRuleOnly — by instance + locked Arr-profile-id.
    pdCurrentRule() {
      if (!this.profileDetail || !this.profileDetail.instance) return null;
      const inst = this.profileDetail.instance;
      const arrId = this.profileDetail._editLockedArrProfileId || 0;
      if (!arrId) return null;
      return (this.autoSyncRules || []).find(r => r.instanceId === inst.id && r.arrProfileId === arrId) || null;
    },
    pdPendingChanges() {
      const r = this.pdCurrentRule();
      return (r && r.pendingChanges) || [];
    },
    // TRaSH-source pending changes (upstream Profile Sync detection).
    // --- Pending-changes resolution for the row-level Quick action modal ---
    //
    // The first attempt used a frontend-only diff (rule.SelectedCFs vs
    // SyncHistoryEntry.SelectedCFs) but those fields carry different shapes:
    // rule.SelectedCFs = user opt-ins only, history.SelectedCFs = resolved
    // final set. Diffing them produced false positives (entire resolved set
    // showing as "added"). Score override mapping by trash_id also missed
    // edits stored under extra-CFs.
    //
    // Switched to /api/sync/dry-run which the backend already computes
    // correctly against Arr's current state. SyncPlan output is converted
    // into the same General/Quality/CFs buckets as drift + updates so
    // the modal renders uniformly.
    async fetchPendingSyncPlan(rule, inst) {
      if (!rule || !inst) {
        this.statusReviewPendingPlan = null;
        return;
      }
      this.statusReviewPendingLoading = true;
      this.statusReviewPendingError = '';
      this.statusReviewPendingPlan = null;
      try {
        const body = this.buildRuleSyncBody(rule, inst);
        const r = await fetch('/api/sync/dry-run', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body),
        });
        if (!r.ok) {
          const data = await r.json().catch(() => ({}));
          this.statusReviewPendingError = data.error || 'Could not compute pending changes';
          return;
        }
        this.statusReviewPendingPlan = await r.json();
      } catch (e) {
        this.statusReviewPendingError = e.message || 'Could not compute pending changes';
      } finally {
        this.statusReviewPendingLoading = false;
      }
    },
    // Build the dry-run body for a rule already persisted in clonarr.json.
    // Mirrors buildSyncBody() but reads from the rule object directly so
    // the modal doesn't need to mount the editor. Always update-mode.
    buildRuleSyncBody(rule, inst) {
      const body = {
        instanceId: rule.instanceId,
        profileTrashId: rule.trashProfileId || '',
        arrProfileId: rule.arrProfileId || 0,
        selectedCFs: Array.isArray(rule.selectedCFs) ? rule.selectedCFs.slice() : [],
        excludedCFs: Array.isArray(rule.excludedCFs) ? rule.excludedCFs.slice() : [],
      };
      if (rule.importedProfileId) body.importedProfileId = rule.importedProfileId;
      if (rule.scoreOverrides && Object.keys(rule.scoreOverrides).length > 0) body.scoreOverrides = { ...rule.scoreOverrides };
      if (Array.isArray(rule.qualityStructure) && rule.qualityStructure.length > 0) body.qualityStructure = rule.qualityStructure;
      if (rule.overrides) body.overrides = { ...rule.overrides };
      if (rule.behavior) body.behavior = rule.behavior;
      if (Array.isArray(rule.keepArrCFIDs) && rule.keepArrCFIDs.length > 0) body.keepArrCFIDs = rule.keepArrCFIDs;
      if (rule.description) body.description = rule.description;
      return body;
    },
    // Convert SyncPlan dry-run output into the same { general, quality, cfs }
    // shape the modal renders. SyncPlan structure (from internal/core/sync.go):
    //   CFActions       []CFAction      — { trashId, name, action: create|update|unchanged, arrId }
    //   ScoreActions    []ScoreAction   — { cfName, arrCfId, oldScore, newScore, action }
    //   QualityPreview  []string        — human-readable "Bluray-2160p: Enabled → Disabled"
    //   SettingsPreview []string        — human-readable "Upgrade Until: WEB 1080p → WEB 2160p"
    syncPlanByCategory(plan) {
      const out = { general: [], quality: [], cfs: [] };
      if (!plan) return out;
      for (const a of (plan.cfActions || [])) {
        if (!a.action || a.action === 'unchanged') continue;
        const verb = a.action === 'create' ? 'Add' : a.action === 'update' ? 'Update' : a.action;
        out.cfs.push({ affectedName: `${verb} ${a.name}`, changeType: 'cf-' + a.action });
      }
      for (const a of (plan.scoreActions || [])) {
        if (!a.action || a.action === 'unchanged') continue;
        const from = a.oldScore != null ? a.oldScore : 0;
        const to = a.newScore != null ? a.newScore : 0;
        out.cfs.push({ affectedName: `${a.cfName}: ${from} → ${to}`, changeType: 'score-changed' });
      }
      for (const s of (plan.settingsPreview || [])) {
        out.general.push({ affectedName: s, changeType: 'profile-modified' });
      }
      for (const q of (plan.qualityPreview || [])) {
        out.quality.push({ affectedName: q, changeType: 'qs-modified' });
      }
      return out;
    },

    pdPendingChangesTrash() {
      return this.pdPendingChanges().filter(c => c.source !== 'drift');
    },
    // Drift-source pending changes (Arr-side edits detected by DriftRunner).
    pdPendingChangesDrift() {
      return this.pdPendingChanges().filter(c => c.source === 'drift');
    },
    // Group pending changes by editor's existing section vocabulary so
    // the Updates / Out of sync panels mirror General / Quality items /
    // Custom Formats grouping the user already knows from other tabs.
    _pdCategorizePending(changes) {
      const out = { general: [], quality: [], cfs: [] };
      for (const c of changes) {
        const t = c.changeType || '';
        // Drift entries (source="drift") route by affectedId prefix:
        // backend stores "setting:<field>" for scalar settings and
        // "quality:<qname>" for quality-item allowed flips. CF score
        // drift uses the CF name as affectedId with no prefix.
        if (c.source === 'drift') {
          const id = c.affectedId || '';
          if (id.startsWith('setting:')) {
            out.general.push(c);
          } else if (id.startsWith('quality:') || t === 'qs-modified') {
            out.quality.push(c);
          } else {
            out.cfs.push(c);
          }
          continue;
        }
        // General — profile-level settings (name, score thresholds,
        // upgrade allowed, language). Score thresholds belong to General
        // tab in TRaSH/Arr's profile editor, not Quality items.
        if (t === 'profile-name' ||
            t === 'profile-upgrade-allowed' ||
            t === 'profile-language' ||
            t === 'profile-min-format-score' ||
            t === 'profile-cutoff-format-score' ||
            t === 'profile-min-upgrade-format-score') {
          out.general.push(c);
          continue;
        }
        // Quality items — cutoff QUALITY (not score) + per-quality allowed flips.
        // profile-modified is the legacy generic emit (pre-granular) —
        // map there since its phrasing was "quality settings updated".
        if (t === 'profile-modified' ||
            t === 'profile-quality-cutoff' ||
            t === 'profile-quality-allowed' ||
            t === 'profile-quality-added' ||
            t === 'profile-quality-removed') {
          out.quality.push(c);
          continue;
        }
        // Custom Formats — cf-modified, cf-group-*, profile-formatitem-*
        out.cfs.push(c);
      }
      return out;
    },
    pdPendingByCategoryTrash() {
      return this._pdCategorizePending(this.pdPendingChangesTrash());
    },
    pdPendingByCategoryDrift() {
      return this._pdCategorizePending(this.pdPendingChangesDrift());
    },
    // Resolve CF display name for CF-section entries — pulls from cache
    // when backend's AffectedName is empty (legacy entries written before
    // the AffectedName fix).
    pdResolveUpdateName(c) {
      if (c.affectedName) return c.affectedName;
      const appType = this.profileDetail?.instance?.type || '';
      const cfs = (this.cfBrowseData && this.cfBrowseData[appType] && this.cfBrowseData[appType].cfs) || [];
      const hit = cfs.find(x => x.trash_id === c.affectedId);
      return hit ? hit.name : (c.affectedId || 'unknown');
    },
    // Mode-aware + rule-state-aware action guidance for the Updates
    // panel footer. Tells the user WHEN/HOW these changes will be
    // applied without putting a per-rule "apply" button in the editor
    // (Pull is a global TRaSH-repo operation, not per-rule).
    // Whether the editor's Update now button should show spinner / be
    // disabled. True while EITHER the rule-level update flow is running
    // (updateThisProfile sets updatingRuleId), OR while a per-instance
    // Update all is running for this rule's instance.
    pdUpdateBusy() {
      const r = this.pdCurrentRule();
      if (!r) return false;
      if (this.updatingRuleId === r.id) return true;
      if (this.updatingInstance === r.instanceId) return true;
      return false;
    },
    // Trigger the same pull-then-sync-one-rule flow as the row-level
    // Update profile button. Synthesises the sh-shape quickSync expects
    // by combining profileDetail (TRaSH profile + Arr profile lookup)
    // with the live rule's persisted selectedCFs / excludedCFs so the
    // sync plan can be built without crashing on undefined fields.
    async pdUpdateNow() {
      if (!this.profileDetail || !this.profileDetail.instance) return;
      const inst = this.profileDetail.instance;
      const arrId = this.profileDetail._editLockedArrProfileId || 0;
      if (!arrId) {
        this.showToast('No saved rule yet — Save & Sync first to create the rule, then Update.', 'warning', 6000);
        return;
      }
      const rule = this.pdCurrentRule();
      const profile = this.profileDetail.profile || {};
      const sh = {
        arrProfileId: arrId,
        arrProfileName: this.profileDetail._arrProfileName || profile.name || '',
        profileName: profile.name || '',
        profileTrashId: profile.trashId || (rule && rule.trashProfileId) || '',
        importedProfileId: (rule && rule.importedProfileId) || '',
        selectedCFs: (rule && Array.isArray(rule.selectedCFs))
          ? Object.fromEntries(rule.selectedCFs.map(id => [id, true]))
          : {},
        excludedCFs: (rule && Array.isArray(rule.excludedCFs)) ? rule.excludedCFs.slice() : [],
        keepArrCFIDs: (rule && rule.keepArrCFIDs) || null,
      };
      await this.updateThisProfile(inst, sh);
    },
    pdUpdateActionLabel() {
      const rule = this.pdCurrentRule();
      if (rule && rule.enabled === false) {
        return 'Auto-sync is OFF on this rule. Update now applies these changes regardless.';
      }
      const mode = (this.config?.profileSync?.mode || '').toLowerCase();
      const remaining = this.nextPullTime();
      const clock = this.nextPullClockLabel();
      const when = remaining ? (remaining === 'soon' ? 'soon' : 'in ' + remaining) + (clock ? ' (' + clock + ')' : '') : '';
      if (mode === 'auto') {
        return when ? 'Otherwise scheduled pull will apply ' + when + '.' : '';
      }
      // notify / delayed / manual — no automatic apply currently. The
      // delayed-mode auto-apply scheduler hasn't shipped yet (Phase D);
      // until it does, "delayed" behaves identically to "notify": detection
      // runs on schedule, apply is manual via this Update button.
      return '';
    },

    nextPullLabel() {
      const remaining = this.nextPullTime();
      if (!remaining) return '';
      const clock = this.nextPullClockLabel();
      const suffix = clock ? ' (' + clock + ')' : '';
      // Mode-aware label: in Notify/Delayed mode the scheduler runs a
      // detection-only check (no pull), so the wording must reflect what
      // actually happens on the next tick — not "pull" which implies
      // fetching new TRaSH data.
      const mode = (this.config?.profileSync?.mode || '').toLowerCase();
      const action = (mode === 'notify' || mode === 'delayed') ? 'check' : 'pull';
      return remaining === 'soon' ? `next ${action} soon` + suffix : `next ${action} in ` + remaining + suffix;
    },

    // Sidebar-footer helpers. Together with the mode-aware nextPullLabel
    // they make the footer reflect what the scheduler actually does in the
    // user's current Mode — "Last checked" in Notify/Delayed, "TRaSH
    // pulled" in Auto — instead of always claiming "pulled" even when no
    // pull happened.
    // Sidebar footer status — is upstream ahead of local? True when
    // ProfileSync detection has noticed new upstream commits we haven't
    // pulled yet. Length-normalised compare (matches backend gate)
    // since the persisted heads can be 7-char short or 40-char full
    // depending on which version of clonarr wrote them.
    // Aggregate pending updates across all rules for the sidebar popover.
    // Same data the per-rule tooltips show, just rolled up so the user
    // can see at-a-glance what's waiting upstream without opening each
    // rule. Grouped by app (Radarr / Sonarr) then by ChangeType class
    // (Custom Formats / Profile settings / Quality items). HTML output
    // because the popover renders via x-html for inline structure.
    pendingUpdatesSummary() {
      const rules = this.autoSyncRules || [];
      const insts = this.instances || [];
      const instByID = new Map();
      for (const i of insts) instByID.set(i.id, i);
      // Collect unique changes across rules. Key by ChangeType + AffectedID
      // so the same CF affecting 8 rules counts once.
      const seen = new Map(); // appType → Map(changeType → Map(key → name))
      for (const r of rules) {
        const pc = r.pendingChanges || [];
        if (!pc.length) continue;
        const inst = instByID.get(r.instanceId);
        if (!inst) continue;
        const app = inst.type || '';
        if (!seen.has(app)) seen.set(app, new Map());
        const byType = seen.get(app);
        const cfs = (this.cfBrowseData && this.cfBrowseData[app] && this.cfBrowseData[app].cfs) || [];
        const nameByID = new Map();
        for (const cf of cfs) nameByID.set(cf.trash_id, cf.name);
        for (const c of pc) {
          // Drift entries live in rule.pendingChanges alongside upstream
          // entries but belong to a different UI surface (per-rule
          // out-of-sync badge + drift section in the editor). The
          // sidebar popover is the "TRaSH-Guides pulled new commits"
          // channel, so drop drift here to avoid mixing the two.
          if (c.source === 'drift') continue;
          const t = c.changeType || 'unknown';
          if (!byType.has(t)) byType.set(t, new Map());
          const key = c.affectedId + '|' + t;
          const name = c.affectedName || nameByID.get(c.affectedId) || c.affectedId || 'unknown';
          byType.get(t).set(key, name);
        }
      }
      if (seen.size === 0) {
        return '<em>No specific affected items detected yet — click Check to refresh.</em>';
      }
      // Map ChangeType into section vocabulary the editor already uses.
      const sectionFor = (t) => {
        if (t === 'profile-name' || t === 'profile-upgrade-allowed' || t === 'profile-language' ||
            t === 'profile-min-format-score' || t === 'profile-cutoff-format-score' || t === 'profile-min-upgrade-format-score') return 'General';
        if (t === 'profile-modified' || t === 'profile-quality-cutoff' || t === 'profile-quality-allowed' ||
            t === 'profile-quality-added' || t === 'profile-quality-removed') return 'Quality items';
        return 'Custom Formats';
      };
      const esc = (s) => String(s).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));
      const parts = [];
      for (const [app, byType] of seen) {
        const appLabel = app === 'radarr' ? 'Radarr' : (app === 'sonarr' ? 'Sonarr' : app);
        // Re-bucket entries by editor-section vocabulary
        const sections = new Map(); // section → Set(name)
        for (const [t, names] of byType) {
          const s = sectionFor(t);
          if (!sections.has(s)) sections.set(s, new Set());
          for (const name of names.values()) sections.get(s).add(name);
        }
        parts.push('<div style="margin-top:6px;font-weight:600">' + esc(appLabel) + '</div>');
        for (const sName of ['General', 'Quality items', 'Custom Formats']) {
          const items = sections.get(sName);
          if (!items || items.size === 0) continue;
          parts.push('<div style="margin-top:2px"><span style="color:var(--text-muted)">' + esc(sName) + ':</span></div>');
          const list = Array.from(items).slice(0, 8);
          for (const name of list) parts.push('<div style="padding-left:8px">• ' + esc(name) + '</div>');
          if (items.size > list.length) parts.push('<div style="padding-left:8px;color:var(--text-muted)">...and ' + (items.size - list.length) + ' more</div>');
        }
      }
      return parts.join('');
    },

    trashUpstreamAhead() {
      const u = this.config?.profileSync?.upstreamHead || '';
      const l = this.config?.profileSync?.localHead || '';
      if (!u || !l) return false;
      const len = Math.min(u.length, l.length);
      return u.slice(0, len) !== l.slice(0, len);
    },
    commitURL(hash) {
      if (!hash) return '';
      const url = (this.config?.trashRepo?.url || '').replace(/\.git$/, '');
      if (!url) return '';
      return url + '/commit/' + hash;
    },
    profileSyncIsNotifyOrDelayed() {
      const mode = (this.config?.profileSync?.mode || '').toLowerCase();
      return mode === 'notify' || mode === 'delayed';
    },
    // Wait for a pull-only operation to finish. /api/trash/pull returns
    // 202 immediately + spawns a background goroutine. Two-phase poll:
    //   Phase 1 — wait until trashStatus.pulling becomes TRUE (so we
    //             don't false-finish before the goroutine even acquired
    //             the pull mutex)
    //   Phase 2 — wait until it becomes FALSE again (pull completed)
    // Without phase 1, downstream orchestration could chain sync against
    // pre-pull TRaSH data. 5s phase-1 budget; 120s phase-2.
    async _waitForPullDone(phase1Ms = 5000, phase2Ms = 120000) {
      const phase1Start = Date.now();
      let started = false;
      while (Date.now() - phase1Start < phase1Ms) {
        await this.loadTrashStatus();
        if (this.trashStatus?.pulling) { started = true; break; }
        await new Promise(r => setTimeout(r, 200));
      }
      if (!started) {
        // Goroutine never flipped pulling=true within budget. Could be
        // empty-clone fast-path, error, or pull already done by polling
        // tick. Treat as done — caller's downstream loadTrashStatus
        // covers staleness.
        return true;
      }
      const phase2Start = Date.now();
      while (Date.now() - phase2Start < phase2Ms) {
        await new Promise(r => setTimeout(r, 1000));
        await this.loadTrashStatus();
        if (!this.trashStatus?.pulling) return true;
      }
      return false;
    },

    // Update all enabled rules on this instance: pull TRaSH data first
    // (since the user wants the latest upstream), then run the existing
    // per-instance sync-all loop. Other instances are not touched —
    // user must run Update all on each instance they care about.
    async updateAllForInstance(inst) {
      if (this.updatingInstance) return;
      this.updatingInstance = inst.id;
      try {
        const prevCommit = this.trashStatus?.commitHash || '';
        await fetch('/api/trash/pull', { method: 'POST' });
        await this._waitForPullDone();
        if (this.trashStatus?.pullError) {
          this.showToast(`Update all (${inst.name}): pull failed — ${this.trashStatus.pullError}`, 'error', 8000);
          return;
        }
        // Reload data sources that the sync loop reads from
        await this.loadCFBrowse(inst.type);
        await this.loadTrashProfiles(inst.type);
        // Now run the existing per-instance sync-all loop
        await this.syncAllForInstance(inst);
        // Refresh CF Sync Rules cache so its pills (Updates / Arr
        // drift) reflect the post-sync state. Without this the user
        // sees stale Update/drift indicators on Custom Formats →
        // Sync Rules even though backend cleared PendingChanges +
        // CFDriftFingerprints during the sync.
        if (typeof this.loadCFSyncRules === 'function' && this.cfSyncRulesLoaded?.[inst.type]) {
          await this.loadCFSyncRules(inst.type);
        }
        if (this.trashStatus?.commitHash && this.trashStatus.commitHash !== prevCommit) {
          this.showToast(`Update all complete (${inst.name}) — TRaSH advanced to ${this.trashStatus.commitHash}`, 'success', 4000);
        }
      } finally {
        this.updatingInstance = '';
      }
    },

    // Update just one rule: pull TRaSH first (so the rule gets the
    // latest upstream content), then sync ONLY this rule. Other rules
    // on the same instance stay at their previous lastSyncCommit + their
    // Arr state is untouched, even though local TRaSH has advanced.
    async updateThisProfile(inst, sh) {
      const rule = this.autoSyncRules.find(r => r.instanceId === inst.id && r.arrProfileId === sh.arrProfileId);
      const ruleId = rule?.id || '';
      if (!ruleId || this.updatingRuleId) return;
      this.updatingRuleId = ruleId;
      try {
        await fetch('/api/trash/pull', { method: 'POST' });
        await this._waitForPullDone();
        if (this.trashStatus?.pullError) {
          this.showToast(`Update profile: pull failed — ${this.trashStatus.pullError}`, 'error', 8000);
          return;
        }
        await this.loadCFBrowse(inst.type);
        await this.loadTrashProfiles(inst.type);
        // Reuse the existing quickSync path for the actual Arr write.
        await this.quickSync(inst, sh);
        await this.loadAutoSyncRules();
        // Same CF Sync Rules refresh as updateAllForInstance — without
        // this, a user updating one profile sees its pills clear on the
        // Profile Sync surface but the CF surface stays stuck.
        if (typeof this.loadCFSyncRules === 'function' && this.cfSyncRulesLoaded?.[inst.type]) {
          await this.loadCFSyncRules(inst.type);
        }
      } finally {
        this.updatingRuleId = '';
      }
    },

    async checkTrashUpdates() {
      if (this.checkingUpdates) return;
      this.checkingUpdates = true;
      try {
        // Two parallel checks: TRaSH upstream + Arr-side drift. Both endpoints
        // are detection-only (no Arr writes), so running them concurrently is
        // safe. Drift bypasses the Sources.ArrDrift gate so a manual press
        // always runs even when scheduled drift is off — the user's intent
        // is "I want to know right now".
        const [tr, dr] = await Promise.allSettled([
          fetch('/api/profile-sync/check', { method: 'POST' }),
          fetch('/api/drift/check', { method: 'POST' }),
        ]);
        let driftFailed = false;
        let cfDriftCount = 0;
        let cfDriftNames = [];
        if (dr.status === 'rejected' || (dr.value && !dr.value.ok)) {
          driftFailed = true;
        } else if (dr.value && dr.value.ok) {
          // Drift response carries both profile drift (`results`) AND
          // CF drift (`cfDrift`) since the Phase 1 cf-drift work. The
          // toast surfaces the CF channel as a separate line so the
          // user sees all three signals (TRaSH update / profile drift /
          // CF drift) without scanning multiple views.
          try {
            const drBody = await dr.value.clone().json();
            const list = Array.isArray(drBody?.cfDrift) ? drBody.cfDrift : [];
            cfDriftCount = list.length;
            cfDriftNames = list.map(e => e.name || e.trashId).filter(Boolean);
          } catch (_) { /* leave cfDriftCount=0 on parse failure */ }
        }
        // Refresh the Sync Rules → Custom Formats sub-tab cache if it
        // was previously loaded so the row pills update right away
        // when the user is already viewing the sub-tab.
        if (this.cfSyncRulesLoaded?.[this.activeAppType]) {
          this.loadCFSyncRules(this.activeAppType);
        }
        const r = tr.status === 'fulfilled' ? tr.value : null;
        if (!r || !r.ok) {
          let msg = 'TRaSH check failed';
          if (r) {
            try { const data = await r.json(); if (data && data.error) msg = data.error; } catch {}
          }
          this.showToast(msg, 'error', 4000);
          return;
        }
        // Refresh state so any newly-detected pendingChanges show up
        await this.loadTrashStatus();
        try {
          const ps = await fetch('/api/profile-sync');
          if (ps.ok) {
            const data = await ps.json();
            if (this.config.profileSync) {
              this.config.profileSync.lastRun = data.lastRun || '';
              this.config.profileSync.upstreamHead = data.upstreamHead || '';
              this.config.profileSync.localHead = data.localHead || '';
            }
          }
        } catch {}
        await this.loadAutoSyncRules();
        // Compose informative toast based on post-check state.
        const upstream = this.config?.profileSync?.upstreamHead || '';
        const local = this.config?.profileSync?.localHead || '';
        // Length-normalised compare (matches backend gate)
        let upstreamAhead = false;
        if (upstream && local) {
          const len = Math.min(upstream.length, local.length);
          upstreamAhead = upstream.slice(0, len) !== local.slice(0, len);
        }
        // Resolve rule → human-readable label (Arr profile name preferred,
        // falls back to the rule's tracked profile name).
        const ruleLabel = r => {
          const sh = (this.syncHistory?.[r.instanceId] || []).find(h => h.arrProfileId === r.arrProfileId);
          return sh?.arrProfileName || sh?.profileName || r.arrProfileName || `Profile #${r.arrProfileId}`;
        };
        const namesShort = (rules, max = 5) => {
          const names = rules.map(ruleLabel);
          const shown = names.slice(0, max).map(n => `• ${n}`);
          if (names.length > max) shown.push(`• +${names.length - max} more`);
          return shown.join('\n');
        };
        // Toast counts mirror the row-level status pill so they stay in
        // sync with what the user actually sees. Going directly through
        // v3RuleStatus avoids miscounts from orphaned rules / stale state
        // that don't render as a row in the Sync Rules table.
        const rulesByStatus = status => (this.autoSyncRules || []).filter(r =>
          !r.orphanedAt && this.v3RuleStatus(r) === status
        );
        const rulesWithDrift = rulesByStatus('out-of-sync');
        const driftRuleCount = rulesWithDrift.length;
        const driftLine = driftFailed
          ? 'Arr drift check failed'
          : (driftRuleCount > 0
              ? `${driftRuleCount} profile${driftRuleCount === 1 ? '' : 's'} with Arr drift:\n${namesShort(rulesWithDrift)}`
              : '');
        // CF drift line: lists up to N drifted CF names. Skipped when
        // empty so a clean Check is a one-line "in sync" message
        // instead of three reassurance lines.
        const cfDriftLine = cfDriftCount > 0
          ? `${cfDriftCount} custom format${cfDriftCount === 1 ? '' : 's'} with Arr drift:\n${cfDriftNames.slice(0, 5).map(n => `• ${n}`).join('\n')}${cfDriftNames.length > 5 ? `\n• +${cfDriftNames.length - 5} more` : ''}`
          : '';
        if (!upstreamAhead) {
          const channels = [];
          if (driftRuleCount > 0) channels.push(driftLine);
          if (cfDriftCount > 0) channels.push(cfDriftLine);
          if (driftFailed) channels.push(driftLine);
          if (channels.length > 0) {
            this.showToast(`TRaSH up to date\n\n${channels.join('\n\n')}`, driftFailed ? 'warning' : 'info', 7000);
          } else {
            this.showToast('TRaSH-Guides is up to date — no upstream changes', 'success', 3000);
          }
          return;
        }
        // Upstream ahead — same v3RuleStatus filter as drift, so toast
        // tally always matches what the row-level pills actually show.
        // Direct pendingChanges filter would include orphaned rules
        // with stale state that aren't rendered in the table.
        const affectedRules = rulesByStatus('updates');
        if (affectedRules.length === 0) {
          const extras = [];
          if (driftLine) extras.push(driftLine);
          if (cfDriftLine) extras.push(cfDriftLine);
          const tail = extras.length > 0 ? `\n\n${extras.join('\n\n')}` : '';
          this.showToast(`TRaSH has new commits but none affect your synced profiles${tail}`, 'info', 6000);
          return;
        }
        // Unique CFs across affected rules (TRaSH-source only)
        const cfNames = new Set();
        for (const r of affectedRules) {
          const appType = r.instanceType || '';
          const cfs = (this.cfBrowseData && this.cfBrowseData[appType] && this.cfBrowseData[appType].cfs) || [];
          const nameByID = new Map();
          for (const cf of cfs) nameByID.set(cf.trash_id, cf.name);
          for (const pc of (r.pendingChanges || [])) {
            if (pc.source === 'drift') continue;
            const name = pc.affectedName || nameByID.get(pc.affectedId) || pc.affectedId;
            cfNames.add(name);
          }
        }
        const ruleCount = affectedRules.length;
        const cfCount = cfNames.size;
        const cfLabel = cfCount === 1 ? '1 CF' : `${cfCount} CFs`;
        const updLabel = ruleCount === 1 ? '1 profile' : `${ruleCount} profiles`;
        // Profile names on their own line for readability — long lists wrap
        // poorly when crammed after the header.
        const updateLine = `Found ${cfLabel} affecting ${updLabel}:\n${namesShort(affectedRules)}`;
        const extras = [];
        if (driftLine) extras.push(driftLine);
        if (cfDriftLine) extras.push(cfDriftLine);
        const tail = extras.length > 0 ? `\n\n${extras.join('\n\n')}` : '';
        this.showToast(`${updateLine}\n\nUse Update all / Update profile to apply${tail}`, 'info', 7000);
      } catch (e) {
        this.showToast('Check failed (network error)', 'error', 4000);
      } finally {
        this.checkingUpdates = false;
      }
    },

    formatCommitDate(dateStr) {
      if (!dateStr) return '';
      try {
        const d = new Date(dateStr);
        return d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short', year: 'numeric' });
      } catch { return dateStr; }
    },

    truncateWord(str, max) {
      if (!str || str.length <= max) return str;
      const cut = str.lastIndexOf(' ', max);
      return (cut > 0 ? str.slice(0, cut) : str.slice(0, max)) + '...';
    },

    formatSyncTime(isoString) {
      if (!isoString) return 'never';
      try {
        const d = new Date(isoString);
        return d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short' }) + ' ' +
               d.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' });
      } catch { return isoString; }
    },

    formatChangelogDate(dateStr) {
      if (!dateStr) return '';
      try {
        const d = new Date(dateStr + 'T00:00:00');
        return d.toLocaleDateString('en-GB', { day: 'numeric', month: 'long', year: 'numeric' });
      } catch { return dateStr; }
    },

    // --- Category Toggles ---

    isCategoryEnabled(cat) {
      return cat.groups.some(g => g.cfs.some(cf => this.selectedOptionalCFs[cf.trashId]));
    },

    toggleCategory(cat) {
      const anyEnabled = this.isCategoryEnabled(cat);
      const updated = { ...this.selectedOptionalCFs };
      for (const group of cat.groups) {
        for (const cf of group.cfs) {
          updated[cf.trashId] = !anyEnabled;
        }
      }
      this.selectedOptionalCFs = updated;
    },

    // --- Group Toggles ---

    isGroupEnabled(category, groupName) {
      const cats = this.profileDetail?.detail?.cfCategories || [];
      const cat = cats.find(c => c.category === category);
      if (!cat) return false;
      const group = cat.groups.find(g => g.shortName === groupName);
      if (!group) return false;
      return group.cfs.some(cf => this.selectedOptionalCFs[cf.trashId]);
    },

    toggleGroup(category, groupName, cfs) {
      // Legacy "select all CFs in group" toggle. Not currently invoked from
      // any template path — Profile Detail uses pdToggleGroup below, which
      // also tracks the `__grp_` enabled flag. Kept for any external caller.
      const anySelected = cfs.some(cf => this.selectedOptionalCFs[cf.trashId]);
      const updated = { ...this.selectedOptionalCFs };
      if (anySelected) {
        for (const cf of cfs) updated[cf.trashId] = false;
      } else {
        for (const cf of cfs) {
          updated[cf.trashId] = !!(cf.required || cf.default);
        }
      }
      this.selectedOptionalCFs = updated;
    },

    // Profile Detail group on/off toggle.
    //   - Enable: flag-only. Per-CF state stays untouched — required +
    //     cf.default=true members come back via the render's
    //     undefined → cf.default fallback. Phase 2c lock-clicks
    //     (sel===false) and user-explicit opt-ins (sel===true) survive.
    //   - Disable: flag-only + clear per-CF=true opt-ins for this
    //     group's CFs + clear cfScoreOverrides for this group's CFs.
    //     The per-CF=true entries were opt-ins UNDER this group;
    //     disabling means user no longer wants them. Score overrides
    //     on the group's CFs would otherwise leak into the sync
    //     payload via the score-override safety net even though the
    //     group is off. Phase 2c lock entries (sel===false) are
    //     preserved across the cycle so they re-apply on enable.
    pdToggleGroup(group, enabled) {
      const updated = { ...this.selectedOptionalCFs };
      updated['__grp_' + group.name] = enabled;
      if (!enabled) {
        const overrides = { ...(this.cfScoreOverrides || {}) };
        let touchedOverrides = false;
        for (const cf of (group.cfs || [])) {
          if (updated[cf.trashId] === true) delete updated[cf.trashId];
          if (overrides[cf.trashId] !== undefined) { delete overrides[cf.trashId]; touchedOverrides = true; }
        }
        if (touchedOverrides) this.cfScoreOverrides = overrides;
      }
      this.selectedOptionalCFs = updated;
    },

  },
};
