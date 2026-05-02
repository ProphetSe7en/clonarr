function defaultAgentEvents() {
  return {
    onSyncSuccess: true,
    onSyncFailure: true,
    onCleanup: true,
    onRepoUpdate: false,
    onChangelog: false,
  };
}

function defaultAgentConfig() {
  return {
    discordWebhook: '',
    discordWebhookUpdates: '',
    gotifyUrl: '',
    gotifyToken: '',
    gotifyPriorityCritical: true,
    gotifyPriorityWarning: true,
    gotifyPriorityInfo: false,
    gotifyCriticalValue: 8,
    gotifyWarningValue: 5,
    gotifyInfoValue: 3,
    pushoverUserKey: '',
    pushoverAppToken: '',
    ntfyUrl: 'https://ntfy.sh',
    ntfyTopic: '',
    ntfyToken: '',
    ntfyPriorityCritical: true,
    ntfyPriorityWarning: true,
    ntfyPriorityInfo: false,
    ntfyCriticalValue: 5,
    ntfyWarningValue: 4,
    ntfyInfoValue: 3,
    appriseUrl: '',
    appriseToken: '',
    appriseUrls: [],
  };
}

function defaultAgentModal() {
  return {
    show: false,
    editId: null,
    name: '',
    type: 'discord',
    enabled: true,
    events: defaultAgentEvents(),
    config: defaultAgentConfig(),
    testing: false,
    testResults: [],
    testPassed: false,
    saving: false,
  };
}

