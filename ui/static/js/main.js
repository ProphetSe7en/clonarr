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
import toasts from './features/toasts.js';

const featureModules = [
  navigation,
  toasts,
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

// Local modal focus trap used by the modal partials. It provides the subset of
// Alpine Focus we need without another plugin: tab containment, focus restore,
// optional scroll lock, and optional inert background content. Scroll/inert
// state is reference-counted so stacked prompts unwind cleanly.
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
let activeTooltipData = null;
let activeTooltipOwner = null;
let tooltipEscapeListenerRegistered = false;
let skipLinkHandlerRegistered = false;

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
        // Peer modal-overlays manage their own trap+inert lifecycle. If we
        // mark them inert here, a stacked modal opening on top inherits the
        // inert state and its buttons become unclickable (only ESC works,
        // since that's bound to window). cleanupResult → confirm-dialog
        // Delete-flow is the canonical repro.
        || (child instanceof Element && child.classList.contains('modal-overlay'))
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

function registerTooltipEscapeListener() {
  if (tooltipEscapeListenerRegistered) return;
  tooltipEscapeListenerRegistered = true;
  window.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape') return;
    if (activeTooltipData?.tt?.show && activeTooltipData.hideTooltip) {
      activeTooltipData.hideTooltip();
    }
    activeTooltipData = null;
    activeTooltipOwner = null;
  });
}

