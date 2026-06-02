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
import trashProfileDiscovery from './features/trash-profile-discovery.js';

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
  trashProfileDiscovery,
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
      // Collapsed-sidebar triggers (60px wide, anchored at the left edge)
      // would land their centered-above tooltip ~130px to the right of
      // the icon because the horizontal clamp below forces x ≥ halfMax+margin.
      // Anchor to the right of the trigger instead, vertically centered.
      if (el.closest('.sidebar.collapsed')) {
        const xR = (r.right + margin) / zoom;
        const yR = (r.top + r.height / 2) / zoom;
        this.tt = { show: true, text: text, x: xR, y: yR, flip: false, placement: 'right' };
        return;
      }
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
      this.tt = { show: true, text: text, x: x, y: y, flip: flip, placement: 'top' };
    },
    hideTooltip() {
      this.tt.show = false;
    },

    // v3 collapsed-sidebar sub-nav flyout — hover-based, VS Code activity-bar
    // pattern. Hover icon → 150ms delay → popup appears anchored to icon.
    // Move mouse to popup → stays open. Move away → 250ms delay → closes.
    // Switching between hovered sections is instant (no delay) when a popup
    // is already showing.
    //
    // Timers live on the Alpine instance but outside the reactive surface
    // (we don't watch them), so writing to them doesn't trigger re-renders.
    _sidebarHoverShowTimer: null,
    _sidebarHoverHideTimer: null,

    showSidebarSubnav(section, el) {
      if (!this.sidebarCollapsed) return;
      if (this._sidebarHoverHideTimer) { clearTimeout(this._sidebarHoverHideTimer); this._sidebarHoverHideTimer = null; }
      if (this.sidebarSubnavPopup === section) return; // already showing this section
      if (this.sidebarSubnavPopup) {
        // Switching from another section's popup — instant, no delay
        if (this._sidebarHoverShowTimer) { clearTimeout(this._sidebarHoverShowTimer); this._sidebarHoverShowTimer = null; }
        this.sidebarSubnavPopupTop = el.getBoundingClientRect().top;
        this.sidebarSubnavPopup = section;
        return;
      }
      // Fresh hover — delayed open avoids flicker on quick sweep-by
      if (this._sidebarHoverShowTimer) clearTimeout(this._sidebarHoverShowTimer);
      this._sidebarHoverShowTimer = setTimeout(() => {
        this.sidebarSubnavPopupTop = el.getBoundingClientRect().top;
        this.sidebarSubnavPopup = section;
        this._sidebarHoverShowTimer = null;
      }, 150);
    },
    scheduleHideSidebarSubnav() {
      // Cancel any pending show — user moved away before delay elapsed
      if (this._sidebarHoverShowTimer) { clearTimeout(this._sidebarHoverShowTimer); this._sidebarHoverShowTimer = null; }
      if (this._sidebarHoverHideTimer) clearTimeout(this._sidebarHoverHideTimer);
      this._sidebarHoverHideTimer = setTimeout(() => {
        this.sidebarSubnavPopup = '';
        this._sidebarHoverHideTimer = null;
      }, 250);
    },
    cancelHideSidebarSubnav() {
      if (this._sidebarHoverHideTimer) { clearTimeout(this._sidebarHoverHideTimer); this._sidebarHoverHideTimer = null; }
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
      // v3 sidebar mobile auto-collapse — sidebars on narrow viewports
      // (Unraid sub-window side-by-side, half-screen split, etc.) eat too
      // much real estate. Force collapsed below 1100px; restore the user's
      // last manual preference (from localStorage) when going wide again.
      // The manual toggleSidebar() button still writes to localStorage, so
      // the user's intentional choice wins on wide widths.
      const narrowMQ = matchMedia('(max-width: 1100px)');
      const applyNarrow = (matches) => {
        if (matches) {
          this.sidebarCollapsed = true;
        } else {
          this.sidebarCollapsed = localStorage.getItem('clonarr-sidebar-collapsed') === '1';
        }
      };
      narrowMQ.addEventListener('change', (e) => applyNarrow(e.matches));
      if (narrowMQ.matches) applyNarrow(true);
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

      // Issue #52 — guard sidebar anchor navigation when the profile
      // editor has unsaved changes. Sidebar links are pure <a href="#x">
      // anchors so hashchange fires AFTER the location update — too late
      // to revert without flicker. Intercept the click in the capture
      // phase, prevent default, show Stay/Discard via closeProfileEditor,
      // then manually navigate on Discard.
      document.addEventListener('click', (event) => {
        if (event.defaultPrevented) return;
        if (event.button !== 0) return;
        if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
        const anchor = event.target.closest('a[href^="#"]');
        if (!anchor) return;
        // Only guard nav-style anchors (sidebar + topnav). Filter
        // tooltip-anchors, in-page #section links, etc. by requiring
        // the href to look like our hash routes (radarr/sonarr/settings/
        // about) — anything else is a real in-page anchor we shouldn't
        // hijack.
        const href = anchor.getAttribute('href') || '';
        if (!/^#(radarr|sonarr|settings|about)(\/|$)/.test(href)) return;
        if (!this.profileDetail) return;
        if (typeof this.profileDetailIsDirty !== 'function' || !this.profileDetailIsDirty()) return;
        event.preventDefault();
        this.closeProfileEditor(() => {
          location.hash = href;
        });
      }, true);

      // Issue #52 — browser-level guard for reload / tab close / cross-
      // site navigation. Modern browsers ignore the returnValue text and
      // show their own generic "Leave site?" prompt, but setting
      // returnValue (or calling preventDefault) is enough to trigger it.
      window.addEventListener('beforeunload', (event) => {
        // Profile editor (Sync Preview) dirty-check from Issue #52.
        const profileDirty = typeof this.profileDetailIsDirty === 'function'
          && this.profileDetailIsDirty();
        // CF editor dirty-check — same pattern, applied to the CF
        // Editor modal (Create / Edit Custom Format). Without this a
        // browser reload while editing a CF silently drops the form.
        const cfDirty = this.showCFEditor
          && typeof this.cfEditorIsDirty === 'function'
          && this.cfEditorIsDirty();
        if (!profileDirty && !cfDirty) return;
        event.preventDefault();
        event.returnValue = '';
      });

      // Section change clears stale per-section state (was inline in the old
      // switchSection). Fires for both anchor clicks and hash restoration.
      this.$watch('currentSection', () => {
        this.profileDetail = null;
        this.syncPlan = null;
        this.syncResult = null;
        // v3: any section change closes the collapsed-sidebar sub-nav
        // popup. @click.outside doesn't fire reliably when navigation is
        // triggered from a sidebar anchor (Settings, About, etc.), so a
        // state-watcher is the robust way to dismiss it.
        this.sidebarSubnavPopup = '';
      });
      // Navigation into the Sync Rules tab triggers the customizations
      // cache load. We don't fire it from every loadAutoSyncRules call
      // (would hammer the backend on init / every toggle); instead the
      // tab-mount is the canonical entry point. Reactive on both
      // section and per-app profileTab changes.
      const maybeLoadCustomizations = () => {
        if (this.currentSection === 'profiles'
            && this.getProfileTab(this.activeAppType) === 'sync-rules'
            && typeof this.loadRuleCustomizations === 'function') {
          this.loadRuleCustomizations();
        }
      };
      this.$watch('currentSection', maybeLoadCustomizations);
      this.$watch('profileTabs', maybeLoadCustomizations);
      this.$watch('activeAppType', maybeLoadCustomizations);

      // Custom Formats → Sync Rules sub-tab: re-fetch the per-CF
      // state every time the user lands on the sub-tab (not just
      // the first time). Without this, auto-sync ticks that fire
      // server-side while the page is open would update PendingChanges
      // and CFDriftFingerprints but the open UI would keep showing
      // stale "in sync" pills until the user manually clicked Check.
      // Re-fetching on tab-visit + on visibility change covers both
      // the navigate-back and the return-to-tab cases.
      const maybeLoadCFSyncRules = () => {
        if (this.currentSection === 'custom-formats'
            && this.getCustomFormatsTab(this.activeAppType) === 'sync-rules'
            && typeof this.loadCFSyncRules === 'function') {
          this.loadCFSyncRules(this.activeAppType);
        }
      };
      this.$watch('currentSection', maybeLoadCFSyncRules);
      this.$watch('customFormatsTabs', maybeLoadCFSyncRules);
      // Attach the visibilitychange listener ONCE across the document
      // lifetime regardless of how many Alpine roots end up sharing this
      // init function. The window-level flag survives Alpine teardowns
      // and re-mounts (rare today since index.html has a single x-data
      // root, but defensive — split-root refactors otherwise stack a new
      // listener per init() call and refresh N times per focus event).
      if (!window._cfSyncRulesVisListenerAttached) {
        window._cfSyncRulesVisListenerAttached = true;
        document.addEventListener('visibilitychange', () => {
          if (document.visibilityState === 'visible') maybeLoadCFSyncRules();
        });
      }

      // Expanding the sidebar (Ctrl+B or click-toggle) closes the popup —
      // when the inline subnav becomes visible, the popup is redundant.
      // Also cancel any pending show-timer: if user was hovering an icon
      // and the 150ms delay hadn't elapsed yet, the timer would otherwise
      // open the popup AFTER the sidebar already expanded.
      this.$watch('sidebarCollapsed', (val) => {
        if (!val) {
          this.sidebarSubnavPopup = '';
          if (this._sidebarHoverShowTimer) { clearTimeout(this._sidebarHoverShowTimer); this._sidebarHoverShowTimer = null; }
          if (this._sidebarHoverHideTimer) { clearTimeout(this._sidebarHoverHideTimer); this._sidebarHoverHideTimer = null; }
        }
      });
      // Window resize closes the popup. The popup's captured top coord
      // becomes stale on resize (the icon's getBoundingClientRect changes
      // if the layout reflows), and the simplest robust answer is dismiss
      // rather than try to re-anchor mid-interaction.
      window.addEventListener('resize', () => {
        if (this.sidebarSubnavPopup) this.sidebarSubnavPopup = '';
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
        // Media Management — ensure the new app's Quality + Naming
        // instance data is loaded whenever we switch app types. Without
        // this, switching Radarr→Sonarr (or vice-versa) on Naming/
        // Quality showed an empty "Currently on instance" card until
        // the user clicked the picker.
        if (this.mediaInstanceId[appType]) {
          this.loadInstanceQS(appType, this.mediaInstanceId[appType]);
          this.loadInstanceNaming(appType);
        }
        // Custom Formats → Sync Rules sub-tab: per-app-type fetch, so
        // an app switch on this sub-tab needs to load the other app's
        // data on its own. Without this the table is stuck on a
        // "Loading custom formats..." spinner because the click handler
        // is the only other fetcher.
        if (this.currentSection === 'custom-formats'
          && this.getCustomFormatsTab(appType) === 'sync-rules'
          && typeof this.loadCFSyncRules === 'function') {
          this.loadCFSyncRules(appType);
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
          // Legacy alias: pre-v3 the Quality Definitions and File Naming
          // sections were top-level. v3 folds both into Media Management
          // with sub-tabs. A stored section value of 'quality-size' or
          // 'naming' now has no matching nav-item and would render a blank
          // page (gates check `currentSection === 'media-management'`).
          // Map to media-management and seed the corresponding sub-tab.
          // Mirrors the restoreFromHash() alias logic at navigation.js:195.
          if (savedSection === 'quality-size' || savedSection === 'naming') {
            this.currentSection = 'media-management';
            const seedAppType = savedAppType || (this.instances[0] && this.instances[0].type) || 'radarr';
            this.setMediaTab(seedAppType, savedSection === 'naming' ? 'naming' : 'quality');
          } else {
            this.currentSection = savedSection;
          }
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
      this.loadTrashProfileDescriptions('radarr');
      this.loadTrashProfileDescriptions('sonarr');
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
      const autoMedia = {};
      const autoCompare = {};
      const autoLoads = [];
      for (const type of ['radarr', 'sonarr']) {
        const typeInsts = this.instances.filter(i => i.type === type);
        if (typeInsts.length === 1) {
          const inst = typeInsts[0];
          autoCompare[type] = inst.id;
          autoMedia[type] = inst.id;
          autoLoads.push({ type, inst });
        }
      }
      // Assign entire objects to trigger Alpine reactivity
      if (Object.keys(autoCompare).length) this.compareInstanceIds = { ...this.compareInstanceIds, ...autoCompare };
      // v3 — Quality Definitions and Movie/Episode Naming share one
      // instance picker (mediaInstanceId) instead of separate qsInstanceId
      // and namingSelectedInstance, so the picker stays put when the
      // user switches between Media Management sub-tabs.
      if (Object.keys(autoMedia).length) this.mediaInstanceId = { ...this.mediaInstanceId, ...autoMedia };
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
        const prevLastRun = this.config?.profileSync?.lastRun || '';
        await this.loadTrashStatus();
        // Refresh ProfileSync state so the sidebar "TRaSH updates available"
        // indicator reflects what the backend watcher persisted on its
        // most-recent tick. Same 30s cadence as trashStatus so they stay
        // in step. Best-effort; failures are non-fatal.
        let lastRunChanged = false;
        try {
          const ps = await fetch('/api/profile-sync');
          if (ps.ok) {
            const data = await ps.json();
            // Merge in just the runner-managed fields; user-config fields
            // (mode, sources, interval) are managed by the Settings UI
            // and shouldn't be clobbered mid-edit.
            if (this.config.profileSync) {
              const newLastRun = data.lastRun || '';
              if (newLastRun && newLastRun !== prevLastRun) {
                lastRunChanged = true;
              }
              this.config.profileSync.lastRun = newLastRun;
              this.config.profileSync.upstreamHead = data.upstreamHead || '';
              this.config.profileSync.localHead = data.localHead || '';
            }
          }
        } catch (e) { /* network blip — try again next tick */ }
        // Always reload the rules list on the poll tick. Scheduled detection
        // (TRaSH upstream OR Arr drift) populates per-rule pendingChanges +
        // drift fingerprints on the backend without any frontend signal — and
        // drift updates DriftWatch.LastRun, not ProfileSync.LastRun, so the
        // lastRunChanged check below would miss drift-only detection. An
        // unconditional reload (the rules endpoint is cheap; the heavier
        // customizations fetch inside is already gated to the Sync Rules tab)
        // guarantees pills reflect backend state within one poll cycle
        // regardless of which detector fired. lastRunChanged is still tracked
        // for the toast logic below.
        void lastRunChanged;
        await this.loadAutoSyncRules();
        // If lastPull changed (scheduled pull completed), reload sync data
        if (this.trashStatus?.lastPull && this.trashStatus.lastPull !== prevPull) {
          // Show pull diff toast for scheduled pulls (only if diff is fresh — newCommit matches current)
          if (this.trashStatus.lastDiff?.summary && this.trashStatus.lastDiff.newCommit === this.trashStatus.commitHash) {
            const diffTime = new Date(this.trashStatus.lastDiff.time).getTime();
            if (Date.now() - diffTime < 60000) { // only if diff is less than 60s old
              // Preserve newlines — toast-content is pre-line, so the diff's
              // per-app + per-CF lines render as a readable list, not one
              // comma-run. Strip only the markdown bold markers.
              const summary = this.trashStatus.lastDiff.summary.replace(/\*\*/g, '').trim();
              this.showToast('TRaSH updated:\n' + summary, 'info', 10000);
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
        // Profile Sync — defaults match the backend migration so a fresh
        // install with no profileSync block in the API response renders
        // sensibly. Sources subobject is initialised so the checkbox bindings
        // don't have to null-guard.
        this.config.profileSync = Object.assign(
          { mode: 'auto', interval: '24h', sources: { trashUpstream: true, arrDrift: false } },
          this.config.profileSync || {}
        );
        this.config.profileSync.sources = Object.assign(
          { trashUpstream: true, arrDrift: false },
          this.config.profileSync.sources || {}
        );
        // Apply delay default — "Wait before applying" stores a single
        // integer minute count. Default 1440 (24h) so a fresh switch to
        // delayed mode has a sensible non-zero value.
        if (!this.config.profileSync.applyDelayMinutes) this.config.profileSync.applyDelayMinutes = 1440;
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


    setPullSchedulePeriod(value) {
      const { hour, minute } = this.pullScheduleTimeParts();
      const isPM = value === 'PM';
      let nextHour = hour % 12;
      if (isPM) nextHour += 12;
      return this.setPullScheduleTime(nextHour, minute);
    },

    // ---- Delayed-apply delay helpers ----
    // "Wait before applying" stores a single integer minute count
    // (config.profileSync.applyDelayMinutes). The Settings UI shows it as a
    // number + unit (minutes/hours/days); these helpers convert between the
    // two. Unit is derived from the stored minute count so the UI re-opens
    // with the friendliest representation (e.g. 1440 → "1 day", not
    // "1440 minutes"). A transient _applyDelayUnit override lets the user
    // pick a unit before typing a value without it snapping back.
    applyDelayUnit() {
      if (this._applyDelayUnitOverride) return this._applyDelayUnitOverride;
      const m = this.config?.profileSync?.applyDelayMinutes || 0;
      if (m > 0 && m % 1440 === 0) return 'days';
      if (m > 0 && m % 60 === 0) return 'hours';
      return 'minutes';
    },
    applyDelayValue() {
      const m = this.config?.profileSync?.applyDelayMinutes || 0;
      switch (this.applyDelayUnit()) {
        case 'days': return Math.max(1, Math.round(m / 1440));
        case 'hours': return Math.max(1, Math.round(m / 60));
        default: return Math.max(1, m);
      }
    },
    _applyDelayToMinutes(value, unit) {
      const v = Math.max(1, Number(value) || 1);
      switch (unit) {
        case 'days': return v * 1440;
        case 'hours': return v * 60;
        default: return v;
      }
    },
    setApplyDelayValue(value) {
      if (!this.config.profileSync) this.config.profileSync = {};
      this.config.profileSync.applyDelayMinutes = this._applyDelayToMinutes(value, this.applyDelayUnit());
    },
    setApplyDelayUnit(unit) {
      this._applyDelayUnitOverride = unit;
      // Re-express the current value in the new unit so the stored minute
      // count matches what the user now sees.
      this.setApplyDelayValue(this.applyDelayValue());
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

    // Save Mode + Sources to /api/profile-sync. Detection schedule (interval,
    // specific) flows through saveConfig() → PUT /api/config. The apply delay
    // (applyDelayMinutes) IS sent here — it lives only on ProfileSync and is
    // consulted only in "Wait before applying" mode.
    async saveProfileSync() {
      try {
        const ps = this.config.profileSync || {};
        const body = {
          mode: ps.mode,
          sources: ps.sources,
        };
        // Only send the apply delay in delayed mode so switching away doesn't
        // persist a value the backend would otherwise ignore anyway.
        if (ps.mode === 'delayed') {
          body.applyDelayMinutes = ps.applyDelayMinutes || 0;
        }
        const r = await fetch('/api/profile-sync', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body),
        });
        if (!r.ok) {
          const msg = (await r.json().catch(() => ({ error: 'save failed' }))).error || 'save failed';
          this.showToast(msg, 'error', 6000);
        }
      } catch (e) {
        console.error('saveProfileSync:', e);
        this.showToast('Could not save Profile Sync settings (network error)', 'error', 6000);
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
// Ctrl/Cmd+B toggles the v3 sidebar collapsed state. Pattern lifted from
// VS Code / Linear / Notion. Skips when a typeable element is focused so we
// don't steal "select to bold" inside text inputs.
function registerSidebarToggleShortcut() {
  document.addEventListener('keydown', (event) => {
    if (!(event.ctrlKey || event.metaKey)) return;
    if (event.key.toLowerCase() !== 'b') return;
    const target = event.target;
    const tag = (target?.tagName || '').toLowerCase();
    if (tag === 'input' || tag === 'textarea' || target?.isContentEditable) return;
    event.preventDefault();
    // Find the active clonarr Alpine root via the body's $data and call
    // toggleSidebar(). Falls back silently if Alpine isn't ready yet.
    const root = document.querySelector('[x-data="clonarr"]');
    const data = root && window.Alpine ? window.Alpine.$data(root) : null;
    if (data && typeof data.toggleSidebar === 'function') data.toggleSidebar();
  });
}

// `/` focuses the first visible page-search input on the current section.
// Industry standard (Slack, GitHub, Discord, Gmail, YouTube) — does NOT
// hijack browser Ctrl+F, which stays available for in-page find. Skips
// when a typeable element is already focused so it doesn't intercept the
// literal slash a user is trying to type into something else.
function registerSearchShortcut() {
  document.addEventListener('keydown', (event) => {
    if (event.key !== '/') return;
    if (event.ctrlKey || event.metaKey || event.altKey) return;
    const target = event.target;
    const tag = (target?.tagName || '').toLowerCase();
    if (tag === 'input' || tag === 'textarea' || target?.isContentEditable) return;
    // Find any visible search field marked with the convention class. Today
    // that's only the Custom Formats browse search; future search fields
    // (Profiles, History, etc.) tag themselves the same way and the
    // shortcut works without further wiring.
    const searches = document.querySelectorAll('.js-page-search');
    for (const el of searches) {
      const visible = el.offsetWidth > 0 && el.offsetHeight > 0;
      if (visible) {
        event.preventDefault();
        el.focus();
        if (typeof el.select === 'function') el.select();
        return;
      }
    }
  });
}

function registerClonarr() {
  window.Alpine.data('clonarr', clonarr);
  registerModalTrapDirective(window.Alpine);
  registerTooltipEscapeListener();
  registerSkipLinkHandler();
  registerSidebarToggleShortcut();
  registerSearchShortcut();
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
