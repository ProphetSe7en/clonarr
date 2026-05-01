export default {
  state: {
    autoSyncSettings: { enabled: false, paused: false },
    autoSyncRules: [],
    autoSyncRuleForSync: null,
  },

  methods: {
    async loadAutoSyncSettings() {
      try {
        const r = await fetch('/api/auto-sync/settings');
        if (r.ok) this.autoSyncSettings = await r.json();
      } catch (e) { console.error('loadAutoSyncSettings:', e); }
    },

    async saveAutoSyncSettings() {
      try {
        await fetch('/api/auto-sync/settings', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(this.autoSyncSettings)
        });
      } catch (e) { console.error('saveAutoSyncSettings:', e); }
    },

    // Toggle the global pause flag. Scheduled syncs are skipped while manual
    // sync actions remain available.
    async setAutoSyncPaused(paused) {
      const previous = this.autoSyncSettings;
      this.autoSyncSettings = { ...this.autoSyncSettings, paused };
      try {
        const r = await fetch('/api/auto-sync/settings', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ paused })
        });
        if (!r.ok) {
          this.autoSyncSettings = previous;
          throw new Error('HTTP ' + r.status);
        }
        this.showToast(paused ? 'Auto-sync paused — manual syncs still work' : 'Auto-sync resumed', paused ? 'warning' : 'success', 4000);
      } catch (e) {
        console.error('setAutoSyncPaused:', e);
        this.autoSyncSettings = previous;
        this.showToast('Failed to update auto-sync state', 'error', 6000);
      }
    },

    async loadAutoSyncRules() {
      try {
        const r = await fetch('/api/auto-sync/rules');
        if (r.ok) this.autoSyncRules = await r.json();
      } catch (e) { console.error('loadAutoSyncRules:', e); }
    },

    findAutoSyncRule(instanceId, arrProfileId) {
      const aid = parseInt(arrProfileId) || 0;
      if (aid > 0) {
        return this.autoSyncRules.find(r => r.instanceId === instanceId && r.arrProfileId === aid) || null;
      }
      return null;
    },

    async setRuleSyncError(instanceId, arrProfileId, error) {
      // Backend now manages LastSyncError + Enabled state directly:
      // handleApply sets both on errors (auto-disable + record message)
      // and clears LastSyncError on a clean success. Just refresh the
      // cache so the UI reflects backend state. PUT'ing the whole rule
      // here used to clobber the auto-disable with a stale enabled=true
      // from the local cache — that's why "Sync All" + individual
      // sync from history wasn't surfacing the disabled state.
      try {
        await this.loadAutoSyncRules();
      } catch (e) { /* best effort */ }
    },

    updateAutoSyncRuleForSync() {
      this.autoSyncRuleForSync = this.findAutoSyncRule(
        this.syncForm.instanceId,
        this.syncForm.arrProfileId
      );
      // Populate behavior from existing auto-sync rule.
      if (this.autoSyncRuleForSync?.behavior) {
        this.syncForm.behavior = { ...this.syncForm.behavior, ...this.autoSyncRuleForSync.behavior };
      }
    },

    async toggleAutoSyncForProfile(enabled) {
      this.debugLog('UI', `Auto-sync: ${enabled ? 'enabled' : 'disabled'} for "${this.syncForm.profileName}" → ${this.syncForm.instanceName}`);
      const existing = this.autoSyncRuleForSync;
      if (existing) {
        // Toggle only enabled. Saved CF/override customizations are refreshed
        // through Apply/Edit Sync Settings, not from modal defaults.
        const updated = { ...existing, enabled: enabled };
        try {
          await fetch(`/api/auto-sync/rules/${existing.id}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(updated)
          });
          await this.loadAutoSyncRules();
          this.updateAutoSyncRuleForSync();
        } catch (e) { console.error('toggleAutoSyncForProfile:', e); }
      } else if (enabled) {
        const syncBody = this.buildSyncBody();
        const rule = {
          enabled: true,
          instanceId: this.syncForm.instanceId,
          profileSource: this.syncForm.importedProfileId ? 'imported' : 'trash',
          trashProfileId: this.syncForm.profileTrashId || '',
          importedProfileId: this.syncForm.importedProfileId || '',
          arrProfileId: parseInt(this.syncForm.arrProfileId) || 0,
          selectedCFs: this.getAllSelectedCFIds(),
          behavior: this.syncForm.behavior,
          overrides: syncBody.overrides || null,
          scoreOverrides: syncBody.scoreOverrides || null,
          qualityOverrides: syncBody.qualityOverrides || null,
          qualityStructure: syncBody.qualityStructure || null
        };
        try {
          const r = await fetch('/api/auto-sync/rules', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(rule)
          });
          if (r.ok) {
            await this.loadAutoSyncRules();
            this.updateAutoSyncRuleForSync();
          }
        } catch (e) { console.error('createAutoSyncRule:', e); }
      }
    },

    async toggleAutoSyncRule(rule) {
      const wasEnabled = rule.enabled;
      rule.enabled = !rule.enabled;
      try {
        await fetch(`/api/auto-sync/rules/${rule.id}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(rule)
        });
        await this.loadAutoSyncRules();
        // Force reactivity update on sync history (toggle text depends on rules).
        const instId = rule.instanceId;
        if (this.syncHistory[instId]) {
          this.syncHistory = { ...this.syncHistory };
        }
        // If just enabled, run sync immediately instead of waiting for next pull.
        if (!wasEnabled && rule.enabled) {
          const sh = (this.syncHistory[instId] || []).find(s => s.arrProfileId === rule.arrProfileId);
          if (sh) {
            const inst = this.instances.find(i => i.id === instId);
            if (inst) {
              await this.quickSync(inst, sh);
            }
          }
        }
      } catch (e) { console.error('toggleAutoSyncRule:', e); }
    },

    async deleteAutoSyncRule(rule) {
      const confirmed = await new Promise(resolve => {
        this.confirmModal = { show: true, title: 'Remove Auto-Sync Rule', message: 'Remove this auto-sync rule?', confirmLabel: 'Remove', onConfirm: () => resolve(true), onCancel: () => resolve(false) };
      });
      if (!confirmed) return;
      try {
        await fetch(`/api/auto-sync/rules/${rule.id}`, { method: 'DELETE' });
        await this.loadAutoSyncRules();
      } catch (e) { console.error('deleteAutoSyncRule:', e); }
    },
  },
};
