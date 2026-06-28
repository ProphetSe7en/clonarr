// Portable sync-rule import/export. Distinct from import-export.js (which is the
// Profile Builder's TRaSH-JSON / Recyclarr export). This exports a single sync
// rule as a self-contained clonarr file that re-imports 1:1 into another Clonarr.
// Backend: internal/core/rule_export.go + internal/api/rule_portability.go.
// See docs/clonarr/sync-rule-import-export-plan.md.

export default {
  state: {
    showExportRuleModal: false,
    exportRuleContent: '',   // pretty-printed JSON shown in the modal
    exportRuleName: '',      // base profile name, used for the download filename
    exportRuleCopied: false,
    // Import
    showImportRuleModal: false,
    importRuleText: '',          // pasted or uploaded JSON
    importRuleError: '',         // validation/server error shown in the modal
    importRuleCollisions: [],    // [{name, usedByRules}] awaiting resolution
    importRuleResolutions: {},   // lower(name) -> 'skip'|'rename'|'replace'
    importRuleBusy: false,
  },
  methods: {
    // Fetch a rule's portable export and show it in the modal (copy / download).
    async exportRule(rule) {
      if (!rule?.id) return;
      try {
        const r = await fetch(`/api/auto-sync/rules/${rule.id}/export`);
        if (!r.ok) {
          const e = await r.json().catch(() => ({}));
          this.showToast(e.error || 'Could not export this rule.', 'error', 6000);
          return;
        }
        const data = await r.json();
        this.exportRuleContent = JSON.stringify(data, null, 2);
        this.exportRuleName = data?.baseProfile?.name || data?.app || 'rule';
        this.exportRuleCopied = false;
        this.showExportRuleModal = true;
      } catch (e) {
        this.showToast('Export error: ' + e.message, 'error', 6000);
      }
    },

    closeExportRuleModal() {
      this.showExportRuleModal = false;
    },

    // Download the shown export as a .json file. Filename is slugified from the
    // profile name so multiple exports do not overwrite each other.
    downloadRuleExport() {
      const slug = (this.exportRuleName || 'rule')
        .toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '') || 'rule';
      const blob = new Blob([this.exportRuleContent], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = `clonarr-rule-${slug}.json`;
      document.body.appendChild(a);
      a.click();
      a.remove();
      URL.revokeObjectURL(url);
    },

    // --- Import ---------------------------------------------------------------

    openImportRuleModal() {
      this.importRuleText = '';
      this.importRuleError = '';
      this.importRuleCollisions = [];
      this.importRuleResolutions = {};
      this.importRuleBusy = false;
      this.showImportRuleModal = true;
    },

    closeImportRuleModal() {
      this.showImportRuleModal = false;
    },

    // Read an uploaded .json file into the paste box.
    onImportFileChange(event) {
      const file = event.target.files && event.target.files[0];
      if (!file) return;
      const reader = new FileReader();
      reader.onload = () => { this.importRuleText = String(reader.result || ''); this.importRuleError = ''; };
      reader.onerror = () => { this.importRuleError = 'Could not read that file.'; };
      reader.readAsText(file);
      event.target.value = ''; // allow re-selecting the same file
    },

    // Validate + recreate custom CFs server-side, then load the result into the
    // editor. A 409 means custom CF name collisions need a choice first.
    async submitImportRule() {
      this.importRuleError = '';
      const text = (this.importRuleText || '').trim();
      if (!text) { this.importRuleError = 'Paste or upload a Clonarr export file first.'; return; }
      this.importRuleBusy = true;
      try {
        const r = await fetch('/api/auto-sync/rules/import', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ content: text, app: this.activeAppType, resolutions: this.importRuleResolutions }),
        });
        if (r.status === 409) {
          const d = await r.json().catch(() => ({}));
          this.importRuleCollisions = d.collisions || [];
          this.importRuleError = 'Some custom formats already exist. Choose what to do with each, then Import again.';
          return;
        }
        if (!r.ok) {
          const d = await r.json().catch(() => ({}));
          this.importRuleError = d.error || 'Import failed.';
          return;
        }
        const d = await r.json();
        this.showImportRuleModal = false;
        await this.loadImportedRuleIntoEditor(d.export, d.baseProfileFound);
      } catch (e) {
        this.importRuleError = 'Import error: ' + e.message;
      } finally {
        this.importRuleBusy = false;
      }
    },

    // Open the profile editor pre-filled from a validated import. Reuses the
    // same applyRuleStateToEditor hook the Compare and edit-rule flows use:
    // open the base profile fresh, then layer the imported overrides on top
    // with arrProfileId 0 (a new, unlocked profile -> Create New / Apply & Sync).
    async loadImportedRuleIntoEditor(exp, baseFound) {
      const app = exp?.app;
      const insts = this.instancesOfType(app);
      if (insts.length === 0) { this.showToast(`Add a ${app} instance first to import this rule.`, 'error', 7000); return; }
      if (!baseFound || !exp.baseProfile?.trashId) {
        this.showToast('The base profile this rule is built on is not available here. Pull TRaSH-Guides data, then try again.', 'error', 8000);
        return;
      }
      const profile = (this.trashProfiles[app] || []).find(p => p.trashId === exp.baseProfile.trashId);
      if (!profile) { this.showToast('Base profile not found in TRaSH data.', 'error', 7000); return; }

      await this.openProfileDetail(insts[0], profile, false);
      if (!this.profileDetail?.detail) { this.showToast('Could not open the editor.', 'error', 6000); return; }

      const ru = exp.rule || {};
      this.applyRuleStateToEditor({
        instanceId: insts[0].id,
        arrProfileId: 0, // new profile, unlocked
        selectedCFs: ru.selectedCFs || [],
        excludedCFs: ru.excludedCFs || [],
        scoreOverrides: ru.scoreOverrides || {},
        qualityStructure: ru.qualityStructure || [],
        qualityOverrides: ru.qualityOverrides || {},
        overrides: ru.overrides || null,
        description: ru.description || '',
      }, this.profileDetail.detail);
      this.pdOverridesEnabled = true;
      this.showToast('Imported. Review the settings, name it, then Apply & Sync to create the rule.', 'success', 7000);
    },
  },
};