export default {
  state: {
    notificationAgents: [],
    agentModal: defaultAgentModal(),
    notifAgentStatus: {},
  },

  methods: {
    async loadNotificationAgents() {
      try {
        const r = await fetch('/api/auto-sync/notification-agents');
        if (r.ok) this.notificationAgents = await r.json();
      } catch (e) { console.error('loadNotificationAgents:', e); }
    },

    openAgentModal(agent = null) {
      this.agentModal.testResults = [];
      this.agentModal.testing = false;
      this.agentModal.saving = false;
      if (agent) {
        // Credential inputs reset testPassed to false when touched, so edits
        // like renaming or event toggles do not force an unnecessary re-test.
        this.agentModal.testPassed = true;
        this.agentModal.editId = agent.id;
        this.agentModal.name = agent.name || '';
        this.agentModal.type = agent.type;
        this.agentModal.enabled = agent.enabled;
        this.agentModal.events = { ...agent.events };
        this.agentModal.config = { ...agent.config };
      } else {
        this.agentModal.testPassed = false;
        this.agentModal.editId = null;
        this.agentModal.name = '';
        this.agentModal.type = 'discord';
        this.agentModal.enabled = true;
        this.agentModal.events = defaultAgentEvents();
        this.agentModal.config = defaultAgentConfig();
      }
      this.agentModal.show = true;
    },

    async saveNotificationAgent() {
      this.agentModal.saving = true;
      const payload = {
        name: this.agentModal.name.trim(),
        type: this.agentModal.type,
        enabled: this.agentModal.enabled,
        events: { ...this.agentModal.events },
        config: { ...this.agentModal.config },
      };
      try {
        let r;
        if (this.agentModal.editId) {
          r = await fetch(`/api/auto-sync/notification-agents/${this.agentModal.editId}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
          });
        } else {
          r = await fetch('/api/auto-sync/notification-agents', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
          });
        }
        if (!r.ok) {
          const err = await r.json().catch(() => ({}));
          this.showToast(err.error || 'Failed to save notification agent', 'error', 8000);
          return;
        }
        this.agentModal.show = false;
        await this.loadNotificationAgents();
      } catch (e) {
        this.showToast('Failed to save notification agent: ' + e.message, 'error', 8000);
      } finally {
        this.agentModal.saving = false;
      }
    },

    async toggleAgentEnabled(agent) {
      const updated = { ...agent, name: agent.name, config: { ...agent.config }, events: { ...agent.events }, enabled: !agent.enabled };
      try {
        const r = await fetch(`/api/auto-sync/notification-agents/${agent.id}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(updated),
        });
        if (!r.ok) {
          this.showToast('Failed to update agent', 'error', 5000);
          return;
        }
        await this.loadNotificationAgents();
      } catch (e) {
        this.showToast('Failed to update agent: ' + e.message, 'error', 5000);
      }
    },

    async deleteNotificationAgent(agent) {
      const displayName = agent.name || agent.type.charAt(0).toUpperCase() + agent.type.slice(1);
      const confirmed = await new Promise(resolve => {
        this.confirmModal = { show: true, title: 'Delete Notification Agent', message: `Delete "${displayName}"? This cannot be undone.`, confirmLabel: 'Delete', onConfirm: () => resolve(true), onCancel: () => resolve(false) };
      });
      if (!confirmed) return;
      try {
        const r = await fetch(`/api/auto-sync/notification-agents/${agent.id}`, { method: 'DELETE' });
        if (!r.ok) {
          const err = await r.json().catch(() => ({}));
          this.showToast(err.error || 'Failed to delete notification agent', 'error', 8000);
          return;
        }
        const { [agent.id]: _, ...rest } = this.notifAgentStatus;
        this.notifAgentStatus = rest;
        await this.loadNotificationAgents();
      } catch (e) {
        this.showToast('Failed to delete notification agent: ' + e.message, 'error', 8000);
      }
    },

    async testNotificationAgent(agent) {
      this.notifAgentStatus = { ...this.notifAgentStatus, [agent.id]: { testing: true, results: [] } };
      try {
        // Handler uses stored credentials directly — no body needed.
        const r = await fetch(`/api/auto-sync/notification-agents/${agent.id}/test`, {
          method: 'POST',
        });
        const data = await r.json().catch(() => ({}));
        if (!r.ok) {
          this.notifAgentStatus = { ...this.notifAgentStatus, [agent.id]: { testing: false, results: [{ label: 'Error', status: 'error', error: data.error || 'Test failed' }] } };
          return;
        }
        this.notifAgentStatus = { ...this.notifAgentStatus, [agent.id]: { testing: false, results: data.results || [] } };
      } catch (e) {
        this.notifAgentStatus = { ...this.notifAgentStatus, [agent.id]: { testing: false, results: [{ label: 'Error', status: 'error', error: e.message }] } };
      }
    },

    agentIconSrc(type) {
      // Most providers ship an SVG; Gotify and Apprise ship a PNG bitmap.
      if (type === 'gotify' || type === 'apprise') {
        return `icons/${type}.png`;
      }
      return `icons/${type}.svg`;
    },

    // Returns true when every required field in the current agent type's
    // FieldSpec has a non-empty value. Driven by the manifest so adding a
    // new provider in Go automatically wires up its required-field check.
    // Falls back to false (Test disabled) when manifest hasn't loaded.
    agentModalCanTest() {
      const meta = this.manifestAgent(this.agentModal.type);
      if (!meta) return false;
      const c = this.agentModal.config;
      for (const g of (meta.fieldSpec?.groups || [])) {
        if (g.kind !== 'field' || !g.field || !g.field.required) continue;
        const v = c[g.field.name];
        if (g.field.kind === 'stringList') {
          if (!Array.isArray(v) || !v.some(s => (s || '').trim() !== '')) return false;
        } else {
          if (!v || !String(v).trim()) return false;
        }
      }
      return true;
    },

    async testAgentInModal() {
      this.agentModal.testing = true;
      this.agentModal.testResults = [];
      this.agentModal.testPassed = false;
      const payload = {
        name: this.agentModal.name.trim(),
        type: this.agentModal.type,
        enabled: this.agentModal.enabled,
        events: { ...this.agentModal.events },
        config: { ...this.agentModal.config },
      };
      // Editing resolves masked credentials server-side; adding posts raw config.
      const url = this.agentModal.editId
        ? `/api/auto-sync/notification-agents/${this.agentModal.editId}/test`
        : '/api/auto-sync/notification-agents/test';
      try {
        const r = await fetch(url, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
        const data = await r.json();
        if (!r.ok) {
          this.agentModal.testResults = [{ label: 'Error', status: 'error', error: data.error || 'Failed' }];
        } else {
          this.agentModal.testResults = data.results || [];
          this.agentModal.testPassed = this.agentModal.testResults.length > 0 &&
            this.agentModal.testResults.every(res => res.status === 'ok');
          // Sync row status for existing agents.
          if (this.agentModal.editId) {
            this.notifAgentStatus = { ...this.notifAgentStatus, [this.agentModal.editId]: { testing: false, results: this.agentModal.testResults } };
          }
        }
      } catch (e) {
        this.agentModal.testResults = [{ label: 'Error', status: 'error', error: e.message }];
      } finally {
        this.agentModal.testing = false;
      }
    },
  },
};
