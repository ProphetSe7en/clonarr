export default {
  state: {},
  methods: {
    // --- Profile Builder ---

    async openProfileBuilder(appType, existing = null) {
      this.pb = {
        editId: existing?.id || null,
        name: existing?.name || '',
        appType: appType,
        scoreSet: existing?.scoreSet || (existing ? 'default' : (this._pbLoadDefaults().scoreSet || 'default')),
        upgradeAllowed: existing?.upgradeAllowed ?? true,
        cutoff: existing?.cutoff || '',
        cutoffScore: existing?.cutoffScore ?? 10000,
        minFormatScore: existing?.minFormatScore ?? 0,
        minUpgradeFormatScore: existing?.minUpgradeFormatScore ?? 1,
        language: existing?.language || 'Original',
        qualityPreset: existing?.qualityPresetId || (existing ? '' : (this._pbLoadDefaults().qualityPresetId || '')),
        qualityPresetId: existing?.qualityPresetId || (existing ? '' : (this._pbLoadDefaults().qualityPresetId || '')),
        qualityAllowedNames: '',
        qualityItems: existing?.qualities || [],
        qualityEditorOpen: false,
        qualityEditGroups: false,
        baselineCFs: existing?.baselineCFs || [],
        coreCFIds: existing?.coreCFIds || [],
        templateId: '',
        selectedCFs: {},
        requiredCFs: {},
        defaultOnCFs: {},
        formatItemCFs: {},
        enabledGroups: {},
        cfStateOverrides: {},
        scoreOverrides: {},
        trashProfileId: existing?.trashProfileId || '',
        trashProfileName: '',
        variantGoldenRule: existing?.variantGoldenRule || (existing ? '' : (this._pbLoadDefaults().variantGoldenRule || '')),
        goldenRuleDefault: existing?.goldenRuleDefault || '',
        variantMisc: existing?.variantMisc || (existing ? '' : (this._pbLoadDefaults().variantMisc || '')),
        trashScoreSet: existing?.trashScoreSet || (existing ? '' : (this._pbLoadDefaults().trashScoreSet || '')),
        trashDescription: existing?.trashDescription || '',
        groupNum: existing?.groupNum || 0,
      };
      // Populate from existing profile
      if (existing?.formatItems) {
        for (const [tid, score] of Object.entries(existing.formatItems)) {
          this.pb.selectedCFs[tid] = true;
          this.pb.scoreOverrides[tid] = score;
        }
      }
      if (existing?.requiredCFs) {
        for (const tid of existing.requiredCFs) {
          this.pb.requiredCFs[tid] = true;
        }
      }
      if (existing?.defaultOnCFs) {
        for (const tid of existing.defaultOnCFs) {
          this.pb.defaultOnCFs[tid] = true;
        }
      }
      // Restore new group-based state
      if (existing?.formatItemCFs && Object.keys(existing.formatItemCFs).length > 0) {
        this.pb.formatItemCFs = { ...existing.formatItemCFs };
      } else if ((existing?.source === 'import' || (!existing?.source && existing?.formatItems)) && existing?.formatItems) {
        // Fallback for profiles imported before formatItemCFs/Source were
        // populated server-side (Recyclarr YAML imports historically
        // omitted both — per the ImportedProfile struct comment, missing
        // source is treated as "import"). A TRaSH profile's formatItems
        // are, by convention, the required CFs. Without this fallback,
        // the Builder opens with an empty Required section AND a Save
        // would wipe the profile (saveCustomProfile builds its sync-set
        // from formatItemCFs + enabledGroups, ignoring selectedCFs).
        for (const tid of Object.keys(existing.formatItems)) {
          this.pb.formatItemCFs[tid] = true;
        }
      }
      if (existing?.enabledGroups) {
        this.pb.enabledGroups = { ...existing.enabledGroups };
      }
      if (existing?.cfStateOverrides) {
        this.pb.cfStateOverrides = { ...existing.cfStateOverrides };
      }
      this.pbExpandedCats = {};
      this.pbAddMoreOpen = false;
      this.pbSettingsOpen = !existing; // collapse settings when editing
      this.pbInstanceImportId = '';
      this.pbInstanceImportProfiles = [];
      this.pbInstanceImportProfileId = '';
      this.profileBuilder = true;
      await this.loadCFPicker(appType);
      // Apply remembered Golden Rule variant (localStorage defaults)
      if (!existing && this.pb.variantGoldenRule) {
        this.pbApplyGoldenRule();
      }
      // After presets loaded, restore quality preset display
      if (existing?.qualityPresetId) {
        // Use saved preset ID
        const match = this.pbQualityPresets.find(p => p.id === existing.qualityPresetId);
        if (match) {
          this.pb.qualityAllowedNames = (match.allowed || []).join(', ');
        }
      } else if (existing?.cutoff) {
        // Fallback: try to match by cutoff
        const match = this.pbQualityPresets.find(p => p.cutoff === existing.cutoff);
        if (match) {
          this.pb.qualityPresetId = match.id;
          this.pb.qualityPreset = match.id;
          this.pb.qualityAllowedNames = (match.allowed || []).join(', ');
        }
      }
      // Update quality display from items if available
      if (existing?.qualities?.length) {
        this.pb.qualityAllowedNames = existing.qualities.filter(q => q.allowed).map(q => q.name).join(', ');
      }
    },

    editCustomProfile(appType, profile) {
      this.openProfileBuilder(appType, profile);
    },

    cancelProfileBuilder() {
      this.profileBuilder = false;
      // Navigate back to Advanced tab (builder lives there now)
      if (this.currentSection !== 'advanced') {
        this.currentSection = 'advanced';
      }
      this._resyncReturnSubTab = null;
    },

    async loadCFPicker(appType) {
      this.pbLoading = true;
      try {
        const [cfRes, qpRes] = await Promise.all([
          fetch(`/api/trash/${appType}/all-cfs`),
          fetch(`/api/trash/${appType}/quality-presets`),
        ]);
        if (cfRes.ok) {
          const data = await cfRes.json();
          this.pbCategories = data.categories || [];
          this.pbScoreSets = data.scoreSets || [];
        }
        if (qpRes.ok) {
          this.pbQualityPresets = await qpRes.json() || [];
        }
      } catch (e) { /* ignore */ }
      this.pbLoading = false;
    },

    pbQualityPresetGroups() {
      const groupOrder = { 'Standard': 1, 'SQP': 2, 'French': 3, 'German': 4, 'Anime': 5 };
      const groups = new Set();
      for (const qp of this.pbQualityPresets) {
        const name = qp.name;
        if (name.startsWith('[SQP]')) groups.add('SQP');
        else if (name.startsWith('[French')) groups.add('French');
        else if (name.startsWith('[German')) groups.add('German');
        else if (name.startsWith('[Anime')) groups.add('Anime');
        else groups.add('Standard');
      }
      return [...groups].sort((a, b) => (groupOrder[a] ?? 99) - (groupOrder[b] ?? 99));
    },

    pbQualityPresetsByGroup(groupName) {
      return this.pbQualityPresets.filter(qp => {
        const name = qp.name;
        if (groupName === 'SQP') return name.startsWith('[SQP]');
        if (groupName === 'French') return name.startsWith('[French');
        if (groupName === 'German') return name.startsWith('[German');
        if (groupName === 'Anime') return name.startsWith('[Anime');
        return !name.startsWith('[');
      });
    },

    // --- Quality Editor ---

    // Ensure every pb.qualityItems entry has a stable _id so shared qs-helpers can track drag/drop
    // and rename by identity (not index). Call this whenever entering the quality editor.
    pbEnsureQualityIds() {
      let changed = false;
      for (const it of (this.pb.qualityItems || [])) {
        if (!it._id) { it._id = ++this._qsIdCounter; changed = true; }
      }
      if (changed) this.pb.qualityItems = [...this.pb.qualityItems];
    },

    async pbInitQualityEditor() {
      // If we already have items, use them
      if (this.pb.qualityItems.length > 0) return;
      // Try to load quality definitions from first instance of matching type
      const inst = this.instancesOfType(this.pb.appType)[0];
      if (inst) {
        try {
          const r = await fetch(`/api/instances/${inst.id}/quality-definitions`);
          if (r.ok) {
            const defs = await r.json();
            // Create default items (all ungrouped, none allowed, reversed for highest priority first)
            this.pb.qualityItems = defs.reverse().map(d => ({ name: d.name, allowed: false }));
            return;
          }
        } catch (e) { /* ignore */ }
      }
      // Fallback: hardcoded Radarr defaults (highest priority first)
      const defaults = ['BR-DISK','Raw-HD','Remux-2160p','Bluray-2160p','WEBRip-2160p','WEBDL-2160p',
        'HDTV-2160p','Remux-1080p','Bluray-1080p','WEBRip-1080p','WEBDL-1080p','HDTV-1080p',
        'Bluray-720p','WEBRip-720p','WEBDL-720p','HDTV-720p',
        'Bluray-576p','Bluray-480p','WEBRip-480p','WEBDL-480p',
        'DVD-R','DVD','SDTV','DVDSCR','REGIONAL','TELECINE','TELESYNC','CAM','WORKPRINT','Unknown'];
      this.pb.qualityItems = defaults.map(name => ({ name, allowed: false }));
    },

    pbMoveQuality(idx, dir) {
      const items = [...this.pb.qualityItems];
      const newIdx = idx + dir;
      if (newIdx < 0 || newIdx >= items.length) return;
      [items[idx], items[newIdx]] = [items[newIdx], items[idx]];
      this.pb.qualityItems = items;
    },

    pbRemoveFromGroup(groupIdx, subIdx) {
      const items = [...this.pb.qualityItems];
      const group = { ...items[groupIdx], items: [...items[groupIdx].items] };
      const removed = group.items.splice(subIdx, 1)[0];
      if (group.items.length === 0) {
        // Group is empty, replace with single quality using group name
        items[groupIdx] = { name: group.name, allowed: group.allowed };
      } else {
        items[groupIdx] = group;
      }
      // Insert removed item after the group, inheriting group's allowed state
      items.splice(groupIdx + 1, 0, { name: removed, allowed: group.allowed });
      this.pbUpdateQualityDisplay();
      this.pb.qualityItems = items;
    },

    pbAddToGroup(itemIdx, groupName) {
      if (!groupName) return;
      const items = [...this.pb.qualityItems];
      const item = items[itemIdx];

      if (groupName === '__new__') {
        const name = prompt('Group name:');
        if (!name) return;
        // Convert item into a group
        items[itemIdx] = { name: name, allowed: item.allowed, items: [item.name] };
        this.pb.qualityItems = items;
        return;
      }

      // Remove item first, then find and update group (avoids index shift)
      items.splice(itemIdx, 1);
      const groupIdx = items.findIndex(q => q.name === groupName && q.items?.length > 0);
      if (groupIdx < 0) return;
      items[groupIdx] = { ...items[groupIdx], items: [...items[groupIdx].items, item.name] };
      this.pb.qualityItems = items;
      this.pbUpdateQualityDisplay();
    },

    pbUpdateQualityDisplay() {
      const allowed = this.pb.qualityItems.filter(q => q.allowed).map(q => q.name);
      this.pb.qualityAllowedNames = allowed.join(', ');
      // Update cutoff if current cutoff is no longer allowed
      if (this.pb.cutoff && !allowed.includes(this.pb.cutoff)) {
        this.pb.cutoff = allowed[0] || '';
      }
    },

    pbApplyQualityPreset() {
      const id = this.pb.qualityPresetId;
      if (!id) {
        this.pb.cutoff = '';
        this.pb.qualityPreset = '';
        this.pb.qualityAllowedNames = '';
        this.pb.qualityItems = [];
        return;
      }
      const preset = this.pbQualityPresets.find(p => p.id === id);
      if (!preset) return;
      this.pb.cutoff = preset.cutoff;
      this.pb.qualityPreset = preset.id;
      this.pb.qualityAllowedNames = (preset.allowed || []).join(', ');
      this.pb.qualityItems = preset.items || [];
    },

    // Languages for profile builder — uses first instance of matching type, or fallback
    get pbLanguages() {
      const inst = this.instancesOfType(this.pb.appType)[0];
      if (inst && this.instanceLanguages[inst.id]) return this.instanceLanguages[inst.id];
      // Trigger async load if instance available
      if (inst && !this.instanceLanguages[inst.id]) this.getLanguagesForInstance(inst.id);
      return [{ id: -1, name: 'Original' }, { id: 0, name: 'Any' }];
    },

    pbSelectedCount() {
      return Object.keys(this.pb.selectedCFs).filter(k => this.pb.selectedCFs[k]).length;
    },

    pbRequiredCount() {
      return Object.keys(this.pb.requiredCFs).filter(k => this.pb.requiredCFs[k] && this.pb.selectedCFs[k]).length;
    },

    get pbFilteredCategories() {
      if (!this.pbCategories.length) return this.pbCategories;

      return this.pbCategories.map(cat => {
        let filtered = cat.groups;

        // Filter by template profile if set
        if (this.pb.trashProfileName) {
          const profName = this.pb.trashProfileName;
          const fiSet = this.pb.formatItemCFs || {};
          const selSet = this.pb.selectedCFs || {};
          const enSet = this.pb.enabledGroups || {};
          filtered = filtered.filter(g => {
            // Always show if group includes this profile
            if (!g.includeProfiles || g.includeProfiles.length === 0 || g.includeProfiles.includes(profName)) return true;
            // Show if group is enabled
            if (g.groupTrashId && enSet[g.groupTrashId]) return true;
            // Show if any of the group's CFs are in formatItems or selected
            if (g.cfs?.some(cf => fiSet[cf.trashId] || selSet[cf.trashId])) return true;
            return false;
          });
        }

        // Filter by variant dropdowns
        {
          if (this.pb.variantGoldenRule === 'HD') {
            filtered = filtered.filter(g => g.name !== '[Required] Golden Rule UHD');
          } else if (this.pb.variantGoldenRule === 'UHD') {
            filtered = filtered.filter(g => g.name !== '[Required] Golden Rule HD');
          } else if (this.pb.variantGoldenRule === 'none') {
            filtered = filtered.filter(g => g.name !== '[Required] Golden Rule HD' && g.name !== '[Required] Golden Rule UHD');
          }
          if (this.pb.variantMisc === 'Standard') {
            filtered = filtered.filter(g => g.name !== '[Optional] Miscellaneous SQP');
          } else if (this.pb.variantMisc === 'SQP') {
            filtered = filtered.filter(g => g.name !== '[Optional] Miscellaneous');
          } else if (this.pb.variantMisc === 'none') {
            filtered = filtered.filter(g => g.name !== '[Optional] Miscellaneous' && g.name !== '[Optional] Miscellaneous SQP');
          }
        }

        if (filtered.length === 0) return null;
        return { ...cat, groups: filtered };
      }).filter(Boolean);
    },

    pbHasGroupVariants() {
      // Check if there are conflicting group pairs in the categories
      const names = new Set();
      for (const cat of this.pbCategories) {
        for (const g of cat.groups) names.add(g.name);
      }
      return (names.has('[Required] Golden Rule HD') && names.has('[Required] Golden Rule UHD')) ||
             (names.has('[Optional] Miscellaneous') && names.has('[Optional] Miscellaneous SQP'));
    },

    pbGroupVariant(type) {
      const names = new Set();
      for (const cat of this.pbCategories) {
        for (const g of cat.groups) names.add(g.name);
      }
      if (type === 'Golden Rule') return names.has('[Required] Golden Rule HD') && names.has('[Required] Golden Rule UHD');
      if (type === 'Miscellaneous') return names.has('[Optional] Miscellaneous') && names.has('[Optional] Miscellaneous SQP');
      return false;
    },

    // Check if a group is disabled because another group sharing CFs has active selections
    pbIsGroupDisabled(group) {
      if (!group.cfs || group.cfs.length === 0) return false;
      const groupCFIds = new Set(group.cfs.map(cf => cf.trashId));
      // Check if any of this group's CFs are selected
      const hasOwnSelection = group.cfs.some(cf => this.pb.selectedCFs[cf.trashId]);
      if (hasOwnSelection) return false; // This group is active, not disabled
      // Check all groups (not just filtered) for shared CF conflicts
      for (const cat of this.pbCategories) {
        for (const g of cat.groups) {
          if (g === group || g.name === group.name) continue;
          const shared = g.cfs?.some(cf => groupCFIds.has(cf.trashId));
          if (shared && g.cfs?.some(cf => this.pb.selectedCFs[cf.trashId])) {
            return true; // Another group with shared CFs has active selections
          }
        }
      }
      return false;
    },

    _pbCatCFs(cat) {
      // Flatten all CFs across groups in a category
      const cfs = [];
      for (const g of (cat.groups || [])) {
        for (const cf of (g.cfs || [])) cfs.push(cf);
      }
      return cfs;
    },

    pbCatSelectedCount(cat) {
      return this._pbCatCFs(cat).filter(cf => this.pb.selectedCFs[cf.trashId]).length;
    },

    pbCatTotalCount(cat) {
      return this._pbCatCFs(cat).length;
    },

    pbIsCatAllSelected(cat) {
      const cfs = this._pbCatCFs(cat);
      return cfs.length > 0 && cfs.every(cf => this.pb.selectedCFs[cf.trashId]);
    },

    pbGroupSelectedCount(group) {
      return (group.cfs || []).filter(cf => this.pb.selectedCFs[cf.trashId]).length;
    },

    pbIsGroupAllSelected(group) {
      return group.cfs.length > 0 && group.cfs.every(cf => this.pb.selectedCFs[cf.trashId]);
    },

    pbToggleCF(trashId, exclusiveGroup) {
      if (this.pb.selectedCFs[trashId]) {
        const {[trashId]: _s, ...restSelected} = this.pb.selectedCFs;
        const {[trashId]: _r, ...restRequired} = this.pb.requiredCFs;
        const {[trashId]: _o, ...restOverrides} = this.pb.scoreOverrides;
        this.pb.selectedCFs = restSelected;
        this.pb.requiredCFs = restRequired;
        this.pb.scoreOverrides = restOverrides;
      } else {
        const newSelected = {...this.pb.selectedCFs, [trashId]: true};
        // Exclusive group: deselect other CFs in this group AND any other
        // exclusive groups that share the same CFs (e.g. Golden Rule HD/UHD)
        if (exclusiveGroup) {
          const sharedIds = new Set(exclusiveGroup.cfs.map(cf => cf.trashId));
          for (const cf of exclusiveGroup.cfs) {
            if (cf.trashId !== trashId) delete newSelected[cf.trashId];
          }
          // Find all other exclusive groups sharing any CF with this group
          for (const cat of this.pbFilteredCategories) {
            for (const g of cat.groups) {
              if (g === exclusiveGroup || !g.exclusive) continue;
              if (g.cfs.some(cf => sharedIds.has(cf.trashId))) {
                for (const cf of g.cfs) {
                  if (cf.trashId !== trashId) delete newSelected[cf.trashId];
                }
              }
            }
          }
        }
        this.pb.selectedCFs = newSelected;
      }
    },

    pbToggleCategory(cat) {
      const cfs = this._pbCatCFs(cat);
      const allSelected = this.pbIsCatAllSelected(cat);
      const newSelected = {...this.pb.selectedCFs};
      const newRequired = {...this.pb.requiredCFs};
      const newOverrides = {...this.pb.scoreOverrides};
      for (const cf of cfs) {
        if (allSelected) {
          delete newSelected[cf.trashId];
          delete newRequired[cf.trashId];
          delete newOverrides[cf.trashId];
        } else {
          newSelected[cf.trashId] = true;
        }
      }
      this.pb.selectedCFs = newSelected;
      this.pb.requiredCFs = newRequired;
      this.pb.scoreOverrides = newOverrides;
    },

    pbIsCatAllRequired(cat) {
      const cfs = this._pbCatCFs(cat);
      const selected = cfs.filter(cf => this.pb.selectedCFs[cf.trashId]);
      return selected.length > 0 && selected.every(cf => this.pb.requiredCFs[cf.trashId]);
    },

    // Toggle a CF group on/off by groupTrashId
    pbToggleGroupInclude(group) {
      const gid = group.groupTrashId;
      const newEnabled = { ...this.pb.enabledGroups };
      const newSelected = { ...this.pb.selectedCFs };
      const newFormatItems = { ...this.pb.formatItemCFs };
      if (newEnabled[gid]) {
        // Disable group — remove its CFs from selectedCFs and formatItemCFs
        delete newEnabled[gid];
        for (const cf of group.cfs) {
          delete newSelected[cf.trashId];
          delete newFormatItems[cf.trashId];
        }
      } else {
        // Enable group — pre-select only the CFs the group's data marks as
        // default (or required). This respects TRaSH's per-CF default flag
        // — the whole point of that flag is to skip the same per-CF toggles
        // in every profile that includes the group. CFs without default
        // still appear in the group and remain user-toggleable.
        //
        // Profile Builder data uses cf.cfDefault (camelCase) on
        // CategorizedCF — see core/trash.go:1087. Profile Detail uses
        // cf.default on ProfileCFGroupEntry. Same TRaSH flag, two JSON
        // names because the two endpoints carry different shapes.
        newEnabled[gid] = true;
        for (const cf of group.cfs) {
          if (cf.required || cf.cfDefault) {
            newSelected[cf.trashId] = true;
          }
        }
      }
      this.pb.enabledGroups = newEnabled;
      this.pb.selectedCFs = newSelected;
      this.pb.formatItemCFs = newFormatItems;
    },

    // Check if a group is enabled
    pbIsGroupEnabled(group) {
      return !!this.pb.enabledGroups[group.groupTrashId];
    },

    // Get CF state: 'formatItems', 'required', or 'optional'
    pbGetCFState(cf) {
      if (this.pb.formatItemCFs[cf.trashId]) return 'formatItems';
      if (this.pb.cfStateOverrides?.[cf.trashId] === 'required') return 'required';
      if (this.pb.cfStateOverrides?.[cf.trashId] === 'optional') return 'optional';
      // Default from TRaSH group data
      return cf.required ? 'required' : 'optional';
    },

    // Set CF state: 'required', 'optional', or 'formatItems'.
    // Also ensures the CF is added to pb.selectedCFs — clicking a state pill
    // is the way to "include" a CF that's in an enabled group but isn't yet
    // in the profile (the dimmed state). Previously only formatItems-state
    // added to selectedCFs, so clicking Req/Opt on a dimmed CF set the state
    // override but the CF stayed out of the profile. After this change all
    // three pills "lift" the CF into the profile with the chosen state.
    pbSetCFState(trashId, state) {
      const newFI = { ...this.pb.formatItemCFs };
      const newOverrides = { ...(this.pb.cfStateOverrides || {}) };

      if (state === 'formatItems') {
        newFI[trashId] = true;
        delete newOverrides[trashId];
      } else {
        delete newFI[trashId];
        newOverrides[trashId] = state;
      }
      this.pb.selectedCFs = { ...this.pb.selectedCFs, [trashId]: true };
      this.pb.formatItemCFs = newFI;
      this.pb.cfStateOverrides = newOverrides;
    },

    // Toggle a CF into/out of formatItemCFs (required/mandatory)
    pbToggleFormatItem(trashId) {
      const newFI = { ...this.pb.formatItemCFs };
      if (newFI[trashId]) {
        delete newFI[trashId];
      } else {
        newFI[trashId] = true;
      }
      this.pb.formatItemCFs = newFI;
    },

    // Get selected formatItem CFs as a list with CF data
    pbFormatItemCFList() {
      const result = [];
      const fiSet = this.pb.formatItemCFs || {};
      const seen = new Set();
      // Include ALL CFs that are in formatItemCFs, regardless of group membership
      for (const cat of this.pbCategories) {
        for (const g of cat.groups) {
          for (const cf of g.cfs) {
            if (fiSet[cf.trashId] && !seen.has(cf.trashId)) {
              result.push(cf);
              seen.add(cf.trashId);
            }
          }
        }
      }
      return result;
    },

    // Get ungrouped CFs NOT in formatItems (for "Add more" section)
    pbAvailableFormatItemCFs() {
      const result = [];
      const fiSet = this.pb.formatItemCFs || {};
      for (const cat of this.pbCategories) {
        for (const g of cat.groups) {
          if (g.groupTrashId) continue; // only ungrouped CFs available to add
          for (const cf of g.cfs) {
            if (!fiSet[cf.trashId]) result.push(cf);
          }
        }
      }
      return result;
    },

    // Get available CFs filtered by search term
    pbFilteredAvailableCFs() {
      const all = this.pbAvailableFormatItemCFs();
      const q = (this.pbFormatItemSearch || '').trim().toLowerCase();
      if (!q) return all;
      return all.filter(cf => cf.name.toLowerCase().includes(q));
    },

    // Tier-based ordering for CF categories. Used by every place that sorts
    // CF groups so the Profile Builder, Custom Formats tab, and Profile
    // Detail (Trash Sync) all show groups in the same order. Mirrors the
    // backend's CompareCFCategories in trash.go.
    //
    //   0 — regular TRaSH categories (alphabetical within tier)
    //   1 — SQP-prefix categories ([SQP], [SQP-1], [SQP-4 (MA Hybrid) Optional]...)
    //   2 — "Other" / unrecognised
    //   3 — Custom (user-authored CFs/groups, kept at the bottom so user-data
    //       stays visually separated from TRaSH-derived data)
    //
    // Within the same tier, category name compared alphabetically.
    _categoryTier(cat) {
      if (!cat) return 2;
      if (cat === 'Custom') return 3;
      if (/^SQP/i.test(cat)) return 1;
      if (cat === 'Other') return 2;
      return 0;
    },
    _compareCFCategories(a, b) {
      const ta = this._categoryTier(a), tb = this._categoryTier(b);
      if (ta !== tb) return ta - tb;
      return (a || '').localeCompare(b || '');
    },

    // Mirror of backend CompareCFGroups (internal/core/trash.go). Sorts
    // cf-groups by the TRaSH-style `group` integer field. Tiers, applied in
    // order:
    //   1. Tier 3 (custom): user-authored groups always sort last
    //   2. Tier 1 (has `group` set): sorts by integer, alphabetical tiebreak
    //   3. Tier 2 (no `group`): alphabetical fallback
    // Backend convention: 1-9 English public, 11-19 German, 21-29 French,
    // 81-89 Anime, 91-99 SQP. `groupNum` may be null/undefined for cf-groups
    // that don't carry the field (TRaSH pre-rollout, user groups left empty).
    _compareCFGroups(aName, aGroup, aCustom, bName, bGroup, bCustom) {
      // Tier 3: custom always last.
      if (aCustom !== bCustom) return aCustom ? 1 : -1;
      const aHas = aGroup !== null && aGroup !== undefined;
      const bHas = bGroup !== null && bGroup !== undefined;
      if (aHas && bHas) {
        if (aGroup !== bGroup) return aGroup - bGroup;
        // tiebreak alphabetical
      } else if (aHas) {
        return -1;
      } else if (bHas) {
        return 1;
      }
      return (aName || '').localeCompare(bName || '');
    },

    // Get all groups as a flat sorted list (not nested under categories).
    // Drops the prior "defaultEnabled first" sub-sort — alphabetical only,
    // matching the rest of the UI. defaultEnabled groups are still visually
    // distinguishable via the green "default" pill.
    pbSortedGroups() {
      const groups = [];
      for (const cat of this.pbFilteredCategories) {
        for (const g of cat.groups) {
          if (!g.groupTrashId) continue;
          groups.push({ ...g, _category: cat.category });
        }
      }
      // Sort by the backend's `group` integer (TRaSH convention). Backend
      // populates g.group + g.isCustom on each CFPickerGroup payload.
      groups.sort((a, b) =>
        this._compareCFGroups(a.shortName || '', a.group, !!a.isCustom,
                              b.shortName || '', b.group, !!b.isCustom));
      return groups;
    },

    // Check if any CF in group has a specific state
    pbGroupHasAnyState(group, state) {
      return group.cfs.some(cf => this.pbGetCFState(cf) === state);
    },

    // Check if ALL CFs in group have a specific state
    pbGroupHasAllState(group, state) {
      return group.cfs.length > 0 && group.cfs.every(cf => this.pbGetCFState(cf) === state);
    },

    // Set all CFs in a group to a state
    pbSetGroupState(group, state) {
      for (const cf of group.cfs) {
        this.pbSetCFState(cf.trashId, state);
      }
      // If all CFs moved to formatItems, disable the group (no longer needed as group)
      if (state === 'formatItems' && group.groupTrashId) {
        const newEnabled = { ...this.pb.enabledGroups };
        delete newEnabled[group.groupTrashId];
        this.pb.enabledGroups = newEnabled;
      }
      // If moving back from formatItems to group state, re-enable the group
      if (state !== 'formatItems' && group.groupTrashId && !this.pb.enabledGroups[group.groupTrashId]) {
        this.pb.enabledGroups = { ...this.pb.enabledGroups, [group.groupTrashId]: true };
      }
    },

    // Count formatItem CFs
    pbFormatItemCount() {
      return Object.keys(this.pb.formatItemCFs || {}).length;
    },

    // Count enabled groups
    pbEnabledGroupCount() {
      return Object.keys(this.pb.enabledGroups || {}).length;
    },

    // Golden Rule: auto-set both CFs when variant is selected
    pbIsGoldenRuleCF(trashId) {
      return trashId === 'dc98083864ea246d05a42df0d05f81cc' || trashId === '839bea857ed2c0a8e084f3cbdbd65ecb';
    },

    pbApplyGoldenRule() {
      const grHDcf1 = 'dc98083864ea246d05a42df0d05f81cc';   // x265 (HD)
      const grUHDcf1 = '839bea857ed2c0a8e084f3cbdbd65ecb';  // x265 (no HDR/DV)
      const grHDGroup = 'f8bf8eab4617f12dfdbd16303d8da245';  // [Required] Golden Rule HD group
      const grUHDGroup = 'ff204bbcecdd487d1cefcefdbf0c278d'; // [Required] Golden Rule UHD group
      const newSelected = {...this.pb.selectedCFs};
      const newEnabled = {...this.pb.enabledGroups};
      const variant = this.pb.variantGoldenRule;

      // Find all CFs in each Golden Rule group from pbCategories
      const grHDCFs = [];
      const grUHDCFs = [];
      for (const cat of this.pbCategories) {
        for (const g of cat.groups) {
          if (g.groupTrashId === grHDGroup) grHDCFs.push(...g.cfs.map(cf => cf.trashId));
          if (g.groupTrashId === grUHDGroup) grUHDCFs.push(...g.cfs.map(cf => cf.trashId));
        }
      }

      if (variant === 'HD') {
        newEnabled[grHDGroup] = true;
        delete newEnabled[grUHDGroup];
        for (const tid of grHDCFs) newSelected[tid] = true;
        for (const tid of grUHDCFs) delete newSelected[tid];
        this.pb.goldenRuleDefault = grHDcf1;
      } else if (variant === 'UHD') {
        delete newEnabled[grHDGroup];
        newEnabled[grUHDGroup] = true;
        for (const tid of grUHDCFs) newSelected[tid] = true;
        for (const tid of grHDCFs) delete newSelected[tid];
        this.pb.goldenRuleDefault = grUHDcf1;
      } else {
        delete newEnabled[grHDGroup];
        delete newEnabled[grUHDGroup];
        for (const tid of grHDCFs) delete newSelected[tid];
        for (const tid of grUHDCFs) delete newSelected[tid];
        this.pb.goldenRuleDefault = '';
      }
      this.pb.selectedCFs = newSelected;
      this.pb.enabledGroups = newEnabled;
    },

    pbApplyMisc() {
      const miscStdGroup = '9337080378236ce4c0b183e35790d2a7';  // [Optional] Miscellaneous
      const miscSqpGroup = 'c4492eebd0c2ddc14c2c91623aa7f95d';  // [Optional] Miscellaneous SQP
      const newEnabled = { ...this.pb.enabledGroups };
      const newSelected = { ...this.pb.selectedCFs };
      const variant = this.pb.variantMisc;

      // Find CFs in each Misc group
      const stdCFs = [];
      const sqpCFs = [];
      for (const cat of this.pbCategories) {
        for (const g of cat.groups) {
          if (g.groupTrashId === miscStdGroup) stdCFs.push(...g.cfs.map(cf => cf.trashId));
          if (g.groupTrashId === miscSqpGroup) sqpCFs.push(...g.cfs.map(cf => cf.trashId));
        }
      }

      if (variant === 'Standard') {
        newEnabled[miscStdGroup] = true;
        delete newEnabled[miscSqpGroup];
        for (const tid of stdCFs) newSelected[tid] = true;
        for (const tid of sqpCFs) delete newSelected[tid];
      } else if (variant === 'SQP') {
        delete newEnabled[miscStdGroup];
        newEnabled[miscSqpGroup] = true;
        for (const tid of sqpCFs) newSelected[tid] = true;
        for (const tid of stdCFs) delete newSelected[tid];
      } else {
        delete newEnabled[miscStdGroup];
        delete newEnabled[miscSqpGroup];
        for (const tid of stdCFs) delete newSelected[tid];
        for (const tid of sqpCFs) delete newSelected[tid];
      }
      this.pb.selectedCFs = newSelected;
      this.pb.enabledGroups = newEnabled;
    },

    pbToggleCatRequired(cat) {
      const cfs = this._pbCatCFs(cat);
      const allReq = this.pbIsCatAllRequired(cat);
      const newRequired = {...this.pb.requiredCFs};
      const newSelected = {...this.pb.selectedCFs};
      for (const cf of cfs) {
        if (allReq) {
          // Switch all to optional (keep selected)
          delete newRequired[cf.trashId];
        } else {
          // Switch all to required (also select unselected CFs)
          newSelected[cf.trashId] = true;
          newRequired[cf.trashId] = true;
        }
      }
      this.pb.selectedCFs = newSelected;
      this.pb.requiredCFs = newRequired;
    },

    pbToggleGroup(group) {
      const allSelected = this.pbIsGroupAllSelected(group);
      const newSelected = {...this.pb.selectedCFs};
      for (const cf of group.cfs) {
        if (allSelected) {
          delete newSelected[cf.trashId];
        } else {
          newSelected[cf.trashId] = true;
        }
      }
      this.pb.selectedCFs = newSelected;
    },

    pbGetScore(cf) {
      if (this.pb.scoreOverrides[cf.trashId] !== undefined) {
        return this.pb.scoreOverrides[cf.trashId];
      }
      const scores = cf.trashScores || {};
      return scores[this.pb.scoreSet] ?? scores['default'] ?? 0;
    },

    pbSetScore(trashId, value) {
      this.pb.scoreOverrides[trashId] = parseInt(value) || 0;
    },

    pbCleanScore(trashId) {
      // If override matches TRaSH default, remove override
      const cf = this._pbFindCF(trashId);
      if (!cf) return;
      const trashScore = cf.trashScores?.[this.pb.scoreSet] ?? cf.trashScores?.['default'] ?? 0;
      if (this.pb.scoreOverrides[trashId] === trashScore) {
        const {[trashId]: _, ...rest} = this.pb.scoreOverrides;
        this.pb.scoreOverrides = rest;
      }
    },

    pbIsScoreOverridden(cf) {
      if (this.pb.scoreOverrides[cf.trashId] === undefined) return false;
      const trashScore = cf.trashScores?.[this.pb.scoreSet] ?? cf.trashScores?.['default'] ?? 0;
      return this.pb.scoreOverrides[cf.trashId] !== trashScore;
    },

    _pbFindCF(trashId) {
      for (const cat of this.pbCategories) {
        for (const g of (cat.groups || [])) {
          for (const cf of (g.cfs || [])) {
            if (cf.trashId === trashId) return cf;
          }
        }
      }
      return null;
    },

    sortedScoreSets() {
      return this.pbScoreSets.filter(s => s !== 'default').sort((a, b) => {
        const aIsSqp = a.startsWith('sqp') ? 0 : 1;
        const bIsSqp = b.startsWith('sqp') ? 0 : 1;
        if (aIsSqp !== bIsSqp) return aIsSqp - bIsSqp;
        return a.localeCompare(b);
      });
    },

    pbScoreSetChanged() {
      // Clear overrides that now match the new score set defaults
      for (const trashId of Object.keys(this.pb.scoreOverrides)) {
        this.pbCleanScore(trashId);
      }
    },

    async pbApplyTemplate() {
      const tid = this.pb.templateId;
      if (!tid) return;
      this.debugLog('UI', `Builder: applying template "${tid}"`);
      this.pbTemplateLoading = true;
      try {
        if (tid.startsWith('trash:')) {
          const trashId = tid.slice(6);
          const r = await fetch(`/api/trash/${this.pb.appType}/profiles/${trashId}`);
          if (!r.ok) { this.showToast('Failed to load TRaSH profile', 'error', 8000); return; }
          const detail = await r.json();
          // Apply score set and link to TRaSH profile (enables v8 export)
          if (detail.scoreCtx) this.pb.scoreSet = detail.scoreCtx;
          this.pb.trashProfileId = trashId;
          this.pb.trashProfileName = detail.profile?.name || '';
          this.pb.trashScoreSet = detail.scoreCtx || '';
          this.pb.trashDescription = detail.profile?.trash_description || '';
          // Apply quality preset and sync dropdown
          this.pb.qualityPreset = trashId;
          this.pb.qualityPresetId = trashId;
          this.pb.qualityItems = detail.profile?.items || [];
          // Apply profile settings
          const prof = detail.profile || {};
          if (prof.cutoff) this.pb.cutoff = prof.cutoff;
          // Update allowed names display
          const matchedPreset = this.pbQualityPresets.find(p => p.id === trashId);
          if (matchedPreset) {
            this.pb.qualityAllowedNames = (matchedPreset.allowed || []).join(', ');
          } else {
            const allowedItems = (prof.items || []).filter(i => i.allowed).map(i => i.name);
            this.pb.qualityAllowedNames = allowedItems.join(', ');
            // The template's quality config may not be in presets — set qualityPresetId to match cutoff
            const cutoffMatch = this.pbQualityPresets.find(p => p.cutoff === prof.cutoff);
            if (cutoffMatch) this.pb.qualityPresetId = cutoffMatch.id;
          }
          if (prof.cutoffFormatScore != null) this.pb.cutoffScore = prof.cutoffFormatScore;
          if (prof.minFormatScore != null) this.pb.minFormatScore = prof.minFormatScore;
          if (prof.minUpgradeFormatScore != null) this.pb.minUpgradeFormatScore = prof.minUpgradeFormatScore;
          if (prof.upgradeAllowed != null) this.pb.upgradeAllowed = prof.upgradeAllowed;
          // Reset expanded state
          this.pbExpandedCats = {};
          this.pbAddMoreOpen = false;
          // Apply required CFs (core profile definition → formatItems)
          this.pb.selectedCFs = {};
          this.pb.scoreOverrides = {};
          this.pb.requiredCFs = {};
          this.pb.formatItemCFs = {};
          this.pb.enabledGroups = {};
          this.pb.cfStateOverrides = {};
          const baselineCFs = new Set();
          const coreCFIds = [];
          const coreCFSet = new Set((detail.coreCFs || []).map(cf => cf.trashId));

          // Build lookup: which CFs belong to which groups (regardless of profile include)
          const cfToGroup = {};
          for (const cat of this.pbCategories) {
            for (const g of cat.groups) {
              if (!g.groupTrashId) continue;
              for (const cf of g.cfs) {
                cfToGroup[cf.trashId] = g;
              }
            }
          }

          // Check if entire groups are in formatItems (all group CFs are in coreCFs)
          const groupsInFormatItems = new Set();
          for (const cat of this.pbCategories) {
            for (const g of cat.groups) {
              if (!g.groupTrashId) continue;
              const allInCore = g.cfs.every(cf => coreCFSet.has(cf.trashId));
              if (allInCore && g.cfs.length > 0) {
                groupsInFormatItems.add(g.groupTrashId);
              }
            }
          }

          for (const cf of (detail.coreCFs || [])) {
            this.pb.selectedCFs[cf.trashId] = true;
            baselineCFs.add(cf.trashId);
            coreCFIds.push(cf.trashId);
            if (cf.score != null) this.pb.scoreOverrides[cf.trashId] = cf.score;

            const group = cfToGroup[cf.trashId];
            if (group && groupsInFormatItems.has(group.groupTrashId)) {
              // CF belongs to a group that's entirely in formatItems → set as Fmt in group
              this.pb.formatItemCFs[cf.trashId] = true;
              this.pb.enabledGroups[group.groupTrashId] = true;
              // Set CF state to formatItems within the group
              if (!this.pb.cfStateOverrides) this.pb.cfStateOverrides = {};
              this.pb.cfStateOverrides[cf.trashId] = 'formatItems';
            } else if (group) {
              // CF is in a group but not all group CFs are in formatItems — treat as individual formatItem
              this.pb.formatItemCFs[cf.trashId] = true;
            } else {
              // Ungrouped CF — normal formatItem
              this.pb.formatItemCFs[cf.trashId] = true;
            }
          }

          // Apply CFs from groups — only enable groups that include this profile
          for (const cat of this.pbCategories) {
            for (const g of cat.groups) {
              const includesProfile = g.includeProfiles?.includes(this.pb.trashProfileName);
              if (!includesProfile) continue;
              // Skip groups already handled as formatItems
              if (groupsInFormatItems.has(g.groupTrashId)) continue;
              // Only auto-enable default groups
              if (g.groupTrashId && g.defaultEnabled) {
                this.pb.enabledGroups[g.groupTrashId] = true;
              }
              if (g.defaultEnabled) {
                for (const cf of g.cfs) {
                  this.pb.selectedCFs[cf.trashId] = true;
                  baselineCFs.add(cf.trashId);
                }
              }
            }
          }
          // Store baseline so export knows what TRaSH defines vs user additions
          this.pb.baselineCFs = [...baselineCFs];
          this.pb.coreCFIds = coreCFIds;
          // Store original formatItems key order from TRaSH profile (for identical export)
          this.pb.formatItemsOrder = detail.formatItemsOrder || [];
          // Detect Golden Rule and Misc variants from groups that include this profile
          const includedGroupNames = new Set();
          for (const cat of this.pbCategories) {
            for (const g of cat.groups) {
              if (g.includeProfiles?.includes(this.pb.trashProfileName)) {
                includedGroupNames.add(g.name);
              }
            }
          }
          if (includedGroupNames.has('[Required] Golden Rule HD')) this.pb.variantGoldenRule = 'HD';
          else if (includedGroupNames.has('[Required] Golden Rule UHD')) this.pb.variantGoldenRule = 'UHD';
          else this.pb.variantGoldenRule = 'none';
          if (includedGroupNames.has('[Optional] Miscellaneous SQP')) this.pb.variantMisc = 'SQP';
          else if (includedGroupNames.has('[Optional] Miscellaneous')) this.pb.variantMisc = 'Standard';
          else this.pb.variantMisc = 'none';
        } else if (tid.startsWith('import:')) {
          const importId = tid.slice(7);
          const profiles = this.importedProfiles[this.pb.appType] || [];
          const prof = profiles.find(p => p.id === importId);
          if (!prof) { this.showToast('Imported profile not found', 'error', 8000); return; }
          // Apply settings
          if (prof.scoreSet) this.pb.scoreSet = prof.scoreSet;
          if (prof.cutoff) this.pb.cutoff = prof.cutoff;
          if (prof.cutoffScore != null) this.pb.cutoffScore = prof.cutoffScore;
          if (prof.minFormatScore != null) this.pb.minFormatScore = prof.minFormatScore;
          if (prof.minUpgradeFormatScore != null) this.pb.minUpgradeFormatScore = prof.minUpgradeFormatScore;
          if (prof.upgradeAllowed != null) this.pb.upgradeAllowed = prof.upgradeAllowed;
          if (prof.language) this.pb.language = prof.language;
          if (prof.trashProfileId) {
            this.pb.qualityPreset = prof.trashProfileId;
            this.pb.qualityPresetId = prof.trashProfileId;
          }
          if (prof.qualities?.length) {
            this.pb.qualityItems = prof.qualities;
          }
          // Apply CFs and scores
          this.pb.selectedCFs = {};
          this.pb.scoreOverrides = {};
          this.pb.requiredCFs = {};
          this.pb.formatItemCFs = {};
          this.pb.enabledGroups = {};
          this.pb.cfStateOverrides = {};
          for (const [trashId, score] of Object.entries(prof.formatItems || {})) {
            this.pb.selectedCFs[trashId] = true;
            this.pb.scoreOverrides[trashId] = score;
          }
          // Restore new model state if available
          if (prof.formatItemCFs) {
            this.pb.formatItemCFs = { ...prof.formatItemCFs };
          }
          if (prof.enabledGroups) {
            this.pb.enabledGroups = { ...prof.enabledGroups };
          }
          if (prof.cfStateOverrides) {
            this.pb.cfStateOverrides = { ...prof.cfStateOverrides };
          }
          // Fallback: old model
          for (const tid of (prof.requiredCFs || [])) {
            this.pb.requiredCFs[tid] = true;
            // If no new model, use old requiredCFs as formatItemCFs
            if (!prof.formatItemCFs) this.pb.formatItemCFs[tid] = true;
          }
        }
        // Clean overrides that match score set defaults
        this.pbScoreSetChanged();
        // Force Alpine reactivity on pb object (needed for x-model on nested selects)
        this.pb = { ...this.pb };
      } catch (e) {
        this.showToast('Error loading template: ' + e.message, 'error', 8000);
      } finally {
        this.pbTemplateLoading = false;
      }
    },

    async pbInstanceChanged() {
      this.pbInstanceImportProfileId = '';
      this.pbInstanceImportProfiles = [];
      if (!this.pbInstanceImportId) return;
      try {
        const r = await fetch(`/api/instances/${this.pbInstanceImportId}/profiles`);
        if (r.ok) this.pbInstanceImportProfiles = await r.json();
      } catch (e) {
        console.error('Failed to load instance profiles:', e);
      }
    },

    async pbApplyInstanceProfile() {
      if (!this.pbInstanceImportId || !this.pbInstanceImportProfileId) return;
      this.pbInstanceImportLoading = true;
      try {
        const r = await fetch(`/api/instances/${this.pbInstanceImportId}/profile-export/${this.pbInstanceImportProfileId}`);
        if (!r.ok) { this.showToast('Failed to load profile from instance', 'error', 8000); return; }
        const data = await r.json();
        const prof = data.profile;
        // Apply directly to builder — no saving until user clicks Create Profile
        this.pb.name = prof.name || '';
        if (prof.cutoff) this.pb.cutoff = prof.cutoff;
        if (prof.cutoffScore != null) this.pb.cutoffScore = prof.cutoffScore;
        if (prof.minFormatScore != null) this.pb.minFormatScore = prof.minFormatScore;
        if (prof.minUpgradeFormatScore != null) this.pb.minUpgradeFormatScore = prof.minUpgradeFormatScore;
        if (prof.upgradeAllowed != null) this.pb.upgradeAllowed = prof.upgradeAllowed;
        if (prof.language) this.pb.language = prof.language;
        // Apply quality items from instance
        if (prof.qualities?.length) {
          this.pb.qualityItems = prof.qualities;
          this.pb.qualityAllowedNames = prof.qualities.filter(q => q.allowed).map(q => q.name).join(', ');
        }
        // Apply CFs and scores. Arr profiles are a flat formatItems array — map each CF
        // into Builder's "Required CFs" section (pb.formatItemCFs). Without this, imported
        // CFs end up in selectedCFs/scoreOverrides only, which doesn't render them anywhere
        // in the UI (grouped CFs need an enabled group; ungrouped CFs need formatItemCFs).
        // Setting formatItemCFs also activates the "Fmt" pill on CFs that happen to be in
        // a TRaSH group, which is the existing convention for moving a grouped CF into
        // formatItems — consistent with manual Fmt clicks.
        this.pb.selectedCFs = {};
        this.pb.scoreOverrides = {};
        this.pb.requiredCFs = {};
        this.pb.formatItemCFs = {};
        for (const [trashId, score] of Object.entries(prof.formatItems || {})) {
          this.pb.selectedCFs[trashId] = true;
          this.pb.scoreOverrides[trashId] = score;
          this.pb.formatItemCFs[trashId] = true;
        }
        this.pb.formatItemCFs = { ...this.pb.formatItemCFs };
        this.pbScoreSetChanged();
        // Notify about unmapped CFs
        if (data.unmapped && data.unmapped.length > 0) {
          this.showToast(`Profile loaded. ${data.unmapped.length} CF(s) could not be mapped to TRaSH IDs:\n\n${data.unmapped.join('\n')}`, 'error', 8000);
        }
      } catch (e) {
        this.showToast('Error loading profile: ' + e.message, 'error', 8000);
      } finally {
        this.pbInstanceImportLoading = false;
      }
    },

    _pbSaveDefaults() {
      try {
        localStorage.setItem('clonarr-pb-defaults', JSON.stringify({
          variantGoldenRule: this.pb.variantGoldenRule,
          variantMisc: this.pb.variantMisc,
          qualityPresetId: this.pb.qualityPresetId,
          scoreSet: this.pb.scoreSet,
          trashScoreSet: this.pb.trashScoreSet,
        }));
      } catch (e) {}
    },

    _pbLoadDefaults() {
      try {
        const raw = localStorage.getItem('clonarr-pb-defaults');
        return raw ? JSON.parse(raw) : {};
      } catch (e) { return {}; }
    },

    async saveCustomProfile() {
      this.pbSaving = true;
      try {
        // Check for duplicate name (only when creating, not editing)
        if (!this.pb.editId) {
          const existing = (this.importedProfiles[this.pb.appType] || []).find(
            p => p.name.toLowerCase() === this.pb.name.trim().toLowerCase()
          );
          if (existing) {
            // Find next available suffix
            const baseName = this.pb.name.trim();
            let suffix = 2;
            let newName = baseName + ' (' + suffix + ')';
            while ((this.importedProfiles[this.pb.appType] || []).some(
              p => p.name.toLowerCase() === newName.toLowerCase()
            )) {
              suffix++;
              newName = baseName + ' (' + suffix + ')';
            }
            await new Promise((resolve, reject) => {
              this.confirmModal = {
                show: true,
                title: 'Profile Name Already Exists',
                message: `A profile named "${baseName}" already exists.\n\nThe new profile will be saved as "${newName}".`,
                onConfirm: resolve,
                onCancel: reject
              };
            }).catch(() => { this.pbSaving = false; throw new Error('cancelled'); });
            this.pb.name = newName;
          }
        }

        // Build formatItems, formatComments, and formatGroups from selected CFs
        const formatItems = {};
        const formatComments = {};
        const formatGroups = {};
        const requiredCFs = [];
        // Build CF → group name lookup from pbCategories
        const cfGroupLookup = {};
        for (const cat of this.pbCategories) {
          for (const g of cat.groups) {
            for (const cf of (g.cfs || [])) {
              cfGroupLookup[cf.trashId] = g.name;
            }
          }
        }
        // Build set of CFs that should be in FormatItems for sync:
        // 1. All formatItemCFs (mandatory)
        // 2. CFs from enabled groups
        const syncSet = new Set(Object.keys(this.pb.formatItemCFs || {}));
        const enabledGroupIds = new Set(Object.keys(this.pb.enabledGroups || {}));
        for (const cat of this.pbCategories) {
          for (const g of cat.groups) {
            if (g.groupTrashId && enabledGroupIds.has(g.groupTrashId)) {
              for (const cf of g.cfs) syncSet.add(cf.trashId);
            }
          }
        }
        for (const trashId of syncSet) {
          const cf = this._pbFindCF(trashId);
          const score = this.pb.scoreOverrides[trashId] ?? cf?.trashScores?.[this.pb.scoreSet] ?? cf?.trashScores?.['default'] ?? 0;
          formatItems[trashId] = score;
          if (cf) formatComments[trashId] = cf.name;
          if (cfGroupLookup[trashId]) formatGroups[trashId] = cfGroupLookup[trashId];
        }

        // Use cached quality items from preset selection (stored when preset is applied)
        let qualities = this.pb.qualityItems || [];
        if (qualities.length === 0) {
          // Try fetching from quality preset if set
          if (this.pb.qualityPreset) {
            try {
              const r = await fetch(`/api/trash/${this.pb.appType}/profiles/${this.pb.qualityPreset}`);
              if (r.ok) {
                const detail = await r.json();
                qualities = detail.profile?.items || [];
              }
            } catch (e) { /* ignore fetch errors */ }
          }
          // Still empty — try from preset dropdown
          if (qualities.length === 0 && this.pb.qualityPresetId) {
            const preset = this.pbQualityPresets.find(p => p.id === this.pb.qualityPresetId);
            if (preset) qualities = preset.items || [];
          }
        }
        if (qualities.length === 0) {
          await new Promise((resolve, reject) => {
            this.confirmModal = {
              show: true,
              title: 'No Quality Items',
              message: 'No quality items configured. The profile will not work in Radarr/Sonarr without quality items.\n\nSelect a Quality Preset to include them.',
              onConfirm: resolve,
              onCancel: reject
            };
          }).catch(() => { this.pbSaving = false; throw new Error('cancelled'); });
        }

        const profile = {
          name: this.pb.name.trim(),
          appType: this.pb.appType,
          source: 'custom',
          scoreSet: this.pb.scoreSet !== 'default' ? this.pb.scoreSet : '',
          upgradeAllowed: this.pb.upgradeAllowed,
          cutoff: this.pb.cutoff,
          cutoffScore: this.pb.cutoffScore,
          minFormatScore: this.pb.minFormatScore,
          minUpgradeFormatScore: this.pb.minUpgradeFormatScore,
          language: this.pb.appType === 'radarr' ? this.pb.language : undefined,
          qualities: qualities,
          formatItems: formatItems,
          formatComments: formatComments,
          formatGroups: Object.keys(formatGroups).length > 0 ? formatGroups : undefined,
          requiredCFs: requiredCFs,
          defaultOnCFs: Object.keys(this.pb.defaultOnCFs || {}).filter(k => this.pb.defaultOnCFs[k] && this.pb.selectedCFs[k]),
          baselineCFs: this.pb.baselineCFs?.length ? this.pb.baselineCFs : undefined,
          coreCFIds: this.pb.coreCFIds?.length ? this.pb.coreCFIds : undefined,
          formatItemsOrder: this.pb.formatItemsOrder?.length ? this.pb.formatItemsOrder : undefined,
          // Builder state (preserved for edit)
          formatItemCFs: Object.keys(this.pb.formatItemCFs || {}).length > 0 ? this.pb.formatItemCFs : undefined,
          enabledGroups: Object.keys(this.pb.enabledGroups || {}).length > 0 ? this.pb.enabledGroups : undefined,
          cfStateOverrides: Object.keys(this.pb.cfStateOverrides || {}).length > 0 ? this.pb.cfStateOverrides : undefined,
          variantGoldenRule: this.pb.variantGoldenRule || undefined,
          goldenRuleDefault: this.pb.goldenRuleDefault || undefined,
          variantMisc: this.pb.variantMisc || undefined,
          qualityPresetId: this.pb.qualityPresetId || undefined,
          // Dev mode
          trashProfileId: this.pb.trashProfileId || undefined,
          trashScoreSet: this.pb.trashScoreSet || undefined,
          trashDescription: this.pb.trashDescription || undefined,
          groupNum: this.pb.groupNum || undefined,
        };

        let url, method;
        if (this.pb.editId) {
          url = `/api/custom-profiles/${this.pb.editId}`;
          method = 'PUT';
        } else {
          url = '/api/custom-profiles';
          method = 'POST';
        }

        const r = await fetch(url, {
          method,
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(profile),
        });

        if (r.ok) {
          this._pbSaveDefaults();
          this.profileBuilder = false;
          // Return to previous subtab if we came from resync Edit
          if (this._resyncReturnSubTab) {
            this.currentSection = 'profiles';
            this._resyncReturnSubTab = null;
          }
          this.loadImportedProfiles(this.pb.appType);
        } else {
          const data = await r.json();
          this.showToast(data.error || 'Failed to save profile', 'error', 8000);
        }
      } catch (e) {
        if (e.message !== 'cancelled') this.showToast('Error: ' + e.message, 'error', 8000);
      }
      this.pbSaving = false;
    },

  },
};
