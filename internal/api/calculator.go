package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"clonarr/internal/core"
	"clonarr/internal/core/calculator"
)

// calculator.go exposes the Scoring Generator (back-solve Custom Format scores
// from a ranked release list). Gated in the UI by Advanced Mode, same as the
// Scoring Sandbox. Persistence is one document per app type (the shared title
// pool + named sets); calculation is stateless - the client posts the ranking
// payload and gets scores back, no server-side session to load.

func calcNow() string { return time.Now().UTC().Format(time.RFC3339) }

// resolutionMessage explains, in plain language, what has to change for the
// ranking to work: ties for releases that match identical Custom Formats, and
// moves for releases that are out of position for their Custom Formats.
func resolutionMessage(groups [][]string, moves []calculator.Move) string {
	switch {
	case len(groups) > 0 && len(moves) > 0:
		return "To rank these the way you asked, some releases need to share a priority (they match identical Custom Formats) and some need to move to where their Custom Formats actually place them."
	case len(moves) > 0:
		if len(moves) == 1 {
			return "One release is out of position for its Custom Formats. Move it to where it actually ranks to continue."
		}
		return fmt.Sprintf("%d releases are out of position for their Custom Formats. Move them to where they actually rank to continue.", len(moves))
	case len(groups) == 1:
		return "Some of these releases match the exact same Custom Formats in the same quality tier, so they can't be ranked apart. Group them at the same priority to continue."
	default:
		return fmt.Sprintf("%d sets of releases match the exact same Custom Formats in the same quality tier, so they can't be ranked apart. Group each set at the same priority to continue.", len(groups))
	}
}

// handleCalcGrabTitles pulls recent grabbed-release titles from an instance's
// history, so the user can seed the pool with real releases from their library.
func (s *Server) handleCalcGrabTitles(w http.ResponseWriter, r *http.Request) {
	instanceID := r.URL.Query().Get("instanceId")
	inst, ok := s.Core.Config.GetInstance(instanceID)
	if !ok {
		writeError(w, 404, "instance not found")
		return
	}
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 500 {
		limit = 500
	}
	client := core.NewArrClientFor(inst, s.Core.HTTPClient)
	path := fmt.Sprintf("/history?page=1&pageSize=%d&sortKey=date&sortDirection=descending&eventType=1", limit)
	data, status, err := client.DoRequest("GET", path, nil)
	if err != nil {
		writeError(w, 502, "could not reach the instance")
		return
	}
	if status != 200 {
		writeError(w, 502, fmt.Sprintf("instance returned HTTP %d", status))
		return
	}
	var resp struct {
		Records []struct {
			SourceTitle string `json:"sourceTitle"`
		} `json:"records"`
	}
	if err := json.Unmarshal(data, &resp); err != nil {
		writeError(w, 502, "could not read history response")
		return
	}
	seen := map[string]bool{}
	titles := make([]string, 0, len(resp.Records))
	for _, rec := range resp.Records {
		t := rec.SourceTitle
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		titles = append(titles, t)
	}
	writeJSON(w, map[string]any{"titles": titles})
}

// handleCalcDocGet returns the saved Scoring Generator document (pool + sets)
// for an app type.
func (s *Server) handleCalcDocGet(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	doc, err := s.Core.CalcSessions.LoadDoc(app)
	if err != nil {
		writeError(w, 400, "could not load generator data")
		return
	}
	writeJSON(w, doc)
}

// handleCalcDocSave persists the document (pool + sets) for an app type.
func (s *Server) handleCalcDocSave(w http.ResponseWriter, r *http.Request) {
	app := r.PathValue("app")
	var doc calculator.GeneratorDoc
	if err := json.NewDecoder(r.Body).Decode(&doc); err != nil {
		writeError(w, 400, "invalid document body")
		return
	}
	doc.UpdatedAt = calcNow()
	if err := s.Core.CalcSessions.SaveDoc(app, &doc); err != nil {
		writeError(w, 400, "could not save generator data")
		return
	}
	writeJSON(w, doc)
}

