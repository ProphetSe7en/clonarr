export default {
  state: {
    _navSkipPush: false,
  },
  methods: {
    // Legacy tab-switching helpers are kept for older call sites. New
    // navigation should use real anchors from navHref(), with section/app
    // side effects attached to state watchers in init().
    switchTab(tab) {
      this.debugLog('UI', `Tab: ${tab}`);
      this.currentTab = tab;
      localStorage.setItem('clonarr_tab', tab);
      this.profileDetail = null;
      this.syncPlan = null;
      this.syncResult = null;
      // Auto-select maintenance instance for this legacy app tab if only one.
      const typeInsts = this.instances.filter(i => i.type === tab);
      if (typeInsts.length === 1 && this.maintenanceInstanceId !== typeInsts[0].id) {
        this.maintenanceInstanceId = typeInsts[0].id;
        this.cleanupInstanceId = typeInsts[0].id;
        this.loadCleanupKeep();
        this.loadCleanupCFNames();
      }
    },

    switchSection(section) {
      // Issue #52 — guard against silently losing unsaved profile-editor
      // changes when the user clicks a different sidebar section.
      // closeProfileEditor handles the dirty-check + Stay/Discard modal;
      // we run the actual section switch from the done callback so a
      // Stay choice cancels navigation entirely (no section change).
      if (this.profileDetail && typeof this.profileDetailIsDirty === 'function' && this.profileDetailIsDirty()) {
        this.closeProfileEditor(() => this._doSwitchSection(section));
        return;
      }
      this._doSwitchSection(section);
    },

    _doSwitchSection(section) {
      this.debugLog('UI', `Section: ${section}`);
      this.currentSection = section;
      localStorage.setItem('clonarr_section', section);
      this.profileDetail = null;
      this.syncPlan = null;
      this.syncResult = null;
      this.pushNav();
    },

    switchAppType(appType) {
      if (appType === this.activeAppType) {
        this._doSwitchAppType(appType);
        return;
      }
      // Issue #52 — profile editor unsaved-changes guard. Reuses the
      // same Stay/Discard pattern as closeProfileEditor so the user
      // gets consistent treatment whether they Cancel or app-switch.
      if (this.profileDetail && typeof this.profileDetailIsDirty === 'function' && this.profileDetailIsDirty()) {
        this.closeProfileEditor(() => this._doSwitchAppType(appType));
        return;
      }
      // Guard unsaved CF Group Builder work: the builder is app-type-scoped,
      // so switching triggers cfgbLoad → cfgbReset which would discard an
      // in-flight edit. Warn via the styled confirm modal (browser's native
      // confirm() was jarring and didn't match the rest of the app).
      const shouldPrompt = this.currentSection === 'advanced'
        && this.advancedTab === 'group-builder'
        && typeof this.cfgbIsDirty === 'function' && this.cfgbIsDirty();
      if (shouldPrompt) {
        const label = this.cfgbEditingId
          ? 'changes to "' + (this.cfgbName || '(unnamed)') + '"'
          : 'the unsaved cf-group draft';
        this.confirmModal = {
          show: true,
          title: 'Discard unsaved cf-group work?',
          message: 'Switch to ' + appType + ' and discard ' + label + '?\n\nThe saved copy on disk (if any) is unaffected.',
          confirmLabel: 'Switch to ' + appType,
          onConfirm: () => this._doSwitchAppType(appType),
          onCancel: () => {},
        };
        return;
      }
      this._doSwitchAppType(appType);
    },

    _doSwitchAppType(appType) {
      this.debugLog('UI', `App type: ${appType}`);
      this.activeAppType = appType;
      localStorage.setItem('clonarr_appType', appType);
      this.pushNav();
      this.profileDetail = null;
      this.syncPlan = null;
      this.syncResult = null;
      // Auto-select maintenance instance for this type
      const typeInsts = this.instances.filter(i => i.type === appType);
      if (typeInsts.length === 1) {
        this.maintenanceInstanceId = typeInsts[0].id;
        this.cleanupInstanceId = typeInsts[0].id;
        this.loadCleanupKeep();
        this.loadCleanupCFNames();
      }
      // Reload app-scoped Advanced data. The CF Group Builder
      // pulls CFs, profiles, and saved groups per Radarr/Sonarr — without this
      // the Radarr list keeps showing when the user flips to Sonarr.
      // Scoring Sandbox has the same issue; reload it too for parity.
      if (this.currentSection === 'advanced') {
        if (this.advancedTab === 'group-builder') this.cfgbLoad(appType);
        else if (this.advancedTab === 'scoring') this.loadSandbox(appType);
      }
    },

    // --- v3 sidebar collapse ---
    // localStorage-backed boolean; flipped by the Collapse button in the
    // sidebar header and by Ctrl/Cmd+B keyboard shortcut.
    toggleSidebar() {
      this.sidebarCollapsed = !this.sidebarCollapsed;
      localStorage.setItem('clonarr-sidebar-collapsed', this.sidebarCollapsed ? '1' : '0');
    },

    // --- Hash routing (back/forward, bookmarks, copyable nav links) ---
    // Hash format: #appType/section[/subtab] — e.g. #radarr/profiles/compare, #settings/prowlarr, #about
    buildNavHash() {
      const s = this.currentSection;
      if (s === 'settings') return '#settings/' + (this.settingsSection || 'instances');
      if (s === 'about') return '#about';
      const app = this.activeAppType;
      let hash = '#' + app + '/' + s;
      if (s === 'profiles') hash += '/' + (this.getProfileTab(app) || 'trash-profiles');
      else if (s === 'custom-formats') hash += '/' + (this.getCustomFormatsTab(app) || 'browse');
      else if (s === 'media-management') hash += '/' + (this.getMediaTab(app) || 'quality');
      else if (s === 'maintenance') hash += '/' + (this.getMaintenanceTab(app) || 'backup');
      else if (s === 'advanced') hash += '/' + (this.advancedTab || 'builder');
      return hash;
    },

    // navHref builds the hash that a target section/sub-tab would produce,
    // without mutating any state. Used by nav anchors so right-click → "Open
    // in new tab" and "Copy link address" work, and the browser can show the
    // URL on hover.
    //
    // opts: { appType, profileTab, advancedTab, settingsSection } — each
    // defaults to the current state when omitted.
    navHref(section, opts = {}) {
      if (section === 'settings') {
        return '#settings/' + (opts.settingsSection || this.settingsSection || 'instances');
      }
      if (section === 'about') return '#about';
      const app = opts.appType || this.activeAppType;
      let hash = '#' + app + '/' + section;
      if (section === 'profiles') {
        hash += '/' + (opts.profileTab || this.getProfileTab(app) || 'trash-profiles');
      } else if (section === 'custom-formats') {
        hash += '/' + (opts.customFormatsTab || this.getCustomFormatsTab(app) || 'browse');
      } else if (section === 'media-management') {
        hash += '/' + (opts.mediaTab || this.getMediaTab(app) || 'quality');
      } else if (section === 'maintenance') {
        hash += '/' + (opts.maintenanceTab || this.getMaintenanceTab(app) || 'backup');
      } else if (section === 'advanced') {
        hash += '/' + (opts.advancedTab || this.advancedTab || 'builder');
      }
      return hash;
    },

    // cfgbNeedsConfirm intercepts an app-type anchor click when the CF Group
    // Builder has unsaved work. Returns true (and pops a confirm modal) only
    // for plain left-clicks; modifier-clicks (Ctrl/Cmd/Shift/middle-click) are
    // allowed through so right-click → "Open in new tab" preserves the dirty
    // draft in the original tab.
    cfgbNeedsConfirm($event, appType) {
      if ($event.metaKey || $event.ctrlKey || $event.shiftKey || $event.altKey || $event.button === 1) return false;
      if (this.currentSection !== 'advanced' || this.advancedTab !== 'group-builder') return false;
      if (appType === this.activeAppType) return false;
      if (typeof this.cfgbIsDirty !== 'function' || !this.cfgbIsDirty()) return false;
      const label = this.cfgbEditingId
        ? 'changes to "' + (this.cfgbName || '(unnamed)') + '"'
        : 'the unsaved cf-group draft';
      const targetHref = this.navHref('advanced', { appType, advancedTab: 'group-builder' });
      this.confirmModal = {
        show: true,
        title: 'Discard unsaved cf-group work?',
        message: 'Switch to ' + appType + ' and discard ' + label + '?\n\nThe saved copy on disk (if any) is unaffected.',
        confirmLabel: 'Switch to ' + appType,
        onConfirm: () => { location.hash = targetHref; },
        onCancel: () => {},
      };
      return true;
    },

    pushNav() {
      if (this._navSkipPush) return;
      const hash = this.buildNavHash();
      if (location.hash !== hash) history.pushState(null, '', hash);
    },

    restoreFromHash(hash) {
      if (!hash || hash === '#') return false;
      // Guard against the watch-loop: pushNav writes the hash, the browser
      // fires hashchange, this runs, watchers re-fire pushNav. Early-return
      // when the hash already matches the state we'd produce.
      if (hash === this.buildNavHash()) return true;
      const parts = hash.replace(/^#/, '').split('/');
      // 'quality-size' and 'naming' kept in this whitelist only so the
      // alias branch below can rewrite them to 'media-management' with
      // the matching sub-tab — they're no longer real top-level
      // sections in v3. Old bookmarks pointing to those hashes stay
      // working as a result.
      const validSections = ['profiles','custom-formats','media-management','quality-size','naming','maintenance','advanced','settings','about'];
      // 'prowlarr' kept as a legacy alias for old bookmarks — Prowlarr config
      // now lives inside the Instances section as its own v3-inst-card.
      const validSettings = ['instances','trash','profile-sync','notifications','display','security','advanced'];
      const settingsAlias = { prowlarr: 'instances' };
      // v3: 'trash-sync' kept as a legacy alias of 'trash-profiles' so old
      // bookmarks and hashes keep working after the sub-tab split. Mapped
      // during hash restore below.
      const validProfileTabs = ['trash-profiles','sync-rules','history','compare','trash-sync'];
      const validCustomFormatsTabs = ['browse','sync-rules'];
      const validMediaTabs = ['quality','naming'];
      const validMaintenanceTabs = ['backup','cleanup'];
      const validAdvancedTabs = ['builder','group-builder','scoring','import'];
      this._navSkipPush = true;
      try {
        if (parts[0] === 'settings') {
          this.currentSection = 'settings';
          if (parts[1]) {
            const aliased = settingsAlias[parts[1]] || parts[1];
            if (validSettings.includes(aliased)) this.settingsSection = aliased;
          }
        } else if (parts[0] === 'about') {
          this.currentSection = 'about';
        } else {
          const appType = parts[0];
          if (appType === 'radarr' || appType === 'sonarr') this.activeAppType = appType;
          if (parts[1] && validSections.includes(parts[1])) this.currentSection = parts[1];
          else return false;
          // Legacy aliases: old 'quality-size' and 'naming' top-level
          // sections folded into 'media-management' with sub-tabs. Old
          // bookmarks stay working.
          if (this.currentSection === 'quality-size') {
            this.currentSection = 'media-management';
            this.setMediaTab(this.activeAppType, 'quality');
          } else if (this.currentSection === 'naming') {
            this.currentSection = 'media-management';
            this.setMediaTab(this.activeAppType, 'naming');
          }
          if (parts[2]) {
            if (this.currentSection === 'profiles' && validProfileTabs.includes(parts[2])) {
              // Legacy alias: pre-v3 hashes used 'trash-sync' for what now
              // splits into 'trash-profiles' + 'sync-rules'. Old links land
              // on the profile-browser tab (the more discoverable side).
              const tab = parts[2] === 'trash-sync' ? 'trash-profiles' : parts[2];
              this.setProfileTab(this.activeAppType, tab);
            }
            else if (this.currentSection === 'custom-formats' && validCustomFormatsTabs.includes(parts[2])) {
              this.setCustomFormatsTab(this.activeAppType, parts[2]);
            }
            else if (this.currentSection === 'media-management' && validMediaTabs.includes(parts[2])) {
              this.setMediaTab(this.activeAppType, parts[2]);
            }
            else if (this.currentSection === 'maintenance' && validMaintenanceTabs.includes(parts[2])) {
              this.setMaintenanceTab(this.activeAppType, parts[2]);
            }
            else if (this.currentSection === 'advanced' && validAdvancedTabs.includes(parts[2])) this.advancedTab = parts[2];
          }
        }
        localStorage.setItem('clonarr_section', this.currentSection);
        localStorage.setItem('clonarr_appType', this.activeAppType);
        return true;
      } finally {
        this._navSkipPush = false;
      }
    },

    getProfileTab(appType) {
      const tab = this.profileTabs[appType];
      if (!tab) return 'trash-profiles';
      // Legacy: map 'trash-sync' to its post-split default ('trash-profiles')
      // so any stale in-memory state from a previous build doesn't 404.
      if (tab === 'trash-sync') return 'trash-profiles';
      return tab;
    },

    setProfileTab(appType, tab) {
      this.profileTabs = { ...this.profileTabs, [appType]: tab };
    },

    // Media Management sub-tabs — same per-app pattern as profileTabs.
    // Default sub-tab is 'quality' (the higher-traffic page).
    getMediaTab(appType) {
      return this.mediaTabs[appType] || 'quality';
    },
    setMediaTab(appType, tab) {
      this.mediaTabs = { ...this.mediaTabs, [appType]: tab };
    },

    // Custom Formats sub-tabs — same per-app pattern as profileTabs.
    // Default sub-tab is 'browse' (the existing TRaSH + custom CF
    // catalog) — most visitors come here to browse, not to manage
    // active sync state. Switching to 'sync-rules' triggers a fetch
    // of /api/cf-sync-rules so the per-CF state is fresh.
    getCustomFormatsTab(appType) {
      return this.customFormatsTabs[appType] || 'browse';
    },
    setCustomFormatsTab(appType, tab) {
      this.customFormatsTabs = { ...this.customFormatsTabs, [appType]: tab };
    },

    // Maintenance sub-tabs — Backup & Restore vs Cleanup. Default
    // 'backup' because that's the safer entry point (no destructive
    // actions visible until the user explicitly switches to Cleanup).
    getMaintenanceTab(appType) {
      return this.maintenanceTabs[appType] || 'backup';
    },
    setMaintenanceTab(appType, tab) {
      this.maintenanceTabs = { ...this.maintenanceTabs, [appType]: tab };
    },

    getCompareInstanceId(appType) {
      return this.compareInstanceIds[appType] || '';
    },
    setCompareInstanceId(appType, id) {
      this.compareInstanceIds = { ...this.compareInstanceIds, [appType]: id };
    },
    getCompareInstance(appType) {
      const id = this.compareInstanceIds[appType];
      return id ? (this.instances.find(i => i.id === id) || null) : null;
    },

    // Sprint 2 — app-banner helpers. Banner has two display modes:
    // app-scoped (full swatch + breadcrumb) and global (plain section
    // text, no swatch). Settings + About are global; everything else
    // is app-scoped.
    isGlobalSection() {
      return this.currentSection === 'settings' || this.currentSection === 'about';
    },

    // Returns the breadcrumb text shown in the banner middle.
    // App-scoped sections: "Profiles / TRaSH Sync", "Custom Formats",
    //                      "Advanced / Profile Builder", etc.
    // Global sections: "Settings", "About".
    // The "App / " prefix is intentionally omitted — the banner swatch
    // already encodes the active app, repeating it as text is redundant.
    currentBreadcrumb() {
      const sectionLabels = {
        'profiles': 'Profiles',
        'custom-formats': 'Custom Formats',
        'media-management': 'Media Management',
        'maintenance': 'Maintenance',
        'advanced': 'Advanced',
        'settings': 'Settings',
        'about': 'About',
      };
      const section = sectionLabels[this.currentSection] || '';
      if (this.isGlobalSection()) return section;

      if (this.currentSection === 'profiles') {
        const tab = this.getProfileTab(this.activeAppType);
        const tabLabel = { 'trash-profiles': 'TRaSH Profiles', 'sync-rules': 'Sync Rules', 'history': 'History', 'compare': 'Compare' }[tab] || '';
        return tabLabel ? `${section} / ${tabLabel}` : section;
      }
      if (this.currentSection === 'media-management') {
        const tab = this.getMediaTab(this.activeAppType);
        const namingLabel = this.activeAppType === 'sonarr' ? 'Episode Naming' : 'Movie Naming';
        const tabLabel = { 'quality': 'Quality Definitions', 'naming': namingLabel }[tab] || '';
        return tabLabel ? `${section} / ${tabLabel}` : section;
      }
      if (this.currentSection === 'maintenance') {
        const tab = this.getMaintenanceTab(this.activeAppType);
        const tabLabel = { 'backup': 'Backup & Restore', 'cleanup': 'Cleanup' }[tab] || '';
        return tabLabel ? `${section} / ${tabLabel}` : section;
      }
      if (this.currentSection === 'advanced') {
        const tab = this.advancedTab;
        const tabLabel = { 'builder': 'Profile Builder', 'scoring': 'Scoring Sandbox', 'group-builder': 'CF Group Builder' }[tab] || '';
        return tabLabel ? `${section} / ${tabLabel}` : section;
      }
      return section;
    },

    // Sprint 2 slice 3 — auto-sync banner chip helpers.
    //
    // Aggregates the auto-sync rules belonging to the currently-active app
    // type (Radarr/Sonarr) and exposes a compact "last activity" summary
    // for the banner chip. Hidden on global sections (Settings, About)
    // and when no enabled rules exist for the active app — auto-sync is
    // per-app-instance, so it has no meaning on those pages.
    //
    // Chip is a STATUS indicator showing "when did the last sync happen,
    // and did it succeed?" — for ANY sync trigger (auto-sync engine,
    // manual /api/sync/apply, Restore, etc.). LastSyncTime / LastSyncError
    // in the backend don't distinguish trigger, so neither does this chip.
    // Two states, no chip otherwise:
    //   ok      — green dot + "Synced to Radarr · 8m ago"
    //   failed  — red dot + "Sync to Radarr failed · 8m ago"
    //
    // App-aware naming ("Synced to Radarr" / "Synced to Sonarr") with the
    // app from activeAppType. The verb "synced TO" (vs "pulled FROM")
    // distinguishes this from the sidebar foot's "TRaSH pulled" label,
    // making it visually obvious that one is upstream fetch (TRaSH repo
    // → clonarr) and the other is push (clonarr → Arr instance).
    //
    // Chip is hidden when no rule has any sync history at all and on
    // global sections (Settings, About) where sync isn't a relevant
    // concept.

    // Rules scoped to the active Radarr/Sonarr app type, regardless of
    // their enabled flag — the chip reports last sync history, which
    // can come from a manual sync on a currently-disabled rule.
    _autoSyncRulesForActiveApp() {
      const ids = new Set(this.instancesOfType(this.activeAppType).map(i => i.id));
      return (this.autoSyncRules || []).filter(r => ids.has(r.instanceId));
    },

    autoSyncChipVisible() {
      if (this.isGlobalSection()) return false;
      const rules = this._autoSyncRulesForActiveApp();
      // Need something to report — either an error or a successful run.
      return rules.some(r => r.lastSyncError || r.lastSyncTime);
    },

    autoSyncChipState() {
      const rules = this._autoSyncRulesForActiveApp();
      return rules.some(r => r.lastSyncError) ? 'failed' : 'ok';
    },

    autoSyncChipLabel() {
      const rules = this._autoSyncRulesForActiveApp().filter(r => r.lastSyncTime);
      let latest = null;
      for (const r of rules) {
        const t = new Date(r.lastSyncTime).getTime();
        if (!latest || t > latest) latest = t;
      }
      const ago = latest ? this.timeAgo(new Date(latest).toISOString()) : 'never';
      const app = this.activeAppType === 'sonarr' ? 'Sonarr' : 'Radarr';
      return this.autoSyncChipState() === 'failed'
        ? `Sync to ${app} failed · ${ago}`
        : `Synced to ${app} · ${ago}`;
    },

    // Click → navigate to Profiles → History so the user can see what ran.
    // navHref already returns a leading-# hash; assign directly to
    // location.hash and the existing hashchange listener restores state.
    autoSyncChipClick() {
      window.location.hash = this.navHref('profiles', { profileTab: 'history' });
    },
  },
};
