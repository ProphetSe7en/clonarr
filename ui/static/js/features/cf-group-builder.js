import { copyToClipboard } from '../utils/clipboard.js';

function cfgbMD5(str) {
  function rh(n) { let s = ''; for (let j = 0; j <= 3; j++) s += ((n >> (j*8+4)) & 0xf).toString(16) + ((n >> (j*8)) & 0xf).toString(16); return s; }
  function ad(a,b) { const l = (a & 0xffff) + (b & 0xffff); return (((a >> 16) + (b >> 16) + (l >> 16)) << 16) | (l & 0xffff); }
  function rot(n,c) { return (n << c) | (n >>> (32 - c)); }
  function cm(q,a,b,x,s,t) { return ad(rot(ad(ad(a,q), ad(x,t)), s), b); }
  function ff(a,b,c,d,x,s,t){return cm((b&c)|(~b&d),a,b,x,s,t);}
  function gg(a,b,c,d,x,s,t){return cm((b&d)|(c&~d),a,b,x,s,t);}
  function hh(a,b,c,d,x,s,t){return cm(b^c^d,a,b,x,s,t);}
  function ii(a,b,c,d,x,s,t){return cm(c^(b|~d),a,b,x,s,t);}
  const utf8 = unescape(encodeURIComponent(str));
  const bl = utf8.length, nb = ((bl + 8) >> 6) + 1, x = new Array(nb*16).fill(0);
  for (let i = 0; i < bl; i++) x[i >> 2] |= utf8.charCodeAt(i) << ((i % 4) * 8);
  x[bl >> 2] |= 0x80 << ((bl % 4) * 8);
  x[nb*16 - 2] = bl * 8;
  let a = 1732584193, b = -271733879, c = -1732584194, d = 271733878;
  for (let i = 0; i < x.length; i += 16) {
    const oa=a, ob=b, oc=c, od=d;
    a=ff(a,b,c,d,x[i+0],7,-680876936);  d=ff(d,a,b,c,x[i+1],12,-389564586);  c=ff(c,d,a,b,x[i+2],17,606105819);    b=ff(b,c,d,a,x[i+3],22,-1044525330);
    a=ff(a,b,c,d,x[i+4],7,-176418897);  d=ff(d,a,b,c,x[i+5],12,1200080426);  c=ff(c,d,a,b,x[i+6],17,-1473231341);  b=ff(b,c,d,a,x[i+7],22,-45705983);
    a=ff(a,b,c,d,x[i+8],7,1770035416);  d=ff(d,a,b,c,x[i+9],12,-1958414417); c=ff(c,d,a,b,x[i+10],17,-42063);     b=ff(b,c,d,a,x[i+11],22,-1990404162);
    a=ff(a,b,c,d,x[i+12],7,1804603682); d=ff(d,a,b,c,x[i+13],12,-40341101);  c=ff(c,d,a,b,x[i+14],17,-1502002290);b=ff(b,c,d,a,x[i+15],22,1236535329);
    a=gg(a,b,c,d,x[i+1],5,-165796510);  d=gg(d,a,b,c,x[i+6],9,-1069501632);  c=gg(c,d,a,b,x[i+11],14,643717713);  b=gg(b,c,d,a,x[i+0],20,-373897302);
    a=gg(a,b,c,d,x[i+5],5,-701558691);  d=gg(d,a,b,c,x[i+10],9,38016083);    c=gg(c,d,a,b,x[i+15],14,-660478335); b=gg(b,c,d,a,x[i+4],20,-405537848);
    a=gg(a,b,c,d,x[i+9],5,568446438);   d=gg(d,a,b,c,x[i+14],9,-1019803690); c=gg(c,d,a,b,x[i+3],14,-187363961);  b=gg(b,c,d,a,x[i+8],20,1163531501);
    a=gg(a,b,c,d,x[i+13],5,-1444681467);d=gg(d,a,b,c,x[i+2],9,-51403784);    c=gg(c,d,a,b,x[i+7],14,1735328473);  b=gg(b,c,d,a,x[i+12],20,-1926607734);
    a=hh(a,b,c,d,x[i+5],4,-378558);     d=hh(d,a,b,c,x[i+8],11,-2022574463); c=hh(c,d,a,b,x[i+11],16,1839030562); b=hh(b,c,d,a,x[i+14],23,-35309556);
    a=hh(a,b,c,d,x[i+1],4,-1530992060); d=hh(d,a,b,c,x[i+4],11,1272893353);  c=hh(c,d,a,b,x[i+7],16,-155497632);  b=hh(b,c,d,a,x[i+10],23,-1094730640);
    a=hh(a,b,c,d,x[i+13],4,681279174);  d=hh(d,a,b,c,x[i+0],11,-358537222);  c=hh(c,d,a,b,x[i+3],16,-722521979);  b=hh(b,c,d,a,x[i+6],23,76029189);
    a=hh(a,b,c,d,x[i+9],4,-640364487);  d=hh(d,a,b,c,x[i+12],11,-421815835); c=hh(c,d,a,b,x[i+15],16,530742520);  b=hh(b,c,d,a,x[i+2],23,-995338651);
    a=ii(a,b,c,d,x[i+0],6,-198630844);  d=ii(d,a,b,c,x[i+7],10,1126891415);  c=ii(c,d,a,b,x[i+14],15,-1416354905);b=ii(b,c,d,a,x[i+5],21,-57434055);
    a=ii(a,b,c,d,x[i+12],6,1700485571); d=ii(d,a,b,c,x[i+3],10,-1894986606); c=ii(c,d,a,b,x[i+10],15,-1051523);   b=ii(b,c,d,a,x[i+1],21,-2054922799);
    a=ii(a,b,c,d,x[i+8],6,1873313359);  d=ii(d,a,b,c,x[i+15],10,-30611744);  c=ii(c,d,a,b,x[i+6],15,-1560198380); b=ii(b,c,d,a,x[i+13],21,1309151649);
    a=ii(a,b,c,d,x[i+4],6,-145523070);  d=ii(d,a,b,c,x[i+11],10,-1120210379);c=ii(c,d,a,b,x[i+2],15,718787259);   b=ii(b,c,d,a,x[i+9],21,-343485551);
    a=ad(a,oa); b=ad(b,ob); c=ad(c,oc); d=ad(d,od);
  }
  return rh(a) + rh(b) + rh(c) + rh(d);
}

