import {
  copyToClipboard,
  genUUID,
  parseCategoryList,
  sanitizeHTML,
} from './api.js';

import baseState from './state.js';
import authSecurity from './features/auth-security.js';
import autoSync from './features/auto-sync.js';
import backupRestore from './features/backup-restore.js';
import cfGroupBuilder from './features/cf-group-builder.js';
import customFormats from './features/custom-formats.js';
import importExport from './features/import-export.js';
import instances from './features/instances.js';
import maintenance from './features/maintenance.js';
import manifest from './features/manifest.js';
import naming from './features/naming.js';
import navigation from './features/navigation.js';
import notifications from './features/notifications.js';
import profileBuilder from './features/profile-builder.js';
import profiles from './features/profiles.js';
import qualitySizes from './features/quality-sizes.js';
import scoring from './features/scoring.js';

const featureModules = [
  navigation,
  manifest,
  authSecurity,
  autoSync,
  instances,
  naming,
  notifications,
  qualitySizes,
  customFormats,
  importExport,
  profileBuilder,
  maintenance,
  backupRestore,
  profiles,
  scoring,
  cfGroupBuilder,
];

function applyFeatureModules(target) {
  for (const feature of featureModules) {
    Object.assign(target, feature.state || {});
  }
  for (const feature of featureModules) {
    Object.defineProperties(target, Object.getOwnPropertyDescriptors(feature.methods || {}));
  }
  return target;
}

