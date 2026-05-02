export default {
  state: {},
  methods: {
    // --- Import ---
    async loadImportedProfiles(appType) {
      try {
        const r = await fetch(`/api/import/${appType}/profiles`);
        if (r.ok) {
          const data = await r.json();
          this.importedProfiles = { ...this.importedProfiles, [appType]: data };
        }
      } catch (e) { /* ignore */ }
    },

    handleImportFiles(fileList) {
      if (!fileList || fileList.length === 0) return;
      for (const file of fileList) {
        if (!file.name.match(/\.(?:ya?ml|json)$/i)) continue;
        const reader = new FileReader();
        const name = file.name;
        reader.onload = (e) => {
          // Avoid duplicates
          if (!this.importFiles.find(f => f.name === name)) {
            this.importFiles.push({ name, content: e.target.result });
          }
        };
        reader.readAsText(file);
      }
    },

    handleImportIncludeFiles(fileList) {
      if (!fileList || fileList.length === 0) return;
      for (const file of fileList) {
        if (!file.name.match(/\.ya?ml$/i)) continue;
        const reader = new FileReader();
        const name = file.name;
        reader.onload = (e) => {
          if (!this.importIncludeFiles.find(f => f.name === name)) {
            this.importIncludeFiles.push({ name, content: e.target.result });
          }
        };
        reader.readAsText(file);
      }
    },

    async submitImport() {
      this.importingProfile = true;
      this.importResult = '';
      this.importError = false;

      // Collect YAML contents to import
      const yamls = [];
      if (this.importMode === 'paste') {
        yamls.push({ name: 'pasted', content: this.importYaml });
      } else {
        for (const f of this.importFiles) {
          yamls.push({ name: f.name, content: f.content });
        }
      }

      // If includes are provided, send them alongside for backend merge
      const includeFiles = this.importIncludeFiles.length > 0
        ? this.importIncludeFiles.map(f => ({ name: f.name, content: f.content }))
        : null;

      let totalImported = 0;
      let totalSkipped = 0;
      const errors = [];
      const renamed = [];

      for (const y of yamls) {
        try {
          const r = await fetch('/api/import/profile', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ yaml: y.content, name: yamls.length === 1 ? this.importNameOverride.trim() : '', appType: this.showImportModal, includes: includeFiles })
          });
          const data = await r.json();
          if (!r.ok) {
            errors.push(`${y.name}: ${data.error || 'failed'}`);
          } else {
            totalImported += data.imported || 0;
            totalSkipped += data.skipped || 0;
            // Check for renamed profiles (name collision)
            if (data.profiles) {
              for (const p of data.profiles) {
                const origName = yamls.length === 1 && this.importNameOverride.trim() ? this.importNameOverride.trim() : null;
                if (origName && p.name !== origName) {
                  renamed.push(`"${origName}" → "${p.name}"`);
                } else if (p.name && p.name.match(/\(\d+\)$/)) {
                  renamed.push(`Saved as "${p.name}" (name already existed)`);
                }
                // Golden Rule variant: ask per profile
                if (!p.trashProfileId && p.formatItems && !p.variantGoldenRule) {
                  const grRadarr = ['dc98083864ea246d05a42df0d05f81cc', '839bea857ed2c0a8e084f3cbdbd65ecb'];
                  const grSonarr = ['47435ece6b99a0b477caf360e79ba0bb', '9b64dff695c2115facf1b6ea59c9bd07'];
                  const grIds = p.appType === 'sonarr' ? grSonarr : grRadarr;
                  if (grIds.some(id => id in p.formatItems)) {
                    const variant = await new Promise(resolve => {
                      this.confirmModal = {
                        show: true, html: true,
                        title: 'Golden Rule — HD or UHD?',
                        message: '<strong>"' + p.name + '"</strong> (' + p.appType.charAt(0).toUpperCase() + p.appType.slice(1) + ') contains Golden Rule CFs but no TRaSH profile reference.<br><br>• <strong>HD</strong> — for 720p/1080p profiles<br>• <strong>UHD</strong> — for 2160p/4K profiles',
                        confirmLabel: 'UHD',
                        cancelLabel: 'HD',
                        onConfirm: () => resolve('UHD'),
                        onCancel: () => resolve('HD')
                      };
                    });
                    await fetch('/api/import/profiles/' + p.id, {
                      method: 'PUT',
                      headers: { 'Content-Type': 'application/json' },
                      body: JSON.stringify({ variantGoldenRule: variant })
                    });
                  }
                }
              }
            }
          }
        } catch (e) {
          errors.push(`${y.name}: ${e.message}`);
        }
      }

      this.loadImportedProfiles('radarr');
      this.loadImportedProfiles('sonarr');

      // Close modal and show result as toast
      this.showImportModal = false;
      this.importYaml = '';
      this.importFiles = [];
      this.importIncludeFiles = [];
      this.importHasIncludes = false;
      this.importNameOverride = '';
      this.importResult = '';

      if (errors.length > 0) {
        let msg = errors.join('\n');
        if (totalImported > 0) msg = `Imported ${totalImported} profile(s), but errors occurred:\n` + msg;
        this.showToast(msg, 'error', 8000);
      } else {
        let msg = `Imported ${totalImported} profile(s)`;
        if (totalSkipped > 0) msg += `, ${totalSkipped} skipped (already exist)`;
        if (renamed.length > 0) msg += '\n' + renamed.join('\n');
        this.showToast(msg, 'info', 8000);
      }

      this.importingProfile = false;
    },

    async deleteImportedProfile(id, appType) {
      const confirmed = await new Promise(resolve => {
        this.confirmModal = { show: true, title: 'Delete Profile', message: 'Delete this imported profile?', confirmLabel: 'Delete', onConfirm: () => resolve(true), onCancel: () => resolve(false) };
      });
      if (!confirmed) return;
      try {
        await fetch(`/api/import/profiles/${id}`, { method: 'DELETE' });
        this.loadImportedProfiles(appType);
      } catch (e) { /* ignore */ }
    },

    async deleteAllImportedProfiles(appType) {
      const profiles = this.importedProfiles[appType] || [];
      for (const p of profiles) {
        try {
          await fetch(`/api/import/profiles/${p.id}`, { method: 'DELETE' });
        } catch (e) { /* ignore */ }
      }
      this.loadImportedProfiles(appType);
    },

    async openImportedProfileDetail(appType, profile) {
      this.syncPlan = null;
      this.syncResult = null;
      this.showProfileInfo = false;
      this.selectedOptionalCFs = {};

      // If this imported profile has a trashProfileId, use TRaSH detail endpoint
      // for proper categorization (Required, Optional groups, descriptions, etc.)
      if (profile.trashProfileId) {
        // Need an instance to call the API — use first of this type
        const inst = this.instancesOfType(appType)[0];
        if (inst) {
          this.profileDetail = { instance: inst, profile: { name: profile.name, trashId: profile.trashProfileId }, detail: null };
          try {
            const r = await fetch(`/api/trash/${appType}/profiles/${profile.trashProfileId}`);
            if (r.ok) {
              const detail = await r.json();
              // Overlay imported profile settings onto TRaSH detail
              detail.imported = true;
              detail.importedRaw = profile;
              // Use imported profile's settings (may differ from TRaSH defaults)
              detail.profile.upgradeAllowed = profile.upgradeAllowed;
              detail.profile.cutoff = profile.cutoff || detail.profile.cutoff;
              detail.profile.minFormatScore = profile.minFormatScore;
              detail.profile.cutoffFormatScore = profile.cutoffScore || detail.profile.cutoffFormatScore;
              detail.profile.minUpgradeFormatScore = profile.minUpgradeFormatScore;
              detail.profile.language = profile.language || detail.profile.language;
              if (profile.scoreSet) detail.profile.scoreSet = profile.scoreSet;
              this.profileDetail = { instance: inst, profile: { name: profile.name, trashId: profile.trashProfileId }, detail };
              this.initDetailSections(detail);
              this.initSelectedCFs(detail);
              return;
            }
          } catch (e) { console.error('loadImportedProfileDetail:', e); }
        }
      }

      // Fallback: use imported profile detail endpoint (builds TRaSH groups from CF membership)
      try {
        const r = await fetch(`/api/import/profiles/${profile.id}/detail`);
        if (r.ok) {
          const detail = await r.json();
          this.profileDetail = {
            instance: { type: profile.appType, name: profile.appType },
            profile: { name: profile.name },
            detail
          };
          this.initDetailSections(detail);
          this.initSelectedCFs(detail);
          return;
        }
      } catch (e) { console.error('loadImportedProfileDetail:', e); }
    },

    openExportModalFromList(appType, profile) {
      this.exportSource = profile;
      this.openExportModal();
    },

    async openExportModal() {
      if (!this.exportSource) {
        this.exportSource = this.profileDetail?.detail?.importedRaw;
      }
      // Ensure CF group data is loaded for v8 YAML export
      const appType = this.exportSource?.appType;
      if (appType && !this.cfBrowseData[appType]) {
        await this.loadCFBrowse(appType);
      }
      // Default to TRaSH JSON unless contributor mode is on (Recyclarr YAML
      // tab is hidden when CLONARR_DEV_FEATURES is unset, so opening on it
      // would leave the modal showing nothing).
      this.exportTab = this.config?.devFeatures ? 'yaml' : 'trash';
      this.exportCopied = false;
      this.generateExport();
      this.showExportModal = true;
    },

    closeExportModal() {
      this.showExportModal = false;
      this.exportSource = null;
    },

    generateExport() {
      const p = this.exportSource;
      if (!p) return;
      if (this.exportTab === 'yaml') {
        this.exportContent = this.generateRecyclarrYAML(p);
        this.exportGroupIncludes = [];
      } else if (this.exportTab === 'trash') {
        this.exportContent = this.generateTrashJSON(p);
        this.exportGroupIncludes = this.generateGroupIncludes(p);
      }
    },

    generateRecyclarrYAML(p) {
      // v8-only: profile must be guide-backed (have trashProfileId). v7 export
      // was dropped in v2.5 — Recyclarr removed v7 includes upstream and we
      // focus only on v8 going forward. Custom-built profiles without a
      // trashProfileId can't currently be exported to Recyclarr YAML.
      if (!p.trashProfileId) {
        return `# This profile was custom-built and isn't linked to a TRaSH guide entry,\n# so it can't be exported as Recyclarr YAML. Use the TRaSH JSON tab\n# instead — that format works for any clonarr profile.`;
      }

      const appType = p.appType || 'radarr';
      const lines = [];
      lines.push(`${appType}:`);
      lines.push(`  exported-profile:`);
      if (p.qualityType) {
        lines.push(`    quality_definition:`);
        lines.push(`      type: ${p.qualityType}`);
      }
      lines.push(``);
      lines.push(`    quality_profiles:`);
      // v8: reference profile by trash_id — guide handles qualities, cutoff, scores
      lines.push(`      - trash_id: ${p.trashProfileId}  # ${p.name}`);
      // Name override if user renamed the profile
      const trashProfiles = this.trashProfiles?.[p.appType || 'radarr'] || [];
      const origProfile = trashProfiles.find(tp => tp.trashId === p.trashProfileId);
      if (origProfile && p.name && p.name !== origProfile.name) {
        lines.push(`        name: ${p.name}`);
      }
      lines.push(`        reset_unmatched_scores:`);
      lines.push(`          enabled: true`);

      this._generateV8CFGroups(p, lines);

      return lines.join('\n');
    },

    _generateV8CFGroups(p, lines) {
      // In v8, guide-backed profiles with score_set get scores automatically from TRaSH data.
      // The YAML only needs custom_format_groups to specify which optional groups to include.
      const appType = p.appType || 'radarr';
      const groups = this.cfBrowseData[appType]?.groups || [];

      const profileCFs = new Set(Object.keys(p.formatItems || {}));

      // Map CF trash_id → group
      const cfToGroup = {};
      for (const g of groups) {
        for (const cf of (g.custom_formats || [])) {
          cfToGroup[cf.trash_id] = { groupId: g.trash_id, groupName: g.name, isDefault: g.default === 'true' };
        }
      }

      // Classify: which groups have CFs selected in this profile?
      const groupedCFs = {}; // groupId → [tids]
      for (const tid of profileCFs) {
        const gi = cfToGroup[tid];
        if (gi) {
          if (!groupedCFs[gi.groupId]) groupedCFs[gi.groupId] = [];
          groupedCFs[gi.groupId].push(tid);
        }
      }

      // Baseline CFs = what the TRaSH profile defines by default (core + default groups)
      const baseline = new Set(p.baselineCFs || []);

      // Build add entries for groups that need explicit configuration
      const addEntries = [];
      // Build skip list for default groups entirely excluded
      const skipEntries = [];

      // Helper: check if a group is linked to this profile via quality_profiles.include
      const profileTrashId = p.trashProfileId || '';
      const isGroupForProfile = (g) => {
        const include = g.quality_profiles?.include || {};
        return Object.values(include).includes(profileTrashId);
      };

      for (const g of groups) {
        const gid = g.trash_id;
        const allGroupCFs = (g.custom_formats || []).map(cf => cf.trash_id);
        const selected = groupedCFs[gid] || [];

        if (g.default === 'true') {
          // Only consider default groups that are linked to this profile
          if (!isGroupForProfile(g)) continue;
          if (selected.length === 0) {
            // No CFs selected → skip entire default group
            skipEntries.push({ groupId: gid, groupName: g.name });
          } else if (selected.length < allGroupCFs.length) {
            // Partial selection → add with select to override default
            addEntries.push({ groupId: gid, groupName: g.name, select: selected });
          }
          // All selected → no entry needed (default behavior)
        } else {
          // Non-default group: must be explicitly added if user selected CFs from it
          if (selected.length === 0) continue;
          // Skip if all selected CFs are already in the baseline (TRaSH profile definition)
          if (selected.every(tid => baseline.has(tid))) continue;
          const allSelected = allGroupCFs.every(tid => profileCFs.has(tid));
          if (allSelected) {
            addEntries.push({ groupId: gid, groupName: g.name, selectAll: true });
          } else {
            addEntries.push({ groupId: gid, groupName: g.name, select: selected });
          }
        }
      }

      if (addEntries.length > 0 || skipEntries.length > 0) {
        lines.push(``);
        lines.push(`    custom_format_groups:`);
        if (addEntries.length > 0) {
          lines.push(`      add:`);
          for (const entry of addEntries) {
            lines.push(`        - trash_id: ${entry.groupId}  # ${entry.groupName}`);
            if (entry.selectAll) {
              lines.push(`          select_all: true`);
            } else if (entry.select) {
              lines.push(`          select:`);
              for (const tid of entry.select) {
                const comment = (p.formatComments || {})[tid];
                lines.push(`            - ${tid}${comment ? '  # ' + comment : ''}`);
              }
            }
          }
        }
        if (skipEntries.length > 0) {
          lines.push(`      skip:`);
          for (const entry of skipEntries) {
            lines.push(`        - ${entry.groupId}  # ${entry.groupName}`);
          }
        }
      }

      // In v8, score_set handles ALL TRaSH CF scores automatically.
      // Only emit custom_formats for user-created custom CFs (not from TRaSH guides).
      const customCFs = {};
      for (const [tid, score] of Object.entries(p.formatItems || {})) {
        if (tid.startsWith('custom:')) {
          const key = String(score);
          if (!customCFs[key]) customCFs[key] = [];
          customCFs[key].push(tid);
        }
      }
      if (Object.keys(customCFs).length > 0) {
        lines.push(``);
        lines.push(`    custom_formats:`);
        const sortedScores = Object.keys(customCFs).sort((a, b) => Number(b) - Number(a));
        for (const score of sortedScores) {
          lines.push(`      - trash_ids:`);
          for (const tid of customCFs[score]) {
            const comment = (p.formatComments || {})[tid];
            lines.push(`          - ${tid}${comment ? '  # ' + comment : ''}`);
          }
          lines.push(`        assign_scores_to:`);
          lines.push(`          - trash_id: ${p.trashProfileId}`);
          lines.push(`            score: ${score}`);
        }
      }
    },

    generateTrashJSON(p) {
      // TRaSH format: formatItems is { "CF Name": "trash_id" }
      // Only formatItemCFs go in formatItems — group CFs belong in CF group files
      const formatItemSet = new Set(Object.keys(p.formatItemCFs || {}));
      const coreCFs = new Set(p.coreCFIds || []);
      const requiredSet = new Set(p.requiredCFs || []);

      // Collect eligible trash IDs
      const eligibleTids = [];
      for (const [tid] of Object.entries(p.formatItems || {})) {
        if (formatItemSet.size > 0) {
          if (!formatItemSet.has(tid)) continue;
        } else if (requiredSet.size > 0 && !requiredSet.has(tid)) continue;
        else if (requiredSet.size === 0 && coreCFs.size > 0 && !coreCFs.has(tid)) continue;
        eligibleTids.push(tid);
      }

      // Sort formatItems to match TRaSH's convention:
      // 1. Grouped CFs (Audio, HQ Groups) — by group order, score descending within
      // 2. Tiers (Remux/HD Bluray/WEB) — by name (natural order: 01, 02, 03)
      // 3. Repack — fixed order: Proper, 2, 3
      // 4. Unwanted — fixed order matching TRaSH convention
      // 5. Misc (10 bit, AV1, etc.)
      // 6. Resolution (1080p, 2160p, 720p) — last
      const comments = p.formatComments || {};
      const scores = p.formatItems || {};

      // Build CF group membership from pbCategories
      const cfGroupIdx = {};
      let gIdx = 0;
      for (const cat of (this.pbCategories || [])) {
        for (const g of (cat.groups || [])) {
          if (!g.groupTrashId) continue;
          for (const cf of (g.cfs || [])) {
            cfGroupIdx[cf.trashId] = gIdx;
          }
          gIdx++;
        }
      }

      // Assign sort bucket to each CF
      const unwantedOrder = ['BR-DISK', 'Generated Dynamic HDR', 'LQ', 'LQ (Release Title)',
        'x265 (HD)', '3D', 'Upscaled', 'Extras', 'Sing-Along Versions'];
      const repackOrder = ['Repack/Proper', 'Repack2', 'Repack3'];
      const resolutionOrder = ['720p', '1080p', '2160p'];
      const getBucket = (tid) => {
        const name = comments[tid] || tid;
        if (resolutionOrder.includes(name)) return 5; // resolution — always last
        if (cfGroupIdx[tid] !== undefined) return 0; // grouped CF
        if (/Tier \d/.test(name) || name === 'BHDStudio' || name === 'hallowed') return 1; // tiers/hq groups
        if (repackOrder.includes(name)) return 2;
        if (unwantedOrder.includes(name) || (scores[tid] ?? 0) <= -10000) return 3;
        return 4; // misc
      };

      eligibleTids.sort((a, b) => {
        const ba = getBucket(a), bb = getBucket(b);
        if (ba !== bb) return ba - bb;

        const nameA = comments[a] || a, nameB = comments[b] || b;

        // Bucket 0: grouped CFs — by group index, then score descending
        if (ba === 0) {
          const gi = cfGroupIdx[a] ?? 999, gj = cfGroupIdx[b] ?? 999;
          if (gi !== gj) return gi - gj;
          const sa = scores[a] ?? 0, sb = scores[b] ?? 0;
          if (sa !== sb) return sb - sa;
          return nameA.localeCompare(nameB);
        }
        // Bucket 1: tiers — score descending
        if (ba === 1) {
          const sa = scores[a] ?? 0, sb = scores[b] ?? 0;
          if (sa !== sb) return sb - sa;
          return nameA.localeCompare(nameB);
        }
        // Bucket 2: repack — fixed order
        if (ba === 2) return repackOrder.indexOf(nameA) - repackOrder.indexOf(nameB);
        // Bucket 3: unwanted — fixed order, then alphabetical for unknowns
        if (ba === 3) {
          const ia = unwantedOrder.indexOf(nameA), ib = unwantedOrder.indexOf(nameB);
          if (ia >= 0 && ib >= 0) return ia - ib;
          if (ia >= 0) return -1;
          if (ib >= 0) return 1;
          return nameA.localeCompare(nameB);
        }
        // Bucket 4: misc — score descending
        if (ba === 4) {
          const sa = scores[a] ?? 0, sb = scores[b] ?? 0;
          if (sa !== sb) return sb - sa;
          return nameA.localeCompare(nameB);
        }
        // Bucket 5: resolution — fixed order (720p, 1080p, 2160p)
        return resolutionOrder.indexOf(nameA) - resolutionOrder.indexOf(nameB);
      });

      const formatItems = {};
      for (const tid of eligibleTids) {
        const name = (p.formatComments || {})[tid] || tid;
        formatItems[name] = tid;
      }

      // Build items array (qualities) — match TRaSH format exactly
      const items = (p.qualities || []).map(q => {
        const item = { name: q.name, allowed: q.allowed !== false };
        if (q.items && q.items.length > 0) {
          item.items = q.items;
        }
        return item;
      });

      // Build profile matching TRaSH's official JSON structure
      const trashProfile = {
        trash_id: p.trashProfileId || '',
        name: p.name,
      };
      if (p.scoreSet) trashProfile.trash_score_set = p.scoreSet;
      if (p.trashDescription) {
        trashProfile.trash_description = p.trashDescription;
      }
      trashProfile.group = p.groupNum || 99;
      trashProfile.upgradeAllowed = p.upgradeAllowed || false;
      trashProfile.cutoff = p.cutoff || '';
      trashProfile.minFormatScore = p.minFormatScore ?? 0;
      trashProfile.cutoffFormatScore = p.cutoffScore ?? 10000;
      trashProfile.minUpgradeFormatScore = p.minUpgradeFormatScore ?? 0;
      // Sonarr profiles don't have a language field — the UI removed it
      // from the General section, but the export was still emitting
      // `"language": "Original"` for Sonarr JSON. TRaSH's actual Sonarr
      // profile files omit the key entirely; match that convention.
      if ((p.appType || 'radarr') === 'radarr') {
        trashProfile.language = p.language || 'Original';
      }
      trashProfile.items = items;
      trashProfile.formatItems = formatItems;

      // No cfGroupIncludes — TRaSH format doesn't use it
      // Group includes are handled via group files, not profile JSON

      // Custom JSON formatting to match TRaSH style:
      // - items array with inline sub-arrays and compact single-line entries
      let json = JSON.stringify(trashProfile, null, 2);

      // Reformat the "items" array to match TRaSH style
      // Replace multi-line items arrays with inline: ["item1", "item2"]
      json = json.replace(/"items": \[\n\s+("(?:[^"]+)"(?:,\n\s+"(?:[^"]+)")*)\n\s+\]/g, (match, inner) => {
        const vals = inner.replace(/\n\s+/g, ' ');
        return '"items": [' + vals + ']';
      });
      // Compact simple quality entries onto single lines
      json = json.replace(/\{\n\s+"name": "([^"]+)",\n\s+"allowed": (true|false)\n\s+\}/g,
        '{ "name": "$1", "allowed": $2 }');

      return json;
    },

    // Generate group include snippets for enabled groups
    generateGroupIncludes(p) {
      const enabledGroups = p.enabledGroups || {};
      if (Object.keys(enabledGroups).length === 0) return [];
      const snippets = [];
      // Use pbCategories (in builder) or raw groups from cfBrowseData
      if (this.pbCategories?.length > 0) {
        for (const cat of this.pbCategories) {
          for (const g of (cat.groups || [])) {
            if (!g.groupTrashId || !enabledGroups[g.groupTrashId]) continue;
            snippets.push({
              groupName: g.name, groupTrashId: g.groupTrashId,
              profileName: p.name, profileTrashId: p.trashProfileId || '',
              snippet: `"${p.name}": "${p.trashProfileId || 'GENERATE_ID'}"`,
            });
          }
        }
      } else {
        // Fallback: raw group data from cfBrowseData
        const groups = this.cfBrowseData[p.appType || 'radarr']?.groups || [];
        for (const g of groups) {
          const gid = g.trash_id;
          if (!gid || !enabledGroups[gid]) continue;
          snippets.push({
            groupName: g.name, groupTrashId: gid,
            profileName: p.name, profileTrashId: p.trashProfileId || '',
            snippet: `"${p.name}": "${p.trashProfileId || 'GENERATE_ID'}"`,
          });
        }
      }
      return snippets;
    },

  },
};
