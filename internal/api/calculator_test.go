package api

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"clonarr/internal/core"
	"clonarr/internal/core/calculator"
)

func calcTestServer(t *testing.T) *Server {
	t.Helper()
	app := &core.App{CalcSessions: calculator.NewStore(t.TempDir())}
	return &Server{Core: app}
}

// Stateless calc: post a ranking, get scores that reproduce it.
func TestCalculator_Calc(t *testing.T) {
	srv := calcTestServer(t)
	body := `{
	  "appType": "radarr",
	  "qualityRanksCache": {"Bluray-1080p": 1},
	  "qualityAllowedCache": ["Bluray-1080p"],
	  "settings": {"snapStep": 1, "scoreBound": 10000},
	  "titles": [
	    {"id":"t1","priorityGroup":1,"partition":"wanted","parsedQuality":"Bluray-1080p","matchedCFs":[{"trashId":"remux"},{"trashId":"atmos"}]},
	    {"id":"t2","priorityGroup":2,"partition":"wanted","parsedQuality":"Bluray-1080p","matchedCFs":[{"trashId":"remux"}]},
	    {"id":"t3","priorityGroup":3,"partition":"unwanted","parsedQuality":"Bluray-1080p","matchedCFs":[{"trashId":"cam"}]}
	  ]
	}`
	req := httptest.NewRequest("POST", "/api/calculator/calc", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleCalcCalculate(w, req)
	if w.Code != 200 {
		t.Fatalf("calc: status %d, body %s", w.Code, w.Body.String())
	}
	var res struct {
		Feasible     bool `json:"feasible"`
		Verification []struct {
			TitleID string  `json:"titleId"`
			Score   float64 `json:"score"`
			Passes  bool    `json:"passes"`
		} `json:"verification"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !res.Feasible {
		t.Fatalf("expected feasible, body %s", w.Body.String())
	}
	score := map[string]float64{}
	for _, v := range res.Verification {
		score[v.TitleID] = v.Score
		if (v.TitleID == "t1" || v.TitleID == "t2") && !v.Passes {
			t.Errorf("%s should pass minFormatScore", v.TitleID)
		}
		if v.TitleID == "t3" && v.Passes {
			t.Errorf("t3 (unwanted) should not pass")
		}
	}
	if !(score["t1"] > score["t2"]) {
		t.Errorf("t1 (%.1f) should outscore t2 (%.1f)", score["t1"], score["t2"])
	}
}

func TestCalculator_CalcInfeasible(t *testing.T) {
	srv := calcTestServer(t)
	body := `{
	  "appType":"radarr",
	  "qualityRanksCache":{"Bluray-1080p":1},
	  "qualityAllowedCache":["Bluray-1080p"],
	  "settings":{"snapStep":1},
	  "titles":[
	    {"id":"a","priorityGroup":1,"partition":"wanted","parsedQuality":"Bluray-1080p","matchedCFs":[{"trashId":"x"}]},
	    {"id":"b","priorityGroup":2,"partition":"wanted","parsedQuality":"Bluray-1080p","matchedCFs":[{"trashId":"x"}]}
	  ]
	}`
	req := httptest.NewRequest("POST", "/api/calculator/calc", strings.NewReader(body))
	w := httptest.NewRecorder()
	srv.handleCalcCalculate(w, req)
	var res struct {
		Feasible bool `json:"feasible"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res.Feasible {
		t.Fatalf("expected not feasible, body %s", w.Body.String())
	}
}

// Document persistence: save a pool + sets, read it back via the handlers.
func TestCalculator_DocRoundtrip(t *testing.T) {
	srv := calcTestServer(t)
	body := `{"pool":[{"id":"p1","title":"A","parsedQuality":"Bluray-1080p","matchedCFs":[{"trashId":"x","name":"X"}]}],"sets":[{"id":"s1","name":"1080p","order":["p1"]}]}`
	req := httptest.NewRequest("PUT", "/api/calculator/radarr/doc", strings.NewReader(body))
	req.SetPathValue("app", "radarr")
	w := httptest.NewRecorder()
	srv.handleCalcDocSave(w, req)
	if w.Code != 200 {
		t.Fatalf("save doc: status %d, body %s", w.Code, w.Body.String())
	}

	req = httptest.NewRequest("GET", "/api/calculator/radarr/doc", nil)
	req.SetPathValue("app", "radarr")
	w = httptest.NewRecorder()
	srv.handleCalcDocGet(w, req)
	var doc calculator.GeneratorDoc
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode doc: %v", err)
	}
	if len(doc.Pool) != 1 || len(doc.Sets) != 1 || doc.Sets[0].Name != "1080p" {
		t.Fatalf("doc roundtrip mismatch: %+v", doc)
	}
}