const modalFocusableSelector = [
  'a[href]',
  'area[href]',
  'button:not([disabled])',
  'input:not([disabled]):not([type="hidden"])',
  'select:not([disabled])',
  'textarea:not([disabled])',
  'iframe',
  'object',
  'embed',
  '[contenteditable="true"]',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

let scrollLockCount = 0;
let scrollLockSnapshot = null;
const inertElementState = new WeakMap();
const activeModalTraps = [];

function isVisibleFocusable(el) {
  if (!(el instanceof HTMLElement)) return false;
  if (el.disabled || el.getAttribute('aria-hidden') === 'true') return false;
  const styles = window.getComputedStyle(el);
  if (styles.display === 'none' || styles.visibility === 'hidden') return false;
  return !!(el.offsetWidth || el.offsetHeight || el.getClientRects().length);
}

function focusableElementsIn(el) {
  return Array.from(el.querySelectorAll(modalFocusableSelector)).filter(isVisibleFocusable);
}

function lockDocumentScroll() {
  if (scrollLockCount === 0) {
    scrollLockSnapshot = {
      htmlOverflow: document.documentElement.style.overflow,
      bodyOverflow: document.body.style.overflow,
    };
    document.documentElement.style.overflow = 'hidden';
    document.body.style.overflow = 'hidden';
  }

  scrollLockCount += 1;
  let released = false;

  return () => {
    if (released) return;
    released = true;
    scrollLockCount = Math.max(0, scrollLockCount - 1);
    if (scrollLockCount !== 0 || !scrollLockSnapshot) return;

    document.documentElement.style.overflow = scrollLockSnapshot.htmlOverflow;
    document.body.style.overflow = scrollLockSnapshot.bodyOverflow;
    scrollLockSnapshot = null;
  };
}

function inertElement(el) {
  const existing = inertElementState.get(el);
  if (existing) {
    existing.count += 1;
    return;
  }

  inertElementState.set(el, {
    count: 1,
    hadInert: el.hasAttribute('inert'),
    inertValue: !!el.inert,
    ariaHidden: el.getAttribute('aria-hidden'),
  });
  el.inert = true;
  el.setAttribute('inert', '');
  el.setAttribute('aria-hidden', 'true');
}

function releaseInertElement(el) {
  const existing = inertElementState.get(el);
  if (!existing) return;

  existing.count -= 1;
  if (existing.count > 0) return;

  if (existing.hadInert) {
    el.setAttribute('inert', '');
  } else {
    el.removeAttribute('inert');
  }
  el.inert = existing.inertValue;
  if (existing.ariaHidden === null) {
    el.removeAttribute('aria-hidden');
  } else {
    el.setAttribute('aria-hidden', existing.ariaHidden);
  }
  inertElementState.delete(el);
}

function inertModalBackground(el) {
  const layer = el.closest('.modal-overlay') || el;
  const inerted = [];
  let branch = layer;

  while (branch && branch.parentElement) {
    const parent = branch.parentElement;
    for (const child of parent.children) {
      if (
        child === branch
        || child.tagName === 'SCRIPT'
        || child.tagName === 'STYLE'
        || child.contains(layer)
        || layer.contains(child)
      ) continue;
      inertElement(child);
      inerted.push(child);
    }
    if (parent === document.body) break;
    branch = parent;
  }

  return () => {
    inerted.forEach(releaseInertElement);
  };
}

function registerModalTrapDirective(Alpine) {
  Alpine.directive('trap', (el, { expression, modifiers }, { evaluateLater, effect, cleanup }) => {
    const evaluateOpen = evaluateLater(expression || 'false');
    const trap = {};
    let active = false;
    let previouslyFocused = null;
    let releaseScroll = null;
    let releaseInert = null;
    let addedTabindex = false;
    let focusTimer = null;

    const clearFocusTimer = () => {
      if (focusTimer === null) return;
      window.clearTimeout(focusTimer);
      focusTimer = null;
    };

    const focusFirst = () => {
      const target = focusableElementsIn(el)[0] || el;
      if (target instanceof HTMLElement) target.focus({ preventScroll: true });
    };

    const isTopTrap = () => activeModalTraps[activeModalTraps.length - 1] === trap;

    const onKeydown = (event) => {
      if (!active || !isTopTrap() || event.key !== 'Tab') return;

      const focusable = focusableElementsIn(el);
      if (focusable.length === 0) {
        event.preventDefault();
        el.focus({ preventScroll: true });
        return;
      }

      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      const current = document.activeElement;

      if (event.shiftKey && (current === first || !el.contains(current))) {
        event.preventDefault();
        last.focus({ preventScroll: true });
      } else if (!event.shiftKey && (current === last || !el.contains(current))) {
        event.preventDefault();
        first.focus({ preventScroll: true });
      }
    };

    const onFocusIn = (event) => {
      if (!active || !isTopTrap() || el.contains(event.target)) return;
      focusFirst();
    };

    const activate = () => {
      if (active) return;
      active = true;
      activeModalTraps.push(trap);
      previouslyFocused = document.activeElement instanceof HTMLElement ? document.activeElement : null;

      if (!el.hasAttribute('tabindex')) {
        el.setAttribute('tabindex', '-1');
        addedTabindex = true;
      }
      if (modifiers.includes('noscroll')) releaseScroll = lockDocumentScroll();
      if (modifiers.includes('inert')) releaseInert = inertModalBackground(el);

      document.addEventListener('keydown', onKeydown, true);
      document.addEventListener('focusin', onFocusIn, true);
      focusTimer = window.setTimeout(() => {
        focusTimer = null;
        if (active && el.isConnected && !el.contains(document.activeElement)) focusFirst();
      }, 0);
    };

    const deactivate = () => {
      if (!active) return;
      active = false;
      const stackIndex = activeModalTraps.lastIndexOf(trap);
      if (stackIndex !== -1) activeModalTraps.splice(stackIndex, 1);
      clearFocusTimer();
      document.removeEventListener('keydown', onKeydown, true);
      document.removeEventListener('focusin', onFocusIn, true);
      if (releaseInert) releaseInert();
      if (releaseScroll) releaseScroll();
      releaseInert = null;
      releaseScroll = null;
      if (addedTabindex) {
        el.removeAttribute('tabindex');
        addedTabindex = false;
      }
      if (previouslyFocused && previouslyFocused.isConnected && document.body.contains(previouslyFocused)) {
        previouslyFocused.focus({ preventScroll: true });
      }
      previouslyFocused = null;
    };

    effect(() => {
      evaluateOpen((open) => {
        if (open) activate();
        else deactivate();
      });
    });

    cleanup(deactivate);
  });
}

export function clonarr() {
  return applyFeatureModules({
    ...baseState(),
    get activeAppLabel() {
      return this.activeAppType.charAt(0).toUpperCase() + this.activeAppType.slice(1);
    },

    get availableAppTypes() {
      const types = new Set();
      for (const inst of this.instances) types.add(inst.type);
      const result = [];
      if (types.has('radarr') || types.size === 0) result.push('radarr');
      if (types.has('sonarr') || types.size === 0) result.push('sonarr');
      return result;
    },


    get maintenanceInstance() {
      return this.instances.find(i => i.id === this.maintenanceInstanceId) || null;
    },

    // Custom tooltip helpers — show/hide a viewport-aware tooltip anchored to
    // an element. Use instead of native title="" when the trigger element sits
    // near the right edge of the viewport (where the OS-level native tooltip
    // would render off-screen). Auto-flips below if too close to the top edge,
    // and clamps horizontal position so the tooltip can never escape the
    // viewport regardless of trigger location.
    showTooltip(el, text) {
      if (!text || !el) return;
      const r = el.getBoundingClientRect();
      const margin = 8;
      // CSS max-width 320 / 2.
      const halfMax = 160;
      // Coordinate-system bridge: getBoundingClientRect() and innerWidth
      // return ACTUAL screen pixels (post-zoom). style.left/top we set are
      // interpreted as CSS pixels and then zoom-scaled by the browser at
      // render time. With UI Scale != 1 (zoom on <html>), assigning the raw
      // actual-pixel value as style.left makes the tooltip render at
      // (value * zoom) actual pixels — offset from the trigger by the zoom
      // factor. Divide every actual-pixel measurement by zoom so the values
      // round-trip back to the same physical position. Defaults to 1 when
      // no zoom is applied.
      const zoom = parseFloat(getComputedStyle(document.documentElement).zoom) || 1;
      const triggerCenterX = (r.left + r.width / 2) / zoom;
      const triggerTop = r.top / zoom;
      const triggerBottom = r.bottom / zoom;
      const viewportW = window.innerWidth / zoom;
      let y = triggerTop;
      let flip = false;
      if (triggerTop < 60) {
        y = triggerBottom;
        flip = true;
      }
      const x = Math.max(halfMax + margin, Math.min(triggerCenterX, viewportW - halfMax - margin));
      this.tt = { show: true, text: text, x: x, y: y, flip: flip };
    },
    hideTooltip() {
      this.tt.show = false;
    },
    async init() {
      // Apply saved UI scale. `zoom` is a Chromium-original property that
      // Firefox only added in v126 (May 2024); the CSS.supports guard avoids
      // a no-op assignment on older Firefox. Modern browsers all support it.
      if (this.uiScale !== '1' && CSS.supports('zoom', '1')) document.documentElement.style.zoom = this.uiScale;
      // Apply theme. The inline pre-paint script in index.html already set
      // data-theme to avoid FOUC; this re-applies it once Alpine state exists
      // and registers a matchMedia listener so "system" tracks OS changes live.
      this.applyTheme();
      matchMedia('(prefers-color-scheme: light)').addEventListener('change', () => {
        if (this.theme === 'system') this.applyTheme();
      });
      // Load the UI manifest first — it carries enum option lists, agent
      // field specs, and category-color tokens that downstream renders need.
      // Awaited so getCategoryClass() / agent modal lookups don't race on
      // initial render. Endpoint payload is small (~3 KB) and cached for 60s.
      await this.loadManifest();
      // Reactive validation: any change to qualityStructure (rename, delete, merge, toggle)
      // re-validates pdOverrides.cutoffQuality and resets it to first allowed if it became invalid.
      this.$watch('qualityStructure', () => this.qsValidateCutoff());
      // Builder: auto-assign stable _id to every pb.qualityItems entry on any reassignment
      // (Apply template/preset/instance, group add/remove). Needed so shared qs-helpers can
      // track drag/drop/rename/expand by identity. pbEnsureQualityIds is idempotent — the
      // spread-reassignment inside only fires when something actually changed, so the watch
      // settles after one tick.
      this.$watch('pb.qualityItems', () => this.pbEnsureQualityIds());
      // Scoring Sandbox must run loadSandbox whenever the page becomes
      // visible — otherwise sb.instanceId stays empty and Score Selected
      // returns early + Instance Profiles dropdown stays empty. The
      // existing call from switchAppType only fires on app-type change,
      // not on direct URL/hash navigation or section/sub-tab switches.
      const ensureSandbox = () => {
        if (this.currentSection === 'advanced' && this.advancedTab === 'scoring') {
          this.loadSandbox(this.activeAppType);
        }
      };
      this.$watch('advancedTab', ensureSandbox);
      this.$watch('currentSection', ensureSandbox);

      // Nav anchors set location.hash directly, which fires hashchange (not
      // popstate). Mirror the popstate handler so anchor clicks restore state.
      // restoreFromHash early-returns when hash already matches state, so this
      // is safe to fire alongside watchers that call pushNav.
      window.addEventListener('hashchange', () => this.restoreFromHash(location.hash));

      // Section change clears stale per-section state (was inline in the old
      // switchSection). Fires for both anchor clicks and hash restoration.
      this.$watch('currentSection', () => {
        this.profileDetail = null;
        this.syncPlan = null;
        this.syncResult = null;
      });

      // Settings → Security loads the API key. Was inline on the old
      // settings-nav @click; now driven by state so right-click → "Open in
      // new tab" on `#settings/security` also fetches.
      this.$watch('settingsSection', (s) => {
        if (s === 'security') this.fetchApiKey();
      });

      // Advanced → CF Group Builder loads CFs/profiles for the active app
      // type. Was inline on the @click; now state-driven.
      this.$watch('advancedTab', (t) => {
        if (this.currentSection === 'advanced' && t === 'group-builder') {
          this.cfgbLoad(this.activeAppType);
        }
      });

      // Profiles → History loads sync history for every instance of the
      // active app type. Triggers on tab change OR app-type change while the
      // History tab is active.
      const ensureHistory = () => {
        if (this.currentSection === 'profiles' && this.getProfileTab(this.activeAppType) === 'history') {
          this.instancesOfType(this.activeAppType).forEach(i => this.loadSyncHistory(i.id));
        }
      };
      this.$watch('profileTabs', ensureHistory);

      // App-type change (Radarr ↔ Sonarr) replays the side-effects that used
      // to live in _doSwitchAppType: clear stale per-section state, auto-pick
      // the maintenance instance when only one exists for the new type, and
      // reload Advanced sub-tabs that are app-type-scoped.
      this.$watch('activeAppType', (appType) => {
        this.profileDetail = null;
        this.syncPlan = null;
        this.syncResult = null;
        const typeInsts = this.instances.filter(i => i.type === appType);
        if (typeInsts.length === 1) {
          this.maintenanceInstanceId = typeInsts[0].id;
          this.cleanupInstanceId = typeInsts[0].id;
          this.loadCleanupKeep();
          this.loadCleanupCFNames();
        }
        if (this.currentSection === 'advanced') {
          if (this.advancedTab === 'group-builder') this.cfgbLoad(appType);
          else if (this.advancedTab === 'scoring') this.loadSandbox(appType);
        }
        ensureHistory();
      });
      await this.loadConfig();
      this.fetchAuthStatus(); // render header user-menu and banner state early
      await this.loadInstances();
      await this.loadTrashStatus();
      // Restore navigation from URL hash (browser back/forward) or localStorage fallback.
      // Hash takes priority — it carries the exact section+subtab the user was on.
      window.addEventListener('popstate', () => this.restoreFromHash(location.hash));
      const oldTab = localStorage.getItem('clonarr_tab');
      if (location.hash && this.restoreFromHash(location.hash)) {
        // hash restored — skip localStorage
      } else {
        const savedSection = localStorage.getItem('clonarr_section');
        const savedAppType = localStorage.getItem('clonarr_appType');
        if (savedSection) {
          this.currentSection = savedSection;
        } else if (oldTab === 'settings' || oldTab === 'about') {
          this.currentSection = oldTab;
        }
        if (savedAppType && this.instances.some(i => i.type === savedAppType)) {
          this.activeAppType = savedAppType;
        } else if (oldTab && this.instances.some(i => i.type === oldTab)) {
          this.activeAppType = oldTab;
        } else if (this.instances.length > 0) {
          this.activeAppType = this.instances[0].type;
        }
      }
      // Seed the initial history entry so the first Back click has somewhere to go.
      history.replaceState(null, '', this.buildNavHash());
      // LEGACY: keep currentTab in sync until full migration
      if (oldTab && (oldTab === 'settings' || oldTab === 'about' || this.instances.some(i => i.type === oldTab))) {
        this.currentTab = oldTab;
      } else if (this.instances.length > 0) {
        this.currentTab = this.instances[0].type;
      }
      this.loadTrashProfiles('radarr');
      this.loadTrashProfiles('sonarr');
      this.loadQualitySizes('radarr');
      this.loadQualitySizes('sonarr');
      this.loadCFBrowse('radarr');
      this.loadCFBrowse('sonarr');
      this.loadConflicts('radarr');
      this.loadConflicts('sonarr');
      this.loadNaming('radarr');
      this.loadNaming('sonarr');
      this.loadImportedProfiles('radarr');
      this.loadImportedProfiles('sonarr');
      this.loadAutoSyncSettings();
      this.loadNotificationAgents();
      this.loadAutoSyncRules();
      this.loadSandboxResults('radarr');
      this.loadSandboxResults('sonarr');
      this.sandboxLoadScoreSets('radarr');
      this.sandboxLoadScoreSets('sonarr');
      // Load sync history for all instances (also triggers stale cleanup)
      for (const inst of this.instances) {
        await this.loadInstanceProfiles(inst);
        await this.loadSyncHistory(inst.id);
      }
      this.checkCleanupEvents();
      // Auto-select instance if only one per type (no need to choose)
      // Build auto-select maps, then assign all at once for Alpine reactivity
      const autoQs = {};
      const autoNaming = {};
      const autoCompare = {};
      const autoLoads = [];
      for (const type of ['radarr', 'sonarr']) {
        const typeInsts = this.instances.filter(i => i.type === type);
        if (typeInsts.length === 1) {
          const inst = typeInsts[0];
          autoCompare[type] = inst.id;
          autoQs[type] = inst.id;
          autoNaming[type] = inst.id;
          autoLoads.push({ type, inst });
        }
      }
      // Assign entire objects to trigger Alpine reactivity
      if (Object.keys(autoCompare).length) this.compareInstanceIds = { ...this.compareInstanceIds, ...autoCompare };
      if (Object.keys(autoQs).length) this.qsInstanceId = { ...this.qsInstanceId, ...autoQs };
      if (Object.keys(autoNaming).length) this.namingSelectedInstance = { ...this.namingSelectedInstance, ...autoNaming };
      // Load data for auto-selected instances
      for (const { type, inst } of autoLoads) {
        this.loadInstanceProfiles(inst);
        this.loadInstanceQS(type, inst.id);
        this.loadInstanceNaming(type);
      }
      // Maintenance: auto-select based on current tab type
      const currentType = this.activeAppType;
      const maintInsts = this.instances.filter(i => i.type === currentType);
      if (maintInsts.length === 1) {
        this.maintenanceInstanceId = maintInsts[0].id;
        this.cleanupInstanceId = maintInsts[0].id;
        this.loadCleanupKeep();
        this.loadCleanupCFNames();
      }
      // Test all instances on load
      this.testAllInstances();
      // Tick every 30s: update timeAgo() and refresh TRaSH status
      setInterval(async () => {
        this._nowTick = Date.now();
        const prevPull = this.trashStatus?.lastPull;
        await this.loadTrashStatus();
        // If lastPull changed (scheduled pull completed), reload sync data
        if (this.trashStatus?.lastPull && this.trashStatus.lastPull !== prevPull) {
          // Show pull diff toast for scheduled pulls (only if diff is fresh — newCommit matches current)
          if (this.trashStatus.lastDiff?.summary && this.trashStatus.lastDiff.newCommit === this.trashStatus.commitHash) {
            const diffTime = new Date(this.trashStatus.lastDiff.time).getTime();
            if (Date.now() - diffTime < 60000) { // only if diff is less than 60s old
              const summary = this.trashStatus.lastDiff.summary.replace(/\*\*/g, '').replace(/^\n/, '').replace(/\n/g, ', ').replace(/:,/g, ':');
              this.showToast('TRaSH updated: ' + summary, 'info', 10000);
            }
          }
          await this.loadAutoSyncRules();
          for (const inst of this.instances) {
            await this.loadSyncHistory(inst.id);
          }
          // Delay auto-sync event check — auto-sync runs async after pull completes
          setTimeout(() => this.checkAutoSyncEvents(), 5000);
        }
      }, 30000);
      // Re-test instances every 60 seconds
      setInterval(() => this.testAllInstances(), 60000);
      // Initial-state coverage: the watchers above only fire when
      // currentSection / advancedTab actually CHANGE. If the user
      // landed on the scoring tab from URL/localStorage at boot, the
      // watchers stay silent so we call once explicitly.
      ensureSandbox();
    },

    async loadConfig() {
      try {
        const r = await fetch('/api/config');
        if (!r.ok) return;
        this.config = await r.json();
        // Ensure prowlarr config object exists
        if (!this.config.prowlarr) this.config.prowlarr = { url: '', apiKey: '', enabled: false, radarrCategories: [], sonarrCategories: [] };
        // Back-fill missing arrays for configs saved before category overrides existed.
        if (!this.config.prowlarr.radarrCategories) this.config.prowlarr.radarrCategories = [];
        if (!this.config.prowlarr.sonarrCategories) this.config.prowlarr.sonarrCategories = [];
        // If auth status has already loaded AND trust-boundary fields are
        // env-locked, display the effective value so the user sees what's
        // actually enforced.
        if (this.authStatus.trustedNetworksLocked) {
          this.config.trustedNetworks = this.authStatus.trustedNetworksEffective;
        }
        if (this.authStatus.trustedProxiesLocked) {
          this.config.trustedProxies = this.authStatus.trustedProxiesEffective;
        }
      } catch (e) { console.error('loadConfig:', e); }
    },

    async saveConfig(fields) {
      try {
        const body = {};
        if (!fields || fields.includes('trashRepo')) body.trashRepo = this.config.trashRepo;
        if (fields && fields.includes('pullInterval')) body.pullInterval = this.config.pullInterval;
        if (fields && fields.includes('devMode')) body.devMode = this.config.devMode;
        if (fields && fields.includes('trashSchemaFields')) body.trashSchemaFields = this.config.trashSchemaFields;
        if (fields && fields.includes('debugLogging')) body.debugLogging = this.config.debugLogging;
        if (fields && fields.includes('prowlarr')) body.prowlarr = this.config.prowlarr;
        // 401 handled centrally by the fetch wrapper.
        await fetch('/api/config', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body)
        });
      } catch (e) { console.error('saveConfig:', e); }
    },

    async loadTrashStatus() {
      try {
        const r = await fetch('/api/trash/status');
        if (!r.ok) return;
        this.trashStatus = await r.json();
      } catch (e) { console.error('loadTrashStatus:', e); }
    },

    async loadTrashProfiles(appType) {
      try {
        const r = await fetch(`/api/trash/${appType}/profiles`);
        if (r.ok) {
          const data = await r.json();
          this.trashProfiles = { ...this.trashProfiles, [appType]: data };
        }
      } catch (e) { /* not yet cloned */ }
    },

  });
}


