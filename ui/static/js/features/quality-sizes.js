export default {
  state: {},
  methods: {
    async loadQualitySizes(appType) {
      try {
        const r = await fetch(`/api/trash/${appType}/quality-sizes`);
        if (r.ok) {
          const data = await r.json();
          // Sort: movie first, then sqp variants, then anime
          const order = { movie: 0, series: 0, 'sqp-streaming': 1, 'sqp-uhd': 2, anime: 3 };
          data.sort((a, b) => (order[a.type] ?? 99) - (order[b.type] ?? 99));
          this.qualitySizesPerApp = { ...this.qualitySizesPerApp, [appType]: data };
        }
      } catch (e) { /* ignore */ }
    },

    getQualitySizes(appType) {
      return this.qualitySizesPerApp[appType] || [];
    },


    getSelectedQS(appType) {
      const all = this.getQualitySizes(appType);
      const idx = this.selectedQSType[appType] || 0;
      return all[idx]?.qualities || [];
    },

    qsBarStyle(qs, appType) {
      const allQs = this.getSelectedQS(appType || this.activeAppType);
      const maxMin = Math.max(...allQs.map(q => q.min), 1);
      const pct = Math.min(100, (qs.min / maxMin) * 100);
      return `width:${Math.max(2, pct)}%`;
    },

    // --- Quality Size Sync ---

    async loadInstanceQS(appType, instanceId) {
      if (!instanceId) {
        this.qsInstanceDefs = { ...this.qsInstanceDefs, [appType]: null };
        this.qsOverrides = { ...this.qsOverrides, [appType]: {} };
        this.qsAutoSync = { ...this.qsAutoSync, [appType]: { enabled: false, type: '' } };
        return;
      }
      try {
        const [defsR, overR, asR] = await Promise.all([
          fetch(`/api/instances/${instanceId}/quality-sizes`),
          fetch(`/api/instances/${instanceId}/quality-sizes/overrides`),
          fetch(`/api/instances/${instanceId}/quality-sizes/auto-sync`)
        ]);
        if (defsR.ok) {
          this.qsInstanceDefs = { ...this.qsInstanceDefs, [appType]: await defsR.json() };
        }
        if (overR.ok) {
          this.qsOverrides = { ...this.qsOverrides, [appType]: await overR.json() };
        }
        if (asR.ok) {
          const as = await asR.json();
          this.qsAutoSync = { ...this.qsAutoSync, [appType]: as };
          // Auto-select the type tab that matches the configured auto-sync type
          if (as.enabled && as.type) {
            const allQS = this.getQualitySizes(appType);
            const idx = allQS.findIndex(q => q.type === as.type);
            if (idx >= 0 && this.selectedQSType[appType] === undefined) {
              this.selectedQSType = { ...this.selectedQSType, [appType]: idx };
            }
          }
        }
      } catch (e) { console.error('loadInstanceQS:', e); }
    },

    _findInstanceDef(appType, qualityName) {
      const defs = this.qsInstanceDefs[appType];
      if (!defs) return null;
      return defs.find(d => d.quality?.name === qualityName || d.title === qualityName) || null;
    },

    getInstanceQSVal(appType, qualityName, field) {
      const def = this._findInstanceDef(appType, qualityName);
      if (!def) return '-';
      const map = { min: 'minSize', preferred: 'preferredSize', max: 'maxSize' };
      const val = def[map[field]] ?? 0;
      return val.toFixed(1);
    },

    qsCellStyle(appType, trashQS, field) {
      const def = this._findInstanceDef(appType, trashQS.quality);
      if (!def) return 'color:#aaa';
      const map = { min: 'minSize', preferred: 'preferredSize', max: 'maxSize' };
      const current = def[map[field]] ?? 0;
      const target = this._qsTargetVal(appType, trashQS, field);
      if (Math.abs(current - target) < 0.05) return 'color:#3fb950'; // match
      return 'color:#d29922'; // diff
    },

    _qsTargetVal(appType, trashQS, field) {
      const overrides = this.qsOverrides[appType] || {};
      const ov = overrides[trashQS.quality];
      if (ov) return ov[field];
      return trashQS[field];
    },

    _defFieldVal(def, field) {
      const map = { min: 'minSize', preferred: 'preferredSize', max: 'maxSize' };
      return def[map[field]] ?? 0;
    },

    qsRowStyle(appType, qs) {
      if (!this.qsInstanceId[appType]) return '';
      const def = this._findInstanceDef(appType, qs.quality);
      if (!def) return '';
      const allMatch = ['min', 'preferred', 'max'].every(f =>
        Math.abs(this._defFieldVal(def, f) - this._qsTargetVal(appType, qs, f)) < 0.05
      );
      return allMatch ? '' : 'background:#1c1f26';
    },

    isQSCustom(appType, qualityName) {
      const overrides = this.qsOverrides[appType] || {};
      return !!overrides[qualityName];
    },

    toggleQSMode(appType, qualityName) {
      const overrides = { ...(this.qsOverrides[appType] || {}) };
      if (overrides[qualityName]) {
        delete overrides[qualityName];
      } else {
        // Default to current instance values; fall back to TRaSH when instance value is 0 (not set)
        const def = this._findInstanceDef(appType, qualityName);
        const trashQS = this.getSelectedQS(appType).find(q => q.quality === qualityName);
        if (def) {
          overrides[qualityName] = {
            min: def.minSize || trashQS?.min || 0,
            preferred: def.preferredSize || trashQS?.preferred || 0,
            max: def.maxSize || trashQS?.max || 0
          };
        } else if (trashQS) {
          overrides[qualityName] = { min: trashQS.min, preferred: trashQS.preferred, max: trashQS.max };
        }
      }
      this.qsOverrides = { ...this.qsOverrides, [appType]: overrides };
      this._saveQSOverrides(appType);
    },

    getQSOverrideVal(appType, qualityName, field, fallback) {
      const overrides = this.qsOverrides[appType] || {};
      const ov = overrides[qualityName];
      return ov ? ov[field] : fallback;
    },

    setQSOverrideVal(appType, qualityName, field, value) {
      const overrides = { ...(this.qsOverrides[appType] || {}) };
      if (!overrides[qualityName]) return;
      overrides[qualityName] = { ...overrides[qualityName], [field]: value };
      this.qsOverrides = { ...this.qsOverrides, [appType]: overrides };
      this._saveQSOverrides(appType);
    },

    async _saveQSOverrides(appType) {
      const instanceId = this.qsInstanceId[appType];
      if (!instanceId) return;
      try {
        await fetch(`/api/instances/${instanceId}/quality-sizes/overrides`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(this.qsOverrides[appType] || {})
        });
      } catch (e) { console.error('saveQSOverrides:', e); }
    },

    async toggleQSAutoSync(appType, enabled, inputEl) {
      const instanceId = this.qsInstanceId[appType];
      if (!instanceId) return;

      if (enabled) {
        const syncCount = this.qsChangeCount(appType);
        const inst = this.instancesOfType(appType).find(i => i.id === instanceId);
        const instName = inst?.name || 'instance';
        const msg = syncCount > 0
          ? `${syncCount} Auto-mode qualities on ${instName} will be updated to TRaSH values immediately.\n\nCustom-mode qualities will not be changed.\nFuture TRaSH pulls will also sync automatically.\n\nMake sure you have set the correct mode (Auto/Custom) per quality before enabling.`
          : `All Auto-mode values on ${instName} currently match TRaSH.\n\nFuture TRaSH pulls will sync automatically.\nCustom-mode qualities will not be changed.`;

        // Show custom confirm modal
        if (inputEl) inputEl.checked = false; // revert until confirmed
        this.confirmModal = {
          show: true,
          title: 'Enable Auto-sync',
          message: msg,
          confirmLabel: syncCount > 0 ? `Sync ${syncCount} now & enable` : 'Enable',
          onConfirm: async () => { await this._applyQSAutoSync(appType, true); },
          onCancel: null
        };
        return;
      }

      await this._applyQSAutoSync(appType, false);
    },

    async _applyQSAutoSync(appType, enabled) {
      const instanceId = this.qsInstanceId[appType];
      if (!instanceId) return;

      const allQS = this.getQualitySizes(appType);
      const idx = this.selectedQSType[appType] || 0;
      const qsType = allQS[idx]?.type || '';

      const as = { enabled, type: qsType };
      this.qsAutoSync = { ...this.qsAutoSync, [appType]: as };
      try {
        await fetch(`/api/instances/${instanceId}/quality-sizes/auto-sync`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(as)
        });
      } catch (e) { console.error('_applyQSAutoSync:', e); }

      // Sync immediately when enabling
      if (enabled && this.qsChangeCount(appType) > 0) {
        await this.syncQualitySizes(appType);
      }
    },

    _qsDiffers(def, appType, qs, field) {
      return Math.abs(this._defFieldVal(def, field) - this._qsTargetVal(appType, qs, field)) >= 0.05;
    },

    qsChangeCount(appType) {
      const trashQualities = this.getSelectedQS(appType);
      const defs = this.qsInstanceDefs[appType];
      if (!defs || !trashQualities.length) return 0;
      let count = 0;
      for (const qs of trashQualities) {
        const def = this._findInstanceDef(appType, qs.quality);
        if (!def) continue;
        if (['min', 'preferred', 'max'].some(f => this._qsDiffers(def, appType, qs, f))) {
          count++;
        }
      }
      return count;
    },

    qsHasRowChange(appType, qs) {
      const def = this._findInstanceDef(appType, qs.quality);
      if (!def) return false;
      return ['min', 'preferred', 'max'].some(f => this._qsDiffers(def, appType, qs, f));
    },

    async syncSingleQS(appType, qualityName) {
      const instanceId = this.qsInstanceId[appType];
      if (!instanceId) return;
      const qs = this.getSelectedQS(appType).find(q => q.quality === qualityName);
      if (!qs) return;
      const def = this._findInstanceDef(appType, qualityName);
      if (!def) return;

      const target = {
        min: this._qsTargetVal(appType, qs, 'min'),
        preferred: this._qsTargetVal(appType, qs, 'preferred'),
        max: this._qsTargetVal(appType, qs, 'max')
      };

      try {
        const r = await fetch(`/api/instances/${instanceId}/quality-sizes/sync`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ definitions: [{ ...def, minSize: target.min, preferredSize: target.preferred, maxSize: target.max }] })
        });
        if (r.ok) {
          this.qsSyncResult = { ...this.qsSyncResult, [appType]: { ok: true, message: `Synced ${qualityName}` } };
          await this.loadInstanceQS(appType, instanceId);
        } else {
          const err = await r.json().catch(() => ({}));
          this.qsSyncResult = { ...this.qsSyncResult, [appType]: { ok: false, message: err.error || `Failed to sync ${qualityName}` } };
        }
      } catch (e) {
        this.qsSyncResult = { ...this.qsSyncResult, [appType]: { ok: false, message: e.message } };
      }
    },

    async syncQualitySizes(appType) {
      const instanceId = this.qsInstanceId[appType];
      if (!instanceId) return;
      this.qsSyncing = { ...this.qsSyncing, [appType]: true };
      this.qsSyncResult = { ...this.qsSyncResult, [appType]: null };

      try {
        const trashQualities = this.getSelectedQS(appType);
        const defs = this.qsInstanceDefs[appType];
        const updated = [];

        for (const qs of trashQualities) {
          const def = this._findInstanceDef(appType, qs.quality);
          if (!def) continue;
          const target = {
            min: this._qsTargetVal(appType, qs, 'min'),
            preferred: this._qsTargetVal(appType, qs, 'preferred'),
            max: this._qsTargetVal(appType, qs, 'max')
          };
          if (this._qsDiffers(def, appType, qs, 'min') ||
              this._qsDiffers(def, appType, qs, 'preferred') ||
              this._qsDiffers(def, appType, qs, 'max')) {
            updated.push({ ...def, minSize: target.min, preferredSize: target.preferred, maxSize: target.max });
          }
        }

        if (updated.length === 0) {
          this.qsSyncResult = { ...this.qsSyncResult, [appType]: { ok: true, message: 'All values already match' } };
          return;
        }

        const r = await fetch(`/api/instances/${instanceId}/quality-sizes/sync`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ definitions: updated })
        });

        if (r.ok) {
          this.qsSyncResult = { ...this.qsSyncResult, [appType]: { ok: true, message: `Synced ${updated.length} quality sizes` } };
          await this.loadInstanceQS(appType, instanceId);
        } else {
          const err = await r.json().catch(() => ({}));
          this.qsSyncResult = { ...this.qsSyncResult, [appType]: { ok: false, message: err.error || 'Sync failed' } };
        }
      } catch (e) {
        this.qsSyncResult = { ...this.qsSyncResult, [appType]: { ok: false, message: e.message } };
      } finally {
        this.qsSyncing = { ...this.qsSyncing, [appType]: false };
      }
    },

  },
};