type calcVerification struct {
	TitleID string  `json:"titleId"`
	Score   float64 `json:"score"`
	Passes  bool    `json:"passes"`  // would the profile accept it (score >= minFormatScore)
	Correct bool    `json:"correct"` // does that match the title's wanted/unwanted intent
}

// handleCalcTest runs the pre-calculation validation checks on a posted ranking.
func (s *Server) handleCalcTest(w http.ResponseWriter, r *http.Request) {
	var req calculator.Session
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	issues := calculator.Validate(req.ToInput())
	writeJSON(w, map[string]any{"issues": issues, "hasErrors": calculator.HasErrors(issues)})
}

// handleCalcCalculate runs the LP on a posted ranking and returns scores +
// minFormatScore + per-title verification, or an infeasibility explanation.
// Stateless: the request body carries the whole ranking.
func (s *Server) handleCalcCalculate(w http.ResponseWriter, r *http.Request) {
	var req calculator.Session
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	if req.Settings.SnapStep == 0 {
		req.Settings.SnapStep = 5
	}
	if req.Settings.ScoreBound == 0 {
		req.Settings.ScoreBound = 10000
	}

	in := req.ToInput()
	issues := calculator.Validate(in)

	// Errors other than identical-CF clusters need explicit user action (an
	// unknown quality, a cross-quality ordering, a Wanted/Unwanted overlap);
	// grouping cannot fix those, so block immediately. Identical-CF clusters are
	// left to the auto-grouping pass below, which resolves them all at once.
	var blocking []calculator.Issue
	for _, is := range issues {
		if is.Severity == "error" && is.Kind != "identical-cfs" {
			blocking = append(blocking, is)
		}
	}
	if len(blocking) > 0 {
		writeJSON(w, map[string]any{"feasible": false, "blocked": true, "issues": blocking})
		return
	}

	res := calculator.Calculate(in)
	if !res.Feasible {
		// Work out, up front, exactly what has to change: which releases must be
		// tied (identical Custom Formats) and which are simply in the wrong place
		// and need to move. The user applies it all in one click.
		r := calculator.Resolve(in)
		if r.Conflict != nil {
			writeJSON(w, map[string]any{
				"feasible": false,
				"conflict": *r.Conflict,
				"issues":   issues,
			})
			return
		}
		if len(r.MergeGroups) > 0 || len(r.Moves) > 0 {
			writeJSON(w, map[string]any{
				"feasible":      false,
				"needsGrouping": true,
				"groups":        r.MergeGroups,
				"moves":         r.Moves,
				"message":       resolutionMessage(r.MergeGroups, r.Moves),
			})
			return
		}
		writeJSON(w, map[string]any{
			"feasible": false,
			"conflict": calculator.ExplainInfeasibility(in),
			"issues":   issues,
		})
		return
	}

	step := req.Settings.SnapStep
	final := res
	if snapped, ok := calculator.Snap(in, res, step); ok {
		final = snapped
	}

	var warnings []string
	for _, i := range issues {
		if i.Severity == "warning" {
			warnings = append(warnings, i.Message)
		}
	}

	verification := make([]calcVerification, 0, len(in.Titles))
	for _, t := range in.Titles {
		if !t.QualityAllowed {
			continue
		}
		score := calculator.TitleScore(t, final.CFScores)
		passes := score >= final.MinFormatScore-0.5
		correct := passes == (t.Partition == calculator.Wanted)
		verification = append(verification, calcVerification{TitleID: t.ID, Score: score, Passes: passes, Correct: correct})
	}

	writeJSON(w, map[string]any{
		"feasible":       true,
		"cfScores":       final.CFScores,
		"minFormatScore": final.MinFormatScore,
		"snapped":        final.CFScores != nil,
		"warnings":       warnings,
		"verification":   verification,
		"issues":         issues,
	})
}