// HTML helpers used directly from inline @click / x-html expressions
// (e.g. @click="copyToClipboard(...)", x-html="sanitizeHTML(...)") must
// remain on window so Alpine evaluates them in scope. clonarr itself no
// longer goes on window — Alpine resolves it via Alpine.data() lookup.
Object.assign(window, {
  copyToClipboard,
  genUUID,
  parseCategoryList,
  sanitizeHTML,
});

// Register the clonarr() data factory with Alpine.
//
// Belt-and-suspenders ordering:
//   - Belt: index.html loads this module BEFORE the Alpine CDN <script>
//     so document-order rules guarantee main.js runs first and the
//     alpine:init listener is registered before Alpine.start() fires it.
//   - Suspenders: if a future HTML edit reorders the tags, the
//     `if (window.Alpine)` branch catches the case where Alpine
//     already loaded — we just register directly.
function registerClonarr() {
  window.Alpine.data('clonarr', clonarr);
  registerModalTrapDirective(window.Alpine);
  // x-tt="'tooltip text'" — viewport-aware custom tooltip directive.
  // Replaces native title="" for elements where the OS tooltip would overflow
  // the viewport (right-edge buttons, long messages). Wires mouseenter/leave
  // listeners that call showTooltip / hideTooltip on the root clonarr scope.
  // Static text:   x-tt="'Reset all overrides'"
  // Dynamic text:  x-tt="someDynamicExpr"
  window.Alpine.directive('tt', (el, { expression }, { evaluateLater, cleanup }) => {
    const getTipText = evaluateLater(expression);
    let idStr = el.getAttribute('id');
    if (!idStr) {
      idStr = 'tt-' + Math.random().toString(36).substr(2, 9);
      el.setAttribute('id', idStr);
    }
    el.setAttribute('aria-describedby', 'global-tooltip');

    let currentEl = null;
    const onEnter = (e) => {
      currentEl = e.currentTarget;
      const target = e.currentTarget;
      getTipText((text) => {
        if (text && currentEl === target) {
          window.Alpine.$data(el).showTooltip(target, text);
        }
      });
    };
    const onLeave = () => {
      currentEl = null;
      const data = window.Alpine.$data(el);
      if (data && data.hideTooltip) data.hideTooltip();
    };
    const onEsc = (e) => {
      if (e.key === 'Escape') {
        const data = window.Alpine.$data(el);
        if (data && data.tt && data.tt.show) data.hideTooltip();
      }
    };
    el.addEventListener('mouseenter', onEnter);
    el.addEventListener('mouseleave', onLeave);
    el.addEventListener('focusin', onEnter);
    el.addEventListener('focusout', onLeave);
    window.addEventListener('keydown', onEsc);
    cleanup(() => {
      el.removeEventListener('mouseenter', onEnter);
      el.removeEventListener('mouseleave', onLeave);
      el.removeEventListener('focusin', onEnter);
      el.removeEventListener('focusout', onLeave);
      window.removeEventListener('keydown', onEsc);
    });
  });
}
if (window.Alpine) {
  registerClonarr();
} else {
  document.addEventListener('alpine:init', registerClonarr);
}
