export default {
  state: {},
  methods: {
    // --- Cleanup ---
    cleanupActionLabel(action) {
      const labels = {
        'duplicates': 'Duplicate Custom Formats',
        'delete-cfs-keep-scores': 'Delete All CFs (Keep Scores)',
        'delete-cfs-and-scores': 'Delete All CFs & Scores',
        'reset-unsynced-scores': 'Reset Non-Synced Scores',
        'orphaned-scores': 'Orphaned Scores',
        'unused-by-clonarr': 'Unused Custom Formats (Clonarr-managed)',
      };
      return labels[action] || action;
    },

    async cleanupScan(action) {
      if (!this.cleanupInstanceId) return;
      this.cleanupScanning = true;
      try {
        const resp = await fetch('/api/instances/' + this.cleanupInstanceId + '/cleanup/scan', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ action, keep: this.cleanupKeepList }),
        });
        if (!resp.ok) {
          const err = await resp.json().catch(() => ({}));
          this.showToast(err.error || 'Scan failed', 'error', 8000);
          return;
        }
        this.cleanupResult = await resp.json();
        this.cleanupFilter = 'all';
      } catch (e) {
        this.showToast('Scan failed: ' + e.message, 'error', 8000);
      } finally {
        this.cleanupScanning = false;
      }
    },

    async cleanupApply(opts = {}) {
      if (!this.cleanupResult?.items?.length) return;
      // For unused-by-clonarr, the user can opt to keep rename-flagged CFs
      // (the safer default) or delete everything. Other cleanup actions
      // ignore opts and delete the full item list.
      const items = opts.skipRenameFlagged
        ? this.cleanupResult.items.filter(i => !i.renamingFlag)
        : this.cleanupResult.items;
      if (items.length === 0) return;

      // Build a clear confirmation message — count + (if rename-tags
      // included) what they are. Destructive actions need explicit ack.
      const includesRenameTags = !opts.skipRenameFlagged && items.some(i => i.renamingFlag);
      const renameCount = items.filter(i => i.renamingFlag).length;
      let message = `Permanently delete ${items.length} custom format${items.length === 1 ? '' : 's'} from ${this.cleanupResult.instance}?`;
      if (includesRenameTags && renameCount > 0) {
        message += `\n\nIncludes ${renameCount} CF${renameCount === 1 ? '' : 's'} tagged for filename rendering. Future renames will no longer include their tags (e.g. [AMZN], [v2]). Existing files on disk are unaffected.`;
      }
      message += `\n\nThis cannot be undone, but you can re-sync any TRaSH or builder profile to recreate CFs.`;

      const confirmed = await new Promise(resolve => {
        this.confirmModal = {
          show: true,
          title: 'Delete Custom Formats',
          message,
          confirmLabel: `Delete ${items.length}`,
          onConfirm: () => resolve(true),
          onCancel: () => resolve(false),
        };
      });
      if (!confirmed) return;

      this.cleanupApplying = true;
      try {
        const ids = items.map(i => i.id);
        const resp = await fetch('/api/instances/' + this.cleanupResult.instanceId + '/cleanup/apply', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ action: this.cleanupResult.action, ids }),
        });
        if (!resp.ok) {
          const err = await resp.json().catch(() => ({}));
          this.cleanupResult = { ...this.cleanupResult, applied: false, applyError: err.error || 'Apply failed' };
          return;
        }
        const result = await resp.json();
        this.cleanupResult = { ...this.cleanupResult, applied: true, applyResult: result };
      } catch (e) {
        this.cleanupResult = { ...this.cleanupResult, applied: false, applyError: e.message };
      } finally {
        this.cleanupApplying = false;
      }
    },

    async loadCleanupKeep() {
      if (!this.cleanupInstanceId) { this.cleanupKeepList = []; return; }
      try {
        const resp = await fetch('/api/instances/' + this.cleanupInstanceId + '/cleanup/keep');
        if (resp.ok) this.cleanupKeepList = await resp.json();
      } catch (e) { this.cleanupKeepList = []; }
    },
    async saveCleanupKeep() {
      if (!this.cleanupInstanceId) return;
      await fetch('/api/instances/' + this.cleanupInstanceId + '/cleanup/keep', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(this.cleanupKeepList),
      });
    },
    async loadCleanupCFNames() {
      if (!this.cleanupInstanceId) { this.cleanupCFNames = []; return; }
      try {
        const resp = await fetch('/api/instances/' + this.cleanupInstanceId + '/cfs');
        if (resp.ok) {
          const cfs = await resp.json();
          this.cleanupCFNames = (cfs || []).map(cf => cf.name).sort();
        }
      } catch (e) { this.cleanupCFNames = []; }
    },
    addCleanupKeepName(name) {
      if (!name) return;
      if (this.cleanupKeepList.some(n => n.toLowerCase() === name.toLowerCase())) {
        return; // already in list — no-op, keep input + dropdown intact
      }
      this.cleanupKeepList.push(name);
      // Keep the input + dropdown open so the user can click another match
      // from the same query. Refresh suggestions so the just-added one
      // disappears and remaining matches stay visible. Empty query → empty
      // suggestions (dropdown closes naturally).
      const q = this.cleanupKeepInput.trim().toLowerCase();
      if (q) {
        this.cleanupKeepSuggestions = this.cleanupCFNames.filter(n =>
          n.toLowerCase().includes(q) &&
          !this.cleanupKeepList.some(k => k.toLowerCase() === n.toLowerCase())
        ).slice(0, 10);
      } else {
        this.cleanupKeepSuggestions = [];
      }
      this.saveCleanupKeep();
    },
    async addCleanupKeep() {
      const name = this.cleanupKeepInput.trim();
      if (!name) return;
      if (this.cleanupKeepList.some(n => n.toLowerCase() === name.toLowerCase())) {
        this.cleanupKeepInput = '';
        return;
      }
      this.cleanupKeepList.push(name);
      this.cleanupKeepInput = '';
      await this.saveCleanupKeep();
    },
    async addAllMatchingKeep() {
      const query = this.cleanupKeepInput.trim().toLowerCase();
      if (!query) return;
      const matching = this.cleanupCFNames.filter(n =>
        n.toLowerCase().includes(query) && !this.cleanupKeepList.some(k => k.toLowerCase() === n.toLowerCase())
      );
      if (matching.length === 0) return;
      this.cleanupKeepList.push(...matching);
      this.cleanupKeepInput = '';
      this.cleanupKeepSuggestions = [];
      await this.saveCleanupKeep();
      this.showToast(`Added ${matching.length} CFs to Keep List`, 'info', 3000);
    },

    async removeCleanupKeep(idx) {
      this.cleanupKeepList.splice(idx, 1);
      await this.saveCleanupKeep();
    },
  },
};