export default {
  state: {},
  methods: {
    // --- CF Group Builder ---
    // Loads CFs + profiles for the active app so the builder UI can populate.
    // Called on first click of the CF Group Builder sub-tab and whenever the
    // user toggles Radarr↔Sonarr (CFs + profiles are app-specific).
    async cfgbLoad(appType) {
      this.cfgbLoadError = '';
      // When the user switches Radarr↔Sonarr the current form has to be reset
      // (a half-built group is scoped to one app type). Refresh saved list too.
      this.cfgbReset();
      // Race guard: if the user rapidly flips Radarr↔Sonarr↔Radarr, multiple
      // cfgbLoad calls are in flight simultaneously. Each stores its appType
      // on entry; on completion we re-check — if a later call has superseded
      // ours, discard the response instead of leaking state across appTypes.
      this._cfgbLoadFor = appType;
      try {
        const [allCfsResp, profResp, savedResp, trashGroupsResp] = await Promise.all([
          fetch('/api/trash/' + appType + '/all-cfs'),
          fetch('/api/trash/' + appType + '/profiles'),
          fetch('/api/cf-groups/' + appType),
          fetch('/api/trash/' + appType + '/cf-groups'),
        ]);
        if (this._cfgbLoadFor !== appType) return; // superseded
        if (!allCfsResp.ok || !profResp.ok) {
          throw new Error('HTTP ' + allCfsResp.status + ' / ' + profResp.status);
        }
        const [allCfsRes, profRes, savedRes, trashGroupsRes] = await Promise.all([
          allCfsResp.json(),
          profResp.json(),
          savedResp.ok ? savedResp.json() : Promise.resolve([]),
          trashGroupsResp.ok ? trashGroupsResp.json() : Promise.resolve([]),
        ]);
        // /all-cfs returns { categories: [{ category, groups: [{ cfs: [...] }] }] }.
        // The "categories" layer is a Clonarr-side abstraction; TRaSH itself
        // organizes CFs by cf-groups (the inner `groups` level). The builder
        // filter uses those REAL TRaSH groups so new upstream groups appear
        // automatically without any Clonarr-side mapping to maintain.
        //
        // Each CF carries its parent group's trashId + name for filtering.
        // Synthetic "Custom" and "Other" groups (emitted by the backend for
        // user-custom CFs and ungrouped CFs) have no groupTrashId — we treat
        // those as two special filter modes instead of real groups.
        // The /all-cfs endpoint lists each CF once per containing cf-group.
        // ~18 TRaSH CFs live in multiple groups (HDR10+, DV-related, a few
        // anime ones). A naive flatten produced duplicate rows — Alpine's
        // <template x-for> with :key="cf.trashId" silently drops rendering
        // at the first duplicate, which truncated the alpha-sorted list
        // around its first repeat (e.g. stopped just past "AV1").
        //
        // Dedup by trashId, but accumulate every groupTrashId the CF appears
        // under. The cf-group dropdown filter walks groupTrashIds (array
        // membership), so a shared CF surfaces under each of its groups.
        // The group count tally counts every appearance so the "(N)" badges
        // in the dropdown reflect real TRaSH group membership.
        const flat = [];
        const byTid = new Map(); // trashId → entry in flat[]
        const groupMap = new Map(); // groupTrashId → {groupTrashId, name, count}
        let hasCustom = false, hasOther = false;
        for (const cat of (allCfsRes.categories || [])) {
          for (const group of (cat.groups || [])) {
            const gid = group.groupTrashId || '';
            const gname = group.name || group.shortName || '';
            if (gid && !groupMap.has(gid)) {
              groupMap.set(gid, { groupTrashId: gid, name: gname, count: 0 });
            }
            for (const cf of (group.cfs || [])) {
              if (!cf.trashId || !cf.name) continue;
              if (gid) groupMap.get(gid).count++;
              if (cf.isCustom) hasCustom = true;
              if (!gid && !cf.isCustom) hasOther = true;
              let row = byTid.get(cf.trashId);
              if (!row) {
                row = {
                  trashId: cf.trashId,
                  name: cf.name,
                  groupTrashIds: [],
                  groupNames: [],
                  isCustom: !!cf.isCustom,
                };
                flat.push(row);
                byTid.set(cf.trashId, row);
              }
              if (gid && !row.groupTrashIds.includes(gid)) {
                row.groupTrashIds.push(gid);
                row.groupNames.push(gname);
              }
            }
          }
        }
        // Cache TRaSH-only state on the instance. cfgbApplyLocalGroups()
        // reads this cache + cfgbSavedGroups to produce cfgbCFs / cfgbGroups /
        // cfgbHasOther — so Save/Delete on a saved group can refresh the
        // dropdown without re-fetching /all-cfs or resetting the form.
        this._cfgbTrashFlat = flat;
        this._cfgbTrashGroupMap = groupMap;
        this._cfgbTrashHasCustom = hasCustom;
        this.cfgbApplyLocalGroups();
        // Profiles API still uses camelCase (api.ProfileListItem).
        this.cfgbProfiles = (profRes || [])
          .map(p => ({
            trashId: p.trashId,
            name: p.name || '',
            group: typeof p.group === 'number' ? p.group : 99,
            groupName: p.groupName || 'Other',
          }))
          .filter(p => p.trashId && p.name);
        // Collapse all profile cards by default — mirrors the Profiles /
        // TRaSH Sync tab behaviour where the user opens the card they care
        // about rather than scrolling past 50+ profiles up front.
        this.cfgbProfileGroupExpanded = {};
        this.cfgbSavedGroups = Array.isArray(savedRes) ? savedRes : [];
        // TRaSH upstream cf-groups — used by the "Copy from TRaSH" section so
        // the user can base a new local group on an existing upstream one and
        // tweak it without touching the TRaSH repo clone.
        this.cfgbTrashCFGroups = Array.isArray(trashGroupsRes) ? trashGroupsRes : [];
      } catch (e) {
        if (this._cfgbLoadFor !== appType) return; // superseded
        console.error('cfgbLoad failed:', e);
        this.cfgbLoadError = 'Failed to load TRaSH data: ' + e.message + '. Try Pull TRaSH in Settings → TRaSH Repo.';
        this.cfgbCFs = [];
        this.cfgbGroups = [];
        this.cfgbHasCustom = false;
        this.cfgbUngroupedTrashCount = 0;
        this.cfgbUngroupedRemainingCount = 0;
        this._cfgbTrashFlat = [];
        this._cfgbTrashGroupMap = new Map();
        this._cfgbTrashHasCustom = false;
        this.cfgbProfiles = [];
        this.cfgbSavedGroups = [];
        this.cfgbTrashCFGroups = [];
      }
    },

    // Combines TRaSH-only cache (set by cfgbLoad) with the current
    // cfgbSavedGroups to produce the CF list, dropdown, and Ungrouped
    // counts. Callable after Save/Delete of a local group so the dropdown
    // refreshes without another /all-cfs fetch.
    //
    // Each CF keeps its TRaSH group memberships in c.trashGroupTrashIds
    // (pure) and the full combined set in c.groupTrashIds (TRaSH + local,
    // used for rendering + filter). The "Ungrouped (TRaSH)" filter walks
    // trashGroupTrashIds; "Ungrouped (after local)" walks groupTrashIds.
    cfgbApplyLocalGroups() {
      // Clone the TRaSH-only flat list so the merge doesn't mutate cache.
      const flat = (this._cfgbTrashFlat || []).map(c => ({
        ...c,
        trashGroupTrashIds: c.groupTrashIds.slice(),
        trashGroupNames: c.groupNames.slice(),
        groupTrashIds: c.groupTrashIds.slice(),
        groupNames: c.groupNames.slice(),
      }));
      const byTid = new Map(flat.map(c => [c.trashId, c]));
      const groupMap = new Map();
      for (const [gid, g] of (this._cfgbTrashGroupMap || new Map())) {
        groupMap.set(gid, { ...g });
      }
      // Merge local cf-groups into dropdown entries + per-CF membership.
      for (const g of (this.cfgbSavedGroups || [])) {
        if (!g || !g.id) continue;
        const localId = 'local:' + g.id;
        const gname = g.name || '(unnamed local)';
        groupMap.set(localId, {
          groupTrashId: localId,
          name: gname,
          count: (g.custom_formats || []).length,
          isLocal: true,
        });
        for (const cf of (g.custom_formats || [])) {
          const tid = cf && cf.trash_id;
          if (!tid) continue;
          const row = byTid.get(tid);
          if (!row) continue; // CF in saved group but not in /all-cfs (stale)
          if (!row.groupTrashIds.includes(localId)) {
            row.groupTrashIds.push(localId);
            row.groupNames.push(gname);
          }
        }
      }
      this.cfgbCFs = flat;
      // TRaSH groups first (alpha), then locals (alpha) at the bottom.
      this.cfgbGroups = Array.from(groupMap.values()).sort((a, b) => {
        if (!!a.isLocal !== !!b.isLocal) return a.isLocal ? 1 : -1;
        return a.name.localeCompare(b.name);
      });
      this.cfgbHasCustom = this._cfgbTrashHasCustom;
      this.cfgbUngroupedTrashCount =
        flat.filter(c => !c.isCustom && c.trashGroupTrashIds.length === 0).length;
      this.cfgbUngroupedRemainingCount =
        flat.filter(c => !c.isCustom && c.groupTrashIds.length === 0).length;
    },

    cfgbUpdateHash() {
      // trash_id is MD5 of the group name, scoped by app-type prefix so the
      // same name ("[Release Groups] Anime") on Radarr vs Sonarr produces
      // different hashes. TRaSH's tooling treats trash_id as a global key
      // across both apps, so identical hashes would collide there even
      // though our on-disk storage separates them per app-type.
      //
      // Hash lock is the escape hatch for editing: when locked, name-input
      // events don't regenerate the hash so typo fixes / minor rewording
      // don't invalidate downstream references. The user toggles the lock
      // explicitly via the edit-banner button.
      if (this.cfgbHashLocked) return;
      const n = (this.cfgbName || '').trim();
      const app = this.activeAppType || 'radarr';
      this.cfgbTrashID = n ? cfgbMD5(app + ':' + n) : '';
    },

    // Toggle the hash lock. Locking restores the original trash_id
    // (cfgbOriginalTrashID), unlocking regenerates from the current name.
    // Only meaningful when cfgbOriginalTrashID is set — fresh new groups
    // have no original to restore, so the button is hidden in that state.
    cfgbToggleHashLock() {
      if (!this.cfgbOriginalTrashID) return;
      this.cfgbHashLocked = !this.cfgbHashLocked;
      if (this.cfgbHashLocked) {
        this.cfgbTrashID = this.cfgbOriginalTrashID;
      } else {
        const n = (this.cfgbName || '').trim();
        const app = this.activeAppType || 'radarr';
        this.cfgbTrashID = n ? cfgbMD5(app + ':' + n) : '';
      }
    },

    cfgbFilteredCFs() {
      const g = this.cfgbGroupFilter || 'all';
      let list = this.cfgbCFs;
      if (g === 'custom') {
        list = list.filter(c => c.isCustom);
      } else if (g === 'other-trash') {
        // Ungrouped per TRaSH: CF isn't in any upstream cf-group. Local
        // groups don't subtract — useful to see the full set TRaSH still
        // needs to categorize.
        list = list.filter(c => !c.isCustom && c.trashGroupTrashIds.length === 0);
      } else if (g === 'other-remaining') {
        // Ungrouped after local work: excludes CFs already placed in any
        // local group. This is "what's left to do" once the user has
        // started organizing.
        list = list.filter(c => !c.isCustom && c.groupTrashIds.length === 0);
      } else if (g !== 'all') {
        // Specific cf-group (TRaSH or local) by its trash_id / localId.
        // A CF in multiple groups matches each of its groups.
        list = list.filter(c => c.groupTrashIds.includes(g));
      }
      // Filter string supports multiple whitespace-separated terms with OR
      // semantics — "mono stereo surround" matches CFs whose name contains
      // any of those words. Makes it easy to pull related-but-separate
      // formats into one view without chaining filter typing.
      const terms = (this.cfgbCFFilter || '')
        .toLowerCase()
        .split(/\s+/)
        .map(t => t.trim())
        .filter(Boolean);
      if (terms.length > 0) {
        list = list.filter(c => {
          const n = c.name.toLowerCase();
          return terms.some(t => n.includes(t));
        });
      }
      return list.slice().sort((a, b) => a.name.localeCompare(b.name));
    },

    // True when every CF the user currently sees (group + text filters
    // applied) is already selected. Drives the Select-all toggle's checked
    // state so one click flips between select-all and deselect-all.
    cfgbFilteredAllSelected() {
      const list = this.cfgbFilteredCFs();
      if (list.length === 0) return false;
      return list.every(c => this.cfgbSelectedCFs[c.trashId]);
    },

    // Applies select-all / deselect-all to whatever the user is looking at.
    // Scoped to cfgbFilteredCFs() so "select all Release Group Tiers" works
    // whether the filter is a cf-group, a text match, or both combined.
    cfgbToggleFilteredAll(on) {
      const list = this.cfgbFilteredCFs();
      const next = { ...this.cfgbSelectedCFs };
      for (const c of list) {
        if (on) next[c.trashId] = true;
        else delete next[c.trashId];
      }
      this.cfgbSelectedCFs = next;
    },
    cfgbGroupFilterLabel() {
      // Short human-readable label for the current filter — used in the count
      // badge so "3 / 12 in [Audio] Audio Channels" is immediately obvious.
      const g = this.cfgbGroupFilter;
      if (g === 'all') return '';
      if (g === 'custom') return 'Custom CFs';
      if (g === 'other-trash') return 'Ungrouped (TRaSH)';
      if (g === 'other-remaining') return 'Ungrouped (after local)';
      const match = this.cfgbGroups.find(gr => gr.groupTrashId === g);
      return match ? match.name : '';
    },
    cfgbFilteredCount() {
      // Count matching the current filters — shows "M of N in category" style.
      return this.cfgbFilteredCFs().length;
    },

    cfgbSortedProfiles() {
      // Sort primarily by profile.group (int), then alphabetical by name.
      // Still exposed because cfgbBuildPayload() walks profiles in display
      // order to produce stable JSON output.
      return this.cfgbProfiles.slice().sort((a, b) => {
        if (a.group !== b.group) return a.group - b.group;
        return a.name.localeCompare(b.name);
      });
    },

    // Group profiles into the same cards the Profiles tab uses (Standard,
    // Anime, French, German, SQP, Other). Sorted by the `group` integer
    // from profile.json (ascending) with alpha tiebreak on the card name,
    // matching TRaSH's convention for profile ordering.
    cfgbGroupedProfiles() {
      const profiles = this.cfgbProfiles;
      const groups = {};
      for (const p of profiles) {
        const g = p.groupName || 'Other';
        if (!groups[g]) groups[g] = { name: g, profiles: [], groupId: p.group, minGroup: Infinity };
        groups[g].profiles.push(p);
        const gnum = typeof p.group === 'number' ? p.group : Infinity;
        if (gnum < groups[g].minGroup) groups[g].minGroup = gnum;
      }
      for (const g of Object.values(groups)) {
        g.profiles.sort((a, b) => a.name.localeCompare(b.name));
      }
      return Object.values(groups).sort((a, b) => {
        if (a.minGroup !== b.minGroup) return a.minGroup - b.minGroup;
        return a.name.localeCompare(b.name);
      });
    },

    cfgbToggleProfileGroupCard(name) {
      this.cfgbProfileGroupExpanded = {
        ...this.cfgbProfileGroupExpanded,
        [name]: !this.cfgbProfileGroupExpanded[name],
      };
    },

    cfgbIsProfileGroupExpanded(name) {
      return !!this.cfgbProfileGroupExpanded[name];
    },

    cfgbGroupAllSelected(group) {
      if (!group.profiles.length) return false;
      return group.profiles.every(p => this.cfgbSelectedProfiles[p.trashId]);
    },

    cfgbGroupSomeSelected(group) {
      return group.profiles.some(p => this.cfgbSelectedProfiles[p.trashId])
        && !this.cfgbGroupAllSelected(group);
    },

    cfgbToggleGroupAll(group, on) {
      const next = { ...this.cfgbSelectedProfiles };
      for (const p of group.profiles) {
        if (on) next[p.trashId] = true;
        else delete next[p.trashId];
      }
      this.cfgbSelectedProfiles = next;
    },

    cfgbGroupSelectedCount(group) {
      return group.profiles.filter(p => this.cfgbSelectedProfiles[p.trashId]).length;
    },

    // Per-panel clears — let the user keep profiles while starting a fresh
    // CF selection (and vice versa). Useful for building several cf-groups
    // that share the same quality-profile targets but differ in CF content.
    cfgbClearCFs() {
      this.cfgbSelectedCFs = {};
      this.cfgbRequiredCFs = {};
      this.cfgbDefaultCFs = {};
    },
    cfgbClearProfiles() {
      this.cfgbSelectedProfiles = {};
    },

    // True when every currently-selected CF already has required=true.
    // Drives the bulk required toggle's label — same click always flips
    // the whole set so the user can mass-mark then mass-unmark quickly.
    cfgbAllSelectedRequired() {
      const selectedIds = Object.keys(this.cfgbSelectedCFs).filter(id => this.cfgbSelectedCFs[id]);
      if (selectedIds.length === 0) return false;
      return selectedIds.every(id => this.cfgbRequiredCFs[id]);
    },
    cfgbToggleAllRequired(on) {
      const next = { ...this.cfgbRequiredCFs };
      for (const id of Object.keys(this.cfgbSelectedCFs)) {
        if (!this.cfgbSelectedCFs[id]) continue;
        if (on) next[id] = true;
        else delete next[id];
      }
      this.cfgbRequiredCFs = next;
    },

    // --- CF sort mode (alpha vs manual) ---

    // Returns the selected CFs in the user-chosen order. In alpha mode this
    // is just case-insensitive alpha by name. In manual mode we follow
    // cfgbCFManualOrder, appending any newly-selected CFs that haven't been
    // placed yet and skipping any whose selection was revoked. Callers use
    // this for the JSON payload AND for the manual reorder UI.
    cfgbOrderedSelectedCFs() {
      const selected = this.cfgbCFs.filter(c => this.cfgbSelectedCFs[c.trashId]);
      if (this.cfgbCFSortMode !== 'manual') {
        return selected.slice().sort((a, b) =>
          a.name.localeCompare(b.name, undefined, { sensitivity: 'base' })
        );
      }
      const byId = new Map(selected.map(c => [c.trashId, c]));
      const result = [];
      const placed = new Set();
      for (const id of this.cfgbCFManualOrder) {
        const cf = byId.get(id);
        if (cf && !placed.has(id)) {
          result.push(cf);
          placed.add(id);
        }
      }
      for (const cf of selected) {
        if (!placed.has(cf.trashId)) result.push(cf);
      }
      return result;
    },

    cfgbSetCFSortMode(mode) {
      // When switching into manual mode, seed the order from the current
      // visible alpha order so the user starts with a sensible baseline
      // rather than an empty list. Switching back to alpha leaves the manual
      // order intact in case the user flips back again.
      if (mode === 'manual' && this.cfgbCFManualOrder.length === 0) {
        this.cfgbCFManualOrder = this.cfgbOrderedSelectedCFs().map(c => c.trashId);
      }
      this.cfgbCFSortMode = mode;
    },

    cfgbMoveCF(trashId, direction) {
      // Move a single CF up (-1) or down (+1) in the manual order. Rebuilds
      // the order from the current selection so we never operate on a stale
      // list that includes since-deselected CFs.
      //
      // Kept as a public method even though the arrow-based UI that drove it
      // was replaced by drag-and-drop (cfgbCFDrop) in v2.2.0 — tests and any
      // future keyboard-accessible reorder path could still use it.
      const current = this.cfgbOrderedSelectedCFs().map(c => c.trashId);
      const idx = current.indexOf(trashId);
      if (idx < 0) return;
      const newIdx = idx + direction;
      if (newIdx < 0 || newIdx >= current.length) return;
      const tmp = current[idx];
      current[idx] = current[newIdx];
      current[newIdx] = tmp;
      this.cfgbCFManualOrder = current;
    },

    // Drag-and-drop reorder for Selected CFs (manual mode only). Mirrors
    // the sandboxDragStart/Over/Drop pattern used by Scoring Sandbox —
    // source + target tracked by trash_id (identity-safe across re-renders),
    // drop rewrites cfgbCFManualOrder from the current selection (stale
    // entries for since-deselected CFs get dropped at the same time).
    cfgbCFDragStart(trashId) {
      this.cfgbDragSrcTid = trashId;
    },
    cfgbCFDragOver(trashId) {
      this.cfgbDragOverTid = trashId;
    },
    cfgbCFDragEnd() {
      this.cfgbDragSrcTid = null;
      this.cfgbDragOverTid = null;
    },
    cfgbCFDrop(targetTid) {
      const src = this.cfgbDragSrcTid;
      this.cfgbDragSrcTid = null;
      this.cfgbDragOverTid = null;
      if (!src || src === targetTid) return;
      const current = this.cfgbOrderedSelectedCFs().map(c => c.trashId);
      const fromIdx = current.indexOf(src);
      const toIdx = current.indexOf(targetTid);
      if (fromIdx < 0 || toIdx < 0) return;
      current.splice(fromIdx, 1);
      current.splice(toIdx, 0, src);
      this.cfgbCFManualOrder = current;
    },

    cfgbResetManualOrder() {
      // Drops the manual ordering — list reverts to alpha the next time
      // manual mode is re-entered.
      this.cfgbCFManualOrder = [];
      this.cfgbCFSortMode = 'alpha';
    },

    cfgbSelectedCFCount()      { return Object.values(this.cfgbSelectedCFs).filter(Boolean).length; },
    cfgbSelectedProfileCount() { return Object.values(this.cfgbSelectedProfiles).filter(Boolean).length; },

    cfgbAllProfilesSelected() {
      if (this.cfgbProfiles.length === 0) return false;
      return this.cfgbProfiles.every(p => this.cfgbSelectedProfiles[p.trashId]);
    },

    cfgbToggleAllProfiles(on) {
      const next = {};
      if (on) this.cfgbProfiles.forEach(p => next[p.trashId] = true);
      this.cfgbSelectedProfiles = next;
    },

    cfgbCanExport() {
      return !!(this.cfgbTrashID && this.cfgbName.trim() && this.cfgbSelectedCFCount() > 0);
    },

    cfgbGenerateJSON() {
      // The preview and Download JSON share one payload builder so what the
      // user sees in the preview is exactly what lands on disk. CF order
      // preserves the /all-cfs API order (category > group > cf) which matches
      // how TRaSH organizes their own source files, so exported diffs against
      // hand-written cf-groups stay minimal. The UI shows alpha-sorted for
      // discoverability; only EXPORT preserves source order.
      return JSON.stringify(this.cfgbBuildPayload(), null, 4) + '\n';
    },

    async cfgbCopyJSON() {
      try {
        await copyToClipboard(this.cfgbGenerateJSON());
        this.cfgbCopyLabel = 'Copied!';
        setTimeout(() => { this.cfgbCopyLabel = 'Copy JSON'; }, 1500);
      } catch (e) {
        alert('Copy failed: ' + e.message);
      }
    },

    cfgbDownloadJSON() {
      // Slug: keep the category prefix (not strip it), so
      // "[Release Groups] Anime" → "release-groups-anime.json".
      // Matches TRaSH's filename convention — the brackets drop but the
      // category words remain as part of the slug, joined to the short
      // name with hyphens.
      const slug = this.cfgbName.trim().toLowerCase()
        .replace(/[\[\]]/g, ' ')         // brackets → space (preserves contents)
        .replace(/[^a-z0-9]+/g, '-')     // non-alphanumerics → hyphen
        .replace(/^-+|-+$/g, '')          // trim leading/trailing hyphens
        // Collapse immediate leading duplication. "[Audio] Audio Formats"
        // produces "audio-audio-formats"; TRaSH's convention drops the
        // repeat so it becomes "audio-formats" (matches his existing files).
        // Only triggers on exact back-to-back word equality at the start,
        // so e.g. "hdr-formats-hdr" (where the category ends with "formats"
        // but isn't doubled) stays untouched.
        .replace(/^([a-z0-9]+)-\1(-|$)/, '$1$2')
        || 'cf-group';
      const blob = new Blob([this.cfgbGenerateJSON()], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = slug + '.json';
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    },

    cfgbReset() {
      this.cfgbName = '';
      this.cfgbDescription = '';
      this.cfgbTrashID = '';
      this.cfgbDefault = false;
      this.cfgbGroup = null;
      this.cfgbCFFilter = '';
      this.cfgbGroupFilter = 'all';
      this.cfgbSelectedCFs = {};
      this.cfgbRequiredCFs = {};
      this.cfgbDefaultCFs = {};
      this.cfgbSelectedProfiles = {};
      this.cfgbEditingId = '';
      this.cfgbOriginalTrashID = '';
      this.cfgbFromTrashName = '';
      this.cfgbHashLocked = false;
      this.cfgbSavingMsg = '';
      this.cfgbCFSortMode = 'alpha';
      this.cfgbCFManualOrder = [];
    },

    // Called from the UI Reset / Discard button. Prompts when we're editing
    // an existing saved group so a single misclick can't nuke the in-flight
    // changes. cfgbReset itself stays prompt-free because it's called from
    // internal flows (cfgbLoad, cfgbSave, delete-current-editing) where a
    // prompt would be wrong.
    cfgbUIReset() {
      if (!this.cfgbEditingId) {
        this.cfgbReset();
        return;
      }
      this.confirmModal = {
        show: true,
        title: 'Discard changes',
        message: 'Discard changes to "' + (this.cfgbName || '(unnamed)') + '"?\n\nThe saved copy on disk is unaffected.',
        confirmLabel: 'Discard',
        onConfirm: () => this.cfgbReset(),
        onCancel: () => {},
      };
    },

    // Returns true when the form has "meaningful work" that would be lost
    // by a silent reset. Used to gate app-type switches + UI reset prompts.
    cfgbIsDirty() {
      if (this.cfgbEditingId) return true;
      if ((this.cfgbName || '').trim()) return true;
      if ((this.cfgbDescription || '').trim()) return true;
      if (Object.keys(this.cfgbSelectedCFs).some(k => this.cfgbSelectedCFs[k])) return true;
      if (Object.keys(this.cfgbSelectedProfiles).some(k => this.cfgbSelectedProfiles[k])) return true;
      return false;
    },

    // --- Saved cf-groups ---
    // Load an existing saved group into the form for editing. Sets
    // cfgbEditingId so Save will PUT instead of POST, keeping the same file
    // on disk. Profile/CF lookups are by trashId which is stable.
    cfgbLoadForEdit(g) {
      this.cfgbName = g.name || '';
      this.cfgbDescription = g.trash_description || '';
      this.cfgbTrashID = g.trash_id || '';
      this.cfgbDefault = g.default === 'true' || g.default === true;
      // group is integer or absent. Treat absent as null so the input field
      // stays blank rather than showing 0.
      this.cfgbGroup = (typeof g.group === 'number') ? g.group : null;
      this.cfgbCFFilter = '';
      this.cfgbGroupFilter = 'all';
      const selCFs = {}, reqCFs = {}, defCFs = {};
      for (const cf of (g.custom_formats || [])) {
        if (!cf.trash_id) continue;
        selCFs[cf.trash_id] = true;
        if (cf.required) reqCFs[cf.trash_id] = true;
        if (cf.default) defCFs[cf.trash_id] = true;
      }
      this.cfgbSelectedCFs = selCFs;
      this.cfgbRequiredCFs = reqCFs;
      this.cfgbDefaultCFs = defCFs;
      // Restore CF order: if the saved CF sequence differs from alpha, flip
      // to manual mode with that exact order. Otherwise stay in alpha.
      const savedOrder = (g.custom_formats || []).map(cf => cf.trash_id).filter(Boolean);
      // Name lookup so the comparator can never hit `undefined.localeCompare`
      // when a saved trash_id is missing from custom_formats (defensive — a
      // corrupted or older saved group might have drift).
      const nameByTid = new Map(
        (g.custom_formats || [])
          .filter(cf => cf.trash_id)
          .map(cf => [cf.trash_id, cf.name || ''])
      );
      const alphaOrder = savedOrder.slice().sort((a, b) =>
        (nameByTid.get(a) || '').localeCompare(nameByTid.get(b) || '', undefined, { sensitivity: 'base' })
      );
      const isAlpha = savedOrder.every((id, i) => id === alphaOrder[i]);
      if (isAlpha) {
        this.cfgbCFSortMode = 'alpha';
        this.cfgbCFManualOrder = [];
      } else {
        this.cfgbCFSortMode = 'manual';
        this.cfgbCFManualOrder = savedOrder;
      }
      const selProf = {};
      const include = (g.quality_profiles && g.quality_profiles.include) || {};
      for (const trashId of Object.values(include)) {
        if (trashId) selProf[trashId] = true;
      }
      this.cfgbSelectedProfiles = selProf;
      this.cfgbEditingId = g.id || '';
      // Capture the trash_id at load time + engage the hash lock so the
      // user can fix typos or tweak the name without invalidating
      // downstream references (profile includes, prior exports, synced
      // Arr profiles). The lock button in the edit banner unlocks it
      // explicitly if the user wants a fresh identity.
      this.cfgbOriginalTrashID = g.trash_id || '';
      this.cfgbHashLocked = !!g.trash_id;
      this.cfgbFromTrashName = '';
      this.cfgbSavingMsg = '';
      // Scroll the form into view so the user sees the loaded fields
      // immediately — the saved-groups list sits above the form.
      setTimeout(() => {
        const el = document.getElementById('cfgb-form-top');
        if (el && el.scrollIntoView) el.scrollIntoView({ behavior: 'smooth', block: 'start' });
      }, 0);
    },

    // Copy an upstream TRaSH cf-group into the builder as the starting point
    // for a new LOCAL group. The TRaSH repo clone is never modified — saving
    // writes to /config/custom/json/{appType}/cf-groups/ alongside the user's
    // own groups (cfgbEditingId stays empty so cfgbSave POSTs). Preserves the
    // upstream trash_id so a user who tweaks without renaming keeps a hash
    // that matches the source group until they explicitly change the name.
    cfgbLoadFromTrash(g) {
      this.cfgbName = g.name || '';
      this.cfgbDescription = g.trash_description || '';
      this.cfgbTrashID = g.trash_id || '';
      this.cfgbDefault = g.default === 'true' || g.default === true;
      this.cfgbGroup = (typeof g.group === 'number') ? g.group : null;
      this.cfgbCFFilter = '';
      this.cfgbGroupFilter = 'all';
      const selCFs = {}, reqCFs = {}, defCFs = {};
      for (const cf of (g.custom_formats || [])) {
        if (!cf.trash_id) continue;
        selCFs[cf.trash_id] = true;
        if (cf.required) reqCFs[cf.trash_id] = true;
        if (cf.default) defCFs[cf.trash_id] = true;
      }
      this.cfgbSelectedCFs = selCFs;
      this.cfgbRequiredCFs = reqCFs;
      this.cfgbDefaultCFs = defCFs;
      // Preserve TRaSH's CF ordering: flip to manual mode if the upstream
      // order isn't already alphabetical, so the copied group matches the
      // source file byte-for-byte until the user edits it.
      const srcOrder = (g.custom_formats || []).map(cf => cf.trash_id).filter(Boolean);
      const nameByTid = new Map(
        (g.custom_formats || [])
          .filter(cf => cf.trash_id)
          .map(cf => [cf.trash_id, cf.name || ''])
      );
      const alphaOrder = srcOrder.slice().sort((a, b) =>
        (nameByTid.get(a) || '').localeCompare(nameByTid.get(b) || '', undefined, { sensitivity: 'base' })
      );
      const isAlpha = srcOrder.every((id, i) => id === alphaOrder[i]);
      if (isAlpha) {
        this.cfgbCFSortMode = 'alpha';
        this.cfgbCFManualOrder = [];
      } else {
        this.cfgbCFSortMode = 'manual';
        this.cfgbCFManualOrder = srcOrder;
      }
      const selProf = {};
      const include = (g.quality_profiles && g.quality_profiles.include) || {};
      for (const trashId of Object.values(include)) {
        if (trashId) selProf[trashId] = true;
      }
      this.cfgbSelectedProfiles = selProf;
      // Key distinction from cfgbLoadForEdit: editingId stays empty so Save
      // POSTs a NEW record rather than PUTting over the TRaSH clone. Capture
      // the upstream trash_id and engage the hash lock so typo fixes or
      // minor rewording of the group name don't invalidate the ID link
      // back to the upstream group.
      this.cfgbEditingId = '';
      this.cfgbOriginalTrashID = g.trash_id || '';
      this.cfgbHashLocked = !!g.trash_id;
      this.cfgbFromTrashName = g.name || '';
      this.cfgbSavingMsg = '';
      setTimeout(() => {
        const el = document.getElementById('cfgb-form-top');
        if (el && el.scrollIntoView) el.scrollIntoView({ behavior: 'smooth', block: 'start' });
      }, 0);
    },

    // Build the TRaSH-schema object used by both Download JSON and Save.
    // Keeping this separate from cfgbGenerateJSON (which returns a string)
    // avoids round-tripping through JSON.parse on Save.
    //
    // Sort contract (per TRaSH's builder spec):
    //  - custom_formats: alpha by name, case-insensitive
    //  - quality_profiles.include: group-number ascending, alpha within
    //    same group — produced by cfgbSortedProfiles() already.
    cfgbBuildPayload() {
      const selectedCFs = this.cfgbOrderedSelectedCFs();
      // Sanitize description — users often paste the whole JSON line
      // ("...text...",) including outer quotes and the trailing comma; strip
      // them so the emitted field holds only the inner text.
      let desc = (this.cfgbDescription || '').trim();
      while (desc.endsWith(',')) desc = desc.slice(0, -1).trim();
      if (desc.startsWith('"') && desc.endsWith('"') && desc.length >= 2) {
        desc = desc.slice(1, -1);
      }
      // Match TRaSH's convention: the `default` field is emitted only when
      // the group is default-on ("true"). Opt-in groups (default unchecked)
      // omit the field entirely, as seen in optional-*.json on disk. Emitting
      // `"default": "false"` was a false diff against upstream files.
      const payload = {
        name: this.cfgbName.trim(),
        trash_id: this.cfgbTrashID,
        trash_description: desc,
      };
      if (this.cfgbDefault) payload.default = 'true';
      // Emit `group` integer when set. Position matches TRaSH's profile-JSON
      // convention: after `default`, before `custom_formats`. Absent when
      // the user leaves the input empty so JSON round-trips cleanly with
      // upstream cf-groups that don't carry the field.
      if (typeof this.cfgbGroup === 'number' && !Number.isNaN(this.cfgbGroup)) {
        payload.group = this.cfgbGroup;
      }
      payload.custom_formats = selectedCFs.map(c => {
        const entry = {
          name: c.name,
          trash_id: c.trashId,
          required: !!this.cfgbRequiredCFs[c.trashId],
        };
        // Match TRaSH's convention: per-CF `default` is emitted only when
        // true (omitted when false). Keeps generated JSON diff-friendly
        // against upstream Golden Rule files.
        if (this.cfgbDefaultCFs[c.trashId]) entry.default = true;
        return entry;
      });
      payload.quality_profiles = {
        include: this.cfgbSortedProfiles()
          .filter(p => this.cfgbSelectedProfiles[p.trashId])
          .reduce((acc, p) => { acc[p.name] = p.trashId; return acc; }, {}),
      };
      return payload;
    },

    async cfgbSave() {
      if (!this.cfgbCanExport()) return;
      // The hash-drift prompt was removed in favour of the explicit hash
      // lock toggle in the edit banner — the user makes the keep-vs-
      // regenerate decision visibly while editing rather than being
      // surprised by a modal at save time.
      return this._cfgbDoSave();
    },

    // Performs the actual POST (new) or PUT (existing local). The hash is
    // whatever cfgbTrashID holds — the lock toggle decides whether that's
    // the original (locked) or the MD5 of the current name (unlocked).
    async _cfgbDoSave() {
      const appType = this.activeAppType;
      const payload = this.cfgbBuildPayload();
      const editing = !!this.cfgbEditingId;
      const url = editing
        ? '/api/cf-groups/' + appType + '/' + encodeURIComponent(this.cfgbEditingId)
        : '/api/cf-groups/' + appType;
      const method = editing ? 'PUT' : 'POST';
      try {
        const resp = await fetch(url, {
          method,
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
        if (!resp.ok) {
          const text = await resp.text();
          throw new Error('HTTP ' + resp.status + ': ' + text);
        }
        const saved = await resp.json();
        // Refresh the saved-list and stay on the form. For creates (incl.
        // TRaSH copies), switch into edit-mode for the new record so the
        // next Save is a PUT.
        //
        // Hash lock on save: if this was a fresh-new group (no prior
        // baseline), engage the lock now that there's a saved identity.
        // If it was an edit, PRESERVE the user's explicit lock choice —
        // they deliberately unlocked/locked and shouldn't be surprised
        // by a silent state reset. A user who saved with the lock off
        // (regenerating hash from a new name) expects to stay unlocked
        // and keep iterating on the name.
        const wasFreshNew = !this.cfgbOriginalTrashID;
        await this.cfgbRefreshSaved();
        this.cfgbEditingId = saved.id || this.cfgbEditingId;
        this.cfgbOriginalTrashID = saved.trash_id || this.cfgbTrashID;
        if (wasFreshNew) {
          this.cfgbHashLocked = !!this.cfgbOriginalTrashID;
        }
        this.cfgbFromTrashName = '';
        this.cfgbSavingOk = true;
        this.cfgbSavingMsg = editing ? 'Updated.' : 'Saved.';
        setTimeout(() => { if (this.cfgbSavingMsg === 'Updated.' || this.cfgbSavingMsg === 'Saved.') this.cfgbSavingMsg = ''; }, 2000);
      } catch (e) {
        console.error('cfgbSave failed:', e);
        this.cfgbSavingOk = false;
        this.cfgbSavingMsg = 'Save failed: ' + e.message;
      }
    },

    cfgbDelete(g) {
      if (!g || !g.id) return;
      this.confirmModal = {
        show: true,
        title: 'Delete saved cf-group',
        message: 'Delete saved cf-group "' + (g.name || g.id) + '"?\n\nThe file is removed from /config/custom/json/' + g.appType + '/cf-groups/.\nExported .json files on disk are unaffected.',
        confirmLabel: 'Delete',
        onConfirm: () => this._cfgbDeleteConfirmed(g),
        onCancel: () => {},
      };
    },
    async _cfgbDeleteConfirmed(g) {
      // Guard against a double-fire: modal's onConfirm is a simple click
      // handler, so a quick Delete→Confirm→Delete→Confirm sequence against
      // two different groups could have their second DELETE land before
      // the first finished. The flag blocks any overlapping delete.
      if (this.cfgbDeleting) return;
      this.cfgbDeleting = true;
      try {
        const resp = await fetch('/api/cf-groups/' + g.appType + '/' + encodeURIComponent(g.id), { method: 'DELETE' });
        if (!resp.ok) {
          const text = await resp.text();
          throw new Error('HTTP ' + resp.status + ': ' + text);
        }
        await this.cfgbRefreshSaved();
        if (this.cfgbEditingId === g.id) this.cfgbReset();
        this.cfgbSavingOk = true;
        this.cfgbSavingMsg = 'Deleted.';
        setTimeout(() => { if (this.cfgbSavingMsg === 'Deleted.') this.cfgbSavingMsg = ''; }, 2000);
      } catch (e) {
        console.error('cfgbDelete failed:', e);
        this.cfgbSavingOk = false;
        this.cfgbSavingMsg = 'Delete failed: ' + e.message;
      } finally {
        this.cfgbDeleting = false;
      }
    },

    async cfgbRefreshSaved() {
      const appType = this.activeAppType;
      try {
        const resp = await fetch('/api/cf-groups/' + appType);
        if (!resp.ok) throw new Error('HTTP ' + resp.status);
        const list = await resp.json();
        this.cfgbSavedGroups = Array.isArray(list) ? list : [];
        // Re-merge local groups into dropdown + CF memberships so the
        // just-saved/deleted group shows up (or vanishes) without needing
        // a full /all-cfs refetch or losing form state.
        this.cfgbApplyLocalGroups();
      } catch (e) {
        console.error('cfgbRefreshSaved failed:', e);
      }
    },

    cfgbSelectedCustomCFCount() {
      // How many selected CFs are user-custom (IDs starting with "custom:").
      // Surfaced in a banner next to Download JSON as a warning when the
      // exported file is meant for TRaSH-Guides contribution — custom IDs
      // don't resolve in the public repo.
      let n = 0;
      for (const cf of this.cfgbCFs) {
        if (cf.isCustom && this.cfgbSelectedCFs[cf.trashId]) n++;
      }
      return n;
    },

  },
};
