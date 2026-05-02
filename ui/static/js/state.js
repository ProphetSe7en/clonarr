export default function baseState() {
  return {
    currentTab: 'settings',  // LEGACY — being replaced by currentSection + activeAppType
    currentSection: 'profiles',  // NEW — feature-first: 'profiles', 'custom-formats', 'quality-size', 'naming', 'maintenance', 'advanced', 'settings', 'about'
    activeAppType: 'radarr',     // NEW — 'radarr' or 'sonarr', independent of section
    advancedTab: 'builder',      // NEW — sub-tab within Advanced: 'builder', 'scoring', 'group-builder'

    // Debug-log download options. When true, the Download button hits
    // ?activity=1 and the server bundles activity.log alongside debug.log
    // in a ZIP. Default off — most bug reports only need the operation
    // trace (debug.log alone).
    includeActivityLog: false,

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
    // cfgbGroup is the TRaSH-style sort-order integer: lower = higher in the
    // cf-group list. Null when not set (group lands in "Other" tier between
    // numbered groups and custom). Per TRaSH convention: 1-9 English public,
    // 11-19 German, 21-29 French, 81-89 Anime, 91-99 SQP.
    cfgbGroup: null,
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
    config: { trashRepo: { url: '', branch: '' }, pullInterval: '24h', prowlarr: { url: '', apiKey: '', enabled: false, radarrCategories: [], sonarrCategories: [] }, authentication: 'forms', authenticationRequired: 'disabled_for_local_addresses', trustedNetworks: '', trustedProxies: '', sessionTtlDays: 30 },
    trashStatus: {},
    _nowTick: Date.now(),
    trashProfiles: { radarr: [], sonarr: [] },
    expandedInstances: {},
    expandedProfileGroups: {},
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
    cleanupFilter: 'all', // unused-by-clonarr only: 'all' | 'rename-flagged' | 'managed'

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

    settingsOpen: 'instances',  // legacy accordion (unused after sidebar redesign)
    settingsSection: 'instances',
    uiScale: localStorage.getItem('clonarr-ui-scale') || '1',

    // Scoring Sandbox (per app-type state)
    sandbox: {
      radarr: { instanceId: '', profileKey: '', compareKey: '', editOpen: false, editScores: {}, editToggles: {}, editMinScore: null, editOriginal: null, inputMode: 'paste', pasteInput: '', bulkInput: '', searchQuery: '', selectedIndexers: [], indexers: [], searchResults: [], results: [], parsing: false, searching: false, searchAbort: null, instanceProfiles: [], showBulk: false, searchError: '', indexerDropdown: false, searchFilterText: '', searchFilterRes: '', sortCol: 'score', sortDir: 'desc', filterToSelected: false, dragSrc: null, dragOver: null, scoreSets: [], activeScoreSet: '' },
      sonarr: { instanceId: '', profileKey: '', compareKey: '', editOpen: false, editScores: {}, editToggles: {}, editMinScore: null, editOriginal: null, inputMode: 'paste', pasteInput: '', bulkInput: '', searchQuery: '', selectedIndexers: [], indexers: [], searchResults: [], results: [], parsing: false, searching: false, searchAbort: null, instanceProfiles: [], showBulk: false, searchError: '', indexerDropdown: false, searchFilterText: '', searchFilterRes: '', sortCol: 'score', sortDir: 'desc', filterToSelected: false, dragSrc: null, dragOver: null, scoreSets: [], activeScoreSet: '' },
    },
    prowlarrTestResult: null,
    prowlarrTesting: false,

  };
}
