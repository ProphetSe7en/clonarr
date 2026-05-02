import { csrfToken as readCSRFToken } from '../utils/csrf.js';
import { copyToClipboard } from '../utils/clipboard.js';

export default {
  state: {
    securityApiKey: '',
    securityApiKeyVisible: false,
    securityApiKeyCopied: false,
    securityRegenerating: false,
    securityRegenConfirm: false,
    pwChange: { current: '', next: '', confirm: '' },
    pwChangeSaving: false,
    pwChangeMsg: '',
    pwChangeOk: false,
    disableAuthModalOpen: false,
    disableAuthPassword: '',
    disableAuthError: '',
    authStatus: { configured: false, authenticated: false, username: '', localBypass: false, authentication: '', authenticationRequired: '', trustedNetworksLocked: false, trustedProxiesLocked: false, trustedNetworksEffective: '', trustedProxiesEffective: '', urlBase: '' },
    authStatusLoadError: false,
    noAuthBannerDismissed: false,
    securitySaveMsg: '',
    securitySaveOk: false,
  },

  methods: {
    // Used by the logout form. AJAX fetches get the header attached by api.js.
    csrfToken() {
      return readCSRFToken();
    },

    async fetchAuthStatus(retriesLeft = 2) {
      try {
        const resp = await fetch('/api/auth/status');
        if (!resp.ok) {
          // Retry on transient failure so locked fields render correctly.
          if (retriesLeft > 0) {
            setTimeout(() => this.fetchAuthStatus(retriesLeft - 1), 1000);
          } else {
            this.authStatusLoadError = true;
          }
          return;
        }
        this.authStatusLoadError = false;
        const data = await resp.json();
        const localBypass = data.configured && !data.authenticated && data.authentication !== 'none';
        const wasNone = this.authStatus.authentication === 'none';
        this.authStatus = {
          configured: data.configured,
          authenticated: data.authenticated,
          username: data.username || '',
          localBypass: localBypass,
          authentication: data.authentication || '',
          authenticationRequired: data.authentication_required || '',
          trustedNetworksLocked: !!data.trusted_networks_locked,
          trustedProxiesLocked: !!data.trusted_proxies_locked,
          trustedNetworksEffective: data.trusted_networks_effective || '',
          trustedProxiesEffective: data.trusted_proxies_effective || '',
          urlBase: data.url_base || '',
        };
        // When env-locked, reflect the effective value in the disabled input
        // so the user can see what's actually enforced. Only applies if
        // config has been populated (post-loadConfig).
        if (this.authStatus.trustedNetworksLocked && this.config) {
          this.config.trustedNetworks = this.authStatus.trustedNetworksEffective;
        }
        if (this.authStatus.trustedProxiesLocked && this.config) {
          this.config.trustedProxies = this.authStatus.trustedProxiesEffective;
        }
        if (this.authStatus.authentication === 'none') {
          this.noAuthBannerDismissed = localStorage.getItem('clonarr_noauth_banner_dismissed') === '1';
        } else {
          this.noAuthBannerDismissed = false;
          if (wasNone) localStorage.removeItem('clonarr_noauth_banner_dismissed');
        }
      } catch (e) {
        console.error('fetchAuthStatus:', e);
        if (retriesLeft > 0) {
          setTimeout(() => this.fetchAuthStatus(retriesLeft - 1), 1000);
        } else {
          this.authStatusLoadError = true;
        }
      }
    },

    dismissNoAuthBanner() {
      this.noAuthBannerDismissed = true;
      localStorage.setItem('clonarr_noauth_banner_dismissed', '1');
    },

    async fetchApiKey() {
      // 401 handled centrally by the fetch wrapper.
      try {
        const resp = await fetch('/api/auth/api-key');
        if (!resp.ok) return;
        const data = await resp.json();
        this.securityApiKey = data.api_key || '';
      } catch (e) { console.error('fetchApiKey:', e); }
    },

    async copyApiKey() {
      if (!this.securityApiKey) return;
      try {
        await copyToClipboard(this.securityApiKey);
        this.securityApiKeyCopied = true;
        setTimeout(() => { this.securityApiKeyCopied = false; }, 2000);
      } catch (e) { console.error('copyApiKey:', e); }
    },

    async regenerateApiKey() {
      this.securityRegenerating = true;
      try {
        const resp = await fetch('/api/auth/regenerate-api-key', { method: 'POST' });
        if (!resp.ok) { alert('Failed to regenerate API key'); return; }
        const data = await resp.json();
        this.securityApiKey = data.api_key || '';
        this.securityApiKeyVisible = true;
        this.securityRegenConfirm = false;
      } catch (e) { console.error('regenerateApiKey:', e); }
      finally { this.securityRegenerating = false; }
    },

    // Mirror of server-side validatePassword (internal/auth/auth.go): >=10
    // chars and >=2 of {upper, lower, digit, symbol}. Returns '' on valid,
    // error message on failure. Server re-validates unconditionally.
    pwComplexityError(pw) {
      if (!pw || pw.length < 10) return 'password must be at least 10 characters';
      let classes = 0;
      if (/[A-Z]/.test(pw)) classes++;
      if (/[a-z]/.test(pw)) classes++;
      if (/[0-9]/.test(pw)) classes++;
      if (/[^A-Za-z0-9]/.test(pw)) classes++;
      if (classes < 2) return 'password must contain at least 2 of: uppercase, lowercase, digit, symbol';
      return '';
    },

    async changePassword() {
      if (!this.pwChange.current || !this.pwChange.next || !this.pwChange.confirm) return;
      if (this.pwChange.next !== this.pwChange.confirm) {
        this.pwChangeOk = false; this.pwChangeMsg = 'New passwords do not match';
        return;
      }
      const complexityErr = this.pwComplexityError(this.pwChange.next);
      if (complexityErr) {
        this.pwChangeOk = false; this.pwChangeMsg = complexityErr;
        return;
      }
      this.pwChangeSaving = true; this.pwChangeMsg = '';
      try {
        const resp = await fetch('/api/auth/change-password', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({
            current_password: this.pwChange.current,
            new_password: this.pwChange.next,
            new_password_confirm: this.pwChange.confirm,
          }),
        });
        const data = await resp.json().catch(() => ({}));
        if (!resp.ok) {
          this.pwChangeOk = false;
          this.pwChangeMsg = data.error || 'Failed to change password';
          return;
        }
        this.pwChangeOk = true;
        this.pwChangeMsg = data.reauth_required ? 'Password changed. Please log in again.' : 'Password changed.';
        this.pwChange = { current: '', next: '', confirm: '' };
        setTimeout(() => { this.pwChangeMsg = ''; }, 5000);
      } catch (e) {
        this.pwChangeOk = false;
        this.pwChangeMsg = e.message || 'Network error';
      } finally {
        this.pwChangeSaving = false;
      }
    },

    async saveSecurityConfig(confirmedNone) {
      // Intercept auth=none transition — requires password confirmation.
      if (this.config.authentication === 'none' && !confirmedNone) {
        try {
          const resp = await fetch('/api/auth/status');
          if (resp.ok) {
            const data = await resp.json();
            if (data.authentication !== 'none') {
              this.disableAuthModalOpen = true;
              this.disableAuthPassword = '';
              this.disableAuthError = '';
              return;
            }
          }
        } catch (_) { /* fall through */ }
      }

      this.pwChangeSaving = true; // reuse the spinner state to disable button
      this.securitySaveMsg = '';
      const body = {
        authentication: this.config.authentication,
        authenticationRequired: this.config.authenticationRequired,
        sessionTtlDays: this.config.sessionTtlDays || 30,
      };
      // Omit env-locked fields from the save payload so the backend never
      // returns a 403 for values the UI can't edit anyway.
      if (!this.authStatus.trustedProxiesLocked) {
        body.trustedProxies = this.config.trustedProxies || '';
      }
      if (!this.authStatus.trustedNetworksLocked) {
        body.trustedNetworks = this.config.trustedNetworks || '';
      }
      if (confirmedNone && this.disableAuthPassword) {
        body.confirm_password = this.disableAuthPassword;
      }
      try {
        // Opt out of the central 401 -> /login redirect for confirmedNone:
        // here a 401 means "confirm_password incorrect", not "session expired".
        const headers = { 'Content-Type': 'application/json' };
        if (confirmedNone) headers['X-Skip-Login-Redirect'] = '1';
        const resp = await fetch('/api/config', {
          method: 'PUT',
          headers,
          body: JSON.stringify(body),
        });
        const data = await resp.json().catch(() => ({}));
        if (!resp.ok) {
          if (resp.status === 401 && confirmedNone) {
            this.disableAuthError = data.error || 'Password incorrect';
            this.disableAuthModalOpen = true;
            this.disableAuthPassword = '';
            return;
          }
          this.securitySaveOk = false;
          this.securitySaveMsg = data.error || 'Failed to save';
          return;
        }
        this.disableAuthPassword = '';
        this.securitySaveOk = true;
        this.securitySaveMsg = 'Saved';
        setTimeout(() => { this.securitySaveMsg = ''; }, 3000);
        this.fetchAuthStatus();
      } catch (e) {
        this.securitySaveOk = false;
        this.securitySaveMsg = e.message || 'Network error';
      } finally {
        this.pwChangeSaving = false;
      }
    },
  },
};
