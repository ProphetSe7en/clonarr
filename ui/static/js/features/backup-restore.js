export default {
  state: {},
  methods: {
    // --- Instance Backup/Restore ---
    async openBackupModal(inst) {
      this.backupInstance = inst;
      this.backupMode = 'profiles';
      this.backupStep = 'mode';
      this.backupProfiles = [];
      this.backupCFs = [];
      this.backupSelectedProfiles = {};
      this.backupSelectedCFs = {};
      this.backupScoredCFs = {};
      this.backupLoading = true;
      this.showBackupModal = true;
      try {
        const [profRes, cfRes] = await Promise.all([
          fetch(`/api/instances/${inst.id}/profiles`),
          fetch(`/api/instances/${inst.id}/cfs`)
        ]);
        if (!profRes.ok || !cfRes.ok) { this.showToast('Failed to load instance data', 'error', 8000); this.showBackupModal = false; return; }
        this.backupProfiles = await profRes.json();
        this.backupCFs = await cfRes.json();
      } catch (e) {
        this.showToast('Failed to load instance data: ' + e.message, 'error', 8000);
        this.showBackupModal = false;
      } finally {
        this.backupLoading = false;
      }
    },

    backupNextStep() {
      // Calculate which CFs are auto-included (scored in selected profiles)
      this.backupScoredCFs = {};
      this.backupSelectedCFs = {};
      for (const p of this.backupProfiles) {
        if (!this.backupSelectedProfiles[p.id]) continue;
        for (const fi of p.formatItems || []) {
          if (fi.score !== 0) {
            this.backupScoredCFs[fi.format] = true;
          }
        }
      }
      this.backupStep = 'cfs';
    },

    async downloadBackup() {
      this.backupLoading = true;
      try {
        const profileIds = Object.entries(this.backupSelectedProfiles)
          .filter(([_, v]) => v).map(([k]) => parseInt(k));
        const extraCfIds = Object.entries(this.backupSelectedCFs)
          .filter(([_, v]) => v).map(([k]) => parseInt(k));

        const r = await fetch(`/api/instances/${this.backupInstance.id}/backup`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ profileIds, extraCfIds })
        });
        if (!r.ok) { const e = await r.json().catch(() => ({})); this.showToast(e.error || 'Backup failed', 'error', 8000); return; }
        const backup = await r.json();
        const json = JSON.stringify(backup, null, 2);
        const blob = new Blob([json], { type: 'application/json' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `${this.backupInstance.name}-backup.json`;
        a.click();
        URL.revokeObjectURL(url);
        this.showBackupModal = false;
      } catch (e) {
        this.showToast('Backup failed: ' + e.message, 'error', 8000);
      } finally {
        this.backupLoading = false;
      }
    },

    async downloadCFBackup() {
      this.backupLoading = true;
      try {
        const cfIds = Object.entries(this.backupSelectedCFs)
          .filter(([_, v]) => v).map(([k]) => parseInt(k));

        const r = await fetch(`/api/instances/${this.backupInstance.id}/backup`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ cfIds })
        });
        if (!r.ok) { const e = await r.json().catch(() => ({})); this.showToast(e.error || 'Backup failed', 'error', 8000); return; }
        const backup = await r.json();
        const json = JSON.stringify(backup, null, 2);
        const blob = new Blob([json], { type: 'application/json' });
        const url = URL.createObjectURL(blob);
        const a = document.createElement('a');
        a.href = url;
        a.download = `${this.backupInstance.name}-cfs-backup.json`;
        a.click();
        URL.revokeObjectURL(url);
        this.showBackupModal = false;
      } catch (e) {
        this.showToast('Backup failed: ' + e.message, 'error', 8000);
      } finally {
        this.backupLoading = false;
      }
    },

    openRestoreModal(inst) {
      this.restoreInstance = inst;
      this.restoreData = null;
      this.restorePreview = null;
      this.restoreResult = null;
      this.restoreLoading = false;
      this.restoreSelectedProfiles = {};
      this.restoreSelectedCFs = {};
      this.showRestoreModal = true;
    },

    async loadRestoreFile(event) {
      const file = event.target.files?.[0];
      if (!file) return;
      try {
        const text = await file.text();
        const data = JSON.parse(text);
        if (!data._clonarrBackup) { this.showToast('Not a valid Clonarr backup file', 'error', 8000); return; }
        if (data.instanceType !== this.restoreInstance.type) {
          this.showToast(`Type mismatch: backup is ${data.instanceType} but instance is ${this.restoreInstance.type}`, 'error', 8000);
          return;
        }
        this.restoreData = data;
        this.restoreSelectedProfiles = {};
        this.restoreSelectedCFs = {};
        // Auto-select all by default
        (data.profiles || []).forEach((_, i) => this.restoreSelectedProfiles[i] = true);
        (data.customFormats || []).forEach((_, i) => this.restoreSelectedCFs[i] = true);
      } catch (e) {
        this.showToast('Failed to parse backup file: ' + e.message, 'error', 8000);
      }
    },

    getFilteredRestoreData() {
      const profiles = (this.restoreData.profiles || []).filter((_, i) => this.restoreSelectedProfiles[i]);
      const customFormats = (this.restoreData.customFormats || []).filter((_, i) => this.restoreSelectedCFs[i]);
      return { ...this.restoreData, profiles, customFormats };
    },

    async previewRestore() {
      this.restoreLoading = true;
      try {
        const filtered = this.getFilteredRestoreData();
        const r = await fetch(`/api/instances/${this.restoreInstance.id}/restore?dryRun=true`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(filtered)
        });
        if (!r.ok) { const e = await r.json().catch(() => ({})); this.showToast(e.error || 'Preview failed', 'error', 8000); return; }
        this.restorePreview = await r.json();
      } catch (e) {
        this.showToast('Preview failed: ' + e.message, 'error', 8000);
      } finally {
        this.restoreLoading = false;
      }
    },

    async applyRestore() {
      const confirmed = await new Promise(resolve => {
        this.confirmModal = { show: true, title: 'Restore Backup', message: `Apply backup to ${this.restoreInstance.name}? This will create/update CFs and profiles.`, confirmLabel: 'Apply', onConfirm: () => resolve(true), onCancel: () => resolve(false) };
      });
      if (!confirmed) return;
      this.restoreLoading = true;
      try {
        const filtered = this.getFilteredRestoreData();
        const r = await fetch(`/api/instances/${this.restoreInstance.id}/restore`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(filtered)
        });
        if (!r.ok) { const e = await r.json().catch(() => ({})); this.showToast(e.error || 'Restore failed', 'error', 8000); return; }
        this.restoreResult = await r.json();
      } catch (e) {
        this.showToast('Restore failed: ' + e.message, 'error', 8000);
      } finally {
        this.restoreLoading = false;
      }
    },

  },
};
