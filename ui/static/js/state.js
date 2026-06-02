export default function baseState() {
  return {
    currentTab: 'settings',  // LEGACY — being replaced by currentSection + activeAppType
    currentSection: 'profiles',  // NEW — feature-first: 'profiles', 'custom-formats', 'quality-size', 'naming', 'maintenance', 'advanced', 'settings', 'about'
    activeAppType: 'radarr',     // NEW — 'radarr' or 'sonarr', independent of section
    advancedTab: 'builder',      // NEW — sub-tab within Advanced: 'builder', 'scoring', 'group-builder'

    // cfSyncRules: per-app-type cache of the per-CF view. Populated by
    // loadCFSyncRules and refreshed after a successful applyCFDrift.
    cfSyncRules: { radarr: [], sonarr: [] },
    cfSyncRulesLoaded: { radarr: false, sonarr: false },
    // cfApplyingKey: "<instanceId>:<trashId>" of the in-flight apply, so
    // the matching button can switch to "Applying..." without racing
    // multiple Apply clicks on the same row.
    cfApplyingKey: '',
    // CF sub-tab sidebar filter — 'all' shows every managed CF,
    // 'cat:<name>' narrows to one category. Per-browser persisted so
    // returning to the sub-tab restores the last filter the user
    // picked. Matches the Custom Formats Browse tab's own persisted
    // category state in localStorage shape.
    cfSyncRulesActiveCat: 'all',
    // CF sub-tab instance picker — '' means "every instance of the
    // active app type"; otherwise a specific instance ID. Narrowing
    // to one instance scopes the row list AND the drift counts AND
    // the Update all batch to that instance, so a 4K-Radarr-only
    // drift event can be acted on without touching main Radarr.
    cfSyncRulesActiveInstance: '',
    // CF sub-tab search query — matches against CF name OR any of
    // the row's usedByProfiles[].profileName entries. Lets a user
    // find "all CFs in SQP-3" or "Bad Release Group" without
    // scrolling. Case-insensitive substring.
    cfSyncRulesSearch: '',
    // CF sub-tab expand-row state — trashId of the currently-open
    // detail row showing per-profile usage. Single-open (clicking a
    // different row collapses the previous) to keep the layout
    // compact; empty string = nothing expanded.
    cfSyncRulesExpandedRow: '',
    // "Apply all" progress state — total drifted (instance, trashId)
    // pairs queued, completed so far, current label. Visible while
    // running so the user sees the batch progressing rather than a
    // single multi-second spinner with no indication something's
    // happening. Cleared on completion.
    cfApplyAllProgress: { running: false, total: 0, done: 0, label: '' },
    // Per-row drift detail cache. Keyed by "<instanceId>:<trashId>"
    // so the same CF on two instances caches independently. Loaded
    // lazily when a drifted row is expanded; entries persist for the
    // tab session so re-opening a previously-viewed row is instant.
    // Shape: { loading: bool, diff: CFSpecDiff|null, error: string }.
    cfDriftDiffCache: {},
    // CF sub-tab sidebar parent-collapse state. Map of parentName →
    // explicit visibility. Loaded from localStorage on init; mutated
    // via cfSRToggleSidebarParent. Empty value means "auto-expand
    // when active filter targets this parent or a child of it".
    cfSyncRulesSidebarExpanded: (() => {
      try { return JSON.parse(localStorage.getItem('clonarr_cfSRSidebarExpanded') || '{}'); }
      catch (_) { return {}; }
    })(),

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
    config: { trashRepo: { url: '', branch: '' }, pullInterval: '24h', pullSchedule: { mode: 'daily', time: '03:00', dayOfWeek: 0, dayOfMonth: 1 }, profileSync: { mode: 'auto', interval: '24h', applyDelayMinutes: 1440, sources: { trashUpstream: true, arrDrift: false } }, prowlarr: { url: '', apiKey: '', enabled: false, radarrCategories: [], sonarrCategories: [] }, authentication: 'forms', authenticationRequired: 'disabled_for_local_addresses', trustedNetworks: '', trustedProxies: '', sessionTtlDays: 30 },
    trashStatus: {},
    _nowTick: Date.now(),
    _trashStatusFetchedAt: 0, // ms; set by loadTrashStatus(). Declared here so Alpine tracks it; the next-pull countdown reads it together with _nowTick to compute server-relative remaining time.
    trashProfiles: { radarr: [], sonarr: [] },
    expandedInstances: {},
    expandedProfileGroups: {},
    pulling: false,
    checkingUpdates: false,
    // Quick action modal — opens on row-level status pill click. Lets the
    // user see what changed AND sync without going through the full editor.
    statusReviewOpen: false,
    statusReviewKind: '',     // 'drift' | 'updates' | 'pending'
    statusReviewInst: null,
    statusReviewSh: null,
    statusReviewApplying: false,
    // Pending kind needs a dry-run round-trip to produce an accurate
    // change list (frontend can't diff resolved-set vs opt-ins-only state).
    statusReviewPendingPlan: null,
    statusReviewPendingLoading: false,
    statusReviewPendingError: '',
    updatingInstance: '', // instance id currently running Update all
    updatingRuleId: '',   // rule id currently running Update profile
    trashResetting: false,
    profileTabs: {},  // per app-type profile tab: { radarr: 'trash-profiles', sonarr: 'sync-rules', ... }
    // Per app-type Custom Formats sub-tab: { radarr: 'browse' | 'sync-rules', sonarr: ... }.
    // browse = the existing TRaSH + custom CF catalog (default).
    // sync-rules = per-CF state for everything any rule pushes to Arr,
    // including drift status + per-instance Update. Mirror of how
    // Profiles section uses sub-tabs (TRaSH Profiles + Sync Rules).
    customFormatsTabs: {},
    // Per app-type Media Management sub-tab: { radarr: 'quality' | 'naming', sonarr: ... }.
    // Section merges the former 'quality-size' + 'naming' top-level pages so
    // *arr-savvy users find both under the same "Media Management" heading.
    mediaTabs: {},
    // Per app-type Maintenance sub-tab: { radarr: 'backup' | 'cleanup', sonarr: ... }.
    // Splits the long Backup & Maintenance page into focused workflows —
    // Backup & Restore (data preservation) vs Cleanup (data hygiene).
    maintenanceTabs: {},
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
    // Custom viewport-aware tooltip — replaces native title="" for elements
    // where the browser tooltip would overflow the viewport (right-edge inputs,
    // long messages, etc.). Driven by showTooltip / hideTooltip helpers in
    // main.js. The global tooltip element lives in partials/modals/tooltip.html.
    tt: { show: false, text: '', x: 0, y: 0, flip: false, placement: 'top' },
    selectedOptionalCFs: {},
    // Profile detail — single global toggle that gates all override editing affordances.
    // OFF (default): user sees a clean "All values follow profile defaults" summary;
    // override cards (General, Quality, Overridden Scores, Extra CFs) are hidden;
    // CF score inputs in Required/Group sections render as read-only colored badges.
    // ON: all 4 override cards appear; score inputs become editable; Quality Edit button shows.
    // Auto-enabled by restoreFromSyncHistory when any saved override is detected, so the
    // toggle always reflects the actual persisted state of the rule (no silent "default" lie).
    pdOverridesEnabled: false,
    // Free-form notes attached to the current sync rule. Edited via the
    // Notes panel in Sync Preview; persisted to AutoSyncRule.Description.
    // pdDescriptionPreview toggles between the markdown textarea and
    // its rendered preview.
    pdDescription: '',
    pdDescriptionPreview: false,
    // Profile overview's Notes card defaults to collapsed — keeps the
    // overview compact since notes are optional. Header always shows;
    // body (editor + preview) renders only when expanded.
    pdNotesExpanded: false,
    // Snapshot of profile-editor state at open time. Used by
    // profileDetailIsDirty to detect unsaved changes — set in
    // openProfileDetail after restoration completes, cleared on
    // save-success + on explicit Discard. JSON string for cheap
    // structural compare.
    _profileBaseline: null,
    spActiveTab: 'default',     // 'default' | 'overview' | 'additional' (Customize-gated)
    spActiveGroup: '__required', // '__required' | <trashGroup.name>
    spOverviewSection: 'all',    // 'all' | 'diffs' | 'general' | 'quality' | 'all-cf' | 'optional-cf' | 'additional-cf'
    // Sync Preview CF search — single state shared across the three
    // sub-nav surfaces (Profile default, Additional CF, Profile
    // overview). When set, panes filter their CF rows to ones whose
    // name matches case-insensitively.
    spSearchFilter: '',
    // Sync Preview sidebar collapsed/expanded state per section
    // (e.g. {"Streaming Services": true, "Audio Formats": false}).
    // Persistent overrides written by chevron click. localStorage-
    // persisted so survives reload.
    spSidebarExpanded: (() => {
      try { return JSON.parse(window.localStorage.getItem('sp-sidebar-expanded') || '{}'); }
      catch (_) { return {}; }
    })(),
    // Transient sidebar expand from label-click. Single section at a
    // time. Cleared whenever the user navigates to a specific group
    // (Required CFs button, group button, etc.) so the section
    // collapses again. Mirrors CF Management's "click parent label =
    // transient expand" pattern. Not persisted.
    spExpandedSection: null,
    spOverviewSort: 'default',   // 'default' | 'name-asc' | 'name-desc' | 'score-desc' | 'score-asc'
    // Show CF section groupings on Overview (true) vs flat list (false).
    // Persisted per-browser via localStorage so the user's choice
    // survives reloads.
    spOverviewGroupCFs: (() => {
      try { return window.localStorage.getItem('sp-ov-group-cfs') === 'true'; } catch (e) { return false; }
    })(),
    // Quality editor modal target — drives which array the modal binds to:
    //   'builder' → pb.qualityItems (Profile Builder flow)
    //   'edit'    → qualityStructure (Profile Detail / Sync Preview flow)
    // Set by the launcher button before flipping pb.qualityEditorOpen.
    qualityEditorTarget: 'builder',
    // Sync Preview's Customize state reads pdOverridesEnabled directly —
    // no separate spCustomize field. A separate field could drift when
    // user toggles Customize in one overlay then switches to the other.
    pdGeneralCollapsed: false,  // Profile-detail General card chevron collapse state (default expanded)
    pdQualityCollapsed: false,  // Profile-detail Quality card chevron collapse state (default expanded)
    pdCFScoresCollapsed: true,  // Profile-detail Overridden Scores card chevron collapse state (default collapsed — list-style sections opened on demand)
    pdExtraCFsCollapsed: true,  // Profile-detail Extra CFs card chevron collapse state (default collapsed — picker lazy-loaded only when user expands)
    // Compare-tab sub-nav. Values:
    // 'overview' | 'optional' | 'general' | 'quality' | 'all-diffs'
    // 'wrong' | 'extra' | 'missing' | 'all-active'
    compareFilter: 'overview',
    cfScoreOverrides: {}, // per-CF score overrides { trashId: score }
    qualityOverrides: {}, // legacy flat overrides { name: allowed(bool) } — kept for backwards compat
    qualityOverrideActive: false, // Quality Items editor modal-open flag (NOT a persistence gate)
    // Quality structure override (full structure replacing TRaSH items).
    // Format: [{ _id, name, allowed, items?: [string] }]. Empty when not in use.
    // When non-empty, this is sent as `qualityStructure` to backend and trumps qualityOverrides.
    qualityStructure: [],
    qualityStructureEditMode: false,
    qualityStructureExpanded: {},
    qualityStructureRenaming: null,
    qualityStructureDrag: { kind: null, src: null, srcGroup: null, srcMember: null, dropGap: null, dropMerge: null, dropMemberGroup: null, dropMemberGap: null },
    _qsIdCounter: 0,
    _sbIdCounter: 0,
    extraCFs: {}, // { trashId: score } — extra CFs not in profile
    // Compare → override-editor convergence (Phase 1): list of Arr CFs not in
    // any TRaSH cf-group for the compared profile. Populated by
    // prefillOverridesFromCompare; rendered in a Phase 2 UI sub-section that
    // mirrors Additional CFs but writes to rule.keepArrCFIDs on save.
    // [{ arrCFID, name, currentScore }] or [] when not in compare flow.
    _compareArrOnlyExtras: [],
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
    importCFList: [],           // [{name, selected, exists, trashMatch}]
    importCFLoading: false,
    importCFFilter: '',         // free-text name filter for the picker
    importCFHideGuide: false,   // hide CFs that also live in TRaSH guides
    importCFHideExisting: true, // hide already-imported (un-selectable) entries
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
    cfEditorSchemaError: '',     // populated when schema fetch fails (Arr unreachable, no instance, etc.)
    cfEditorSpecCounter: 0,     // unique ID counter for x-for keys (specifications)
    cfEditorScoreCounter: 0,    // unique ID counter for x-for keys (trashScores)
    cfEditorActiveTab: 'general', // selected tab inside the editor modal (General / Conditions / TRaSH)
    cfEditorDescriptionPreview: false, // toggle for the Description field's edit-vs-preview view
    // Inline link-popover state for the markdown editor — replaces the
    // native window.prompt that used to back the Link toolbar button.
    cfMdLinkPopover: { open: false, target: null, url: '', selStart: 0, selEnd: 0 },

    // Quality sizes (cached per app type)
    qualitySizesPerApp: {},
    qsExpanded: {},
    selectedQSType: {},  // per app-type: index into quality sizes array
    // Per app-type Media Management instance picker. Shared by both
    // Quality Definitions and Movie/Episode Naming sub-tabs so the
    // picker stays at the same position when the user switches
    // sub-tabs — previously each sub-tab had its own picker that
    // jittered between them.
    mediaInstanceId: {},
    qsInstanceDefs: {},  // per app-type: current instance quality definitions
    qsOverrides: {},     // per app-type: { qualityName: { min, preferred, max } }
    qsSyncing: {},       // per app-type: boolean
    qsSyncResult: {},    // per app-type: { ok, message }
    qsAutoSync: {},      // per app-type: { enabled, type }
    confirmModal: { show: false, title: '', message: '', confirmLabel: '', cancelLabel: '', secondaryLabel: '', hideCancel: false, onConfirm: null, onCancel: null, onSecondary: null },
    inputModal: { show: false, title: '', message: '', value: '', placeholder: '', confirmLabel: '', onConfirm: null, onCancel: null },
    cloneProfileModal: { open: false, sh: null, sourceInstanceId: '', appType: '', sourceName: '', name: '', targetInstanceId: '', saving: false, error: '' },
    sandboxCopyModal: { show: false, title: '', text: '', copied: false },
    // Sandbox export modal — exports the currently-visible-and-sorted
    // result list as plain text, diff-tool friendly (line-per-release).
    // includeBreakdown toggles between "title + total only" (compact,
    // good for high-level diff) and "per-release block with CF rows"
    // (deep diff for per-CF score comparison across two sessions).
    // The "no scoring" path is implicit: if a release has no scoring,
    // its line shows just the title regardless of toggle.
    sandboxExportModal: { show: false, appType: '', includeBreakdown: false, text: '', copied: false },
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
    userMenuOpen: false,            // v3 banner: user-chip click toggles logout popover
    // v3 sidebar: when collapsed, the section icons can't show their sub-nav
    // (no room). Clicking an icon that owns sub-tabs (Profiles / Advanced)
    // opens a flyout to the right with the same options. Value holds the
    // section key whose popup is open, or '' for closed.
    sidebarSubnavPopup: '',
    // Top viewport coordinate where the popup should anchor — captured
    // from the clicked icon's getBoundingClientRect on open so the popup
    // sits aligned with whichever nav-item was clicked.
    sidebarSubnavPopupTop: 0,
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
    cleanupSelected: {},  // unused-by-clonarr only: { [cfId]: true } for per-row selection

    syncForm: { instanceId: '', instanceName: '', appType: '', profileTrashId: '', importedProfileId: '', profileName: '', arrProfileId: '0', newProfileName: '', behavior: { addMode: 'add_missing', removeMode: 'remove_custom', resetMode: 'reset_to_zero' } },
    arrProfiles: [],
    instanceLanguages: {},  // instanceId → [{id, name}] cache
    syncPlan: null,
    syncResult: null,
    syncResultDetailsOpen: false,
    dryrunDetailsOpen: false,
    syncing: false,
    savingRule: false,       // Save-only PUT in flight (Profile Detail editor "Save" button)
    syncPreview: null,       // dry-run preview for update mode in sync modal
    syncPreviewLoading: false,

    settingsOpen: 'instances',  // legacy accordion (unused after sidebar redesign)
    settingsSection: 'instances',
    uiScale: localStorage.getItem('clonarr-ui-scale') || '1',
    theme: localStorage.getItem('clonarr-theme') || 'system',
    // v3 sidebar collapse state — persists per-browser. Default expanded.
    sidebarCollapsed: localStorage.getItem('clonarr-sidebar-collapsed') === '1',
    // v3 content alignment — 'center' (default, balanced) or 'left'
    // (anchored next to the sidebar, shorter mouse travel on widescreen).
    contentAlign: localStorage.getItem('clonarr-content-align') || 'center',
    // v3 navigation style — 'sidebar' (default) or 'topnav'. Some beta
    // testers prefer the classic horizontal navigation; the topnav variant
    // is v3-styled (app-color underline, app pill, 4-sub-tab Profiles
    // split) so it carries the same visual language as the sidebar.
    navStyle: localStorage.getItem('clonarr-nav-style') || 'sidebar',
    // v3 Sync Rules — per-rule customization-count cache. Keyed by rule
    // ID, populated by loadRuleCustomizations() when the Sync Rules tab
    // mounts. Each entry: { quality, extraCFs, customScores, general, total }.
    // Empty {} = not yet loaded; missing keys = unknown rule (will render —).
    ruleCustomizations: {},
    ruleCustomizationsLoaded: false,

    // Scoring Sandbox (per app-type state)
    sandbox: {
      radarr: { instanceId: '', profileKey: '', compareKey: '', editOpen: false, editScores: {}, editToggles: {}, editMinScore: null, editOriginal: null, inputMode: 'paste', pasteInput: '', bulkInput: '', searchQuery: '', selectedIndexers: [], indexers: [], searchResults: [], results: [], parsing: false, searching: false, searchAbort: null, instanceProfiles: [], showBulk: false, searchError: '', indexerDropdown: false, searchFilterText: '', searchFilterRes: '', sortCol: 'score', sortDir: 'desc', filterToSelected: false, dragSrc: null, dragOver: null, scoreSets: [], activeScoreSet: '', searchCooldownRemaining: 0 },
      sonarr: { instanceId: '', profileKey: '', compareKey: '', editOpen: false, editScores: {}, editToggles: {}, editMinScore: null, editOriginal: null, inputMode: 'paste', pasteInput: '', bulkInput: '', searchQuery: '', selectedIndexers: [], indexers: [], searchResults: [], results: [], parsing: false, searching: false, searchAbort: null, instanceProfiles: [], showBulk: false, searchError: '', indexerDropdown: false, searchFilterText: '', searchFilterRes: '', sortCol: 'score', sortDir: 'desc', filterToSelected: false, dragSrc: null, dragOver: null, scoreSets: [], activeScoreSet: '', searchCooldownRemaining: 0 },
    },
    prowlarrTestResult: null,
    prowlarrTesting: false,

  };
}
