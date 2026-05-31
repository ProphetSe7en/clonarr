package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"clonarr/internal/arr"
	"clonarr/internal/core"
)

// handleCFDriftApply re-pushes clonarr's saved spec for a single CF on
// a single Arr instance, overwriting whatever the user (or another
// tool) edited directly in Arr. On success the per-(instance, trashID)
// fingerprint is cleared from Instance.CFDriftFingerprints so the
// next Check sees the CF as in-sync, a SyncHistoryEntry tagged "cf"
// lands so the History tab's Custom Formats sub-tab carries an audit
// row, and NotifyCFDriftReconciled fires per the dedup rules in
// cf_drift.go.
//
// Body shape: {instanceId, trashId}. The trashId may be either a
// real TRaSH-Guides id (in which case the spec is loaded from
// /data/trash-guides/) or a custom: prefix (in which case the spec
// is loaded from the user's CustomCFs registry). Both flows resolve
// the same TrashCF shape; the arr.ArrClient.UpdateCustomFormat call
// is the same regardless of source.
func (s *Server) handleCFDriftApply(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InstanceID string `json:"instanceId"`
		TrashID    string `json:"trashId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return
	}
	if body.InstanceID == "" || body.TrashID == "" {
		writeError(w, http.StatusBadRequest, "instanceId and trashId are required")
		return
	}

	cfg := s.Core.Config.Get()
	var inst *core.Instance
	for i := range cfg.Instances {
		if cfg.Instances[i].ID == body.InstanceID {
			inst = &cfg.Instances[i]
			break
		}
	}
	if inst == nil {
		writeError(w, http.StatusNotFound, "instance not found")
		return
	}

	// Resolve disk spec: TRaSH or custom. resolveCFForApply mirrors the
	// detection-side resolveCFDiskSpec helper so apply and detection
	// can't disagree about what "clonarr's saved state" looks like.
	appData := s.Core.Trash.GetAppData(inst.Type)
	customs := s.Core.CustomCFs.List(inst.Type)
	customsByID := make(map[string]core.CustomCF, len(customs))
	for _, c := range customs {
		customsByID[c.ID] = c
	}

	var diskCFName string
	var arrCFPayload *arr.ArrCF
	if appData != nil {
		if cf, ok := appData.CustomFormats[body.TrashID]; ok && cf != nil {
			diskCFName = cf.Name
			arrCFPayload = core.TrashCFToArr(cf)
		}
	}
	if arrCFPayload == nil {
		if c, ok := customsByID[body.TrashID]; ok {
			diskCFName = c.Name
			arrCFPayload = core.CustomCFToArr(&c)
		}
	}
	if arrCFPayload == nil {
		writeError(w, http.StatusNotFound, "trash id not in TRaSH data or custom CFs")
		return
	}

	// Find the live CF in Arr so we know the id to PUT against.
	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
	liveCFs, err := client.ListCustomFormats()
	if err != nil {
		writeError(w, http.StatusBadGateway, "list arr custom formats: "+err.Error())
		return
	}
	var liveID int
	for _, cf := range liveCFs {
		if cf.Name == diskCFName {
			liveID = cf.ID
			break
		}
	}
	if liveID == 0 {
		writeError(w, http.StatusNotFound, "custom format not found in arr (it would be created on the next sync)")
		return
	}

	if _, err := client.UpdateCustomFormat(liveID, arrCFPayload); err != nil {
		writeError(w, http.StatusBadGateway, "update arr custom format: "+err.Error())
		return
	}

	now := time.Now().UTC().Format(time.RFC3339)

	// Clear the fingerprint entry so the next Check sees in-sync state,
	// and write a one-CF history entry so the user has audit trail.
	// Both go through one Config.Update closure so a reader can't catch
	// half-applied state.
	if err := s.Core.Config.Update(func(c *core.Config) {
		for i := range c.Instances {
			if c.Instances[i].ID != body.InstanceID {
				continue
			}
			if len(c.Instances[i].CFDriftFingerprints) == 0 {
				continue
			}
			delete(c.Instances[i].CFDriftFingerprints, body.TrashID)
			if len(c.Instances[i].CFDriftFingerprints) == 0 {
				// Drop the map entirely so the JSON output stays
				// quiet — omitempty doesn't fire for an empty (but
				// non-nil) map.
				c.Instances[i].CFDriftFingerprints = nil
			}
		}
	}); err != nil {
		log.Printf("cf-drift apply: clear fingerprint failed: %v", err)
	}

	entry := core.SyncHistoryEntry{
		InstanceID:   inst.ID,
		InstanceType: inst.Type,
		// CF-only entries don't belong to a profile sync, so the
		// profile fields stay empty — the History tab's Custom
		// Formats sub-tab filters by Categories, not by profile.
		LastSync:    now,
		AppliedAt:   now,
		TriggerType: core.TriggerCFDriftApply,
		Categories:  []string{"cf"},
		CFsUpdated:  1,
		Changes: &core.SyncChanges{
			CFDetails: []string{fmt.Sprintf("Re-pushed: %s (drift apply)", diskCFName)},
		},
	}
	if err := s.Core.Config.UpsertSyncHistory(entry); err != nil {
		log.Printf("cf-drift apply: persist history failed: %v", err)
	}

	// Fire the per-CF reconciled notification using the same path the
	// drift pass uses. Single event so the aggregator just sends "1
	// custom format back in sync on <instance>".
	s.Core.NotifyCFDriftReconciled([]*core.CFDriftEvent{{
		Event:        core.CFDriftReconciled,
		InstanceID:   inst.ID,
		InstanceName: inst.Name,
		AppType:      inst.Type,
		TrashID:      body.TrashID,
		CFName:       diskCFName,
	}})

	writeJSON(w, map[string]any{
		"appliedAt":  now,
		"trashId":    body.TrashID,
		"instanceId": inst.ID,
		"name":       diskCFName,
	})
}
