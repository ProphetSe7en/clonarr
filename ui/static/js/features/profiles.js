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
      const extraIds = Object.keys(this.extraCFs);
      return extraIds.length > 0 ? [...ids, ...extraIds] : ids;
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

    async loadExtraCFList() {
      const t = this.profileDetail?.instance?.type;
      if (!t) return;
      try {
        const r = await fetch(`/api/trash/${t}/all-cfs`);
        if (!r.ok) return;
        const d = await r.json();
        // Build grouped + ungrouped lists. Backend marks user-created CFs
        // with cf.isCustom=true regardless of which category the user
        // assigned. Pull them all into a dedicated "Custom" group so the
        // picker matches Custom Formats tab behaviour (one Custom bucket,
        // not scattered across the user's chosen categories).
        const groups = []; // { name, category, cfs[] }
        const ungrouped = [];
        const customCFs = [];
        for (const c of (d.categories || [])) for (const g of c.groups) {
          if (g.groupTrashId) {
            // category passed through so getCategoryClass() can paint the
            // left-border color the same way the main Groups section does.
            groups.push({ name: g.name, category: c.category, cfs: g.cfs });
          } else {
            for (const cf of g.cfs) {
              if (cf.isCustom) customCFs.push(cf);
              else ungrouped.push(cf);
            }
          }
        }
        if (ungrouped.length > 0) groups.push({ name: 'Other', category: 'Other', cfs: ungrouped });
        if (customCFs.length > 0) groups.push({ name: 'Custom', category: 'Other', cfs: customCFs });
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

    showToast(message, type = 'info', duration = 8000) {
      const id = Date.now() + Math.random();
      this.toasts = [...this.toasts, { id, message, type, duration }];
      setTimeout(() => { this.toasts = this.toasts.filter(t => t.id !== id); }, duration);
    },

    async checkCleanupEvents() {
      try {
        const r = await fetch('/api/cleanup-events');
        if (!r.ok) return;
        const events = await r.json();
        if (events.length === 0) return;
        // Coalesce per instance: bulk deletions in Arr would otherwise spawn
        // one toast per affected profile, stacking 25+ deep with their own
        // scrollbar. One toast per instance with first-three names + count
        // is informative without being a wall of yellow.
        const byInstance = {};
        for (const ev of events) {
          (byInstance[ev.instanceName] = byInstance[ev.instanceName] || []).push(ev.profileName);
        }
        for (const [instanceName, names] of Object.entries(byInstance)) {
          if (names.length === 1) {
            this.showToast(`"${names[0]}" — deleted in ${instanceName}, sync rule removed`, 'warning', 6000);
          } else {
            const preview = names.slice(0, 3).map(n => `"${n}"`).join(', ');
            const extra = names.length > 3 ? ` and ${names.length - 3} more` : '';
            this.showToast(`${names.length} profiles deleted in ${instanceName}, sync rules removed:\n${preview}${extra}`, 'warning', 8000);
          }
        }
      } catch (e) { /* ignore */ }
    },

    async checkAutoSyncEvents() {
      try {
        const r = await fetch('/api/auto-sync/events');
        if (!r.ok) return;
        const events = await r.json();
        for (let i = 0; i < events.length; i++) {
          const ev = events[i];
          setTimeout(() => {
            if (ev.error) {
              this.showToast(`Auto-sync failed: ${ev.instanceName} — ${ev.profileName}: ${ev.error}`, 'error', 8000);
            } else {
              const profileLabel = ev.arrProfileName && ev.arrProfileName !== ev.profileName
                ? `${ev.profileName} → ${ev.arrProfileName}` : ev.profileName;
              let msg = `Auto-sync: ${ev.instanceName} — "${profileLabel}"`;
              if (ev.details?.length > 0) {
                msg += '\n' + ev.details.join('\n');
              }
              this.showToast(msg, 'info', 8000);
            }
          }, i * 3000);
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
              const summary = this.trashStatus.lastDiff.summary.replace(/\*\*/g, '').replace(/^\n/, '').replace(/\n/g, ', ').replace(/:,/g, ':');
              this.showToast('TRaSH updated: ' + summary, 'info', 10000);
            } else {
              this.showToast('TRaSH data up to date', 'info', 3000);
            }
            this.loadTrashProfiles('radarr');
            this.loadTrashProfiles('sonarr');
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

    // --- Profile Detail ---

    async openProfileDetail(inst, profile) {
      this.debugLog('UI', `Profile opened: "${profile.name}" on ${inst.name}`);
      this.syncPlan = null;
      this.syncResult = null;
      this.selectedOptionalCFs = {};

      this.showProfileInfo = false;
      this.profileDetail = { instance: inst, profile: profile, detail: null };
      // Pre-load languages and quality presets for this instance (for override dropdowns)
      this.getLanguagesForInstance(inst.id);
      if (!this.pbQualityPresets.length) {
        fetch(`/api/trash/${inst.type}/quality-presets`).then(r => r.ok ? r.json() : []).then(d => this.pbQualityPresets = d || []).catch(() => {});
      }
      try {
        const r = await fetch(`/api/trash/${inst.type}/profiles/${profile.trashId}`);
        if (!r.ok) { console.error('loadProfileDetail: HTTP', r.status); return; }
        const detail = await r.json();
        this.profileDetail = { ...this.profileDetail, detail: detail };
        this.initDetailSections(detail);
        this.initSelectedCFs(detail);
        // Reset profile-detail override state on every load so stale state from a prior
        // profile doesn't leak. pdInitOverrides then seeds pdOverrides from the new profile's
        // defaults; the rule-restore branch below re-enables overrides if persisted.
        this.pdResetDetailState();
        this.pdInitOverrides(detail.profile || null);
        // If a saved sync rule exists for this (instance, TRaSH profile), pre-fill
        // the editor with its persisted state. Without this, opening a profile
        // that already has a sync rule shows TRaSH defaults — and the user's
        // next Save & Sync silently wipes their previously-saved extras /
        // overrides because buildSyncBody reads from this.extraCFs (empty)
        // instead of the rule's saved scoreOverrides. Repro: add 19 extras
        // via Customize → Save & Sync (creates them in Arr + persists rule)
        // → reopen profile → Save & Sync again → rule.SelectedCFs / .ScoreOverrides
        // get overwritten with the smaller in-editor set → next Sync All
        // zeroes the dropped extras. This block makes the editor show the
        // rule's current state on open, so Save & Sync is idempotent unless
        // the user actually edited something.
        const matchingRules = (this.autoSyncRules || []).filter(rl =>
          rl.instanceId === inst.id &&
          rl.trashProfileId === profile.trashId &&
          !rl.orphanedAt
        );
        if (matchingRules.length === 1) {
          this.applyRuleStateToEditor(matchingRules[0], detail);
        }
        // Multiple rules for the same TRaSH profile (different Arr profiles)
        // are not auto-restored — ambiguous which to load. User reaches the
        // specific rule via Sync History → resyncProfile in that case.
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
      // selectedCFs → selectedOptionalCFs map. Picks up exactly what the
      // rule has tracked, so optional-group CFs (default-off members user
      // explicitly added via Additional CFs picker) come back into the
      // editor.
      const selOpt = { ...(this.selectedOptionalCFs || {}) };
      for (const tid of (rule.selectedCFs || [])) selOpt[tid] = true;
      // Reconstruct group on/off flags from rule membership. Without this,
      // editor renders default-off groups (e.g. Optional Movie Versions)
      // as toggled OFF even when the rule clearly wants them on (members
      // present in selectedCFs). User-visible symptom: editor reports "0
      // overrides", but Reset-to-Profile-Default still produces a long
      // diff because reset clears the per-CF entries and the next Save &
      // Sync drops every member of the (apparently-off) group.
      // Algorithm: if ANY CF from a default-off group is in selectedCFs,
      // mark the group toggle on; if NO CF from a default-on group is in
      // selectedCFs, mark the group toggle off. Otherwise leave alone
      // (matches default).
      const ruleSet = new Set(rule.selectedCFs || []);
      for (const g of (detail.trashGroups || [])) {
        const cfTids = (g.cfs || []).map(cf => cf.trashId);
        const anyInRule = cfTids.some(tid => ruleSet.has(tid));
        const grpKey = '__grp_' + g.name;
        if (!g.defaultEnabled && anyInRule) {
          selOpt[grpKey] = true;
        } else if (g.defaultEnabled && !anyInRule && cfTids.length > 0) {
          selOpt[grpKey] = false;
        }
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
      el.innerHTML = sanitizeHTML(text);
      el.style.display = 'block';
      const rect = event.target.getBoundingClientRect();
      // Position to the right of the icon, vertically centered
      const w = el.offsetWidth || 340;
      const h = el.offsetHeight;
      let x = rect.right + 12;
      // If not enough space on the right, try left
      if (x + w > window.innerWidth - 8) {
        x = rect.left - w - 12;
      }
      x = Math.max(8, x);
      let y = rect.top - 8;
      if (y + h > window.innerHeight - 8) {
        y = Math.max(8, window.innerHeight - h - 8);
      }
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
      // Include individually toggled optional CFs
      for (const [k, v] of Object.entries(this.selectedOptionalCFs)) {
        if (v && !k.startsWith('__grp_')) idSet.add(k);
      }
      // Include required CFs from active TRaSH groups
      const groups = this.profileDetail?.detail?.trashGroups || [];
      for (const group of groups) {
        const grpOn = this.selectedOptionalCFs['__grp_' + group.name] !== undefined
          ? this.selectedOptionalCFs['__grp_' + group.name]
          : group.defaultEnabled;
        if (!grpOn) continue;
        for (const cf of group.cfs) {
          if (cf.required) {
            idSet.add(cf.trashId);
          }
        }
      }
      // Include any CF that has a score override. Without this, a user-overridden
      // score is sent in the scoreOverrides map but the backend's BuildArrProfile
      // only processes trashIDs present in FormatItems ∪ selectedCFs — so the
      // override would be silently dropped. (restoreFromSyncHistory filters
      // cfScoreOverrides down to in-profile CFs, so this loop is normally a
      // no-op for restored rules; it's the safety net for live edits.)
      if (this.cfScoreOverrides) {
        for (const trashId of Object.keys(this.cfScoreOverrides)) {
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
        selectedCFs: this.getAllSelectedCFIds()
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
      // Per-CF score overrides + extra CFs scores.
      const allScoreOverrides = { ...this.cfScoreOverrides };
      for (const [tid, score] of Object.entries(this.extraCFs)) allScoreOverrides[tid] = score;
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
          if (inst && profile) await this.openProfileDetail(inst, profile);
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
          if (inst && profile) await this.openProfileDetail(inst, profile);
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
            let msg = `"${this.syncForm.profileName}" synced`;
            if (details.length > 0) {
              const shown = details.length > 5 ? [...details.slice(0, 4), `...and ${details.length - 4} more`] : details;
              msg += '\n' + shown.join('\n');
            } else {
              msg += ' — no changes';
            }
            this.showToast(msg, 'info', details.length > 0 ? 8000 : 4000);
          }
        }
        this.syncResult = result;
        this.syncResultDetailsOpen = false;
        this.syncPlan = null;
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
        if (existingRule && !hadErrors) {
          const updated = {
            ...existingRule,
            selectedCFs: this.getAllSelectedCFIds(),
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

    // --- Quick Sync ---

    async quickSync(inst, sh, silent = false, useHistoryOnly = false) {
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
      const body = {
        instanceId: inst.id,
        profileTrashId: sh.profileTrashId,
        importedProfileId,
        arrProfileId: sh.arrProfileId,
        selectedCFs,
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
        let msg = `${inst.name} — "${sh.profileName}" synced`;
        if (details.length > 0) {
          const shown = details.length > 5 ? [...details.slice(0, 4), `...and ${details.length - 4} more`] : details;
          msg += '\n' + shown.join('\n');
        } else {
          msg += ' — no changes';
        }
        if (!silent) this.showToast(msg, 'info', details.length > 0 ? 8000 : 4000);
        this.setRuleSyncError(inst.id, sh.arrProfileId, '');
        await this.loadSyncHistory(inst.id);
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

    async cloneProfile(inst, sh) {
      const name = await new Promise(resolve => {
        this.inputModal = {
          show: true,
          title: 'Clone Profile',
          message: `Create a copy of "${sh.arrProfileName}" with all overrides and settings.`,
          placeholder: 'New profile name',
          value: sh.arrProfileName + ' (Copy)',
          confirmLabel: 'Clone',
          onConfirm: (val) => resolve(val),
          onCancel: () => resolve(null)
        };
      });
      if (!name || !name.trim()) return;
      // Resolve importedProfileId from rule if missing in history
      let importedProfileId = sh.importedProfileId || '';
      if (!importedProfileId) {
        const rule = this.autoSyncRules.find(r => r.instanceId === inst.id && r.arrProfileId === sh.arrProfileId);
        if (rule?.importedProfileId) importedProfileId = rule.importedProfileId;
      }
      const body = {
        instanceId: inst.id,
        profileTrashId: sh.profileTrashId,
        importedProfileId,
        arrProfileId: 0, // create mode
        profileName: name.trim(),
        selectedCFs: Object.keys(sh.selectedCFs || {}).filter(k => sh.selectedCFs[k]),
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
          this.showToast(`Clone failed: ${result.error || result.errors[0]}`, 'error', 8000);
          return;
        }
        this.showToast(`Cloned "${sh.arrProfileName}" → "${name.trim()}"`, 'info', 5000);
        await this.loadSyncHistory(inst.id);
        await this.loadAutoSyncRules();
      } catch (e) {
        this.showToast(`Clone error: ${e.message}`, 'error', 8000);
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
        this.showToast(`Sync All (${inst.name}): no profiles with auto-sync enabled`, 'warning', 4000);
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
      const lines = results.map(r => {
        if (!r.ok) return `${r.name} — FAILED: ${r.error}`;
        if (r.details?.length > 0) return `${r.name} — ${r.details.slice(0, 2).join(', ')}`;
        return `${r.name} — no changes`;
      });
      const errors = results.filter(r => !r.ok).length;
      const toastType = errors === results.length ? 'error' : errors > 0 ? 'warning' : 'info';
      this.showToast(`Sync All (${inst.name}):\n${lines.join('\n')}`, toastType, 10000);
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
      await this.openProfileDetail(inst, profile);
      // Show which Arr profile this is synced to
      this.profileDetail._arrProfileName = sh.arrProfileName || null;
      // Lock the edit session to this Arr profile. resyncTargetArrProfileId
      // is consumed once per modal open in _loadSyncInstanceData, so on a
      // second Save & Sync (after Dry Run / Cancel) it would fall back to
      // 'create' mode. Persist the target on profileDetail (cleared when
      // user leaves the detail view) so every subsequent openSyncModal in
      // this edit session re-arms the lock. Bypass: openSyncModalAsNew.
      this.profileDetail._editLockedArrProfileId = sh.arrProfileId;
      // Restore optional CF selections from sync history
      if (sh.selectedCFs && Object.keys(sh.selectedCFs).length > 0) {
        const groups = this.profileDetail?.detail?.trashGroups || [];
        // Look up the rule's PriorAvailableGroups snapshot — backend stamps
        // this at every successful sync, and migrates pre-fix rules from
        // their LastSyncCommit on first AutoSyncAfterPull. We use it to
        // tell "user explicitly opted out of an existing group" apart
        // from "group is brand new since last sync (TRaSH restructure)".
        const ruleForRestore = this.autoSyncRules.find(r => r.instanceId === inst.id && r.arrProfileId === sh.arrProfileId);
        const priorAvailable = (ruleForRestore && ruleForRestore.priorAvailableGroups) || {};
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
          const groupWasSynced = group.cfs.some(cf => effectiveSelectedCFs[cf.trashId]);
          const groupExistedAtLastSync = group.trashId && (group.trashId in priorAvailable);
          for (const cf of group.cfs) {
            if (cf.required) continue;
            if (effectiveSelectedCFs[cf.trashId]) {
              // CF is currently in the rule's selection → restore as on.
              this.selectedOptionalCFs[cf.trashId] = true;
            } else if (groupExistedAtLastSync || !group.defaultEnabled) {
              // Either the group existed at last sync (user's "off" choice
              // is preserved) OR the group is opt-in (per-CF default
              // doesn't apply when group is off). Mark CF off explicitly.
              this.selectedOptionalCFs[cf.trashId] = false;
            } else if (cf.default) {
              // Brand-new default-on group + CF is default-on within the
              // group → default to on (matches TRaSH's recommendation).
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
      // Restore overrides from sync history. Values are written to pdOverrides;
      // pdOverridesEnabled is flipped to true at the end if ANY override was
      // found, so the global toggle reflects the saved state of the rule.
      let anyOverride = false;
      if (sh.overrides) {
        const ov = sh.overrides;
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

      // Split sh.scoreOverrides into (Extra CF) vs (base-profile override).
      // Rule: if trashID is NOT in the base profile, it's an Extra — belongs
      // in extraCFs, NOT cfScoreOverrides. Otherwise it's a base-profile
      // override, and only kept if score differs from TRaSH default (prevents
      // "false-positive" overrides with `default → default` rows that
      // reappear after every refresh).
      const extras = {};
      const baseOverrides = {};
      if (sh.scoreOverrides) {
        for (const [tid, v] of Object.entries(sh.scoreOverrides)) {
          if (!inProfile.has(tid)) {
            // Only add to extras if also selected (legacy sync-history may have
            // score entries for CFs that are no longer selected).
            if (sh.selectedCFs && sh.selectedCFs[tid]) {
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
      if (sh.qualityStructure && sh.qualityStructure.length > 0) {
        this.qualityStructure = sh.qualityStructure.map(it => {
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
      } else if (sh.qualityOverrides && Object.keys(sh.qualityOverrides).length > 0) {
        this.qualityOverrides = { ...sh.qualityOverrides };
        anyOverride = true;
      }
      // Apply the Extra CFs computed above.
      if (Object.keys(extras).length > 0) {
        this.extraCFs = extras;
        anyOverride = true;
        // Load all CFs for the browser
        const appType = this.profileDetail?.instance?.type;
        if (appType) this.loadExtraCFList();
      }
      // Restore behavior from sync history
      if (sh.behavior) {
        this.syncForm.behavior = { ...this.syncForm.behavior, ...sh.behavior };
      }
      // Auto-enable the Profile Detail overrides toggle if ANY override was
      // restored, so the UI reflects the saved state of the rule (no "All
      // values follow profile defaults" lie when there are real overrides).
      if (anyOverride) this.pdOverridesEnabled = true;
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
      return [...rules].sort((a, b) => {
        const av = col === 'trash' ? (a.profileName || '') : (a.arrProfileName || '');
        const bv = col === 'trash' ? (b.profileName || '') : (b.arrProfileName || '');
        return dir * av.localeCompare(bv);
      });
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
        const details = result.details?.length ? '\n' + result.details.slice(0, 5).join('\n') : '';
        this.showToast(`Rolled back "${entry.arrProfileName}" to ${prevDate}. Auto-sync disabled.${details}`, 'info', 8000);
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
    // to determine if it's a TRaSH-blessed customization (non-default
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
      const cfScores = Object.keys(this.cfScoreOverrides).length;
      const extraCFs = Object.keys(this.extraCFs).length;
      const optional = this.pdOptionalCount();
      // total = overrides only (profile-level settings the user changed).
      // optional = separate count of TRaSH-blessed optional CFs / groups
      //   activated outside the profile's defaults — semantically distinct
      //   from overrides (overrides change WHAT the rule does; optional
      //   activations change WHAT'S in the profile within the TRaSH spec).
      // Both are reset by pdDisableOverrides; the UI shows them as two
      // separate badges so the user understands the category of change.
      return {
        general, quality, cfScores, extraCFs, optional,
        total: general + quality + cfScores + extraCFs,
      };
    },

    // Count of TRaSH-blessed optional CFs and groups activated outside the
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

    // Compare filter visibility predicates. Called from x-show on CF rows in every diff section.
    compareRowVisible(status) {
      // status: 'match' | 'wrong' | 'missing' | 'extra' | 'optional'
      // 'optional' = TRaSH-blessed activation outside profile defaults
      // (default-OFF group toggled on, or default:false CF activated in
      // default-ON group). Visible under 'all', 'diff' (it IS a diff vs
      // profile-default), and the dedicated 'optional' chip.
      switch (this.compareFilter) {
        case 'all': return true;
        case 'diff': return status !== 'match';
        case 'wrong': return status === 'wrong';
        case 'missing': return status === 'missing';
        case 'extra': return status === 'extra';
        case 'optional': return status === 'optional';
        case 'match': return status === 'match';
        default: return true;
      }
    },
    // Determine status class for a format-item row (required CF or group CF)
    compareFormatItemStatus(fi) {
      if (!fi.exists) return 'missing';
      if (!fi.scoreMatch) return 'wrong';
      return 'match';
    },
    // Build a flat list of rows for the CF Groups table. Each entry is a single <tr>:
    // - { type: 'sub', group } — group header subrow
    // - { type: 'multi', group } — multi-scored warning subrow (exclusive groups only)
    // - { type: 'cf', cf, group } — a CF row
    // Used to keep the table structure HTML-valid (single <tbody>, one row per iteration) while
    // interleaving subheaders and CF rows.
    flatCompareGroupRows(cr) {
      const rows = [];
      for (const group of (cr?.groups || [])) {
        if (!(group.cfs || []).some(cf => this.compareRowVisible(this.compareGroupCFStatus(cf, group)))) continue;
        const isGolden = !!(group.exclusive && group.defaultEnabled);
        rows.push({ type: 'sub', group, isGolden, key: 'h-' + group.name });
        if (group.exclusive && group.cfs.filter(c => c.exists && c.currentScore !== 0).length > 1) {
          rows.push({ type: 'multi', group, key: 'm-' + group.name });
        }
        for (const cf of group.cfs) {
          rows.push({ type: 'cf', cf, group, isGolden, key: 'r-' + group.name + '-' + cf.trashId });
        }
      }
      return rows;
    },

    // Strip HTML tags for use as a plain-text tooltip (TRaSH descriptions are HTML fragments)
    stripHtml(html) {
      if (!html) return '';
      const tmp = document.createElement('div');
      tmp.innerHTML = html;
      return (tmp.textContent || tmp.innerText || '').trim();
    },

    // Status for a CF inside a group. Exclusive default-enabled groups without any in-use variant
    // report ALL variants as 'missing' so they show in diff/wrong/missing filters — the user must pick one.
    // Doesn't rely on group.present (some backends count scored-but-unused); uses cf.inUse directly.
    compareGroupCFStatus(cf, group) {
      if (group.exclusive && group.defaultEnabled) {
        const anyInUse = (group.cfs || []).some(c => c.exists && c.inUse);
        if (!anyInUse) return 'missing';
      }
      if (cf.exists && cf.inUse && !cf.scoreMatch) return 'wrong';
      if (!cf.exists && group.defaultEnabled && cf.required) return 'missing';
      if (cf.exists && cf.inUse && cf.scoreMatch) {
        // Active CF that matches profile-spec score. Classify as
        // 'optional' (= TRaSH-blessed customization) when its activation
        // diverges from the profile defaults: either the parent group is
        // default-OFF (any member counts) or the CF is `default:false`
        // inside a default-ON group. Otherwise it's a plain 'match'.
        if (!group.defaultEnabled) return 'optional';
        if (!cf.required && !cf.default) return 'optional';
        return 'match';
      }
      return 'match'; // not-in-use / non-required-missing — not a diff for filter purposes
    },

    // Does this exclusive group have any variant with a diff? Used in CF Groups
    // table to force ALL variants visible when one has a diff — Golden Rule HD/UHD
    // groups are "pick one of the two", and hiding the matching variant in
    // 'Only diffs' lets the user activate the wrong-score variant without
    // realising they need to deactivate the other to keep the exclusive
    // invariant. Showing both rows preserves the constraint visually.
    compareGroupHasDiff(group) {
      return (group?.cfs || []).some(cf => this.compareGroupCFStatus(cf, group) !== 'match');
    },

    // Returns compare summary counts for filter chips. Backend already augments Missing with
    // +1 per exclusive-required group without any inUse variant (see handlers.go:1874-1885), so
    // this is a thin pass-through with defensive null/undefined coercion.
    compareAdjustedCounts(cr) {
      const s = cr?.summary || {};
      const missing = s.missing || 0;
      const wrong = s.wrongScore || 0;
      const extra = s.extra || 0;
      const settings = s.settingsDiffs || 0;
      const quality = s.qualityDiffs || 0;
      const matching = s.matching || 0;
      // 'optional' is derived frontend-side from groups + cf flags since
      // the backend summary doesn't separate it from 'matching' yet.
      let optional = 0;
      for (const g of (cr?.groups || [])) {
        for (const cf of (g.cfs || [])) {
          if (this.compareGroupCFStatus(cf, g) === 'optional') optional++;
        }
      }
      const diffs = wrong + missing + extra + settings + quality + optional;
      return { missing, wrong, extra, settings, quality, matching, optional, diffs, all: matching + diffs };
    },

    // Per-group count of optional CFs activated outside profile defaults.
    // Mirror of pdGroupOptionalCount but reads from compare-result group
    // structure (cf.inUse / cf.required / cf.default + group.defaultEnabled)
    // instead of editor state. Used for the small blue dot on each group
    // sub-header in the Compare's Groups table.
    compareGroupOptionalCount(group) {
      let n = 0;
      for (const cf of (group?.cfs || [])) {
        if (this.compareGroupCFStatus(cf, group) === 'optional') n++;
      }
      return n;
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
      this.extraCFAllCFs = [];
      this.extraCFGroups = [];
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
      if (s.cfScores > 0) overridesParts.push(`Overridden Scores: ${s.cfScores}`);
      if (s.extraCFs > 0) overridesParts.push(`Additional CFs: ${s.extraCFs}`);
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
      await this.openProfileDetail(inst, trashProfile);
      // Lock subsequent Save & Sync to the Arr profile we're editing.
      this.profileDetail._arrProfileName = comparison.arrProfileName || this.resolveArrProfileName(inst.id, arrIdNum) || null;
      this.profileDetail._editLockedArrProfileId = arrIdNum;
      this.resyncTargetArrProfileId = arrIdNum;
      // Wait for the all-CFs lookup table to load before prefilling — without it,
      // comparison.extraCFs (which carry only name + arrCFID, no trashID) can't
      // be mapped to the editor's trashID-keyed extraCFs map. Without this step,
      // user customs like "!PL Tier 02" / "Dubs Only" / "2.0 Stereo" appear in
      // Compare's All/Only-diffs view but vanish from Edit & Sync's Additional CFs.
      await this.loadExtraCFList();
      // applyRuleStateToEditor already ran via openProfileDetail's auto-restore
      // path if exactly one rule matches (instance + trashProfile). Layer the
      // compare-derived overrides on top so Arr-state-vs-TRaSH-default deltas
      // surface in the editor too.
      this.prefillOverridesFromCompare(comparison);
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
      const unresolved = [];
      for (const ecf of (comparison.extraCFs || [])) {
        const tid = nameToTrashId[ecf.name];
        if (tid) {
          extras[tid] = ecf.score;
        } else {
          unresolved.push({ arrCFID: ecf.format, name: ecf.name, currentScore: ecf.score });
        }
      }
      this.extraCFs = extras;
      this._compareArrOnlyExtras = unresolved;

      if (anyOverride || (this._compareArrOnlyExtras && this._compareArrOnlyExtras.length > 0)) {
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
      const lastPull = this.trashStatus?.lastPull;
      if (!interval || interval === 'off' || !lastPull) return '';
      const match = interval.match(/^(\d+)(m|h)$/);
      if (!match) return '';
      const ms = parseInt(match[1]) * (match[2] === 'h' ? 3600000 : 60000);
      const next = new Date(lastPull).getTime() + ms;
      const diff = next - Date.now();
      if (diff <= 0) return 'soon';
      const mins = Math.floor(diff / 60000);
      if (mins < 60) return mins + 'm';
      const hours = Math.floor(mins / 60);
      const remMins = mins % 60;
      return remMins > 0 ? hours + 'h ' + remMins + 'm' : hours + 'h';
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

    // Profile Detail group on/off toggle. Sets the `__grp_<name>` flag for
    // group enabled/disabled state and updates per-CF selections according
    // to the TRaSH group data:
    //   - Enable + non-exclusive: pre-select required CFs and CFs marked
    //     `default: true` in the group definition. Non-default optional CFs
    //     remain visible (group is expanded) but unchecked — user can tick
    //     them individually.
    //   - Enable + exclusive: don't auto-pick anything; user picks one
    //     (Golden Rule HD x265 vs no-HDR-DV, HDR Formats variant, etc.).
    //   - Disable: clear all per-CF selections in the group.
    // Replaces the inline @change handler that used to live in the template
    // — moved here so the cf.default fix only has to live in one place.
    pdToggleGroup(group, enabled) {
      const updated = { ...this.selectedOptionalCFs };
      updated['__grp_' + group.name] = enabled;
      if (enabled) {
        if (!group.exclusive) {
          for (const cf of (group.cfs || [])) {
            if (cf.required) {
              updated[cf.trashId] = true;
            } else if (cf.default) {
              updated[cf.trashId] = true;
            }
            // else: leave as-is (unchecked by default), user can tick later
          }
        }
      } else {
        for (const cf of (group.cfs || [])) {
          updated[cf.trashId] = false;
        }
      }
      this.selectedOptionalCFs = updated;
    },

  },
};
