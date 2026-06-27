package calculator

import "testing"

func TestStore_DocRoundtrip(t *testing.T) {
	st := NewStore(t.TempDir())

	// Missing file -> empty doc, not an error.
	doc, err := st.LoadDoc("radarr")
	if err != nil {
		t.Fatalf("load empty: %v", err)
	}
	if len(doc.Pool) != 0 || len(doc.Sets) != 0 {
		t.Fatalf("expected empty doc, got %+v", doc)
	}

	doc.Pool = []PoolTitle{{ID: "p1", Title: "A", ParsedQuality: "Bluray-1080p", MatchedCFs: []SessionCF{{TrashID: "x", Name: "X"}}}}
	doc.Sets = []GeneratorSet{{ID: "s1", Name: "My 1080p", Order: []string{"p1"}, IgnoredCFs: []string{"y"}}}
	if err := st.SaveDoc("radarr", doc); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := st.LoadDoc("radarr")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(got.Pool) != 1 || got.Pool[0].MatchedCFs[0].TrashID != "x" {
		t.Fatalf("pool roundtrip mismatch: %+v", got.Pool)
	}
	if len(got.Sets) != 1 || got.Sets[0].Name != "My 1080p" || got.Sets[0].Order[0] != "p1" {
		t.Fatalf("set roundtrip mismatch: %+v", got.Sets)
	}
	// Sonarr is a separate document.
	if s, _ := st.LoadDoc("sonarr"); len(s.Pool) != 0 {
		t.Errorf("sonarr doc should be independent/empty")
	}
}

func TestStore_RejectsBadApp(t *testing.T) {
	st := NewStore(t.TempDir())
	for _, bad := range []string{"../etc", "radarr/x", "", "lidarr"} {
		if _, err := st.LoadDoc(bad); err == nil {
			t.Errorf("LoadDoc(%q) should error", bad)
		}
		if err := st.SaveDoc(bad, &GeneratorDoc{}); err == nil {
			t.Errorf("SaveDoc(%q) should error", bad)
		}
	}
}

func TestSession_ToInput(t *testing.T) {
	sess := &Session{
		QualityRanksCache:   map[string]int{"Remux-2160p": 1, "WEBDL-1080p": 3},
		QualityAllowedCache: []string{"Remux-2160p"},
		Titles: []SessionTitle{
			{ID: "t1", PriorityGroup: 1, Partition: "wanted", ParsedQuality: "Remux-2160p",
				MatchedCFs: []SessionCF{{TrashID: "atmos"}}},
			{ID: "t2", PriorityGroup: 2, Partition: "unwanted", ParsedQuality: "WEBDL-1080p",
				MatchedCFs: []SessionCF{{TrashID: "lq"}}},
			{ID: "t3", PriorityGroup: 3, Partition: "wanted", ParsedQuality: "Unknown-9000"},
		},
		CFOverrides: map[string]Override{"lq": {Type: "unwanted", Max: -10000}},
		Settings:    SessionSettings{ScoreBound: 5000},
	}
	in := sess.ToInput()
	if len(in.Titles) != 3 || in.ScoreBound != 5000 {
		t.Fatalf("unexpected input: %+v", in)
	}
	if in.Titles[0].QualityRank != 1 || !in.Titles[0].QualityAllowed {
		t.Errorf("t1 should be rank 1 allowed: %+v", in.Titles[0])
	}
	if in.Titles[1].QualityAllowed {
		t.Errorf("t2 quality should not be allowed")
	}
	if in.Titles[1].Partition != Unwanted {
		t.Errorf("t2 should be unwanted")
	}
	if in.Titles[2].QualityRank != -1 {
		t.Errorf("t3 unknown quality should be rank -1, got %d", in.Titles[2].QualityRank)
	}
}

func TestSession_ToInputStripsIgnored(t *testing.T) {
	sess := &Session{
		QualityRanksCache:   map[string]int{"Bluray-1080p": 1},
		QualityAllowedCache: []string{"Bluray-1080p"},
		Titles: []SessionTitle{
			{ID: "t1", PriorityGroup: 1, Partition: "wanted", ParsedQuality: "Bluray-1080p",
				MatchedCFs: []SessionCF{{TrashID: "keep"}, {TrashID: "drop"}}},
		},
		IgnoredCFs: []string{"drop"},
	}
	in := sess.ToInput()
	if len(in.Titles[0].MatchedCFs) != 1 || in.Titles[0].MatchedCFs[0] != "keep" {
		t.Fatalf("ignored CF not stripped: %+v", in.Titles[0].MatchedCFs)
	}
}
