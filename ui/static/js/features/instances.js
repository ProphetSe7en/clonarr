export default {
  state: {
    instances: [],
    instanceStatus: {},
    instanceVersion: {},
    showInstanceModal: false,
    editingInstance: null,
    instanceForm: { name: '', type: 'radarr', url: '', apiKey: '' },
    instanceFormErrors: {},
    modalTestResult: null,
  },

  methods: {
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

    openInstanceModal(inst = null) {
      this.editingInstance = inst;
      this.modalTestResult = null;
      if (inst) {
        this.instanceForm = { name: inst.name, type: inst.type, url: inst.url, apiKey: '' };
      } else {
        this.instanceForm = { name: '', type: ['radarr','sonarr'].includes(this.activeAppType) ? this.activeAppType : 'radarr', url: '', apiKey: '' };
      }
      this.showInstanceModal = true;
    },

    async saveInstance() {
      this.instanceFormErrors = {};
      if (!this.instanceForm.url) this.instanceFormErrors.url = 'URL is required';
      if (!this.instanceForm.name) this.instanceFormErrors.name = 'Name is required';
      if (!this.editingInstance && !this.instanceForm.apiKey) this.instanceFormErrors.apiKey = 'API Key is required';
      if (Object.keys(this.instanceFormErrors).length > 0) return;

      const data = { ...this.instanceForm };
      let r;
      if (this.editingInstance) {
        if (!data.apiKey) data.apiKey = this.editingInstance.apiKey;
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

    async testAllInstances() {
      for (const inst of this.instances) {
        this.testInstance(inst);
      }
      // Also test Prowlarr if configured.
      if (this.config.prowlarr?.enabled && this.config.prowlarr?.url && this.config.prowlarr?.apiKey) {
        this.testProwlarr();
      }
    },

    async testInstance(inst) {
      this.instanceStatus = { ...this.instanceStatus, [inst.id]: 'testing' };
      try {
        const r = await fetch(`/api/instances/${inst.id}/test`, { method: 'POST' });
        if (!r.ok) { this.instanceStatus = { ...this.instanceStatus, [inst.id]: 'failed' }; return; }
        const data = await r.json();
        this.instanceStatus = { ...this.instanceStatus, [inst.id]: data.connected ? 'connected' : 'failed' };
        if (data.connected && data.version) {
          this.instanceVersion = { ...this.instanceVersion, [inst.id]: data.version };
        }
      } catch (e) {
        this.instanceStatus = { ...this.instanceStatus, [inst.id]: 'failed' };
      }
    },

    async testConnectionInModal() {
      this.modalTestResult = 'testing';
      try {
        let r;
        if (this.editingInstance && !this.instanceForm.apiKey) {
          // Use saved instance endpoint, which has the real API key.
          r = await fetch(`/api/instances/${this.editingInstance.id}/test`, { method: 'POST' });
        } else {
          r = await fetch('/api/test-connection', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ url: this.instanceForm.url, apiKey: this.instanceForm.apiKey })
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
