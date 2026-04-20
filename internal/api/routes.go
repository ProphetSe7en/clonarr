package api

import "net/http"

func (s *Server) registerRoutes(mux *http.ServeMux) {
	// core.Config
	mux.HandleFunc("GET /api/config", s.handleGetConfig)
	mux.HandleFunc("PUT /api/config", s.handleUpdateConfig)

	// Instances
	mux.HandleFunc("GET /api/instances", s.handleListInstances)
	mux.HandleFunc("POST /api/instances", s.handleCreateInstance)
	mux.HandleFunc("PUT /api/instances/{id}", s.handleUpdateInstance)
	mux.HandleFunc("DELETE /api/instances/{id}", s.handleDeleteInstance)
	mux.HandleFunc("POST /api/instances/{id}/test", s.handleTestInstance)
	mux.HandleFunc("POST /api/test-connection", s.handleTestConnection)
	mux.HandleFunc("GET /api/instances/{id}/profiles", s.handleInstanceProfiles)
	mux.HandleFunc("PUT /api/instances/{id}/profiles/{profileId}/rename", s.handleRenameProfile)
	mux.HandleFunc("GET /api/instances/{id}/languages", s.handleInstanceLanguages)
	mux.HandleFunc("GET /api/instances/{id}/cfs", s.handleInstanceCFs)
	mux.HandleFunc("GET /api/instances/{id}/quality-sizes", s.handleInstanceQualitySizes)
	mux.HandleFunc("POST /api/instances/{id}/quality-sizes/sync", s.handleSyncQualitySizes)
	mux.HandleFunc("GET /api/instances/{id}/quality-sizes/overrides", s.handleGetQSOverrides)
	mux.HandleFunc("PUT /api/instances/{id}/quality-sizes/overrides", s.handleSaveQSOverrides)
	mux.HandleFunc("GET /api/instances/{id}/quality-sizes/auto-sync", s.handleGetQSAutoSync)
	mux.HandleFunc("PUT /api/instances/{id}/quality-sizes/auto-sync", s.handleSaveQSAutoSync)
	mux.HandleFunc("GET /api/instances/{id}/quality-definitions", s.handleQualityDefinitions)
	mux.HandleFunc("GET /api/instances/{id}/profile-export/{profileId}", s.handleInstanceProfileExport)
	mux.HandleFunc("POST /api/instances/{id}/backup", s.handleInstanceBackup)
	mux.HandleFunc("POST /api/instances/{id}/restore", s.handleInstanceRestore)
	mux.HandleFunc("GET /api/instances/{id}/naming", s.handleGetInstanceNaming)
	mux.HandleFunc("PUT /api/instances/{id}/naming", s.handleApplyNaming)
	mux.HandleFunc("GET /api/instances/{id}/compare", s.handleCompareProfile)
	mux.HandleFunc("POST /api/instances/{id}/profile-cfs/remove", s.handleRemoveProfileCFs)
	mux.HandleFunc("POST /api/instances/{id}/profile-cfs/sync-one", s.handleSyncSingleCF)

	// TRaSH
	mux.HandleFunc("GET /api/trash/status", s.handleTrashStatus)
	mux.HandleFunc("POST /api/trash/pull", s.handleTrashPull)
	mux.HandleFunc("GET /api/trash/{app}/cfs", s.handleTrashCFs)
	mux.HandleFunc("GET /api/trash/{app}/score-contexts", s.handleTrashScoreContexts)
	mux.HandleFunc("GET /api/trash/{app}/cf-groups", s.handleTrashCFGroups)
	mux.HandleFunc("GET /api/trash/{app}/profiles", s.handleTrashProfiles)
	mux.HandleFunc("GET /api/trash/{app}/profiles/{id}", s.handleTrashProfileDetail)
	mux.HandleFunc("GET /api/trash/{app}/quality-sizes", s.handleTrashQualitySizes)
	mux.HandleFunc("GET /api/trash/{app}/naming", s.handleTrashNaming)
	mux.HandleFunc("GET /api/trash/{app}/conflicts", s.handleTrashConflicts)

	// Import
	mux.HandleFunc("POST /api/import/profile", s.handleImportProfile)
	mux.HandleFunc("GET /api/import/{app}/profiles", s.handleGetImportedProfiles)
	mux.HandleFunc("GET /api/import/profiles/{id}/detail", s.handleImportedProfileDetail)
	mux.HandleFunc("PUT /api/import/profiles/{id}", s.handleUpdateImportedProfile)
	mux.HandleFunc("DELETE /api/import/profiles/{id}", s.handleDeleteImportedProfile)

	// Custom Profiles
	mux.HandleFunc("GET /api/trash/{app}/quality-presets", s.handleQualityPresets)
	mux.HandleFunc("GET /api/trash/{app}/all-cfs", s.handleAllCFsCategorized)
	mux.HandleFunc("POST /api/custom-profiles", s.handleCreateCustomProfile)
	mux.HandleFunc("PUT /api/custom-profiles/{id}", s.handleUpdateCustomProfile)

	// Custom CFs
	mux.HandleFunc("GET /api/custom-cfs/{app}", s.handleListCustomCFs)
	mux.HandleFunc("POST /api/custom-cfs", s.handleCreateCustomCFs)
	mux.HandleFunc("DELETE /api/custom-cfs/{id}", s.handleDeleteCustomCF)
	mux.HandleFunc("PUT /api/custom-cfs/{id}", s.handleUpdateCustomCF)
	mux.HandleFunc("POST /api/custom-cfs/import-from-instance", s.handleImportCFsFromInstance)
	mux.HandleFunc("GET /api/customformat/schema/{app}", s.handleCFSchema)

	// Sync
	mux.HandleFunc("POST /api/sync/dry-run", s.handleDryRun)
	mux.HandleFunc("POST /api/sync/apply", s.handleApply)

	// Sync History
	mux.HandleFunc("GET /api/instances/{id}/sync-history", s.handleSyncHistory)
	mux.HandleFunc("GET /api/instances/{id}/sync-history/{arrProfileId}/changes", s.handleProfileChangeHistory)
	mux.HandleFunc("DELETE /api/instances/{id}/sync-history/{arrProfileId}", s.handleDeleteSyncHistory)

	// Auto-Sync
	mux.HandleFunc("GET /api/auto-sync/settings", s.handleGetAutoSyncSettings)
	mux.HandleFunc("PUT /api/auto-sync/settings", s.handleSaveAutoSyncSettings)
	// Notification agents: each provider (Discord, Gotify, Pushover) is
	// configured as an independent agent with its own credentials and event
	// subscriptions. Use the inline route to test before saving, or the
	// saved-agent route to re-test an existing agent by ID.
	mux.HandleFunc("GET /api/auto-sync/notification-agents", s.handleListNotificationAgents)
	mux.HandleFunc("POST /api/auto-sync/notification-agents", s.handleCreateNotificationAgent)
	mux.HandleFunc("PUT /api/auto-sync/notification-agents/{id}", s.handleUpdateNotificationAgent)
	mux.HandleFunc("DELETE /api/auto-sync/notification-agents/{id}", s.handleDeleteNotificationAgent)
	mux.HandleFunc("POST /api/auto-sync/notification-agents/test", s.handleTestNotificationAgentInline)
	mux.HandleFunc("POST /api/auto-sync/notification-agents/{id}/test", s.handleTestNotificationAgent)
	mux.HandleFunc("GET /api/auto-sync/rules", s.handleListAutoSyncRules)
	mux.HandleFunc("POST /api/auto-sync/rules", s.handleCreateAutoSyncRule)
	mux.HandleFunc("PUT /api/auto-sync/rules/{id}", s.handleUpdateAutoSyncRule)
	mux.HandleFunc("DELETE /api/auto-sync/rules/{id}", s.handleDeleteAutoSyncRule)

	// Cleanup events
	mux.HandleFunc("GET /api/cleanup-events", s.handleCleanupEvents)
	mux.HandleFunc("GET /api/auto-sync/events", s.handleAutoSyncEvents)

	// Debug logging
	mux.HandleFunc("POST /api/debug/log", s.handleDebugLog)
	mux.HandleFunc("GET /api/debug/log/download", s.handleDebugDownload)

	// Cleanup
	mux.HandleFunc("POST /api/instances/{id}/cleanup/scan", s.handleCleanupScan)
	mux.HandleFunc("POST /api/instances/{id}/cleanup/apply", s.handleCleanupApply)
	mux.HandleFunc("GET /api/instances/{id}/cleanup/keep", s.handleGetCleanupKeep)
	mux.HandleFunc("PUT /api/instances/{id}/cleanup/keep", s.handleSaveCleanupKeep)

	// Scoring Sandbox
	mux.HandleFunc("POST /api/prowlarr/test", s.handleTestProwlarr)
	mux.HandleFunc("GET /api/scoring/prowlarr/indexers", s.handleScoringProwlarrIndexers)
	mux.HandleFunc("POST /api/scoring/prowlarr/search", s.handleScoringProwlarrSearch)
	mux.HandleFunc("POST /api/scoring/parse", s.handleScoringParse)
	mux.HandleFunc("POST /api/scoring/parse/batch", s.handleScoringParseBatch)
	mux.HandleFunc("GET /api/scoring/profile-scores", s.handleScoringProfileScores)
}
