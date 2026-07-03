export default {
  state: {
    instances: [],
    instanceStatus: {},
    instanceVersion: {},
    showInstanceModal: false,
    editingInstance: null,
    instanceForm: { name: '', type: 'radarr', url: '', apiKey: '', externalAuth: false, username: '', password: '' },
    instanceFormErrors: {},
    modalTestResult: null,
    // Prowlarr's data shape (single global config) differs from the
    // multi-instance Radarr/Sonarr list. The v3 Instances page presents
    // it as a sibling section using the same row + inline-edit pattern,
    // so visually it matches Radarr/Sonarr (Test/Edit/Delete on the row,
    // Save/Cancel at the bottom of the form). prowlarrForm holds the
    // staged edits so Cancel can discard cleanly — unlike the old
    // @change auto-save which made Cancel misleading.
    prowlarrForm: { enabled: false, url: '', apiKey: '', radarrCategories: [], sonarrCategories: [] },
    // v3 Instances page uses inline-expand for Edit (matches Prowlarr's
    // pattern) instead of a modal. inlineEditingId tracks which row's
    // form is open:
    //   null            → no inline form open
    //   inst.id (string) → editing an existing instance
    //   'new-radarr'    → adding a new Radarr (form at section bottom)
    //   'new-sonarr'    → adding a new Sonarr
    // Only one slot can be open at a time; clicking Edit on another row
    // silently switches.
    inlineEditingId: null,
  },

  methods: {
    normalizedInstanceForm() {
      return {
        name: (this.instanceForm.name || '').trim(),
        type: (this.instanceForm.type || '').trim(),
        url: (this.instanceForm.url || '').trim(),
        apiKey: (this.instanceForm.apiKey || '').trim(),
        externalAuth: !!this.instanceForm.externalAuth,
        username: (this.instanceForm.username || '').trim(),
        password: (this.instanceForm.password || '').trim(),
      };
    },

    async loadInstances() {
      try {
        const r = await fetch('/api/instances');
        if (!r.ok) return;
        this.instances = await r.json();
      } catch (e) { console.error('loadInstances:', e); }
    },

    instancesOfType(type) {
      return this.instances.filter(i => i.type === type).sort((a, b) => a.name.localeCompare(b.name));
    },

    instanceIconUrl(inst) {
      const is4k = /4k|uhd/i.test(inst.name);
      if (inst.type === 'radarr') return is4k ? 'icons/radarr4kNew.png' : 'icons/radarrNew.png';
      return is4k ? 'icons/sonarr4k.png' : 'icons/sonarr.png';
    },

    // defaultType lets the v3 Instances page's per-section "+ Add"
    // buttons preselect the right type ("+ Add Radarr" → radarr) without
    // depending on the active app cascade.
    openInstanceModal(inst = null, defaultType = null) {
      this.editingInstance = inst;
      this.modalTestResult = null;
      if (inst) {
        this.instanceForm = { name: inst.name, type: inst.type, url: inst.url, apiKey: '', externalAuth: !!inst.externalAuth, username: inst.username || '', password: '' };
      } else {
        const fallbackType = ['radarr','sonarr'].includes(this.activeAppType) ? this.activeAppType : 'radarr';
        this.instanceForm = { name: '', type: defaultType || fallbackType, url: '', apiKey: '', externalAuth: false, username: '', password: '' };
      }
      this.showInstanceModal = true;
    },

    // Inline-edit entry points used by the v3 Instances page. Same form
    // state (instanceForm + instanceFormErrors + modalTestResult) so
    // saveInstance / testConnectionInModal don't need a parallel code
    // path — they read instanceForm regardless of which surface opened
    // the form.
    startInlineEdit(inst) {
      this.inlineEditingId = inst.id;
      this.editingInstance = inst;
      this.instanceForm = { name: inst.name, type: inst.type, url: inst.url, apiKey: '', externalAuth: !!inst.externalAuth, username: inst.username || '', password: '' };
      this.instanceFormErrors = {};
      this.modalTestResult = null;
    },
    startInlineAdd(type) {
      this.inlineEditingId = 'new-' + type;
      this.editingInstance = null;
      this.instanceForm = { name: '', type, url: '', apiKey: '', externalAuth: false, username: '', password: '' };
      this.instanceFormErrors = {};
      this.modalTestResult = null;
    },
    cancelInlineEdit() {
      this.inlineEditingId = null;
      this.editingInstance = null;
      this.instanceFormErrors = {};
      this.modalTestResult = null;
    },

    // Prowlarr-specific inline-edit helpers. Same UX shape as
    // startInlineEdit/Add but talks to the prowlarrForm temp state +
    // config.prowlarr singleton instead of the instance list.
    startProwlarrEdit() {
      this.inlineEditingId = 'prowlarr';
      this.prowlarrForm = {
        enabled: !!this.config.prowlarr?.enabled,
        url: this.config.prowlarr?.url || '',
        apiKey: this.config.prowlarr?.apiKey || '',
        radarrCategories: [...(this.config.prowlarr?.radarrCategories || [])],
        sonarrCategories: [...(this.config.prowlarr?.sonarrCategories || [])],
      };
      this.prowlarrTestResult = null;
    },
    startProwlarrAdd() {
      this.inlineEditingId = 'new-prowlarr';
      this.prowlarrForm = { enabled: true, url: '', apiKey: '', radarrCategories: [], sonarrCategories: [] };
      this.prowlarrTestResult = null;
    },
    async saveProwlarrConfig() {
      // Commit prowlarrForm into config.prowlarr and persist via the
      // existing config save endpoint (same path the old @change wiring
      // used — sync engine untouched).
      if (!this.config.prowlarr) this.config.prowlarr = {};
      this.config.prowlarr.enabled = this.prowlarrForm.enabled;
      this.config.prowlarr.url = (this.prowlarrForm.url || '').trim();
      this.config.prowlarr.apiKey = (this.prowlarrForm.apiKey || '').trim();
      this.config.prowlarr.radarrCategories = this.prowlarrForm.radarrCategories;
      this.config.prowlarr.sonarrCategories = this.prowlarrForm.sonarrCategories;
      await this.saveConfig(['prowlarr']);
      this.inlineEditingId = null;
    },
    async deleteProwlarr() {
      const confirmed = await new Promise(resolve => {
        this.confirmModal = { show: true, title: 'Remove Prowlarr', message: 'Disconnect Prowlarr from Clonarr? The Scoring Sandbox will lose release-search until you reconnect.', confirmLabel: 'Remove', onConfirm: () => resolve(true), onCancel: () => resolve(false) };
      });
      if (!confirmed) return;
      if (!this.config.prowlarr) this.config.prowlarr = {};
      this.config.prowlarr.enabled = false;
      this.config.prowlarr.url = '';
      this.config.prowlarr.apiKey = '';
      await this.saveConfig(['prowlarr']);
      this.prowlarrTestResult = null;
    },

    async saveInstance() {
      const data = this.normalizedInstanceForm();
      this.instanceFormErrors = {};
      if (!data.url) this.instanceFormErrors.url = 'URL is required';
      if (!data.name) this.instanceFormErrors.name = 'Name is required';
      if (!this.editingInstance && !data.apiKey) this.instanceFormErrors.apiKey = 'API Key is required';
      if (data.externalAuth && !this.editingInstance && !data.username) this.instanceFormErrors.username = 'Username is required when external authentication is enabled';
      if (data.externalAuth && !this.editingInstance && !data.password) this.instanceFormErrors.password = 'Password is required when external authentication is enabled';
      if (Object.keys(this.instanceFormErrors).length > 0) return;

      let r;
      if (this.editingInstance) {
        if (!data.apiKey) data.apiKey = this.editingInstance.apiKey;
        // password left blank → backend preserves existing (mirrors apiKey behaviour)
        r = await fetch(`/api/instances/${this.editingInstance.id}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(data)
        });
      } else {
        r = await fetch('/api/instances', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(data)
        });
      }
      if (!r.ok) {
        const err = await r.json().catch(() => ({}));
        this.showToast(err.error || 'Failed to save instance', 'error', 8000);
        return;
      }
      this.showInstanceModal = false;
      this.inlineEditingId = null;
      await this.loadInstances();
      this.testAllInstances();
      // Reload sync data in case orphaned data was migrated.
      await this.loadAutoSyncRules();
      for (const inst of this.instances) {
        await this.loadInstanceProfiles(inst);
        await this.loadSyncHistory(inst.id);
      }
    },

    async deleteInstance(inst) {
      const confirmed = await new Promise(resolve => {
        this.confirmModal = { show: true, title: 'Delete Instance', message: `Delete ${inst.name}? Sync history and rules will be preserved and restored if you re-add the instance.`, confirmLabel: 'Delete', onConfirm: () => resolve(true), onCancel: () => resolve(false) };
      });
      if (!confirmed) return;
      const r = await fetch(`/api/instances/${inst.id}`, { method: 'DELETE' });
      if (!r.ok) {
        const err = await r.json().catch(() => ({}));
        this.showToast(err.error || 'Failed to delete instance', 'error', 8000);
        return;
      }
      // Clear cached status for deleted instance.
      const { [inst.id]: _, ...restStatus } = this.instanceStatus;
      this.instanceStatus = restStatus;
      await this.loadInstances();
    },

    // Write a status only when it actually changes, so a stable instance does
    // not churn the instanceStatus object on every poll. The object reference is
    // what Alpine watches; rewriting it with the same value still re-evaluates
    // every binding (e.g. the Profile Editor action buttons), so guard it.
    _setInstanceStatus(id, status) {
      if (this.instanceStatus[id] === status) return;
      this.instanceStatus = { ...this.instanceStatus, [id]: status };
    },

    async testAllInstances({ silent = false } = {}) {
      for (const inst of this.instances) {
        this.testInstance(inst, { silent });
      }
      // Also test Prowlarr if configured.
      if (this.config.prowlarr?.enabled && this.config.prowlarr?.url && this.config.prowlarr?.apiKey) {
        this.testProwlarr();
      }
    },

    // silent: the background re-test (every 60s) skips the transient 'testing'
    // state and only writes the final status when it changed. Without this, each
    // poll flipped every connected instance 'connected' -> 'testing' -> 'connected',
    // which flickered the Profile Editor buttons bound to instanceStatus. A user-
    // initiated test (silent=false) still shows 'testing' for immediate feedback.
    async testInstance(inst, { silent = false } = {}) {
      if (!silent) this._setInstanceStatus(inst.id, 'testing');
      try {
        const r = await fetch(`/api/instances/${inst.id}/test`, { method: 'POST' });
        if (!r.ok) { this._setInstanceStatus(inst.id, 'failed'); return; }
        const data = await r.json();
        this._setInstanceStatus(inst.id, data.connected ? 'connected' : 'failed');
        if (data.connected && data.version && this.instanceVersion[inst.id] !== data.version) {
          this.instanceVersion = { ...this.instanceVersion, [inst.id]: data.version };
        }
      } catch (e) {
        this._setInstanceStatus(inst.id, 'failed');
      }
    },

    async testConnectionInModal() {
      this.modalTestResult = 'testing';
      try {
        const formData = this.normalizedInstanceForm();
        let r;
        if (this.editingInstance && !formData.apiKey) {
          // Use saved instance endpoint, which has the real API key.
          r = await fetch(`/api/instances/${this.editingInstance.id}/test`, { method: 'POST' });
        } else {
          r = await fetch('/api/test-connection', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ url: formData.url, apiKey: formData.apiKey, externalAuth: formData.externalAuth, username: formData.username, password: formData.password })
          });
        }
        const data = await r.json();
        if (!r.ok) {
          this.modalTestResult = { connected: false, error: data.error || 'Request failed' };
        } else {
          this.modalTestResult = data;
        }
      } catch (e) {
        this.modalTestResult = { connected: false, error: 'Request failed' };
      }
    },
  },
};