// Skip-link click → focus #main-content. preventDefault keeps location.hash
// clean — our hash routing would otherwise interpret #main-content as a nav
// target. Delegated listener so we don't need an inline onclick on the anchor
// (CSP-tightening compatibility — see docs/security TODO #8).
function registerSkipLinkHandler() {
  if (skipLinkHandlerRegistered) return;
  skipLinkHandlerRegistered = true;
  document.addEventListener('click', (event) => {
    const link = event.target.closest('.skip-link');
    if (!link) return;
    event.preventDefault();
    const main = document.getElementById('main-content');
    if (main) main.focus();
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
      // active app type. Triggers on profileTabs state change OR app-type
      // change while the History tab is active.
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
      // Maintenance: auto-select based on current app type
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
        this.config.pullSchedule = Object.assign({ mode: 'daily', time: '03:00', dayOfWeek: 0, dayOfMonth: 1 }, this.config.pullSchedule || {});
        this.config.syncSchedule = Object.assign({ enabled: false, mode: 'daily', time: '04:00', dayOfWeek: 0, dayOfMonth: 1 }, this.config.syncSchedule || {});
        // UI's mode dropdown uses 'disabled' as a sentinel for "off". Map the
        // backend's enabled=false (regardless of saved mode) to that sentinel
        // so the dropdown reflects the actual state. Saved mode is preserved
        // in a stash so we can restore it if user re-enables.
        if (!this.config.syncSchedule.enabled) {
          this._syncScheduleSavedMode = this.config.syncSchedule.mode || 'daily';
          this.config.syncSchedule.mode = 'disabled';
        }
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

    // The API stores scheduled pull times as container-local HH:MM. The
    // dropdown labels follow the browser's 12h/24h preference only for display.
    pullScheduleTimeParts() {
      const time = this.config?.pullSchedule?.time || '03:00';
      const match = time.match(/^(\d{2}):(\d{2})$/);
      if (!match) return { hour: 3, minute: 0 };
      const hour = Math.max(0, Math.min(23, parseInt(match[1], 10)));
      const minute = Math.max(0, Math.min(59, parseInt(match[2], 10)));
      return { hour, minute };
    },

    pullScheduleUses12Hour() {
      if (this._pullScheduleUses12Hour !== undefined) return this._pullScheduleUses12Hour;
      const opts = new Intl.DateTimeFormat(undefined, { hour: 'numeric' }).resolvedOptions();
      this._pullScheduleUses12Hour = opts.hourCycle
        ? opts.hourCycle === 'h11' || opts.hourCycle === 'h12'
        : new Intl.DateTimeFormat(undefined, { hour: 'numeric' }).formatToParts(new Date(2020, 0, 1, 13, 0)).some(p => p.type === 'dayPeriod');
      return this._pullScheduleUses12Hour;
    },

    browserTimeZoneName() {
      return Intl.DateTimeFormat().resolvedOptions().timeZone || 'local';
    },

    browserTimeZoneOffset() {
      return -new Date().getTimezoneOffset() * 60;
    },

    serverTimeZoneDisplay() {
      const zone = this.config?.serverTimeZone || 'container local time';
      if (!this.config?.serverTimeZoneConfigured && zone === 'UTC') return 'UTC (default)';
      return zone;
    },

    scheduleTimeZoneMismatch() {
      return Number.isFinite(this.config?.serverTimeZoneOffset) &&
        this.config.serverTimeZoneOffset !== this.browserTimeZoneOffset();
    },

    scheduleTimeZoneHelperText() {
      const server = this.serverTimeZoneDisplay();
      if (!this.scheduleTimeZoneMismatch()) return 'Schedules use container time: ' + server + '.';
      const browser = this.browserTimeZoneName();
      const setTZ = browser && browser !== 'local' ? ' Set TZ=' + browser + ' to schedule in your browser timezone.' : ' Set TZ to your local timezone to schedule in browser time.';
      return 'Schedules use container time: ' + server + '. Your browser time is ' + browser + '.' + setTZ;
    },

    pullScheduleHourOptions() {
      if (this.pullScheduleUses12Hour()) {
        return Array.from({ length: 12 }, (_, i) => {
          const value = i + 1;
          return { value, label: String(value) };
        });
      }
      return Array.from({ length: 24 }, (_, i) => ({ value: i, label: String(i).padStart(2, '0') }));
    },

    pullScheduleMinuteOptions() {
      if (this._pullScheduleMinuteOptions) return this._pullScheduleMinuteOptions;
      this._pullScheduleMinuteOptions = Array.from({ length: 60 }, (_, i) => ({ value: i, label: String(i).padStart(2, '0') }));
      return this._pullScheduleMinuteOptions;
    },

    pullSchedulePeriodOptions() {
      if (this._pullSchedulePeriodOptions) return this._pullSchedulePeriodOptions;
      const labelFor = (hour, fallback) => {
        const parts = new Intl.DateTimeFormat(undefined, { hour: 'numeric' }).formatToParts(new Date(2020, 0, 1, hour, 0));
        return parts.find(p => p.type === 'dayPeriod')?.value || fallback;
      };
      this._pullSchedulePeriodOptions = [
        { value: 'AM', label: labelFor(9, 'AM') },
        { value: 'PM', label: labelFor(21, 'PM') },
      ];
      return this._pullSchedulePeriodOptions;
    },

    pullScheduleHourValue() {
      const { hour } = this.pullScheduleTimeParts();
      if (!this.pullScheduleUses12Hour()) return hour;
      const h = hour % 12;
      return h === 0 ? 12 : h;
    },

    pullScheduleMinuteValue() {
      return this.pullScheduleTimeParts().minute;
    },

    pullSchedulePeriodValue() {
      return this.pullScheduleTimeParts().hour < 12 ? 'AM' : 'PM';
    },

    formatPullScheduleClock(hour, minute) {
      // Explicit hour12 flag so display matches the picker's mode. Without it,
      // Intl.DateTimeFormat picks based on the resolved locale's default cycle —
      // for en-US (common default even outside the US, when English sits high
      // in the browser language list) that's 12h, producing AM/PM next to the
      // container TZ label even when the picker's already showing 24h.
      const opts = { hour: 'numeric', minute: '2-digit', hour12: this.pullScheduleUses12Hour() };
      return new Intl.DateTimeFormat(undefined, opts).format(new Date(2020, 0, 1, hour, minute));
    },

    formatScheduleClockValue(value) {
      const match = String(value || '').match(/^(\d{2}):(\d{2})$/);
      if (!match) return '';
      return this.formatPullScheduleClock(parseInt(match[1], 10), parseInt(match[2], 10));
    },

    formatLocalClock(isoString) {
      if (!isoString) return '';
      try {
        return new Date(isoString).toLocaleTimeString([], { hour: 'numeric', minute: '2-digit', hour12: this.pullScheduleUses12Hour() });
      } catch {
        return '';
      }
    },

    setPullScheduleTime(hour, minute) {
      const h = Math.max(0, Math.min(23, Number(hour) || 0));
      const m = Math.max(0, Math.min(59, Number(minute) || 0));
      const next = String(h).padStart(2, '0') + ':' + String(m).padStart(2, '0');
      const changed = this.config.pullSchedule.time !== next;
      this.config.pullSchedule.time = next;
      return changed;
    },

    setPullScheduleHour(value) {
      const { hour, minute } = this.pullScheduleTimeParts();
      let nextHour = Number(value);
      if (this.pullScheduleUses12Hour()) {
        const period = hour < 12 ? 'AM' : 'PM';
        if (nextHour === 12) nextHour = 0;
        if (period === 'PM') nextHour += 12;
      }
      return this.setPullScheduleTime(nextHour, minute);
    },

    setPullScheduleMinute(value) {
      const { hour } = this.pullScheduleTimeParts();
      return this.setPullScheduleTime(hour, Number(value));
    },

    // ---- Auto-sync schedule clock helpers ----
    // Parallel to pull-schedule helpers above; same HH/MM/AM-PM dropdown shape
    // so the Settings UI has consistent design between Pull Interval and
    // Auto-sync Schedule. Reuses pullScheduleUses12Hour / pullScheduleHourOptions
    // / pullScheduleMinuteOptions / pullSchedulePeriodOptions (those don't
    // depend on the schedule — only on browser locale + minute range).

    syncScheduleTimeParts() {
      const time = this.config?.syncSchedule?.time || '04:00';
      const match = time.match(/^(\d{2}):(\d{2})$/);
      if (!match) return { hour: 4, minute: 0 };
      const hour = Math.max(0, Math.min(23, parseInt(match[1], 10)));
      const minute = Math.max(0, Math.min(59, parseInt(match[2], 10)));
      return { hour, minute };
    },

    syncScheduleHourValue() {
      const { hour } = this.syncScheduleTimeParts();
      if (!this.pullScheduleUses12Hour()) return hour;
      const h = hour % 12;
      return h === 0 ? 12 : h;
    },

    syncScheduleMinuteValue() {
      return this.syncScheduleTimeParts().minute;
    },

    syncSchedulePeriodValue() {
      return this.syncScheduleTimeParts().hour < 12 ? 'AM' : 'PM';
    },

    setSyncScheduleTime(hour, minute) {
      const h = Math.max(0, Math.min(23, Number(hour) || 0));
      const m = Math.max(0, Math.min(59, Number(minute) || 0));
      const next = String(h).padStart(2, '0') + ':' + String(m).padStart(2, '0');
      const changed = this.config.syncSchedule.time !== next;
      this.config.syncSchedule.time = next;
      return changed;
    },

    setSyncScheduleHour(value) {
      const { hour, minute } = this.syncScheduleTimeParts();
      let nextHour = Number(value);
      if (this.pullScheduleUses12Hour()) {
        const period = hour < 12 ? 'AM' : 'PM';
        if (nextHour === 12) nextHour = 0;
        if (period === 'PM') nextHour += 12;
      }
      return this.setSyncScheduleTime(nextHour, minute);
    },

    setSyncScheduleMinute(value) {
      const { hour } = this.syncScheduleTimeParts();
      return this.setSyncScheduleTime(hour, Number(value));
    },

    setSyncSchedulePeriod(value) {
      const { hour, minute } = this.syncScheduleTimeParts();
      const isPM = value === 'PM';
      let nextHour = hour % 12;
      if (isPM) nextHour += 12;
      return this.setSyncScheduleTime(nextHour, minute);
    },

    setPullSchedulePeriod(value) {
      const { hour, minute } = this.pullScheduleTimeParts();
      const isPM = value === 'PM';
      let nextHour = hour % 12;
      if (isPM) nextHour += 12;
      return this.setPullScheduleTime(nextHour, minute);
    },

    async saveConfig(fields) {
      try {
        const body = {};
        const pullScheduleChanged = fields && (fields.includes('pullInterval') || fields.includes('pullSchedule'));
        if (!fields || fields.includes('trashRepo')) body.trashRepo = this.config.trashRepo;
        if (fields && fields.includes('pullInterval')) {
          body.pullInterval = this.config.pullInterval;
          if (this.config.pullInterval === 'specific') body.pullSchedule = this.config.pullSchedule;
        }
        if (fields && fields.includes('pullSchedule')) body.pullSchedule = this.config.pullSchedule;
        if (fields && fields.includes('syncSchedule')) {
          // Translate UI sentinel 'disabled' back to enabled=false + a real
          // mode value (backend rejects mode==''). Also normalize time to
          // HH:MM exactly — some browsers' <input type="time"> emit HH:MM:SS,
          // which the backend validator (^\d{2}:\d{2}$) would silently reject.
          const ui = this.config.syncSchedule;
          const mode = ui.mode === 'disabled' ? (this._syncScheduleSavedMode || 'daily') : ui.mode;
          body.syncSchedule = {
            ...ui,
            enabled: ui.mode !== 'disabled',
            mode: mode,
            time: String(ui.time || '').slice(0, 5),
          };
          // Mirror the toggle decision back into local state so subsequent
          // saves (e.g. user picks Disabled then changes time) don't
          // re-enable inadvertently.
          this.config.syncSchedule.enabled = body.syncSchedule.enabled;
        }
        if (fields && fields.includes('devMode')) body.devMode = this.config.devMode;
        if (fields && fields.includes('trashSchemaFields')) body.trashSchemaFields = this.config.trashSchemaFields;
        if (fields && fields.includes('debugLogging')) body.debugLogging = this.config.debugLogging;
        if (fields && fields.includes('prowlarr')) body.prowlarr = this.config.prowlarr;
        // 401 handled centrally by the fetch wrapper.
        const r = await fetch('/api/config', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body)
        });
        if (!r.ok) {
          // Surface validation errors to the user instead of silently swallowing
          // (esp. for the new pull-schedule path where bad combinations now reject).
          let msg = 'Could not save settings';
          try { const data = await r.json(); if (data && data.error) msg = data.error; } catch {}
          this.showToast(msg, 'error', 6000);
          return;
        }
        if (pullScheduleChanged) await this.loadTrashStatus();
      } catch (e) {
        console.error('saveConfig:', e);
        this.showToast('Could not save settings (network error)', 'error', 6000);
      }
    },

    async loadTrashStatus() {
      try {
        const r = await fetch('/api/trash/status');
        if (!r.ok) return;
        this.trashStatus = await r.json();
        this._trashStatusFetchedAt = Date.now();
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
//   - Belt: index.html loads this module BEFORE the Alpine script
//     so document-order rules guarantee main.js runs first and the
//     alpine:init listener is registered before Alpine.start() fires it.
//   - Suspenders: if a future HTML edit reorders the tags, the
//     `if (window.Alpine)` branch catches the case where Alpine
//     already loaded — we just register directly.
function registerClonarr() {
  window.Alpine.data('clonarr', clonarr);
  registerModalTrapDirective(window.Alpine);
  registerTooltipEscapeListener();
  registerSkipLinkHandler();
  // x-tt="'tooltip text'" — viewport-aware custom tooltip directive.
  // Replaces native title="" for elements where the OS tooltip would overflow
  // the viewport (right-edge buttons, long messages). Wires hover, focus, and
  // shared Escape handling to showTooltip / hideTooltip on the root clonarr scope.
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

    // evaluateLater resolves on a microtask; track the current trigger so a
    // dynamic tooltip cannot appear after pointer/focus already left.
    const tooltipOwner = {};
    let currentEl = null;
    const onEnter = (e) => {
      currentEl = e.currentTarget;
      const target = e.currentTarget;
      getTipText((text) => {
        if (text && currentEl === target) {
          const data = window.Alpine.$data(el);
          if (data && data.showTooltip) {
            data.showTooltip(target, text);
            activeTooltipData = data;
            activeTooltipOwner = tooltipOwner;
          }
        }
      });
    };
    const onLeave = () => {
      currentEl = null;
      const data = window.Alpine.$data(el);
      if (activeTooltipOwner === tooltipOwner && data && data.hideTooltip) {
        data.hideTooltip();
        activeTooltipData = null;
        activeTooltipOwner = null;
      }
    };
    el.addEventListener('mouseenter', onEnter);
    el.addEventListener('mouseleave', onLeave);
    el.addEventListener('focusin', onEnter);
    el.addEventListener('focusout', onLeave);
    cleanup(() => {
      el.removeEventListener('mouseenter', onEnter);
      el.removeEventListener('mouseleave', onLeave);
      el.removeEventListener('focusin', onEnter);
      el.removeEventListener('focusout', onLeave);
      // If this directive owned the visible tooltip, hide it before
      // releasing ownership. Otherwise the tooltip element lingers in
      // the DOM when the host gets ripped out by an x-if / x-for change
      // mid-hover (e.g. clicking a button that empties the surrounding
      // template — the host vanishes before mouseleave fires).
      if (activeTooltipOwner === tooltipOwner) {
        if (activeTooltipData && activeTooltipData.hideTooltip) {
          activeTooltipData.hideTooltip();
        }
        activeTooltipData = null;
        activeTooltipOwner = null;
      }
    });
  });
}
if (window.Alpine) {
  registerClonarr();
} else {
  document.addEventListener('alpine:init', registerClonarr);
}
