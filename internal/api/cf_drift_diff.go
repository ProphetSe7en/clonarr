package api

import (
	"net/http"

	"clonarr/internal/arr"
	"clonarr/internal/core"
)

// handleCFDriftDiff returns the per-CF drift diff for a single
// (instance, trashId) pair. Read-only — recomputes the diff on
// demand by resolving the disk spec, fetching the live CF from
// Arr, and running the same DiffCFSpecs that the detection pass
// uses. Used by the Sync Rules → Custom Formats sub-tab to render
// "what specifically drifted" inside the expanded row so the user
// can decide whether the diff is intended (manual edit they want
// to keep) or accidental (other tool / manual mistake to re-push).
//
// Path: GET /api/cf-drift/diff?instanceId=X&trashId=Y
//
// The handler intentionally does not consult the stored
// CFDriftFingerprints map — recomputing against current Arr state
// is the only way to surface a diff that's accurate at view time
// (a CF the user just fixed manually in Arr should show no diff
// even though the stale fingerprint still says drifted; next
// Check tick will clear it).
func (s *Server) handleCFDriftDiff(w http.ResponseWriter, r *http.Request) {
	instanceID := r.URL.Query().Get("instanceId")
	trashID := r.URL.Query().Get("trashId")
	if instanceID == "" || trashID == "" {
		writeError(w, http.StatusBadRequest, "instanceId and trashId are required")
		return
	}

	cfg := s.Core.Config.Get()
	var inst *core.Instance
	for i := range cfg.Instances {
		if cfg.Instances[i].ID == instanceID {
			inst = &cfg.Instances[i]
			break
		}
	}
	if inst == nil {
		writeError(w, http.StatusNotFound, "instance not found")
		return
	}

	appData := s.Core.Trash.GetAppData(inst.Type)
	customs := s.Core.CustomCFs.List(inst.Type)
	customsByID := make(map[string]core.CustomCF, len(customs))
	for _, c := range customs {
		customsByID[c.ID] = c
	}

	// Disk spec — TRaSH first, custom fallback (same precedence as
	// runCFSpecDriftPass / handleCFDriftApply so the three callers
	// never disagree on which side counts as "saved state").
	var diskSpec *core.TrashCF
	var diskName string
	if appData != nil {
		if cf, ok := appData.CustomFormats[trashID]; ok && cf != nil {
			diskSpec = cf
			diskName = cf.Name
		}
	}
	if diskSpec == nil {
		if c, ok := customsByID[trashID]; ok {
			diskName = c.Name
			// Match the conversion the detection pass uses.
			diskSpec = customCFToTrashSpec(c)
		}
	}
	if diskSpec == nil {
		writeError(w, http.StatusNotFound, "trash id not in TRaSH data or custom CFs")
		return
	}

	client := arr.NewArrClient(inst.URL, inst.APIKey, s.Core.HTTPClient)
	liveCFs, err := client.ListCustomFormats()
	if err != nil {
		writeError(w, http.StatusBadGateway, "list arr custom formats: "+err.Error())
		return
	}
	var live *arr.ArrCF
	for i := range liveCFs {
		if liveCFs[i].Name == diskName {
			live = &liveCFs[i]
			break
		}
	}
	if live == nil {
		// CF isn't yet in Arr — no diff to show. Returning 200 with an
		// empty diff lets the UI render "Not yet pushed" rather than
		// erroring out.
		writeJSON(w, map[string]any{
			"trashId":     trashID,
			"instanceId":  inst.ID,
			"name":        diskName,
			"liveMissing": true,
			"diff":        nil,
		})
		return
	}

	// Strip TrashScores on the disk-side before diffing — Arr's API
	// doesn't return per-CF scores, so leaving them in produces N
	// score "removed" entries per CF and turns every CF into drift.
	// Same fix the detection pass applies.
	diskForDiff := *diskSpec
	diskForDiff.TrashScores = nil
	liveAsTrashCF := core.ArrCFToTrashCFExported(live)
	diff := core.DiffCFSpecs(&diskForDiff, liveAsTrashCF)

	writeJSON(w, map[string]any{
		"trashId":    trashID,
		"instanceId": inst.ID,
		"name":       diskName,
		"diff":       diff,
	})
}

// customCFToTrashSpec mirrors the unexported customCFToTrashCF used
// in cf_drift.go. Kept in the api package to avoid exporting from
// core just for this handler — the two definitions are byte-for-byte
// equivalent and any divergence would surface immediately as a
// detect/apply/show-diff disagreement.
func customCFToTrashSpec(c core.CustomCF) *core.TrashCF {
	specs := make([]core.CFSpecification, 0, len(c.Specifications))
	for _, s := range c.Specifications {
		specs = append(specs, core.CFSpecification{
			Name:           s.Name,
			Implementation: s.Implementation,
			Negate:         s.Negate,
			Required:       s.Required,
			Fields:         s.Fields,
		})
	}
	return &core.TrashCF{
		Name:            c.Name,
		IncludeInRename: c.IncludeInRename,
		Specifications:  specs,
	}
}
