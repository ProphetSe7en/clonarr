function clonarr() {
  return {
    currentTab: 'settings',  // LEGACY — being replaced by currentSection + activeAppType
    currentSection: 'profiles',  // NEW — feature-first: 'profiles', 'custom-formats', 'quality-size', 'naming', 'maintenance', 'advanced', 'settings', 'about'
    activeAppType: 'radarr',     // NEW — 'radarr' or 'sonarr', independent of section
    advancedTab: 'builder',      // NEW — sub-tab within Advanced: 'builder', 'scoring', 'group-builder'

    // CF Group Builder state — advancedTab === 'group-builder'
    // Mirrors the on-disk shape of TRaSH cf-groups/*.json so export is a straight serialize.
    cfgbName: '',
    cfgbDescription: '',
    cfgbTrashID: '',                         // MD5 of cfgbName — auto-computed on input (unless cfgbHashLocked)
    // When true, cfgbTrashID is frozen at cfgbOriginalTrashID and name
    // changes do NOT regenerate the hash. Flips on automatically when the
    // form is populated by an edit / TRaSH copy so the user can fix typos
    // or tweak names without invalidating downstream references. Flips off
    // manually via the lock button in the edit banner; fresh new groups
    // keep it off (nothing to lock to).
    cfgbHashLocked: false,
    cfgbDefault: false,
    cfgbCFs: [],                             // [{trashId, name, groupTrashId, groupName, isCustom}] — flattened from /api/trash/{app}/all-cfs
    cfgbGroups: [],                          // [{groupTrashId, name, count}] — actual TRaSH cf-groups for the dropdown
    cfgbGroupFilter: 'all',                  // 'all' | 'custom' | 'other' | a TRaSH groupTrashId
    cfgbHasCustom: false,                    // true if the list contains any user-custom CFs (toggles the Custom filter option)
    // Ungrouped counts come in two flavours so TRaSH can see both the raw
    // upstream scope ("CFs TRaSH hasn't grouped yet") and the residual
    // after his local work ("still to do after what I've placed locally").
    cfgbUngroupedTrashCount: 0,              // CFs with 0 TRaSH group memberships (ignores local groups)
    cfgbUngroupedRemainingCount: 0,          // CFs with 0 memberships at all — TRaSH and local combined
    cfgbCFFilter: '',
    cfgbSelectedCFs: {},                     // trashId → true (boolean map for easier Alpine binding)
    cfgbRequiredCFs: {},                     // trashId → true (per-CF required flag)
    cfgbDefaultCFs: {},                      // trashId → true (per-CF default override — rare; see Golden Rule UHD)
    cfgbProfiles: [],                        // [{trashId, name, group, groupName}] — all TRaSH profiles for current appType
    cfgbSelectedProfiles: {},                // trashId → true
    cfgbProfileGroupExpanded: {},            // groupName → bool — card collapse state (all expanded by default)
    cfgbCopyLabel: 'Copy JSON',              // swaps to "Copied!" briefly on click
    cfgbLoadError: '',                        // user-visible error when /api/trash/* fails
    cfgbPreviewOpen: false,                   // JSON-preview collapsible state
    // CF sort mode. 'alpha' is the TRaSH-spec default; 'manual' lets the
    // user hand-order selected CFs (up/down arrows) for cases where a
    // specific order matters (audio-format by quality, tier groupings, etc).
    // cfgbCFManualOrder holds trash_ids in the chosen order; entries for
    // deselected CFs are pruned lazily when the payload is built.
    cfgbCFSortMode: 'alpha',
    cfgbCFManualOrder: [],
    // Drag-and-drop reorder state for Selected CFs manual mode. Both hold
    // trash_ids; null when no drag is in flight.
    cfgbDragSrcTid: null,
    cfgbDragOverTid: null,
    // Saved cf-groups (persistent, stored in Clonarr). Loaded per app type on
    // tab entry. Edit loads one into the form; Save writes it back (POST for
    // new, PUT for existing). Storage is scoped per appType on disk so a
    // Radarr and Sonarr group with the same name never overwrite each other.
    cfgbSavedGroups: [],                     // CFGroup[] from GET /api/cf-groups/{app}
    cfgbTrashCFGroups: [],                   // TrashCFGroup[] from GET /api/trash/{app}/cf-groups — upstream groups the user can copy into local storage
    cfgbTrashListOpen: false,                // whether the "TRaSH cf-groups" section is expanded; default collapsed to keep the page short
    cfgbEditingId: '',                       // '' = new (POST), non-empty = editing existing (PUT)
    // trash_id captured at the moment the form was populated (either from a
    // local edit or a TRaSH copy). Used by cfgbSave to detect a rename that
    // would regenerate the MD5 so we can prompt the user to keep vs regenerate
    // the hash. '' means "fresh new group" — no prompt needed.
    cfgbOriginalTrashID: '',
    // Human-readable name of the TRaSH group the user copied from, for the
    // mode banner. '' when not copying from TRaSH.
    cfgbFromTrashName: '',
    cfgbSavingMsg: '',                       // transient save/delete feedback
    cfgbSavingOk: false,                     // whether cfgbSavingMsg is success (green) or error (red)
    cfgbDeleting: false,                     // guard against double-fire on Delete → Confirm (modal's onConfirm could run twice under fast clicks)
    profileTab: 'trash-sync',    // NEW — simple variable replacing per-app profileTabs: 'trash-sync', 'compare'
    instances: [],
    config: { trashRepo: { url: '', branch: '' }, pullInterval: '24h', prowlarr: { url: '', apiKey: '', enabled: false, radarrCategories: [], sonarrCategories: [] }, authentication: 'forms', authenticationRequired: 'disabled_for_local_addresses', trustedNetworks: '', trustedProxies: '', sessionTtlDays: 30 },
    // Security section state
    securityApiKey: '',
    securityApiKeyVisible: false,
    securityApiKeyCopied: false,
    securityRegenerating: false,
    securityRegenConfirm: false,
    pwChange: { current: '', next: '', confirm: '' },
    pwChangeSaving: false,
    pwChangeMsg: '',
    pwChangeOk: false,
    // Disable-auth confirmation modal
    disableAuthModalOpen: false,
    disableAuthPassword: '',
    disableAuthError: '',
    // Auth status (for logout button + no-auth banner)
    authStatus: { configured: false, authenticated: false, username: '', localBypass: false, authentication: '', authenticationRequired: '', trustedNetworksLocked: false, trustedProxiesLocked: false, trustedNetworksEffective: '', trustedProxiesEffective: '' },
    authStatusLoadError: false, // true after 2 fetchAuthStatus retries fail → Security save button warns
    noAuthBannerDismissed: false,
    securitySaveMsg: '',
    securitySaveOk: false,
    trashStatus: {},
    _nowTick: Date.now(),
    trashProfiles: { radarr: [], sonarr: [] },
    expandedInstances: {},
    expandedProfileGroups: {},
    instanceStatus: {},
    instanceVersion: {},
    pulling: false,
    profileTabs: {},  // per app-type profile tab: { radarr: 'trash-sync', sonarr: 'trash-sync' }
    compareInstanceIds: {},  // per app-type: { radarr: 'id', sonarr: 'id' }
    syncRulesExpanded: {},  // per app-type: { radarr: true, sonarr: false }
    syncRulesSort: { col: '', dir: 'asc' },
    historyExpanded: '',      // 'instanceId:arrProfileId' of expanded row in History tab
    historySort: { col: '', dir: 'asc' },
    historyEntries: [],       // loaded change history for the expanded profile
    historyLoading: false,
    historyDetailIdx: -1,     // which change entry is expanded (-1 = none)

    // Instance modal
    showInstanceModal: false,
    editingInstance: null,
    instanceForm: { name: '', type: 'radarr', url: '', apiKey: '' },
    modalTestResult: null,

    // Profile detail
    profileDetail: null,
    detailSections: { core: true },
    groupExpanded: {},
    cfDescExpanded: {},
    cfTooltip: {},
    selectedOptionalCFs: {},
    // Profile detail overrides (per-section active flags — when true, stored values are applied at sync time)
    pdGeneralActive: false,  // General card override (Language, Upgrades, Min/Cutoff scores)
    pdQualityActive: false,  // Quality card override (Cutoff quality)
    // Compare-tab filter: 'all' shows everything, 'diff' hides rows that match (default),
    // 'wrong'/'missing'/'extra'/'match' restricts to one status class.
    compareFilter: 'diff',
    // Per-card Quick Sync — lightweight modal with Sync/Dry Run/Cancel, no dropdowns.
    // Shape: { show, inst, cr, section, title, summary, running }
    compareQuickSync: { show: false },
    // Stored context from the last Compare dry-run so the banner's "Apply" button can re-run
    // the same scoped sync without reopening the quick-sync modal.
    compareLastDryRunContext: null,
    cfScoreOverrides: {}, // per-CF score overrides { trashId: score }
    cfScoreOverrideActive: false, // whether CF score editing is enabled
    qualityOverrides: {}, // legacy flat overrides { name: allowed(bool) } — kept for backwards compat
    qualityOverrideActive: false, // whether quality editing is enabled
    qualityOverrideCollapsed: false, // panel collapsed state (body hidden, header stays)
    extraCFsCollapsed: false, // Extra CFs panel collapsed state
    // Quality structure override (full structure replacing TRaSH items).
    // Format: [{ _id, name, allowed, items?: [string] }]. Empty when not in use.
    // When non-empty, this is sent as `qualityStructure` to backend and trumps qualityOverrides.
    qualityStructure: [],
    qualityStructureEditMode: false,
    qualityStructureExpanded: {},
    qualityStructureRenaming: null,
    qualityStructureDrag: { kind: null, src: null, srcGroup: null, srcMember: null, dropGap: null, dropMerge: null },
    _qsIdCounter: 0,
    _sbIdCounter: 0,
    extraCFs: {}, // { trashId: score } — extra CFs not in profile
    extraCFsActive: false,
    extraCFSearch: '',
    extraCFAllCFs: [], // flat list of all TRaSH CFs (for filtering)
    extraCFGroups: [], // { name, cfs[] } — TRaSH groups + ungrouped "Other"
    pdOverrides: {
      language: { enabled: true, value: 'Original' },
      upgradeAllowed: { enabled: true, value: true },
      minFormatScore: { enabled: true, value: 0 },
      minUpgradeFormatScore: { enabled: true, value: 1 },
      cutoffFormatScore: { enabled: true, value: 10000 },
      cutoffQuality: '',
    },
    // Instance profile compare
    instProfiles: {},           // instanceId → [ArrQualityProfile]
    instProfilesLoading: {},    // instanceId → bool
    instBackupLoading: {},      // instanceId → bool
    // Backup modal
    showBackupModal: false,
    backupInstance: null,       // instance being backed up
    backupMode: 'profiles',    // 'profiles' or 'cfs-only'
    backupProfiles: [],        // profiles from instance
    backupCFs: [],             // all CFs from instance
    backupSelectedProfiles: {},// profileId → bool
    backupSelectedCFs: {},     // cfId → bool (for score=0 CFs or CF-only mode)
    backupScoredCFs: {},       // cfId → bool (auto-included, score ≠ 0)
    backupLoading: false,
    backupStep: 'mode',        // 'mode', 'profiles', 'cfs', 'cfs-select'
    // Restore modal
    showRestoreModal: false,
    restoreInstance: null,
    restoreData: null,         // parsed backup JSON
    restorePreview: null,      // dry-run result
    restoreResult: null,       // apply result
    restoreLoading: false,
    restoreSelectedProfiles: {},// index → bool (selection from backup)
    restoreSelectedCFs: {},     // index → bool (selection from backup)
    instCompareProfile: {},     // instanceId → arrProfileId (selected)
    instCompareTrashId: {},     // instanceId → trashProfileId (selected)
    instCompareResult: {},      // instanceId → ProfileComparison
    instCompareLoading: {},     // instanceId → bool
    instCompareSelected: {},    // instanceId → { trashId: bool } for selective sync
    instCompareSettingsSelected: {}, // instanceId → { settingName: bool } for settings sync (checked = sync to TRaSH value)
    instCompareQualitySelected: {},  // instanceId → { qualityName: bool } for quality sync
    instRemoveSelected: {},     // instanceId → { arrCfId: bool } for removal
    showProfileInfo: false,

    // Sync history
    syncHistory: {},

    // CF browse (all CFs + groups per app type)
    cfBrowseData: {},  // { radarr: { cfs: [...], groups: [...] } }
    conflictsData: {}, // { radarr: { custom_formats: [[...], ...] }, sonarr: ... }

    // Import Custom CFs
    showImportCFModal: false,
    importCFAppType: '',
    importCFSource: 'instance',
    importCFInstanceId: '',
    importCFList: [],           // [{name, selected, exists}]
    importCFLoading: false,
    importCFCategory: 'Custom',
    importCFNewCategory: '',
    importCFJsonText: '',
    importCFJsonError: '',
    importCFResult: null,
    importCFImporting: false,

    // CF Editor (create/edit)
    showCFEditor: false,
    cfEditorMode: 'create',      // 'create' or 'edit'
    cfEditorForm: {
      id: '',
      name: '',
      appType: 'radarr',
      category: 'Custom',
      newCategory: '',
      includeInRename: false,
      specifications: [],        // [{name, implementation, negate, required, fields: [{name, value}]}]
      trashId: '',
      trashScores: [],           // [{context, score}]
      description: '',
    },
    cfEditorSaving: false,
    cfEditorResult: null,        // {error?, message}
    cfExportContent: '',         // TRaSH JSON export text for modal
    cfExportCopied: false,       // clipboard copy feedback
    cfEditorSchema: {},          // cached per app type: [{implementation, fields:[{name,label,type,selectOptions}]}]
    cfEditorSchemaLoading: false,
    cfEditorShowPreview: false,
    cfEditorSpecCounter: 0,     // unique ID counter for x-for keys

    // Quality sizes (cached per app type)
    qualitySizesPerApp: {},
    qsExpanded: {},
    selectedQSType: {},  // per app-type: index into quality sizes array
    qsInstanceId: {},    // per app-type: selected instance ID for comparison
    qsInstanceDefs: {},  // per app-type: current instance quality definitions
    qsOverrides: {},     // per app-type: { qualityName: { min, preferred, max } }
    qsSyncing: {},       // per app-type: boolean
    qsSyncResult: {},    // per app-type: { ok, message }
    qsAutoSync: {},      // per app-type: { enabled, type }
    confirmModal: { show: false, title: '', message: '', confirmLabel: '', cancelLabel: '', secondaryLabel: '', onConfirm: null, onCancel: null, onSecondary: null },
    inputModal: { show: false, title: '', message: '', value: '', placeholder: '', confirmLabel: '', onConfirm: null, onCancel: null },
    sandboxCopyModal: { show: false, title: '', text: '', copied: false },
    toasts: [], // { id, message, type: 'info'|'warning'|'error', timeout }

    // Naming schemes (cached per app type)
    namingData: {},  // { radarr: { folder: {...}, file: {...} }, sonarr: { season: {...}, series: {...}, episodes: {...} } }
    namingSelectedInstance: {},  // { radarr: 'instance-id', sonarr: 'instance-id' }
    namingInstanceData: {},      // { radarr: { renameMovies: true, ... }, sonarr: { ... } }
    namingApplyResult: {},       // { radarr: 'Success message', sonarr: '' }
    namingMediaServer: {},       // { radarr: 'standard', sonarr: 'standard' }
    namingPlexSingleEntry: {},   // { radarr: false, sonarr: false }

    // Import
    importedProfiles: { radarr: [], sonarr: [] },
    showImportModal: false, // false or app type string
    importMode: 'paste',
    importYaml: '',
    importFiles: [],       // array of { name, content } for multi-file
    importHasIncludes: false, // whether config uses include files
    importIncludeFiles: [], // array of { name, content } for include files
    importDragOver: false,
    importNameOverride: '',
    importResult: '',
    importError: false,
    importingProfile: false,

    // Export
    showExportModal: false,
    exportSource: null,
    exportTab: 'yaml', // 'yaml', 'json', 'trash'
    exportContent: '',
    exportCopied: false,
    exportGroupIncludes: [],
    showExportGroupIncludes: false,

    // Profile Builder
    profileBuilder: false,
    _resyncReturnSubTab: null,
    _resyncNavigating: false,
    pbSettingsOpen: true,
    pbInitTab: 'trash', // 'trash' | 'instance'
    pbAdvancedOpen: false,
    pbLoading: false,
    pbTemplateLoading: false,
    pbInstanceImportId: '',       // selected instance for "Import from Instance"
    pbInstanceImportProfiles: [], // profiles loaded from selected instance
    pbInstanceImportProfileId: '', // selected profile ID
    pbInstanceImportLoading: false,
    pbSaving: false,
    pbCategories: [],
    pbScoreSets: [],
    pbExpandedCats: {},
    pbFormatItemSearch: '',
    pbAddMoreOpen: false,
    pbQualityPresets: [],
    pbExpandedGroups: {},
    pbEditDescription: false,
    pb: {
      editId: null,
      name: '',
      appType: 'radarr',
      scoreSet: 'default',
      upgradeAllowed: true,
      cutoff: '',
      cutoffScore: 10000,
      minFormatScore: 0,
      minUpgradeFormatScore: 1,
      language: 'Original',
      qualityPreset: '',
      qualityPresetId: '',
      qualityAllowedNames: '',
      qualityItems: [],
      qualityEditorOpen: false,
      qualityEditGroups: false,
      baselineCFs: [],
      coreCFIds: [],
      selectedCFs: {},
      requiredCFs: {},
      defaultOnCFs: {},
      formatItemCFs: {},    // CFs that go into formatItems (required/mandatory)
      enabledGroups: {},    // { groupTrashId: true } — which CF groups are included
      cfStateOverrides: {}, // { trashId: 'required'|'optional' } — overrides TRaSH default per CF
      scoreOverrides: {},
      // Dev mode
      trashProfileId: '',
      trashProfileName: '',
      variantGoldenRule: '',
      goldenRuleDefault: '',
      variantMisc: '',
      trashScoreSet: '',
      trashDescription: '',
      groupNum: 0,
    },

    // Sync
    showChangelog: false,
    sandboxCFBrowser: { open: false, appType: '', categories: [], customCFs: [], selected: {}, scores: {}, expanded: {}, filter: '' },
    showSyncModal: false,
    syncMode: 'create',
    resyncTargetArrProfileId: null, // set by resyncProfile to ensure correct Arr profile is selected
    // Maintenance
    maintenanceInstanceId: '',

    // Cleanup
    cleanupInstanceId: '',
    cleanupKeepList: [],
    cleanupKeepInput: '',
    cleanupCFNames: [],        // all CF names from selected instance (for autocomplete)
    cleanupKeepSuggestions: [], // filtered suggestions
    cleanupKeepFocused: false,  // whether input is focused
    cleanupResult: null,
    cleanupScanning: false,
    cleanupApplying: false,

    syncForm: { instanceId: '', instanceName: '', appType: '', profileTrashId: '', importedProfileId: '', profileName: '', arrProfileId: '0', newProfileName: '', behavior: { addMode: 'add_missing', removeMode: 'remove_custom', resetMode: 'reset_to_zero' } },
    arrProfiles: [],
    instanceLanguages: {},  // instanceId → [{id, name}] cache
    syncPlan: null,
    syncResult: null,
    syncResultDetailsOpen: false,
    dryrunDetailsOpen: false,
    syncing: false,
    syncPreview: null,       // dry-run preview for update mode in sync modal
    syncPreviewLoading: false,

    // Auto-Sync
    autoSyncSettings: { enabled: false },
    notificationAgents: [],
    agentModal: {
      show: false,
      editId: null,
      name: '',
      type: 'discord',
      enabled: true,
      events: { onSyncSuccess: true, onSyncFailure: true, onCleanup: true, onRepoUpdate: false, onChangelog: false },
      config: { discordWebhook: '', discordWebhookUpdates: '', gotifyUrl: '', gotifyToken: '', gotifyPriorityCritical: true, gotifyPriorityWarning: true, gotifyPriorityInfo: false, gotifyCriticalValue: 8, gotifyWarningValue: 5, gotifyInfoValue: 3, pushoverUserKey: '', pushoverAppToken: '' },
      testing: false,
      testResults: [],
      testPassed: false,
      saving: false,
    },
    notifAgentStatus: {},
    settingsOpen: 'instances',  // legacy accordion (unused after sidebar redesign)
    settingsSection: 'instances',
    uiScale: localStorage.getItem('clonarr-ui-scale') || '1',
    autoSyncRules: [],
    autoSyncRuleForSync: null, // existing rule that matches current syncForm (if any)

    // Scoring Sandbox (per app-type state)
    sandbox: {
      radarr: { instanceId: '', profileKey: '', compareKey: '', editOpen: false, editScores: {}, editToggles: {}, editMinScore: null, editOriginal: null, inputMode: 'paste', pasteInput: '', bulkInput: '', searchQuery: '', selectedIndexers: [], indexers: [], searchResults: [], results: [], parsing: false, searching: false, searchAbort: null, instanceProfiles: [], showBulk: false, searchError: '', indexerDropdown: false, searchFilterText: '', searchFilterRes: '', sortCol: 'score', sortDir: 'desc', filterToSelected: false, dragSrc: null, dragOver: null },
      sonarr: { instanceId: '', profileKey: '', compareKey: '', editOpen: false, editScores: {}, editToggles: {}, editMinScore: null, editOriginal: null, inputMode: 'paste', pasteInput: '', bulkInput: '', searchQuery: '', selectedIndexers: [], indexers: [], searchResults: [], results: [], parsing: false, searching: false, searchAbort: null, instanceProfiles: [], showBulk: false, searchError: '', indexerDropdown: false, searchFilterText: '', searchFilterRes: '', sortCol: 'score', sortDir: 'desc', filterToSelected: false, dragSrc: null, dragOver: null },
    },
    prowlarrTestResult: null,
    prowlarrTesting: false,

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

    async init() {
      // Apply saved UI scale
      if (this.uiScale !== '1') document.documentElement.style.zoom = this.uiScale;
      // Reactive validation: any change to qualityStructure (rename, delete, merge, toggle)
      // re-validates pdOverrides.cutoffQuality and resets it to first allowed if it became invalid.
      this.$watch('qualityStructure', () => this.qsValidateCutoff());
      // Builder: auto-assign stable _id to every pb.qualityItems entry on any reassignment
      // (Apply template/preset/instance, group add/remove). Needed so shared qs-helpers can
      // track drag/drop/rename/expand by identity. pbEnsureQualityIds is idempotent — the
      // spread-reassignment inside only fires when something actually changed, so the watch
      // settles after one tick.
      this.$watch('pb.qualityItems', () => this.pbEnsureQualityIds());
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

    // CSRF token helper — reads the clonarr_csrf cookie set by the server
    // on the first GET. Used inside <form :value="csrfToken()"> bindings
    // for the logout form. AJAX fetches get the header attached
    // automatically by the window.fetch wrapper at the top of this file.
    csrfToken() {
      const m = document.cookie.match(/(?:^|; )clonarr_csrf=([^;]+)/);
      return m ? m[1] : '';
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
        await navigator.clipboard.writeText(this.securityApiKey);
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

    // Mirror of server-side validatePassword (internal/auth/auth.go): ≥10
    // chars and ≥2 of {upper, lower, digit, symbol}. Returns '' on valid,
    // error message on failure. Enforced client-side for fast UX; server
    // re-validates unconditionally — this is not a trust boundary.
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
        // Opt out of the central 401→/login redirect for confirmedNone:
        // here a 401 means "confirm_password incorrect", not "session
        // expired", and must surface to the modal handler below. Session
        // expiry on the non-confirmedNone path is handled by the wrapper.
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

    async loadInstances() {
      try {
        const r = await fetch('/api/instances');
        if (!r.ok) return;
        this.instances = await r.json();
      } catch (e) { console.error('loadInstances:', e); }
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

    async loadQualitySizes(appType) {
      try {
        const r = await fetch(`/api/trash/${appType}/quality-sizes`);
        if (r.ok) {
          const data = await r.json();
          // Sort: movie first, then sqp variants, then anime
          const order = { movie: 0, series: 0, 'sqp-streaming': 1, 'sqp-uhd': 2, anime: 3 };
          data.sort((a, b) => (order[a.type] ?? 99) - (order[b.type] ?? 99));
          this.qualitySizesPerApp = { ...this.qualitySizesPerApp, [appType]: data };
        }
      } catch (e) { /* ignore */ }
    },

    getQualitySizes(appType) {
      return this.qualitySizesPerApp[appType] || [];
    },

    async loadNaming(appType) {
      try {
        const r = await fetch(`/api/trash/${appType}/naming`);
        if (r.ok) {
          const data = await r.json();
          this.namingData = { ...this.namingData, [appType]: data };
        }
      } catch (e) { /* ignore */ }
    },

    getNaming(appType) {
      return this.namingData[appType] || null;
    },

    getNamingSections(appType, mediaServer, plexSingleEntry) {
      const n = this.getNaming(appType);
      if (!n) return [];
      const ms = mediaServer || 'standard';

      // Descriptions sourced verbatim from TRaSH-Guides where available.
      // Schemes without TRaSH-authored descriptions have no desc field.
      const schemeDesc = {
        'standard': { label: 'Standard', recommended: true },
        'default': { label: 'Default', recommended: true },
        'original': { label: 'Original Title', desc: 'Another option is to use {Original Title} instead of the recommended naming scheme above. {Original Title} uses the title of the release, which includes all the information from the release itself. The benefit of this naming scheme is that it prevents download loops that can happen during import when there\'s a mismatch between the release title and the file contents (for example, if the release title says DTS-ES but the contents are actually DTS). The downside is that you have less control over how the files are named.' },
        'p2p-scene': { label: 'P2P / Scene', desc: 'Use P2P/Scene naming if you don\'t like spaces and brackets in the filename. It\'s the closest to the P2P/scene naming scheme, except it uses the exact audio and HDR formats from the media file, where the original release or filename might be unclear.' },
        'plex-imdb': { label: 'Plex (IMDb)', recommended: true },
        'plex-tmdb': { label: 'Plex (TMDb)' },
        'plex-tvdb': { label: 'Plex (TVDb)' },
        'plex-anime-imdb': { label: 'Plex Anime (IMDb)' },
        'plex-anime-tmdb': { label: 'Plex Anime (TMDb)' },
        'emby-imdb': { label: 'Emby (IMDb)', recommended: true },
        'emby-tmdb': { label: 'Emby (TMDb)' },
        'emby-tvdb': { label: 'Emby (TVDb)' },
        'emby-anime-imdb': { label: 'Emby Anime (IMDb)' },
        'emby-anime-tmdb': { label: 'Emby Anime (TMDb)' },
        'jellyfin-imdb': { label: 'Jellyfin (IMDb)', recommended: true },
        'jellyfin-tmdb': { label: 'Jellyfin (TMDb)' },
        'jellyfin-tvdb': { label: 'Jellyfin (TVDb)' },
        'jellyfin-anime-imdb': { label: 'Jellyfin Anime (IMDb)' },
        'jellyfin-anime-tmdb': { label: 'Jellyfin Anime (TMDb)' },
      };

      // Media server key filters
      const msFilters = {
        standard: k => !k.includes('-'),  // standard, default, original, p2p-scene have no media server prefix
        plex: k => k.startsWith('plex-'),
        emby: k => k.startsWith('emby-'),
        jellyfin: k => k.startsWith('jellyfin-'),
      };
      const standardKeys = new Set(['standard', 'default', 'original', 'p2p-scene']);
      const filterFn = ms === 'standard'
        ? k => standardKeys.has(k)
        : (msFilters[ms] || (() => true));

      const applyEditionToggle = (pattern, example) => {
        if (!plexSingleEntry || ms !== 'plex') return { pattern, example };
        return {
          pattern: pattern.replace(/\{edition-\{Edition Tags\}\}/g, '{Edition Tags}'),
          example: example ? example.replace(/\{edition-([^}]+)\}/g, '$1') : example,
        };
      };

      const radarrExamples = {
        folder: {
          'default': 'The Movie Title (2010)',
          'plex-imdb': 'The Movie Title (2010) {imdb-tt1520211}',
          'plex-tmdb': 'The Movie Title (2010) {tmdb-345691}',
          'emby-imdb': 'The Movie Title (2010) [imdb-tt1520211]',
          'emby-tmdb': 'The Movie Title (2010) [tmdb-345691]',
          'jellyfin-imdb': 'The Movie Title (2010) [imdbid-tt1520211]',
          'jellyfin-tmdb': 'The Movie Title (2010) [tmdbid-345691]',
        },
        file: {
          'standard': 'The Movie Title (2010) {edition-Ultimate Extended Edition} [IMAX HYBRID][Bluray-1080p Proper][3D][DV HDR10][DTS 5.1][x264]-RlsGrp',
          'original': 'The.Movie.Title.2010.REMASTERED.1080p.BluRay.x264-RlsGrp',
          'p2p-scene': 'The.Movie.Title.2010.Ultimate.Extended.Edition.3D.Hybrid.Remux-2160p.TrueHD.Atmos.7.1.DV.HDR10Plus.HEVC-RlsGrp',
          'plex-imdb': 'The Movie Title (2010) {imdb-tt1520211} - {edition-Ultimate Extended Edition} [IMAX HYBRID][Bluray-1080p Proper][3D][DV HDR10][DTS 5.1][x264]-RlsGrp',
          'plex-tmdb': 'The Movie Title (2010) {tmdb-345691} - {edition-Ultimate Extended Edition} [IMAX HYBRID][Bluray-1080p Proper][3D][DV HDR10][DTS 5.1][x264]-RlsGrp',
          'plex-anime-imdb': 'The Movie Title (2010) {imdb-tt1520211} - {edition-Ultimate Extended Edition} [Surround Sound x264][Bluray-1080p Proper][3D][DTS 5.1][DE][10bit][AVC]-RlsGrp',
          'plex-anime-tmdb': 'The Movie Title (2010) {tmdb-345691} - {edition-Ultimate Extended Edition} [Surround Sound x264][Bluray-1080p Proper][3D][DTS 5.1][DE][10bit][AVC]-RlsGrp',
          'emby-imdb': 'The Movie Title (2010) [imdb-tt0066921] - {edition-Ultimate Extended Edition} [IMAX HYBRID][Bluray-1080p Proper][3D][DV HDR10][DTS 5.1][x264]-RlsGrp',
          'emby-tmdb': 'The Movie Title (2010) [tmdb-345691] - {edition-Ultimate Extended Edition} [IMAX HYBRID][Bluray-1080p Proper][3D][DV HDR10][DTS 5.1][x264]-RlsGrp',
          'emby-anime-imdb': 'The Movie Title (2010) [imdb-tt0066921] - {edition-Ultimate Extended Edition} [Surround Sound x264][Bluray-1080p Proper][3D][DTS 5.1][DE][10bit][AVC]-RlsGrp',
          'emby-anime-tmdb': 'The Movie Title (2010) [tmdb-345691] - {edition-Ultimate Extended Edition} [Surround Sound x264][Bluray-1080p Proper][3D][DTS 5.1][DE][10bit][AVC]-RlsGrp',
          'jellyfin-imdb': 'The Movie Title (2010) [imdbid-tt0106145] - {edition-Ultimate Extended Edition} [IMAX HYBRID][Bluray-1080p Proper][3D][DV HDR10][DTS 5.1][x264]-RlsGrp',
          'jellyfin-tmdb': 'The Movie Title (2010) [tmdbid-345691] - {edition-Ultimate Extended Edition} [IMAX HYBRID][Bluray-1080p Proper][3D][DV HDR10][DTS 5.1][x264]-RlsGrp',
          'jellyfin-anime-imdb': 'The Movie Title (2010) [imdbid-tt0106145] - {edition-Ultimate Extended Edition} [Surround Sound x264][Bluray-1080p Proper][3D][DTS 5.1][DE][10bit][AVC]-RlsGrp',
          'jellyfin-anime-tmdb': 'The Movie Title (2010) [tmdbid-345691] - {edition-Ultimate Extended Edition} [Surround Sound x264][Bluray-1080p Proper][3D][DTS 5.1][DE][10bit][AVC]-RlsGrp',
        }
      };

      const sonarrExamples = {
        series: {
          'default': 'The Series Title! (2010)',
          'plex-imdb': 'The Series Title! (2010) {imdb-tt1520211}',
          'plex-tvdb': 'The Series Title! (2010) {tvdb-1520211}',
          'emby-imdb': 'The Series Title! (2010) [imdb-tt1520211]',
          'emby-tvdb': 'The Series Title! (2010) [tvdb-1520211]',
          'jellyfin-imdb': 'The Series Title! (2010) [imdbid-tt1520211]',
          'jellyfin-tvdb': 'The Series Title! (2010) [tvdbid-1520211]',
        },
        episodes: {
          standard: { 'default': 'The Series Title! (2010) - S01E01 - Episode Title 1 [AMZN WEBDL-1080p Proper][DV HDR10][DTS 5.1][x264]-RlsGrp' },
          daily: { 'default': 'The Series Title! (2010) - 2013-10-30 - Episode Title 1 [AMZN WEBDL-1080p Proper][DV HDR10][DTS 5.1][x264]-RlsGrp' },
          anime: { 'default': 'The Series Title! (2010) - S01E01 - 001 - Episode Title 1 [iNTERNAL HDTV-720p v2][HDR10][10bit][x264][DTS 5.1][JA]-RlsGrp' },
        }
      };

      // Enforce consistent ordering
      const keyOrder = ['standard', 'default', 'plex-imdb', 'plex-tmdb', 'plex-anime-imdb', 'plex-anime-tmdb', 'plex-tvdb',
        'emby-imdb', 'emby-tmdb', 'emby-anime-imdb', 'emby-anime-tmdb', 'emby-tvdb',
        'jellyfin-imdb', 'jellyfin-tmdb', 'jellyfin-anime-imdb', 'jellyfin-anime-tmdb', 'jellyfin-tvdb',
        'original', 'p2p-scene'];

      const makeSchemes = (map, sectionKey, examplesMap) => {
        const entries = Object.entries(map || {}).filter(([key]) => filterFn(key));
        entries.sort((a, b) => {
          const ai = keyOrder.indexOf(a[0]), bi = keyOrder.indexOf(b[0]);
          return (ai === -1 ? 999 : ai) - (bi === -1 ? 999 : bi);
        });
        return entries.map(([key, pattern]) => {
          const meta = schemeDesc[key] || { label: key.replace(/-/g, ' ').replace(/\b\w/g, c => c.toUpperCase()) };
          const ed = applyEditionToggle(pattern, examplesMap?.[key] || '');
          return {
            key,
            label: meta.label || key,
            recommended: meta.recommended || false,
            description: meta.desc || '',
            pattern: ed.pattern,
            example: ed.example,
          };
        });
      };

      const sections = [];

      // Section descriptions sourced verbatim from TRaSH-Guides where available.
      // Sonarr: docs/Sonarr/Sonarr-recommended-naming-scheme.md
      // Radarr: docs/Radarr/Radarr-recommended-naming-scheme.md (+ includes/radarr/radarr-folder-name-after-year-info.md)
      const radarrFileDesc = {
        standard: '',
        plex: 'This naming scheme is designed to work with the New Plex Agent.',
        emby: 'Source: Emby Wiki/Docs',
        jellyfin: 'Source: Jellyfin Wiki/Docs',
      };
      const radarrFolderDesc = {
        standard: 'The minimum needed and recommended format',
        plex: 'Keep in mind adding anything additional after the release year could give issues during a fresh import into Radarr, but it can help for movies that have the same release name and year',
        emby: 'Keep in mind adding anything additional after the release year could give issues during a fresh import into Radarr, but it can help for movies that have the same release name and year',
        jellyfin: 'Keep in mind adding anything additional after the release year could give issues during a fresh import into Radarr, but it can help for movies that have the same release name and year',
      };
      const sonarrSeriesDesc = {
        standard: '',
        plex: 'This naming scheme is made to be used with the New Plex TV Series Scanner.',
        emby: 'Source: Emby Wiki/Docs',
        jellyfin: 'Source: Jellyfin Wiki/Docs — Jellyfin doesn\'t support IMDb IDs for shows.',
      };

      if (appType === 'radarr') {
        // File format first, folder second
        const fileSchemes = makeSchemes(n.file, 'file', radarrExamples.file);
        if (fileSchemes.length > 0) {
          sections.push({
            key: 'file',
            label: 'Standard Movie Format',
            description: radarrFileDesc[ms] || '',
            schemes: fileSchemes,
            showEditionToggle: ms === 'plex',
          });
        }
        const folderSchemes = makeSchemes(n.folder, 'folder', radarrExamples.folder);
        if (folderSchemes.length > 0) {
          sections.push({
            key: 'folder',
            label: 'Movie Folder Format',
            description: radarrFolderDesc[ms] || '',
            schemes: folderSchemes,
          });
        }
      } else {
        // Episodes first (most important)
        for (const [epType, schemes] of Object.entries(n.episodes || {})) {
          const epLabel = epType.charAt(0).toUpperCase() + epType.slice(1);
          const epSchemes = makeSchemes(schemes, epType, sonarrExamples.episodes?.[epType]);
          if (epSchemes.length > 0) {
            sections.push({
              key: 'episodes-' + epType,
              label: 'Episode Format — ' + epLabel,
              description: '',
              schemes: epSchemes,
            });
          }
        }
        const seriesSchemes = makeSchemes(n.series, 'series', sonarrExamples.series);
        if (seriesSchemes.length > 0) sections.push({
          key: 'series',
          label: 'Series Folder Format',
          description: sonarrSeriesDesc[ms] || '',
          schemes: seriesSchemes,
        });
        if (n.season && ms === 'standard') {
          sections.push({
            key: 'season',
            label: 'Season Folder Format',
            description: 'For this, there\'s only one real option to use in our opinion.',
            schemes: makeSchemes(n.season, 'season', { 'default': 'Season 01' }),
          });
        }
      }

      return sections;
    },

    getInstanceName(appType, instId) {
      const inst = this.instances.find(i => i.id === instId);
      return inst ? inst.name : '';
    },

    async loadInstanceNaming(appType) {
      const instId = this.namingSelectedInstance[appType];
      if (!instId) {
        this.namingInstanceData = { ...this.namingInstanceData, [appType]: null };
        return;
      }
      try {
        const r = await fetch(`/api/instances/${instId}/naming`);
        if (r.ok) {
          const data = await r.json();
          this.namingInstanceData = { ...this.namingInstanceData, [appType]: data };
        }
      } catch (e) { console.error('Failed to load instance naming:', e); }
    },

    async applyNamingScheme(appType, sectionKey, scheme) {
      const instId = this.namingSelectedInstance[appType];
      if (!instId) return;
      const instName = this.getInstanceName(appType, instId);
      const body = {};
      if (sectionKey === 'folder' || sectionKey === 'series' || sectionKey === 'season') {
        body[sectionKey] = scheme.pattern;
        if (sectionKey === 'series') body.series = scheme.pattern;
        if (sectionKey === 'season') body.season = scheme.pattern;
        if (sectionKey === 'folder') body.folder = scheme.pattern;
      } else {
        // file/episodes section
        body.file = scheme.pattern;
      }
      try {
        const r = await fetch(`/api/instances/${instId}/naming`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body)
        });
        if (r.ok) {
          this.namingApplyResult = { ...this.namingApplyResult, [appType]: `Applied "${scheme.label}" ${sectionKey} naming to ${instName}` };
          this.loadInstanceNaming(appType);
          setTimeout(() => { this.namingApplyResult = { ...this.namingApplyResult, [appType]: '' }; }, 5000);
        } else {
          const err = await r.json().catch(() => ({}));
          this.namingApplyResult = { ...this.namingApplyResult, [appType]: `Failed: ${err.error || r.statusText}` };
        }
      } catch (e) {
        this.namingApplyResult = { ...this.namingApplyResult, [appType]: `Error: ${e.message}` };
      }
    },

    async loadCFBrowse(appType) {
      try {
        const [cfsRes, groupsRes, customRes] = await Promise.all([
          fetch(`/api/trash/${appType}/cfs`),
          fetch(`/api/trash/${appType}/cf-groups`),
          fetch(`/api/custom-cfs/${appType}`)
        ]);
        if (!cfsRes.ok || !groupsRes.ok) return;
        const cfs = await cfsRes.json();
        const groups = await groupsRes.json();
        const customCFs = customRes.ok ? await customRes.json() : [];
        this.cfBrowseData = { ...this.cfBrowseData, [appType]: { cfs, groups, customCFs } };
      } catch (e) { /* not yet cloned */ }
    },

    async loadConflicts(appType) {
      try {
        const res = await fetch(`/api/trash/${appType}/conflicts`);
        if (res.ok) this.conflictsData = { ...this.conflictsData, [appType]: await res.json() };
      } catch (e) { /* not available */ }
    },

    // --- Import ---
    async loadImportedProfiles(appType) {
      try {
        const r = await fetch(`/api/import/${appType}/profiles`);
        if (r.ok) {
          const data = await r.json();
          this.importedProfiles = { ...this.importedProfiles, [appType]: data };
        }
      } catch (e) { /* ignore */ }
    },

    handleImportFiles(fileList) {
      if (!fileList || fileList.length === 0) return;
      for (const file of fileList) {
        if (!file.name.match(/\.(?:ya?ml|json)$/i)) continue;
        const reader = new FileReader();
        const name = file.name;
        reader.onload = (e) => {
          // Avoid duplicates
          if (!this.importFiles.find(f => f.name === name)) {
            this.importFiles.push({ name, content: e.target.result });
          }
        };
        reader.readAsText(file);
      }
    },

    handleImportIncludeFiles(fileList) {
      if (!fileList || fileList.length === 0) return;
      for (const file of fileList) {
        if (!file.name.match(/\.ya?ml$/i)) continue;
        const reader = new FileReader();
        const name = file.name;
        reader.onload = (e) => {
          if (!this.importIncludeFiles.find(f => f.name === name)) {
            this.importIncludeFiles.push({ name, content: e.target.result });
          }
        };
        reader.readAsText(file);
      }
    },

    async submitImport() {
      this.importingProfile = true;
      this.importResult = '';
      this.importError = false;

      // Collect YAML contents to import
      const yamls = [];
      if (this.importMode === 'paste') {
        yamls.push({ name: 'pasted', content: this.importYaml });
      } else {
        for (const f of this.importFiles) {
          yamls.push({ name: f.name, content: f.content });
        }
      }

      // If includes are provided, send them alongside for backend merge
      const includeFiles = this.importIncludeFiles.length > 0
        ? this.importIncludeFiles.map(f => ({ name: f.name, content: f.content }))
        : null;

      let totalImported = 0;
      let totalSkipped = 0;
      const errors = [];
      const renamed = [];

      for (const y of yamls) {
        try {
          const r = await fetch('/api/import/profile', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ yaml: y.content, name: yamls.length === 1 ? this.importNameOverride.trim() : '', appType: this.showImportModal, includes: includeFiles })
          });
          const data = await r.json();
          if (!r.ok) {
            errors.push(`${y.name}: ${data.error || 'failed'}`);
          } else {
            totalImported += data.imported || 0;
            totalSkipped += data.skipped || 0;
            // Check for renamed profiles (name collision)
            if (data.profiles) {
              for (const p of data.profiles) {
                const origName = yamls.length === 1 && this.importNameOverride.trim() ? this.importNameOverride.trim() : null;
                if (origName && p.name !== origName) {
                  renamed.push(`"${origName}" → "${p.name}"`);
                } else if (p.name && p.name.match(/\(\d+\)$/)) {
                  renamed.push(`Saved as "${p.name}" (name already existed)`);
                }
                // Golden Rule variant: ask per profile
                if (!p.trashProfileId && p.formatItems && !p.variantGoldenRule) {
                  const grRadarr = ['dc98083864ea246d05a42df0d05f81cc', '839bea857ed2c0a8e084f3cbdbd65ecb'];
                  const grSonarr = ['47435ece6b99a0b477caf360e79ba0bb', '9b64dff695c2115facf1b6ea59c9bd07'];
                  const grIds = p.appType === 'sonarr' ? grSonarr : grRadarr;
                  if (grIds.some(id => id in p.formatItems)) {
                    const variant = await new Promise(resolve => {
                      this.confirmModal = {
                        show: true, html: true,
                        title: 'Golden Rule — HD or UHD?',
                        message: '<strong>"' + p.name + '"</strong> (' + p.appType.charAt(0).toUpperCase() + p.appType.slice(1) + ') contains Golden Rule CFs but no TRaSH profile reference.<br><br>• <strong>HD</strong> — for 720p/1080p profiles<br>• <strong>UHD</strong> — for 2160p/4K profiles',
                        confirmLabel: 'UHD',
                        cancelLabel: 'HD',
                        onConfirm: () => resolve('UHD'),
                        onCancel: () => resolve('HD')
                      };
                    });
                    await fetch('/api/import/profiles/' + p.id, {
                      method: 'PUT',
                      headers: { 'Content-Type': 'application/json' },
                      body: JSON.stringify({ variantGoldenRule: variant })
                    });
                  }
                }
              }
            }
          }
        } catch (e) {
          errors.push(`${y.name}: ${e.message}`);
        }
      }

      this.loadImportedProfiles('radarr');
      this.loadImportedProfiles('sonarr');

      // Close modal and show result as toast
      this.showImportModal = false;
      this.importYaml = '';
      this.importFiles = [];
      this.importIncludeFiles = [];
      this.importHasIncludes = false;
      this.importNameOverride = '';
      this.importResult = '';

      if (errors.length > 0) {
        let msg = errors.join('\n');
        if (totalImported > 0) msg = `Imported ${totalImported} profile(s), but errors occurred:\n` + msg;
        this.showToast(msg, 'error', 8000);
      } else {
        let msg = `Imported ${totalImported} profile(s)`;
        if (totalSkipped > 0) msg += `, ${totalSkipped} skipped (already exist)`;
        if (renamed.length > 0) msg += '\n' + renamed.join('\n');
        this.showToast(msg, 'info', 8000);
      }

      this.importingProfile = false;
    },

    async deleteImportedProfile(id, appType) {
      const confirmed = await new Promise(resolve => {
        this.confirmModal = { show: true, title: 'Delete Profile', message: 'Delete this imported profile?', confirmLabel: 'Delete', onConfirm: () => resolve(true), onCancel: () => resolve(false) };
      });
      if (!confirmed) return;
      try {
        await fetch(`/api/import/profiles/${id}`, { method: 'DELETE' });
        this.loadImportedProfiles(appType);
      } catch (e) { /* ignore */ }
    },

    async deleteAllImportedProfiles(appType) {
      const profiles = this.importedProfiles[appType] || [];
      for (const p of profiles) {
        try {
          await fetch(`/api/import/profiles/${p.id}`, { method: 'DELETE' });
        } catch (e) { /* ignore */ }
      }
      this.loadImportedProfiles(appType);
    },

    // --- Profile Builder ---

    async openProfileBuilder(appType, existing = null) {
      this.pb = {
        editId: existing?.id || null,
        name: existing?.name || '',
        appType: appType,
        scoreSet: existing?.scoreSet || (existing ? 'default' : (this._pbLoadDefaults().scoreSet || 'default')),
        upgradeAllowed: existing?.upgradeAllowed ?? true,
        cutoff: existing?.cutoff || '',
        cutoffScore: existing?.cutoffScore ?? 10000,
        minFormatScore: existing?.minFormatScore ?? 0,
        minUpgradeFormatScore: existing?.minUpgradeFormatScore ?? 1,
        language: existing?.language || 'Original',
        qualityPreset: existing?.qualityPresetId || (existing ? '' : (this._pbLoadDefaults().qualityPresetId || '')),
        qualityPresetId: existing?.qualityPresetId || (existing ? '' : (this._pbLoadDefaults().qualityPresetId || '')),
        qualityAllowedNames: '',
        qualityItems: existing?.qualities || [],
        qualityEditorOpen: false,
        qualityEditGroups: false,
        baselineCFs: existing?.baselineCFs || [],
        coreCFIds: existing?.coreCFIds || [],
        templateId: '',
        selectedCFs: {},
        requiredCFs: {},
        defaultOnCFs: {},
        formatItemCFs: {},
        enabledGroups: {},
        cfStateOverrides: {},
        scoreOverrides: {},
        trashProfileId: existing?.trashProfileId || '',
        trashProfileName: '',
        variantGoldenRule: existing?.variantGoldenRule || (existing ? '' : (this._pbLoadDefaults().variantGoldenRule || '')),
        goldenRuleDefault: existing?.goldenRuleDefault || '',
        variantMisc: existing?.variantMisc || (existing ? '' : (this._pbLoadDefaults().variantMisc || '')),
        trashScoreSet: existing?.trashScoreSet || (existing ? '' : (this._pbLoadDefaults().trashScoreSet || '')),
        trashDescription: existing?.trashDescription || '',
        groupNum: existing?.groupNum || 0,
      };
      // Populate from existing profile
      if (existing?.formatItems) {
        for (const [tid, score] of Object.entries(existing.formatItems)) {
          this.pb.selectedCFs[tid] = true;
          this.pb.scoreOverrides[tid] = score;
        }
      }
      if (existing?.requiredCFs) {
        for (const tid of existing.requiredCFs) {
          this.pb.requiredCFs[tid] = true;
        }
      }
      if (existing?.defaultOnCFs) {
        for (const tid of existing.defaultOnCFs) {
          this.pb.defaultOnCFs[tid] = true;
        }
      }
      // Restore new group-based state
      if (existing?.formatItemCFs && Object.keys(existing.formatItemCFs).length > 0) {
        this.pb.formatItemCFs = { ...existing.formatItemCFs };
      } else if (existing?.source === 'import' && existing?.formatItems) {
        // Fallback for profiles imported before v2.1.1: a TRaSH profile's
        // formatItems are, by TRaSH convention, the required CFs. Older
        // imports don't have FormatItemCFs populated on disk — derive it
        // here so the Builder shows the Required section correctly when
        // opening those profiles. New imports get this from the backend.
        for (const tid of Object.keys(existing.formatItems)) {
          this.pb.formatItemCFs[tid] = true;
        }
      }
      if (existing?.enabledGroups) {
        this.pb.enabledGroups = { ...existing.enabledGroups };
      }
      if (existing?.cfStateOverrides) {
        this.pb.cfStateOverrides = { ...existing.cfStateOverrides };
      }
      this.pbExpandedCats = {};
      this.pbAddMoreOpen = false;
      this.pbSettingsOpen = !existing; // collapse settings when editing
      this.pbInstanceImportId = '';
      this.pbInstanceImportProfiles = [];
      this.pbInstanceImportProfileId = '';
      this.profileBuilder = true;
      await this.loadCFPicker(appType);
      // Apply remembered Golden Rule variant (localStorage defaults)
      if (!existing && this.pb.variantGoldenRule) {
        this.pbApplyGoldenRule();
      }
      // After presets loaded, restore quality preset display
      if (existing?.qualityPresetId) {
        // Use saved preset ID
        const match = this.pbQualityPresets.find(p => p.id === existing.qualityPresetId);
        if (match) {
          this.pb.qualityAllowedNames = (match.allowed || []).join(', ');
        }
      } else if (existing?.cutoff) {
        // Fallback: try to match by cutoff
        const match = this.pbQualityPresets.find(p => p.cutoff === existing.cutoff);
        if (match) {
          this.pb.qualityPresetId = match.id;
          this.pb.qualityPreset = match.id;
          this.pb.qualityAllowedNames = (match.allowed || []).join(', ');
        }
      }
      // Update quality display from items if available
      if (existing?.qualities?.length) {
        this.pb.qualityAllowedNames = existing.qualities.filter(q => q.allowed).map(q => q.name).join(', ');
      }
    },

    editCustomProfile(appType, profile) {
      this.openProfileBuilder(appType, profile);
    },

    cancelProfileBuilder() {
      this.profileBuilder = false;
      // Navigate back to Advanced tab (builder lives there now)
      if (this.currentSection !== 'advanced') {
        this.currentSection = 'advanced';
      }
      this._resyncReturnSubTab = null;
    },

    async loadCFPicker(appType) {
      this.pbLoading = true;
      try {
        const [cfRes, qpRes] = await Promise.all([
          fetch(`/api/trash/${appType}/all-cfs`),
          fetch(`/api/trash/${appType}/quality-presets`),
        ]);
        if (cfRes.ok) {
          const data = await cfRes.json();
          this.pbCategories = data.categories || [];
          this.pbScoreSets = data.scoreSets || [];
        }
        if (qpRes.ok) {
          this.pbQualityPresets = await qpRes.json() || [];
        }
      } catch (e) { /* ignore */ }
      this.pbLoading = false;
    },

    pbQualityPresetGroups() {
      const groupOrder = { 'Standard': 1, 'SQP': 2, 'French': 3, 'German': 4, 'Anime': 5 };
      const groups = new Set();
      for (const qp of this.pbQualityPresets) {
        const name = qp.name;
        if (name.startsWith('[SQP]')) groups.add('SQP');
        else if (name.startsWith('[French')) groups.add('French');
        else if (name.startsWith('[German')) groups.add('German');
        else if (name.startsWith('[Anime')) groups.add('Anime');
        else groups.add('Standard');
      }
      return [...groups].sort((a, b) => (groupOrder[a] ?? 99) - (groupOrder[b] ?? 99));
    },

    pbQualityPresetsByGroup(groupName) {
      return this.pbQualityPresets.filter(qp => {
        const name = qp.name;
        if (groupName === 'SQP') return name.startsWith('[SQP]');
        if (groupName === 'French') return name.startsWith('[French');
        if (groupName === 'German') return name.startsWith('[German');
        if (groupName === 'Anime') return name.startsWith('[Anime');
        return !name.startsWith('[');
      });
    },

    // --- Quality Editor ---

    // Ensure every pb.qualityItems entry has a stable _id so shared qs-helpers can track drag/drop
    // and rename by identity (not index). Call this whenever entering the quality editor.
    pbEnsureQualityIds() {
      let changed = false;
      for (const it of (this.pb.qualityItems || [])) {
        if (!it._id) { it._id = ++this._qsIdCounter; changed = true; }
      }
      if (changed) this.pb.qualityItems = [...this.pb.qualityItems];
    },

    async pbInitQualityEditor() {
      // If we already have items, use them
      if (this.pb.qualityItems.length > 0) return;
      // Try to load quality definitions from first instance of matching type
      const inst = this.instancesOfType(this.pb.appType)[0];
      if (inst) {
        try {
          const r = await fetch(`/api/instances/${inst.id}/quality-definitions`);
          if (r.ok) {
            const defs = await r.json();
            // Create default items (all ungrouped, none allowed, reversed for highest priority first)
            this.pb.qualityItems = defs.reverse().map(d => ({ name: d.name, allowed: false }));
            return;
          }
        } catch (e) { /* ignore */ }
      }
      // Fallback: hardcoded Radarr defaults (highest priority first)
      const defaults = ['BR-DISK','Raw-HD','Remux-2160p','Bluray-2160p','WEBRip-2160p','WEBDL-2160p',
        'HDTV-2160p','Remux-1080p','Bluray-1080p','WEBRip-1080p','WEBDL-1080p','HDTV-1080p',
        'Bluray-720p','WEBRip-720p','WEBDL-720p','HDTV-720p',
        'Bluray-576p','Bluray-480p','WEBRip-480p','WEBDL-480p',
        'DVD-R','DVD','SDTV','DVDSCR','REGIONAL','TELECINE','TELESYNC','CAM','WORKPRINT','Unknown'];
      this.pb.qualityItems = defaults.map(name => ({ name, allowed: false }));
    },

    pbMoveQuality(idx, dir) {
      const items = [...this.pb.qualityItems];
      const newIdx = idx + dir;
      if (newIdx < 0 || newIdx >= items.length) return;
      [items[idx], items[newIdx]] = [items[newIdx], items[idx]];
      this.pb.qualityItems = items;
    },

    pbRemoveFromGroup(groupIdx, subIdx) {
      const items = [...this.pb.qualityItems];
      const group = { ...items[groupIdx], items: [...items[groupIdx].items] };
      const removed = group.items.splice(subIdx, 1)[0];
      if (group.items.length === 0) {
        // Group is empty, replace with single quality using group name
        items[groupIdx] = { name: group.name, allowed: group.allowed };
      } else {
        items[groupIdx] = group;
      }
      // Insert removed item after the group, inheriting group's allowed state
      items.splice(groupIdx + 1, 0, { name: removed, allowed: group.allowed });
      this.pbUpdateQualityDisplay();
      this.pb.qualityItems = items;
    },

    pbAddToGroup(itemIdx, groupName) {
      if (!groupName) return;
      const items = [...this.pb.qualityItems];
      const item = items[itemIdx];

      if (groupName === '__new__') {
        const name = prompt('Group name:');
        if (!name) return;
        // Convert item into a group
        items[itemIdx] = { name: name, allowed: item.allowed, items: [item.name] };
        this.pb.qualityItems = items;
        return;
      }

      // Remove item first, then find and update group (avoids index shift)
      items.splice(itemIdx, 1);
      const groupIdx = items.findIndex(q => q.name === groupName && q.items?.length > 0);
      if (groupIdx < 0) return;
      items[groupIdx] = { ...items[groupIdx], items: [...items[groupIdx].items, item.name] };
      this.pb.qualityItems = items;
      this.pbUpdateQualityDisplay();
    },

    pbUpdateQualityDisplay() {
      const allowed = this.pb.qualityItems.filter(q => q.allowed).map(q => q.name);
      this.pb.qualityAllowedNames = allowed.join(', ');
      // Update cutoff if current cutoff is no longer allowed
      if (this.pb.cutoff && !allowed.includes(this.pb.cutoff)) {
        this.pb.cutoff = allowed[0] || '';
      }
    },

    pbApplyQualityPreset() {
      const id = this.pb.qualityPresetId;
      if (!id) {
        this.pb.cutoff = '';
        this.pb.qualityPreset = '';
        this.pb.qualityAllowedNames = '';
        this.pb.qualityItems = [];
        return;
      }
      const preset = this.pbQualityPresets.find(p => p.id === id);
      if (!preset) return;
      this.pb.cutoff = preset.cutoff;
      this.pb.qualityPreset = preset.id;
      this.pb.qualityAllowedNames = (preset.allowed || []).join(', ');
      this.pb.qualityItems = preset.items || [];
    },

    // Languages for profile builder — uses first instance of matching type, or fallback
    get pbLanguages() {
      const inst = this.instancesOfType(this.pb.appType)[0];
      if (inst && this.instanceLanguages[inst.id]) return this.instanceLanguages[inst.id];
      // Trigger async load if instance available
      if (inst && !this.instanceLanguages[inst.id]) this.getLanguagesForInstance(inst.id);
      return [{ id: -1, name: 'Original' }, { id: 0, name: 'Any' }];
    },

    pbSelectedCount() {
      return Object.keys(this.pb.selectedCFs).filter(k => this.pb.selectedCFs[k]).length;
    },

    pbRequiredCount() {
      return Object.keys(this.pb.requiredCFs).filter(k => this.pb.requiredCFs[k] && this.pb.selectedCFs[k]).length;
    },

    get pbFilteredCategories() {
      if (!this.pbCategories.length) return this.pbCategories;

      return this.pbCategories.map(cat => {
        let filtered = cat.groups;

        // Filter by template profile if set
        if (this.pb.trashProfileName) {
          const profName = this.pb.trashProfileName;
          const fiSet = this.pb.formatItemCFs || {};
          const selSet = this.pb.selectedCFs || {};
          const enSet = this.pb.enabledGroups || {};
          filtered = filtered.filter(g => {
            // Always show if group includes this profile
            if (!g.includeProfiles || g.includeProfiles.length === 0 || g.includeProfiles.includes(profName)) return true;
            // Show if group is enabled
            if (g.groupTrashId && enSet[g.groupTrashId]) return true;
            // Show if any of the group's CFs are in formatItems or selected
            if (g.cfs?.some(cf => fiSet[cf.trashId] || selSet[cf.trashId])) return true;
            return false;
          });
        }

        // Filter by variant dropdowns
        {
          if (this.pb.variantGoldenRule === 'HD') {
            filtered = filtered.filter(g => g.name !== '[Required] Golden Rule UHD');
          } else if (this.pb.variantGoldenRule === 'UHD') {
            filtered = filtered.filter(g => g.name !== '[Required] Golden Rule HD');
          } else if (this.pb.variantGoldenRule === 'none') {
            filtered = filtered.filter(g => g.name !== '[Required] Golden Rule HD' && g.name !== '[Required] Golden Rule UHD');
          }
          if (this.pb.variantMisc === 'Standard') {
            filtered = filtered.filter(g => g.name !== '[Optional] Miscellaneous SQP');
          } else if (this.pb.variantMisc === 'SQP') {
            filtered = filtered.filter(g => g.name !== '[Optional] Miscellaneous');
          } else if (this.pb.variantMisc === 'none') {
            filtered = filtered.filter(g => g.name !== '[Optional] Miscellaneous' && g.name !== '[Optional] Miscellaneous SQP');
          }
        }

        if (filtered.length === 0) return null;
        return { ...cat, groups: filtered };
      }).filter(Boolean);
    },

    pbHasGroupVariants() {
      // Check if there are conflicting group pairs in the categories
      const names = new Set();
      for (const cat of this.pbCategories) {
        for (const g of cat.groups) names.add(g.name);
      }
      return (names.has('[Required] Golden Rule HD') && names.has('[Required] Golden Rule UHD')) ||
             (names.has('[Optional] Miscellaneous') && names.has('[Optional] Miscellaneous SQP'));
    },

    pbGroupVariant(type) {
      const names = new Set();
      for (const cat of this.pbCategories) {
        for (const g of cat.groups) names.add(g.name);
      }
      if (type === 'Golden Rule') return names.has('[Required] Golden Rule HD') && names.has('[Required] Golden Rule UHD');
      if (type === 'Miscellaneous') return names.has('[Optional] Miscellaneous') && names.has('[Optional] Miscellaneous SQP');
      return false;
    },

    // Check if a group is disabled because another group sharing CFs has active selections
    pbIsGroupDisabled(group) {
      if (!group.cfs || group.cfs.length === 0) return false;
      const groupCFIds = new Set(group.cfs.map(cf => cf.trashId));
      // Check if any of this group's CFs are selected
      const hasOwnSelection = group.cfs.some(cf => this.pb.selectedCFs[cf.trashId]);
      if (hasOwnSelection) return false; // This group is active, not disabled
      // Check all groups (not just filtered) for shared CF conflicts
      for (const cat of this.pbCategories) {
        for (const g of cat.groups) {
          if (g === group || g.name === group.name) continue;
          const shared = g.cfs?.some(cf => groupCFIds.has(cf.trashId));
          if (shared && g.cfs?.some(cf => this.pb.selectedCFs[cf.trashId])) {
            return true; // Another group with shared CFs has active selections
          }
        }
      }
      return false;
    },

    _pbCatCFs(cat) {
      // Flatten all CFs across groups in a category
      const cfs = [];
      for (const g of (cat.groups || [])) {
        for (const cf of (g.cfs || [])) cfs.push(cf);
      }
      return cfs;
    },

    pbCatSelectedCount(cat) {
      return this._pbCatCFs(cat).filter(cf => this.pb.selectedCFs[cf.trashId]).length;
    },

    pbCatTotalCount(cat) {
      return this._pbCatCFs(cat).length;
    },

    pbIsCatAllSelected(cat) {
      const cfs = this._pbCatCFs(cat);
      return cfs.length > 0 && cfs.every(cf => this.pb.selectedCFs[cf.trashId]);
    },

    pbGroupSelectedCount(group) {
      return (group.cfs || []).filter(cf => this.pb.selectedCFs[cf.trashId]).length;
    },

    pbIsGroupAllSelected(group) {
      return group.cfs.length > 0 && group.cfs.every(cf => this.pb.selectedCFs[cf.trashId]);
    },

    pbToggleCF(trashId, exclusiveGroup) {
      if (this.pb.selectedCFs[trashId]) {
        const {[trashId]: _s, ...restSelected} = this.pb.selectedCFs;
        const {[trashId]: _r, ...restRequired} = this.pb.requiredCFs;
        const {[trashId]: _o, ...restOverrides} = this.pb.scoreOverrides;
        this.pb.selectedCFs = restSelected;
        this.pb.requiredCFs = restRequired;
        this.pb.scoreOverrides = restOverrides;
      } else {
        const newSelected = {...this.pb.selectedCFs, [trashId]: true};
        // Exclusive group: deselect other CFs in this group AND any other
        // exclusive groups that share the same CFs (e.g. Golden Rule HD/UHD)
        if (exclusiveGroup) {
          const sharedIds = new Set(exclusiveGroup.cfs.map(cf => cf.trashId));
          for (const cf of exclusiveGroup.cfs) {
            if (cf.trashId !== trashId) delete newSelected[cf.trashId];
          }
          // Find all other exclusive groups sharing any CF with this group
          for (const cat of this.pbFilteredCategories) {
            for (const g of cat.groups) {
              if (g === exclusiveGroup || !g.exclusive) continue;
              if (g.cfs.some(cf => sharedIds.has(cf.trashId))) {
                for (const cf of g.cfs) {
                  if (cf.trashId !== trashId) delete newSelected[cf.trashId];
                }
              }
            }
          }
        }
        this.pb.selectedCFs = newSelected;
      }
    },

    pbToggleCategory(cat) {
      const cfs = this._pbCatCFs(cat);
      const allSelected = this.pbIsCatAllSelected(cat);
      const newSelected = {...this.pb.selectedCFs};
      const newRequired = {...this.pb.requiredCFs};
      const newOverrides = {...this.pb.scoreOverrides};
      for (const cf of cfs) {
        if (allSelected) {
          delete newSelected[cf.trashId];
          delete newRequired[cf.trashId];
          delete newOverrides[cf.trashId];
        } else {
          newSelected[cf.trashId] = true;
        }
      }
      this.pb.selectedCFs = newSelected;
      this.pb.requiredCFs = newRequired;
      this.pb.scoreOverrides = newOverrides;
    },

    pbIsCatAllRequired(cat) {
      const cfs = this._pbCatCFs(cat);
      const selected = cfs.filter(cf => this.pb.selectedCFs[cf.trashId]);
      return selected.length > 0 && selected.every(cf => this.pb.requiredCFs[cf.trashId]);
    },

    // Toggle a CF group on/off by groupTrashId
    pbToggleGroupInclude(group) {
      const gid = group.groupTrashId;
      const newEnabled = { ...this.pb.enabledGroups };
      const newSelected = { ...this.pb.selectedCFs };
      const newFormatItems = { ...this.pb.formatItemCFs };
      if (newEnabled[gid]) {
        // Disable group — remove its CFs from selectedCFs and formatItemCFs
        delete newEnabled[gid];
        for (const cf of group.cfs) {
          delete newSelected[cf.trashId];
          delete newFormatItems[cf.trashId];
        }
      } else {
        // Enable group — add its CFs to selectedCFs (group CFs don't go in formatItems)
        newEnabled[gid] = true;
        for (const cf of group.cfs) {
          newSelected[cf.trashId] = true;
        }
      }
      this.pb.enabledGroups = newEnabled;
      this.pb.selectedCFs = newSelected;
      this.pb.formatItemCFs = newFormatItems;
    },

    // Check if a group is enabled
    pbIsGroupEnabled(group) {
      return !!this.pb.enabledGroups[group.groupTrashId];
    },

    // Get CF state: 'formatItems', 'required', or 'optional'
    pbGetCFState(cf) {
      if (this.pb.formatItemCFs[cf.trashId]) return 'formatItems';
      if (this.pb.cfStateOverrides?.[cf.trashId] === 'required') return 'required';
      if (this.pb.cfStateOverrides?.[cf.trashId] === 'optional') return 'optional';
      // Default from TRaSH group data
      return cf.required ? 'required' : 'optional';
    },

    // Set CF state: 'required', 'optional', or 'formatItems'
    pbSetCFState(trashId, state) {
      const newFI = { ...this.pb.formatItemCFs };
      const newOverrides = { ...(this.pb.cfStateOverrides || {}) };

      if (state === 'formatItems') {
        newFI[trashId] = true;
        delete newOverrides[trashId];
        this.pb.selectedCFs = { ...this.pb.selectedCFs, [trashId]: true };
      } else {
        delete newFI[trashId];
        newOverrides[trashId] = state;
      }
      this.pb.formatItemCFs = newFI;
      this.pb.cfStateOverrides = newOverrides;
    },

    // Toggle a CF into/out of formatItemCFs (required/mandatory)
    pbToggleFormatItem(trashId) {
      const newFI = { ...this.pb.formatItemCFs };
      if (newFI[trashId]) {
        delete newFI[trashId];
      } else {
        newFI[trashId] = true;
      }
      this.pb.formatItemCFs = newFI;
    },

    // Get selected formatItem CFs as a list with CF data
    pbFormatItemCFList() {
      const result = [];
      const fiSet = this.pb.formatItemCFs || {};
      const seen = new Set();
      // Include ALL CFs that are in formatItemCFs, regardless of group membership
      for (const cat of this.pbCategories) {
        for (const g of cat.groups) {
          for (const cf of g.cfs) {
            if (fiSet[cf.trashId] && !seen.has(cf.trashId)) {
              result.push(cf);
              seen.add(cf.trashId);
            }
          }
        }
      }
      return result;
    },

    // Get ungrouped CFs NOT in formatItems (for "Add more" section)
    pbAvailableFormatItemCFs() {
      const result = [];
      const fiSet = this.pb.formatItemCFs || {};
      for (const cat of this.pbCategories) {
        for (const g of cat.groups) {
          if (g.groupTrashId) continue; // only ungrouped CFs available to add
          for (const cf of g.cfs) {
            if (!fiSet[cf.trashId]) result.push(cf);
          }
        }
      }
      return result;
    },

    // Get available CFs filtered by search term
    pbFilteredAvailableCFs() {
      const all = this.pbAvailableFormatItemCFs();
      const q = (this.pbFormatItemSearch || '').trim().toLowerCase();
      if (!q) return all;
      return all.filter(cf => cf.name.toLowerCase().includes(q));
    },

    // Get all groups as a flat sorted list (not nested under categories)
    pbSortedGroups() {
      const groupOrder = [
        'Golden Rule', 'Audio', 'HDR Formats', 'HQ Release Groups', 'Resolution',
        'Streaming Services', 'Miscellaneous', 'Optional', 'SQP',
        'Release Groups', 'Unwanted', 'Movie Versions', 'Anime',
        'French Audio Version', 'French HQ Source Groups',
        'German Source Groups', 'German Miscellaneous', 'Language Profiles', 'Other'
      ];
      const groups = [];
      for (const cat of this.pbFilteredCategories) {
        for (const g of cat.groups) {
          if (!g.groupTrashId) continue;
          groups.push({ ...g, _category: cat.category });
        }
      }
      groups.sort((a, b) => {
        const ai = groupOrder.indexOf(a._category);
        const bi = groupOrder.indexOf(b._category);
        const ao = ai === -1 ? 999 : ai;
        const bo = bi === -1 ? 999 : bi;
        if (ao !== bo) return ao - bo;
        // Within same category: default-enabled first, then alphabetical
        if (a.defaultEnabled !== b.defaultEnabled) return a.defaultEnabled ? -1 : 1;
        return a.shortName.localeCompare(b.shortName);
      });
      return groups;
    },

    // Check if any CF in group has a specific state
    pbGroupHasAnyState(group, state) {
      return group.cfs.some(cf => this.pbGetCFState(cf) === state);
    },

    // Check if ALL CFs in group have a specific state
    pbGroupHasAllState(group, state) {
      return group.cfs.length > 0 && group.cfs.every(cf => this.pbGetCFState(cf) === state);
    },

    // Set all CFs in a group to a state
    pbSetGroupState(group, state) {
      for (const cf of group.cfs) {
        this.pbSetCFState(cf.trashId, state);
      }
      // If all CFs moved to formatItems, disable the group (no longer needed as group)
      if (state === 'formatItems' && group.groupTrashId) {
        const newEnabled = { ...this.pb.enabledGroups };
        delete newEnabled[group.groupTrashId];
        this.pb.enabledGroups = newEnabled;
      }
      // If moving back from formatItems to group state, re-enable the group
      if (state !== 'formatItems' && group.groupTrashId && !this.pb.enabledGroups[group.groupTrashId]) {
        this.pb.enabledGroups = { ...this.pb.enabledGroups, [group.groupTrashId]: true };
      }
    },

    // Count formatItem CFs
    pbFormatItemCount() {
      return Object.keys(this.pb.formatItemCFs || {}).length;
    },

    // Count enabled groups
    pbEnabledGroupCount() {
      return Object.keys(this.pb.enabledGroups || {}).length;
    },

    // Golden Rule: auto-set both CFs when variant is selected
    pbIsGoldenRuleCF(trashId) {
      return trashId === 'dc98083864ea246d05a42df0d05f81cc' || trashId === '839bea857ed2c0a8e084f3cbdbd65ecb';
    },

    pbApplyGoldenRule() {
      const grHDcf1 = 'dc98083864ea246d05a42df0d05f81cc';   // x265 (HD)
      const grUHDcf1 = '839bea857ed2c0a8e084f3cbdbd65ecb';  // x265 (no HDR/DV)
      const grHDGroup = 'f8bf8eab4617f12dfdbd16303d8da245';  // [Required] Golden Rule HD group
      const grUHDGroup = 'ff204bbcecdd487d1cefcefdbf0c278d'; // [Required] Golden Rule UHD group
      const newSelected = {...this.pb.selectedCFs};
      const newEnabled = {...this.pb.enabledGroups};
      const variant = this.pb.variantGoldenRule;

      // Find all CFs in each Golden Rule group from pbCategories
      const grHDCFs = [];
      const grUHDCFs = [];
      for (const cat of this.pbCategories) {
        for (const g of cat.groups) {
          if (g.groupTrashId === grHDGroup) grHDCFs.push(...g.cfs.map(cf => cf.trashId));
          if (g.groupTrashId === grUHDGroup) grUHDCFs.push(...g.cfs.map(cf => cf.trashId));
        }
      }

      if (variant === 'HD') {
        newEnabled[grHDGroup] = true;
        delete newEnabled[grUHDGroup];
        for (const tid of grHDCFs) newSelected[tid] = true;
        for (const tid of grUHDCFs) delete newSelected[tid];
        this.pb.goldenRuleDefault = grHDcf1;
      } else if (variant === 'UHD') {
        delete newEnabled[grHDGroup];
        newEnabled[grUHDGroup] = true;
        for (const tid of grUHDCFs) newSelected[tid] = true;
        for (const tid of grHDCFs) delete newSelected[tid];
        this.pb.goldenRuleDefault = grUHDcf1;
      } else {
        delete newEnabled[grHDGroup];
        delete newEnabled[grUHDGroup];
        for (const tid of grHDCFs) delete newSelected[tid];
        for (const tid of grUHDCFs) delete newSelected[tid];
        this.pb.goldenRuleDefault = '';
      }
      this.pb.selectedCFs = newSelected;
      this.pb.enabledGroups = newEnabled;
    },

    pbApplyMisc() {
      const miscStdGroup = '9337080378236ce4c0b183e35790d2a7';  // [Optional] Miscellaneous
      const miscSqpGroup = 'c4492eebd0c2ddc14c2c91623aa7f95d';  // [Optional] Miscellaneous SQP
      const newEnabled = { ...this.pb.enabledGroups };
      const newSelected = { ...this.pb.selectedCFs };
      const variant = this.pb.variantMisc;

      // Find CFs in each Misc group
      const stdCFs = [];
      const sqpCFs = [];
      for (const cat of this.pbCategories) {
        for (const g of cat.groups) {
          if (g.groupTrashId === miscStdGroup) stdCFs.push(...g.cfs.map(cf => cf.trashId));
          if (g.groupTrashId === miscSqpGroup) sqpCFs.push(...g.cfs.map(cf => cf.trashId));
        }
      }

      if (variant === 'Standard') {
        newEnabled[miscStdGroup] = true;
        delete newEnabled[miscSqpGroup];
        for (const tid of stdCFs) newSelected[tid] = true;
        for (const tid of sqpCFs) delete newSelected[tid];
      } else if (variant === 'SQP') {
        delete newEnabled[miscStdGroup];
        newEnabled[miscSqpGroup] = true;
        for (const tid of sqpCFs) newSelected[tid] = true;
        for (const tid of stdCFs) delete newSelected[tid];
      } else {
        delete newEnabled[miscStdGroup];
        delete newEnabled[miscSqpGroup];
        for (const tid of stdCFs) delete newSelected[tid];
        for (const tid of sqpCFs) delete newSelected[tid];
      }
      this.pb.selectedCFs = newSelected;
      this.pb.enabledGroups = newEnabled;
    },

    pbToggleCatRequired(cat) {
      const cfs = this._pbCatCFs(cat);
      const allReq = this.pbIsCatAllRequired(cat);
      const newRequired = {...this.pb.requiredCFs};
      const newSelected = {...this.pb.selectedCFs};
      for (const cf of cfs) {
        if (allReq) {
          // Switch all to optional (keep selected)
          delete newRequired[cf.trashId];
        } else {
          // Switch all to required (also select unselected CFs)
          newSelected[cf.trashId] = true;
          newRequired[cf.trashId] = true;
        }
      }
      this.pb.selectedCFs = newSelected;
      this.pb.requiredCFs = newRequired;
    },

    pbToggleGroup(group) {
      const allSelected = this.pbIsGroupAllSelected(group);
      const newSelected = {...this.pb.selectedCFs};
      for (const cf of group.cfs) {
        if (allSelected) {
          delete newSelected[cf.trashId];
        } else {
          newSelected[cf.trashId] = true;
        }
      }
      this.pb.selectedCFs = newSelected;
    },

    pbGetScore(cf) {
      if (this.pb.scoreOverrides[cf.trashId] !== undefined) {
        return this.pb.scoreOverrides[cf.trashId];
      }
      const scores = cf.trashScores || {};
      return scores[this.pb.scoreSet] ?? scores['default'] ?? 0;
    },

    pbSetScore(trashId, value) {
      this.pb.scoreOverrides[trashId] = parseInt(value) || 0;
    },

    pbCleanScore(trashId) {
      // If override matches TRaSH default, remove override
      const cf = this._pbFindCF(trashId);
      if (!cf) return;
      const trashScore = cf.trashScores?.[this.pb.scoreSet] ?? cf.trashScores?.['default'] ?? 0;
      if (this.pb.scoreOverrides[trashId] === trashScore) {
        const {[trashId]: _, ...rest} = this.pb.scoreOverrides;
        this.pb.scoreOverrides = rest;
      }
    },

    pbIsScoreOverridden(cf) {
      if (this.pb.scoreOverrides[cf.trashId] === undefined) return false;
      const trashScore = cf.trashScores?.[this.pb.scoreSet] ?? cf.trashScores?.['default'] ?? 0;
      return this.pb.scoreOverrides[cf.trashId] !== trashScore;
    },

    _pbFindCF(trashId) {
      for (const cat of this.pbCategories) {
        for (const g of (cat.groups || [])) {
          for (const cf of (g.cfs || [])) {
            if (cf.trashId === trashId) return cf;
          }
        }
      }
      return null;
    },

    sortedScoreSets() {
      return this.pbScoreSets.filter(s => s !== 'default').sort((a, b) => {
        const aIsSqp = a.startsWith('sqp') ? 0 : 1;
        const bIsSqp = b.startsWith('sqp') ? 0 : 1;
        if (aIsSqp !== bIsSqp) return aIsSqp - bIsSqp;
        return a.localeCompare(b);
      });
    },

    pbScoreSetChanged() {
      // Clear overrides that now match the new score set defaults
      for (const trashId of Object.keys(this.pb.scoreOverrides)) {
        this.pbCleanScore(trashId);
      }
    },

    async pbApplyTemplate() {
      const tid = this.pb.templateId;
      if (!tid) return;
      this.debugLog('UI', `Builder: applying template "${tid}"`);
      this.pbTemplateLoading = true;
      try {
        if (tid.startsWith('trash:')) {
          const trashId = tid.slice(6);
          const r = await fetch(`/api/trash/${this.pb.appType}/profiles/${trashId}`);
          if (!r.ok) { this.showToast('Failed to load TRaSH profile', 'error', 8000); return; }
          const detail = await r.json();
          // Apply score set and link to TRaSH profile (enables v8 export)
          if (detail.scoreCtx) this.pb.scoreSet = detail.scoreCtx;
          this.pb.trashProfileId = trashId;
          this.pb.trashProfileName = detail.profile?.name || '';
          this.pb.trashScoreSet = detail.scoreCtx || '';
          this.pb.trashDescription = detail.profile?.trash_description || '';
          // Apply quality preset and sync dropdown
          this.pb.qualityPreset = trashId;
          this.pb.qualityPresetId = trashId;
          this.pb.qualityItems = detail.profile?.items || [];
          // Apply profile settings
          const prof = detail.profile || {};
          if (prof.cutoff) this.pb.cutoff = prof.cutoff;
          // Update allowed names display
          const matchedPreset = this.pbQualityPresets.find(p => p.id === trashId);
          if (matchedPreset) {
            this.pb.qualityAllowedNames = (matchedPreset.allowed || []).join(', ');
          } else {
            const allowedItems = (prof.items || []).filter(i => i.allowed).map(i => i.name);
            this.pb.qualityAllowedNames = allowedItems.join(', ');
            // The template's quality config may not be in presets — set qualityPresetId to match cutoff
            const cutoffMatch = this.pbQualityPresets.find(p => p.cutoff === prof.cutoff);
            if (cutoffMatch) this.pb.qualityPresetId = cutoffMatch.id;
          }
          if (prof.cutoffFormatScore != null) this.pb.cutoffScore = prof.cutoffFormatScore;
          if (prof.minFormatScore != null) this.pb.minFormatScore = prof.minFormatScore;
          if (prof.minUpgradeFormatScore != null) this.pb.minUpgradeFormatScore = prof.minUpgradeFormatScore;
          if (prof.upgradeAllowed != null) this.pb.upgradeAllowed = prof.upgradeAllowed;
          // Reset expanded state
          this.pbExpandedCats = {};
          this.pbAddMoreOpen = false;
          // Apply required CFs (core profile definition → formatItems)
          this.pb.selectedCFs = {};
          this.pb.scoreOverrides = {};
          this.pb.requiredCFs = {};
          this.pb.formatItemCFs = {};
          this.pb.enabledGroups = {};
          this.pb.cfStateOverrides = {};
          const baselineCFs = new Set();
          const coreCFIds = [];
          const coreCFSet = new Set((detail.coreCFs || []).map(cf => cf.trashId));

          // Build lookup: which CFs belong to which groups (regardless of profile include)
          const cfToGroup = {};
          for (const cat of this.pbCategories) {
            for (const g of cat.groups) {
              if (!g.groupTrashId) continue;
              for (const cf of g.cfs) {
                cfToGroup[cf.trashId] = g;
              }
            }
          }

          // Check if entire groups are in formatItems (all group CFs are in coreCFs)
          const groupsInFormatItems = new Set();
          for (const cat of this.pbCategories) {
            for (const g of cat.groups) {
              if (!g.groupTrashId) continue;
              const allInCore = g.cfs.every(cf => coreCFSet.has(cf.trashId));
              if (allInCore && g.cfs.length > 0) {
                groupsInFormatItems.add(g.groupTrashId);
              }
            }
          }

          for (const cf of (detail.coreCFs || [])) {
            this.pb.selectedCFs[cf.trashId] = true;
            baselineCFs.add(cf.trashId);
            coreCFIds.push(cf.trashId);
            if (cf.score != null) this.pb.scoreOverrides[cf.trashId] = cf.score;

            const group = cfToGroup[cf.trashId];
            if (group && groupsInFormatItems.has(group.groupTrashId)) {
              // CF belongs to a group that's entirely in formatItems → set as Fmt in group
              this.pb.formatItemCFs[cf.trashId] = true;
              this.pb.enabledGroups[group.groupTrashId] = true;
              // Set CF state to formatItems within the group
              if (!this.pb.cfStateOverrides) this.pb.cfStateOverrides = {};
              this.pb.cfStateOverrides[cf.trashId] = 'formatItems';
            } else if (group) {
              // CF is in a group but not all group CFs are in formatItems — treat as individual formatItem
              this.pb.formatItemCFs[cf.trashId] = true;
            } else {
              // Ungrouped CF — normal formatItem
              this.pb.formatItemCFs[cf.trashId] = true;
            }
          }

          // Apply CFs from groups — only enable groups that include this profile
          for (const cat of this.pbCategories) {
            for (const g of cat.groups) {
              const includesProfile = g.includeProfiles?.includes(this.pb.trashProfileName);
              if (!includesProfile) continue;
              // Skip groups already handled as formatItems
              if (groupsInFormatItems.has(g.groupTrashId)) continue;
              // Only auto-enable default groups
              if (g.groupTrashId && g.defaultEnabled) {
                this.pb.enabledGroups[g.groupTrashId] = true;
              }
              if (g.defaultEnabled) {
                for (const cf of g.cfs) {
                  this.pb.selectedCFs[cf.trashId] = true;
                  baselineCFs.add(cf.trashId);
                }
              }
            }
          }
          // Store baseline so export knows what TRaSH defines vs user additions
          this.pb.baselineCFs = [...baselineCFs];
          this.pb.coreCFIds = coreCFIds;
          // Store original formatItems key order from TRaSH profile (for identical export)
          this.pb.formatItemsOrder = detail.formatItemsOrder || [];
          // Detect Golden Rule and Misc variants from groups that include this profile
          const includedGroupNames = new Set();
          for (const cat of this.pbCategories) {
            for (const g of cat.groups) {
              if (g.includeProfiles?.includes(this.pb.trashProfileName)) {
                includedGroupNames.add(g.name);
              }
            }
          }
          if (includedGroupNames.has('[Required] Golden Rule HD')) this.pb.variantGoldenRule = 'HD';
          else if (includedGroupNames.has('[Required] Golden Rule UHD')) this.pb.variantGoldenRule = 'UHD';
          else this.pb.variantGoldenRule = 'none';
          if (includedGroupNames.has('[Optional] Miscellaneous SQP')) this.pb.variantMisc = 'SQP';
          else if (includedGroupNames.has('[Optional] Miscellaneous')) this.pb.variantMisc = 'Standard';
          else this.pb.variantMisc = 'none';
        } else if (tid.startsWith('import:')) {
          const importId = tid.slice(7);
          const profiles = this.importedProfiles[this.pb.appType] || [];
          const prof = profiles.find(p => p.id === importId);
          if (!prof) { this.showToast('Imported profile not found', 'error', 8000); return; }
          // Apply settings
          if (prof.scoreSet) this.pb.scoreSet = prof.scoreSet;
          if (prof.cutoff) this.pb.cutoff = prof.cutoff;
          if (prof.cutoffScore != null) this.pb.cutoffScore = prof.cutoffScore;
          if (prof.minFormatScore != null) this.pb.minFormatScore = prof.minFormatScore;
          if (prof.minUpgradeFormatScore != null) this.pb.minUpgradeFormatScore = prof.minUpgradeFormatScore;
          if (prof.upgradeAllowed != null) this.pb.upgradeAllowed = prof.upgradeAllowed;
          if (prof.language) this.pb.language = prof.language;
          if (prof.trashProfileId) {
            this.pb.qualityPreset = prof.trashProfileId;
            this.pb.qualityPresetId = prof.trashProfileId;
          }
          if (prof.qualities?.length) {
            this.pb.qualityItems = prof.qualities;
          }
          // Apply CFs and scores
          this.pb.selectedCFs = {};
          this.pb.scoreOverrides = {};
          this.pb.requiredCFs = {};
          this.pb.formatItemCFs = {};
          this.pb.enabledGroups = {};
          this.pb.cfStateOverrides = {};
          for (const [trashId, score] of Object.entries(prof.formatItems || {})) {
            this.pb.selectedCFs[trashId] = true;
            this.pb.scoreOverrides[trashId] = score;
          }
          // Restore new model state if available
          if (prof.formatItemCFs) {
            this.pb.formatItemCFs = { ...prof.formatItemCFs };
          }
          if (prof.enabledGroups) {
            this.pb.enabledGroups = { ...prof.enabledGroups };
          }
          if (prof.cfStateOverrides) {
            this.pb.cfStateOverrides = { ...prof.cfStateOverrides };
          }
          // Fallback: old model
          for (const tid of (prof.requiredCFs || [])) {
            this.pb.requiredCFs[tid] = true;
            // If no new model, use old requiredCFs as formatItemCFs
            if (!prof.formatItemCFs) this.pb.formatItemCFs[tid] = true;
          }
        }
        // Clean overrides that match score set defaults
        this.pbScoreSetChanged();
        // Force Alpine reactivity on pb object (needed for x-model on nested selects)
        this.pb = { ...this.pb };
      } catch (e) {
        this.showToast('Error loading template: ' + e.message, 'error', 8000);
      } finally {
        this.pbTemplateLoading = false;
      }
    },

    async pbInstanceChanged() {
      this.pbInstanceImportProfileId = '';
      this.pbInstanceImportProfiles = [];
      if (!this.pbInstanceImportId) return;
      try {
        const r = await fetch(`/api/instances/${this.pbInstanceImportId}/profiles`);
        if (r.ok) this.pbInstanceImportProfiles = await r.json();
      } catch (e) {
        console.error('Failed to load instance profiles:', e);
      }
    },

    async pbApplyInstanceProfile() {
      if (!this.pbInstanceImportId || !this.pbInstanceImportProfileId) return;
      this.pbInstanceImportLoading = true;
      try {
        const r = await fetch(`/api/instances/${this.pbInstanceImportId}/profile-export/${this.pbInstanceImportProfileId}`);
        if (!r.ok) { this.showToast('Failed to load profile from instance', 'error', 8000); return; }
        const data = await r.json();
        const prof = data.profile;
        // Apply directly to builder — no saving until user clicks Create Profile
        this.pb.name = prof.name || '';
        if (prof.cutoff) this.pb.cutoff = prof.cutoff;
        if (prof.cutoffScore != null) this.pb.cutoffScore = prof.cutoffScore;
        if (prof.minFormatScore != null) this.pb.minFormatScore = prof.minFormatScore;
        if (prof.minUpgradeFormatScore != null) this.pb.minUpgradeFormatScore = prof.minUpgradeFormatScore;
        if (prof.upgradeAllowed != null) this.pb.upgradeAllowed = prof.upgradeAllowed;
        if (prof.language) this.pb.language = prof.language;
        // Apply quality items from instance
        if (prof.qualities?.length) {
          this.pb.qualityItems = prof.qualities;
          this.pb.qualityAllowedNames = prof.qualities.filter(q => q.allowed).map(q => q.name).join(', ');
        }
        // Apply CFs and scores. Arr profiles are a flat formatItems array — map each CF
        // into Builder's "Required CFs" section (pb.formatItemCFs). Without this, imported
        // CFs end up in selectedCFs/scoreOverrides only, which doesn't render them anywhere
        // in the UI (grouped CFs need an enabled group; ungrouped CFs need formatItemCFs).
        // Setting formatItemCFs also activates the "Fmt" pill on CFs that happen to be in
        // a TRaSH group, which is the existing convention for moving a grouped CF into
        // formatItems — consistent with manual Fmt clicks.
        this.pb.selectedCFs = {};
        this.pb.scoreOverrides = {};
        this.pb.requiredCFs = {};
        this.pb.formatItemCFs = {};
        for (const [trashId, score] of Object.entries(prof.formatItems || {})) {
          this.pb.selectedCFs[trashId] = true;
          this.pb.scoreOverrides[trashId] = score;
          this.pb.formatItemCFs[trashId] = true;
        }
        this.pb.formatItemCFs = { ...this.pb.formatItemCFs };
        this.pbScoreSetChanged();
        // Notify about unmapped CFs
        if (data.unmapped && data.unmapped.length > 0) {
          this.showToast(`Profile loaded. ${data.unmapped.length} CF(s) could not be mapped to TRaSH IDs:\n\n${data.unmapped.join('\n')}`, 'error', 8000);
        }
      } catch (e) {
        this.showToast('Error loading profile: ' + e.message, 'error', 8000);
      } finally {
        this.pbInstanceImportLoading = false;
      }
    },

    _pbSaveDefaults() {
      try {
        localStorage.setItem('clonarr-pb-defaults', JSON.stringify({
          variantGoldenRule: this.pb.variantGoldenRule,
          variantMisc: this.pb.variantMisc,
          qualityPresetId: this.pb.qualityPresetId,
          scoreSet: this.pb.scoreSet,
          trashScoreSet: this.pb.trashScoreSet,
        }));
      } catch (e) {}
    },

    _pbLoadDefaults() {
      try {
        const raw = localStorage.getItem('clonarr-pb-defaults');
        return raw ? JSON.parse(raw) : {};
      } catch (e) { return {}; }
    },

    async saveCustomProfile() {
      this.pbSaving = true;
      try {
        // Check for duplicate name (only when creating, not editing)
        if (!this.pb.editId) {
          const existing = (this.importedProfiles[this.pb.appType] || []).find(
            p => p.name.toLowerCase() === this.pb.name.trim().toLowerCase()
          );
          if (existing) {
            // Find next available suffix
            const baseName = this.pb.name.trim();
            let suffix = 2;
            let newName = baseName + ' (' + suffix + ')';
            while ((this.importedProfiles[this.pb.appType] || []).some(
              p => p.name.toLowerCase() === newName.toLowerCase()
            )) {
              suffix++;
              newName = baseName + ' (' + suffix + ')';
            }
            await new Promise((resolve, reject) => {
              this.confirmModal = {
                show: true,
                title: 'Profile Name Already Exists',
                message: `A profile named "${baseName}" already exists.\n\nThe new profile will be saved as "${newName}".`,
                onConfirm: resolve,
                onCancel: reject
              };
            }).catch(() => { this.pbSaving = false; throw new Error('cancelled'); });
            this.pb.name = newName;
          }
        }

        // Build formatItems, formatComments, and formatGroups from selected CFs
        const formatItems = {};
        const formatComments = {};
        const formatGroups = {};
        const requiredCFs = [];
        // Build CF → group name lookup from pbCategories
        const cfGroupLookup = {};
        for (const cat of this.pbCategories) {
          for (const g of cat.groups) {
            for (const cf of (g.cfs || [])) {
              cfGroupLookup[cf.trashId] = g.name;
            }
          }
        }
        // Build set of CFs that should be in FormatItems for sync:
        // 1. All formatItemCFs (mandatory)
        // 2. CFs from enabled groups
        const syncSet = new Set(Object.keys(this.pb.formatItemCFs || {}));
        const enabledGroupIds = new Set(Object.keys(this.pb.enabledGroups || {}));
        for (const cat of this.pbCategories) {
          for (const g of cat.groups) {
            if (g.groupTrashId && enabledGroupIds.has(g.groupTrashId)) {
              for (const cf of g.cfs) syncSet.add(cf.trashId);
            }
          }
        }
        for (const trashId of syncSet) {
          const cf = this._pbFindCF(trashId);
          const score = this.pb.scoreOverrides[trashId] ?? cf?.trashScores?.[this.pb.scoreSet] ?? cf?.trashScores?.['default'] ?? 0;
          formatItems[trashId] = score;
          if (cf) formatComments[trashId] = cf.name;
          if (cfGroupLookup[trashId]) formatGroups[trashId] = cfGroupLookup[trashId];
        }

        // Use cached quality items from preset selection (stored when preset is applied)
        let qualities = this.pb.qualityItems || [];
        if (qualities.length === 0) {
          // Try fetching from quality preset if set
          if (this.pb.qualityPreset) {
            try {
              const r = await fetch(`/api/trash/${this.pb.appType}/profiles/${this.pb.qualityPreset}`);
              if (r.ok) {
                const detail = await r.json();
                qualities = detail.profile?.items || [];
              }
            } catch (e) { /* ignore fetch errors */ }
          }
          // Still empty — try from preset dropdown
          if (qualities.length === 0 && this.pb.qualityPresetId) {
            const preset = this.pbQualityPresets.find(p => p.id === this.pb.qualityPresetId);
            if (preset) qualities = preset.items || [];
          }
        }
        if (qualities.length === 0) {
          await new Promise((resolve, reject) => {
            this.confirmModal = {
              show: true,
              title: 'No Quality Items',
              message: 'No quality items configured. The profile will not work in Radarr/Sonarr without quality items.\n\nSelect a Quality Preset to include them.',
              onConfirm: resolve,
              onCancel: reject
            };
          }).catch(() => { this.pbSaving = false; throw new Error('cancelled'); });
        }

        const profile = {
          name: this.pb.name.trim(),
          appType: this.pb.appType,
          source: 'custom',
          scoreSet: this.pb.scoreSet !== 'default' ? this.pb.scoreSet : '',
          upgradeAllowed: this.pb.upgradeAllowed,
          cutoff: this.pb.cutoff,
          cutoffScore: this.pb.cutoffScore,
          minFormatScore: this.pb.minFormatScore,
          minUpgradeFormatScore: this.pb.minUpgradeFormatScore,
          language: this.pb.appType === 'radarr' ? this.pb.language : undefined,
          qualities: qualities,
          formatItems: formatItems,
          formatComments: formatComments,
          formatGroups: Object.keys(formatGroups).length > 0 ? formatGroups : undefined,
          requiredCFs: requiredCFs,
          defaultOnCFs: Object.keys(this.pb.defaultOnCFs || {}).filter(k => this.pb.defaultOnCFs[k] && this.pb.selectedCFs[k]),
          baselineCFs: this.pb.baselineCFs?.length ? this.pb.baselineCFs : undefined,
          coreCFIds: this.pb.coreCFIds?.length ? this.pb.coreCFIds : undefined,
          formatItemsOrder: this.pb.formatItemsOrder?.length ? this.pb.formatItemsOrder : undefined,
          // Builder state (preserved for edit)
          formatItemCFs: Object.keys(this.pb.formatItemCFs || {}).length > 0 ? this.pb.formatItemCFs : undefined,
          enabledGroups: Object.keys(this.pb.enabledGroups || {}).length > 0 ? this.pb.enabledGroups : undefined,
          cfStateOverrides: Object.keys(this.pb.cfStateOverrides || {}).length > 0 ? this.pb.cfStateOverrides : undefined,
          variantGoldenRule: this.pb.variantGoldenRule || undefined,
          goldenRuleDefault: this.pb.goldenRuleDefault || undefined,
          variantMisc: this.pb.variantMisc || undefined,
          qualityPresetId: this.pb.qualityPresetId || undefined,
          // Dev mode
          trashProfileId: this.pb.trashProfileId || undefined,
          trashScoreSet: this.pb.trashScoreSet || undefined,
          trashDescription: this.pb.trashDescription || undefined,
          groupNum: this.pb.groupNum || undefined,
        };

        let url, method;
        if (this.pb.editId) {
          url = `/api/custom-profiles/${this.pb.editId}`;
          method = 'PUT';
        } else {
          url = '/api/custom-profiles';
          method = 'POST';
        }

        const r = await fetch(url, {
          method,
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(profile),
        });

        if (r.ok) {
          this._pbSaveDefaults();
          this.profileBuilder = false;
          // Return to previous subtab if we came from resync Edit
          if (this._resyncReturnSubTab) {
            this.currentSection = 'profiles';
            this._resyncReturnSubTab = null;
          }
          this.loadImportedProfiles(this.pb.appType);
        } else {
          const data = await r.json();
          this.showToast(data.error || 'Failed to save profile', 'error', 8000);
        }
      } catch (e) {
        if (e.message !== 'cancelled') this.showToast('Error: ' + e.message, 'error', 8000);
      }
      this.pbSaving = false;
    },

    async openImportedProfileDetail(appType, profile) {
      this.syncPlan = null;
      this.syncResult = null;
      this.showProfileInfo = false;
      this.selectedOptionalCFs = {};

      // If this imported profile has a trashProfileId, use TRaSH detail endpoint
      // for proper categorization (Required, Optional groups, descriptions, etc.)
      if (profile.trashProfileId) {
        // Need an instance to call the API — use first of this type
        const inst = this.instancesOfType(appType)[0];
        if (inst) {
          this.profileDetail = { instance: inst, profile: { name: profile.name, trashId: profile.trashProfileId }, detail: null };
          try {
            const r = await fetch(`/api/trash/${appType}/profiles/${profile.trashProfileId}`);
            if (r.ok) {
              const detail = await r.json();
              // Overlay imported profile settings onto TRaSH detail
              detail.imported = true;
              detail.importedRaw = profile;
              // Use imported profile's settings (may differ from TRaSH defaults)
              detail.profile.upgradeAllowed = profile.upgradeAllowed;
              detail.profile.cutoff = profile.cutoff || detail.profile.cutoff;
              detail.profile.minFormatScore = profile.minFormatScore;
              detail.profile.cutoffFormatScore = profile.cutoffScore || detail.profile.cutoffFormatScore;
              detail.profile.minUpgradeFormatScore = profile.minUpgradeFormatScore;
              detail.profile.language = profile.language || detail.profile.language;
              if (profile.scoreSet) detail.profile.scoreSet = profile.scoreSet;
              this.profileDetail = { instance: inst, profile: { name: profile.name, trashId: profile.trashProfileId }, detail };
              this.initDetailSections(detail);
              this.initSelectedCFs(detail);
              return;
            }
          } catch (e) { console.error('loadImportedProfileDetail:', e); }
        }
      }

      // Fallback: use imported profile detail endpoint (builds TRaSH groups from CF membership)
      try {
        const r = await fetch(`/api/import/profiles/${profile.id}/detail`);
        if (r.ok) {
          const detail = await r.json();
          this.profileDetail = {
            instance: { type: profile.appType, name: profile.appType },
            profile: { name: profile.name },
            detail
          };
          this.initDetailSections(detail);
          this.initSelectedCFs(detail);
          return;
        }
      } catch (e) { console.error('loadImportedProfileDetail:', e); }
    },

    openExportModalFromList(appType, profile) {
      this.exportSource = profile;
      this.openExportModal();
    },

    async openExportModal() {
      if (!this.exportSource) {
        this.exportSource = this.profileDetail?.detail?.importedRaw;
      }
      // Ensure CF group data is loaded for v8 YAML export
      const appType = this.exportSource?.appType;
      if (appType && !this.cfBrowseData[appType]) {
        await this.loadCFBrowse(appType);
      }
      this.exportTab = 'yaml';
      this.exportCopied = false;
      this.generateExport();
      this.showExportModal = true;
    },

    closeExportModal() {
      this.showExportModal = false;
      this.exportSource = null;
    },

    generateExport() {
      const p = this.exportSource;
      if (!p) return;
      if (this.exportTab === 'yaml') {
        this.exportContent = this.generateRecyclarrYAML(p);
        this.exportGroupIncludes = [];
      } else if (this.exportTab === 'trash') {
        this.exportContent = this.generateTrashJSON(p);
        this.exportGroupIncludes = this.generateGroupIncludes(p);
      }
    },

    generateRecyclarrYAML(p) {
      const appType = p.appType || 'radarr';
      const hasTrashId = !!p.trashProfileId;

      // v8 format when profile has trashProfileId, v7 otherwise
      const lines = [];
      lines.push(`${appType}:`);
      lines.push(`  exported-profile:`);
      if (p.qualityType) {
        lines.push(`    quality_definition:`);
        lines.push(`      type: ${p.qualityType}`);
      }
      lines.push(``);
      lines.push(`    quality_profiles:`);
      if (hasTrashId) {
        // v8: reference profile by trash_id — guide handles qualities, cutoff, scores
        lines.push(`      - trash_id: ${p.trashProfileId}  # ${p.name}`);
        // Name override if user renamed the profile
        const trashProfiles = this.trashProfiles?.[p.appType || 'radarr'] || [];
        const origProfile = trashProfiles.find(tp => tp.trashId === p.trashProfileId);
        if (origProfile && p.name && p.name !== origProfile.name) {
          lines.push(`        name: ${p.name}`);
        }
        lines.push(`        reset_unmatched_scores:`);
        lines.push(`          enabled: true`);
      } else {
        // v7: reference profile by name
        lines.push(`      - name: ${p.name}`);
        if (p.scoreSet) {
          lines.push(`        score_set: ${p.scoreSet}`);
        }
        if (p.upgradeAllowed || p.cutoff || p.cutoffScore) {
          lines.push(`        upgrade:`);
          lines.push(`          allowed: ${p.upgradeAllowed ? 'true' : 'false'}`);
          if (p.cutoff) lines.push(`          until_quality: ${p.cutoff}`);
          if (p.cutoffScore) lines.push(`          until_score: ${p.cutoffScore}`);
        }
        if (p.minFormatScore !== undefined && p.minFormatScore !== null) {
          lines.push(`        min_format_score: ${p.minFormatScore}`);
        }
        if (p.minUpgradeFormatScore !== undefined && p.minUpgradeFormatScore !== null) {
          lines.push(`        min_upgrade_format_score: ${p.minUpgradeFormatScore}`);
        }
      }
      if (!hasTrashId && p.resetUnmatchedScores) {
        lines.push(`        reset_unmatched_scores:`);
        lines.push(`          enabled: true`);
        if (p.resetExcept && p.resetExcept.length > 0) {
          lines.push(`          except:`);
          for (const name of p.resetExcept) {
            lines.push(`            - "${name}"`);
          }
        }
      }
      if (!hasTrashId && p.qualities && p.qualities.length > 0) {
        lines.push(`        quality_sort: top`);
        lines.push(`        qualities:`);
        for (const q of p.qualities) {
          lines.push(`          - name: ${q.name}`);
          if (q.items && q.items.length > 0) {
            lines.push(`            qualities:`);
            for (const item of q.items) {
              lines.push(`              - ${item}`);
            }
          }
          if (q.allowed === false) {
            lines.push(`            enabled: false`);
          }
        }
      }

      if (hasTrashId) {
        // v8: use custom_format_groups
        this._generateV8CFGroups(p, lines);
      } else {
        // v7: use custom_formats with explicit scores
        this._generateV7CFs(p, lines);
      }

      return lines.join('\n');
    },

    _generateV7CFs(p, lines) {
      const scoreGroups = {};
      for (const [tid, score] of Object.entries(p.formatItems || {})) {
        const key = String(score);
        if (!scoreGroups[key]) scoreGroups[key] = [];
        const comment = (p.formatComments || {})[tid];
        scoreGroups[key].push({ tid, comment });
      }
      if (Object.keys(scoreGroups).length > 0) {
        lines.push(``);
        lines.push(`    custom_formats:`);
        const sortedScores = Object.keys(scoreGroups).sort((a, b) => Number(b) - Number(a));
        for (const score of sortedScores) {
          const cfs = scoreGroups[score];
          lines.push(`      - trash_ids:`);
          for (const cf of cfs) {
            const comment = cf.comment ? ` # ${cf.comment}` : '';
            lines.push(`          - ${cf.tid}${comment}`);
          }
          lines.push(`        assign_scores_to:`);
          lines.push(`          - name: ${p.name}`);
          lines.push(`            score: ${score}`);
        }
      }
    },

    _generateV8CFGroups(p, lines) {
      // In v8, guide-backed profiles with score_set get scores automatically from TRaSH data.
      // The YAML only needs custom_format_groups to specify which optional groups to include.
      const appType = p.appType || 'radarr';
      const groups = this.cfBrowseData[appType]?.groups || [];
      if (groups.length === 0) {
        this._generateV7CFs(p, lines);
        return;
      }

      const profileCFs = new Set(Object.keys(p.formatItems || {}));

      // Map CF trash_id → group
      const cfToGroup = {};
      for (const g of groups) {
        for (const cf of (g.custom_formats || [])) {
          cfToGroup[cf.trash_id] = { groupId: g.trash_id, groupName: g.name, isDefault: g.default === 'true' };
        }
      }

      // Classify: which groups have CFs selected in this profile?
      const groupedCFs = {}; // groupId → [tids]
      for (const tid of profileCFs) {
        const gi = cfToGroup[tid];
        if (gi) {
          if (!groupedCFs[gi.groupId]) groupedCFs[gi.groupId] = [];
          groupedCFs[gi.groupId].push(tid);
        }
      }

      // Baseline CFs = what the TRaSH profile defines by default (core + default groups)
      const baseline = new Set(p.baselineCFs || []);

      // Build add entries for groups that need explicit configuration
      const addEntries = [];
      // Build skip list for default groups entirely excluded
      const skipEntries = [];

      // Helper: check if a group is linked to this profile via quality_profiles.include
      const profileTrashId = p.trashProfileId || '';
      const isGroupForProfile = (g) => {
        const include = g.quality_profiles?.include || {};
        return Object.values(include).includes(profileTrashId);
      };

      for (const g of groups) {
        const gid = g.trash_id;
        const allGroupCFs = (g.custom_formats || []).map(cf => cf.trash_id);
        const selected = groupedCFs[gid] || [];

        if (g.default === 'true') {
          // Only consider default groups that are linked to this profile
          if (!isGroupForProfile(g)) continue;
          if (selected.length === 0) {
            // No CFs selected → skip entire default group
            skipEntries.push({ groupId: gid, groupName: g.name });
          } else if (selected.length < allGroupCFs.length) {
            // Partial selection → add with select to override default
            addEntries.push({ groupId: gid, groupName: g.name, select: selected });
          }
          // All selected → no entry needed (default behavior)
        } else {
          // Non-default group: must be explicitly added if user selected CFs from it
          if (selected.length === 0) continue;
          // Skip if all selected CFs are already in the baseline (TRaSH profile definition)
          if (selected.every(tid => baseline.has(tid))) continue;
          const allSelected = allGroupCFs.every(tid => profileCFs.has(tid));
          if (allSelected) {
            addEntries.push({ groupId: gid, groupName: g.name, selectAll: true });
          } else {
            addEntries.push({ groupId: gid, groupName: g.name, select: selected });
          }
        }
      }

      if (addEntries.length > 0 || skipEntries.length > 0) {
        lines.push(``);
        lines.push(`    custom_format_groups:`);
        if (addEntries.length > 0) {
          lines.push(`      add:`);
          for (const entry of addEntries) {
            lines.push(`        - trash_id: ${entry.groupId}  # ${entry.groupName}`);
            if (entry.selectAll) {
              lines.push(`          select_all: true`);
            } else if (entry.select) {
              lines.push(`          select:`);
              for (const tid of entry.select) {
                const comment = (p.formatComments || {})[tid];
                lines.push(`            - ${tid}${comment ? '  # ' + comment : ''}`);
              }
            }
          }
        }
        if (skipEntries.length > 0) {
          lines.push(`      skip:`);
          for (const entry of skipEntries) {
            lines.push(`        - ${entry.groupId}  # ${entry.groupName}`);
          }
        }
      }

      // In v8, score_set handles ALL TRaSH CF scores automatically.
      // Only emit custom_formats for user-created custom CFs (not from TRaSH guides).
      const customCFs = {};
      for (const [tid, score] of Object.entries(p.formatItems || {})) {
        if (tid.startsWith('custom:')) {
          const key = String(score);
          if (!customCFs[key]) customCFs[key] = [];
          customCFs[key].push(tid);
        }
      }
      if (Object.keys(customCFs).length > 0) {
        lines.push(``);
        lines.push(`    custom_formats:`);
        const sortedScores = Object.keys(customCFs).sort((a, b) => Number(b) - Number(a));
        for (const score of sortedScores) {
          lines.push(`      - trash_ids:`);
          for (const tid of customCFs[score]) {
            const comment = (p.formatComments || {})[tid];
            lines.push(`          - ${tid}${comment ? '  # ' + comment : ''}`);
          }
          lines.push(`        assign_scores_to:`);
          lines.push(`          - trash_id: ${p.trashProfileId}`);
          lines.push(`            score: ${score}`);
        }
      }
    },

    generateTrashJSON(p) {
      // TRaSH format: formatItems is { "CF Name": "trash_id" }
      // Only formatItemCFs go in formatItems — group CFs belong in CF group files
      const formatItemSet = new Set(Object.keys(p.formatItemCFs || {}));
      const coreCFs = new Set(p.coreCFIds || []);
      const requiredSet = new Set(p.requiredCFs || []);

      // Collect eligible trash IDs
      const eligibleTids = [];
      for (const [tid] of Object.entries(p.formatItems || {})) {
        if (formatItemSet.size > 0) {
          if (!formatItemSet.has(tid)) continue;
        } else if (requiredSet.size > 0 && !requiredSet.has(tid)) continue;
        else if (requiredSet.size === 0 && coreCFs.size > 0 && !coreCFs.has(tid)) continue;
        eligibleTids.push(tid);
      }

      // Sort formatItems to match TRaSH's convention:
      // 1. Grouped CFs (Audio, HQ Groups) — by group order, score descending within
      // 2. Tiers (Remux/HD Bluray/WEB) — by name (natural order: 01, 02, 03)
      // 3. Repack — fixed order: Proper, 2, 3
      // 4. Unwanted — fixed order matching TRaSH convention
      // 5. Misc (10 bit, AV1, etc.)
      // 6. Resolution (1080p, 2160p, 720p) — last
      const comments = p.formatComments || {};
      const scores = p.formatItems || {};

      // Build CF group membership from pbCategories
      const cfGroupIdx = {};
      let gIdx = 0;
      for (const cat of (this.pbCategories || [])) {
        for (const g of (cat.groups || [])) {
          if (!g.groupTrashId) continue;
          for (const cf of (g.cfs || [])) {
            cfGroupIdx[cf.trashId] = gIdx;
          }
          gIdx++;
        }
      }

      // Assign sort bucket to each CF
      const unwantedOrder = ['BR-DISK', 'Generated Dynamic HDR', 'LQ', 'LQ (Release Title)',
        'x265 (HD)', '3D', 'Upscaled', 'Extras', 'Sing-Along Versions'];
      const repackOrder = ['Repack/Proper', 'Repack2', 'Repack3'];
      const resolutionOrder = ['720p', '1080p', '2160p'];
      const getBucket = (tid) => {
        const name = comments[tid] || tid;
        if (resolutionOrder.includes(name)) return 5; // resolution — always last
        if (cfGroupIdx[tid] !== undefined) return 0; // grouped CF
        if (/Tier \d/.test(name) || name === 'BHDStudio' || name === 'hallowed') return 1; // tiers/hq groups
        if (repackOrder.includes(name)) return 2;
        if (unwantedOrder.includes(name) || (scores[tid] ?? 0) <= -10000) return 3;
        return 4; // misc
      };

      eligibleTids.sort((a, b) => {
        const ba = getBucket(a), bb = getBucket(b);
        if (ba !== bb) return ba - bb;

        const nameA = comments[a] || a, nameB = comments[b] || b;

        // Bucket 0: grouped CFs — by group index, then score descending
        if (ba === 0) {
          const gi = cfGroupIdx[a] ?? 999, gj = cfGroupIdx[b] ?? 999;
          if (gi !== gj) return gi - gj;
          const sa = scores[a] ?? 0, sb = scores[b] ?? 0;
          if (sa !== sb) return sb - sa;
          return nameA.localeCompare(nameB);
        }
        // Bucket 1: tiers — score descending
        if (ba === 1) {
          const sa = scores[a] ?? 0, sb = scores[b] ?? 0;
          if (sa !== sb) return sb - sa;
          return nameA.localeCompare(nameB);
        }
        // Bucket 2: repack — fixed order
        if (ba === 2) return repackOrder.indexOf(nameA) - repackOrder.indexOf(nameB);
        // Bucket 3: unwanted — fixed order, then alphabetical for unknowns
        if (ba === 3) {
          const ia = unwantedOrder.indexOf(nameA), ib = unwantedOrder.indexOf(nameB);
          if (ia >= 0 && ib >= 0) return ia - ib;
          if (ia >= 0) return -1;
          if (ib >= 0) return 1;
          return nameA.localeCompare(nameB);
        }
        // Bucket 4: misc — score descending
        if (ba === 4) {
          const sa = scores[a] ?? 0, sb = scores[b] ?? 0;
          if (sa !== sb) return sb - sa;
          return nameA.localeCompare(nameB);
        }
        // Bucket 5: resolution — fixed order (720p, 1080p, 2160p)
        return resolutionOrder.indexOf(nameA) - resolutionOrder.indexOf(nameB);
      });

      const formatItems = {};
      for (const tid of eligibleTids) {
        const name = (p.formatComments || {})[tid] || tid;
        formatItems[name] = tid;
      }

      // Build items array (qualities) — match TRaSH format exactly
      const items = (p.qualities || []).map(q => {
        const item = { name: q.name, allowed: q.allowed !== false };
        if (q.items && q.items.length > 0) {
          item.items = q.items;
        }
        return item;
      });

      // Build profile matching TRaSH's official JSON structure
      const trashProfile = {
        trash_id: p.trashProfileId || '',
        name: p.name,
      };
      if (p.scoreSet) trashProfile.trash_score_set = p.scoreSet;
      if (p.trashDescription) {
        trashProfile.trash_description = p.trashDescription;
      }
      trashProfile.group = p.groupNum || 99;
      trashProfile.upgradeAllowed = p.upgradeAllowed || false;
      trashProfile.cutoff = p.cutoff || '';
      trashProfile.minFormatScore = p.minFormatScore ?? 0;
      trashProfile.cutoffFormatScore = p.cutoffScore ?? 10000;
      trashProfile.minUpgradeFormatScore = p.minUpgradeFormatScore ?? 0;
      // Sonarr profiles don't have a language field — the UI removed it
      // from the General section, but the export was still emitting
      // `"language": "Original"` for Sonarr JSON. TRaSH's actual Sonarr
      // profile files omit the key entirely; match that convention.
      if ((p.appType || 'radarr') === 'radarr') {
        trashProfile.language = p.language || 'Original';
      }
      trashProfile.items = items;
      trashProfile.formatItems = formatItems;

      // No cfGroupIncludes — TRaSH format doesn't use it
      // Group includes are handled via group files, not profile JSON

      // Custom JSON formatting to match TRaSH style:
      // - items array with inline sub-arrays and compact single-line entries
      let json = JSON.stringify(trashProfile, null, 2);

      // Reformat the "items" array to match TRaSH style
      // Replace multi-line items arrays with inline: ["item1", "item2"]
      json = json.replace(/"items": \[\n\s+("(?:[^"]+)"(?:,\n\s+"(?:[^"]+)")*)\n\s+\]/g, (match, inner) => {
        const vals = inner.replace(/\n\s+/g, ' ');
        return '"items": [' + vals + ']';
      });
      // Compact simple quality entries onto single lines
      json = json.replace(/\{\n\s+"name": "([^"]+)",\n\s+"allowed": (true|false)\n\s+\}/g,
        '{ "name": "$1", "allowed": $2 }');

      return json;
    },

    // Generate group include snippets for enabled groups
    generateGroupIncludes(p) {
      const enabledGroups = p.enabledGroups || {};
      if (Object.keys(enabledGroups).length === 0) return [];
      const snippets = [];
      // Use pbCategories (in builder) or raw groups from cfBrowseData
      if (this.pbCategories?.length > 0) {
        for (const cat of this.pbCategories) {
          for (const g of (cat.groups || [])) {
            if (!g.groupTrashId || !enabledGroups[g.groupTrashId]) continue;
            snippets.push({
              groupName: g.name, groupTrashId: g.groupTrashId,
              profileName: p.name, profileTrashId: p.trashProfileId || '',
              snippet: `"${p.name}": "${p.trashProfileId || 'GENERATE_ID'}"`,
            });
          }
        }
      } else {
        // Fallback: raw group data from cfBrowseData
        const groups = this.cfBrowseData[p.appType || 'radarr']?.groups || [];
        for (const g of groups) {
          const gid = g.trash_id;
          if (!gid || !enabledGroups[gid]) continue;
          snippets.push({
            groupName: g.name, groupTrashId: gid,
            profileName: p.name, profileTrashId: p.trashProfileId || '',
            snippet: `"${p.name}": "${p.trashProfileId || 'GENERATE_ID'}"`,
          });
        }
      }
      return snippets;
    },

    getCFBrowseGroups(appType) {
      const data = this.cfBrowseData[appType];
      if (!data) return [];

      // Build CF lookup by trash_id
      const cfMap = {};
      for (const cf of data.cfs) {
        cfMap[cf.trash_id] = cf;
      }

      // Each TRaSH group file becomes its own top-level category
      const categories = [];
      const usedCFIds = new Set();

      for (const group of data.groups) {
        let prefix = '', shortName = '';
        if (group.name.startsWith('[')) {
          const idx = group.name.indexOf(']');
          if (idx > 0) {
            prefix = group.name.substring(1, idx).trim();
            shortName = group.name.substring(idx + 1).trim();
          }
        }
        // Remap prefixes
        if (prefix === 'Required') prefix = 'Golden Rule';
        if (prefix === 'SQP') prefix = 'Miscellaneous';
        // Display name: use shortName if present, otherwise prefix, otherwise full name
        const displayName = shortName ? (prefix + ' — ' + shortName) : (prefix || group.name);
        // Category class uses the prefix for color matching
        const categoryClass = prefix || 'Other';

        const cfs = [];
        for (const cfEntry of (group.custom_formats || [])) {
          usedCFIds.add(cfEntry.trash_id);
          const cf = cfMap[cfEntry.trash_id];
          cfs.push({
            trashId: cfEntry.trash_id,
            name: cfEntry.name || cf?.name || cfEntry.trash_id,
            description: cf?.description || '',
            score: cf?.trash_scores?.default,
          });
        }

        if (cfs.length > 0) {
          categories.push({
            category: categoryClass,
            displayName,
            groups: [{ name: group.name, shortName: shortName || displayName, cfs }],
            totalCFs: cfs.length,
            trashDescription: group.trash_description || '',
          });
        }
      }

      // CFs not in any TRaSH group go into "Other"
      const ungrouped = [];
      for (const cf of data.cfs) {
        if (!usedCFIds.has(cf.trash_id)) {
          ungrouped.push({ trashId: cf.trash_id, name: cf.name, description: cf.description || '', score: cf.trash_scores?.default });
        }
      }
      if (ungrouped.length > 0) {
        ungrouped.sort((a, b) => a.name.localeCompare(b.name));
        categories.push({ category: 'Other', displayName: 'Other', groups: [{ name: 'Other', shortName: 'Other', cfs: ungrouped }], totalCFs: ungrouped.length });
      }

      // Inject custom CFs
      const customCFs = data.customCFs || [];
      if (customCFs.length > 0) {
        const allCustomCFs = customCFs.map(ccf => ({ trashId: ccf.id, name: ccf.name, description: '', score: undefined, isCustom: true }));
        allCustomCFs.sort((a, b) => a.name.localeCompare(b.name));
        categories.push({ category: 'Custom', displayName: 'Custom', groups: [{ name: 'Custom Formats', shortName: 'Custom Formats', cfs: allCustomCFs }], totalCFs: allCustomCFs.length, isCustom: true });
      }

      // Sort by prefix order, then by display name within same prefix
      const order = { 'Golden Rule': 0, 'Audio': 1, 'HDR Formats': 2, 'HQ Release Groups': 3,
                       'Resolution': 4, 'Streaming Services': 5, 'Miscellaneous': 6, 'Optional': 7,
                       'Release Groups': 8, 'Unwanted': 9, 'Movie Versions': 10, 'Anime': 11,
                       'French Audio Version': 12, 'French HQ Source Groups': 13,
                       'German Source Groups': 14, 'German Miscellaneous': 15,
                       'Language Profiles': 16, 'SQP': 17, 'Other': 18, 'Custom': 99 };
      return categories.sort((a, b) => {
        const oa = order[a.category] ?? 50, ob = order[b.category] ?? 50;
        if (oa !== ob) return oa - ob;
        return a.displayName.localeCompare(b.displayName);
      });
    },

    // --- CF Editor (Create/Edit) ---

    async openCFEditor(mode, appType, existingCF = null) {
      this.cfEditorMode = mode;
      this.cfEditorResult = null;
      this.cfEditorSaving = false;
      this.cfEditorShowPreview = false;
      this.cfEditorSpecCounter = 0;

      // Set appType first so loadCFEditorSchema can read it
      this.cfEditorForm.appType = appType;
      await this.loadCFEditorSchema();

      if (mode === 'edit' && existingCF) {
        // Load full custom CF data from API
        let allCFs;
        try {
          const res = await fetch(`/api/custom-cfs/${appType}`);
          allCFs = await res.json();
        } catch (e) {
          this.showToast('Could not load custom CF data: ' + e.message, 'error', 8000);
          return;
        }
        const full = (allCFs || []).find(c => c.id === existingCF.trashId);
        if (!full) {
          this.showToast('Custom CF not found — it may have been deleted', 'error', 8000);
          return;
        }
        this.cfEditorForm = {
          id: full.id,
          name: full.name,
          appType: full.appType,
          category: full.category || 'Custom',
          newCategory: '',
          includeInRename: full.includeInRename || false,
          specifications: (full.specifications || []).map(s => this.arrSpecToEditorSpec(s)),
          trashId: full.trashId || '',
          trashScores: Object.entries(full.trashScores || {}).map(([k,v]) => ({context:k, score:v})),
          description: full.description || '',
        };
      } else {
        this.cfEditorForm = {
          id: '',
          name: '',
          appType: appType,
          category: 'Custom',
          newCategory: '',
          includeInRename: false,
          specifications: [],
          trashId: '',
          trashScores: [],
          description: '',
        };
      }

      // Force Alpine reactivity on form object (needed for x-model on nested selects)
      this.cfEditorForm = { ...this.cfEditorForm };
      this.showCFEditor = true;
    },

    // Convert Arr API specification to editor format.
    // Matches fields against the loaded schema to restore dropdowns, checkboxes, etc.
    // Without this, Language specs show "value: 3" instead of a dropdown on edit.
    arrSpecToEditorSpec(arrSpec) {
      let fields = [];
      // Parse raw fields from the stored spec
      let rawFields = {};
      if (arrSpec.fields) {
        let parsed = arrSpec.fields;
        if (typeof parsed === 'string') {
          try { parsed = JSON.parse(parsed); } catch(e) { parsed = []; }
        }
        if (Array.isArray(parsed)) {
          for (const f of parsed) rawFields[f.name] = f.value;
        } else if (typeof parsed === 'object') {
          rawFields = { ...parsed };
        }
      }
      // Try to match against schema for this implementation type
      const schema = (this.cfEditorSchema[this.cfEditorForm.appType] || [])
        .find(s => s.implementation === arrSpec.implementation);
      if (schema) {
        fields = schema.fields.map(f => {
          let val = rawFields[f.name] !== undefined ? rawFields[f.name] : (f.defaultValue !== undefined ? f.defaultValue : '');
          // Select fields: keep as string to match HTML select behavior (x-model always returns strings).
          // Number coercion happens at save time, not at load time.
          if (f.type === 'select') val = String(val);
          return { name: f.name, value: val, label: f.label, type: f.type, selectOptions: f.selectOptions || [], placeholder: f.placeholder || '' };
        });
      } else {
        // No schema match — fallback to guessing
        fields = Object.entries(rawFields).map(([k, v]) => ({
          name: k,
          value: v,
          label: k,
          type: this.guessFieldType(k, v),
          selectOptions: [],
        }));
      }
      return {
        _key: ++this.cfEditorSpecCounter,
        name: arrSpec.name || '',
        implementation: arrSpec.implementation || '',
        negate: arrSpec.negate || false,
        required: arrSpec.required || false,
        fields: fields,
      };
    },

    guessFieldType(name, value) {
      if (typeof value === 'boolean') return 'checkbox';
      if (typeof value === 'number') return 'number';
      if (name === 'value' && typeof value === 'string') return 'textbox';
      return 'textbox';
    },

    async loadCFEditorSchema() {
      const appType = this.cfEditorForm.appType;
      if (this.cfEditorSchema[appType]) return;

      this.cfEditorSchemaLoading = true;
      try {
        const res = await fetch(`/api/customformat/schema/${appType}`);
        if (res.ok) {
          const schema = await res.json();
          // Parse schema into usable format: [{implementation, implementationName, fields:[{name,label,type,selectOptions}]}]
          const parsed = (schema || []).map(s => ({
            implementation: s.implementation,
            implementationName: s.implementationName || s.implementation.replace('Specification', ''),
            fields: (s.fields || []).map(f => ({
              name: f.name,
              label: f.label || f.name,
              type: this.mapSchemaFieldType(f),
              selectOptions: (f.selectOptions || []).map(o => ({
                value: o.value !== undefined ? o.value : o.id,
                name: o.name || String(o.value ?? o.id),
              })),
              placeholder: f.helpText || '',
              defaultValue: f.value,
            })),
          }));
          this.cfEditorSchema = { ...this.cfEditorSchema, [appType]: parsed };
        }
      } catch (e) {
        console.error('Failed to load CF schema:', e);
      } finally {
        this.cfEditorSchemaLoading = false;
      }
    },

    mapSchemaFieldType(field) {
      if (field.type === 'textbox' || field.type === 'text') return 'textbox';
      if (field.type === 'number' || field.type === 'integer') return 'number';
      if (field.type === 'select' || field.type === 'selectOption' || (field.selectOptions && field.selectOptions.length > 0)) return 'select';
      if (field.type === 'checkbox' || field.type === 'bool') return 'checkbox';
      // Guess from name/value
      if (typeof field.value === 'boolean') return 'checkbox';
      if (typeof field.value === 'number') return 'number';
      return 'textbox';
    },

    getAvailableImplementations() {
      return this.cfEditorSchema[this.cfEditorForm.appType] || [];
    },

    populatePBCutoffSelect(el, qualityItems, selectedValue) {
      // Build options from items with allowed=true. When no items are allowed
      // the select has a single disabled "No allowed qualities" option. x-for
      // inside <select> doesn't re-render when items[].allowed toggles, hence
      // the programmatic approach.
      const allowed = (qualityItems || []).filter(q => q.allowed);
      el.innerHTML = '';
      if (allowed.length === 0) {
        const o = document.createElement('option');
        o.value = '';
        o.textContent = 'No allowed qualities';
        o.disabled = true;
        el.appendChild(o);
        return;
      }
      for (const item of allowed) {
        const o = document.createElement('option');
        o.value = item.name;
        o.textContent = item.name;
        el.appendChild(o);
      }
      // Preserve selection if still in allowed list; otherwise pick first.
      const stillValid = allowed.some(q => q.name === selectedValue);
      const targetValue = stillValid ? selectedValue : allowed[0].name;
      el.value = targetValue;
      // Programmatic assignment does NOT fire @change, so Alpine's
      // `pb.cutoff = $el.value` binding never runs when we auto-pick the
      // first allowed quality on a new profile. The dropdown looks selected
      // but pb.cutoff stays empty — export produces `cutoff: ""`. Dispatch
      // a change event so the binding runs. Safe from looping: x-effect's
      // next pass sees pb.cutoff == targetValue and skips the dispatch.
      if (targetValue !== selectedValue) {
        el.dispatchEvent(new Event('change', { bubbles: true }));
      }
    },

    populateCutoffSelect(el, qualityStructure, profile, selectedValue, qualityOverrides) {
      // Two sources depending on mode:
      // 1) STRUCTURE-DRIVEN: qualityStructure has entries — user has grouped or
      //    reordered via Edit Groups. Use allowed flag on each item.
      // 2) LEGACY FLAT-TOGGLE: qualityStructure is empty; user toggles write to
      //    qualityOverrides map keyed by name. Here we MUST apply the overrides
      //    on top of profile.items — otherwise a just-toggled-on resolution
      //    won't appear in the cutoff dropdown until user opens Edit Groups
      //    (which initializes qualityStructure). That was the v2.0.6 bug.
      let items;
      if (qualityStructure.length > 0) {
        items = qualityStructure.filter(i => i.allowed !== false);
      } else {
        const overrides = qualityOverrides || {};
        items = (profile?.items || []).filter(i => {
          const effective = overrides[i.name] !== undefined ? overrides[i.name] : i.allowed;
          return effective !== false;
        });
      }
      const trashDefault = profile?.cutoff || '';
      const trashValid = !trashDefault || items.some(i => i.name === trashDefault);
      const options = [];
      // TRaSH default option (first)
      if (trashDefault) {
        options.push({ value: trashDefault, name: trashDefault + (trashValid ? ' (TRaSH default)' : ' (TRaSH default — not in structure)'), disabled: !trashValid });
      }
      // All allowed items except TRaSH default (avoid duplicate)
      for (const item of items) {
        if (item.name !== trashDefault) options.push({ value: item.name, name: item.name });
      }
      // Skip option
      options.push({ value: '__skip__', name: '— Don\'t sync cutoff —' });
      // Rebuild options
      el.innerHTML = '';
      for (const opt of options) {
        const o = document.createElement('option');
        o.value = opt.value;
        o.textContent = opt.name;
        if (opt.disabled) o.disabled = true;
        el.appendChild(o);
      }
      const targetValue = selectedValue || trashDefault;
      if (el.value !== targetValue) el.value = targetValue;
      // Same class of bug populatePBCutoffSelect fixed: programmatic
      // el.value doesn't fire @change, so pdOverrides.cutoffQuality stays
      // at a stale value when the dropdown auto-corrects (e.g. user
      // toggles off the quality that was the cutoff, the list rebuilds,
      // el.value falls back to TRaSH default, but the override state
      // never updates). Dispatch so the @change binding runs.
      if (targetValue !== selectedValue) {
        el.dispatchEvent(new Event('change', { bubbles: true }));
      }
    },

    populateSelectField(el, options, selectedValue) {
      const currentCount = el.options.length;
      const needsRebuild = currentCount !== options.length;
      if (needsRebuild) {
        el.innerHTML = '';
        for (const opt of options) {
          const o = document.createElement('option');
          o.value = String(opt.value ?? opt);
          o.textContent = opt.name ?? String(opt.value ?? opt);
          el.appendChild(o);
        }
      }
      if (el.value !== selectedValue) el.value = selectedValue;
    },

    populateImplSelect(el, selectedImpl) {
      const impls = this.getAvailableImplementations();
      // Remove old dynamic options (keep first "Select type..." option)
      for (let i = el.options.length - 1; i > 0; i--) el.remove(i);
      // Add options from schema
      impls.forEach(impl => {
        const opt = document.createElement('option');
        opt.value = impl.implementation;
        opt.textContent = impl.implementationName || impl.implementation.replace('Specification', '');
        el.appendChild(opt);
      });
      el.value = selectedImpl;
    },

    // TRaSH trash_scores context keys, derived at runtime from the actual
    // CF JSON files on disk via /api/trash/{app}/score-contexts.
    // Keeps the Custom Format editor dropdown in sync with upstream TRaSH
    // (new SQP tiers, new language variants, etc.) without hardcoded lists.
    // Cached per appType in _trashScoreContextCache; lazy-loaded on first access.
    trashScoreContexts(appType) {
      if (!appType) return ['default'];
      const cached = this._trashScoreContextCache[appType];
      if (cached) return cached;
      // Seed with 'default' so the dropdown is never empty while the fetch
      // is in flight. Alpine will re-render once the cache is populated.
      if (this._trashScoreContextCache[appType] === undefined) {
        this._trashScoreContextCache[appType] = ['default'];
        fetch(`/api/trash/${appType}/score-contexts`)
          .then(r => r.ok ? r.json() : ['default'])
          .then(keys => {
            this._trashScoreContextCache = { ...this._trashScoreContextCache, [appType]: (keys && keys.length ? keys : ['default']) };
          })
          .catch(() => {});
      }
      return this._trashScoreContextCache[appType];
    },

    addCFSpec() {
      this.cfEditorForm.specifications.push({
        _key: ++this.cfEditorSpecCounter,
        name: '',
        implementation: '',
        negate: false,
        required: false,
        fields: [],
      });
    },

    onSpecTypeChange(specIdx) {
      const spec = this.cfEditorForm.specifications[specIdx];
      const schema = this.getAvailableImplementations().find(s => s.implementation === spec.implementation);
      if (schema) {
        spec.fields = schema.fields.map(f => ({
          name: f.name,
          value: f.defaultValue !== undefined ? f.defaultValue : (f.type === 'checkbox' ? false : f.type === 'number' ? 0 : ''),
          label: f.label,
          type: f.type,
          selectOptions: f.selectOptions || [],
          placeholder: f.placeholder || '',
        }));
      } else {
        spec.fields = [{ name: 'value', value: '', label: 'Value', type: 'textbox', selectOptions: [], placeholder: '' }];
      }
    },

    getCFEditorPreviewJSON() {
      const f = this.cfEditorForm;
      const obj = {
        name: f.name,
        includeCustomFormatWhenRenaming: f.includeInRename,
        specifications: f.specifications.map(s => ({
          name: s.name,
          implementation: s.implementation,
          negate: s.negate,
          required: s.required,
          fields: s.fields.map(fld => ({ name: fld.name, value: fld.value })),
        })),
      };
      return JSON.stringify(obj, null, 2);
    },

    async saveCFEditor() {
      const f = this.cfEditorForm;
      if (!f.name.trim()) {
        this.cfEditorResult = { error: true, message: 'Name is required' };
        return;
      }
      if (f.specifications.length === 0) {
        this.cfEditorResult = { error: true, message: 'At least one specification is required' };
        return;
      }
      if (f.specifications.some(s => !s.implementation)) {
        this.cfEditorResult = { error: true, message: 'All specifications must have a type selected' };
        return;
      }

      const category = f.category === '' ? f.newCategory.trim() : f.category;
      if (!category) {
        this.cfEditorResult = { error: true, message: 'Please enter a category name' };
        return;
      }

      // Build payload in Arr field format: [{name, value}]
      // Coerce select field string values to numbers where appropriate (HTML select always returns strings)
      const specifications = f.specifications.map(s => ({
        name: s.name,
        implementation: s.implementation,
        negate: s.negate,
        required: s.required,
        fields: JSON.parse(JSON.stringify(s.fields.map(fld => {
          let val = fld.value;
          if (fld.type === 'select' && typeof val === 'string' && val !== '') {
            const n = Number(val);
            if (!isNaN(n)) val = n;
          }
          return { name: fld.name, value: val };
        }))),
      }));

      // Build trash_scores as object
      const trashScores = {};
      for (const ts of f.trashScores) {
        if (ts.context) trashScores[ts.context] = ts.score;
      }

      const payload = {
        name: f.name.trim(),
        appType: f.appType,
        category: category,
        includeInRename: f.includeInRename,
        specifications: specifications,
        trashId: f.trashId || '',
        trashScores: Object.keys(trashScores).length > 0 ? trashScores : undefined,
        description: f.description || '',
      };

      this.cfEditorSaving = true;
      this.cfEditorResult = null;

      try {
        let res;
        if (this.cfEditorMode === 'edit' && f.id) {
          // Update existing
          payload.id = f.id;
          res = await fetch(`/api/custom-cfs/${f.id}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(payload),
          });
        } else {
          // Create new
          res = await fetch('/api/custom-cfs', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ cfs: [payload] }),
          });
        }

        if (!res.ok) {
          let errMsg = 'Save failed';
          try { const err = await res.json(); errMsg = err.error || errMsg; } catch(_) {}
          this.cfEditorResult = { error: true, message: errMsg };
          return;
        }

        this.cfEditorResult = { error: false, message: this.cfEditorMode === 'edit' ? 'Updated successfully' : 'Created successfully' };
        // Refresh CF browse data
        this.loadCFBrowse(f.appType);
        // Close after brief delay to show success (keep saving state active)
        setTimeout(() => { this.showCFEditor = false; this.cfEditorSaving = false; }, 800);
        return; // skip finally's cfEditorSaving reset
      } catch (e) {
        this.cfEditorResult = { error: true, message: 'Network error: ' + e.message };
      }
      this.cfEditorSaving = false;
    },

    async deleteCustomCF(cf, appType) {
      if (!cf.isCustom || !cf.trashId) return;
      this.confirmModal = {
        show: true,
        title: 'Delete Custom Format',
        message: `Delete "${cf.name}"? This cannot be undone.`,
        confirmLabel: 'Delete',
        onConfirm: async () => {
          try {
            const res = await fetch(`/api/custom-cfs/${cf.trashId}`, { method: 'DELETE' });
            if (res.ok) {
              this.loadCFBrowse(appType);
            } else {
              let errMsg = 'Delete failed';
              try { const err = await res.json(); errMsg = err.error || errMsg; } catch(_) {}
              this.showToast(errMsg, 'error', 8000);
            }
          } catch (e) {
            this.showToast('Delete failed: ' + e.message, 'error', 8000);
          }
        },
        onCancel: null,
      };
    },

    async deleteCFFromEditor() {
      const f = this.cfEditorForm;
      if (!f.id) return;
      this.confirmModal = {
        show: true,
        title: 'Delete Custom Format',
        message: `Delete "${f.name}"? This cannot be undone.`,
        confirmLabel: 'Delete',
        onConfirm: async () => {
          try {
            const res = await fetch(`/api/custom-cfs/${f.id}`, { method: 'DELETE' });
            if (res.ok) {
              this.showCFEditor = false;
              this.loadCFBrowse(f.appType);
            } else {
              let errMsg = 'Delete failed';
              try { const err = await res.json(); errMsg = err.error || errMsg; } catch(_) {}
              this.cfEditorResult = { error: true, message: errMsg };
            }
          } catch (e) {
            this.cfEditorResult = { error: true, message: 'Delete failed: ' + e.message };
          }
        },
        onCancel: null,
      };
    },

    exportTrashJSON() {
      const f = this.cfEditorForm;
      const trashScores = {};
      for (const ts of f.trashScores) {
        if (ts.context) trashScores[ts.context] = ts.score;
      }

      const trashJSON = {
        trash_id: f.trashId || '',
        trash_scores: trashScores,
        name: f.name,
        includeCustomFormatWhenRenaming: f.includeInRename,
        specifications: f.specifications.map(s => ({
          name: s.name,
          implementation: s.implementation,
          negate: s.negate,
          required: s.required,
          fields: Object.fromEntries(s.fields.map(fld => [fld.name, fld.value])),
        })),
      };

      this.cfExportContent = JSON.stringify(trashJSON, null, 2);
      this.cfExportCopied = false;
    },

    // --- Import Custom CFs ---

    openImportCFModal(appType) {
      this.importCFAppType = appType;
      this.importCFSource = 'instance';
      this.importCFInstanceId = '';
      this.importCFList = [];
      this.importCFLoading = false;
      this.importCFCategory = 'Custom';
      this.importCFNewCategory = '';
      this.importCFJsonText = '';
      this.importCFJsonError = '';
      this.importCFResult = null;
      this.importCFImporting = false;
      this.showImportCFModal = true;
    },

    async fetchInstanceCFsForImport() {
      if (!this.importCFInstanceId) { this.importCFList = []; return; }
      this.importCFLoading = true;
      this.importCFList = [];
      try {
        // Fetch CFs from instance
        const res = await fetch(`/api/instances/${this.importCFInstanceId}/cfs`);
        const arrCFs = await res.json();
        // Fetch existing custom CFs to mark duplicates
        const existRes = await fetch(`/api/custom-cfs/${this.importCFAppType}`);
        const existing = await existRes.json();
        const existingNames = new Set((existing || []).map(c => c.name));
        // Also exclude TRaSH CFs (they're already in the browser)
        const trashRes = await fetch(`/api/trash/${this.importCFAppType}/cfs`);
        const trashCFs = await trashRes.json();
        const trashNames = new Set((trashCFs || []).map(c => c.name));

        this.importCFList = arrCFs
          .filter(cf => !trashNames.has(cf.name))  // skip TRaSH CFs
          .map(cf => ({
            name: cf.name,
            arrId: cf.id,
            specifications: cf.specifications,
            selected: false,
            exists: existingNames.has(cf.name),
          }))
          .sort((a, b) => a.name.localeCompare(b.name));
      } catch (e) {
        console.error('Failed to fetch CFs:', e);
      } finally {
        this.importCFLoading = false;
      }
    },

    async doImportCFs() {
      this.importCFResult = null;
      this.importCFJsonError = '';
      const category = this.importCFCategory === '' ? this.importCFNewCategory.trim() : this.importCFCategory;
      if (!category) {
        this.importCFResult = { error: true, message: 'Please enter a category name' };
        return;
      }

      this.importCFImporting = true;
      try {
        if (this.importCFSource === 'instance') {
          const selected = this.importCFList.filter(c => c.selected && !c.exists);
          if (selected.length === 0) {
            this.importCFResult = { error: true, message: 'No CFs selected' };
            return;
          }
          const res = await fetch('/api/custom-cfs/import-from-instance', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({
              instanceId: this.importCFInstanceId,
              cfNames: selected.map(c => c.name),
              category: category,
              appType: this.importCFAppType,
            }),
          });
          const result = await res.json();
          if (!res.ok) {
            this.importCFResult = { error: true, message: result.error || 'Import failed' };
            return;
          }
          this.importCFResult = { error: false, message: `Imported ${result.added} CF(s)${result.skipped > 0 ? ` (${result.skipped} skipped as duplicates)` : ''}` };
          // Mark imported CFs as existing
          for (const cf of this.importCFList) {
            if (cf.selected) cf.exists = true;
          }
        } else {
          // JSON import
          let parsed;
          try {
            parsed = JSON.parse(this.importCFJsonText);
          } catch (e) {
            this.importCFJsonError = 'Invalid JSON: ' + e.message;
            return;
          }
          // Accept both single CF and array
          if (!Array.isArray(parsed)) parsed = [parsed];
          const cfs = parsed.map(cf => ({
            name: cf.name || 'Unnamed CF',
            appType: this.importCFAppType,
            category: category,
            specifications: cf.specifications || [],
          }));

          const res = await fetch('/api/custom-cfs', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ cfs }),
          });
          const result = await res.json();
          if (!res.ok) {
            this.importCFResult = { error: true, message: result.error || 'Import failed' };
            return;
          }
          this.importCFResult = { error: false, message: `Imported ${result.added} CF(s)` };
        }
        // Refresh CF browse data
        this.loadCFBrowse(this.importCFAppType);
      } catch (e) {
        this.importCFResult = { error: true, message: 'Error: ' + e.message };
      } finally {
        this.importCFImporting = false;
      }
    },

    getSelectedQS(appType) {
      const all = this.getQualitySizes(appType);
      const idx = this.selectedQSType[appType] || 0;
      return all[idx]?.qualities || [];
    },

    qsBarStyle(qs, appType) {
      const allQs = this.getSelectedQS(appType || this.activeAppType);
      const maxMin = Math.max(...allQs.map(q => q.min), 1);
      const pct = Math.min(100, (qs.min / maxMin) * 100);
      return `width:${Math.max(2, pct)}%`;
    },

    // --- Quality Size Sync ---

    async loadInstanceQS(appType, instanceId) {
      if (!instanceId) {
        this.qsInstanceDefs = { ...this.qsInstanceDefs, [appType]: null };
        this.qsOverrides = { ...this.qsOverrides, [appType]: {} };
        this.qsAutoSync = { ...this.qsAutoSync, [appType]: { enabled: false, type: '' } };
        return;
      }
      try {
        const [defsR, overR, asR] = await Promise.all([
          fetch(`/api/instances/${instanceId}/quality-sizes`),
          fetch(`/api/instances/${instanceId}/quality-sizes/overrides`),
          fetch(`/api/instances/${instanceId}/quality-sizes/auto-sync`)
        ]);
        if (defsR.ok) {
          this.qsInstanceDefs = { ...this.qsInstanceDefs, [appType]: await defsR.json() };
        }
        if (overR.ok) {
          this.qsOverrides = { ...this.qsOverrides, [appType]: await overR.json() };
        }
        if (asR.ok) {
          const as = await asR.json();
          this.qsAutoSync = { ...this.qsAutoSync, [appType]: as };
          // Auto-select the type tab that matches the configured auto-sync type
          if (as.enabled && as.type) {
            const allQS = this.getQualitySizes(appType);
            const idx = allQS.findIndex(q => q.type === as.type);
            if (idx >= 0 && this.selectedQSType[appType] === undefined) {
              this.selectedQSType = { ...this.selectedQSType, [appType]: idx };
            }
          }
        }
      } catch (e) { console.error('loadInstanceQS:', e); }
    },

    _findInstanceDef(appType, qualityName) {
      const defs = this.qsInstanceDefs[appType];
      if (!defs) return null;
      return defs.find(d => d.quality?.name === qualityName || d.title === qualityName) || null;
    },

    getInstanceQSVal(appType, qualityName, field) {
      const def = this._findInstanceDef(appType, qualityName);
      if (!def) return '-';
      const map = { min: 'minSize', preferred: 'preferredSize', max: 'maxSize' };
      const val = def[map[field]] ?? 0;
      return val.toFixed(1);
    },

    qsCellStyle(appType, trashQS, field) {
      const def = this._findInstanceDef(appType, trashQS.quality);
      if (!def) return 'color:#aaa';
      const map = { min: 'minSize', preferred: 'preferredSize', max: 'maxSize' };
      const current = def[map[field]] ?? 0;
      const target = this._qsTargetVal(appType, trashQS, field);
      if (Math.abs(current - target) < 0.05) return 'color:#3fb950'; // match
      return 'color:#d29922'; // diff
    },

    _qsTargetVal(appType, trashQS, field) {
      const overrides = this.qsOverrides[appType] || {};
      const ov = overrides[trashQS.quality];
      if (ov) return ov[field];
      return trashQS[field];
    },

    _defFieldVal(def, field) {
      const map = { min: 'minSize', preferred: 'preferredSize', max: 'maxSize' };
      return def[map[field]] ?? 0;
    },

    qsRowStyle(appType, qs) {
      if (!this.qsInstanceId[appType]) return '';
      const def = this._findInstanceDef(appType, qs.quality);
      if (!def) return '';
      const allMatch = ['min', 'preferred', 'max'].every(f =>
        Math.abs(this._defFieldVal(def, f) - this._qsTargetVal(appType, qs, f)) < 0.05
      );
      return allMatch ? '' : 'background:#1c1f26';
    },

    isQSCustom(appType, qualityName) {
      const overrides = this.qsOverrides[appType] || {};
      return !!overrides[qualityName];
    },

    toggleQSMode(appType, qualityName) {
      const overrides = { ...(this.qsOverrides[appType] || {}) };
      if (overrides[qualityName]) {
        delete overrides[qualityName];
      } else {
        // Default to current instance values; fall back to TRaSH when instance value is 0 (not set)
        const def = this._findInstanceDef(appType, qualityName);
        const trashQS = this.getSelectedQS(appType).find(q => q.quality === qualityName);
        if (def) {
          overrides[qualityName] = {
            min: def.minSize || trashQS?.min || 0,
            preferred: def.preferredSize || trashQS?.preferred || 0,
            max: def.maxSize || trashQS?.max || 0
          };
        } else if (trashQS) {
          overrides[qualityName] = { min: trashQS.min, preferred: trashQS.preferred, max: trashQS.max };
        }
      }
      this.qsOverrides = { ...this.qsOverrides, [appType]: overrides };
      this._saveQSOverrides(appType);
    },

    getQSOverrideVal(appType, qualityName, field, fallback) {
      const overrides = this.qsOverrides[appType] || {};
      const ov = overrides[qualityName];
      return ov ? ov[field] : fallback;
    },

    setQSOverrideVal(appType, qualityName, field, value) {
      const overrides = { ...(this.qsOverrides[appType] || {}) };
      if (!overrides[qualityName]) return;
      overrides[qualityName] = { ...overrides[qualityName], [field]: value };
      this.qsOverrides = { ...this.qsOverrides, [appType]: overrides };
      this._saveQSOverrides(appType);
    },

    async _saveQSOverrides(appType) {
      const instanceId = this.qsInstanceId[appType];
      if (!instanceId) return;
      try {
        await fetch(`/api/instances/${instanceId}/quality-sizes/overrides`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(this.qsOverrides[appType] || {})
        });
      } catch (e) { console.error('saveQSOverrides:', e); }
    },

    async toggleQSAutoSync(appType, enabled, inputEl) {
      const instanceId = this.qsInstanceId[appType];
      if (!instanceId) return;

      if (enabled) {
        const syncCount = this.qsChangeCount(appType);
        const inst = this.instancesOfType(appType).find(i => i.id === instanceId);
        const instName = inst?.name || 'instance';
        const msg = syncCount > 0
          ? `${syncCount} Auto-mode qualities on ${instName} will be updated to TRaSH values immediately.\n\nCustom-mode qualities will not be changed.\nFuture TRaSH pulls will also sync automatically.\n\nMake sure you have set the correct mode (Auto/Custom) per quality before enabling.`
          : `All Auto-mode values on ${instName} currently match TRaSH.\n\nFuture TRaSH pulls will sync automatically.\nCustom-mode qualities will not be changed.`;

        // Show custom confirm modal
        if (inputEl) inputEl.checked = false; // revert until confirmed
        this.confirmModal = {
          show: true,
          title: 'Enable Auto-sync',
          message: msg,
          confirmLabel: syncCount > 0 ? `Sync ${syncCount} now & enable` : 'Enable',
          onConfirm: async () => { await this._applyQSAutoSync(appType, true); },
          onCancel: null
        };
        return;
      }

      await this._applyQSAutoSync(appType, false);
    },

    async _applyQSAutoSync(appType, enabled) {
      const instanceId = this.qsInstanceId[appType];
      if (!instanceId) return;

      const allQS = this.getQualitySizes(appType);
      const idx = this.selectedQSType[appType] || 0;
      const qsType = allQS[idx]?.type || '';

      const as = { enabled, type: qsType };
      this.qsAutoSync = { ...this.qsAutoSync, [appType]: as };
      try {
        await fetch(`/api/instances/${instanceId}/quality-sizes/auto-sync`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(as)
        });
      } catch (e) { console.error('_applyQSAutoSync:', e); }

      // Sync immediately when enabling
      if (enabled && this.qsChangeCount(appType) > 0) {
        await this.syncQualitySizes(appType);
      }
    },

    _qsDiffers(def, appType, qs, field) {
      return Math.abs(this._defFieldVal(def, field) - this._qsTargetVal(appType, qs, field)) >= 0.05;
    },

    qsChangeCount(appType) {
      const trashQualities = this.getSelectedQS(appType);
      const defs = this.qsInstanceDefs[appType];
      if (!defs || !trashQualities.length) return 0;
      let count = 0;
      for (const qs of trashQualities) {
        const def = this._findInstanceDef(appType, qs.quality);
        if (!def) continue;
        if (['min', 'preferred', 'max'].some(f => this._qsDiffers(def, appType, qs, f))) {
          count++;
        }
      }
      return count;
    },

    qsHasRowChange(appType, qs) {
      const def = this._findInstanceDef(appType, qs.quality);
      if (!def) return false;
      return ['min', 'preferred', 'max'].some(f => this._qsDiffers(def, appType, qs, f));
    },

    async syncSingleQS(appType, qualityName) {
      const instanceId = this.qsInstanceId[appType];
      if (!instanceId) return;
      const qs = this.getSelectedQS(appType).find(q => q.quality === qualityName);
      if (!qs) return;
      const def = this._findInstanceDef(appType, qualityName);
      if (!def) return;

      const target = {
        min: this._qsTargetVal(appType, qs, 'min'),
        preferred: this._qsTargetVal(appType, qs, 'preferred'),
        max: this._qsTargetVal(appType, qs, 'max')
      };

      try {
        const r = await fetch(`/api/instances/${instanceId}/quality-sizes/sync`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ definitions: [{ ...def, minSize: target.min, preferredSize: target.preferred, maxSize: target.max }] })
        });
        if (r.ok) {
          this.qsSyncResult = { ...this.qsSyncResult, [appType]: { ok: true, message: `Synced ${qualityName}` } };
          await this.loadInstanceQS(appType, instanceId);
        } else {
          const err = await r.json().catch(() => ({}));
          this.qsSyncResult = { ...this.qsSyncResult, [appType]: { ok: false, message: err.error || `Failed to sync ${qualityName}` } };
        }
      } catch (e) {
        this.qsSyncResult = { ...this.qsSyncResult, [appType]: { ok: false, message: e.message } };
      }
    },

    async syncQualitySizes(appType) {
      const instanceId = this.qsInstanceId[appType];
      if (!instanceId) return;
      this.qsSyncing = { ...this.qsSyncing, [appType]: true };
      this.qsSyncResult = { ...this.qsSyncResult, [appType]: null };

      try {
        const trashQualities = this.getSelectedQS(appType);
        const defs = this.qsInstanceDefs[appType];
        const updated = [];

        for (const qs of trashQualities) {
          const def = this._findInstanceDef(appType, qs.quality);
          if (!def) continue;
          const target = {
            min: this._qsTargetVal(appType, qs, 'min'),
            preferred: this._qsTargetVal(appType, qs, 'preferred'),
            max: this._qsTargetVal(appType, qs, 'max')
          };
          if (this._qsDiffers(def, appType, qs, 'min') ||
              this._qsDiffers(def, appType, qs, 'preferred') ||
              this._qsDiffers(def, appType, qs, 'max')) {
            updated.push({ ...def, minSize: target.min, preferredSize: target.preferred, maxSize: target.max });
          }
        }

        if (updated.length === 0) {
          this.qsSyncResult = { ...this.qsSyncResult, [appType]: { ok: true, message: 'All values already match' } };
          return;
        }

        const r = await fetch(`/api/instances/${instanceId}/quality-sizes/sync`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ definitions: updated })
        });

        if (r.ok) {
          this.qsSyncResult = { ...this.qsSyncResult, [appType]: { ok: true, message: `Synced ${updated.length} quality sizes` } };
          await this.loadInstanceQS(appType, instanceId);
        } else {
          const err = await r.json().catch(() => ({}));
          this.qsSyncResult = { ...this.qsSyncResult, [appType]: { ok: false, message: err.error || 'Sync failed' } };
        }
      } catch (e) {
        this.qsSyncResult = { ...this.qsSyncResult, [appType]: { ok: false, message: e.message } };
      } finally {
        this.qsSyncing = { ...this.qsSyncing, [appType]: false };
      }
    },

    instancesOfType(type) {
      return this.instances.filter(i => i.type === type).sort((a, b) => a.name.localeCompare(b.name));
    },

    // --- Cleanup ---
    cleanupActionLabel(action) {
      const labels = {
        'duplicates': 'Duplicate Custom Formats',
        'delete-cfs-keep-scores': 'Delete All CFs (Keep Scores)',
        'delete-cfs-and-scores': 'Delete All CFs & Scores',
        'reset-unsynced-scores': 'Reset Non-Synced Scores',
        'orphaned-scores': 'Orphaned Scores',
      };
      return labels[action] || action;
    },

    async cleanupScan(action) {
      if (!this.cleanupInstanceId) return;
      this.cleanupScanning = true;
      try {
        const resp = await fetch('/api/instances/' + this.cleanupInstanceId + '/cleanup/scan', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ action, keep: this.cleanupKeepList }),
        });
        if (!resp.ok) {
          const err = await resp.json().catch(() => ({}));
          this.showToast(err.error || 'Scan failed', 'error', 8000);
          return;
        }
        this.cleanupResult = await resp.json();
      } catch (e) {
        this.showToast('Scan failed: ' + e.message, 'error', 8000);
      } finally {
        this.cleanupScanning = false;
      }
    },

    async cleanupApply() {
      if (!this.cleanupResult?.items?.length) return;
      this.cleanupApplying = true;
      try {
        const ids = this.cleanupResult.items.map(i => i.id);
        const resp = await fetch('/api/instances/' + this.cleanupResult.instanceId + '/cleanup/apply', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ action: this.cleanupResult.action, ids }),
        });
        if (!resp.ok) {
          const err = await resp.json().catch(() => ({}));
          this.cleanupResult = { ...this.cleanupResult, applied: false, applyError: err.error || 'Apply failed' };
          return;
        }
        const result = await resp.json();
        this.cleanupResult = { ...this.cleanupResult, applied: true, applyResult: result };
      } catch (e) {
        this.cleanupResult = { ...this.cleanupResult, applied: false, applyError: e.message };
      } finally {
        this.cleanupApplying = false;
      }
    },

    async loadCleanupKeep() {
      if (!this.cleanupInstanceId) { this.cleanupKeepList = []; return; }
      try {
        const resp = await fetch('/api/instances/' + this.cleanupInstanceId + '/cleanup/keep');
        if (resp.ok) this.cleanupKeepList = await resp.json();
      } catch (e) { this.cleanupKeepList = []; }
    },
    async saveCleanupKeep() {
      if (!this.cleanupInstanceId) return;
      await fetch('/api/instances/' + this.cleanupInstanceId + '/cleanup/keep', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(this.cleanupKeepList),
      });
    },
    async loadCleanupCFNames() {
      if (!this.cleanupInstanceId) { this.cleanupCFNames = []; return; }
      try {
        const resp = await fetch('/api/instances/' + this.cleanupInstanceId + '/cfs');
        if (resp.ok) {
          const cfs = await resp.json();
          this.cleanupCFNames = (cfs || []).map(cf => cf.name).sort();
        }
      } catch (e) { this.cleanupCFNames = []; }
    },
    addCleanupKeepName(name) {
      if (!name) return;
      if (this.cleanupKeepList.some(n => n.toLowerCase() === name.toLowerCase())) {
        this.cleanupKeepInput = '';
        this.cleanupKeepSuggestions = [];
        return;
      }
      this.cleanupKeepList.push(name);
      this.cleanupKeepInput = '';
      this.cleanupKeepSuggestions = [];
      this.saveCleanupKeep();
    },
    async addCleanupKeep() {
      const name = this.cleanupKeepInput.trim();
      if (!name) return;
      if (this.cleanupKeepList.some(n => n.toLowerCase() === name.toLowerCase())) {
        this.cleanupKeepInput = '';
        return;
      }
      this.cleanupKeepList.push(name);
      this.cleanupKeepInput = '';
      await this.saveCleanupKeep();
    },
    async addAllMatchingKeep() {
      const query = this.cleanupKeepInput.trim().toLowerCase();
      if (!query) return;
      const matching = this.cleanupCFNames.filter(n =>
        n.toLowerCase().includes(query) && !this.cleanupKeepList.some(k => k.toLowerCase() === n.toLowerCase())
      );
      if (matching.length === 0) return;
      this.cleanupKeepList.push(...matching);
      this.cleanupKeepInput = '';
      this.cleanupKeepSuggestions = [];
      await this.saveCleanupKeep();
      this.showToast(`Added ${matching.length} CFs to Keep List`, 'info', 3000);
    },

    async removeCleanupKeep(idx) {
      this.cleanupKeepList.splice(idx, 1);
      await this.saveCleanupKeep();
    },

    instanceIconUrl(inst) {
      const is4k = /4k|uhd/i.test(inst.name);
      if (inst.type === 'radarr') return is4k ? '/icons/radarr4kNew.png' : '/icons/radarrNew.png';
      return is4k ? '/icons/sonarr4k.png' : '/icons/sonarr.png';
    },

    trashProfileCount(type) {
      return (this.trashProfiles[type] || []).length;
    },

    groupedProfiles(type) {
      // TRaSH convention: sort cards by the `group` integer from profile.json
      // (ascending), then alpha by card name as tiebreak. A card can contain
      // profiles with different group ints (Standard has 1 and 2); use the
      // minimum as the card's sort key so it still lands in the right slot.
      // User-created "Other" profiles without a group int drift to the end.
      const profiles = this.trashProfiles[type] || [];
      const groups = {};
      for (const p of profiles) {
        const g = p.groupName || 'Other';
        if (!groups[g]) groups[g] = { name: g, profiles: [], minGroup: Infinity };
        groups[g].profiles.push(p);
        const gnum = typeof p.group === 'number' ? p.group : Infinity;
        if (gnum < groups[g].minGroup) groups[g].minGroup = gnum;
      }
      // Alpha-sort profiles within each card so order doesn't depend on
      // whatever filesystem read order /api/trash/{app}/profiles happened
      // to return. Matches the CF Group Builder's within-card sort.
      for (const g of Object.values(groups)) {
        g.profiles.sort((a, b) => a.name.localeCompare(b.name));
      }
      return Object.values(groups).sort((a, b) => {
        if (a.minGroup !== b.minGroup) return a.minGroup - b.minGroup;
        return a.name.localeCompare(b.name);
      });
    },

    toggleInstance(id) {
      const opening = !this.expandedInstances[id];
      this.expandedInstances = { ...this.expandedInstances, [id]: opening };
      if (opening) {
        this.loadSyncHistory(id);
        // Auto-load profiles on expand if not already loaded
        if (!this.instProfiles[id]) {
          const inst = this.instances.find(i => i.id === id);
          if (inst) this.loadInstanceProfiles(inst);
        }
      }
    },

    // Profile group collapse/expand
    toggleProfileGroup(instId, groupName) {
      const key = instId + ':' + groupName;
      this.expandedProfileGroups = { ...this.expandedProfileGroups, [key]: !this.expandedProfileGroups[key] };
    },

    isProfileGroupExpanded(instId, groupName) {
      const key = instId + ':' + groupName;
      return !!this.expandedProfileGroups[key];
    },

    toggleQSExpanded(instId) {
      this.qsExpanded = { ...this.qsExpanded, [instId]: !this.qsExpanded[instId] };
    },

    switchTab(tab) {
      this.debugLog('UI', `Tab: ${tab}`);
      this.currentTab = tab;
      localStorage.setItem('clonarr_tab', tab);
      this.profileDetail = null;
      this.syncPlan = null;
      this.syncResult = null;
      // Auto-select maintenance instance for this tab type if only one
      const typeInsts = this.instances.filter(i => i.type === tab);
      if (typeInsts.length === 1 && this.maintenanceInstanceId !== typeInsts[0].id) {
        this.maintenanceInstanceId = typeInsts[0].id;
        this.cleanupInstanceId = typeInsts[0].id;
        this.loadCleanupKeep();
        this.loadCleanupCFNames();
      }
    },

    switchSection(section) {
      this.debugLog('UI', `Section: ${section}`);
      this.currentSection = section;
      localStorage.setItem('clonarr_section', section);
      this.profileDetail = null;
      this.syncPlan = null;
      this.syncResult = null;
      this.pushNav();
    },

    switchAppType(appType) {
      // Guard unsaved CF Group Builder work: the builder is app-type-scoped,
      // so switching triggers cfgbLoad → cfgbReset which would discard an
      // in-flight edit. Warn via the styled confirm modal (browser's native
      // confirm() was jarring and didn't match the rest of the app).
      const shouldPrompt = this.currentSection === 'advanced'
        && this.advancedTab === 'group-builder'
        && appType !== this.activeAppType
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
      // Reload tab-scoped data that depends on appType. The CF Group Builder
      // pulls CFs, profiles, and saved groups per Radarr/Sonarr — without this
      // the Radarr list keeps showing when the user flips to Sonarr.
      // Scoring Sandbox has the same issue; reload it too for parity.
      if (this.currentSection === 'advanced') {
        if (this.advancedTab === 'group-builder') this.cfgbLoad(appType);
        else if (this.advancedTab === 'scoring') this.loadSandbox(appType);
      }
    },

    // --- Browser History API (back/forward navigation) ---
    // Hash format: #appType/section[/subtab] — e.g. #radarr/profiles/compare, #settings/prowlarr, #about
    _navSkipPush: false,

    buildNavHash() {
      const s = this.currentSection;
      if (s === 'settings') return '#settings/' + (this.settingsSection || 'instances');
      if (s === 'about') return '#about';
      const app = this.activeAppType;
      let hash = '#' + app + '/' + s;
      if (s === 'profiles') hash += '/' + (this.getProfileTab(app) || 'trash-sync');
      else if (s === 'advanced') hash += '/' + (this.advancedTab || 'builder');
      return hash;
    },

    pushNav() {
      if (this._navSkipPush) return;
      const hash = this.buildNavHash();
      if (location.hash !== hash) history.pushState(null, '', hash);
    },

    restoreFromHash(hash) {
      if (!hash || hash === '#') return false;
      const parts = hash.replace(/^#/, '').split('/');
      const validSections = ['profiles','custom-formats','quality-size','naming','maintenance','advanced','settings','about'];
      const validSettings = ['instances','trash','prowlarr','notifications','display','advanced'];
      const validProfileTabs = ['trash-sync','history','compare'];
      const validAdvancedTabs = ['builder','scoring','import'];
      this._navSkipPush = true;
      try {
        if (parts[0] === 'settings') {
          this.currentSection = 'settings';
          if (parts[1] && validSettings.includes(parts[1])) this.settingsSection = parts[1];
        } else if (parts[0] === 'about') {
          this.currentSection = 'about';
        } else {
          const appType = parts[0];
          if (appType === 'radarr' || appType === 'sonarr') this.activeAppType = appType;
          if (parts[1] && validSections.includes(parts[1])) this.currentSection = parts[1];
          else return false;
          if (parts[2]) {
            if (this.currentSection === 'profiles' && validProfileTabs.includes(parts[2])) this.setProfileTab(this.activeAppType, parts[2]);
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
      return this.profileTabs[appType] || 'trash-sync';
    },

    setProfileTab(appType, tab) {
      this.profileTabs = { ...this.profileTabs, [appType]: tab };
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

    // --- Instance CRUD ---

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
      // Reload sync data in case orphaned data was migrated
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
      // Clear cached status for deleted instance
      const { [inst.id]: _, ...restStatus } = this.instanceStatus;
      this.instanceStatus = restStatus;
      await this.loadInstances();
    },

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

    async testAllInstances() {
      for (const inst of this.instances) {
        this.testInstance(inst);
      }
      // Also test Prowlarr if configured
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
          // Use saved instance endpoint (has real API key)
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

    // --- Notifications ---

    getAllSelectedCFIds() {
      const ids = this.getSelectedCFIds();
      const extraIds = Object.keys(this.extraCFs);
      return extraIds.length > 0 ? [...ids, ...extraIds] : ids;
    },

    resolveCFName(tid) {
      const detail = this.profileDetail?.detail;
      if (!detail) return tid.substring(0, 12);
      for (const fi of (detail.formatItemNames || [])) {
        if (fi.trashId === tid) return fi.name;
      }
      for (const g of (detail.trashGroups || [])) {
        for (const cf of g.cfs) {
          if (cf.trashId === tid) return cf.name;
        }
      }
      // Fallback: extras and other CFs loaded via /all-cfs (Extra CFs picker list)
      for (const cf of (this.extraCFAllCFs || [])) {
        if (cf.trashId === tid) return cf.name;
      }
      return tid.replace(/^custom:/, '').substring(0, 12);
    },

    resolveCFDefaultScore(tid) {
      const detail = this.profileDetail?.detail;
      if (!detail) return '?';
      for (const fi of (detail.formatItemNames || [])) {
        if (fi.trashId === tid) return fi.score ?? 0;
      }
      for (const g of (detail.trashGroups || [])) {
        for (const cf of g.cfs) {
          if (cf.trashId === tid) return cf.score ?? 0;
        }
      }
      // Fallback: extras — resolve default score from CF's trashScores map using current score set
      const scoreSet = detail.scoreCtx || detail.profile?.trashScoreSet || 'default';
      for (const cf of (this.extraCFAllCFs || [])) {
        if (cf.trashId === tid) {
          return cf.trashScores?.[scoreSet] ?? cf.trashScores?.default ?? 0;
        }
      }
      return '?';
    },

    resolveExtraCFName(tid) {
      // Fallback name resolver for extra CFs not found in extraCFAllCFs.
      // Checks instance CF names (for CFs synced to Arr but not in TRaSH groups).
      if (this._extraCFNameCache?.[tid]) return this._extraCFNameCache[tid];
      const instCFs = this.cleanupCFNames || [];
      // Custom CFs: try to find by partial ID match in extraCFAllCFs or instance
      for (const cf of (this.extraCFAllCFs || [])) {
        if (cf.trashId === tid) { return cf.name; }
      }
      return tid.replace(/^custom:/, '').substring(0, 12);
    },

    async loadExtraCFList() {
      const t = this.profileDetail?.instance?.type;
      if (!t) return;
      try {
        const r = await fetch(`/api/trash/${t}/all-cfs`);
        if (!r.ok) return;
        const d = await r.json();
        // Build grouped + ungrouped lists
        const groups = []; // { name, cfs[] }
        const ungrouped = [];
        for (const c of (d.categories || [])) for (const g of c.groups) {
          if (g.groupTrashId) {
            groups.push({ name: g.name, cfs: g.cfs });
          } else {
            for (const cf of g.cfs) ungrouped.push(cf);
          }
        }
        if (ungrouped.length > 0) groups.push({ name: 'Other', cfs: ungrouped });
        this.extraCFGroups = groups;
        // Ensure all groups start collapsed (Alpine's reactive proxy can make missing keys truthy otherwise)
        for (const g of groups) this.detailSections['extra_' + g.name] = false;
        // Flat list for filtering
        const all = [];
        for (const g of groups) for (const cf of g.cfs) all.push(cf);
        this.extraCFAllCFs = all;
      } catch (e) { console.error('loadExtraCFs:', e); }
    },

    _extraInProfile(trashId) {
      if (!this._extraInProfileSet) {
        this._extraInProfileSet = new Set();
        for (const fi of (this.profileDetail?.detail?.formatItemNames || [])) this._extraInProfileSet.add(fi.trashId);
        for (const g of (this.profileDetail?.detail?.trashGroups || [])) for (const cf of g.cfs) this._extraInProfileSet.add(cf.trashId);
      }
      return this._extraInProfileSet.has(trashId);
    },

    extraCFAvailable() {
      const q = (this.extraCFSearch || '').toLowerCase();
      const inProfile = new Set();
      for (const fi of (this.profileDetail?.detail?.formatItemNames || [])) inProfile.add(fi.trashId);
      for (const g of (this.profileDetail?.detail?.trashGroups || [])) {
        for (const cf of g.cfs) inProfile.add(cf.trashId);
      }
      return this.extraCFAllCFs.filter(cf =>
        !inProfile.has(cf.trashId) && !this.extraCFs[cf.trashId] && (!q || cf.name.toLowerCase().includes(q))
      );
    },

    async toggleAdvancedMode(enable) {
      if (enable) {
        const ok = await new Promise(resolve => {
          this.confirmModal = {
            show: true,
            title: 'Enable Advanced Mode',
            html: true,
            message: 'Advanced Mode enables tools for power users and guide contributors:<br><br>• Profile Builder — create custom profiles with fixed scores <span style="color:#f85149;font-weight:600">(no auto-sync — scores will NOT update when TRaSH Guides change)</span><br>• Scoring Sandbox — test how releases score against profiles<br>• TRaSH JSON export — for contributing to TRaSH Guides<br><br><strong style="color:#d29922;font-size:14px">Most users don\'t need this.</strong> TRaSH Sync handles profiles, scores, and updates automatically. Only enable Advanced Mode if you have a specific need that TRaSH Sync doesn\'t cover.<br><br>Enable Advanced Mode?',
            onConfirm: () => resolve(true),
            onCancel: () => resolve(false)
          };
        });
        if (!ok) return;
      }
      this.config.devMode = enable;
      this.saveConfig(['devMode']);
    },

    setUIScale(value) {
      this.uiScale = value;
      localStorage.setItem('clonarr-ui-scale', value);
      document.documentElement.style.zoom = value;
    },

    showToast(message, type = 'info', duration = 8000) {
      const id = Date.now() + Math.random();
      this.toasts = [...this.toasts, { id, message, type, duration }];
      setTimeout(() => { this.toasts = this.toasts.filter(t => t.id !== id); }, duration);
    },

    async checkCleanupEvents() {
      try {
        const r = await fetch('/api/cleanup-events');
        if (!r.ok) return;
        const events = await r.json();
        for (const ev of events) {
          this.showToast(`"${ev.profileName}" — deleted in ${ev.instanceName}, sync rule removed`, 'warning', 6000);
        }
      } catch (e) { /* ignore */ }
    },

    async checkAutoSyncEvents() {
      try {
        const r = await fetch('/api/auto-sync/events');
        if (!r.ok) return;
        const events = await r.json();
        for (let i = 0; i < events.length; i++) {
          const ev = events[i];
          setTimeout(() => {
            if (ev.error) {
              this.showToast(`Auto-sync failed: ${ev.instanceName} — ${ev.profileName}: ${ev.error}`, 'error', 8000);
            } else {
              const profileLabel = ev.arrProfileName && ev.arrProfileName !== ev.profileName
                ? `${ev.profileName} → ${ev.arrProfileName}` : ev.profileName;
              let msg = `Auto-sync: ${ev.instanceName} — "${profileLabel}"`;
              if (ev.details?.length > 0) {
                msg += '\n' + ev.details.join('\n');
              }
              this.showToast(msg, 'info', 8000);
            }
          }, i * 3000);
        }
        // Reload sync history if any events came through
        if (events.length > 0) {
          for (const inst of this.instances) {
            await this.loadSyncHistory(inst.id);
          }
        }
      } catch (e) { /* ignore */ }
    },

    // Find the instance that has sync history for an imported profile
    findSyncedInstance(appType, importedProfileId) {
      for (const inst of this.instancesOfType(appType)) {
        const history = (this.syncHistory[inst.id] || []).find(h => h.importedProfileId === importedProfileId);
        if (history) return { inst, history };
      }
      // Fallback to first instance
      const inst = this.instancesOfType(appType)[0];
      return inst ? { inst, history: null } : null;
    },

    // --- TRaSH ---

    async pullTrash() {
      this.pulling = true;
      const prevCommit = this.trashStatus?.commitHash || '';
      try {
        const r = await fetch('/api/trash/pull', { method: 'POST' });
        if (!r.ok) { this.pulling = false; this.showToast('Pull failed', 'error'); return; }
        const poll = setInterval(async () => {
          await this.loadTrashStatus();
          if (!this.trashStatus.pulling) {
            clearInterval(poll);
            this.pulling = false;
            // Show toast based on pull result
            if (this.trashStatus.pullError) {
              this.showToast('Pull failed: ' + this.trashStatus.pullError, 'error');
            } else if (this.trashStatus.commitHash !== prevCommit && this.trashStatus.lastDiff?.summary) {
              const summary = this.trashStatus.lastDiff.summary.replace(/\*\*/g, '').replace(/^\n/, '').replace(/\n/g, ', ').replace(/:,/g, ':');
              this.showToast('TRaSH updated: ' + summary, 'info', 10000);
            } else {
              this.showToast('TRaSH data up to date', 'info', 3000);
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
            // Reload sync data and check for cleanup events
            await this.loadAutoSyncRules();
            for (const inst of this.instances) {
              await this.loadSyncHistory(inst.id);
            }
            this.checkCleanupEvents();
            // Delay auto-sync event check — auto-sync runs async after pull completes
            setTimeout(() => this.checkAutoSyncEvents(), 5000);
          }
        }, 2000);
        setTimeout(() => { clearInterval(poll); this.pulling = false; }, 120000);
      } catch (e) {
        this.pulling = false;
        this.showToast('Pull failed: ' + e.message, 'error');
      }
    },

    // --- Profile Detail ---

    async openProfileDetail(inst, profile) {
      this.debugLog('UI', `Profile opened: "${profile.name}" on ${inst.name}`);
      this.syncPlan = null;
      this.syncResult = null;
      this.selectedOptionalCFs = {};

      this.showProfileInfo = false;
      this.profileDetail = { instance: inst, profile: profile, detail: null };
      // Pre-load languages and quality presets for this instance (for override dropdowns)
      this.getLanguagesForInstance(inst.id);
      if (!this.pbQualityPresets.length) {
        fetch(`/api/trash/${inst.type}/quality-presets`).then(r => r.ok ? r.json() : []).then(d => this.pbQualityPresets = d || []).catch(() => {});
      }
      try {
        const r = await fetch(`/api/trash/${inst.type}/profiles/${profile.trashId}`);
        if (!r.ok) { console.error('loadProfileDetail: HTTP', r.status); return; }
        const detail = await r.json();
        this.profileDetail = { ...this.profileDetail, detail: detail };
        this.initDetailSections(detail);
        this.initSelectedCFs(detail);
        // Reset profile-detail override state on every load. pdInitOverrides gets the TRaSH
        // profile when available; imported profiles (no detail.profile) still need the reset
        // so stale pdGeneralActive/pdQualityActive/etc. from a prior TRaSH profile don't leak.
        this.pdResetDetailState();
        this.pdInitOverrides(detail.profile || null);
      } catch (e) { console.error('loadProfileDetail:', e); }
    },

    initDetailSections(detail) {
      const sections = { core: false };
      const groups = {};
      for (const cat of (detail.cfCategories || [])) {
        sections[cat.category] = false;
      }
      this.detailSections = sections;
      this.groupExpanded = groups;
      this.cfDescExpanded = {};
    },

    initSelectedCFs(detail) {
      const selected = {};
      // Use trashGroups (new system) if available, fall back to cfCategories (legacy)
      if (detail.trashGroups?.length) {
        for (const group of detail.trashGroups) {
          if (group.defaultEnabled) {
            if (group.exclusive) {
              // Exclusive group: only enable the default CF
              for (const cf of (group.cfs || [])) {
                if (!cf.required) selected[cf.trashId] = !!cf.default;
              }
            } else {
              // Only set optional CFs individually; required CFs are handled by group state
              for (const cf of (group.cfs || [])) {
                if (!cf.required) selected[cf.trashId] = true;
              }
            }
          }
        }
      } else {
        for (const cat of (detail.cfCategories || [])) {
          for (const group of cat.groups) {
            if (group.defaultEnabled) {
              if (group.exclusive) {
                for (const cf of (group.cfs || [])) {
                  selected[cf.trashId] = !!cf.default;
                }
              } else {
                for (const cf of (group.cfs || [])) {
                  selected[cf.trashId] = true;
                }
              }
            }
          }
        }
      }
      this.selectedOptionalCFs = selected;
    },

    toggleDetailSection(section) {
      this.detailSections = { ...this.detailSections, [section]: !this.detailSections[section] };
    },

    toggleGroupExpanded(category, groupName) {
      const key = category + '/' + groupName;
      this.groupExpanded = { ...this.groupExpanded, [key]: !this.groupExpanded[key] };
    },

    // Returns conflicting CF trash_ids that should be deactivated when activating trashId
    getConflictingCFs(appType, trashId) {
      const conflicts = this.conflictsData[appType]?.custom_formats;
      if (!conflicts) return [];
      const conflicting = [];
      for (const group of conflicts) {
        if (group.some(cf => cf.trash_id === trashId)) {
          for (const cf of group) {
            if (cf.trash_id !== trashId) conflicting.push(cf.trash_id);
          }
        }
      }
      return conflicting;
    },

    toggleExclusiveCF(trashId, groupCFs, mustHaveOne = false) {
      const updated = { ...this.selectedOptionalCFs };
      const enabling = !updated[trashId];
      if (enabling) {
        // Radio behavior: activate this one, deactivate all others in group
        for (const cf of groupCFs) {
          updated[cf.trashId] = (cf.trashId === trashId);
        }
        // Also deactivate cross-group conflicts from conflicts.json
        const appType = this.profileDetail?.instance?.type || this.currentTab;
        for (const conflictId of this.getConflictingCFs(appType, trashId)) {
          updated[conflictId] = false;
        }
      } else if (mustHaveOne) {
        // Golden Rule: cannot deactivate the last active one
        updated[trashId] = true;
      } else {
        // Optional exclusive (e.g. SDR): allow deactivating all
        updated[trashId] = false;
      }
      this.selectedOptionalCFs = updated;
    },

    showCFTooltip(event, text) {
      clearTimeout(window._tooltipHideTimer);
      const el = document.getElementById('cf-tooltip-portal');
      if (!el) return;
      el.innerHTML = sanitizeHTML(text);
      el.style.display = 'block';
      const rect = event.target.getBoundingClientRect();
      // Position to the right of the icon, vertically centered
      const w = el.offsetWidth || 340;
      const h = el.offsetHeight;
      let x = rect.right + 12;
      // If not enough space on the right, try left
      if (x + w > window.innerWidth - 8) {
        x = rect.left - w - 12;
      }
      x = Math.max(8, x);
      let y = rect.top - 8;
      if (y + h > window.innerHeight - 8) {
        y = Math.max(8, window.innerHeight - h - 8);
      }
      el.style.left = x + 'px';
      el.style.top = y + 'px';
    },

    hideCFTooltip() {
      window._tooltipHideTimer = setTimeout(() => {
        const el = document.getElementById('cf-tooltip-portal');
        if (el) el.style.display = 'none';
      }, 200);
    },

    toggleOptionalCF(trashId) {
      const updated = { ...this.selectedOptionalCFs, [trashId]: !this.selectedOptionalCFs[trashId] };
      // If enabling, deactivate any conflicting CFs (cross-group conflicts from conflicts.json)
      if (updated[trashId]) {
        const appType = this.profileDetail?.instance?.type || this.currentTab;
        for (const conflictId of this.getConflictingCFs(appType, trashId)) {
          updated[conflictId] = false;
        }
      }
      this.selectedOptionalCFs = updated;
    },

    // --- Category helpers ---

    getCategoryClass(category) {
      const map = {
        'Golden Rule': 'cat-golden-rule',
        'Audio': 'cat-audio',
        'HDR Formats': 'cat-hdr',
        'HQ Release Groups': 'cat-hq-release-groups',
        'Resolution': 'cat-resolution',
        'Streaming Services': 'cat-streaming',
        'Miscellaneous': 'cat-miscellaneous',
        'Optional': 'cat-optional',
        'Unwanted': 'cat-unwanted',
        'Anime': 'cat-anime',
        'French Audio Version': 'cat-french',
        'French HQ Source Groups': 'cat-french',
        'German Source Groups': 'cat-german',
        'German Miscellaneous': 'cat-german',
        'Language Profiles': 'cat-language',
        'Movie Versions': 'cat-optional',
        'Release Groups': 'cat-hq-release-groups',
      };
      return map[category] || 'cat-other';
    },

    countCategoryCFs(cat) {
      let count = 0;
      for (const g of (cat.groups || [])) count += (g.cfs || []).length;
      return count;
    },

    countSelectedCategoryCFs(cat) {
      let count = 0;
      for (const g of (cat.groups || [])) {
        for (const cf of (g.cfs || [])) {
          if (this.selectedOptionalCFs[cf.trashId]) count++;
        }
      }
      return count;
    },

    countSelectedGroupCFs(catName, group) {
      let count = 0;
      for (const cf of (group.cfs || [])) {
        if (this.selectedOptionalCFs[cf.trashId]) count++;
      }
      return count;
    },

    countAllCategoryCFs() {
      // Use trashGroups if available
      const groups = this.profileDetail?.detail?.trashGroups || [];
      if (groups.length) {
        let count = 0;
        for (const group of groups) {
          const grpOn = this.selectedOptionalCFs['__grp_' + group.name] !== undefined
            ? this.selectedOptionalCFs['__grp_' + group.name] : group.defaultEnabled;
          if (grpOn) count += group.cfs.length;
        }
        return count;
      }
      // Legacy fallback
      let count = 0;
      for (const cat of (this.profileDetail?.detail?.cfCategories || [])) {
        count += this.countCategoryCFs(cat);
      }
      return count;
    },

    countGroupCFs(groups) {
      if (!groups) return 0;
      let count = 0;
      for (const g of groups) count += (g.cfs || []).length;
      return count;
    },

    // --- Instance Profile Compare ---

    async loadInstanceProfiles(inst) {
      this.instProfilesLoading = {...this.instProfilesLoading, [inst.id]: true};
      try {
        const r = await fetch(`/api/instances/${inst.id}/profiles`);
        if (!r.ok) return;
        const profiles = await r.json();
        this.instProfiles = {...this.instProfiles, [inst.id]: profiles};
      } catch (e) {
        console.error('loadInstanceProfiles:', e);
      } finally {
        this.instProfilesLoading = {...this.instProfilesLoading, [inst.id]: false};
      }
    },

    selectInstProfile(inst, arrProfile) {
      const current = this.instCompareProfile[inst.id];
      if (current === arrProfile.id) {
        // Toggle off
        this.instCompareProfile = {...this.instCompareProfile, [inst.id]: null};
        this.instCompareResult = {...this.instCompareResult, [inst.id]: null};
        this.instCompareTrashId = {...this.instCompareTrashId, [inst.id]: ''};
      } else {
        this.instCompareProfile = {...this.instCompareProfile, [inst.id]: arrProfile.id};
        this.instCompareResult = {...this.instCompareResult, [inst.id]: null};
        this.instCompareTrashId = {...this.instCompareTrashId, [inst.id]: ''};
      }
    },

    async runProfileCompare(inst, arrProfileId, trashProfileId) {
      this.debugLog('UI', `Compare: arr profile ${arrProfileId} vs TRaSH "${trashProfileId}" on ${inst.name}`);
      // Clear stale banner state from a previous Compare run — syncResult/syncPlan banners were
      // pinned to the prior profile via syncForm._fromCompare, and compareLastDryRunContext
      // captured the old (inst, cr, section). Without clearing, switching profile/instance
      // leaves those banners pointing at the previous target → Apply could sync the wrong profile.
      this.syncResult = null;
      this.syncPlan = null;
      this.compareLastDryRunContext = null;
      if (this.syncForm && this.syncForm._fromCompare) {
        this.syncForm = {...this.syncForm, _fromCompare: false, _pendingExtrasRemove: [], _compareArrProfileId: undefined};
      }
      this.instCompareTrashId = {...this.instCompareTrashId, [inst.id]: trashProfileId};
      if (!trashProfileId) {
        this.instCompareResult = {...this.instCompareResult, [inst.id]: null};
        return;
      }
      this.instCompareLoading = {...this.instCompareLoading, [inst.id]: true};
      try {
        const r = await fetch(`/api/instances/${inst.id}/compare?arrProfileId=${arrProfileId}&trashProfileId=${encodeURIComponent(trashProfileId)}`);
        if (!r.ok) {
          this.instCompareResult = {...this.instCompareResult, [inst.id]: {error: 'Failed to compare'}};
          return;
        }
        const result = await r.json();
        this.instCompareResult = {...this.instCompareResult, [inst.id]: result};
        if (!result.error) this.compareApplyDefaultSelections(result, inst.id);
      } catch (e) {
        console.error('runProfileCompare:', e);
        this.instCompareResult = {...this.instCompareResult, [inst.id]: {error: e.message}};
      } finally {
        this.instCompareLoading = {...this.instCompareLoading, [inst.id]: false};
      }
    },

    // Initial selection state for a compare result — represents "sync this to match TRaSH completely".
    // Called by runProfileCompare on first load and by the Reset-to-default button.
    // - formatItems (Required CFs): select all missing + wrong-score rows
    // - Non-exclusive groups: select required-missing + wrong-score rows
    // - Exclusive required groups (Golden Rule): pre-pick in-use variant, else TRaSH default; else leave empty (user is prompted via picker on sync)
    // - All Extra in Arr: pre-marked for removal
    // - Settings/Quality: maps cleared. The x-bind checkboxes default to true for diffs via `!== false` logic, so empty map == everything pre-checked.
    compareApplyDefaultSelections(result, instId) {
      const sel = {};
      for (const fi of (result.formatItems || [])) {
        if (!fi.exists || !fi.scoreMatch) sel[fi.trashId] = true;
      }
      for (const group of (result.groups || [])) {
        if (group.exclusive && group.defaultEnabled) {
          // Exclusive required: always pick exactly one variant. Priority:
          //  1. In-use variant (preserve user's existing choice)
          //  2. TRaSH-recommended (cf.default && cf.required)
          //  3. TRaSH-recommended (cf.default alone)
          //  4. First variant in group (fallback — should never be needed)
          // Guarantee: group always has exactly one picked, so no picker modal needed.
          const inUseCF = group.cfs.find(cf => cf.exists && cf.inUse);
          const defaultCF = group.cfs.find(cf => cf.default && cf.required) || group.cfs.find(cf => cf.default);
          const pick = inUseCF || defaultCF || group.cfs[0];
          if (pick) sel[pick.trashId] = true;
          continue;
        }
        for (const cf of group.cfs) {
          if (!cf.exists && group.defaultEnabled && cf.required) sel[cf.trashId] = true;
          else if (cf.exists && !cf.scoreMatch) sel[cf.trashId] = true;
        }
      }
      this.instCompareSelected = {...this.instCompareSelected, [instId]: sel};
      // All extras in Arr: pre-marked for removal (match-TRaSH default)
      const extraSel = {};
      for (const ecf of (result.extraCFs || [])) extraSel[ecf.format] = true;
      this.instRemoveSelected = {...this.instRemoveSelected, [instId]: extraSel};
      // Settings/quality "keep-as-override" flags reset: empty map => every diff checkbox defaults to true
      this.instCompareSettingsSelected = {...this.instCompareSettingsSelected, [instId]: {}};
      this.instCompareQualitySelected = {...this.instCompareQualitySelected, [instId]: {}};
    },

    getTrashProfileGroups(appType) {
      const profiles = this.trashProfiles[appType] || [];
      const order = { 'Standard': 0, 'Anime': 1, 'French': 2, 'German': 3, 'SQP': 99 };
      const seen = new Set();
      const groups = [];
      for (const p of profiles) {
        const gn = p.groupName || 'Other';
        if (!seen.has(gn)) { seen.add(gn); groups.push(gn); }
      }
      return groups.sort((a, b) => (order[a] ?? 50) - (order[b] ?? 50));
    },

    getTrashProfilesByGroup(appType, groupName) {
      return (this.trashProfiles[appType] || []).filter(p => (p.groupName || 'Other') === groupName);
    },

    toggleCompareSelect(instId, trashId, checked) {
      const sel = {...(this.instCompareSelected[instId] || {})};
      const result = this.instCompareResult[instId];
      // Exclusive required groups other than Golden Rule: block unchecking the last variant.
      // Golden Rule is now optional — a profile may legitimately have no HD or UHD variant active.
      // Matches Profile Edit (imported-profile flow) and Profile Builder (variantGoldenRule: 'none').
      // "Never both" is still enforced below when checking — so user can end with one or none,
      // but never both active simultaneously.
      if (!checked && result) {
        for (const group of (result.groups || [])) {
          if (!group.exclusive || !group.defaultEnabled) continue;
          // Golden Rule is optional — allow unchecking the last variant.
          if ((group.name || '').toLowerCase().includes('golden rule')) continue;
          const cfsInGroup = group.cfs.map(c => c.trashId);
          if (cfsInGroup.includes(trashId)) {
            const otherSelected = cfsInGroup.some(tid => tid !== trashId && sel[tid]);
            if (!otherSelected) {
              this.showToast(`"${group.name}" needs one variant picked — pick the other one first to switch`, 'error', 6000);
              // Force Alpine to re-render the checkbox back to its stored state. Without this
              // the browser's native toggle leaves the checkbox visually unchecked even though
              // our data still says checked. Re-assigning the map triggers :checked re-bind.
              this.instCompareSelected = {...this.instCompareSelected, [instId]: sel};
              return;
            }
          }
        }
      }
      sel[trashId] = checked;
      // For exclusive groups: deselect other CFs in the same group when one is checked
      if (checked && result) {
        for (const group of (result.groups || [])) {
          if (!group.exclusive) continue;
          const cfsInGroup = group.cfs.map(c => c.trashId);
          if (cfsInGroup.includes(trashId)) {
            for (const otherTid of cfsInGroup) {
              if (otherTid !== trashId) sel[otherTid] = false;
            }
          }
        }
      }
      this.instCompareSelected = {...this.instCompareSelected, [instId]: sel};
    },

    toggleAllCompareSection(instId, section) {
      const result = this.instCompareResult[instId];
      if (!result) return;
      if (section === 'extra') {
        const sel = {...(this.instRemoveSelected[instId] || {})};
        const ids = (result.extraCFs || []).map(e => e.format);
        const allChecked = ids.every(id => sel[id]);
        for (const id of ids) { sel[id] = !allChecked; }
        this.instRemoveSelected = {...this.instRemoveSelected, [instId]: sel};
      } else if (section === 'formatItems') {
        const sel = {...(this.instCompareSelected[instId] || {})};
        const ids = (result.formatItems || []).filter(fi => !fi.exists || !fi.scoreMatch).map(fi => fi.trashId);
        const allChecked = ids.every(id => sel[id]);
        for (const id of ids) { sel[id] = !allChecked; }
        this.instCompareSelected = {...this.instCompareSelected, [instId]: sel};
      } else {
        // 'all' or other — select all genuine errors across formatItems + groups
        const sel = {...(this.instCompareSelected[instId] || {})};
        const ids = [];
        for (const fi of (result.formatItems || [])) {
          if (!fi.exists || !fi.scoreMatch) ids.push(fi.trashId);
        }
        for (const group of (result.groups || [])) {
          if (group.exclusive && group.defaultEnabled) {
            // Pick exactly one variant: prefer in-use, fall back to TRaSH default
            const inUse = group.cfs.find(cf => cf.exists && cf.inUse);
            const def = group.cfs.find(cf => cf.default && cf.required);
            const pick = inUse || def;
            if (pick) ids.push(pick.trashId);
            continue;
          }
          for (const cf of group.cfs) {
            if (!cf.exists && group.defaultEnabled && cf.required) ids.push(cf.trashId);
            else if (cf.exists && cf.inUse && !cf.scoreMatch) ids.push(cf.trashId);
          }
        }
        const allChecked = ids.every(id => sel[id]);
        for (const id of ids) { sel[id] = !allChecked; }
        this.instCompareSelected = {...this.instCompareSelected, [instId]: sel};
      }
    },

    isOptGroupMarkedForRemoval(instId, trashId) {
      return !!(this.instRemoveSelected[instId] || {})[trashId + '_grp'];
    },

    toggleOptGroupRemove(instId, oc, markForRemoval) {
      const sel = {...(this.instRemoveSelected[instId] || {})};
      sel[oc.trashId + '_grp'] = markForRemoval;
      if (markForRemoval) {
        sel['_arrId_' + oc.trashId] = oc.arrId;
      } else {
        delete sel['_arrId_' + oc.trashId];
      }
      this.instRemoveSelected = {...this.instRemoveSelected, [instId]: sel};
    },

    toggleRemoveSelect(instId, arrCfId, checked) {
      const sel = {...(this.instRemoveSelected[instId] || {})};
      sel[arrCfId] = checked;
      this.instRemoveSelected = {...this.instRemoveSelected, [instId]: sel};
    },

    getCompareSelectedCount(instId) {
      return Object.values(this.instCompareSelected[instId] || {}).filter(v => v).length;
    },

    getCompareSettingsCount(instId, cr) {
      let count = 0;
      const settingSel = this.instCompareSettingsSelected[instId] || {};
      for (const sd of (cr?.settingsDiffs || [])) {
        if (!sd.match && settingSel[sd.name] !== false) count++;
      }
      const qualitySel = this.instCompareQualitySelected[instId] || {};
      for (const qd of (cr?.qualityDiffs || [])) {
        if (!qd.match && qualitySel[qd.name] !== false) count++;
      }
      return count;
    },

    getRemoveSelectedCount(instId) {
      const sel = this.instRemoveSelected[instId] || {};
      // Count entries that are actual selections (not _arrId_ metadata keys)
      return Object.entries(sel).filter(([k, v]) => v && !k.startsWith('_arrId_')).length;
    },

    getTrashCFName(appType, trashId) {
      // Look up CF name from loaded TRaSH browse data
      const data = this.cfBrowseData[appType];
      if (data) {
        // Check individual CFs
        for (const cf of (data.cfs || [])) {
          if (cf.trash_id === trashId) return cf.name;
        }
        // Check CF groups
        for (const g of (data.groups || [])) {
          for (const cf of (g.custom_formats || [])) {
            if (cf.trash_id === trashId) return cf.name;
          }
        }
      }
      return trashId.substring(0, 8) + '...';
    },

    async removeFromProfile(inst, comparison) {
      const sel = this.instRemoveSelected[inst.id] || {};
      // Collect Arr CF IDs from extra CFs (format is the Arr ID)
      const cfIds = [];
      const names = [];
      // Extra CFs in Arr profile
      for (const ecf of (comparison.extraCFs || [])) {
        if (sel[ecf.format]) {
          cfIds.push(ecf.format);
          names.push(ecf.name);
        }
      }
      // Optional group CFs marked for removal (key: trashId_grp, arrId stored separately)
      for (const [key, val] of Object.entries(sel)) {
        if (key.endsWith('_grp') && val) {
          const trashId = key.slice(0, -4);
          const arrId = sel['_arrId_' + trashId];
          if (arrId) {
            cfIds.push(arrId);
            // Find name from groups or cfStates
            const state = (comparison.cfStates || {})[trashId];
            if (state?.trashName) { names.push(state.trashName); }
            else {
              for (const g of (comparison.groups || [])) {
                const cf = g.cfs.find(c => c.trashId === trashId);
                if (cf) { names.push(cf.name); break; }
              }
            }
          }
        }
      }
      if (!cfIds.length) return;
      this.confirmModal = {
        show: true,
        title: 'Remove CF Scores',
        message: `Remove ${cfIds.length} CF score(s) from profile?\n\n${names.join(', ')}\n\nScores will be set to 0.`,
        onConfirm: async () => {
          try {
            const r = await fetch(`/api/instances/${inst.id}/profile-cfs/remove`, {
              method: 'POST',
              headers: {'Content-Type': 'application/json'},
              body: JSON.stringify({ arrProfileId: comparison.arrProfileId, cfIds })
            });
            if (!r.ok) { const e = await r.json(); this.showToast(e.error || 'Failed', 'error', 8000); return; }
            const result = await r.json();
            this.instRemoveSelected = {...this.instRemoveSelected, [inst.id]: {}};
            this.runProfileCompare(inst, comparison.arrProfileId, comparison.trashProfileId);
          } catch (e) { this.showToast('Error: ' + e.message, 'error', 8000); }
        }
      };
    },

    async syncSingleCF(inst, trashId, score, arrProfileId) {
      try {
        const r = await fetch(`/api/instances/${inst.id}/profile-cfs/sync-one`, {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({ arrProfileId, trashId, score })
        });
        if (!r.ok) { const e = await r.json(); this.showToast(e.error || 'Failed', 'error', 8000); return; }
        const result = await r.json();
        // Deselect this CF
        if (this.instCompareSelected[inst.id]) {
          delete this.instCompareSelected[inst.id][trashId];
          this.instCompareSelected = {...this.instCompareSelected};
        }
        // Re-run compare to refresh
        const comp = this.instCompareResult[inst.id];
        if (comp) this.runProfileCompare(inst, comp.arrProfileId, comp.trashProfileId);
      } catch (e) { this.showToast('Error: ' + e.message, 'error', 8000); }
    },

    async removeSingleCF(inst, arrCfId, arrProfileId) {
      if (!arrCfId || !arrProfileId) return;
      try {
        const r = await fetch(`/api/instances/${inst.id}/profile-cfs/remove`, {
          method: 'POST',
          headers: {'Content-Type': 'application/json'},
          body: JSON.stringify({ arrProfileId, cfIds: [arrCfId] })
        });
        if (!r.ok) { const e = await r.json(); this.showToast(e.error || 'Failed', 'error', 8000); return; }
        // Re-run compare to refresh
        const comp = this.instCompareResult[inst.id];
        if (comp) this.runProfileCompare(inst, comp.arrProfileId, comp.trashProfileId);
      } catch (e) { this.showToast('Error: ' + e.message, 'error', 8000); }
    },

    // Re-applies the initial "match TRaSH" selection state — undoes any manual unchecks/changes
    // the user has made since loading Compare. Cheap: no API call, just re-runs the default logic
    // against the already-fetched result.
    resetCompareToDefault(inst, cr) {
      if (!cr) return;
      this.compareApplyDefaultSelections(cr, inst.id);
    },

    // Per-card Quick Sync — opens simplified confirm dialog scoped to one card. Only applies
    // the diffs visible in that card; other cards' diffs are treated as "keep as override".
    compareQuickSyncOpen(inst, cr, section) {
      const sel = this.instCompareSelected[inst.id] || {};
      const settingSel = this.instCompareSettingsSelected[inst.id] || {};
      const qualSel = this.instCompareQualitySelected[inst.id] || {};
      const removeSel = this.instRemoveSelected[inst.id] || {};
      let title = '', summary = '';
      if (section === 'settings') {
        const g = (cr.settingsDiffs || []).filter(s => !s.match && ['Language','Upgrade Allowed','Min Format Score','Min Upgrade Format Score','Cutoff Format Score'].includes(s.name) && settingSel[s.name] !== false).length;
        const q = (cr.settingsDiffs || []).filter(s => !s.match && s.name === 'Cutoff' && settingSel[s.name] !== false).length;
        const qi = (cr.qualityDiffs || []).filter(qd => !qd.match && qualSel[qd.name] !== false).length;
        title = 'Sync Profile Settings';
        summary = [
          g > 0 ? `${g} general setting${g === 1 ? '' : 's'}` : '',
          q > 0 ? `cutoff quality` : '',
          qi > 0 ? `${qi} quality item${qi === 1 ? '' : 's'}` : '',
        ].filter(Boolean).join(', ') || 'no changes';
      } else if (section === 'requiredCfs') {
        const n = (cr.formatItems || []).filter(fi => (!fi.exists || !fi.scoreMatch) && sel[fi.trashId]).length;
        title = 'Sync Required CFs';
        summary = n > 0 ? `${n} CF${n === 1 ? '' : 's'}` : 'no changes';
      } else if (section === 'groups') {
        let n = 0;
        for (const g of (cr.groups || [])) {
          for (const cf of g.cfs) {
            if (sel[cf.trashId] && ((cf.exists && !cf.scoreMatch) || (!cf.exists && g.defaultEnabled && cf.required) || (g.exclusive && g.defaultEnabled))) n++;
          }
        }
        title = 'Sync CF Groups';
        summary = n > 0 ? `${n} CF${n === 1 ? '' : 's'} across groups` : 'no changes';
      } else if (section === 'extras') {
        const n = (cr.extraCFs || []).filter(e => removeSel[e.format]).length;
        title = 'Remove Extras';
        summary = n > 0 ? `${n} extra CF${n === 1 ? '' : 's'} — scores will be set to 0 in Arr` : 'no changes';
      }
      this.compareQuickSync = { show: true, inst, cr, section, title, summary, running: false };
    },

    // Apply the sync that was just dry-run from Compare, using the stored context. Re-opens the
    // quick-sync modal with section/inst/cr preserved then runs mode='sync'.
    async compareApplyFromDryRun() {
      const ctx = this.compareLastDryRunContext;
      if (!ctx) { this.showToast('No dry-run to apply', 'error', 5000); return; }
      this.compareQuickSync = { show: true, inst: ctx.inst, cr: ctx.cr, section: ctx.section, title: '', summary: '', running: false };
      await this.compareQuickSyncRun('sync');
      this.compareLastDryRunContext = null;
    },

    async compareQuickSyncRun(mode) {
      const q = this.compareQuickSync;
      if (!q.show) return;
      q.running = true;
      try {
        const { inst, cr, section } = q;
        if (section === 'extras') {
          const removeSel = this.instRemoveSelected[inst.id] || {};
          const cfIds = (cr.extraCFs || []).filter(e => removeSel[e.format]).map(e => e.format);
          if (cfIds.length === 0) { this.compareQuickSync = { show: false }; return; }
          if (mode === 'dryrun') { this.showToast(`Dry-run: ${cfIds.length} extra CF score${cfIds.length === 1 ? '' : 's'} would be set to 0`, 'info', 8000); return; }
          const r = await fetch(`/api/instances/${inst.id}/profile-cfs/remove`, {
            method: 'POST', headers: {'Content-Type':'application/json'},
            body: JSON.stringify({ arrProfileId: cr.arrProfileId, cfIds })
          });
          if (!r.ok) { const e = await r.json().catch(()=>({})); this.showToast('Remove failed: ' + (e.error || r.statusText), 'error', 8000); return; }
          this.instRemoveSelected = {...this.instRemoveSelected, [inst.id]: {}};
          this.showToast(`Removed ${cfIds.length} extra CF score${cfIds.length === 1 ? '' : 's'}`, 'info', 6000);
          this.runProfileCompare(inst, cr.arrProfileId, cr.trashProfileId);
          return;
        }
        const sel = this.instCompareSelected[inst.id] || {};
        const settingSel = this.instCompareSettingsSelected[inst.id] || {};
        const qualSel = this.instCompareQualitySelected[inst.id] || {};
        const selectedCFs = [];
        for (const fi of (cr.formatItems || [])) {
          if (fi.exists && fi.scoreMatch) selectedCFs.push(fi.trashId);
          else if (section === 'requiredCfs' && sel[fi.trashId]) selectedCFs.push(fi.trashId);
          else if (fi.exists) selectedCFs.push(fi.trashId);
        }
        for (const g of (cr.groups || [])) {
          for (const cf of g.cfs) {
            if (cf.exists && cf.inUse) selectedCFs.push(cf.trashId);
            else if (section === 'groups' && sel[cf.trashId]) selectedCFs.push(cf.trashId);
          }
        }
        const overrides = {};
        const qualityOverrides = {};
        const preserveAsOverride = (sd) => {
          if (sd.name === 'Min Format Score') overrides.minFormatScore = parseInt(sd.current) || 0;
          else if (sd.name === 'Cutoff Format Score') overrides.cutoffFormatScore = parseInt(sd.current) || 0;
          else if (sd.name === 'Min Upgrade Format Score') overrides.minUpgradeFormatScore = parseInt(sd.current) || 0;
          else if (sd.name === 'Cutoff') overrides.cutoffQuality = sd.current;
          else if (sd.name === 'Language') overrides.language = sd.current;
          else if (sd.name === 'Upgrade Allowed') overrides.upgradeAllowed = sd.current === 'true' || sd.current === true;
        };
        for (const sd of (cr.settingsDiffs || [])) {
          if (sd.match) continue;
          if (section !== 'settings') { preserveAsOverride(sd); continue; }
          if (settingSel[sd.name] === false) preserveAsOverride(sd);
        }
        for (const qd of (cr.qualityDiffs || [])) {
          if (qd.match) continue;
          if (section !== 'settings') { qualityOverrides[qd.name] = qd.currentAllowed; continue; }
          if (qualSel[qd.name] === false) qualityOverrides[qd.name] = qd.currentAllowed;
        }
        // scoreOverrides: for non-target sections we preserve every existing CF's current Arr
        // score so the backend's sync-all doesn't "fix" scores outside the target scope.
        // For the target section, explicitly set the TRaSH desired score for diff CFs the user
        // kept checked (uncheck → preserve as current).
        const scoreOverrides = {};
        for (const fi of (cr.formatItems || [])) {
          if (!fi.exists) continue;
          const picked = sel[fi.trashId] === true;
          const isTarget = section === 'requiredCfs';
          if (isTarget && picked && !fi.scoreMatch) scoreOverrides[fi.trashId] = fi.desiredScore;
          else scoreOverrides[fi.trashId] = fi.currentScore;
        }
        for (const g of (cr.groups || [])) {
          for (const cf of g.cfs) {
            if (!cf.exists) continue;
            const picked = sel[cf.trashId] === true;
            const isTarget = section === 'groups';
            if (isTarget && picked && !cf.scoreMatch) scoreOverrides[cf.trashId] = cf.desiredScore;
            else scoreOverrides[cf.trashId] = cf.currentScore;
          }
        }

        // Per-section behavior. For settings-only sync, tell backend NOT to add CFs and NOT to
        // overwrite existing CF scores — only profile-level settings + quality items change.
        // For CF-touching sections, use normal behavior so new CFs are added and scores set.
        const behavior = section === 'settings'
          ? { addMode: 'do_not_add', removeMode: 'allow_custom', resetMode: 'reset_to_zero' }
          : { addMode: 'add_missing', removeMode: 'remove_custom', resetMode: 'reset_to_zero' };
        const body = {
          instanceId: inst.id,
          profileTrashId: cr.trashProfileId,
          arrProfileId: cr.arrProfileId,
          selectedCFs,
          scoreOverrides: Object.keys(scoreOverrides).length > 0 ? scoreOverrides : undefined,
          behavior,
          overrides: Object.keys(overrides).length > 0 ? overrides : undefined,
          qualityOverrides: Object.keys(qualityOverrides).length > 0 ? qualityOverrides : undefined,
        };
        const url = mode === 'dryrun' ? '/api/sync/dry-run' : '/api/sync/apply';
        const r = await fetch(url, { method: 'POST', headers: {'Content-Type':'application/json'}, body: JSON.stringify(body) });
        const data = await r.json().catch(() => ({}));
        if (!r.ok) { this.showToast((data && data.error) || 'Failed', 'error', 8000); return; }
        if (mode === 'dryrun') {
          this.syncPlan = data;
          this.syncForm = {...(this.syncForm || {}), _fromCompare: true, profileName: cr.trashProfileName, instanceId: inst.id};
          this.dryrunDetailsOpen = false;
          // Save context so Apply-from-banner can re-run the same scoped sync without reopening modal
          this.compareLastDryRunContext = { inst, cr, section };
          this.showToast('Dry-run complete — see banner in Compare', 'info', 5000);
        } else {
          this.syncResult = data;
          this.syncForm = {...(this.syncForm || {}), _fromCompare: true, profileName: cr.trashProfileName, instanceId: inst.id, arrProfileName: cr.arrProfileName};
          this.syncPlan = null;
          this.syncResultDetailsOpen = false;
          this.showToast('Sync complete', 'info', 5000);
          await this.loadSyncHistory(inst.id);
          this.runProfileCompare(inst, cr.arrProfileId, cr.trashProfileId);
        }
      } catch (e) {
        console.error('compareQuickSyncRun:', e);
        this.showToast('Error: ' + e.message, 'error', 8000);
      } finally {
        this.compareQuickSync = { show: false };
      }
    },

    async syncFromCompare(inst, comparison) {
      this.debugLog('UI', `Sync from Compare: "${comparison.trashProfileName}" on ${inst.name}`);
      // Find the TRaSH profile and open sync modal with pre-filled data
      const profile = (this.trashProfiles[inst.type] || []).find(p => p.trashId === comparison.trashProfileId);
      if (!profile) { this.showToast('TRaSH profile not found', 'error', 8000); return; }
      // Build selectedOptionalCFs from compare data:
      // Include CFs user actively has (inUse) + CFs user checked for sync (instCompareSelected)
      const sel = {};
      const checked = this.instCompareSelected[inst.id] || {};
      for (const group of (comparison.groups || [])) {
        let groupActive = false;
        for (const cf of group.cfs) {
          if (cf.inUse || checked[cf.trashId]) {
            sel[cf.trashId] = true;
            groupActive = true;
          }
        }
        if (groupActive) {
          sel['__grp_' + group.name] = true;
        }
      }
      this.selectedOptionalCFs = sel;
      // Build per-CF score overrides: preserve current Arr score for every diff CF the user did
      // NOT check. Backend's sync-all would otherwise apply TRaSH's desired score to all inUse
      // CFs, ignoring the checkbox state. buildSyncBody picks these up via this.cfScoreOverrides.
      const compareScoreOverrides = {};
      for (const fi of (comparison.formatItems || [])) {
        if (!fi.exists || fi.scoreMatch) continue;
        if (checked[fi.trashId] !== true) compareScoreOverrides[fi.trashId] = fi.currentScore;
      }
      for (const group of (comparison.groups || [])) {
        for (const cf of group.cfs) {
          if (!cf.exists || cf.scoreMatch) continue;
          if (checked[cf.trashId] !== true) compareScoreOverrides[cf.trashId] = cf.currentScore;
        }
      }
      this.cfScoreOverrides = compareScoreOverrides;
      // Build overrides from unchecked settings (keep Arr value)
      const overrides = {};
      const settingSel = this.instCompareSettingsSelected[inst.id] || {};
      const settingsMap = {};
      for (const sd of (comparison.settingsDiffs || [])) {
        if (!sd.match) settingsMap[sd.name] = sd;
      }
      if (settingSel['Min Format Score'] === false && settingsMap['Min Format Score'])
        overrides.minFormatScore = parseInt(settingsMap['Min Format Score'].current) || 0;
      if (settingSel['Cutoff Format Score'] === false && settingsMap['Cutoff Format Score'])
        overrides.cutoffFormatScore = parseInt(settingsMap['Cutoff Format Score'].current) || 0;
      if (settingSel['Min Upgrade Format Score'] === false && settingsMap['Min Upgrade Format Score'])
        overrides.minUpgradeFormatScore = parseInt(settingsMap['Min Upgrade Format Score'].current) || 0;
      if (settingSel['Cutoff'] === false && settingsMap['Cutoff'])
        overrides.cutoffQuality = settingsMap['Cutoff'].current;
      if (settingSel['Language'] === false && settingsMap['Language'])
        overrides.language = settingsMap['Language'].current;

      // Build quality overrides from unchecked quality items (keep Arr value)
      const qualityOverrides = {};
      const qualitySel = this.instCompareQualitySelected[inst.id] || {};
      for (const qd of (comparison.qualityDiffs || [])) {
        if (!qd.match && qualitySel[qd.name] === false) {
          qualityOverrides[qd.name] = qd.currentAllowed;
        }
      }

      // Collect extras the user marked for removal — chained after the sync completes in startApply.
      const extraRemoveSel = this.instRemoveSelected[inst.id] || {};
      const pendingExtras = [];
      for (const ecf of (comparison.extraCFs || [])) {
        if (extraRemoveSel[ecf.format]) pendingExtras.push(ecf.format);
      }

      this.syncForm = {
        instanceId: inst.id,
        instanceName: inst.name,
        appType: inst.type,
        profileTrashId: comparison.trashProfileId,
        profileName: comparison.trashProfileName,
        arrProfileId: '0',
        newProfileName: comparison.trashProfileName,
        importedProfileId: '',
        behavior: { addMode: 'add_missing', removeMode: 'remove_custom', resetMode: 'reset_to_zero' },
        overrides: Object.keys(overrides).length > 0 ? overrides : undefined,
        qualityOverrides: Object.keys(qualityOverrides).length > 0 ? qualityOverrides : undefined,
        _fromCompare: true,
        _pendingExtrasRemove: pendingExtras,
        _compareArrProfileId: comparison.arrProfileId,
      };
      this.resyncTargetArrProfileId = comparison.arrProfileId;
      this.syncMode = 'update';
      this.syncPreview = null;
      await this._loadSyncInstanceData(inst.id, comparison.trashProfileId);
      this.showSyncModal = true;
    },

    getSyncOptionalBreakdown() {
      const sel = this.selectedOptionalCFs || {};
      const groups = this.profileDetail?.detail?.trashGroups || [];
      const breakdown = [];
      if (groups.length) {
        // New: use trashGroups
        for (const group of groups) {
          const grpOn = sel['__grp_' + group.name] !== undefined ? sel['__grp_' + group.name] : group.defaultEnabled;
          if (!grpOn) continue;
          const count = group.cfs.filter(cf => cf.required || sel[cf.trashId]).length;
          if (count > 0) breakdown.push({ category: group.name, count });
        }
      } else {
        // Legacy: use cfCategories
        for (const cat of (this.profileDetail?.detail?.cfCategories || [])) {
          let count = 0;
          for (const g of cat.groups) {
            for (const cf of g.cfs) { if (sel[cf.trashId]) count++; }
          }
          if (count > 0) breakdown.push({ category: cat.category, count });
        }
      }
      return breakdown;
    },

    getSelectedCFIds() {
      const idSet = new Set();
      // Include individually toggled optional CFs
      for (const [k, v] of Object.entries(this.selectedOptionalCFs)) {
        if (v && !k.startsWith('__grp_')) idSet.add(k);
      }
      // Include required CFs from active TRaSH groups
      const groups = this.profileDetail?.detail?.trashGroups || [];
      for (const group of groups) {
        const grpOn = this.selectedOptionalCFs['__grp_' + group.name] !== undefined
          ? this.selectedOptionalCFs['__grp_' + group.name]
          : group.defaultEnabled;
        if (!grpOn) continue;
        for (const cf of group.cfs) {
          if (cf.required) {
            idSet.add(cf.trashId);
          }
        }
      }
      // Include any CF that has an active score override. Without this, a user
      // edit in the "Overridden Scores" panel is sent in the scoreOverrides
      // map but the backend's BuildArrProfile only processes trashIDs present
      // in FormatItems ∪ selectedCFs — so the override is silently dropped.
      // This matched the Clonarr bug "Overridden Scores changes don't sync".
      if (this.cfScoreOverrideActive && this.cfScoreOverrides) {
        for (const trashId of Object.keys(this.cfScoreOverrides)) {
          idSet.add(trashId);
        }
      }
      return [...idSet];
    },

    // --- Sync ---

    async openSyncModal(inst, profile) {
      this.syncForm = {
        instanceId: inst.id,
        instanceName: inst.name,
        appType: inst.type,
        profileTrashId: profile.trashId,
        importedProfileId: '',
        profileName: profile.name,
        arrProfileId: '0',
        newProfileName: profile.name,
        behavior: { addMode: 'add_missing', removeMode: 'remove_custom', resetMode: 'reset_to_zero' }
      };
      await this._loadSyncInstanceData(inst.id, profile.trashId);
      this.showSyncModal = true;
    },

    async openSyncModalAsNew(inst, profile) {
      this.resyncTargetArrProfileId = null;
      this.syncForm = {
        instanceId: inst.id,
        instanceName: inst.name,
        appType: inst.type,
        profileTrashId: profile.trashId,
        importedProfileId: '',
        profileName: profile.name,
        arrProfileId: '0',
        newProfileName: profile.name + ' (Copy)',
        behavior: { addMode: 'add_missing', removeMode: 'remove_custom', resetMode: 'reset_to_zero' }
      };
      try {
        const r = await fetch(`/api/instances/${inst.id}/profiles`);
        this.arrProfiles = r.ok ? await r.json() : [];
      } catch (e) { this.arrProfiles = []; }
      this.getLanguagesForInstance(inst.id);
      this.syncMode = 'create';
      this.autoSyncRuleForSync = null;
      this.syncPreview = null;
      this.showSyncModal = true;
    },

    async openImportedSyncModalFromList(appType, profile) {
      const inst = this.instancesOfType(appType)[0];
      if (!inst) { this.showToast('No ' + appType + ' instance configured', 'error', 8000); return; }
      this.syncForm = {
        instanceId: inst.id,
        instanceName: inst.name,
        appType: appType,
        profileTrashId: profile.trashProfileId || '',
        importedProfileId: profile.id,
        profileName: profile.name,
        arrProfileId: '0',
        newProfileName: profile.name,
        behavior: { addMode: 'add_missing', removeMode: 'remove_custom', resetMode: 'reset_to_zero' }
      };
      await this._loadSyncInstanceData(inst.id, profile.trashProfileId || profile.id);
      this.showSyncModal = true;
    },

    async openImportedSyncModalAsNew() {
      const raw = this.profileDetail?.detail?.importedRaw;
      if (!raw) return;
      const appType = raw.appType || this.profileDetail?.profile?.appType;
      const inst = this.instancesOfType(appType)[0];
      if (!inst) { this.showToast('No ' + appType + ' instance configured', 'error', 8000); return; }
      this.resyncTargetArrProfileId = null;
      this.syncForm = {
        instanceId: inst.id,
        instanceName: inst.name,
        appType: appType,
        profileTrashId: raw.trashProfileId || '',
        importedProfileId: raw.id,
        profileName: raw.name,
        arrProfileId: '0',
        newProfileName: raw.name + ' (Copy)',
        behavior: { addMode: 'add_missing', removeMode: 'remove_custom', resetMode: 'reset_to_zero' }
      };
      try {
        const r = await fetch(`/api/instances/${inst.id}/profiles`);
        this.arrProfiles = r.ok ? await r.json() : [];
      } catch (e) { this.arrProfiles = []; }
      this.getLanguagesForInstance(inst.id);
      this.syncMode = 'create';
      this.autoSyncRuleForSync = null;
      this.syncPreview = null;
      this.showSyncModal = true;
    },

    async saveAndSyncBuilderAsNew() {
      const editId = this.pb.editId;
      const appType = this.pb.appType;
      await this.saveCustomProfile();
      const allImported = this.importedProfiles[appType] || [];
      const imported = allImported.find(p => p.id === editId);
      if (!imported) return;
      const found = this.findSyncedInstance(appType, imported.id);
      if (!found) { this.showToast('No ' + appType + ' instance configured', 'error', 8000); return; }
      const { inst } = found;
      this.resyncTargetArrProfileId = null;
      this.syncForm = {
        instanceId: inst.id, instanceName: inst.name, appType: appType,
        profileTrashId: imported.trashProfileId || '', importedProfileId: imported.id,
        profileName: imported.name, arrProfileId: '0', newProfileName: imported.name + ' (Copy)',
        behavior: { addMode: 'add_missing', removeMode: 'remove_custom', resetMode: 'reset_to_zero' }
      };
      try {
        const r = await fetch(`/api/instances/${inst.id}/profiles`);
        this.arrProfiles = r.ok ? await r.json() : [];
      } catch (e) { this.arrProfiles = []; }
      this.getLanguagesForInstance(inst.id);
      this.syncMode = 'create';
      this.autoSyncRuleForSync = null;
      this.syncPreview = null;
      this.showSyncModal = true;
    },

    async saveAndSyncBuilder() {
      const editId = this.pb.editId;
      const appType = this.pb.appType;
      await this.saveCustomProfile();
      const allImported = this.importedProfiles[appType] || [];
      const imported = allImported.find(p => p.id === editId);
      if (!imported) return;
      const found = this.findSyncedInstance(appType, imported.id);
      if (!found) { this.showToast('No ' + appType + ' instance configured', 'error', 8000); return; }
      const { inst, history } = found;
      if (history) this.resyncTargetArrProfileId = history.arrProfileId;
      this.syncForm = {
        instanceId: inst.id, instanceName: inst.name, appType: appType,
        profileTrashId: imported.trashProfileId || '', importedProfileId: imported.id,
        profileName: imported.name, arrProfileId: '0', newProfileName: imported.name,
        behavior: history?.behavior || { addMode: 'add_missing', removeMode: 'remove_custom', resetMode: 'reset_to_zero' }
      };
      await this._loadSyncInstanceData(inst.id, imported.trashProfileId || imported.id);
      this.showSyncModal = true;
    },

    async openBuilderSyncModal() {
      if (!this.pb.editId || !this.pb.appType) return;
      const appType = this.pb.appType;
      const allImported = this.importedProfiles[appType] || [];
      const imported = allImported.find(p => p.id === this.pb.editId);
      if (!imported) { this.showToast('Profile not found', 'error', 8000); return; }
      const found = this.findSyncedInstance(appType, imported.id);
      if (!found) { this.showToast('No ' + appType + ' instance configured', 'error', 8000); return; }
      const { inst, history } = found;
      if (history) this.resyncTargetArrProfileId = history.arrProfileId;
      this.syncForm = {
        instanceId: inst.id,
        instanceName: inst.name,
        appType: appType,
        profileTrashId: imported.trashProfileId || '',
        importedProfileId: imported.id,
        profileName: imported.name,
        arrProfileId: '0',
        newProfileName: imported.name,
        behavior: history?.behavior || { addMode: 'add_missing', removeMode: 'remove_custom', resetMode: 'reset_to_zero' }
      };
      await this._loadSyncInstanceData(inst.id, imported.trashProfileId || imported.id);
      this.showSyncModal = true;
    },

    async openImportedSyncModal() {
      const raw = this.profileDetail?.detail?.importedRaw;
      if (!raw) return;
      const appType = raw.appType || this.profileDetail?.profile?.appType;
      const inst = this.instancesOfType(appType)[0];
      if (!inst) { this.showToast('No ' + appType + ' instance configured', 'error', 8000); return; }
      this.syncForm = {
        instanceId: inst.id,
        instanceName: inst.name,
        appType: appType,
        profileTrashId: raw.trashProfileId || '',
        importedProfileId: raw.id,
        profileName: raw.name,
        arrProfileId: '0',
        newProfileName: raw.name,
        behavior: { addMode: 'add_missing', removeMode: 'remove_custom', resetMode: 'reset_to_zero' }
      };
      await this._loadSyncInstanceData(inst.id, raw.trashProfileId || raw.id);
      this.showSyncModal = true;
    },

    async switchSyncInstance(newInstId) {
      const inst = this.instancesOfType(this.syncForm.appType).find(i => i.id === newInstId);
      if (!inst) return;
      this.syncForm.instanceId = inst.id;
      this.syncForm.instanceName = inst.name;
      this.syncForm.arrProfileId = '0';
      this.syncPreview = null;
      await this._loadSyncInstanceData(inst.id, this.syncForm.profileTrashId || this.syncForm.importedProfileId);
    },

    // Returns sorted language list for an instance (Original first, then alphabetical). Uses cache.
    async getLanguagesForInstance(instId) {
      if (this.instanceLanguages[instId]) return this.instanceLanguages[instId];
      try {
        const r = await fetch(`/api/instances/${instId}/languages`);
        if (r.ok) {
          const langs = await r.json();
          // Sort: Original first, Any second, then alphabetical
          langs.sort((a, b) => {
            if (a.name === 'Original') return -1;
            if (b.name === 'Original') return 1;
            if (a.name === 'Any') return -1;
            if (b.name === 'Any') return 1;
            return a.name.localeCompare(b.name);
          });
          this.instanceLanguages[instId] = langs;
          return langs;
        }
      } catch (e) { /* ignore */ }
      return [{ id: -1, name: 'Original' }, { id: 0, name: 'Any' }];
    },

    // Shorthand: languages for current sync form instance (or fallback)
    get syncLanguages() {
      return this.instanceLanguages[this.syncForm.instanceId] || [{ id: -1, name: 'Original' }, { id: 0, name: 'Any' }];
    },

    async _loadSyncInstanceData(instId, profileTrashId) {
      try {
        const r = await fetch(`/api/instances/${instId}/profiles`);
        this.arrProfiles = r.ok ? await r.json() : [];
      } catch (e) {
        this.arrProfiles = [];
      }
      // Load languages for this instance
      this.getLanguagesForInstance(instId);
      // Auto-detect mode: use resync target if set, otherwise find first matching history entry
      const arrProfileIds = new Set((this.arrProfiles || []).map(p => p.id));
      if (this.resyncTargetArrProfileId && arrProfileIds.has(this.resyncTargetArrProfileId)) {
        this.syncMode = 'update';
        this.syncForm.arrProfileId = String(this.resyncTargetArrProfileId);
        this.resyncTargetArrProfileId = null;
      } else {
        const history = (this.syncHistory[instId] || []).find(h =>
          (h.profileTrashId === profileTrashId || h.importedProfileId === profileTrashId) && arrProfileIds.has(h.arrProfileId)
        );
        if (history && history.arrProfileId) {
          this.syncMode = 'update';
          this.syncForm.arrProfileId = String(history.arrProfileId);
        } else {
          this.syncMode = 'create';
        }
      }
      this.syncPreview = null;
      // Auto-fetch preview if update mode with pre-selected profile
      if (this.syncMode === 'update' && this.syncForm.arrProfileId && this.syncForm.arrProfileId !== '0') {
        this.fetchSyncPreview();
      }
      // Check for existing auto-sync rule
      this.updateAutoSyncRuleForSync();
    },

    buildSyncBody() {
      const body = {
        instanceId: this.syncForm.instanceId,
        profileTrashId: this.syncForm.profileTrashId,
        arrProfileId: this.syncMode === 'create' ? 0 : parseInt(this.syncForm.arrProfileId),
        selectedCFs: this.getAllSelectedCFIds()
      };
      if (this.syncForm.importedProfileId) {
        body.importedProfileId = this.syncForm.importedProfileId;
      }
      if (this.syncMode === 'create') {
        body.profileName = this.syncForm.newProfileName;
      }
      // Build overrides from per-section active flags. Values are kept in pdOverrides regardless,
      // but only sent when the matching section toggle is on.
      const ov = this.pdOverrides;
      const p = this.profileDetail?.detail?.profile || {};
      const overrides = {};
      let hasOverrides = false;
      if (this.pdGeneralActive) {
        if (this.activeAppType === 'radarr' && ov.language.value !== (p.language || 'Original')) { overrides.language = ov.language.value; hasOverrides = true; }
        const upVal = ov.upgradeAllowed.value === true || ov.upgradeAllowed.value === 'true';
        if (upVal !== (p.upgradeAllowed ?? true)) { overrides.upgradeAllowed = upVal; hasOverrides = true; }
        if (ov.minFormatScore.value !== (p.minFormatScore ?? 0)) { overrides.minFormatScore = ov.minFormatScore.value; hasOverrides = true; }
        if (ov.minUpgradeFormatScore.value !== (p.minUpgradeFormatScore ?? 1)) { overrides.minUpgradeFormatScore = ov.minUpgradeFormatScore.value; hasOverrides = true; }
        if (ov.cutoffFormatScore.value !== (p.cutoffFormatScore || p.cutoffScore || 10000)) { overrides.cutoffFormatScore = ov.cutoffFormatScore.value; hasOverrides = true; }
      }
      if (this.pdQualityActive) {
        const defaultCutoff = p.cutoff || '';
        if (ov.cutoffQuality && ov.cutoffQuality !== defaultCutoff) { overrides.cutoffQuality = ov.cutoffQuality; hasOverrides = true; }
      }
      if (hasOverrides) body.overrides = overrides;
      // Per-CF score overrides + extra CFs scores
      const allScoreOverrides = { ...this.cfScoreOverrides };
      for (const [tid, score] of Object.entries(this.extraCFs)) allScoreOverrides[tid] = score;
      if (Object.keys(allScoreOverrides).length > 0) body.scoreOverrides = allScoreOverrides;
      // Quality overrides: structure (new) trumps flat map (legacy)
      if (this.qualityStructure.length > 0) {
        body.qualityStructure = this.qsForBackend();
      } else if (Object.keys(this.qualityOverrides).length > 0) {
        body.qualityOverrides = this.qualityOverrides;
      }
      // Sync behavior rules
      if (this.syncForm.behavior) body.behavior = this.syncForm.behavior;
      return body;
    },

    async fetchSyncPreview() {
      this.syncPreview = null;
      if (!this.syncForm.arrProfileId || this.syncForm.arrProfileId === '0') return;
      this.syncPreviewLoading = true;
      try {
        const r = await fetch('/api/sync/dry-run', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(this.buildSyncBody())
        });
        if (r.ok) {
          this.syncPreview = await r.json();
        } else {
          const e = await r.json();
          this.syncPreview = { error: e.error || 'Preview failed' };
        }
      } catch (e) {
        this.syncPreview = { error: e.message };
      } finally {
        this.syncPreviewLoading = false;
      }
    },

    async startDryRun() {
      // Check for name collision in Create mode — prevent silent overwrite
      if (this.syncMode === 'create') {
        const newName = this.syncForm.newProfileName.trim().toLowerCase();
        const existing = this.arrProfiles.find(p => p.name.toLowerCase() === newName);
        if (existing) {
          this.showToast(`Profile "${this.syncForm.newProfileName.trim()}" already exists in ${this.syncForm.instanceName}. Choose a different name or use Update mode.`, 'error', 10000);
          return;
        }
      }

      this.syncing = true;
      this.debugLog('UI', `Dry-run: "${this.syncForm.profileName}" → ${this.syncForm.instanceName} | ${this.getSelectedCFIds().length} selected CFs`);
      try {
        const body = this.buildSyncBody();
        const r = await fetch('/api/sync/dry-run', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body)
        });
        const data = await r.json();
        if (!r.ok) {
          this.showToast(data.error || 'Dry-run failed', 'error', 8000);
          return;
        }
        if (this.syncForm._fromCompare) {
          // Compare flow: close modal, stay in Compare tab. syncPlan is set below and rendered
          // as a .dryrun-bar inside the Compare section (same style as TRaSH Sync's banner).
          this.showSyncModal = false;
        } else {
          this.showSyncModal = false;
          if (!this.profileDetail && !this.syncForm.importedProfileId) {
            const inst = this.instances.find(i => i.id === this.syncForm.instanceId);
            const profile = (this.trashProfiles[inst.type] || []).find(p => p.trashId === this.syncForm.profileTrashId);
            if (inst && profile) await this.openProfileDetail(inst, profile);
          }
        }
        this.syncPlan = data;
        this.dryrunDetailsOpen = false;
      } catch (e) {
        console.error('dryRun:', e);
      } finally {
        this.syncing = false;
      }
    },

    async startApply() {
      // Check for name collision in Create mode — prevent silent overwrite
      if (this.syncMode === 'create') {
        const newName = this.syncForm.newProfileName.trim().toLowerCase();
        const existing = this.arrProfiles.find(p => p.name.toLowerCase() === newName);
        if (existing) {
          this.showToast(`Profile "${this.syncForm.newProfileName.trim()}" already exists in ${this.syncForm.instanceName}. Choose a different name or use Update mode.`, 'error', 10000);
          return;
        }
      }

      // Check for source type conflict (builder↔TRaSH)
      const arrId = parseInt(this.syncForm.arrProfileId) || 0;
      if (arrId > 0) {
        const instId = this.syncForm.instanceId;
        const existing = (this.syncHistory[instId] || []).find(sh => sh.arrProfileId === arrId);
        if (existing) {
          const isBuilder = !!this.syncForm.importedProfileId;
          const wasBuilder = !!existing.importedProfileId;
          if (wasBuilder && !isBuilder) {
            // Builder → TRaSH: OK with warning
            const ok = await new Promise(resolve => {
              this.confirmModal = { show: true, title: 'Convert to TRaSH Sync',
                message: 'This profile is currently synced via Profile Builder with fixed scores.\n\nConverting to TRaSH Sync means CFs and scores will follow TRaSH Guide updates automatically. You can set overrides in Customize.\n\nThis will replace the builder sync rule.',
                onConfirm: () => resolve(true), onCancel: () => resolve(false) };
            });
            if (!ok) return;
          } else if (!wasBuilder && isBuilder) {
            // TRaSH → Builder: warning about losing auto-sync
            const ok = await new Promise(resolve => {
              this.confirmModal = { show: true, title: 'Replace TRaSH Sync Rule',
                message: 'This profile is currently synced with TRaSH Guides and receives automatic updates.\n\nSwitching to a builder profile will stop all TRaSH Guide sync — scores become fixed and will no longer update automatically.\n\nAre you sure?',
                onConfirm: () => resolve(true), onCancel: () => resolve(false) };
            });
            if (!ok) return;
          }
        }
      }
      this.syncing = true;
      this.debugLog('UI', `Apply: "${this.syncForm.profileName}" → ${this.syncForm.instanceName}`);
      try {
        const r = await fetch('/api/sync/apply', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(this.buildSyncBody())
        });
        const result = await r.json();
        this.showSyncModal = false;
        if (!this.profileDetail && !this.syncForm.importedProfileId) {
          const inst = this.instances.find(i => i.id === this.syncForm.instanceId);
          const profile = (this.trashProfiles[inst.type] || []).find(p => p.trashId === this.syncForm.profileTrashId);
          if (inst && profile) await this.openProfileDetail(inst, profile);
        }
        // Show toast for imported profiles (no profile detail view to show results)
        if (this.syncForm.importedProfileId) {
          if (result.error) {
            this.showToast(`Sync failed: ${result.error}`, 'error', 8000);
          } else if (result.errors?.length) {
            this.showToast(`Sync failed: ${result.errors[0]}`, 'error', 8000);
          } else {
            const details = [
              ...(result.cfDetails || []),
              ...(result.scoreDetails || []),
              ...(result.qualityDetails || []),
              ...(result.settingsDetails || [])
            ];
            let msg = `"${this.syncForm.profileName}" synced`;
            if (details.length > 0) {
              const shown = details.length > 5 ? [...details.slice(0, 4), `...and ${details.length - 4} more`] : details;
              msg += '\n' + shown.join('\n');
            } else {
              msg += ' — no changes';
            }
            this.showToast(msg, 'info', details.length > 0 ? 8000 : 4000);
          }
        }
        this.syncResult = result;
        this.syncResultDetailsOpen = false;
        this.syncPlan = null;
        // Reload Arr profiles first (new profile may have been created), then sync history + rules
        const inst = this.instances.find(i => i.id === this.syncForm.instanceId);
        if (inst) await this.loadInstanceProfiles(inst);
        await this.loadAutoSyncRules();
        await this.loadSyncHistory(this.syncForm.instanceId);
        // Auto-update existing auto-sync rule with current settings
        const syncBody = this.buildSyncBody();
        const arrId = parseInt(this.syncForm.arrProfileId) || 0;
        const existingRule = arrId > 0
          ? this.autoSyncRules.find(r => r.instanceId === this.syncForm.instanceId && r.arrProfileId === arrId)
          : null;
        if (existingRule) {
          const updated = {
            ...existingRule,
            selectedCFs: this.getAllSelectedCFIds(),
            arrProfileId: arrId,
            behavior: this.syncForm.behavior || existingRule.behavior,
            overrides: syncBody.overrides || null,
            scoreOverrides: syncBody.scoreOverrides || null,
            qualityOverrides: syncBody.qualityOverrides || null,
            qualityStructure: syncBody.qualityStructure || null
          };
          try {
            await fetch(`/api/auto-sync/rules/${existingRule.id}`, {
              method: 'PUT',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify(updated)
            });
            await this.loadAutoSyncRules();
          } catch (e) { console.error('updateAutoSyncRule:', e); }
        }
        // Chain extras removal from Compare flow (syncForm._pendingExtrasRemove was set by syncFromCompare)
        if (Array.isArray(this.syncForm._pendingExtrasRemove) && this.syncForm._pendingExtrasRemove.length > 0) {
          try {
            const arrId = this.syncForm._compareArrProfileId || parseInt(this.syncForm.arrProfileId) || 0;
            const r = await fetch(`/api/instances/${this.syncForm.instanceId}/profile-cfs/remove`, {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ arrProfileId: arrId, cfIds: this.syncForm._pendingExtrasRemove }),
            });
            if (!r.ok) {
              const e = await r.json().catch(() => ({}));
              this.showToast('Extras removal failed: ' + (e.error || r.statusText), 'error', 8000);
            } else {
              const inst = this.instances.find(i => i.id === this.syncForm.instanceId);
              if (inst) this.instRemoveSelected = {...this.instRemoveSelected, [inst.id]: {}};
            }
          } catch (e) { console.error('pending-extras remove:', e); }
        }
      } catch (e) {
        console.error('apply:', e);
      } finally {
        this.syncing = false;
      }
    },

    async applySync() {
      if (!this.profileDetail || !this.syncForm.instanceId) return;
      await this.startApply();
    },

    // --- Quick Sync ---

    async quickSync(inst, sh, silent = false) {
      // Fallback: check auto-sync rule for importedProfileId if missing from history (pre-1.7.1 migration)
      let importedProfileId = sh.importedProfileId || '';
      if (!importedProfileId) {
        const rule = this.autoSyncRules.find(r => r.instanceId === inst.id && r.arrProfileId === sh.arrProfileId);
        if (rule?.importedProfileId) importedProfileId = rule.importedProfileId;
      }
      const body = {
        instanceId: inst.id,
        profileTrashId: sh.profileTrashId,
        importedProfileId,
        arrProfileId: sh.arrProfileId,
        selectedCFs: Object.keys(sh.selectedCFs || {}).filter(k => sh.selectedCFs[k]),
        scoreOverrides: sh.scoreOverrides || null,
        qualityOverrides: sh.qualityOverrides || null,
        qualityStructure: sh.qualityStructure || null,
        overrides: sh.overrides || null,
        behavior: sh.behavior || null
      };
      try {
        const r = await fetch('/api/sync/apply', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body)
        });
        const result = await r.json();
        if (result.error) {
          if (!silent) this.showToast(`"${sh.profileName}" sync failed: ${result.error}`, 'error', 8000);
          this.setRuleSyncError(inst.id, sh.arrProfileId, result.error);
          return { ok: false, name: sh.profileName, error: result.error };
        }
        if (result.errors?.length) {
          if (!silent) this.showToast(`"${sh.profileName}" sync failed: ${result.errors[0]}`, 'error', 8000);
          this.setRuleSyncError(inst.id, sh.arrProfileId, result.errors[0]);
          return { ok: false, name: sh.profileName, error: result.errors[0] };
        }
        const details = [
          ...(result.cfDetails || []),
          ...(result.scoreDetails || []),
          ...(result.qualityDetails || []),
          ...(result.settingsDetails || [])
        ];
        let msg = `${inst.name} — "${sh.profileName}" synced`;
        if (details.length > 0) {
          const shown = details.length > 5 ? [...details.slice(0, 4), `...and ${details.length - 4} more`] : details;
          msg += '\n' + shown.join('\n');
        } else {
          msg += ' — no changes';
        }
        if (!silent) this.showToast(msg, 'info', details.length > 0 ? 8000 : 4000);
        this.setRuleSyncError(inst.id, sh.arrProfileId, '');
        await this.loadSyncHistory(inst.id);
        const summary = details.length > 0 ? details.slice(0, 3).join(', ') : 'no changes';
        return { ok: true, name: sh.profileName, summary, details };
      } catch (e) {
        if (!silent) this.showToast(`Sync error: ${e.message}`, 'error', 8000);
        this.setRuleSyncError(inst.id, sh.arrProfileId, e.message);
        return { ok: false, name: sh.profileName, error: e.message };
      }
    },

    async renameArrProfile(inst, sh, newName) {
      try {
        const r = await fetch(`/api/instances/${inst.id}/profiles/${sh.arrProfileId}/rename`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ name: newName })
        });
        if (!r.ok) {
          const err = await r.json().catch(() => ({}));
          this.showToast(`Rename failed: ${err.error || 'Unknown error'}`, 'error', 6000);
          return;
        }
        sh.arrProfileName = newName;
        this.showToast(`Renamed → "${newName}"`, 'info', 3000);
        await this.loadSyncHistory(inst.id);
      } catch (e) {
        this.showToast(`Rename error: ${e.message}`, 'error', 6000);
      }
    },

    async cloneProfile(inst, sh) {
      const name = await new Promise(resolve => {
        this.inputModal = {
          show: true,
          title: 'Clone Profile',
          message: `Create a copy of "${sh.arrProfileName}" with all overrides and settings.`,
          placeholder: 'New profile name',
          value: sh.arrProfileName + ' (Copy)',
          confirmLabel: 'Clone',
          onConfirm: (val) => resolve(val),
          onCancel: () => resolve(null)
        };
      });
      if (!name || !name.trim()) return;
      // Resolve importedProfileId from rule if missing in history
      let importedProfileId = sh.importedProfileId || '';
      if (!importedProfileId) {
        const rule = this.autoSyncRules.find(r => r.instanceId === inst.id && r.arrProfileId === sh.arrProfileId);
        if (rule?.importedProfileId) importedProfileId = rule.importedProfileId;
      }
      const body = {
        instanceId: inst.id,
        profileTrashId: sh.profileTrashId,
        importedProfileId,
        arrProfileId: 0, // create mode
        profileName: name.trim(),
        selectedCFs: Object.keys(sh.selectedCFs || {}).filter(k => sh.selectedCFs[k]),
        scoreOverrides: sh.scoreOverrides || null,
        qualityOverrides: sh.qualityOverrides || null,
        qualityStructure: sh.qualityStructure || null,
        overrides: sh.overrides || null,
        behavior: sh.behavior || null
      };
      try {
        const r = await fetch('/api/sync/apply', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(body)
        });
        const result = await r.json();
        if (result.error || result.errors?.length) {
          this.showToast(`Clone failed: ${result.error || result.errors[0]}`, 'error', 8000);
          return;
        }
        this.showToast(`Cloned "${sh.arrProfileName}" → "${name.trim()}"`, 'info', 5000);
        await this.loadSyncHistory(inst.id);
        await this.loadAutoSyncRules();
      } catch (e) {
        this.showToast(`Clone error: ${e.message}`, 'error', 8000);
      }
    },

    async setRuleSyncError(instanceId, arrProfileId, error) {
      const rule = this.autoSyncRules.find(r => r.instanceId === instanceId && r.arrProfileId === arrProfileId);
      if (!rule) return;
      rule.lastSyncError = error;
      try {
        await fetch(`/api/auto-sync/rules/${rule.id}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(rule)
        });
        await this.loadAutoSyncRules();
      } catch (e) { /* best effort */ }
    },

    async syncAllForInstance(inst, builderOnly = false) {
      // Use sortedSyncRules which deduplicates to latest entry per arrProfileId.
      // Before the ring-buffer, syncHistory had one entry per profile. Now it can
      // have up to 10 — iterating all would sync older entries with different
      // selectedCFs, causing score oscillation (each old entry undoes the next).
      const all = this.sortedSyncRules(inst.id);
      const entries = (builderOnly ? all.filter(sh => sh.importedProfileId) : all.filter(sh => !sh.importedProfileId))
        .filter(sh => {
          const rule = this.autoSyncRules.find(r => r.instanceId === inst.id && r.arrProfileId === sh.arrProfileId);
          return rule?.enabled;
        });
      if (!entries.length) {
        this.showToast(`Sync All (${inst.name}): no profiles with auto-sync enabled`, 'warning', 4000);
        return;
      }
      const results = [];
      for (const sh of entries) {
        results.push(await this.quickSync(inst, sh, true));
      }
      const lines = results.map(r => {
        if (!r.ok) return `${r.name} — FAILED: ${r.error}`;
        if (r.details?.length > 0) return `${r.name} — ${r.details.slice(0, 2).join(', ')}`;
        return `${r.name} — no changes`;
      });
      const errors = results.filter(r => !r.ok).length;
      const toastType = errors === results.length ? 'error' : errors > 0 ? 'warning' : 'info';
      this.showToast(`Sync All (${inst.name}):\n${lines.join('\n')}`, toastType, 10000);
    },

    // --- Sync History ---

    async loadSyncHistory(instanceId) {
      try {
        const r = await fetch(`/api/instances/${instanceId}/sync-history`);
        if (r.ok) {
          const data = await r.json();
          this.syncHistory = { ...this.syncHistory, [instanceId]: data };
          // If the History tab has a profile expanded for this instance, refresh its entries too
          if (this.historyExpanded && this.historyExpanded.startsWith(instanceId + ':')) {
            const arrId = parseInt(this.historyExpanded.split(':')[1], 10);
            if (!isNaN(arrId)) this.loadProfileHistory(instanceId, arrId);
          }
        }
      } catch (e) { console.error('loadSyncHistory:', e); }
    },

    async resyncProfile(inst, shArg) {
      // Always use the latest sync history entry for this profile — after rollback
      // or other changes, the passed-in sh may be stale (Alpine template reference).
      const freshHistory = (this.syncHistory[inst.id] || []).filter(h => h.arrProfileId === shArg.arrProfileId);
      const sh = freshHistory[0] || shArg;
      // Set target Arr profile for sync modal to pick up
      this.resyncTargetArrProfileId = sh.arrProfileId;
      // Imported/builder profile: open in Profile Builder editor
      if (sh.importedProfileId) {
        const allImported = this.importedProfiles[inst.type] || [];
        const imported = allImported.find(p => p.id === sh.importedProfileId);
        if (!imported) {
          this.showToast('Imported profile no longer available', 'error', 8000);
          return;
        }
        this.activeAppType = inst.type;
        this.currentSection = 'advanced';
        this.advancedTab = 'builder';
        this.editCustomProfile(inst.type, imported);
        return;
      }
      // TRaSH profile: find and open profile detail
      const profile = (this.trashProfiles[inst.type] || []).find(p => p.trashId === sh.profileTrashId);
      if (!profile) {
        this.showToast('Profile no longer available in TRaSH data', 'error', 8000);
        return;
      }
      // Navigate to profile detail with defaults
      this.activeAppType = inst.type;
      await this.openProfileDetail(inst, profile);
      // Show which Arr profile this is synced to
      this.profileDetail._arrProfileName = sh.arrProfileName || null;
      // Restore optional CF selections from sync history
      if (sh.selectedCFs && Object.keys(sh.selectedCFs).length > 0) {
        const groups = this.profileDetail?.detail?.trashGroups || [];
        for (const group of groups) {
          // Check if ANY CF from this group (required or optional) was in the sync
          const groupWasSynced = group.cfs.some(cf => sh.selectedCFs[cf.trashId]);
          for (const cf of group.cfs) {
            if (!cf.required) {
              this.selectedOptionalCFs[cf.trashId] = !!sh.selectedCFs[cf.trashId];
            }
          }
          // Restore group state based on whether any CF from it was synced
          if (groupWasSynced) {
            this.selectedOptionalCFs['__grp_' + group.name] = true;
          } else if (group.defaultEnabled) {
            this.selectedOptionalCFs['__grp_' + group.name] = false;
          }
        }
        this.selectedOptionalCFs = { ...this.selectedOptionalCFs };
      }
      // Restore overrides from sync history. Any general field present → enable General override.
      // Cutoff quality present → enable Quality override. Values are preserved regardless.
      if (sh.overrides) {
        const ov = sh.overrides;
        let anyGeneral = false;
        if (ov.language !== undefined) { this.pdOverrides.language.enabled = false; this.pdOverrides.language.value = ov.language; anyGeneral = true; }
        if (ov.minFormatScore !== undefined) { this.pdOverrides.minFormatScore.enabled = false; this.pdOverrides.minFormatScore.value = ov.minFormatScore; anyGeneral = true; }
        if (ov.minUpgradeFormatScore !== undefined) { this.pdOverrides.minUpgradeFormatScore.enabled = false; this.pdOverrides.minUpgradeFormatScore.value = ov.minUpgradeFormatScore; anyGeneral = true; }
        if (ov.cutoffFormatScore !== undefined) { this.pdOverrides.cutoffFormatScore.enabled = false; this.pdOverrides.cutoffFormatScore.value = ov.cutoffFormatScore; anyGeneral = true; }
        if (ov.upgradeAllowed !== undefined) { this.pdOverrides.upgradeAllowed.enabled = false; this.pdOverrides.upgradeAllowed.value = ov.upgradeAllowed; anyGeneral = true; }
        if (anyGeneral) this.pdGeneralActive = true;
        if (ov.cutoffQuality !== undefined) { this.pdOverrides.cutoffQuality = ov.cutoffQuality; this.pdQualityActive = true; }
      }
      // Determine which trashIDs are part of the TRaSH base profile. Used by
      // both the Extra-CF split below AND the Overridden-Scores filter.
      // Without this set, we can't tell "user overrode this profile CF's
      // score" from "user added this CF as Extra with its default score".
      const inProfile = new Set();
      for (const fi of (this.profileDetail?.detail?.formatItemNames || [])) inProfile.add(fi.trashId);
      for (const g of (this.profileDetail?.detail?.trashGroups || [])) {
        for (const cf of g.cfs) inProfile.add(cf.trashId);
      }

      // Split sh.scoreOverrides into (Extra CF) vs (base-profile override).
      // Rule: if trashID is NOT in the base profile, it's an Extra — belongs
      // in extraCFs, NOT cfScoreOverrides. Otherwise it's a base-profile
      // override, and only kept if score differs from TRaSH default (prevents
      // "false-positive" overrides with `default → default` rows that
      // reappear after every refresh).
      const extras = {};
      const baseOverrides = {};
      if (sh.scoreOverrides) {
        for (const [tid, v] of Object.entries(sh.scoreOverrides)) {
          if (!inProfile.has(tid)) {
            // Only add to extras if also selected (legacy sync-history may have
            // score entries for CFs that are no longer selected).
            if (sh.selectedCFs && sh.selectedCFs[tid]) {
              extras[tid] = v;
            }
            continue;
          }
          const def = this.resolveCFDefaultScore(tid);
          // Keep only if score differs from TRaSH default. When default can't
          // be resolved (older data, missing profile context), keep the entry
          // — we can't prove it's redundant.
          if (def === '?' || v !== def) {
            baseOverrides[tid] = v;
          }
        }
      }
      this.cfScoreOverrides = baseOverrides;
      this.cfScoreOverrideActive = Object.keys(baseOverrides).length > 0;

      // Restore quality overrides — prefer structure override over legacy flat map.
      // Presence of either means Quality override was active on the saved sync rule, so also
      // flip pdQualityActive so the Quality card reflects the correct override state.
      if (sh.qualityStructure && sh.qualityStructure.length > 0) {
        this.qualityStructure = sh.qualityStructure.map(it => {
          const out = { _id: ++this._qsIdCounter, name: it.name, allowed: !!it.allowed };
          if (it.items && it.items.length > 0) out.items = [...it.items];
          return out;
        });
        this.qualityOverrideActive = true;
        this.pdQualityActive = true;
        // If TRaSH default cutoff is not in the overridden structure, pick first allowed
        const defaultCutoff = this.profileDetail?.detail?.profile?.cutoff || '';
        if (!this.pdOverrides.cutoffQuality && defaultCutoff) {
          const inStructure = this.qualityStructure.some(it => it.name === defaultCutoff && it.allowed !== false);
          if (!inStructure) {
            const firstAllowed = this.qualityStructure.find(it => it.allowed !== false);
            if (firstAllowed) this.pdOverrides.cutoffQuality = firstAllowed.name;
          }
        }
      } else if (sh.qualityOverrides && Object.keys(sh.qualityOverrides).length > 0) {
        this.qualityOverrides = { ...sh.qualityOverrides };
        this.qualityOverrideActive = true;
        this.pdQualityActive = true;
      }
      // Apply the Extra CFs computed above.
      if (Object.keys(extras).length > 0) {
        this.extraCFs = extras;
        this.extraCFsActive = true;
        // Load all CFs for the browser
        const appType = this.profileDetail?.instance?.type;
        if (appType) this.loadExtraCFList();
      }
      // Restore behavior from sync history
      if (sh.behavior) {
        this.syncForm.behavior = { ...this.syncForm.behavior, ...sh.behavior };
      }
    },

    async removeSyncHistory(instanceId, arrProfileId) {
      const confirmed = await new Promise(resolve => {
        this.confirmModal = { show: true, title: 'Remove Sync Entry', message: 'Remove this sync history entry and its auto-sync rule?', confirmLabel: 'Remove', onConfirm: () => resolve(true), onCancel: () => resolve(false) };
      });
      if (!confirmed) return;
      try {
        const r = await fetch(`/api/instances/${instanceId}/sync-history/${arrProfileId}`, { method: 'DELETE' });
        if (!r.ok) { console.error('removeSyncHistory: HTTP', r.status); }
        // Also remove associated auto-sync rule
        const rule = this.autoSyncRules.find(r => r.instanceId === instanceId && r.arrProfileId === arrProfileId);
        if (rule) {
          await fetch(`/api/auto-sync/rules/${rule.id}`, { method: 'DELETE' });
          await this.loadAutoSyncRules();
        }
        await this.loadSyncHistory(instanceId);
      } catch (e) { console.error('removeSyncHistory:', e); }
    },

    // Deduplicate sync history to latest entry per arrProfileId (entries are newest-first
    // from backend). Then apply optional column sort.
    sortedSyncRules(instId) {
      const all = (this.syncHistory[instId] || []).filter(sh => !sh.importedProfileId);
      const seen = new Set();
      const rules = [];
      for (const sh of all) {
        if (!seen.has(sh.arrProfileId)) {
          seen.add(sh.arrProfileId);
          rules.push(sh);
        }
      }
      const col = this.syncRulesSort.col || 'arr';
      const dir = this.syncRulesSort.dir === 'desc' ? -1 : 1;
      return [...rules].sort((a, b) => {
        const av = col === 'trash' ? (a.profileName || '') : (a.arrProfileName || '');
        const bv = col === 'trash' ? (b.profileName || '') : (b.arrProfileName || '');
        return dir * av.localeCompare(bv);
      });
    },

    historyEventCount(instId, arrProfileId) {
      return (this.syncHistory[instId] || []).filter(sh => sh.arrProfileId === arrProfileId && sh.changes).length;
    },

    sortedHistoryProfiles(instId) {
      const rules = this.sortedSyncRules(instId);
      const col = this.historySort.col;
      if (!col) return rules;
      const dir = this.historySort.dir === 'asc' ? 1 : -1;
      return [...rules].sort((a, b) => {
        switch (col) {
          case 'trash': return dir * (a.profileName || '').localeCompare(b.profileName || '');
          case 'arr': return dir * (a.arrProfileName || '').localeCompare(b.arrProfileName || '');
          case 'changed': {
            const at = a.changes ? new Date(a.appliedAt || a.lastSync).getTime() : 0;
            const bt = b.changes ? new Date(b.appliedAt || b.lastSync).getTime() : 0;
            return dir * (at - bt);
          }
          case 'events': return dir * (this.historyEventCount(instId, a.arrProfileId) - this.historyEventCount(instId, b.arrProfileId));
        }
        return 0;
      });
    },

    toggleHistorySort(col) {
      if (this.historySort.col === col) {
        this.historySort.dir = this.historySort.dir === 'asc' ? 'desc' : 'asc';
      } else {
        this.historySort.col = col;
        this.historySort.dir = col === 'changed' || col === 'events' ? 'desc' : 'asc';
      }
    },

    async rollbackSync(inst, entry, entryIdx) {
      // To undo the changes shown in this entry, we sync with the PREVIOUS entry's
      // settings (the state before this sync ran). The previous entry is the next one
      // in the array (newest-first ordering).
      const allEntries = this.historyEntries;
      const changeEntries = allEntries.filter(e => e.changes);
      const prevEntry = changeEntries[entryIdx + 1] || allEntries[allEntries.length - 1];
      if (!prevEntry || prevEntry === entry) {
        this.showToast('No previous state to rollback to — this is the earliest recorded sync.', 'warning', 6000);
        return;
      }
      const date = new Date(entry.appliedAt || entry.lastSync).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false });
      const prevDate = new Date(prevEntry.appliedAt || prevEntry.lastSync).toLocaleString(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false });
      // Build summary of what will be undone
      const changes = entry.changes || {};
      const summary = [
        ...(changes.cfDetails || []).slice(0, 5),
        ...(changes.scoreDetails || []).slice(0, 5),
        ...(changes.qualityDetails || []).slice(0, 3),
        ...(changes.settingsDetails || []).slice(0, 3),
      ];
      const summaryText = summary.length > 0
        ? '\n\nChanges that will be reversed:\n' + summary.slice(0, 8).join('\n') + (summary.length > 8 ? `\n...and ${summary.length - 8} more` : '')
        : '';
      const confirmed = await new Promise(resolve => {
        this.confirmModal = {
          show: true,
          title: 'Rollback Profile',
          message: `Undo the changes from ${date} and restore "${entry.arrProfileName}" to the state from ${prevDate}?\n\nAuto-sync will be disabled to prevent it from overwriting the rollback.${summaryText}`,
          confirmLabel: 'Rollback',
          onConfirm: () => resolve(true),
          onCancel: () => resolve(false),
        };
      });
      if (!confirmed) return;
      this.showToast(`Rolling back "${entry.arrProfileName}" to ${prevDate}...`, 'info', 3000);
      const result = await this.quickSync(inst, prevEntry, true);
      if (result.ok) {
        const rule = this.autoSyncRules.find(r => r.instanceId === inst.id && r.arrProfileId === entry.arrProfileId);
        if (rule && rule.enabled) {
          await this.toggleAutoSyncRule(rule);
        }
        const details = result.details?.length ? '\n' + result.details.slice(0, 5).join('\n') : '';
        this.showToast(`Rolled back "${entry.arrProfileName}" to ${prevDate}. Auto-sync disabled.${details}`, 'info', 8000);
        await this.loadProfileHistory(inst.id, entry.arrProfileId);
        await this.loadSyncHistory(inst.id);
      } else {
        this.showToast(`Rollback failed: ${result.error}`, 'error', 8000);
      }
    },

    async loadProfileHistory(instanceId, arrProfileId) {
      this.historyEntries = [];
      this.historyDetailIdx = -1;
      this.historyLoading = true;
      try {
        const r = await fetch(`/api/instances/${instanceId}/sync-history/${arrProfileId}/changes`);
        if (r.ok) this.historyEntries = await r.json();
      } catch (e) { console.error('loadProfileHistory:', e); }
      finally { this.historyLoading = false; }
    },

    toggleSyncRulesSort(col) {
      if (this.syncRulesSort.col === col) {
        this.syncRulesSort.dir = this.syncRulesSort.dir === 'asc' ? 'desc' : 'asc';
      } else {
        this.syncRulesSort = { col, dir: 'asc' };
      }
    },

    // --- Auto-Sync ---

    async loadAutoSyncSettings() {
      try {
        const r = await fetch('/api/auto-sync/settings');
        if (r.ok) this.autoSyncSettings = await r.json();
      } catch (e) { console.error('loadAutoSyncSettings:', e); }
    },

    async saveAutoSyncSettings() {
      try {
        await fetch('/api/auto-sync/settings', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(this.autoSyncSettings)
        });
      } catch (e) { console.error('saveAutoSyncSettings:', e); }
    },

    // --- Notification Agents ---

    async loadNotificationAgents() {
      try {
        const r = await fetch('/api/auto-sync/notification-agents');
        if (r.ok) this.notificationAgents = await r.json();
      } catch (e) { console.error('loadNotificationAgents:', e); }
    },

    openAgentModal(agent = null) {
      this.agentModal.testResults = [];
      this.agentModal.testing = false;
      this.agentModal.testPassed = false;
      this.agentModal.saving = false;
      if (agent) {
        this.agentModal.editId = agent.id;
        this.agentModal.name = agent.name || '';
        this.agentModal.type = agent.type;
        this.agentModal.enabled = agent.enabled;
        this.agentModal.events = { ...agent.events };
        this.agentModal.config = { ...agent.config };
      } else {
        this.agentModal.editId = null;
        this.agentModal.name = '';
        this.agentModal.type = 'discord';
        this.agentModal.enabled = true;
        this.agentModal.events = { onSyncSuccess: true, onSyncFailure: true, onCleanup: true, onRepoUpdate: false, onChangelog: false };
        this.agentModal.config = { discordWebhook: '', discordWebhookUpdates: '', gotifyUrl: '', gotifyToken: '', gotifyPriorityCritical: true, gotifyPriorityWarning: true, gotifyPriorityInfo: false, gotifyCriticalValue: 8, gotifyWarningValue: 5, gotifyInfoValue: 3, pushoverUserKey: '', pushoverAppToken: '' };
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
      return type === 'gotify' ? '/icons/gotify.png' : `/icons/${type}.svg`;
    },

    agentModalCanTest() {
      const c = this.agentModal.config;
      switch (this.agentModal.type) {
        case 'discord':   return !!c.discordWebhook;
        case 'gotify':    return !!c.gotifyUrl && !!c.gotifyToken;
        case 'pushover':  return !!c.pushoverUserKey && !!c.pushoverAppToken;
        default:          return false;
      }
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
      // If editing, resolve masked credentials server-side via the {id}/test route.
      // If adding (no editId), post to the inline test endpoint with raw config.
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
          // Sync row status for existing agents
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

    async loadAutoSyncRules() {
      try {
        const r = await fetch('/api/auto-sync/rules');
        if (r.ok) this.autoSyncRules = await r.json();
      } catch (e) { console.error('loadAutoSyncRules:', e); }
    },

    findAutoSyncRule(instanceId, arrProfileId) {
      const aid = parseInt(arrProfileId) || 0;
      if (aid > 0) {
        return this.autoSyncRules.find(r => r.instanceId === instanceId && r.arrProfileId === aid) || null;
      }
      return null;
    },

    // Count how many General fields differ from TRaSH defaults.
    pdGeneralChangeCount() {
      const p = this.profileDetail?.detail?.profile || {};
      const ov = this.pdOverrides;
      let n = 0;
      if (this.activeAppType === 'radarr' && ov.language.value !== (p.language || 'Original')) n++;
      const upVal = ov.upgradeAllowed.value === true || ov.upgradeAllowed.value === 'true';
      if (upVal !== (p.upgradeAllowed ?? true)) n++;
      if (ov.minFormatScore.value !== (p.minFormatScore ?? 0)) n++;
      if (ov.minUpgradeFormatScore.value !== (p.minUpgradeFormatScore ?? 1)) n++;
      if (ov.cutoffFormatScore.value !== (p.cutoffFormatScore || p.cutoffScore || 10000)) n++;
      return n;
    },

    // Count how many Quality fields differ (currently: cutoffQuality only).
    // Any value that isn't the TRaSH default counts — including "__skip__" (don't sync).
    pdQualityChangeCount() {
      const p = this.profileDetail?.detail?.profile || {};
      const cq = this.pdOverrides.cutoffQuality || '';
      const def = p.cutoff || '';
      return cq !== def ? 1 : 0;
    },

    // Summary across all override sources, used by the banner above the settings card.
    // Quality counts cutoff diff (when Quality override is active) + per-item diffs.
    pdOverrideSummary() {
      const general = this.pdGeneralActive ? this.pdGeneralChangeCount() : 0;
      const qualityCutoff = this.pdQualityActive ? this.pdQualityChangeCount() : 0;
      const qualityItems = this.pdQualityItemsChangeCount();
      const quality = qualityCutoff + qualityItems;
      const cfScores = Object.keys(this.cfScoreOverrides).length;
      const extraCFs = Object.keys(this.extraCFs).length;
      return { general, quality, cfScores, extraCFs, total: general + quality + cfScores + extraCFs };
    },

    // Toggle-all helper for a Compare card section. If all diffs in the section are currently
    // checked, unchecks them all; otherwise checks them all. Golden Rule variants are preserved
    // (the always-one-picked invariant is never broken).
    toggleAllInCompareSection(iid, cr, section) {
      if (section === 'settings') {
        const sMap = {...(this.instCompareSettingsSelected[iid] || {})};
        const qMap = {...(this.instCompareQualitySelected[iid] || {})};
        const sdDiffs = (cr.settingsDiffs || []).filter(s => !s.match);
        const qdDiffs = (cr.qualityDiffs || []).filter(q => !q.match);
        // Checked means sMap[name] !== false. All currently checked?
        const allChecked = sdDiffs.every(sd => sMap[sd.name] !== false) && qdDiffs.every(qd => qMap[qd.name] !== false);
        for (const sd of sdDiffs) sMap[sd.name] = !allChecked;
        for (const qd of qdDiffs) qMap[qd.name] = !allChecked;
        this.instCompareSettingsSelected = {...this.instCompareSettingsSelected, [iid]: sMap};
        this.instCompareQualitySelected = {...this.instCompareQualitySelected, [iid]: qMap};
      } else if (section === 'requiredCfs') {
        const sel = {...(this.instCompareSelected[iid] || {})};
        const diffs = (cr.formatItems || []).filter(fi => !fi.exists || !fi.scoreMatch);
        const allChecked = diffs.every(fi => sel[fi.trashId] === true);
        for (const fi of diffs) sel[fi.trashId] = !allChecked;
        this.instCompareSelected = {...this.instCompareSelected, [iid]: sel};
      } else if (section === 'groups') {
        const sel = {...(this.instCompareSelected[iid] || {})};
        const diffs = [];
        for (const g of (cr.groups || [])) {
          if (g.exclusive && g.defaultEnabled) continue; // skip Golden Rule — preserve invariant
          for (const cf of g.cfs) {
            if (!cf.exists && g.defaultEnabled && cf.required) diffs.push(cf.trashId);
            else if (cf.exists && cf.inUse && !cf.scoreMatch) diffs.push(cf.trashId);
          }
        }
        const allChecked = diffs.every(tid => sel[tid] === true);
        for (const tid of diffs) sel[tid] = !allChecked;
        this.instCompareSelected = {...this.instCompareSelected, [iid]: sel};
      } else if (section === 'extras') {
        const sel = {...(this.instRemoveSelected[iid] || {})};
        const ids = (cr.extraCFs || []).map(e => e.format);
        const allChecked = ids.every(id => sel[id]);
        for (const id of ids) sel[id] = !allChecked;
        this.instRemoveSelected = {...this.instRemoveSelected, [iid]: sel};
      }
    },

    // Toggle Extra Custom Formats collapse. When collapsing, reset all inner group expand states so
    // the section opens cleanly (all groups collapsed) on next expand.
    toggleExtraCFsCollapsed() {
      if (!this.extraCFsCollapsed) {
        // About to collapse → reset inner group states
        for (const g of (this.extraCFGroups || [])) this.detailSections['extra_' + g.name] = false;
      }
      this.extraCFsCollapsed = !this.extraCFsCollapsed;
    },

    // Compare filter visibility predicates. Called from x-show on CF rows in every diff section.
    compareRowVisible(status) {
      // status: 'match' | 'wrong' | 'missing' | 'extra'
      switch (this.compareFilter) {
        case 'all': return true;
        case 'diff': return status !== 'match';
        case 'wrong': return status === 'wrong';
        case 'missing': return status === 'missing';
        case 'extra': return status === 'extra';
        case 'match': return status === 'match';
        default: return true;
      }
    },
    // Determine status class for a format-item row (required CF or group CF)
    compareFormatItemStatus(fi) {
      if (!fi.exists) return 'missing';
      if (!fi.scoreMatch) return 'wrong';
      return 'match';
    },
    // Build a flat list of rows for the CF Groups table. Each entry is a single <tr>:
    // - { type: 'sub', group } — group header subrow
    // - { type: 'multi', group } — multi-scored warning subrow (exclusive groups only)
    // - { type: 'cf', cf, group } — a CF row
    // Used to keep the table structure HTML-valid (single <tbody>, one row per iteration) while
    // interleaving subheaders and CF rows.
    flatCompareGroupRows(cr) {
      const rows = [];
      for (const group of (cr?.groups || [])) {
        if (!(group.cfs || []).some(cf => this.compareRowVisible(this.compareGroupCFStatus(cf, group)))) continue;
        const isGolden = !!(group.exclusive && group.defaultEnabled);
        rows.push({ type: 'sub', group, isGolden, key: 'h-' + group.name });
        if (group.exclusive && group.cfs.filter(c => c.exists && c.currentScore !== 0).length > 1) {
          rows.push({ type: 'multi', group, key: 'm-' + group.name });
        }
        for (const cf of group.cfs) {
          rows.push({ type: 'cf', cf, group, isGolden, key: 'r-' + group.name + '-' + cf.trashId });
        }
      }
      return rows;
    },

    // Strip HTML tags for use as a plain-text tooltip (TRaSH descriptions are HTML fragments)
    stripHtml(html) {
      if (!html) return '';
      const tmp = document.createElement('div');
      tmp.innerHTML = html;
      return (tmp.textContent || tmp.innerText || '').trim();
    },

    // Status for a CF inside a group. Exclusive default-enabled groups without any in-use variant
    // report ALL variants as 'missing' so they show in diff/wrong/missing filters — the user must pick one.
    // Doesn't rely on group.present (some backends count scored-but-unused); uses cf.inUse directly.
    compareGroupCFStatus(cf, group) {
      if (group.exclusive && group.defaultEnabled) {
        const anyInUse = (group.cfs || []).some(c => c.exists && c.inUse);
        if (!anyInUse) return 'missing';
      }
      if (cf.exists && cf.inUse && cf.scoreMatch) return 'match';
      if (cf.exists && cf.inUse && !cf.scoreMatch) return 'wrong';
      if (!cf.exists && group.defaultEnabled && cf.required) return 'missing';
      return 'match'; // not-in-use / non-required-missing — not a diff for filter purposes
    },

    // Returns compare summary counts for filter chips. Backend already augments Missing with
    // +1 per exclusive-required group without any inUse variant (see handlers.go:1874-1885), so
    // this is a thin pass-through with defensive null/undefined coercion.
    compareAdjustedCounts(cr) {
      const s = cr?.summary || {};
      const missing = s.missing || 0;
      const wrong = s.wrongScore || 0;
      const extra = s.extra || 0;
      const settings = s.settingsDiffs || 0;
      const quality = s.qualityDiffs || 0;
      const matching = s.matching || 0;
      const diffs = wrong + missing + extra + settings + quality;
      return { missing, wrong, extra, settings, quality, matching, diffs, all: matching + diffs };
    },


    pdSetGeneralActive(on) { this.pdGeneralActive = !!on; },
    pdSetQualityActive(on) {
      this.pdQualityActive = !!on;
      // Turning Quality override off also closes the Quality Items editor (structure/overrides stay stored)
      if (!on) this.qualityOverrideActive = false;
    },

    // Effective diff: how many leaf resolutions end up with a different `allowed` state than the
    // TRaSH original after the user's edits. Grouping/rename/reorder alone don't count — only
    // changes that actually affect the sync outcome (which resolutions Arr will see as enabled).
    pdQualityItemsChangeCount() {
      // Flatten a structure to a {leafName → allowed} map. Groups push their allowed down to members.
      const leafMap = (items) => {
        const m = new Map();
        for (const it of items || []) {
          if (it.items && it.items.length > 0) {
            for (const leaf of it.items) m.set(leaf, !!it.allowed);
          } else {
            m.set(it.name, !!it.allowed);
          }
        }
        return m;
      };
      const orig = leafMap(this.profileDetail?.detail?.profile?.items);
      if (this.qualityStructure.length > 0) {
        const cur = leafMap(this.qualityStructure);
        let n = 0;
        for (const [name, allowed] of cur) {
          if (orig.get(name) !== allowed) n++;
        }
        // Leaves dropped entirely from structure (rare) count too
        for (const name of orig.keys()) if (!cur.has(name)) n++;
        return n;
      }
      return Object.keys(this.qualityOverrides).length;
    },

    // Reset General values to TRaSH defaults (keeps override toggle ON so user can re-edit).
    pdResetGeneral() {
      const p = this.profileDetail?.detail?.profile || {};
      this.pdOverrides.language.value = p.language || 'Original';
      this.pdOverrides.upgradeAllowed.value = p.upgradeAllowed ?? true;
      this.pdOverrides.minFormatScore.value = p.minFormatScore ?? 0;
      this.pdOverrides.minUpgradeFormatScore.value = p.minUpgradeFormatScore ?? 1;
      this.pdOverrides.cutoffFormatScore.value = p.cutoffFormatScore || p.cutoffScore || 10000;
    },

    // Reset Quality cutoff to TRaSH default (keeps override toggle ON).
    pdResetQuality() {
      const p = this.profileDetail?.detail?.profile || {};
      this.pdOverrides.cutoffQuality = p.cutoff || '';
    },

    // Full reset: all overrides back to TRaSH, toggles off, editor state cleared.
    // Values stored in pdOverrides are reset to the current profile's defaults.
    pdResetAllOverrides() {
      this.pdResetGeneral();
      this.pdResetQuality();
      this.pdResetDetailState();
    },

    // Clear all profile-detail override flags and transient editor state.
    // Does NOT touch pdOverrides values — caller handles that via pdInitOverrides() if needed.
    // Used by: loadProfileDetail (fresh load), Back-link (leaving the view), pdResetAllOverrides.
    pdResetDetailState() {
      this.pdGeneralActive = false;
      this.pdQualityActive = false;
      this.cfScoreOverrides = {};
      this.cfScoreOverrideActive = false;
      this.qualityOverrides = {};
      this.qualityOverrideActive = false;
      this.qualityOverrideCollapsed = false;
      this.qualityStructure = [];
      this.qualityStructureEditMode = false;
      this.qualityStructureExpanded = {};
      this.qualityStructureRenaming = null;
      this.extraCFs = {};
      this.extraCFsActive = false;
      this.extraCFsCollapsed = false;
      this.extraCFSearch = '';
      this.extraCFAllCFs = [];
      this.extraCFGroups = [];
      this._extraInProfileSet = null;
    },

    // Seed pdOverrides from a profile's TRaSH defaults (or global defaults if no profile).
    pdInitOverrides(p) {
      p = p || {};
      this.pdOverrides = {
        language: { enabled: true, value: p.language || 'Original' },
        upgradeAllowed: { enabled: true, value: p.upgradeAllowed ?? true },
        minFormatScore: { enabled: true, value: p.minFormatScore ?? 0 },
        minUpgradeFormatScore: { enabled: true, value: p.minUpgradeFormatScore ?? 1 },
        cutoffFormatScore: { enabled: true, value: p.cutoffFormatScore || p.cutoffScore || 10000 },
        cutoffQuality: p.cutoff || '',
      };
    },

    // ======================================================================
    // Quality Structure Override (full structure replacing TRaSH items)
    // ======================================================================

    // Initialize qualityStructure from the current profile's items.
    // Bakes any legacy flat overrides into the structure, then clears them.
    qsInitFromProfile() {
      const items = this.profileDetail?.detail?.profile?.items || [];
      if (items.length === 0) return;
      this.qualityStructure = items.map(it => {
        // Apply legacy flat override if present
        const legacy = this.qualityOverrides[it.name];
        const allowed = (legacy !== undefined) ? legacy : !!it.allowed;
        const out = { _id: ++this._qsIdCounter, name: it.name, allowed };
        if (it.items && it.items.length > 0) {
          out.items = [...it.items];
        }
        return out;
      });
      // Now that legacy is migrated, clear it (we only want one source of truth)
      this.qualityOverrides = {};
    },

    // Trash default lookup helper (used for "is overridden" indicator)
    qsTrashDefaultFor(name) {
      const items = this.profileDetail?.detail?.profile?.items || [];
      return items.find(i => i.name === name);
    },

    qsIsOverridden(item) {
      const def = this.qsTrashDefaultFor(item.name);
      if (!def) return true; // user-created group
      if (!!def.allowed !== !!item.allowed) return true;
      const a = (def.items || []).slice().sort().join('|');
      const b = (item.items || []).slice().sort().join('|');
      return a !== b;
    },

    // Returns true if the TRaSH default cutoff name is a valid allowed entry given the current state.
    // When no structure override is active, the TRaSH default is always valid (sourced from profile.items).
    // When structure override is active, it's only valid if a top-level allowed item with that exact name exists.
    qsTrashDefaultCutoffValid() {
      const trashCutoff = this.profileDetail?.detail?.profile?.cutoff || '';
      if (!trashCutoff) return false;
      if (this.qualityStructure.length === 0) return true;
      return this.qualityStructure.some(it => it.name === trashCutoff && it.allowed);
    },

    // Validate pdOverrides.cutoffQuality against the current source-of-truth.
    // If invalid (name no longer exists or is disabled), reset to first allowed entry from
    // the structure (or back to TRaSH default if structure is empty).
    // Triggered reactively whenever qualityStructure changes (rename, delete, merge, etc.).
    qsValidateCutoff() {
      const cutoff = this.pdOverrides?.cutoffQuality;
      if (cutoff === undefined || cutoff === '__skip__' || cutoff === '') return;

      // When no structure override is active, fall back to profile.items as source
      const source = this.qualityStructure.length > 0
        ? this.qualityStructure
        : (this.profileDetail?.detail?.profile?.items || []);
      if (source.length === 0) return;

      // Check if current cutoff is a valid allowed entry
      const valid = source.some(it => it.name === cutoff && it.allowed);
      if (valid) return;

      // Not valid — pick first allowed as fallback
      const firstAllowed = source.find(it => it.allowed);
      this.pdOverrides.cutoffQuality = firstAllowed ? firstAllowed.name : '';
    },

    // Toggle Edit Groups mode. On first activation, lazy-init structure from TRaSH default.
    qsToggleEditMode() {
      if (!this.qualityStructureEditMode && this.qualityStructure.length === 0) {
        this.qsInitFromProfile();
      }
      this.qualityStructureEditMode = !this.qualityStructureEditMode;
      if (!this.qualityStructureEditMode) {
        this.qualityStructureExpanded = {};
        this.qualityStructureRenaming = null;
        this.qsResetDrag();
      }
    },

    qsStartRename(item) {
      this.qualityStructureRenaming = item._id;
    },

    qsResetDrag() {
      this.qualityStructureDrag = { kind: null, src: null, srcGroup: null, srcMember: null, dropGap: null, dropMerge: null };
    },

    // Shared qs editor state (editMode / expanded / renaming) is used by BOTH the Builder's
    // inline editor and the Edit view's inline editor. Must be cleared whenever either editor
    // closes — otherwise re-opening the other one lands mid-edit with drag handles visible.
    qsCloseSharedState() {
      this.qualityStructureEditMode = false;
      this.qualityStructureExpanded = {};
      this.qualityStructureRenaming = null;
      this.qsResetDrag();
    },

    // Drag-drop on a gap → reorder (or ungroup-and-insert if dragging a member)
    // Resolve the target array for quality-editor helpers. 'edit' = profile-detail's qualityStructure,
    // 'builder' = Profile Builder's pb.qualityItems. Both share the same shape { name, allowed, items? }
    // and the same editor UI state (qualityStructureEditMode/Expanded/Renaming/Drag) — only one
    // editor is open at a time so shared state is safe.
    _qsArr(target) { return target === 'builder' ? this.pb.qualityItems : this.qualityStructure; },
    _qsSetArr(target, v) {
      if (target === 'builder') this.pb.qualityItems = v;
      else this.qualityStructure = v;
    },

    qsHandleDropOnGap(gapIdx, target = 'edit') {
      const d = this.qualityStructureDrag;
      const arr = this._qsArr(target);
      if (d.kind === 'top') {
        const src = d.src;
        if (src === gapIdx || src === gapIdx - 1) { this.qsResetDrag(); return; }
        const moved = arr.splice(src, 1)[0];
        const insertAt = src < gapIdx ? gapIdx - 1 : gapIdx;
        arr.splice(insertAt, 0, moved);
      } else if (d.kind === 'member') {
        const grp = arr[d.srcGroup];
        if (!grp || !grp.items) { this.qsResetDrag(); return; }
        const memberName = grp.items.splice(d.srcMember, 1)[0];
        const newSingle = { _id: ++this._qsIdCounter, name: memberName, allowed: false };
        let insertAt = gapIdx;
        if (grp.items.length === 0) {
          arr.splice(d.srcGroup, 1);
          if (d.srcGroup < gapIdx) insertAt -= 1;
        }
        arr.splice(insertAt, 0, newSingle);
      }
      this.qsResetDrag();
    },

    // Drag-drop on a row → merge (create group if both singles, add to group otherwise)
    qsHandleDropOnRow(targetIdx, target = 'edit') {
      const d = this.qualityStructureDrag;
      const arr = this._qsArr(target);
      if (d.kind === 'top') {
        const src = d.src;
        if (src === targetIdx) { this.qsResetDrag(); return; }
        const srcItem = arr[src];
        const tgtItem = arr[targetIdx];
        if (tgtItem.items) {
          const newMembers = srcItem.items ? srcItem.items : [srcItem.name];
          tgtItem.items.push(...newMembers);
          arr.splice(src, 1);
        } else if (srcItem.items) {
          srcItem.items.push(tgtItem.name);
          arr.splice(targetIdx, 1);
        } else {
          const defaultName = `${srcItem.name} | ${tgtItem.name}`;
          this.inputModal = {
            show: true,
            title: 'New Quality Group',
            message: 'Both qualities will be merged into a single group. Arr will treat them as equal — CF scores decide the winner.',
            value: defaultName,
            placeholder: 'Group name',
            confirmLabel: 'Create',
            onConfirm: (groupName) => {
              if (!groupName) return;
              const newGroup = {
                _id: ++this._qsIdCounter,
                name: groupName,
                allowed: true,
                items: [srcItem.name, tgtItem.name],
              };
              const indices = [src, targetIdx].sort((a, b) => b - a);
              indices.forEach(i => arr.splice(i, 1));
              const insertAt = Math.min(src, targetIdx);
              arr.splice(insertAt, 0, newGroup);
              this.qualityStructureExpanded[newGroup._id] = true;
            },
            onCancel: null,
          };
        }
      } else if (d.kind === 'member') {
        const oldGroup = arr[d.srcGroup];
        if (!oldGroup || !oldGroup.items) { this.qsResetDrag(); return; }
        const memberName = oldGroup.items.splice(d.srcMember, 1)[0];
        let tIdx = targetIdx;
        if (oldGroup.items.length === 0) {
          arr.splice(d.srcGroup, 1);
          if (d.srcGroup < tIdx) tIdx -= 1;
        }
        const tgtItem = arr[tIdx];
        if (!tgtItem) { this.qsResetDrag(); return; }
        if (tgtItem.items) {
          tgtItem.items.push(memberName);
        } else {
          const defaultName = `${memberName} | ${tgtItem.name}`;
          this.inputModal = {
            show: true,
            title: 'New Quality Group',
            message: 'Both qualities will be merged into a single group.',
            value: defaultName,
            placeholder: 'Group name',
            confirmLabel: 'Create',
            onConfirm: (groupName) => {
              if (!groupName) return;
              const newGroup = {
                _id: ++this._qsIdCounter,
                name: groupName,
                allowed: true,
                items: [memberName, tgtItem.name],
              };
              arr.splice(tIdx, 1, newGroup);
              this.qualityStructureExpanded[newGroup._id] = true;
            },
            onCancel: null,
          };
        }
      }
      this.qsResetDrag();
    },

    qsDeleteGroup(idx, target = 'edit') {
      const arr = this._qsArr(target);
      const grp = arr[idx];
      if (!grp || !grp.items) return;
      const singles = grp.items.map(name => ({
        _id: ++this._qsIdCounter,
        name,
        allowed: false,
      }));
      arr.splice(idx, 1, ...singles);
    },

    qsUngroupMember(groupIdx, memberIdx, target = 'edit') {
      const arr = this._qsArr(target);
      const grp = arr[groupIdx];
      if (!grp || !grp.items) return;
      const removed = grp.items.splice(memberIdx, 1)[0];
      arr.splice(groupIdx + 1, 0, {
        _id: ++this._qsIdCounter,
        name: removed,
        allowed: false,
      });
      if (grp.items.length === 0) {
        arr.splice(groupIdx, 1);
      }
    },

    // Reset all quality overrides to TRaSH default. Clears both legacy and structure overrides.
    // Target 'edit' resets qualityStructure + qualityOverrides. Target 'builder' clears
    // pb.qualityItems (user will need to re-apply template to repopulate).
    qsResetAll(target = 'edit') {
      this.confirmModal = {
        show: true,
        title: target === 'builder' ? 'Reset Quality Items' : 'Reset Quality Overrides',
        message: target === 'builder'
          ? 'Clear all quality items?\n\nThis removes the current qualities and groups. Re-apply a template or preset to repopulate.'
          : 'Reset to TRaSH default?\n\nAll override structure changes (toggles, groups, ordering, renames) will be discarded. This cannot be undone.',
        confirmLabel: 'Reset',
        onConfirm: () => {
          if (target === 'builder') {
            this.pb.qualityItems = [];
          } else {
            this.qualityStructure = [];
            this.qualityOverrides = {};
          }
          this.qualityStructureExpanded = {};
          this.qualityStructureRenaming = null;
          this.qsResetDrag();
        },
        onCancel: null,
      };
    },

    // Strip _id before sending to backend (backend doesn't need it)
    qsForBackend() {
      return this.qualityStructure.map(it => {
        const out = { name: it.name, allowed: it.allowed };
        if (it.items && it.items.length > 0) out.items = [...it.items];
        return out;
      });
    },

    updateAutoSyncRuleForSync() {
      this.autoSyncRuleForSync = this.findAutoSyncRule(
        this.syncForm.instanceId,
        this.syncForm.arrProfileId
      );
      // Populate behavior from existing auto-sync rule
      if (this.autoSyncRuleForSync?.behavior) {
        this.syncForm.behavior = { ...this.syncForm.behavior, ...this.autoSyncRuleForSync.behavior };
      }
    },

    async toggleAutoSyncForProfile(enabled) {
      this.debugLog('UI', `Auto-sync: ${enabled ? 'enabled' : 'disabled'} for "${this.syncForm.profileName}" → ${this.syncForm.instanceName}`);
      const existing = this.autoSyncRuleForSync;
      if (existing) {
        // Update existing rule — toggle enabled and update settings
        const syncBody = this.buildSyncBody();
        const updated = {
          ...existing,
          enabled: enabled,
          selectedCFs: this.getAllSelectedCFIds(),
          behavior: this.syncForm.behavior,
          overrides: syncBody.overrides || null,
          scoreOverrides: syncBody.scoreOverrides || null,
          qualityOverrides: syncBody.qualityOverrides || null,
          qualityStructure: syncBody.qualityStructure || null,
          arrProfileId: parseInt(this.syncForm.arrProfileId) || existing.arrProfileId
        };
        try {
          await fetch(`/api/auto-sync/rules/${existing.id}`, {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(updated)
          });
          await this.loadAutoSyncRules();
          this.updateAutoSyncRuleForSync();
        } catch (e) { console.error('toggleAutoSyncForProfile:', e); }
      } else if (enabled) {
        // No rule exists yet — create one
        const syncBody = this.buildSyncBody();
        const rule = {
          enabled: true,
          instanceId: this.syncForm.instanceId,
          profileSource: this.syncForm.importedProfileId ? 'imported' : 'trash',
          trashProfileId: this.syncForm.profileTrashId || '',
          importedProfileId: this.syncForm.importedProfileId || '',
          arrProfileId: parseInt(this.syncForm.arrProfileId) || 0,
          selectedCFs: this.getAllSelectedCFIds(),
          behavior: this.syncForm.behavior,
          overrides: syncBody.overrides || null,
          scoreOverrides: syncBody.scoreOverrides || null,
          qualityOverrides: syncBody.qualityOverrides || null,
          qualityStructure: syncBody.qualityStructure || null
        };
        try {
          const r = await fetch('/api/auto-sync/rules', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify(rule)
          });
          if (r.ok) {
            await this.loadAutoSyncRules();
            this.updateAutoSyncRuleForSync();
          }
        } catch (e) { console.error('createAutoSyncRule:', e); }
      }
    },

    async toggleAutoSyncRule(rule) {
      const wasEnabled = rule.enabled;
      rule.enabled = !rule.enabled;
      try {
        await fetch(`/api/auto-sync/rules/${rule.id}`, {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(rule)
        });
        await this.loadAutoSyncRules();
        // Force reactivity update on sync history (toggle text depends on autoSyncRules)
        const instId = rule.instanceId;
        if (this.syncHistory[instId]) {
          this.syncHistory = { ...this.syncHistory };
        }
        // If just enabled, run sync immediately instead of waiting for next pull
        if (!wasEnabled && rule.enabled) {
          const sh = (this.syncHistory[instId] || []).find(s => s.arrProfileId === rule.arrProfileId);
          if (sh) {
            const inst = this.instances.find(i => i.id === instId);
            if (inst) {
              await this.quickSync(inst, sh);
            }
          }
        }
      } catch (e) { console.error('toggleAutoSyncRule:', e); }
    },

    async deleteAutoSyncRule(rule) {
      const confirmed = await new Promise(resolve => {
        this.confirmModal = { show: true, title: 'Remove Auto-Sync Rule', message: 'Remove this auto-sync rule?', confirmLabel: 'Remove', onConfirm: () => resolve(true), onCancel: () => resolve(false) };
      });
      if (!confirmed) return;
      try {
        await fetch(`/api/auto-sync/rules/${rule.id}`, { method: 'DELETE' });
        await this.loadAutoSyncRules();
      } catch (e) { console.error('deleteAutoSyncRule:', e); }
    },

    // Debug logging helper — fire-and-forget POST to backend
    debugLog(category, message) {
      if (!this.config?.debugLogging) return;
      fetch('/api/debug/log', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ category, message })
      }).catch(() => {});
    },

    timeAgo(isoString) {
      if (!isoString) return 'never';
      void this._nowTick; // reactive dependency — triggers re-render every 30s
      const diff = Date.now() - new Date(isoString).getTime();
      const mins = Math.floor(diff / 60000);
      if (mins < 1) return 'just now';
      if (mins < 60) return mins + 'm ago';
      const hours = Math.floor(mins / 60);
      if (hours < 24) return hours + 'h ago';
      const days = Math.floor(hours / 24);
      return days + 'd ago';
    },

    nextPullTime() {
      void this._nowTick;
      const interval = this.config.pullInterval;
      const lastPull = this.trashStatus?.lastPull;
      if (!interval || interval === 'off' || !lastPull) return '';
      const match = interval.match(/^(\d+)(m|h)$/);
      if (!match) return '';
      const ms = parseInt(match[1]) * (match[2] === 'h' ? 3600000 : 60000);
      const next = new Date(lastPull).getTime() + ms;
      const diff = next - Date.now();
      if (diff <= 0) return 'soon';
      const mins = Math.floor(diff / 60000);
      if (mins < 60) return mins + 'm';
      const hours = Math.floor(mins / 60);
      const remMins = mins % 60;
      return remMins > 0 ? hours + 'h ' + remMins + 'm' : hours + 'h';
    },

    formatCommitDate(dateStr) {
      if (!dateStr) return '';
      try {
        const d = new Date(dateStr);
        return d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short', year: 'numeric' });
      } catch { return dateStr; }
    },

    truncateWord(str, max) {
      if (!str || str.length <= max) return str;
      const cut = str.lastIndexOf(' ', max);
      return (cut > 0 ? str.slice(0, cut) : str.slice(0, max)) + '...';
    },

    formatSyncTime(isoString) {
      if (!isoString) return 'never';
      try {
        const d = new Date(isoString);
        return d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short' }) + ' ' +
               d.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' });
      } catch { return isoString; }
    },

    formatChangelogDate(dateStr) {
      if (!dateStr) return '';
      try {
        const d = new Date(dateStr + 'T00:00:00');
        return d.toLocaleDateString('en-GB', { day: 'numeric', month: 'long', year: 'numeric' });
      } catch { return dateStr; }
    },

    // --- Category Toggles ---

    isCategoryEnabled(cat) {
      return cat.groups.some(g => g.cfs.some(cf => this.selectedOptionalCFs[cf.trashId]));
    },

    toggleCategory(cat) {
      const anyEnabled = this.isCategoryEnabled(cat);
      const updated = { ...this.selectedOptionalCFs };
      for (const group of cat.groups) {
        for (const cf of group.cfs) {
          updated[cf.trashId] = !anyEnabled;
        }
      }
      this.selectedOptionalCFs = updated;
    },

    // --- Group Toggles ---

    isGroupEnabled(category, groupName) {
      const cats = this.profileDetail?.detail?.cfCategories || [];
      const cat = cats.find(c => c.category === category);
      if (!cat) return false;
      const group = cat.groups.find(g => g.shortName === groupName);
      if (!group) return false;
      return group.cfs.some(cf => this.selectedOptionalCFs[cf.trashId]);
    },

    toggleGroup(category, groupName, cfs) {
      const anySelected = cfs.some(cf => this.selectedOptionalCFs[cf.trashId]);
      const updated = { ...this.selectedOptionalCFs };
      for (const cf of cfs) {
        updated[cf.trashId] = !anySelected;
      }
      this.selectedOptionalCFs = updated;
    },

    // --- Scoring Sandbox ---

    async loadSandbox(appType) {
      const sb = this.sandbox[appType];
      // Default to first instance of this type
      if (!sb.instanceId) {
        const insts = this.instancesOfType(appType);
        if (insts.length > 0) sb.instanceId = insts[0].id;
      }
      // Load Prowlarr indexers if enabled and not loaded
      if (this.config.prowlarr?.enabled && sb.indexers.length === 0) {
        try {
          const r = await fetch('/api/scoring/prowlarr/indexers');
          if (r.ok) sb.indexers = await r.json();
        } catch (e) { /* ignore */ }
      }
      // Load instance profiles for the "Score against" dropdown
      if (sb.instanceId && sb.instanceProfiles.length === 0) {
        try {
          const r = await fetch(`/api/instances/${sb.instanceId}/profiles`);
          if (r.ok) sb.instanceProfiles = await r.json();
        } catch (e) { /* ignore */ }
      }
    },

    async sandboxInstanceChanged(appType) {
      const sb = this.sandbox[appType];
      sb.instanceProfiles = [];
      if (sb.instanceId) {
        try {
          const r = await fetch(`/api/instances/${sb.instanceId}/profiles`);
          if (r.ok) sb.instanceProfiles = await r.json();
        } catch (e) { /* ignore */ }
      }
      // Re-score if using instance profile
      if (sb.profileKey?.startsWith('inst:')) {
        sb.profileKey = '';
      }
      this.rescoreSandbox(appType);
    },

    sandboxTrashProfiles(appType) {
      return (this.trashProfiles[appType] || []).map(p => ({ trashId: p.trashId, name: p.name }));
    },

    sandboxImportedProfiles(appType) {
      return (this.importedProfiles[appType] || []).map(p => ({ id: p.id, name: p.name }));
    },

    // Stamp stable _sid on sandbox results for :key tracking during drag reorder.
    _sbEnsureIds(results) {
      for (const r of results) {
        if (!r._sid) r._sid = ++this._sbIdCounter;
      }
      return results;
    },

    // Sorted results. sortCol 'manual' (or empty) preserves the underlying sb.results
    // order — set by drag-reorder so manual ordering survives until the user clicks
    // a column header to re-sort.
    sortedSandboxResults(appType) {
      const sb = this.sandbox[appType];
      const results = [...(sb.results || [])];
      const col = sb.sortCol;
      if (!col || col === 'manual') return results;
      const dir = sb.sortDir === 'asc' ? 1 : -1;
      results.sort((a, b) => {
        switch (col) {
          case 'score': return dir * ((a.scoring?.total ?? -99999) - (b.scoring?.total ?? -99999));
          case 'status': {
            const aPass = (a.scoring?.total ?? 0) >= (a.scoring?.minScore || 0) ? 1 : 0;
            const bPass = (b.scoring?.total ?? 0) >= (b.scoring?.minScore || 0) ? 1 : 0;
            return dir * (aPass - bPass);
          }
          case 'quality': return dir * (a.parsed?.quality || '').localeCompare(b.parsed?.quality || '');
          case 'group': return dir * (a.parsed?.releaseGroup || '').localeCompare(b.parsed?.releaseGroup || '');
          case 'title': return dir * a.title.localeCompare(b.title);
        }
        return 0;
      });
      return results;
    },

    // Sort then apply the "Show selected only" filter. Table uses this instead of
    // sortedSandboxResults directly so the filter lives in one place.
    visibleSandboxResults(appType) {
      const sb = this.sandbox[appType];
      this._sbEnsureIds(sb.results || []);
      let results = this.sortedSandboxResults(appType);
      if (sb.filterToSelected) results = results.filter(r => r._selected === true);
      return results;
    },

    sandboxSelectedCount(appType) {
      return (this.sandbox[appType].results || []).filter(r => r._selected === true).length;
    },

    toggleSandboxSelectAll(appType) {
      const sb = this.sandbox[appType];
      const all = (sb.results || []);
      const allSelected = all.length > 0 && all.every(r => r._selected === true);
      all.forEach(r => { r._selected = !allSelected; });
      // trigger reactivity — mutating props in place isn't always picked up
      sb.results = [...all];
    },

    toggleSandboxSort(appType, col) {
      const sb = this.sandbox[appType];
      if (sb.sortCol === col) {
        sb.sortDir = sb.sortDir === 'asc' ? 'desc' : 'asc';
      } else {
        sb.sortCol = col;
        sb.sortDir = col === 'title' || col === 'group' ? 'asc' : 'desc';
      }
    },

    // Format a single sandbox result as a readable plain-text block for sharing.
    // Includes the full title, parsed metadata, scores (primary profile + compare
    // if active), and the matched/unmatched CF breakdown. Monospace-friendly.
    formatSandboxResultForCopy(appType, res) {
      const sb = this.sandbox[appType];
      const lines = [];
      lines.push(res.title);
      lines.push('');
      const p = res.parsed || {};
      if (p.quality)      lines.push('Quality:      ' + p.quality);
      if (p.releaseGroup) lines.push('Group:        ' + p.releaseGroup);
      if (p.languages?.length) lines.push('Languages:    ' + p.languages.join(', '));
      if (p.edition)      lines.push('Edition:      ' + p.edition);
      const scoreLine = (label, s) => {
        if (!s) return;
        const status = (s.total ?? 0) >= (s.minScore || 0) ? 'PASS' : 'FAIL';
        lines.push(`${label.padEnd(13)} ${s.total} (${status}, min: ${s.minScore || 0})`);
      };
      scoreLine('Score:', res.scoring);
      if (sb.compareKey && res.scoringB) {
        const cmpName = this.sandboxCompareProfileName(appType) || 'Compare';
        scoreLine(cmpName.slice(0, 12) + ':', res.scoringB);
      }
      const breakdown = res.scoring?.breakdown || [];
      const matched = breakdown.filter(b => b.matched);
      const unmatched = breakdown.filter(b => !b.matched && b.score !== 0);
      if (matched.length) {
        lines.push('');
        lines.push('Matched CFs:');
        for (const b of matched) {
          const sgn = b.score > 0 ? '+' : '';
          lines.push(`  ${(sgn + b.score).padStart(6)}  ${b.name}`);
        }
      }
      if (unmatched.length) {
        lines.push('');
        lines.push('Unmatched (in profile, not in release):');
        for (const b of unmatched) {
          const sgn = b.score > 0 ? '+' : '';
          lines.push(`  ${(sgn + b.score).padStart(6)}  ${b.name}`);
        }
      }
      return lines.join('\n');
    },

    openSandboxCopy(appType, res) {
      this.sandboxCopyModal = {
        show: true,
        title: res.title,
        text: this.formatSandboxResultForCopy(appType, res),
        copied: false,
      };
    },

    copySandboxModalText() {
      copyToClipboard(this.sandboxCopyModal.text);
      this.sandboxCopyModal.copied = true;
      setTimeout(() => { this.sandboxCopyModal.copied = false; }, 1500);
    },

    // Drag-reorder rows. Works only when sortCol is 'manual' (or user just dropped —
    // we set it to 'manual' so the drag outcome sticks). Operates on the underlying
    // sb.results array by matching the dragged/target result objects (identity-safe).
    sandboxDragStart(appType, res) {
      this.sandbox[appType].dragSrc = res;
    },
    sandboxDragOver(appType, res) {
      this.sandbox[appType].dragOver = res;
    },
    sandboxDrop(appType, targetRes) {
      const sb = this.sandbox[appType];
      const src = sb.dragSrc;
      sb.dragSrc = null;
      sb.dragOver = null;
      if (!src || src === targetRes) return;
      const arr = [...(sb.results || [])];
      const fromIdx = arr.indexOf(src);
      const toIdx = arr.indexOf(targetRes);
      if (fromIdx < 0 || toIdx < 0) return;
      arr.splice(fromIdx, 1);
      arr.splice(toIdx, 0, src);
      sb.results = arr;
      sb.sortCol = 'manual'; // exit sorted view so the drag order sticks
      this.saveSandboxResults(appType);
    },

    async sandboxParse(appType) {
      const sb = this.sandbox[appType];
      const title = sb.pasteInput?.trim();
      if (!title || !sb.instanceId) return;
      sb.parsing = true;
      try {
        const r = await fetch('/api/scoring/parse', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ instanceId: sb.instanceId, title })
        });
        if (!r.ok) { const e = await r.json().catch(() => ({})); this.showToast(e.error || 'Parse failed', 'error', 8000); return; }
        const result = await r.json();
        const scored = await this.calculateScoring(result, appType);
        sb.results = [scored, ...sb.results];
        this.saveSandboxResults(appType);
        sb.pasteInput = '';
      } catch (e) { this.showToast('Parse error: ' + e.message, 'error', 8000); }
      finally { sb.parsing = false; }
    },

    async sandboxParseBulk(appType) {
      const sb = this.sandbox[appType];
      const lines = (sb.bulkInput || '').split('\n').map(l => l.trim()).filter(Boolean);
      if (lines.length === 0 || !sb.instanceId) return;
      sb.parsing = true;
      try {
        const r = await fetch('/api/scoring/parse/batch', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ instanceId: sb.instanceId, titles: lines })
        });
        if (!r.ok) { const e = await r.json().catch(() => ({})); this.showToast(e.error || 'Batch parse failed', 'error', 8000); return; }
        const results = await r.json();
        const scored = await Promise.all(results.map(result => this.calculateScoring(result, appType)));
        sb.results = [...scored, ...sb.results];
        this.saveSandboxResults(appType);
        sb.bulkInput = '';
      } catch (e) { this.showToast('Batch parse error: ' + e.message, 'error', 8000); }
      finally { sb.parsing = false; }
    },

    sandboxIndexerLabel(appType) {
      const sb = this.sandbox[appType];
      const sel = sb.selectedIndexers || [];
      const all = sb.indexers || [];
      if (sel.length === 0 || sel.length === all.length) return 'All Indexers';
      if (sel.length === 1) {
        const idx = all.find(i => i.id === sel[0]);
        return idx ? idx.name : '1 indexer';
      }
      return sel.length + ' indexers';
    },

    sandboxToggleIndexer(appType, id) {
      const sb = this.sandbox[appType];
      if (!sb.selectedIndexers) sb.selectedIndexers = [];
      const i = sb.selectedIndexers.indexOf(id);
      if (i >= 0) {
        sb.selectedIndexers.splice(i, 1);
      } else {
        sb.selectedIndexers.push(id);
      }
    },

    sandboxToggleAllIndexers(appType) {
      const sb = this.sandbox[appType];
      const all = (sb.indexers || []).map(i => i.id);
      if (sb.selectedIndexers?.length === all.length) {
        sb.selectedIndexers = [];
      } else {
        sb.selectedIndexers = [...all];
      }
    },

    async sandboxSearch(appType) {
      const sb = this.sandbox[appType];
      const query = sb.searchQuery?.trim();
      if (!query) return;
      if (sb.searchAbort) sb.searchAbort.abort();
      const abort = new AbortController();
      sb.searchAbort = abort;
      sb.searching = true;
      sb.searchError = '';
      sb.searchResults = [];
      sb.searchFilterText = '';
      sb.searchFilterRes = '';
      sb.indexerDropdown = false;
      try {
        // Categories: use user override from Settings if set, else Newznab defaults
        // (2000 = Movies root, 5000 = TV root). Some private-tracker indexer definitions
        // don't cascade the parent ID to sub-categories, so users may need to specify
        // sub-IDs explicitly (e.g. 2040, 2045) for searches to return results.
        const defaultCats = appType === 'radarr' ? [2000] : [5000];
        const override = appType === 'radarr'
          ? this.config.prowlarr?.radarrCategories
          : this.config.prowlarr?.sonarrCategories;
        const categories = (override && override.length > 0) ? override : defaultCats;
        const indexerIds = sb.selectedIndexers?.length > 0 ? sb.selectedIndexers : [];
        const r = await fetch('/api/scoring/prowlarr/search', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ query, categories, indexerIds }),
          signal: abort.signal
        });
        if (!r.ok) { const e = await r.json().catch(() => ({})); sb.searchError = e.error || 'Search failed'; return; }
        const results = await r.json();
        sb.searchResults = results.map(r => ({ ...r, _selected: false }));
      } catch (e) {
        if (e.name === 'AbortError') { sb.searchError = ''; return; }
        sb.searchError = 'Search error: ' + e.message;
      }
      finally { sb.searching = false; sb.searchAbort = null; }
    },

    sandboxCancelSearch(appType) {
      const sb = this.sandbox[appType];
      if (sb.searchAbort) { sb.searchAbort.abort(); sb.searchAbort = null; }
      sb.searching = false;
    },

    filteredSearchResults(appType) {
      const sb = this.sandbox[appType];
      let results = sb.searchResults || [];
      const text = sb.searchFilterText?.trim().toLowerCase();
      if (text) results = results.filter(r => r.title.toLowerCase().includes(text));
      const res = sb.searchFilterRes;
      if (res) {
        // Match exact resolution token — not source descriptors like "UHD BluRay"
        const patterns = {
          '2160p': /\b2160p\b/i,
          '1080p': /\b1080p\b/i,
          '720p': /\b720p\b/i,
          '480p': /\b480p\b/i,
        };
        const pat = patterns[res];
        if (pat) results = results.filter(r => pat.test(r.title));
      }
      return results;
    },

    saveSandboxResults(appType) {
      const sb = this.sandbox[appType];
      const data = (sb.results || []).map(r => ({ title: r.title, parsed: r.parsed, matchedCFs: r.matchedCFs, instanceScore: r.instanceScore }));
      try { localStorage.setItem('clonarr-sandbox-' + appType, JSON.stringify(data)); } catch (e) {}
    },

    async loadSandboxResults(appType) {
      try {
        const raw = localStorage.getItem('clonarr-sandbox-' + appType);
        if (!raw) return;
        const data = JSON.parse(raw);
        if (!Array.isArray(data) || data.length === 0) return;
        const sb = this.sandbox[appType];
        sb.results = data;
        // Re-apply scoring if profile is selected
        if (sb.profileKey) {
          const profileData = await this.fetchProfileScores(sb.profileKey, appType);
          sb.results = sb.results.map(res => this.applyScoring(res, profileData));
        }
      } catch (e) {}
    },

    async sandboxScoreSelected(appType) {
      const sb = this.sandbox[appType];
      const selected = (sb.searchResults || []).filter(r => r._selected);
      if (selected.length === 0 || !sb.instanceId) return;
      sb.parsing = true;
      try {
        const titles = selected.map(r => r.title);
        const r = await fetch('/api/scoring/parse/batch', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ instanceId: sb.instanceId, titles })
        });
        if (!r.ok) { const e = await r.json().catch(() => ({})); this.showToast(e.error || 'Parse failed', 'error', 8000); return; }
        const results = await r.json();
        const scored = await Promise.all(results.map(result => this.calculateScoring(result, appType)));
        sb.results = [...scored, ...sb.results];
        this.saveSandboxResults(appType);
        // Clear selections
        sb.searchResults.forEach(r => r._selected = false);
      } catch (e) { this.showToast('Score error: ' + e.message, 'error', 8000); }
      finally { sb.parsing = false; }
    },

    // Profile score cache: { "radarr:trash:abc123": { scores: [{trashId, name, score}], minScore: 0 } }
    _profileScoreCache: {},

    async fetchProfileScores(profileKey, appType) {
      const cacheKey = appType + ':' + profileKey;
      if (this._profileScoreCache[cacheKey]) return this._profileScoreCache[cacheKey];
      const sb = this.sandbox[appType];
      const params = new URLSearchParams({ profileKey, appType });
      if (profileKey.startsWith('inst:')) params.set('instanceId', sb.instanceId);
      try {
        const r = await fetch('/api/scoring/profile-scores?' + params);
        if (!r.ok) return { scores: [], minScore: 0 };
        const data = await r.json();
        this._profileScoreCache[cacheKey] = data;
        return data;
      } catch (e) { return { scores: [], minScore: 0 }; }
    },

    async rescoreSandbox(appType) {
      const sb = this.sandbox[appType];
      if (!sb.results?.length || !sb.profileKey) return;
      const cacheKey = appType + ':' + sb.profileKey;
      delete this._profileScoreCache[cacheKey];
      const profileData = await this.fetchProfileScores(sb.profileKey, appType);
      sb.results = sb.results.map(res => this.applyScoring(res, profileData));
      // Re-score compare profile too
      if (sb.compareKey) this.rescoreCompare(appType);
    },

    async rescoreCompare(appType) {
      const sb = this.sandbox[appType];
      if (!sb.results?.length || !sb.compareKey) {
        sb.results = sb.results.map(res => { const r = {...res}; delete r.scoringB; return r; });
        return;
      }
      const cacheKey = appType + ':' + sb.compareKey;
      delete this._profileScoreCache[cacheKey];
      const profileData = await this.fetchProfileScores(sb.compareKey, appType);
      sb.results = sb.results.map(res => {
        const scored = this.applyScoring(res, profileData);
        return { ...res, scoringB: scored.scoring };
      });
    },

    async toggleSandboxEdit(appType) {
      const sb = this.sandbox[appType];
      if (sb.editOpen) {
        sb.editOpen = false;
        // Re-score with original profile to undo edits
        await this.rescoreSandbox(appType);
        return;
      }
      if (!sb.profileKey) return;
      const profileData = await this.fetchProfileScores(sb.profileKey, appType);
      sb.editOriginal = JSON.parse(JSON.stringify(profileData));
      sb.editScores = {};
      sb.editToggles = {};
      sb.editMinScore = null;
      sb.editOpen = true;
    },

    resetSandboxEdit(appType) {
      const sb = this.sandbox[appType];
      sb.editScores = {};
      sb.editToggles = {};
      sb.editMinScore = null;
      this.applySandboxEdit(appType);
    },

    _sandboxEditTimer: null,
    debounceSandboxEdit(appType) {
      clearTimeout(this._sandboxEditTimer);
      this._sandboxEditTimer = setTimeout(() => this.applySandboxEdit(appType), 200);
    },

    applySandboxEdit(appType) {
      const sb = this.sandbox[appType];
      if (!sb.editOriginal || !sb.results?.length) return;
      // Build modified profile data from original + edits
      const modified = {
        scores: sb.editOriginal.scores
          .filter(s => sb.editToggles[s.trashId || s.name] !== false)
          .map(s => ({
            ...s,
            score: sb.editScores[s.trashId || s.name] ?? s.score
          })),
        minScore: sb.editMinScore ?? sb.editOriginal.minScore ?? 0
      };
      // Add any extra CFs added by user
      for (const key of Object.keys(sb.editToggles)) {
        if (sb.editToggles[key] === 'added') {
          modified.scores.push({ trashId: key, name: sb._addedCFNames?.[key] || key, score: sb.editScores[key] ?? 0 });
        }
      }
      sb.results = sb.results.map(res => this.applyScoring(res, modified));
    },

    _sandboxCFCache: {},
    _trashScoreContextCache: {},
    async openSandboxCFBrowser(appType) {
      const sb = this.sandbox[appType];
      const selected = {};
      const scores = {};
      const inProfile = {};
      // Mark CFs already in the profile (show as ON + disabled)
      for (const s of (sb.editOriginal?.scores || [])) {
        const key = s.trashId || s.name;
        selected[key] = true;
        scores[key] = sb.editScores[key] ?? s.score;
        inProfile[key] = true;
      }
      // Also mark CFs added via editToggles
      for (const key of Object.keys(sb.editToggles)) {
        if (sb.editToggles[key] === 'added') {
          selected[key] = true;
          scores[key] = sb.editScores[key] ?? 0;
        }
      }
      this.sandboxCFBrowser = { open: true, appType, categories: [], customCFs: [], selected, scores, inProfile, expanded: {}, filter: '' };
      // Fetch categories + custom CFs
      try {
        const [cfRes, customRes] = await Promise.all([
          fetch(`/api/trash/${appType}/all-cfs`),
          fetch(`/api/custom-cfs/${appType}`)
        ]);
        if (cfRes.ok) {
          const data = await cfRes.json();
          this.sandboxCFBrowser.categories = data.categories || [];
        }
        if (customRes.ok) {
          this.sandboxCFBrowser.customCFs = await customRes.json() || [];
        }
      } catch (e) { console.error('openSandboxCFBrowser:', e); }
    },

    closeSandboxCFBrowser() {
      const br = this.sandboxCFBrowser;
      const sb = this.sandbox[br.appType];
      if (!sb) { br.open = false; return; }
      // Apply selected CFs to edit state
      if (!sb._addedCFNames) sb._addedCFNames = {};
      // Remove previously added CFs that are now deselected
      for (const key of Object.keys(sb.editToggles)) {
        if (sb.editToggles[key] === 'added' && !br.selected[key]) {
          delete sb.editToggles[key];
          delete sb.editScores[key];
          delete sb._addedCFNames[key];
        }
      }
      // Add newly selected CFs
      const allCFs = {};
      for (const cat of br.categories) {
        for (const g of cat.groups) {
          for (const cf of g.cfs) { allCFs[cf.trashId] = cf.name; }
        }
      }
      for (const cf of br.customCFs || []) { allCFs[cf.id] = cf.name; }
      for (const [key, on] of Object.entries(br.selected)) {
        if (on) {
          const existing = (sb.editOriginal?.scores || []).find(s => s.trashId === key);
          if (!existing) {
            sb.editToggles[key] = 'added';
            sb.editScores[key] = br.scores[key] ?? 0;
            sb._addedCFNames[key] = allCFs[key] || key;
          }
        }
      }
      br.open = false;
      this.applySandboxEdit(br.appType);
    },

    sandboxCFBrowserCatCount(cat) {
      let count = 0;
      for (const g of cat.groups) {
        for (const cf of g.cfs) {
          if (this.sandboxCFBrowser.selected[cf.trashId]) count++;
        }
      }
      const total = cat.groups.reduce((sum, g) => sum + g.cfs.length, 0);
      return count + '/' + total;
    },

    async sandboxSearchCFs(appType, query) {
      if (!query || query.length < 2) return [];
      // Cache TRaSH + custom CFs per appType
      if (!this._sandboxCFCache[appType]) {
        try {
          const [trashRes, customRes] = await Promise.all([
            fetch(`/api/trash/${appType}/cfs`),
            fetch(`/api/custom-cfs/${appType}`)
          ]);
          const trashCFs = trashRes.ok ? await trashRes.json() : [];
          const customCFs = customRes.ok ? await customRes.json() : [];
          // Merge: custom CFs use their id as trashId, marked with isCustom
          const merged = [...(trashCFs || [])];
          for (const cf of (customCFs || [])) {
            merged.push({ trashId: cf.id, name: cf.name, isCustom: true });
          }
          this._sandboxCFCache[appType] = merged;
        } catch { this._sandboxCFCache[appType] = []; }
      }
      const q = query.toLowerCase();
      const existing = new Set((this.sandbox[appType].editOriginal?.scores || []).map(s => s.trashId));
      const added = this.sandbox[appType].editToggles || {};
      return this._sandboxCFCache[appType].filter(cf => cf.name.toLowerCase().includes(q) && !existing.has(cf.trashId) && added[cf.trashId] !== 'added').slice(0, 15);
    },

    addSandboxEditCF(appType, cf) {
      const sb = this.sandbox[appType];
      if (!sb._addedCFNames) sb._addedCFNames = {};
      sb._addedCFNames[cf.trashId] = cf.name;
      sb.editToggles[cf.trashId] = 'added';
      sb.editScores[cf.trashId] = 0;
      this.debounceSandboxEdit(appType);
    },

    sandboxCompareProfileName(appType) {
      const key = this.sandbox[appType].compareKey;
      if (!key) return '';
      if (key.startsWith('trash:')) {
        const tid = key.replace('trash:', '');
        const p = (this.trashProfiles[appType] || []).find(p => p.trashId === tid);
        return p?.name || tid;
      }
      if (key.startsWith('imported:')) {
        const id = key.replace('imported:', '');
        const p = (this.importedProfiles[appType] || []).find(p => p.id === id);
        return p?.name || id;
      }
      if (key.startsWith('inst:')) {
        const id = parseInt(key.replace('inst:', ''));
        const p = (this.sandbox[appType].instanceProfiles || []).find(p => p.id === id);
        return p?.name || key;
      }
      return key;
    },

    async calculateScoring(result, appType) {
      const sb = this.sandbox[appType];
      const profileKey = sb.profileKey;
      if (!profileKey || !result.matchedCFs) return result;
      const profileData = await this.fetchProfileScores(profileKey, appType);
      let scored = this.applyScoring(result, profileData);
      // Also score against compare profile if active
      if (sb.compareKey) {
        const compareData = await this.fetchProfileScores(sb.compareKey, appType);
        const compScored = this.applyScoring(result, compareData);
        scored = { ...scored, scoringB: compScored.scoring };
      }
      return scored;
    },

    applyScoring(result, profileData) {
      if (!result.matchedCFs || !profileData?.scores?.length) return result;

      // Build lookup maps: by trashId and by name
      const byTrashId = {};
      const byName = {};
      for (const s of profileData.scores) {
        if (s.trashId) byTrashId[s.trashId] = s;
        if (s.name) byName[s.name] = s;
      }

      let total = 0;
      const breakdown = [];
      const matchedKeys = new Set();

      // Score matched CFs
      for (const cf of result.matchedCFs) {
        const entry = (cf.trashId && byTrashId[cf.trashId]) || byName[cf.name];
        const score = entry?.score ?? 0;
        total += score;
        breakdown.push({ name: cf.name, trashId: cf.trashId, score, matched: true });
        if (cf.trashId) matchedKeys.add(cf.trashId);
        matchedKeys.add(cf.name);
      }

      // Unmatched CFs from profile
      for (const s of profileData.scores) {
        if (matchedKeys.has(s.trashId) || matchedKeys.has(s.name)) continue;
        breakdown.push({ name: s.name, trashId: s.trashId, score: s.score, matched: false });
        if (s.trashId) matchedKeys.add(s.trashId);
        matchedKeys.add(s.name);
      }

      // Sort: matched first (by |score| desc), then unmatched
      breakdown.sort((a, b) => {
        if (a.matched !== b.matched) return a.matched ? -1 : 1;
        return Math.abs(b.score) - Math.abs(a.score);
      });

      return { ...result, scoring: { total, breakdown, minScore: profileData.minScore || 0 } };
    },

    formatBytes(bytes) {
      if (!bytes || bytes === 0) return '0 B';
      const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
      const i = Math.floor(Math.log(bytes) / Math.log(1024));
      return (bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0) + ' ' + units[i];
    },

    async testProwlarr() {
      this.prowlarrTesting = true;
      this.prowlarrTestResult = null;
      try {
        const r = await fetch('/api/prowlarr/test', {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ url: this.config.prowlarr?.url, apiKey: this.config.prowlarr?.apiKey })
        });
        const data = await r.json();
        if (data.connected) {
          this.prowlarrTestResult = { ok: true, message: 'Connected', version: data.version };
        } else {
          this.prowlarrTestResult = { ok: false, message: data.error || 'Connection failed' };
        }
      } catch (e) {
        this.prowlarrTestResult = { ok: false, message: 'Network error: ' + e.message };
      }
      finally { this.prowlarrTesting = false; }
    },

    // --- CF Group Builder ---
    // Loads CFs + profiles for the active app so the builder UI can populate.
    // Called on first click of the CF Group Builder sub-tab and whenever the
    // user toggles Radarr↔Sonarr (CFs + profiles are app-specific).
    async cfgbLoad(appType) {
      this.cfgbLoadError = '';
      // When the user switches Radarr↔Sonarr the current form has to be reset
      // (a half-built group is scoped to one app type). Refresh saved list too.
      this.cfgbReset();
      // Race guard: if the user rapidly flips Radarr↔Sonarr↔Radarr, multiple
      // cfgbLoad calls are in flight simultaneously. Each stores its appType
      // on entry; on completion we re-check — if a later call has superseded
      // ours, discard the response instead of leaking state across appTypes.
      this._cfgbLoadFor = appType;
      try {
        const [allCfsResp, profResp, savedResp, trashGroupsResp] = await Promise.all([
          fetch('/api/trash/' + appType + '/all-cfs'),
          fetch('/api/trash/' + appType + '/profiles'),
          fetch('/api/cf-groups/' + appType),
          fetch('/api/trash/' + appType + '/cf-groups'),
        ]);
        if (this._cfgbLoadFor !== appType) return; // superseded
        if (!allCfsResp.ok || !profResp.ok) {
          throw new Error('HTTP ' + allCfsResp.status + ' / ' + profResp.status);
        }
        const [allCfsRes, profRes, savedRes, trashGroupsRes] = await Promise.all([
          allCfsResp.json(),
          profResp.json(),
          savedResp.ok ? savedResp.json() : Promise.resolve([]),
          trashGroupsResp.ok ? trashGroupsResp.json() : Promise.resolve([]),
        ]);
        // /all-cfs returns { categories: [{ category, groups: [{ cfs: [...] }] }] }.
        // The "categories" layer is a Clonarr-side abstraction; TRaSH itself
        // organizes CFs by cf-groups (the inner `groups` level). The builder
        // filter uses those REAL TRaSH groups so new upstream groups appear
        // automatically without any Clonarr-side mapping to maintain.
        //
        // Each CF carries its parent group's trashId + name for filtering.
        // Synthetic "Custom" and "Other" groups (emitted by the backend for
        // user-custom CFs and ungrouped CFs) have no groupTrashId — we treat
        // those as two special filter modes instead of real groups.
        // The /all-cfs endpoint lists each CF once per containing cf-group.
        // ~18 TRaSH CFs live in multiple groups (HDR10+, DV-related, a few
        // anime ones). A naive flatten produced duplicate rows — Alpine's
        // <template x-for> with :key="cf.trashId" silently drops rendering
        // at the first duplicate, which truncated the alpha-sorted list
        // around its first repeat (e.g. stopped just past "AV1").
        //
        // Dedup by trashId, but accumulate every groupTrashId the CF appears
        // under. The cf-group dropdown filter walks groupTrashIds (array
        // membership), so a shared CF surfaces under each of its groups.
        // The group count tally counts every appearance so the "(N)" badges
        // in the dropdown reflect real TRaSH group membership.
        const flat = [];
        const byTid = new Map(); // trashId → entry in flat[]
        const groupMap = new Map(); // groupTrashId → {groupTrashId, name, count}
        let hasCustom = false, hasOther = false;
        for (const cat of (allCfsRes.categories || [])) {
          for (const group of (cat.groups || [])) {
            const gid = group.groupTrashId || '';
            const gname = group.name || group.shortName || '';
            if (gid && !groupMap.has(gid)) {
              groupMap.set(gid, { groupTrashId: gid, name: gname, count: 0 });
            }
            for (const cf of (group.cfs || [])) {
              if (!cf.trashId || !cf.name) continue;
              if (gid) groupMap.get(gid).count++;
              if (cf.isCustom) hasCustom = true;
              if (!gid && !cf.isCustom) hasOther = true;
              let row = byTid.get(cf.trashId);
              if (!row) {
                row = {
                  trashId: cf.trashId,
                  name: cf.name,
                  groupTrashIds: [],
                  groupNames: [],
                  isCustom: !!cf.isCustom,
                };
                flat.push(row);
                byTid.set(cf.trashId, row);
              }
              if (gid && !row.groupTrashIds.includes(gid)) {
                row.groupTrashIds.push(gid);
                row.groupNames.push(gname);
              }
            }
          }
        }
        // Cache TRaSH-only state on the instance. cfgbApplyLocalGroups()
        // reads this cache + cfgbSavedGroups to produce cfgbCFs / cfgbGroups /
        // cfgbHasOther — so Save/Delete on a saved group can refresh the
        // dropdown without re-fetching /all-cfs or resetting the form.
        this._cfgbTrashFlat = flat;
        this._cfgbTrashGroupMap = groupMap;
        this._cfgbTrashHasCustom = hasCustom;
        this.cfgbApplyLocalGroups();
        // Profiles API still uses camelCase (api.ProfileListItem).
        this.cfgbProfiles = (profRes || [])
          .map(p => ({
            trashId: p.trashId,
            name: p.name || '',
            group: typeof p.group === 'number' ? p.group : 99,
            groupName: p.groupName || 'Other',
          }))
          .filter(p => p.trashId && p.name);
        // Collapse all profile cards by default — mirrors the Profiles /
        // TRaSH Sync tab behaviour where the user opens the card they care
        // about rather than scrolling past 50+ profiles up front.
        this.cfgbProfileGroupExpanded = {};
        this.cfgbSavedGroups = Array.isArray(savedRes) ? savedRes : [];
        // TRaSH upstream cf-groups — used by the "Copy from TRaSH" section so
        // the user can base a new local group on an existing upstream one and
        // tweak it without touching the TRaSH repo clone.
        this.cfgbTrashCFGroups = Array.isArray(trashGroupsRes) ? trashGroupsRes : [];
      } catch (e) {
        if (this._cfgbLoadFor !== appType) return; // superseded
        console.error('cfgbLoad failed:', e);
        this.cfgbLoadError = 'Failed to load TRaSH data: ' + e.message + '. Try Pull TRaSH in Settings → TRaSH Repo.';
        this.cfgbCFs = [];
        this.cfgbGroups = [];
        this.cfgbHasCustom = false;
        this.cfgbUngroupedTrashCount = 0;
        this.cfgbUngroupedRemainingCount = 0;
        this._cfgbTrashFlat = [];
        this._cfgbTrashGroupMap = new Map();
        this._cfgbTrashHasCustom = false;
        this.cfgbProfiles = [];
        this.cfgbSavedGroups = [];
        this.cfgbTrashCFGroups = [];
      }
    },

    // Combines TRaSH-only cache (set by cfgbLoad) with the current
    // cfgbSavedGroups to produce the CF list, dropdown, and Ungrouped
    // counts. Callable after Save/Delete of a local group so the dropdown
    // refreshes without another /all-cfs fetch.
    //
    // Each CF keeps its TRaSH group memberships in c.trashGroupTrashIds
    // (pure) and the full combined set in c.groupTrashIds (TRaSH + local,
    // used for rendering + filter). The "Ungrouped (TRaSH)" filter walks
    // trashGroupTrashIds; "Ungrouped (after local)" walks groupTrashIds.
    cfgbApplyLocalGroups() {
      // Clone the TRaSH-only flat list so the merge doesn't mutate cache.
      const flat = (this._cfgbTrashFlat || []).map(c => ({
        ...c,
        trashGroupTrashIds: c.groupTrashIds.slice(),
        trashGroupNames: c.groupNames.slice(),
        groupTrashIds: c.groupTrashIds.slice(),
        groupNames: c.groupNames.slice(),
      }));
      const byTid = new Map(flat.map(c => [c.trashId, c]));
      const groupMap = new Map();
      for (const [gid, g] of (this._cfgbTrashGroupMap || new Map())) {
        groupMap.set(gid, { ...g });
      }
      // Merge local cf-groups into dropdown entries + per-CF membership.
      for (const g of (this.cfgbSavedGroups || [])) {
        if (!g || !g.id) continue;
        const localId = 'local:' + g.id;
        const gname = g.name || '(unnamed local)';
        groupMap.set(localId, {
          groupTrashId: localId,
          name: gname,
          count: (g.custom_formats || []).length,
          isLocal: true,
        });
        for (const cf of (g.custom_formats || [])) {
          const tid = cf && cf.trash_id;
          if (!tid) continue;
          const row = byTid.get(tid);
          if (!row) continue; // CF in saved group but not in /all-cfs (stale)
          if (!row.groupTrashIds.includes(localId)) {
            row.groupTrashIds.push(localId);
            row.groupNames.push(gname);
          }
        }
      }
      this.cfgbCFs = flat;
      // TRaSH groups first (alpha), then locals (alpha) at the bottom.
      this.cfgbGroups = Array.from(groupMap.values()).sort((a, b) => {
        if (!!a.isLocal !== !!b.isLocal) return a.isLocal ? 1 : -1;
        return a.name.localeCompare(b.name);
      });
      this.cfgbHasCustom = this._cfgbTrashHasCustom;
      this.cfgbUngroupedTrashCount =
        flat.filter(c => !c.isCustom && c.trashGroupTrashIds.length === 0).length;
      this.cfgbUngroupedRemainingCount =
        flat.filter(c => !c.isCustom && c.groupTrashIds.length === 0).length;
    },

    cfgbUpdateHash() {
      // trash_id is MD5 of the group name, scoped by app-type prefix so the
      // same name ("[Release Groups] Anime") on Radarr vs Sonarr produces
      // different hashes. TRaSH's tooling treats trash_id as a global key
      // across both apps, so identical hashes would collide there even
      // though our on-disk storage separates them per app-type.
      //
      // Hash lock is the escape hatch for editing: when locked, name-input
      // events don't regenerate the hash so typo fixes / minor rewording
      // don't invalidate downstream references. The user toggles the lock
      // explicitly via the edit-banner button.
      if (this.cfgbHashLocked) return;
      const n = (this.cfgbName || '').trim();
      const app = this.activeAppType || 'radarr';
      this.cfgbTrashID = n ? cfgbMD5(app + ':' + n) : '';
    },

    // Toggle the hash lock. Locking restores the original trash_id
    // (cfgbOriginalTrashID), unlocking regenerates from the current name.
    // Only meaningful when cfgbOriginalTrashID is set — fresh new groups
    // have no original to restore, so the button is hidden in that state.
    cfgbToggleHashLock() {
      if (!this.cfgbOriginalTrashID) return;
      this.cfgbHashLocked = !this.cfgbHashLocked;
      if (this.cfgbHashLocked) {
        this.cfgbTrashID = this.cfgbOriginalTrashID;
      } else {
        const n = (this.cfgbName || '').trim();
        const app = this.activeAppType || 'radarr';
        this.cfgbTrashID = n ? cfgbMD5(app + ':' + n) : '';
      }
    },

    cfgbFilteredCFs() {
      const g = this.cfgbGroupFilter || 'all';
      let list = this.cfgbCFs;
      if (g === 'custom') {
        list = list.filter(c => c.isCustom);
      } else if (g === 'other-trash') {
        // Ungrouped per TRaSH: CF isn't in any upstream cf-group. Local
        // groups don't subtract — useful to see the full set TRaSH still
        // needs to categorize.
        list = list.filter(c => !c.isCustom && c.trashGroupTrashIds.length === 0);
      } else if (g === 'other-remaining') {
        // Ungrouped after local work: excludes CFs already placed in any
        // local group. This is "what's left to do" once the user has
        // started organizing.
        list = list.filter(c => !c.isCustom && c.groupTrashIds.length === 0);
      } else if (g !== 'all') {
        // Specific cf-group (TRaSH or local) by its trash_id / localId.
        // A CF in multiple groups matches each of its groups.
        list = list.filter(c => c.groupTrashIds.includes(g));
      }
      // Filter string supports multiple whitespace-separated terms with OR
      // semantics — "mono stereo surround" matches CFs whose name contains
      // any of those words. Makes it easy to pull related-but-separate
      // formats into one view without chaining filter typing.
      const terms = (this.cfgbCFFilter || '')
        .toLowerCase()
        .split(/\s+/)
        .map(t => t.trim())
        .filter(Boolean);
      if (terms.length > 0) {
        list = list.filter(c => {
          const n = c.name.toLowerCase();
          return terms.some(t => n.includes(t));
        });
      }
      return list.slice().sort((a, b) => a.name.localeCompare(b.name));
    },

    // True when every CF the user currently sees (group + text filters
    // applied) is already selected. Drives the Select-all toggle's checked
    // state so one click flips between select-all and deselect-all.
    cfgbFilteredAllSelected() {
      const list = this.cfgbFilteredCFs();
      if (list.length === 0) return false;
      return list.every(c => this.cfgbSelectedCFs[c.trashId]);
    },

    // Applies select-all / deselect-all to whatever the user is looking at.
    // Scoped to cfgbFilteredCFs() so "select all Release Group Tiers" works
    // whether the filter is a cf-group, a text match, or both combined.
    cfgbToggleFilteredAll(on) {
      const list = this.cfgbFilteredCFs();
      const next = { ...this.cfgbSelectedCFs };
      for (const c of list) {
        if (on) next[c.trashId] = true;
        else delete next[c.trashId];
      }
      this.cfgbSelectedCFs = next;
    },
    cfgbGroupFilterLabel() {
      // Short human-readable label for the current filter — used in the count
      // badge so "3 / 12 in [Audio] Audio Channels" is immediately obvious.
      const g = this.cfgbGroupFilter;
      if (g === 'all') return '';
      if (g === 'custom') return 'Custom CFs';
      if (g === 'other-trash') return 'Ungrouped (TRaSH)';
      if (g === 'other-remaining') return 'Ungrouped (after local)';
      const match = this.cfgbGroups.find(gr => gr.groupTrashId === g);
      return match ? match.name : '';
    },
    cfgbFilteredCount() {
      // Count matching the current filters — shows "M of N in category" style.
      return this.cfgbFilteredCFs().length;
    },

    cfgbSortedProfiles() {
      // Sort primarily by profile.group (int), then alphabetical by name.
      // Still exposed because cfgbBuildPayload() walks profiles in display
      // order to produce stable JSON output.
      return this.cfgbProfiles.slice().sort((a, b) => {
        if (a.group !== b.group) return a.group - b.group;
        return a.name.localeCompare(b.name);
      });
    },

    // Group profiles into the same cards the Profiles tab uses (Standard,
    // Anime, French, German, SQP, Other). Sorted by the `group` integer
    // from profile.json (ascending) with alpha tiebreak on the card name,
    // matching TRaSH's convention for profile ordering.
    cfgbGroupedProfiles() {
      const profiles = this.cfgbProfiles;
      const groups = {};
      for (const p of profiles) {
        const g = p.groupName || 'Other';
        if (!groups[g]) groups[g] = { name: g, profiles: [], groupId: p.group, minGroup: Infinity };
        groups[g].profiles.push(p);
        const gnum = typeof p.group === 'number' ? p.group : Infinity;
        if (gnum < groups[g].minGroup) groups[g].minGroup = gnum;
      }
      for (const g of Object.values(groups)) {
        g.profiles.sort((a, b) => a.name.localeCompare(b.name));
      }
      return Object.values(groups).sort((a, b) => {
        if (a.minGroup !== b.minGroup) return a.minGroup - b.minGroup;
        return a.name.localeCompare(b.name);
      });
    },

    cfgbToggleProfileGroupCard(name) {
      this.cfgbProfileGroupExpanded = {
        ...this.cfgbProfileGroupExpanded,
        [name]: !this.cfgbProfileGroupExpanded[name],
      };
    },

    cfgbIsProfileGroupExpanded(name) {
      return !!this.cfgbProfileGroupExpanded[name];
    },

    cfgbGroupAllSelected(group) {
      if (!group.profiles.length) return false;
      return group.profiles.every(p => this.cfgbSelectedProfiles[p.trashId]);
    },

    cfgbGroupSomeSelected(group) {
      return group.profiles.some(p => this.cfgbSelectedProfiles[p.trashId])
        && !this.cfgbGroupAllSelected(group);
    },

    cfgbToggleGroupAll(group, on) {
      const next = { ...this.cfgbSelectedProfiles };
      for (const p of group.profiles) {
        if (on) next[p.trashId] = true;
        else delete next[p.trashId];
      }
      this.cfgbSelectedProfiles = next;
    },

    cfgbGroupSelectedCount(group) {
      return group.profiles.filter(p => this.cfgbSelectedProfiles[p.trashId]).length;
    },

    // Per-panel clears — let the user keep profiles while starting a fresh
    // CF selection (and vice versa). Useful for building several cf-groups
    // that share the same quality-profile targets but differ in CF content.
    cfgbClearCFs() {
      this.cfgbSelectedCFs = {};
      this.cfgbRequiredCFs = {};
      this.cfgbDefaultCFs = {};
    },
    cfgbClearProfiles() {
      this.cfgbSelectedProfiles = {};
    },

    // True when every currently-selected CF already has required=true.
    // Drives the bulk required toggle's label — same click always flips
    // the whole set so the user can mass-mark then mass-unmark quickly.
    cfgbAllSelectedRequired() {
      const selectedIds = Object.keys(this.cfgbSelectedCFs).filter(id => this.cfgbSelectedCFs[id]);
      if (selectedIds.length === 0) return false;
      return selectedIds.every(id => this.cfgbRequiredCFs[id]);
    },
    cfgbToggleAllRequired(on) {
      const next = { ...this.cfgbRequiredCFs };
      for (const id of Object.keys(this.cfgbSelectedCFs)) {
        if (!this.cfgbSelectedCFs[id]) continue;
        if (on) next[id] = true;
        else delete next[id];
      }
      this.cfgbRequiredCFs = next;
    },

    // --- CF sort mode (alpha vs manual) ---

    // Returns the selected CFs in the user-chosen order. In alpha mode this
    // is just case-insensitive alpha by name. In manual mode we follow
    // cfgbCFManualOrder, appending any newly-selected CFs that haven't been
    // placed yet and skipping any whose selection was revoked. Callers use
    // this for the JSON payload AND for the manual reorder UI.
    cfgbOrderedSelectedCFs() {
      const selected = this.cfgbCFs.filter(c => this.cfgbSelectedCFs[c.trashId]);
      if (this.cfgbCFSortMode !== 'manual') {
        return selected.slice().sort((a, b) =>
          a.name.localeCompare(b.name, undefined, { sensitivity: 'base' })
        );
      }
      const byId = new Map(selected.map(c => [c.trashId, c]));
      const result = [];
      const placed = new Set();
      for (const id of this.cfgbCFManualOrder) {
        const cf = byId.get(id);
        if (cf && !placed.has(id)) {
          result.push(cf);
          placed.add(id);
        }
      }
      for (const cf of selected) {
        if (!placed.has(cf.trashId)) result.push(cf);
      }
      return result;
    },

    cfgbSetCFSortMode(mode) {
      // When switching into manual mode, seed the order from the current
      // visible alpha order so the user starts with a sensible baseline
      // rather than an empty list. Switching back to alpha leaves the manual
      // order intact in case the user flips back again.
      if (mode === 'manual' && this.cfgbCFManualOrder.length === 0) {
        this.cfgbCFManualOrder = this.cfgbOrderedSelectedCFs().map(c => c.trashId);
      }
      this.cfgbCFSortMode = mode;
    },

    cfgbMoveCF(trashId, direction) {
      // Move a single CF up (-1) or down (+1) in the manual order. Rebuilds
      // the order from the current selection so we never operate on a stale
      // list that includes since-deselected CFs.
      //
      // Kept as a public method even though the arrow-based UI that drove it
      // was replaced by drag-and-drop (cfgbCFDrop) in v2.2.0 — tests and any
      // future keyboard-accessible reorder path could still use it.
      const current = this.cfgbOrderedSelectedCFs().map(c => c.trashId);
      const idx = current.indexOf(trashId);
      if (idx < 0) return;
      const newIdx = idx + direction;
      if (newIdx < 0 || newIdx >= current.length) return;
      const tmp = current[idx];
      current[idx] = current[newIdx];
      current[newIdx] = tmp;
      this.cfgbCFManualOrder = current;
    },

    // Drag-and-drop reorder for Selected CFs (manual mode only). Mirrors
    // the sandboxDragStart/Over/Drop pattern used by Scoring Sandbox —
    // source + target tracked by trash_id (identity-safe across re-renders),
    // drop rewrites cfgbCFManualOrder from the current selection (stale
    // entries for since-deselected CFs get dropped at the same time).
    cfgbCFDragStart(trashId) {
      this.cfgbDragSrcTid = trashId;
    },
    cfgbCFDragOver(trashId) {
      this.cfgbDragOverTid = trashId;
    },
    cfgbCFDragEnd() {
      this.cfgbDragSrcTid = null;
      this.cfgbDragOverTid = null;
    },
    cfgbCFDrop(targetTid) {
      const src = this.cfgbDragSrcTid;
      this.cfgbDragSrcTid = null;
      this.cfgbDragOverTid = null;
      if (!src || src === targetTid) return;
      const current = this.cfgbOrderedSelectedCFs().map(c => c.trashId);
      const fromIdx = current.indexOf(src);
      const toIdx = current.indexOf(targetTid);
      if (fromIdx < 0 || toIdx < 0) return;
      current.splice(fromIdx, 1);
      current.splice(toIdx, 0, src);
      this.cfgbCFManualOrder = current;
    },

    cfgbResetManualOrder() {
      // Drops the manual ordering — list reverts to alpha the next time
      // manual mode is re-entered.
      this.cfgbCFManualOrder = [];
      this.cfgbCFSortMode = 'alpha';
    },

    cfgbSelectedCFCount()      { return Object.values(this.cfgbSelectedCFs).filter(Boolean).length; },
    cfgbSelectedProfileCount() { return Object.values(this.cfgbSelectedProfiles).filter(Boolean).length; },

    cfgbAllProfilesSelected() {
      if (this.cfgbProfiles.length === 0) return false;
      return this.cfgbProfiles.every(p => this.cfgbSelectedProfiles[p.trashId]);
    },

    cfgbToggleAllProfiles(on) {
      const next = {};
      if (on) this.cfgbProfiles.forEach(p => next[p.trashId] = true);
      this.cfgbSelectedProfiles = next;
    },

    cfgbCanExport() {
      return !!(this.cfgbTrashID && this.cfgbName.trim() && this.cfgbSelectedCFCount() > 0);
    },

    cfgbGenerateJSON() {
      // The preview and Download JSON share one payload builder so what the
      // user sees in the preview is exactly what lands on disk. CF order
      // preserves the /all-cfs API order (category > group > cf) which matches
      // how TRaSH organizes their own source files, so exported diffs against
      // hand-written cf-groups stay minimal. The UI shows alpha-sorted for
      // discoverability; only EXPORT preserves source order.
      return JSON.stringify(this.cfgbBuildPayload(), null, 4) + '\n';
    },

    async cfgbCopyJSON() {
      try {
        await copyToClipboard(this.cfgbGenerateJSON());
        this.cfgbCopyLabel = 'Copied!';
        setTimeout(() => { this.cfgbCopyLabel = 'Copy JSON'; }, 1500);
      } catch (e) {
        alert('Copy failed: ' + e.message);
      }
    },

    cfgbDownloadJSON() {
      // Slug: keep the category prefix (not strip it), so
      // "[Release Groups] Anime" → "release-groups-anime.json".
      // Matches TRaSH's filename convention — the brackets drop but the
      // category words remain as part of the slug, joined to the short
      // name with hyphens.
      const slug = this.cfgbName.trim().toLowerCase()
        .replace(/[\[\]]/g, ' ')         // brackets → space (preserves contents)
        .replace(/[^a-z0-9]+/g, '-')     // non-alphanumerics → hyphen
        .replace(/^-+|-+$/g, '')          // trim leading/trailing hyphens
        // Collapse immediate leading duplication. "[Audio] Audio Formats"
        // produces "audio-audio-formats"; TRaSH's convention drops the
        // repeat so it becomes "audio-formats" (matches his existing files).
        // Only triggers on exact back-to-back word equality at the start,
        // so e.g. "hdr-formats-hdr" (where the category ends with "formats"
        // but isn't doubled) stays untouched.
        .replace(/^([a-z0-9]+)-\1(-|$)/, '$1$2')
        || 'cf-group';
      const blob = new Blob([this.cfgbGenerateJSON()], { type: 'application/json' });
      const url = URL.createObjectURL(blob);
      const a = document.createElement('a');
      a.href = url;
      a.download = slug + '.json';
      document.body.appendChild(a);
      a.click();
      document.body.removeChild(a);
      URL.revokeObjectURL(url);
    },

    cfgbReset() {
      this.cfgbName = '';
      this.cfgbDescription = '';
      this.cfgbTrashID = '';
      this.cfgbDefault = false;
      this.cfgbCFFilter = '';
      this.cfgbGroupFilter = 'all';
      this.cfgbSelectedCFs = {};
      this.cfgbRequiredCFs = {};
      this.cfgbDefaultCFs = {};
      this.cfgbSelectedProfiles = {};
      this.cfgbEditingId = '';
      this.cfgbOriginalTrashID = '';
      this.cfgbFromTrashName = '';
      this.cfgbHashLocked = false;
      this.cfgbSavingMsg = '';
      this.cfgbCFSortMode = 'alpha';
      this.cfgbCFManualOrder = [];
    },

    // Called from the UI Reset / Discard button. Prompts when we're editing
    // an existing saved group so a single misclick can't nuke the in-flight
    // changes. cfgbReset itself stays prompt-free because it's called from
    // internal flows (cfgbLoad, cfgbSave, delete-current-editing) where a
    // prompt would be wrong.
    cfgbUIReset() {
      if (!this.cfgbEditingId) {
        this.cfgbReset();
        return;
      }
      this.confirmModal = {
        show: true,
        title: 'Discard changes',
        message: 'Discard changes to "' + (this.cfgbName || '(unnamed)') + '"?\n\nThe saved copy on disk is unaffected.',
        confirmLabel: 'Discard',
        onConfirm: () => this.cfgbReset(),
        onCancel: () => {},
      };
    },

    // Returns true when the form has "meaningful work" that would be lost
    // by a silent reset. Used to gate app-type switches + UI reset prompts.
    cfgbIsDirty() {
      if (this.cfgbEditingId) return true;
      if ((this.cfgbName || '').trim()) return true;
      if ((this.cfgbDescription || '').trim()) return true;
      if (Object.keys(this.cfgbSelectedCFs).some(k => this.cfgbSelectedCFs[k])) return true;
      if (Object.keys(this.cfgbSelectedProfiles).some(k => this.cfgbSelectedProfiles[k])) return true;
      return false;
    },

    // --- Saved cf-groups ---
    // Load an existing saved group into the form for editing. Sets
    // cfgbEditingId so Save will PUT instead of POST, keeping the same file
    // on disk. Profile/CF lookups are by trashId which is stable.
    cfgbLoadForEdit(g) {
      this.cfgbName = g.name || '';
      this.cfgbDescription = g.trash_description || '';
      this.cfgbTrashID = g.trash_id || '';
      this.cfgbDefault = g.default === 'true' || g.default === true;
      this.cfgbCFFilter = '';
      this.cfgbGroupFilter = 'all';
      const selCFs = {}, reqCFs = {}, defCFs = {};
      for (const cf of (g.custom_formats || [])) {
        if (!cf.trash_id) continue;
        selCFs[cf.trash_id] = true;
        if (cf.required) reqCFs[cf.trash_id] = true;
        if (cf.default) defCFs[cf.trash_id] = true;
      }
      this.cfgbSelectedCFs = selCFs;
      this.cfgbRequiredCFs = reqCFs;
      this.cfgbDefaultCFs = defCFs;
      // Restore CF order: if the saved CF sequence differs from alpha, flip
      // to manual mode with that exact order. Otherwise stay in alpha.
      const savedOrder = (g.custom_formats || []).map(cf => cf.trash_id).filter(Boolean);
      // Name lookup so the comparator can never hit `undefined.localeCompare`
      // when a saved trash_id is missing from custom_formats (defensive — a
      // corrupted or older saved group might have drift).
      const nameByTid = new Map(
        (g.custom_formats || [])
          .filter(cf => cf.trash_id)
          .map(cf => [cf.trash_id, cf.name || ''])
      );
      const alphaOrder = savedOrder.slice().sort((a, b) =>
        (nameByTid.get(a) || '').localeCompare(nameByTid.get(b) || '', undefined, { sensitivity: 'base' })
      );
      const isAlpha = savedOrder.every((id, i) => id === alphaOrder[i]);
      if (isAlpha) {
        this.cfgbCFSortMode = 'alpha';
        this.cfgbCFManualOrder = [];
      } else {
        this.cfgbCFSortMode = 'manual';
        this.cfgbCFManualOrder = savedOrder;
      }
      const selProf = {};
      const include = (g.quality_profiles && g.quality_profiles.include) || {};
      for (const trashId of Object.values(include)) {
        if (trashId) selProf[trashId] = true;
      }
      this.cfgbSelectedProfiles = selProf;
      this.cfgbEditingId = g.id || '';
      // Capture the trash_id at load time + engage the hash lock so the
      // user can fix typos or tweak the name without invalidating
      // downstream references (profile includes, prior exports, synced
      // Arr profiles). The lock button in the edit banner unlocks it
      // explicitly if the user wants a fresh identity.
      this.cfgbOriginalTrashID = g.trash_id || '';
      this.cfgbHashLocked = !!g.trash_id;
      this.cfgbFromTrashName = '';
      this.cfgbSavingMsg = '';
      // Scroll the form into view so the user sees the loaded fields
      // immediately — the saved-groups list sits above the form.
      setTimeout(() => {
        const el = document.getElementById('cfgb-form-top');
        if (el && el.scrollIntoView) el.scrollIntoView({ behavior: 'smooth', block: 'start' });
      }, 0);
    },

    // Copy an upstream TRaSH cf-group into the builder as the starting point
    // for a new LOCAL group. The TRaSH repo clone is never modified — saving
    // writes to /config/custom/json/{appType}/cf-groups/ alongside the user's
    // own groups (cfgbEditingId stays empty so cfgbSave POSTs). Preserves the
    // upstream trash_id so a user who tweaks without renaming keeps a hash
    // that matches the source group until they explicitly change the name.
    cfgbLoadFromTrash(g) {
      this.cfgbName = g.name || '';
      this.cfgbDescription = g.trash_description || '';
      this.cfgbTrashID = g.trash_id || '';
      this.cfgbDefault = g.default === 'true' || g.default === true;
      this.cfgbCFFilter = '';
      this.cfgbGroupFilter = 'all';
      const selCFs = {}, reqCFs = {}, defCFs = {};
      for (const cf of (g.custom_formats || [])) {
        if (!cf.trash_id) continue;
        selCFs[cf.trash_id] = true;
        if (cf.required) reqCFs[cf.trash_id] = true;
        if (cf.default) defCFs[cf.trash_id] = true;
      }
      this.cfgbSelectedCFs = selCFs;
      this.cfgbRequiredCFs = reqCFs;
      this.cfgbDefaultCFs = defCFs;
      // Preserve TRaSH's CF ordering: flip to manual mode if the upstream
      // order isn't already alphabetical, so the copied group matches the
      // source file byte-for-byte until the user edits it.
      const srcOrder = (g.custom_formats || []).map(cf => cf.trash_id).filter(Boolean);
      const nameByTid = new Map(
        (g.custom_formats || [])
          .filter(cf => cf.trash_id)
          .map(cf => [cf.trash_id, cf.name || ''])
      );
      const alphaOrder = srcOrder.slice().sort((a, b) =>
        (nameByTid.get(a) || '').localeCompare(nameByTid.get(b) || '', undefined, { sensitivity: 'base' })
      );
      const isAlpha = srcOrder.every((id, i) => id === alphaOrder[i]);
      if (isAlpha) {
        this.cfgbCFSortMode = 'alpha';
        this.cfgbCFManualOrder = [];
      } else {
        this.cfgbCFSortMode = 'manual';
        this.cfgbCFManualOrder = srcOrder;
      }
      const selProf = {};
      const include = (g.quality_profiles && g.quality_profiles.include) || {};
      for (const trashId of Object.values(include)) {
        if (trashId) selProf[trashId] = true;
      }
      this.cfgbSelectedProfiles = selProf;
      // Key distinction from cfgbLoadForEdit: editingId stays empty so Save
      // POSTs a NEW record rather than PUTting over the TRaSH clone. Capture
      // the upstream trash_id and engage the hash lock so typo fixes or
      // minor rewording of the group name don't invalidate the ID link
      // back to the upstream group.
      this.cfgbEditingId = '';
      this.cfgbOriginalTrashID = g.trash_id || '';
      this.cfgbHashLocked = !!g.trash_id;
      this.cfgbFromTrashName = g.name || '';
      this.cfgbSavingMsg = '';
      setTimeout(() => {
        const el = document.getElementById('cfgb-form-top');
        if (el && el.scrollIntoView) el.scrollIntoView({ behavior: 'smooth', block: 'start' });
      }, 0);
    },

    // Build the TRaSH-schema object used by both Download JSON and Save.
    // Keeping this separate from cfgbGenerateJSON (which returns a string)
    // avoids round-tripping through JSON.parse on Save.
    //
    // Sort contract (per TRaSH's builder spec):
    //  - custom_formats: alpha by name, case-insensitive
    //  - quality_profiles.include: group-number ascending, alpha within
    //    same group — produced by cfgbSortedProfiles() already.
    cfgbBuildPayload() {
      const selectedCFs = this.cfgbOrderedSelectedCFs();
      // Sanitize description — users often paste the whole JSON line
      // ("...text...",) including outer quotes and the trailing comma; strip
      // them so the emitted field holds only the inner text.
      let desc = (this.cfgbDescription || '').trim();
      while (desc.endsWith(',')) desc = desc.slice(0, -1).trim();
      if (desc.startsWith('"') && desc.endsWith('"') && desc.length >= 2) {
        desc = desc.slice(1, -1);
      }
      // Match TRaSH's convention: the `default` field is emitted only when
      // the group is default-on ("true"). Opt-in groups (default unchecked)
      // omit the field entirely, as seen in optional-*.json on disk. Emitting
      // `"default": "false"` was a false diff against upstream files.
      const payload = {
        name: this.cfgbName.trim(),
        trash_id: this.cfgbTrashID,
        trash_description: desc,
      };
      if (this.cfgbDefault) payload.default = 'true';
      payload.custom_formats = selectedCFs.map(c => {
        const entry = {
          name: c.name,
          trash_id: c.trashId,
          required: !!this.cfgbRequiredCFs[c.trashId],
        };
        // Match TRaSH's convention: per-CF `default` is emitted only when
        // true (omitted when false). Keeps generated JSON diff-friendly
        // against upstream Golden Rule files.
        if (this.cfgbDefaultCFs[c.trashId]) entry.default = true;
        return entry;
      });
      payload.quality_profiles = {
        include: this.cfgbSortedProfiles()
          .filter(p => this.cfgbSelectedProfiles[p.trashId])
          .reduce((acc, p) => { acc[p.name] = p.trashId; return acc; }, {}),
      };
      return payload;
    },

    async cfgbSave() {
      if (!this.cfgbCanExport()) return;
      // The hash-drift prompt was removed in favour of the explicit hash
      // lock toggle in the edit banner — the user makes the keep-vs-
      // regenerate decision visibly while editing rather than being
      // surprised by a modal at save time.
      return this._cfgbDoSave();
    },

    // Performs the actual POST (new) or PUT (existing local). The hash is
    // whatever cfgbTrashID holds — the lock toggle decides whether that's
    // the original (locked) or the MD5 of the current name (unlocked).
    async _cfgbDoSave() {
      const appType = this.activeAppType;
      const payload = this.cfgbBuildPayload();
      const editing = !!this.cfgbEditingId;
      const url = editing
        ? '/api/cf-groups/' + appType + '/' + encodeURIComponent(this.cfgbEditingId)
        : '/api/cf-groups/' + appType;
      const method = editing ? 'PUT' : 'POST';
      try {
        const resp = await fetch(url, {
          method,
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify(payload),
        });
        if (!resp.ok) {
          const text = await resp.text();
          throw new Error('HTTP ' + resp.status + ': ' + text);
        }
        const saved = await resp.json();
        // Refresh the saved-list and stay on the form. For creates (incl.
        // TRaSH copies), switch into edit-mode for the new record so the
        // next Save is a PUT.
        //
        // Hash lock on save: if this was a fresh-new group (no prior
        // baseline), engage the lock now that there's a saved identity.
        // If it was an edit, PRESERVE the user's explicit lock choice —
        // they deliberately unlocked/locked and shouldn't be surprised
        // by a silent state reset. A user who saved with the lock off
        // (regenerating hash from a new name) expects to stay unlocked
        // and keep iterating on the name.
        const wasFreshNew = !this.cfgbOriginalTrashID;
        await this.cfgbRefreshSaved();
        this.cfgbEditingId = saved.id || this.cfgbEditingId;
        this.cfgbOriginalTrashID = saved.trash_id || this.cfgbTrashID;
        if (wasFreshNew) {
          this.cfgbHashLocked = !!this.cfgbOriginalTrashID;
        }
        this.cfgbFromTrashName = '';
        this.cfgbSavingOk = true;
        this.cfgbSavingMsg = editing ? 'Updated.' : 'Saved.';
        setTimeout(() => { if (this.cfgbSavingMsg === 'Updated.' || this.cfgbSavingMsg === 'Saved.') this.cfgbSavingMsg = ''; }, 2000);
      } catch (e) {
        console.error('cfgbSave failed:', e);
        this.cfgbSavingOk = false;
        this.cfgbSavingMsg = 'Save failed: ' + e.message;
      }
    },

    cfgbDelete(g) {
      if (!g || !g.id) return;
      this.confirmModal = {
        show: true,
        title: 'Delete saved cf-group',
        message: 'Delete saved cf-group "' + (g.name || g.id) + '"?\n\nThe file is removed from /config/custom/json/' + g.appType + '/cf-groups/.\nExported .json files on disk are unaffected.',
        confirmLabel: 'Delete',
        onConfirm: () => this._cfgbDeleteConfirmed(g),
        onCancel: () => {},
      };
    },
    async _cfgbDeleteConfirmed(g) {
      // Guard against a double-fire: modal's onConfirm is a simple click
      // handler, so a quick Delete→Confirm→Delete→Confirm sequence against
      // two different groups could have their second DELETE land before
      // the first finished. The flag blocks any overlapping delete.
      if (this.cfgbDeleting) return;
      this.cfgbDeleting = true;
      try {
        const resp = await fetch('/api/cf-groups/' + g.appType + '/' + encodeURIComponent(g.id), { method: 'DELETE' });
        if (!resp.ok) {
          const text = await resp.text();
          throw new Error('HTTP ' + resp.status + ': ' + text);
        }
        await this.cfgbRefreshSaved();
        if (this.cfgbEditingId === g.id) this.cfgbReset();
        this.cfgbSavingOk = true;
        this.cfgbSavingMsg = 'Deleted.';
        setTimeout(() => { if (this.cfgbSavingMsg === 'Deleted.') this.cfgbSavingMsg = ''; }, 2000);
      } catch (e) {
        console.error('cfgbDelete failed:', e);
        this.cfgbSavingOk = false;
        this.cfgbSavingMsg = 'Delete failed: ' + e.message;
      } finally {
        this.cfgbDeleting = false;
      }
    },

    async cfgbRefreshSaved() {
      const appType = this.activeAppType;
      try {
        const resp = await fetch('/api/cf-groups/' + appType);
        if (!resp.ok) throw new Error('HTTP ' + resp.status);
        const list = await resp.json();
        this.cfgbSavedGroups = Array.isArray(list) ? list : [];
        // Re-merge local groups into dropdown + CF memberships so the
        // just-saved/deleted group shows up (or vanishes) without needing
        // a full /all-cfs refetch or losing form state.
        this.cfgbApplyLocalGroups();
      } catch (e) {
        console.error('cfgbRefreshSaved failed:', e);
      }
    },

    cfgbSelectedCustomCFCount() {
      // How many selected CFs are user-custom (IDs starting with "custom:").
      // Surfaced in a banner next to Download JSON as a warning when the
      // exported file is meant for TRaSH-Guides contribution — custom IDs
      // don't resolve in the public repo.
      let n = 0;
      for (const cf of this.cfgbCFs) {
        if (cf.isCustom && this.cfgbSelectedCFs[cf.trashId]) n++;
      }
      return n;
    },

    // --- Utility ---
  };
}

// MD5 — compact, public-domain implementation (based on Paul Johnston's
// well-tested reference). Used by the CF Group Builder to compute trash_ids
// from group names client-side. TRaSH's schema uses MD5 for identifiers so
// we match that exactly. Takes a string, returns 32-char lowercase hex.
function cfgbMD5(str) {
  function rh(n) { let s = ''; for (let j = 0; j <= 3; j++) s += ((n >> (j*8+4)) & 0xf).toString(16) + ((n >> (j*8)) & 0xf).toString(16); return s; }
  function ad(a,b) { const l = (a & 0xffff) + (b & 0xffff); return (((a >> 16) + (b >> 16) + (l >> 16)) << 16) | (l & 0xffff); }
  function rot(n,c) { return (n << c) | (n >>> (32 - c)); }
  function cm(q,a,b,x,s,t) { return ad(rot(ad(ad(a,q), ad(x,t)), s), b); }
  function ff(a,b,c,d,x,s,t){return cm((b&c)|(~b&d),a,b,x,s,t);}
  function gg(a,b,c,d,x,s,t){return cm((b&d)|(c&~d),a,b,x,s,t);}
  function hh(a,b,c,d,x,s,t){return cm(b^c^d,a,b,x,s,t);}
  function ii(a,b,c,d,x,s,t){return cm(c^(b|~d),a,b,x,s,t);}
  const utf8 = unescape(encodeURIComponent(str));
  const bl = utf8.length, nb = ((bl + 8) >> 6) + 1, x = new Array(nb*16).fill(0);
  for (let i = 0; i < bl; i++) x[i >> 2] |= utf8.charCodeAt(i) << ((i % 4) * 8);
  x[bl >> 2] |= 0x80 << ((bl % 4) * 8);
  x[nb*16 - 2] = bl * 8;
  let a = 1732584193, b = -271733879, c = -1732584194, d = 271733878;
  for (let i = 0; i < x.length; i += 16) {
    const oa=a, ob=b, oc=c, od=d;
    a=ff(a,b,c,d,x[i+0],7,-680876936);  d=ff(d,a,b,c,x[i+1],12,-389564586);  c=ff(c,d,a,b,x[i+2],17,606105819);    b=ff(b,c,d,a,x[i+3],22,-1044525330);
    a=ff(a,b,c,d,x[i+4],7,-176418897);  d=ff(d,a,b,c,x[i+5],12,1200080426);  c=ff(c,d,a,b,x[i+6],17,-1473231341);  b=ff(b,c,d,a,x[i+7],22,-45705983);
    a=ff(a,b,c,d,x[i+8],7,1770035416);  d=ff(d,a,b,c,x[i+9],12,-1958414417); c=ff(c,d,a,b,x[i+10],17,-42063);     b=ff(b,c,d,a,x[i+11],22,-1990404162);
    a=ff(a,b,c,d,x[i+12],7,1804603682); d=ff(d,a,b,c,x[i+13],12,-40341101);  c=ff(c,d,a,b,x[i+14],17,-1502002290);b=ff(b,c,d,a,x[i+15],22,1236535329);
    a=gg(a,b,c,d,x[i+1],5,-165796510);  d=gg(d,a,b,c,x[i+6],9,-1069501632);  c=gg(c,d,a,b,x[i+11],14,643717713);  b=gg(b,c,d,a,x[i+0],20,-373897302);
    a=gg(a,b,c,d,x[i+5],5,-701558691);  d=gg(d,a,b,c,x[i+10],9,38016083);    c=gg(c,d,a,b,x[i+15],14,-660478335); b=gg(b,c,d,a,x[i+4],20,-405537848);
    a=gg(a,b,c,d,x[i+9],5,568446438);   d=gg(d,a,b,c,x[i+14],9,-1019803690); c=gg(c,d,a,b,x[i+3],14,-187363961);  b=gg(b,c,d,a,x[i+8],20,1163531501);
    a=gg(a,b,c,d,x[i+13],5,-1444681467);d=gg(d,a,b,c,x[i+2],9,-51403784);    c=gg(c,d,a,b,x[i+7],14,1735328473);  b=gg(b,c,d,a,x[i+12],20,-1926607734);
    a=hh(a,b,c,d,x[i+5],4,-378558);     d=hh(d,a,b,c,x[i+8],11,-2022574463); c=hh(c,d,a,b,x[i+11],16,1839030562); b=hh(b,c,d,a,x[i+14],23,-35309556);
    a=hh(a,b,c,d,x[i+1],4,-1530992060); d=hh(d,a,b,c,x[i+4],11,1272893353);  c=hh(c,d,a,b,x[i+7],16,-155497632);  b=hh(b,c,d,a,x[i+10],23,-1094730640);
    a=hh(a,b,c,d,x[i+13],4,681279174);  d=hh(d,a,b,c,x[i+0],11,-358537222);  c=hh(c,d,a,b,x[i+3],16,-722521979);  b=hh(b,c,d,a,x[i+6],23,76029189);
    a=hh(a,b,c,d,x[i+9],4,-640364487);  d=hh(d,a,b,c,x[i+12],11,-421815835); c=hh(c,d,a,b,x[i+15],16,530742520);  b=hh(b,c,d,a,x[i+2],23,-995338651);
    a=ii(a,b,c,d,x[i+0],6,-198630844);  d=ii(d,a,b,c,x[i+7],10,1126891415);  c=ii(c,d,a,b,x[i+14],15,-1416354905);b=ii(b,c,d,a,x[i+5],21,-57434055);
    a=ii(a,b,c,d,x[i+12],6,1700485571); d=ii(d,a,b,c,x[i+3],10,-1894986606); c=ii(c,d,a,b,x[i+10],15,-1051523);   b=ii(b,c,d,a,x[i+1],21,-2054922799);
    a=ii(a,b,c,d,x[i+8],6,1873313359);  d=ii(d,a,b,c,x[i+15],10,-30611744);  c=ii(c,d,a,b,x[i+6],15,-1560198380); b=ii(b,c,d,a,x[i+13],21,1309151649);
    a=ii(a,b,c,d,x[i+4],6,-145523070);  d=ii(d,a,b,c,x[i+11],10,-1120210379);c=ii(c,d,a,b,x[i+2],15,718787259);   b=ii(b,c,d,a,x[i+9],21,-343485551);
    a=ad(a,oa); b=ad(b,ob); c=ad(c,oc); d=ad(d,od);
  }
  return rh(a) + rh(b) + rh(c) + rh(d);
}

// Clipboard fallback for non-HTTPS contexts (e.g., LAN HTTP access)
function copyToClipboard(text) {
  if (navigator.clipboard && window.isSecureContext) {
    return navigator.clipboard.writeText(text);
  }
  const ta = document.createElement('textarea');
  ta.value = text;
  ta.style.position = 'fixed';
  ta.style.left = '-9999px';
  document.body.appendChild(ta);
  ta.select();
  document.execCommand('copy');
  document.body.removeChild(ta);
  return Promise.resolve();
}
