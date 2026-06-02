// trash-profile-discovery.js — drives the v3 TRaSH Profiles browse tab.
// Renders rich auto-derived descriptions (axes + cf-groups + markdown notes
// + workflow logic) so users can pick a profile without entering the editor.
//
// Data flow: GET /api/trash/{app}/profiles/descriptions returns
// ProfileDescription[] (see internal/core/trash_profile_describer.go for the
// schema). We cache per-app, re-fetch on TRaSH pull-complete.
//
// View modes: 'grid' (default — 3-col auto-fill, all cards expanded) and
// 'list' (vertical compact rows, click-to-expand). Filter chips narrow
// the set by HDR/audio/in-use/etc; search narrows by name + tagline.

export default {
  state: {
    // app → ProfileDescription[]
    trashProfileDescriptions: { radarr: [], sonarr: [] },
    // app → bool (in-flight request)
    tpdLoading: { radarr: false, sonarr: false },
    // 'grid' | 'list' (per-browser localStorage)
    tpdView: localStorage.getItem('clonarr_tpdView') || 'grid',
    // Multi-select feature filters — array of any combination of
    // 'hd' | 'uhd' | 'hdr' | 'lossless' | 'in-use'. Empty means "All".
    // Within the group selections are OR'd (HDR + Lossless → either),
    // and the feature group AND's with the category group.
    tpdFeatureFilters: [],
    // Multi-select category filters — array of group names from
    // tpdCategoryList (Standard / SQP / Anime / …). Empty means "All".
    // Selections within the group are OR'd.
    tpdCategoryFilters: [],
    tpdSearch: '',
    // trash_id → bool (which cards are expanded in list view)
    tpdOpenIds: {},
  },

  methods: {
    async loadTrashProfileDescriptions(appType) {
      if (!appType) return;
      if (this.tpdLoading[appType]) return;
      this.tpdLoading = { ...this.tpdLoading, [appType]: true };
      try {
        const r = await fetch(`/api/trash/${appType}/profiles/descriptions`);
        if (!r.ok) {
          // 4xx/5xx — clear cached list so empty-state renders
          this.trashProfileDescriptions = { ...this.trashProfileDescriptions, [appType]: [] };
          return;
        }
        const data = await r.json();
        this.trashProfileDescriptions = { ...this.trashProfileDescriptions, [appType]: data || [] };
      } catch (e) {
        console.error('loadTrashProfileDescriptions:', e);
        this.trashProfileDescriptions = { ...this.trashProfileDescriptions, [appType]: [] };
      } finally {
        this.tpdLoading = { ...this.tpdLoading, [appType]: false };
      }
    },

    tpdSetView(view) {
      if (view !== 'grid' && view !== 'list') return;
      this.tpdView = view;
      localStorage.setItem('clonarr_tpdView', view);
    },

    // Toggle a feature pill in or out of the active set. Clicking an
    // active pill removes it; clicking the "All" pill clears the entire
    // set. Same shape for the two groups, kept as separate helpers so
    // template binding is symmetric with isActive checks.
    tpdToggleFeature(filter) {
      const i = this.tpdFeatureFilters.indexOf(filter);
      if (i >= 0) this.tpdFeatureFilters.splice(i, 1);
      else this.tpdFeatureFilters.push(filter);
    },
    tpdClearFeatures() {
      this.tpdFeatureFilters = [];
    },
    tpdIsFeatureActive(filter) {
      return this.tpdFeatureFilters.includes(filter);
    },

    tpdToggleCategory(cat) {
      const i = this.tpdCategoryFilters.indexOf(cat);
      if (i >= 0) this.tpdCategoryFilters.splice(i, 1);
      else this.tpdCategoryFilters.push(cat);
    },
    tpdClearCategories() {
      this.tpdCategoryFilters = [];
    },
    tpdIsCategoryActive(cat) {
      return this.tpdCategoryFilters.includes(cat);
    },

    // tpdCategoryList returns category names in the same order they
    // appear as section headers in tpdGrouped — by min `group` int
    // ascending, alpha tiebreak. Drives the category-filter pill row
    // so its order matches the on-page grouping (Standard first,
    // SQP next, then Anime / French / German etc).
    tpdCategoryList(appType) {
      const meta = this.trashProfiles[appType] || [];
      const groups = {};
      for (const p of meta) {
        const name = p.groupName || 'Other';
        const gnum = typeof p.group === 'number' ? p.group : Infinity;
        if (!groups[name] || gnum < groups[name].minGroup) {
          groups[name] = { name, minGroup: gnum };
        }
      }
      return Object.values(groups).sort((a, b) => {
        if (a.minGroup !== b.minGroup) return a.minGroup - b.minGroup;
        return a.name.localeCompare(b.name);
      }).map(g => g.name);
    },

    tpdToggleOpen(trashId) {
      this.tpdOpenIds = { ...this.tpdOpenIds, [trashId]: !this.tpdOpenIds[trashId] };
    },

    tpdIsOpen(trashId) {
      return !!this.tpdOpenIds[trashId];
    },

    // True when at least one profile detail body is currently expanded.
    // Drives the Expand-all / Collapse-all toggle label in list view —
    // the same button flips between the two actions based on this state.
    tpdAnyOpen() {
      return Object.values(this.tpdOpenIds).some(v => v);
    },

    // Open every profile in the current filtered set. Operates only on
    // tpdFiltered so a filter narrows the bulk-expand scope (e.g. filter
    // to UHD then Expand all → just those expand, not all 30).
    tpdExpandAll(appType) {
      const next = { ...this.tpdOpenIds };
      for (const d of this.tpdFiltered(appType)) next[d.trashId] = true;
      this.tpdOpenIds = next;
    },

    // Reset all open states. Cheap one-liner; preserved as a named helper
    // so the template stays declarative.
    tpdCollapseAll() {
      this.tpdOpenIds = {};
    },

    // True when at least one auto-sync rule references this trash_id on any
    // instance of the same app type. Drives the "in use" badge.
    tpdProfileInUse(appType, trashId) {
      const rules = this.autoSyncRules || [];
      const instIds = new Set(this.instancesOfType(appType).map(i => i.id));
      return rules.some(r => instIds.has(r.instanceId) && r.trashProfileId === trashId);
    },

    // Multi-line tooltip body for the in-use badge — count line + bullet
    // list, same shape whether 1 or N instances so the badge itself stays
    // consistent ("in use" always) and details live in the tooltip.
    // Rendered via white-space: pre-line on the tooltip stylesheet.
    // (Per-section instance picker would make this redundant — open
    // redesign item noted in CLAUDE.md.)
    tpdInUseTooltip(appType, trashId) {
      const rules = this.autoSyncRules || [];
      const insts = this.instancesOfType(appType);
      const byId = new Map(insts.map(i => [i.id, i.name]));
      const names = rules
        .filter(r => r.trashProfileId === trashId && byId.has(r.instanceId))
        // Collapse stray whitespace so an instance name with embedded
        // newlines/tabs can't break the bullet-list layout.
        .map(r => (byId.get(r.instanceId) || '').replace(/\s+/g, ' ').trim());
      if (names.length === 0) return '';
      const header = names.length === 1
        ? 'Used in 1 instance:'
        : `Used in ${names.length} instances:`;
      return header + '\n' + names.map(n => '• ' + n).join('\n');
    },

    // Apply search + category filter + feature filter to the description
    // list. All three combine with AND (a profile must match all active
    // filters to appear). Returns a new array — original state isn't
    // mutated.
    tpdFiltered(appType) {
      const all = this.trashProfileDescriptions[appType] || [];
      const q = (this.tpdSearch || '').toLowerCase().trim();
      const cats = this.tpdCategoryFilters || [];
      const feats = this.tpdFeatureFilters || [];
      // Category lookup via the classic trashProfiles metadata — same
      // source tpdGrouped uses, so a profile matches its category-pill
      // exactly when its section heading on the page reads the same.
      const meta = this.trashProfiles[appType] || [];
      const metaById = new Map();
      for (const p of meta) metaById.set(p.trashId, p);

      const matchesFeature = (d, f) => {
        switch (f) {
          case 'hd':       return (d.axes?.resolution || '').includes('1080p') && !(d.axes?.resolution || '').includes('2160p');
          case 'uhd':      return (d.axes?.resolution || '').includes('2160p');
          case 'hdr':      return !!d.axes?.hdr?.scored;
          case 'lossless': return !!d.axes?.audio?.scored;
          case 'in-use':   return this.tpdProfileInUse(appType, d.trashId);
        }
        return false;
      };

      return all.filter(d => {
        if (q) {
          // Build a wider haystack so words from the description match too.
          // Includes name + tagline + every Highlights bullet + axes prose
          // (resolution, sources list, HDR opt-ins, average size) + the
          // disclaimer prose. Matches user intent: searching "atmos" finds
          // the lossless-audio bullet; "remux" matches the source label
          // even when not in the profile name.
          const hayParts = [
            d.name,
            d.tagline || '',
            (d.highlights || []).join(' '),
            d.axes?.resolution || '',
            (d.axes?.sources || []).join(' '),
            (d.axes?.hdr?.optIns || []).join(' '),
            d.axes?.avgSize || '',
            d.disclaimer?.before || '',
            d.disclaimer?.linkText || '',
            d.disclaimer?.after || '',
          ];
          const hay = hayParts.join(' ').toLowerCase();
          if (!hay.includes(q)) return false;
        }
        // Category — empty array means "All". Multiple selections OR'd
        // (Standard + SQP → either group qualifies).
        if (cats.length > 0) {
          const m = metaById.get(d.trashId);
          const groupName = (m && m.groupName) || 'Other';
          if (!cats.includes(groupName)) return false;
        }
        // Features — empty array means "All". Multiple selections OR'd
        // (HDR + Lossless → either feature qualifies). To require all
        // selected features at once, swap .some → .every; OR was chosen
        // because most chip-grids in this app behave additively.
        if (feats.length > 0) {
          if (!feats.some(f => matchesFeature(d, f))) return false;
        }
        return true;
      });
    },

    // Group filtered profiles by their groupName (Standard, SQP, Anime, …).
    // Returns [{ name, profiles[] }] sorted by the same rule as the classic
    // groupedProfiles() helper: by min `group` integer ascending (TRaSH's
    // own ordering hint), then alpha for ties. Profiles within each group
    // sorted alphabetically so display order is stable regardless of which
    // order the backend returned them in.
    tpdGrouped(appType) {
      const filtered = this.tpdFiltered(appType);
      // Lookup table from the classic profile list for groupName + group int
      const meta = this.trashProfiles[appType] || [];
      const metaById = new Map();
      for (const p of meta) metaById.set(p.trashId, p);

      const groups = {};
      for (const d of filtered) {
        const m = metaById.get(d.trashId);
        const groupName = (m && m.groupName) || 'Other';
        const groupInt = (m && typeof m.group === 'number') ? m.group : Infinity;
        if (!groups[groupName]) {
          groups[groupName] = { name: groupName, profiles: [], minGroup: Infinity };
        }
        groups[groupName].profiles.push(d);
        if (groupInt < groups[groupName].minGroup) groups[groupName].minGroup = groupInt;
      }
      for (const g of Object.values(groups)) {
        g.profiles.sort((a, b) => a.name.localeCompare(b.name));
      }
      return Object.values(groups).sort((a, b) => {
        if (a.minGroup !== b.minGroup) return a.minGroup - b.minGroup;
        return a.name.localeCompare(b.name);
      });
    },

    // --- Pill-rendering helpers ---
    // Principle: only ASSERT positive features. Don't render pills for
    // missing features ("No HDR", "Lossy audio") — they were called out as
    // visual noise. A card with fewer pills correctly signals fewer
    // differentiators.

    // tpdAudioPillText returns the user-outcome audio label across three
    // states the backend describer now reports:
    //   scored → "Lossless audio"      (group default-on, user gets it by default)
    //   optIn  → "Lossless available"  (group bundled but default-off, user opts in)
    //   else   → "Lossy audio"
    // Template uses different pill classes (.aud for lossless = subtle
    // green, neutral for lossy / available) so the three states don't
    // visually compete. "Lossless available" sits with the neutral
    // styling — it advertises capability without claiming the outcome.
    tpdAudioPillText(d) {
      if (d.axes?.audio?.scored) return 'Lossless audio';
      if (d.axes?.audio?.optIn)  return 'Lossless available';
      return 'Lossy audio';
    },

    // tpdHDRPillText returns the SHORT HDR pill label — full opt-in
    // enumeration goes in the Highlights bullet list, not on the pill
    // (where long strings break the visual rhythm). Just "HDR" when no
    // variants are available; "HDR · DV available" when at least one
    // Dolby Vision opt-in exists (DV being the most "brag-worthy"
    // optional, more than HDR10+ for most users).
    tpdHDRPillText(d) {
      const hdr = d.axes?.hdr;
      if (!hdr?.scored) return '';
      if (hdr.optIns && hdr.optIns.some(o => o.startsWith('DV'))) {
        return 'HDR · DV available';
      }
      return 'HDR';
    },

    // tpdSourceLabel reduces the raw items[] source list (which may have 6+
    // entries including HDTV / DVD / fallbacks that nobody cares about) to a
    // single canonical label per profile family.
    //
    // Derived from the SOURCES list (not cutoff) — cutoff naming differs
    // between standard ("Remux-1080p"), anime ("Remux 1080p", space), and
    // Sonarr ("WEB 1080p") profiles. The sources list normalises via
    // extractSource() in the backend so the same set of labels appears
    // regardless of which profile family we're rendering. Priority order:
    //   UHD Remux  > Bluray Remux  > UHD Bluray  > Bluray  > WEB-DL only
    // and "+ WEB" suffix added whenever WEB sources are also accepted, so
    // a Remux profile that also accepts WEB-DL reads as "Bluray Remux + WEB"
    // — matches how users think about the profile.
    tpdSourceLabel(d) {
      const srcs = d.axes?.sources || [];
      const set = new Set(srcs);
      const hasWeb = set.has('WEB-DL') || set.has('WEBRip');
      const webSuffix = hasWeb ? ' + WEB' : '';
      if (set.has('UHD Bluray Remux')) return 'UHD Remux' + webSuffix;
      if (set.has('Bluray Remux'))     return 'Bluray Remux' + webSuffix;
      if (set.has('UHD Bluray'))       return 'UHD Bluray' + webSuffix;
      if (set.has('Bluray'))           return 'Bluray' + webSuffix;
      if (hasWeb)                       return 'WEB-DL';
      // Truly unusual profile — just show first 2 sources so we don't lie
      return srcs.slice(0, 2).join(' + ') || 'Mixed sources';
    },

    // tpdResolutionLabel returns just the primary resolution token (1080p /
    // 2160p / 720p). Strips the verbose fallback chain ("720p, 576p, 480p
    // fallback") that exposes raw items[] data nobody cares about for
    // card-level scanning.
    tpdResolutionLabel(d) {
      const raw = d.axes?.resolution || '';
      const m = raw.match(/^(\d+p)/);
      return m ? m[1] : raw;
    },

    // Minimal card mode — true for SQP profiles ([SQP] prefix) and the
    // Base Profile (TRaSH's internal test profile). These profiles carry
    // hand-written disclaimer copy that explains what they are; our
    // auto-derived tagline + "What you get" highlights become redundant
    // noise on top of that. Templates check this to suppress the tagline
    // (both list-view and card-body) and the highlights section, leaving
    // just the disclaimer + pills + action buttons.
    tpdIsMinimalCard(d) {
      return !!(d?.name && (d.name.startsWith('[SQP]') || d.name === 'Base Profile'));
    },

    // Returns the auto-derived ProfileDescription for the profile currently
    // open in the editor overlay, or null. Reused by the Sync Preview's
    // profile-info panel so the editor opens with the same rich axes /
    // highlights / disclaimer copy as the TPD profile card.
    spProfileDescription() {
      const appType = this.profileDetail?.instance?.type;
      const trashId = this.profileDetail?.profile?.trashId;
      if (!appType || !trashId) return null;
      const list = this.trashProfileDescriptions?.[appType] || [];
      return list.find(d => d.trashId === trashId) || null;
    },

    // Parses TRaSH cf-group names like "[Streaming Services] General" and
    // returns a sectioned structure for the Sync Preview sub-nav:
    //   [{ section: 'STREAMING SERVICES', items: [{ ...group, _shortName: 'General' }, ...] }, ...]
    // Groups without a bracket prefix fall into "OTHER". Section order is
    // preserved from the input list (TRaSH sorts groups by its own `group`
    // field, so categories come out in the same order as TRaSH renders
    // them on its website).
    spGroupBySection(groups) {
      const re = /^\[([^\]]+)\]\s*(.*)$/;
      const sections = new Map();
      for (const g of (groups || [])) {
        const m = (g.name || '').match(re);
        const section = m ? m[1].trim().toUpperCase() : 'OTHER';
        const shortName = m ? (m[2].trim() || g.name) : g.name;
        if (!sections.has(section)) sections.set(section, []);
        sections.get(section).push({ ...g, _shortName: shortName });
      }
      return Array.from(sections, ([section, items]) => ({ section, items }));
    },

    // Sidebar section expand/collapse for the Profile editor sub-nav.
    // Three sources, in priority order:
    //   1. Explicit override from chevron click (persisted localStorage)
    //   2. Transient focus from label click (spExpandedSection) — when
    //      set, ONLY that section is expanded; auto-expand from active
    //      group is suppressed. Lets the user "look at section X"
    //      without main pane following along, and ensures the
    //      currently-focused section visibly collapses when another
    //      label is clicked even if it was auto-expanded before.
    //   3. Default: expand the section that contains spActiveGroup.
    isSpSidebarSectionExpanded(sectionName, sectionItems) {
      const explicit = this.spSidebarExpanded?.[sectionName];
      if (explicit !== undefined) return explicit;
      if (this.spExpandedSection !== null && this.spExpandedSection !== undefined) {
        return this.spExpandedSection === sectionName;
      }
      if (this.spActiveGroup && this.spActiveGroup !== '__required') {
        return (sectionItems || []).some(g => g.name === this.spActiveGroup);
      }
      return false;
    },
    // Chevron click — toggles persistent expand state. Survives across
    // sessions via localStorage; overrides label-click transient and
    // auto-expand by being checked first.
    toggleSpSidebarSection(sectionName, sectionItems) {
      const next = !this.isSpSidebarSectionExpanded(sectionName, sectionItems);
      const updated = { ...(this.spSidebarExpanded || {}), [sectionName]: next };
      this.spSidebarExpanded = updated;
      try { window.localStorage.setItem('sp-sidebar-expanded', JSON.stringify(updated)); } catch (_) {}
    },
    // Label click — toggles transient focus on a section. Clears any
    // explicit chevron-set override on the same section so the
    // transient can actually take effect (otherwise a previously-
    // collapsed section stays collapsed because explicit=false wins
    // over transient in isSpSidebarSectionExpanded). Doesn't change
    // spActiveGroup, so main pane stays on whatever group was
    // selected. Clears via spSelectGroup when user picks a specific
    // group from the expanded list.
    //
    // Single-child shortcut: when the section has exactly one group,
    // click navigates directly to that group instead of just
    // expanding (which would force a second click on the single
    // child). Saves the wasted click for sections like Unwanted
    // (only Unwanted Formats), Custom (one user-cat), etc.
    spExpandSectionTransient(sectionName, sectionItems) {
      if (Array.isArray(sectionItems) && sectionItems.length === 1 && sectionItems[0]?.name) {
        this.spSelectGroup(sectionItems[0].name);
        return;
      }
      const next = (this.spExpandedSection === sectionName) ? null : sectionName;
      this.spExpandedSection = next;
      // Clear chevron-set explicit override on this section so the
      // transient focus controls visibility.
      if (this.spSidebarExpanded && Object.prototype.hasOwnProperty.call(this.spSidebarExpanded, sectionName)) {
        const updated = { ...this.spSidebarExpanded };
        delete updated[sectionName];
        this.spSidebarExpanded = updated;
        try { window.localStorage.setItem('sp-sidebar-expanded', JSON.stringify(updated)); } catch (_) {}
      }
    },
    // Group selection — sets active group + clears transient expand
    // so the previously label-expanded section collapses. Used by
    // every group button (Required CFs + per-group + Additional CF).
    spSelectGroup(name) {
      this.spActiveGroup = name;
      this.spExpandedSection = null;
    },

    // Effective active-CF count for a group, mirroring the action-row
    // counter logic but resolving group-state via selectedOptionalCFs +
    // group.defaultEnabled fallback. Used by the sub-nav to render
    // "X / total" so the user can scan which groups are in use without
    // clicking through each one.
    //
    // Rules (same as the action-row counter):
    //  - Group with required and/or default-on CFs (hasToggle): when group
    //    is OFF, count is 0; when ON, count = required + effectively-on
    //    optional CFs.
    //  - Group without auto-includes (opt-in only — no required, no
    //    default-on): group-toggle is irrelevant; count purely by per-CF
    //    effective state.
    spGroupActiveCount(g) {
      const cfs = g.cfs || [];
      const hasToggle = cfs.some(cf => cf.required)
        || (!g.exclusive && cfs.some(cf => cf.default));
      const sel = this.selectedOptionalCFs || {};
      const grpKey = '__grp_' + g.name;
      const grpOn = sel[grpKey] !== undefined ? sel[grpKey] : g.defaultEnabled;
      const cfOn = (cf) => sel[cf.trashId] === undefined ? !!cf.default : !!sel[cf.trashId];
      // Phase 2c: a required CF can be individually excluded via lock-icon
      // click (sel[id] === false). Don't count it as active in that case
      // — the group's sub-nav inactive-dim should lift only when at least
      // one CF actually syncs.
      const reqOn = (cf) => cf.required && sel[cf.trashId] !== false;
      if (!hasToggle) {
        return cfs.filter(cf => cfOn(cf)).length;
      }
      if (!grpOn) return 0;
      return cfs.filter(cf => reqOn(cf) || cfOn(cf)).length;
    },

    // Active-count for the Additional CF tab sub-nav. Counts only
    // CFs that are user-opted-in among the group's UNIQUE CFs (the
    // ones spAdditionalGroupCFs surfaces — already filtered to
    // exclude those active via the profile-default path).
    //
    // Why this is separate from spGroupActiveCount: shared CFs
    // between an Additional group (e.g. "Unwanted French") and a
    // profile-default group (e.g. "Unwanted Formats") will have
    // sel[id] = true via the profile group. The generic active
    // count would treat the Additional group as "active" even
    // though the user hasn't actually opted into anything UNIQUE
    // to it — giving the false impression that the sub-nav item is
    // engaged when none of its visible CFs are checked.
    spAdditionalGroupActiveCount(g) {
      const sel = this.selectedOptionalCFs || {};
      return this.spAdditionalGroupCFs(g).filter(cf => !!sel[cf.trashId]).length;
    },

    // === Sync Preview "Profile overview" tab helpers ===
    // Read-only summary of what's currently configured on the profile —
    // built so the user can see the whole spec at a glance without
    // navigating into each group. Edit-affordances layer on top later
    // once the Customize button wires basics-editing.

    // The 6 basics fields with effective value + whether the user has
    // overridden them. Modified detection compares values directly
    // against the profile default — matches pdGeneralChangeCount
    // (profiles.js:2963-2968). The .enabled flag on pdOverrides is
    // only set when loading a saved rule; live edits don't flip it,
    // so a flag-based check would miss in-flight changes.
    // Language skipped for Sonarr (no profile-level language field).
    spOverviewBasics() {
      const pd = this.profileDetail?.detail?.profile;
      const ov = this.pdOverrides || {};
      if (!pd) return [];
      const isRadarr = (this.profileDetail?.instance?.type || this.activeAppType) === 'radarr';
      const rows = [];
      if (isRadarr) {
        const langDefault = pd.language?.name || pd.language || 'Original';
        const langValue = ov.language?.value || langDefault;
        rows.push({
          label: 'Language',
          value: langValue,
          modified: langValue !== langDefault,
          defaultValue: langDefault,
        });
      }
      const minDefault = pd.minFormatScore ?? 0;
      const minValue = ov.minFormatScore?.value ?? minDefault;
      const minUpDefault = pd.minUpgradeFormatScore ?? 1;
      const minUpValue = ov.minUpgradeFormatScore?.value ?? minUpDefault;
      const cutScoreDefault = pd.cutoffFormatScore ?? 10000;
      const cutScoreValue = ov.cutoffFormatScore?.value ?? cutScoreDefault;
      const upDefault = !!pd.upgradeAllowed;
      const upValue = ov.upgradeAllowed?.value === true || ov.upgradeAllowed?.value === 'true' || (ov.upgradeAllowed?.value === undefined && upDefault);
      rows.push(
        ...[
        {
          label: 'Min score',
          value: minValue,
          modified: Number(minValue) !== Number(minDefault),
          defaultValue: minDefault,
        },
        {
          label: 'Min upgrade',
          value: minUpValue,
          modified: Number(minUpValue) !== Number(minUpDefault),
          defaultValue: minUpDefault,
        },
        {
          label: 'Cutoff score',
          value: cutScoreValue,
          modified: Number(cutScoreValue) !== Number(cutScoreDefault),
          defaultValue: cutScoreDefault,
        },
        {
          label: 'Upgrades allowed',
          value: upValue ? 'On' : 'Off',
          modified: upValue !== upDefault,
          defaultValue: upDefault ? 'On' : 'Off',
        },
        {
          label: 'Cutoff quality',
          value: this.pdOverrides?.cutoffQuality || pd.cutoff || '—',
          // pdInitOverrides seeds cutoffQuality with the profile's default
          // (profiles.js:3523 / :3417) so it's always non-empty. A truthy
          // check would falsely flag it as modified — compare against the
          // default like profiles.js:1601 does in its canonical "did it
          // actually change?" check.
          modified: !!this.pdOverrides?.cutoffQuality
                 && this.pdOverrides.cutoffQuality !== pd.cutoff,
          defaultValue: pd.cutoff || '—',
        },
      ]);
      return rows;
    },

    // CFs grouped by source (Required first, then by in-profile group,
    // then by Additional CF group). Each section carries a `kind` flag
    // so sub-nav filter views can pick a subset:
    //   'required'   — formatItemNames (profile-level required CFs)
    //   'in-profile' — TRaSH groups inside profile.quality_profiles.include
    //   'additional' — extraCFGroups (opt-in pool outside profile)
    // Each CF carries `_isRequired` + a `_score` object with effective +
    // original + isOverridden, so the row can render strikethrough on
    // the original when the user has overridden a score (customizations
    // shown inline, not in a separate card).
    // De-dupes by trashId in case the same CF lives in multiple groups.
    // skipSort skips the per-section sort applied at the end. Used by
    // spOverviewFlatCFs which re-sorts across the flattened list — the
    // per-section sort would otherwise be wasted work.
    spOverviewEnabledCFs(skipSort) {
      if (!this.profileDetail) return [];
      const sel = this.selectedOptionalCFs || {};
      const overrides = this.cfScoreOverrides || {};
      // Resolve TRaSH default score per CF — formatItems and trashGroups
      // carry a pre-resolved cf.score, but extraCFGroups (Additional CFs)
      // come from /api/trash/{type}/all-cfs which only sends the raw
      // trashScores map (keyed by profile context). resolveCFDefaultScore
      // walks formatItems → trashGroups → extraCFAllCFs[trashScores] in
      // that order, picking the right score for the active scoreCtx.
      // extraCFs: user-saved scores for opted-in Additional CFs
      // (out-of-profile). Restored separately from cfScoreOverrides
      // (which is reserved for in-profile overrides). Read it first so
      // the Profile overview's Additional CF section + flat list show
      // the user's saved score instead of the TRaSH-context default.
      const extras = this.extraCFs || {};
      const scoreInfo = (cf) => {
        let orig = cf.score;
        if (orig === undefined || orig === null) {
          const resolved = this.resolveCFDefaultScore?.(cf.trashId);
          orig = (typeof resolved === 'number') ? resolved : null;
        }
        // Score priority for "effective": extras (out-of-profile saved
        // score) → cfScoreOverrides (in-profile override) → original.
        // isOverridden flags any deviation from orig, regardless of
        // which map the override came from.
        let effective = extras[cf.trashId];
        if (effective === undefined) effective = overrides[cf.trashId];
        if (effective !== undefined && effective !== orig) {
          return { effective, original: orig, isOverridden: true };
        }
        return { effective: orig, original: orig, isOverridden: false };
      };
      const seen = new Set();
      const sections = [];
      const pushSection = (label, category, cfs, kind) => {
        const filtered = cfs.filter(cf => {
          if (seen.has(cf.trashId)) return false;
          seen.add(cf.trashId);
          return true;
        });
        if (filtered.length > 0) sections.push({ source: label, sourceCategory: category, cfs: filtered, kind });
      };

      // 1. Required CFs at profile level — formatItemNames.
      // Phase 2c: skip CFs the user excluded via lock-icon click (they
      // won't sync, so showing them as "enabled" would lie).
      pushSection(
        'Required CFs',
        'required',
        (this.profileDetail.detail?.formatItemNames || [])
          .filter(fi => sel[fi.trashId] !== false)
          .map(fi => ({ ...fi, _isRequired: true, _score: scoreInfo(fi) })),
        'required'
      );

      // 2. In-profile trashGroups
      for (const g of (this.profileDetail.detail?.trashGroups || [])) {
        const hasToggle = g.cfs.some(cf => cf.required)
          || (!g.exclusive && g.cfs.some(cf => cf.default));
        const grpKey = '__grp_' + g.name;
        const grpOn = sel[grpKey] !== undefined ? sel[grpKey] : g.defaultEnabled;
        if (hasToggle && !grpOn) continue;
        const m = (g.name || '').match(/^\[([^\]]+)\]\s*(.*)$/);
        const shortName = m ? (m[2].trim() || g.name) : g.name;
        const enabled = [];
        for (const cf of g.cfs) {
          let isOn;
          if (cf.required) {
            // Phase 2c: required CF can be individually excluded.
            isOn = (!hasToggle || grpOn) && sel[cf.trashId] !== false;
          } else {
            const cfOn = sel[cf.trashId] === undefined ? !!cf.default : !!sel[cf.trashId];
            isOn = (!hasToggle || grpOn) && cfOn;
          }
          if (isOn) enabled.push({ ...cf, _isRequired: !!cf.required, _score: scoreInfo(cf) });
        }
        pushSection(shortName, g.category, enabled, 'in-profile');
      }

      // 3. Additional CF groups (opt-in pool outside the profile)
      for (const g of (this.extraCFGroups || [])) {
        const m = (g.name || '').match(/^\[([^\]]+)\]\s*(.*)$/);
        const shortName = m ? (m[2].trim() || g.name) : g.name;
        const enabled = g.cfs
          .filter(cf => !!sel[cf.trashId])
          .map(cf => ({ ...cf, _isRequired: false, _score: scoreInfo(cf) }));
        pushSection(shortName, g.category, enabled, 'additional');
      }

      // Apply user-selected sort within each section. Default keeps the
      // natural source order (formatItems then group order then category).
      const sort = this.spOverviewSort || 'default';
      if (!skipSort && sort !== 'default') {
        const cmp = {
          'name-asc':   (a, b) => (a.name || '').localeCompare(b.name || ''),
          'name-desc':  (a, b) => (b.name || '').localeCompare(a.name || ''),
          'score-desc': (a, b) => (b._score?.effective ?? 0) - (a._score?.effective ?? 0),
          'score-asc':  (a, b) => (a._score?.effective ?? 0) - (b._score?.effective ?? 0),
        }[sort];
        if (cmp) {
          for (const s of sections) s.cfs = [...s.cfs].sort(cmp);
        }
      }

      return sections;
    },

    // Flat CF list for Overview when "Show CF Groups" is off. Flattens
    // every section returned by spOverviewEnabledCFs() into a single
    // list, with each CF carrying its source group name as `fromGroup`
    // (used by the row template's secondary line). `kindFilter` lets
    // the Optional CF / Additional CF cards restrict the flat list to
    // their own subset.
    //
    // Sorting honours spOverviewSort the same way the grouped path
    // does, but applies across the FULL list rather than per-section.
    spOverviewFlatCFs(kindFilter) {
      // skipSort=true: we re-sort across the flat list below, so the
      // per-section sort that spOverviewEnabledCFs normally applies
      // would be wasted work.
      const sections = this.spOverviewEnabledCFs(true);
      const out = [];
      for (const s of sections) {
        if (kindFilter === 'optional-in-profile') {
          if (s.kind !== 'in-profile') continue;
        } else if (kindFilter === 'additional') {
          if (s.kind !== 'additional') continue;
        }
        for (const cf of s.cfs) {
          if (kindFilter === 'optional-in-profile' && cf._isRequired) continue;
          out.push({ ...cf, fromGroup: s.source, sourceCategory: s.sourceCategory });
        }
      }
      const sort = this.spOverviewSort || 'default';
      const cmp = {
        'name-asc':   (a, b) => (a.name || '').localeCompare(b.name || ''),
        'name-desc':  (a, b) => (b.name || '').localeCompare(a.name || ''),
        'score-desc': (a, b) => (b._score?.effective ?? 0) - (a._score?.effective ?? 0),
        'score-asc':  (a, b) => (a._score?.effective ?? 0) - (b._score?.effective ?? 0),
      }[sort];
      if (cmp) out.sort(cmp);
      return out;
    },

    // Column-header click handler — cycles sort direction for the
    // clicked field, drops sort on the other field.
    spToggleOverviewSort(field) {
      if (field === 'name') {
        this.spOverviewSort = (this.spOverviewSort === 'name-asc') ? 'name-desc' : 'name-asc';
      } else if (field === 'score') {
        this.spOverviewSort = (this.spOverviewSort === 'score-desc') ? 'score-asc' : 'score-desc';
      }
    },

    // Persistent setter for the "Show CF Groups" toggle — writes
    // localStorage so the user's pick survives reloads.
    spSetOverviewGroup(value) {
      this.spOverviewGroupCFs = !!value;
      try { window.localStorage.setItem('sp-ov-group-cfs', String(this.spOverviewGroupCFs)); } catch (e) {}
    },

    // Diffs view — everything that diverges from the TRaSH default for
    // this profile. Four buckets in priority order:
    //   1. modifiedBasics — pdOverrides where .enabled === false (legacy
    //      inverse semantics) + cutoffQuality if set.
    //   2. scoreOverrides — CFs with cfScoreOverrides[trashId] !== cf.score.
    //   3. additionalCFs — CFs from extraCFGroups the user has opted into
    //      (outside the profile's default scope entirely).
    //   4. excludedCFs — default-on CFs in still-active groups that the
    //      user has explicitly turned off (selectedOptionalCFs[id] === false).
    //
    // Intentionally NOT included: optional CFs the user has activated
    // within in-profile groups. Those live inside the profile's own
    // structure and aren't considered "external" deviations — they're
    // visible via the "Optional CF" sub-nav view.
    //
    // Each bucket returns rich objects so the template can render the
    // diff with original-vs-current side by side. Computed lazily on
    // each access (Alpine reactivity tracks the dependencies).
    spOverviewDiffs() {
      const out = { modifiedBasics: [], scoreOverrides: [], additionalCFs: [], excludedCFs: [], excludedRequiredCFs: [], qualityItems: [] };
      if (!this.profileDetail) return out;
      // Quality items diffs — leaf-flatten profile defaults vs user's
      // qualityStructure (or legacy qualityOverrides flat map) and
      // emit per-leaf rows for any changed `allowed` flag. Without
      // this, opening Quality Items editor → toggling a quality →
      // Save left zero trace in the Diffs view despite header count
      // reflecting the change.
      const pdQ = this.profileDetail?.detail?.profile?.items || [];
      const flattenLeaves = (items) => {
        const m = new Map();
        for (const it of (items || [])) {
          if (it.items && it.items.length > 0) {
            for (const leaf of it.items) m.set(leaf, !!it.allowed);
          } else {
            const name = it.name || it.quality?.name;
            if (name) m.set(name, !!it.allowed);
          }
        }
        return m;
      };
      const origLeaves = flattenLeaves(pdQ);
      if (this.qualityStructure && this.qualityStructure.length > 0) {
        const curLeaves = flattenLeaves(this.qualityStructure);
        for (const [name, allowed] of curLeaves) {
          const orig = origLeaves.get(name);
          if (orig === undefined) {
            out.qualityItems.push({ name, current: allowed ? 'Allowed' : 'Disallowed', original: '—', allowed });
          } else if (orig !== allowed) {
            out.qualityItems.push({ name, current: allowed ? 'Allowed' : 'Disallowed', original: orig ? 'Allowed' : 'Disallowed', allowed });
          }
        }
        for (const [name, orig] of origLeaves) {
          if (!curLeaves.has(name)) {
            out.qualityItems.push({ name, current: '—', original: orig ? 'Allowed' : 'Disallowed', allowed: false });
          }
        }
      } else if (this.qualityOverrides && Object.keys(this.qualityOverrides).length > 0) {
        for (const [name, allowed] of Object.entries(this.qualityOverrides)) {
          const orig = origLeaves.get(name);
          const a = !!allowed;
          if (orig !== a) {
            out.qualityItems.push({ name, current: a ? 'Allowed' : 'Disallowed', original: orig === undefined ? '—' : (orig ? 'Allowed' : 'Disallowed'), allowed: a });
          }
        }
      }
      const sel = this.selectedOptionalCFs || {};
      const overrides = this.cfScoreOverrides || {};
      const pd = this.profileDetail?.detail?.profile;

      // 1. Modified basics — value-comparison detection (matches
      // pdGeneralChangeCount, profiles.js:2963-2968). The .enabled flag
      // on pdOverrides is only set when loading a saved rule; live
      // edits don't flip it. Language is radarr-only.
      const ov = this.pdOverrides || {};
      const isRadarr = (this.profileDetail?.instance?.type || this.activeAppType) === 'radarr';
      const checks = [
        {
          field: 'minFormatScore', label: 'Min score',
          defaultVal: pd?.minFormatScore ?? 0,
          curVal: ov.minFormatScore?.value ?? (pd?.minFormatScore ?? 0),
          numeric: true,
          display: (v) => v,
        },
        {
          field: 'minUpgradeFormatScore', label: 'Min upgrade',
          defaultVal: pd?.minUpgradeFormatScore ?? 1,
          curVal: ov.minUpgradeFormatScore?.value ?? (pd?.minUpgradeFormatScore ?? 1),
          numeric: true,
          display: (v) => v,
        },
        {
          field: 'cutoffFormatScore', label: 'Cutoff score',
          defaultVal: pd?.cutoffFormatScore ?? 10000,
          curVal: ov.cutoffFormatScore?.value ?? (pd?.cutoffFormatScore ?? 10000),
          numeric: true,
          display: (v) => v,
        },
        {
          field: 'upgradeAllowed', label: 'Upgrades allowed',
          defaultVal: !!pd?.upgradeAllowed,
          curVal: ov.upgradeAllowed?.value === undefined ? !!pd?.upgradeAllowed : !!ov.upgradeAllowed.value,
          numeric: false,
          display: (v) => v ? 'On' : 'Off',
        },
      ];
      if (isRadarr) {
        const langDefault = pd?.language?.name || pd?.language || 'Original';
        checks.push({
          field: 'language', label: 'Language',
          defaultVal: langDefault,
          curVal: ov.language?.value || langDefault,
          numeric: false,
          display: (v) => v,
        });
      }
      for (const c of checks) {
        const changed = c.numeric
          ? Number(c.curVal) !== Number(c.defaultVal)
          : c.curVal !== c.defaultVal;
        if (changed) {
          out.modifiedBasics.push({
            label: c.label,
            field: c.field,
            current: c.display(c.curVal),
            original: c.display(c.defaultVal),
            originalRaw: c.defaultVal,
          });
        }
      }
      // Same pattern as profiles.js:1601 — only flag as modified when
      // the override actually differs from the profile default. Include
      // `field` so the Diffs row's reset-button knows what to restore
      // (pdResetBasic dispatches on the field name).
      if (this.pdOverrides?.cutoffQuality && this.pdOverrides.cutoffQuality !== pd?.cutoff) {
        out.modifiedBasics.push({
          label: 'Cutoff quality',
          field: 'cutoffQuality',
          current: this.pdOverrides.cutoffQuality,
          original: pd?.cutoff || '—',
          originalRaw: pd?.cutoff || '',
        });
      }

      // 2. Score overrides — walk every CF source and pick out diffs
      const seenScoreOv = new Set();
      const collectScoreOv = (cf, fromGroup) => {
        if (seenScoreOv.has(cf.trashId)) return;
        // Custom CFs (user-created or imported) don't have a TRaSH default
        // to deviate from — their score IS the user's score. Skip them in
        // "Score overrides" so the column "Profile default" doesn't read
        // a misleading 0. They surface in "Added Additional CFs" instead.
        if (cf.isCustom || (cf.trashId || '').startsWith('custom:')) return;
        const o = overrides[cf.trashId];
        if (o !== undefined && o !== cf.score) {
          seenScoreOv.add(cf.trashId);
          out.scoreOverrides.push({ trashId: cf.trashId, name: cf.name, current: o, original: cf.score, fromGroup });
        }
      };
      for (const cf of (this.profileDetail.detail?.formatItemNames || [])) collectScoreOv(cf, 'Required CFs');
      for (const g of (this.profileDetail.detail?.trashGroups || [])) {
        const m = (g.name || '').match(/^\[([^\]]+)\]\s*(.*)$/);
        const shortName = m ? (m[2].trim() || g.name) : g.name;
        for (const cf of g.cfs) collectScoreOv(cf, shortName);
      }
      // Note: extraCFGroups are intentionally NOT iterated here.
      // Their cf.score is undefined (only trashScores map is populated),
      // so `o !== cf.score` would falsely flag any override as different
      // from a meaningless undefined baseline and produce "undefined → N"
      // rows. Opted-in Additional CFs with score overrides surface in
      // bucket 3 (Added Additional CFs) below — that row uses
      // resolveCFDefaultScore as fallback and renders the correct value.

      // 3. Additional CFs activated — CFs the user opted into from
      // extraCFGroups that are TRULY outside the profile's default
      // scope. Two filters:
      //   a) Skip groups whose name matches a profile trashGroup name
      //      (Default Unwanted is in profile.trashGroups; the same
      //      group in extraCFGroups isn't an "addition").
      //   b) Skip CFs whose trashId is already in the profile-default
      //      active set (the CF syncs via a profile group, even when
      //      it also appears in a regional variant like French).
      // Without (b), the same CF appears 3-4 times in the Diffs list,
      // once per Additional variant group it overlaps with — pure
      // noise since the user hasn't actually opted into anything.
      const inProfileGroup = new Set();
      const profileActiveCFs = new Set();
      for (const g of (this.profileDetail?.detail?.trashGroups || [])) {
        if (g.name) inProfileGroup.add(g.name);
        const grpKey = '__grp_' + g.name;
        const grpOn = sel[grpKey] !== undefined ? sel[grpKey] : g.defaultEnabled;
        if (!grpOn) continue;
        for (const cf of (g.cfs || [])) {
          // CF is active via this profile group if it's required OR
          // user-opted-in (sel[trashId] truthy).
          if (cf.required || sel[cf.trashId]) {
            profileActiveCFs.add(cf.trashId);
          }
        }
      }
      // formatItemNames also count as in-profile active.
      for (const fi of (this.profileDetail?.detail?.formatItemNames || [])) {
        profileActiveCFs.add(fi.trashId);
      }
      // Same trashId can appear in multiple Additional groups (e.g.
      // regional Unwanted variants share several CFs). Guard so each
      // opted-in CF appears exactly once in the Diffs list.
      const seenAddCF = new Set();
      // extraCFs holds the user-saved score for opted-in Additional
      // CFs (out-of-profile entries). Restored via resyncProfile's
      // scoreOverrides split — entries land here, NOT in
      // cfScoreOverrides (which is reserved for in-profile overrides).
      // Read it first so the score column shows the user's value, not
      // the TRaSH-context default.
      const extras = this.extraCFs || {};
      for (const g of (this.extraCFGroups || [])) {
        if (!g.name || inProfileGroup.has(g.name)) continue;
        const m = (g.name || '').match(/^\[([^\]]+)\]\s*(.*)$/);
        const shortName = m ? (m[2].trim() || g.name) : g.name;
        for (const cf of (g.cfs || [])) {
          if (profileActiveCFs.has(cf.trashId)) continue; // already in profile, not an addition
          if (seenAddCF.has(cf.trashId)) continue;
          if (!!sel[cf.trashId]) {
            seenAddCF.add(cf.trashId);
            // Score priority:
            //   1. extras[trashId]    — user's saved score for this
            //      Additional CF (loaded from rule.scoreOverrides)
            //   2. overrides[trashId] — defensive (live edits inside
            //      Sync Preview Customize mode may write here)
            //   3. cf.score           — extraCFGroups catalog usually
            //      has trashScores map but no resolved cf.score
            //   4. resolveCFDefaultScore — TRaSH default via profile
            //      scoreCtx (the eventual fallback)
            let score = extras[cf.trashId];
            if (score === undefined) score = overrides[cf.trashId];
            if (score === undefined) {
              score = cf.score;
              if (score === undefined || score === null) {
                const r = this.resolveCFDefaultScore?.(cf.trashId);
                score = (typeof r === 'number') ? r : null;
              }
            }
            out.additionalCFs.push({
              trashId: cf.trashId,
              name: cf.name,
              score,
              fromGroup: shortName,
              sourceCategory: g.category,
              isCustom: !!cf.isCustom || (cf.trashId || '').startsWith('custom:'),
            });
          }
        }
      }

      // 4. Excluded default-on CFs — only counted when the group is
      // still active (an inactive group's CFs aren't "excluded" — the
      // whole group is off).
      for (const g of (this.profileDetail.detail?.trashGroups || [])) {
        const grpKey = '__grp_' + g.name;
        const grpOn = sel[grpKey] !== undefined ? sel[grpKey] : g.defaultEnabled;
        if (!grpOn) continue;
        const m = (g.name || '').match(/^\[([^\]]+)\]\s*(.*)$/);
        const shortName = m ? (m[2].trim() || g.name) : g.name;
        for (const cf of g.cfs) {
          if (cf.required) continue;
          if (cf.default && sel[cf.trashId] === false) {
            out.excludedCFs.push({
              trashId: cf.trashId,
              name: cf.name,
              score: cf.score,
              fromGroup: shortName,
              sourceCategory: g.category,
            });
          }
        }
      }

      // 5. Excluded required CFs — Phase 2c. Two sources:
      //    a) formatItemNames the user explicitly excluded (lock-icon
      //       click in the standalone Required CFs tab).
      //    b) Group-required CFs from active groups that the user
      //       excluded individually. Inactive groups don't count — the
      //       whole group is off, so the CF isn't being filtered out
      //       specifically.
      for (const fi of (this.profileDetail.detail?.formatItemNames || [])) {
        if (sel[fi.trashId] === false) {
          out.excludedRequiredCFs.push({
            trashId: fi.trashId,
            name: fi.name,
            score: fi.score,
            fromGroup: 'Required CFs',
          });
        }
      }
      // Required CFs from off groups are intentionally NOT surfaced
      // here. Design rationale: when the user turns a group OFF, the
      // entire group's CFs stop syncing regardless of individual
      // exclusion state — so a "you excluded this" entry in Diffs
      // would be misleading (the CF wasn't going to sync anyway).
      // Backend's getExcludedCFIds + ComputeRuleCustomizations bucket 4
      // follow the same convention (only count entries whose trashId
      // resolves through ComputeTrashDefaults, which gates on
      // group.defaultEnabled). End-to-end consistent: off-group
      // exclusions are dead state by design.
      for (const g of (this.profileDetail.detail?.trashGroups || [])) {
        const grpKey = '__grp_' + g.name;
        const grpOn = sel[grpKey] !== undefined ? sel[grpKey] : g.defaultEnabled;
        if (!grpOn) continue;
        const m = (g.name || '').match(/^\[([^\]]+)\]\s*(.*)$/);
        const shortName = m ? (m[2].trim() || g.name) : g.name;
        for (const cf of g.cfs) {
          if (cf.required && sel[cf.trashId] === false) {
            out.excludedRequiredCFs.push({
              trashId: cf.trashId,
              name: cf.name,
              score: cf.score,
              fromGroup: shortName,
              sourceCategory: g.category,
            });
          }
        }
      }

      return out;
    },

    // Additional CF groups — extraCFGroups minus:
    //   1. Groups already part of the profile by name match.
    //   2. Variant groups — Additional groups whose entire CF set
    //      (by trashId) equals a profile-active group's CF set.
    //      Catches Golden Rule HD vs UHD: identical CF lists, only
    //      the description targets a different resolution tier.
    //      When UHD is in profile.trashGroups, the user shouldn't
    //      see HD as an "extra" — it's the same CFs.
    // CF-level overlap filtering for non-variant partial-overlap
    // groups (Unwanted French vs Default) happens in the template
    // via spAdditionalGroupCFs which trims overlapping CFs and
    // leaves the unique ones.
    spAdditionalCFGroups() {
      const all = this.extraCFGroups || [];
      const inProfileName = new Set();
      const profileCFSets = new Set();
      for (const g of (this.profileDetail?.detail?.trashGroups || [])) {
        if (g.name) inProfileName.add(g.name);
        const ids = (g.cfs || []).map(c => c.trashId).filter(Boolean).sort().join('|');
        if (ids) profileCFSets.add(ids);
      }
      return all.filter(g => {
        if (!g.name || inProfileName.has(g.name)) return false;
        const ids = (g.cfs || []).map(c => c.trashId).filter(Boolean).sort().join('|');
        if (ids && profileCFSets.has(ids)) return false; // variant of active profile group
        return true;
      });
    },

    // CFs in the given Additional CF group that aren't already
    // syncing via a PROFILE-DEFAULT group. Used by the template to
    // render the CF list AND to gate group visibility.
    //
    // CRITICAL: active set is built ONLY from profile.trashGroups
    // (the groups that ARE part of this profile via
    // quality_profiles.include) — NOT from raw selectedOptionalCFs
    // entries. selectedOptionalCFs is a flat map shared with
    // Additional CF activations; if we read it directly, every CF
    // the user toggles on in Additional CF would immediately
    // disappear from view (filter sees its trashId in sel and
    // excludes it). Source-tracking would require restructuring
    // the map; computing active-via-profile-only sidesteps that.
    //
    // For each profile trashGroup that's on, we include:
    //   - all required CFs (always sync when group is on)
    //   - all optional CFs whose effective state is on (explicit
    //     sel[trashId] truthy OR fall back to cf.default)
    // formatItemNames at profile level always count as active.
    //
    // Plain-object active map (not Set) — Alpine's reactive Proxy
    // strips Set's .has() method making it un-callable from
    // template expressions. Property access bypasses the trap.
    spAdditionalGroupCFs(g) {
      const cfs = g?.cfs || [];
      if (cfs.length === 0) return cfs;
      // Filter structurally — any CF that EXISTS in a profile-default
      // group (regardless of whether it's currently active) is hidden
      // from the Additional picker. Two reasons:
      //   1. Activating an opt-in CF inside the Additional group when
      //      it ALSO lives as an opt-in inside a profile-default group
      //      is just confusing — the CF has a natural home in the
      //      profile-default group (e.g. Scene / Retag in Unwanted),
      //      activating from there is the obvious path. Showing it in
      //      both places (Profile default's Unwanted + Additional's
      //      Unwanted SQP / German / French) produces visual duplicates.
      //   2. The previous active-only filter caused activated
      //      Additional CFs to vanish from the picker the moment the
      //      user toggled them on — sel[id] = true → "active" → filter
      //      excluded them. The user couldn't deactivate without
      //      navigating elsewhere. Structural filtering separates
      //      "where it lives" from "is it currently on".
      const inProfile = {};
      const groups = (this.profileDetail?.detail?.trashGroups) || [];
      for (const group of groups) {
        for (const cf of (group.cfs || [])) {
          if (cf?.trashId) inProfile[cf.trashId] = true;
        }
      }
      for (const fi of (this.profileDetail?.detail?.formatItemNames || [])) {
        if (fi?.trashId) inProfile[fi.trashId] = true;
      }
      return cfs.filter(cf => !inProfile[cf.trashId]);
    },

    // Sort CFActions / ScoreActions by the profile's natural order so
    // dry-run + apply results match the structure the user sees in the
    // editor (formatItems first, then default-on groups in declared
    // order, then Additional CFs). Backend currently returns these in
    // Go map iteration order (randomized per Go spec since 1.0).
    //
    // TODO (deferred — backend fix, ~30-45 min): sort.SliceStable inside
    // BuildSyncPlan + ExecuteSyncPlan so EVERY consumer (Classic editor,
    // sync history, API responses) gets natural-order results, not just
    // Sync Preview. Tracked in container CLAUDE.md.
    //
    // @param actions  array of {name, ...} or {cfName, ...} objects
    // @param nameKey  which field carries the CF name ("name" or "cfName")
    spSortActions(actions, nameKey) {
      if (!Array.isArray(actions) || actions.length === 0 || !this.profileDetail) return actions || [];
      const order = new Map();
      let idx = 0;
      for (const fi of (this.profileDetail.detail?.formatItemNames || [])) {
        if (fi.name && !order.has(fi.name)) order.set(fi.name, idx++);
      }
      for (const g of (this.profileDetail.detail?.trashGroups || [])) {
        for (const cf of (g.cfs || [])) {
          if (cf.name && !order.has(cf.name)) order.set(cf.name, idx++);
        }
      }
      for (const g of (this.extraCFGroups || [])) {
        for (const cf of (g.cfs || [])) {
          if (cf.name && !order.has(cf.name)) order.set(cf.name, idx++);
        }
      }
      return [...actions].sort((a, b) => {
        const an = a[nameKey] || '';
        const bn = b[nameKey] || '';
        const ai = order.has(an) ? order.get(an) : Infinity;
        const bi = order.has(bn) ? order.get(bn) : Infinity;
        if (ai !== bi) return ai - bi;
        return an.localeCompare(bn);
      });
    },

    // Reset a single basics field to the profile's TRaSH default.
    // Called from the Diffs view + General section reset icons.
    // cutoffQuality is stored on pdOverrides directly (not under .value)
    // — separate-case to keep the rest of the fields uniform.
    pdResetBasic(field) {
      const pd = this.profileDetail?.detail?.profile || {};
      if (field === 'cutoffQuality') {
        this.pdOverrides.cutoffQuality = pd.cutoff || '';
        return;
      }
      if (!this.pdOverrides[field]) return;
      let def;
      switch (field) {
        case 'language': def = pd.language?.name || pd.language || 'Original'; break;
        case 'minFormatScore': def = pd.minFormatScore ?? 0; break;
        case 'minUpgradeFormatScore': def = pd.minUpgradeFormatScore ?? 1; break;
        case 'cutoffFormatScore': def = pd.cutoffFormatScore ?? 10000; break;
        case 'upgradeAllowed': def = !!pd.upgradeAllowed; break;
        default: return;
      }
      this.pdOverrides[field].value = def;
    },

    // Drop a CF score override so the CF reverts to its TRaSH-baseline
    // score. Replace the whole map so Alpine picks up the change (delete
    // alone on a reactive proxy can miss).
    pdResetCFScore(trashId) {
      if (!trashId) return;
      // Two stores carry overrides: cfScoreOverrides for in-profile
      // CFs, extraCFs for opted-in Additional CFs (out-of-profile).
      // Reset must clear from BOTH — pre-fix the extras store kept
      // its value when the user clicked Reset on the Profile overview
      // because the action only touched cfScoreOverrides, leaving
      // the stale value visible everywhere extras is read.
      if (this.cfScoreOverrides && this.cfScoreOverrides[trashId] !== undefined) {
        const updated = { ...this.cfScoreOverrides };
        delete updated[trashId];
        this.cfScoreOverrides = updated;
      }
      if (this.extraCFs && this.extraCFs[trashId] !== undefined) {
        const updated = { ...this.extraCFs };
        delete updated[trashId];
        this.extraCFs = updated;
      }
    },

    // Reset action for the Diffs view's Added Additional CFs row —
    // un-opts the CF so it stops syncing. Equivalent to flipping the
    // toggle off in the Additional CF tab.
    spRemoveAdditionalCF(trashId) {
      if (!trashId) return;
      const updated = { ...(this.selectedOptionalCFs || {}) };
      updated[trashId] = false;
      this.selectedOptionalCFs = updated;
    },

    // Reset action for the Diffs view's Excluded default-on CFs row —
    // re-includes the CF that was previously turned off. Deletes the
    // entry (rather than writing explicit `true`) so the CF tracks its
    // upstream default — if TRaSH later flips the CF's default to off,
    // we don't keep it pinned on against the new default.
    spReincludeExcludedCF(trashId) {
      if (!trashId) return;
      const updated = { ...(this.selectedOptionalCFs || {}) };
      delete updated[trashId];
      this.selectedOptionalCFs = updated;
    },

    // Phase 2c — toggle a required CF between excluded and included.
    // sel[trashId] === false → re-include (delete entry, tracks upstream
    // default). Anything else → exclude (write false). Backend's
    // getExcludedCFIds picks it up via computeTrashDefaults, which
    // already covers formatItemNames + required CFs from default-on
    // groups, so the sync engine filters them out end-to-end.
    spToggleRequiredCF(trashId) {
      if (!trashId) return;
      const updated = { ...(this.selectedOptionalCFs || {}) };
      if (updated[trashId] === false) {
        delete updated[trashId];
      } else {
        updated[trashId] = false;
      }
      this.selectedOptionalCFs = updated;
    },

    // Deactivate a non-required CF (optional in-profile or additional) from
    // the Profile overview's active lists. Writes selectedOptionalCFs[id] =
    // false so the CF drops out of the enabled set and its row disappears on
    // re-render. Re-activating happens on the Profile default tab, since the
    // overview only lists active CFs.
    spDeactivateOverviewCF(trashId) {
      if (!trashId) return;
      const updated = { ...(this.selectedOptionalCFs || {}) };
      updated[trashId] = false;
      this.selectedOptionalCFs = updated;
    },

    // Customize-mode entry confirmation. Profile customization can
    // change scores, exclude required CFs, add extras — each of which
    // shifts how Sonarr/Radarr selects releases. A user who flips
    // Customize on without thinking can produce a profile that
    // syncs nothing or that ignores the curated baseline. The modal
    // makes that risk explicit before they enter editor mode.
    spConfirmEnableCustomize() {
      this.confirmModal = {
        show: true,
        title: 'Customize this profile',
        message: 'Customizing this profile lets you change scores, exclude required CFs, and add extras beyond the profile defaults.\n\nThese edits can change how the profile picks releases — make sure you understand what each setting does before changing it.',
        cancelLabel: 'Use defaults',
        confirmLabel: 'I understand',
        onConfirm: () => { this.pdOverridesEnabled = true; },
        onCancel: () => {},
      };
    },

    // Persist (or clear) a CF score override from an inline editor. If
    // the new value is empty / NaN / equals the CF's TRaSH default, the
    // override entry is deleted so the rule payload stays clean
    // (matches save-time filter at profiles.js:1601 pattern). Otherwise
    // the override map is written. Empty-string check is explicit
    // because Number("") === 0 — without the guard, clearing the field
    // would silently write a 0 override for CFs whose TRaSH default
    // is non-zero (e.g. HQ Source Groups at +500).
    spApplyCFScore(trashId, value, defaultScore) {
      if (!trashId) return;
      const isEmpty = value === '' || value == null;
      const v = isEmpty ? NaN : Number(value);
      const reset = isEmpty || Number.isNaN(v) || v === Number(defaultScore);
      // Two stores: cfScoreOverrides (in-profile CFs) vs extraCFs
      // (opted-in Additional CFs, out-of-profile). Determine which
      // store the CF belongs to so the user's edit lands in the right
      // place and is visible everywhere. Heuristic: if the CF is in
      // any profile-default trashGroup or formatItems, it's
      // in-profile. Otherwise it's an Additional CF.
      const inProfileGroups = new Set();
      for (const fi of (this.profileDetail?.detail?.formatItemNames || [])) inProfileGroups.add(fi.trashId);
      for (const g of (this.profileDetail?.detail?.trashGroups || [])) {
        for (const cf of (g.cfs || [])) inProfileGroups.add(cf.trashId);
      }
      const isExtra = !inProfileGroups.has(trashId);
      // ALWAYS clear from the wrong store too — covers cases where
      // the CF's classification changed (e.g. TRaSH restructure
      // moved it) or where an older code path wrote to the wrong
      // map. Without this the stale entry would shadow the new one.
      const cfScoreOverrides = { ...this.cfScoreOverrides };
      const extraCFs = { ...this.extraCFs };
      delete cfScoreOverrides[trashId];
      delete extraCFs[trashId];
      if (!reset) {
        if (isExtra) extraCFs[trashId] = v;
        else cfScoreOverrides[trashId] = v;
      }
      this.cfScoreOverrides = cfScoreOverrides;
      this.extraCFs = extraCFs;
    },

    // Total diff count for the sub-nav badge.
    spOverviewDiffCount() {
      const d = this.spOverviewDiffs();
      return d.modifiedBasics.length + d.scoreOverrides.length + d.additionalCFs.length + d.excludedCFs.length + d.excludedRequiredCFs.length + (d.qualityItems?.length || 0);
    },

    // Allowed qualities in their qualityStructure order. Falls back to
    // profile.items if qualityStructure isn't populated (initial load).
    // Each entry is { name, isGroup, members[] } so the Overview tab
    // can render groups (e.g. "WEB 2160p" containing WEBDL-2160p +
    // WEBRip-2160p) with their members nested. Singles have isGroup
    // false and members empty.
    spOverviewQualities() {
      const items = (this.qualityStructure && this.qualityStructure.length)
        ? this.qualityStructure
        : (this.profileDetail?.detail?.profile?.items || []);
      const out = [];
      for (const item of items) {
        if (!item.allowed) continue;
        const name = item.name || item.quality?.name || '';
        if (!name) continue;
        const rawMembers = item.items || [];
        const members = rawMembers.map(m => {
          if (typeof m === 'string') return m;
          return m.quality?.name || m.name || '';
        }).filter(n => n);
        out.push({ name, isGroup: members.length > 0, members });
      }
      return out;
    },

    // Click-handler for the primary "Use →" CTA on a card. Opens the
    // detail/editor overlay so the user can configure customizations and
    // pick their target instance in Save & Sync's picker. We pass insts[0]
    // as the editor's default working instance (so internal data flow and
    // the sync-modal's picker have a valid starting point), but mark the
    // overlay as browse-mode so the header doesn't display that default
    // as if it were a committed target — the user picks the real instance
    // when they hit Save & Sync. Sync Rule → Edit entries skip this flag
    // (they pass restoreFromRule=true) and continue to show the bound
    // instance as today.
    async tpdUseProfile(appType, trashId) {
      const insts = this.instancesOfType(appType);
      if (insts.length === 0) {
        this.showToast(`Add a ${appType} instance first to use this profile`, 'error', 6000);
        return;
      }
      const profile = (this.trashProfiles[appType] || []).find(p => p.trashId === trashId);
      if (!profile) {
        this.showToast('Profile not found in TRaSH data', 'error', 6000);
        return;
      }
      await this.openProfileDetail(insts[0], profile);
      if (this.profileDetail) {
        this.profileDetail = { ...this.profileDetail, _browseMode: true };
      }
    },
  },
};
