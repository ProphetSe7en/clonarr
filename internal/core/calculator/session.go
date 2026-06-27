package calculator

// session.go defines the persisted calculator session (the iterative unit of
// work) and converts it into the solver Input. Titles are parsed (quality +
// matched CFs) when added in the UI, so Test/Calculate need no live Arr calls.

// Session is the full saved state of a calculator workflow.
type Session struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AppType   string `json:"appType"` // "radarr" | "sonarr"
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`

	BoundProfile string `json:"boundProfile"` // "trash:<id>" | "imported:<id>" | "inst:<inst>:<prof>" | ""

	QualityOrdering     []QualityTier  `json:"qualityOrdering"`
	QualityRanksCache   map[string]int `json:"qualityRanksCache"`
	QualityAllowedCache []string       `json:"qualityAllowedCache"`

	Titles      []SessionTitle      `json:"titles"`
	CFOverrides map[string]Override `json:"cfOverrides"`
	IgnoredCFs  []string            `json:"ignoredCFs,omitempty"` // CF ids dropped from the calculation entirely

	Settings   SessionSettings `json:"settings"`
	LastResult *SessionResult  `json:"lastResult,omitempty"`
}

// QualityTier is one row of the quality ordering; members share a rank.
type QualityTier struct {
	Tier    int      `json:"tier"`
	Name    string   `json:"name"`
	Members []string `json:"members"`
}

// SessionTitle is a ranked release with its parsed data.
type SessionTitle struct {
	ID            string      `json:"id"`
	Title         string      `json:"title"`
	PriorityGroup int         `json:"priorityGroup"`
	Partition     string      `json:"partition"` // "wanted" | "unwanted"
	ParsedQuality string      `json:"parsedQuality"`
	MatchedCFs    []SessionCF `json:"matchedCFs"`
}

// SessionCF is a matched Custom Format reference.
type SessionCF struct {
	TrashID string `json:"trashId"`
	Name    string `json:"name"`
}

// SessionSettings holds per-session solver options.
type SessionSettings struct {
	SnapStep   int      `json:"snapStep"`
	ScoreBound float64  `json:"scoreBound"`
	MinScore   *float64 `json:"minScore,omitempty"` // pin minFormatScore (nil = solve)
}

// SessionResult is the last computed output, cached for display.
type SessionResult struct {
	ComputedAt     string             `json:"computedAt"`
	Feasible       bool               `json:"feasible"`
	CFScores       map[string]float64 `json:"cfScores"`
	MinFormatScore float64            `json:"minFormatScore"`
	Warnings       []string           `json:"warnings,omitempty"`
}

// GeneratorDoc is the persisted Scoring Generator state for one app type: a
// shared pool of added release titles plus the named sets (orderings/selections
// of the pool). Mirrors the Scoring Sandbox's results + score sets.
type GeneratorDoc struct {
	AppType   string         `json:"appType"`
	Pool      []PoolTitle    `json:"pool"`
	Sets      []GeneratorSet `json:"sets"`
	UpdatedAt string         `json:"updatedAt,omitempty"`
}

// PoolTitle is one parsed release in the shared pool.
type PoolTitle struct {
	ID            string      `json:"id"`
	Title         string      `json:"title"`
	ParsedQuality string      `json:"parsedQuality"`
	MatchedCFs    []SessionCF `json:"matchedCFs"`
}

// GeneratorSet is a named ranking: an ordered selection of pool titles plus the
// per-set wanted/unwanted partition and per-CF state. Titles are referenced by
// pool id, so the same release can appear in several sets without duplication.
type GeneratorSet struct {
	ID          string              `json:"id"`
	Name        string              `json:"name"`
	Order       []string            `json:"order"`              // pool title ids, ranked (top = highest priority)
	Tied        []string            `json:"tied,omitempty"`     // pool title ids that share the priority of the title above them
	Unwanted    []string            `json:"unwanted,omitempty"` // pool title ids marked unwanted in this set
	CFOverrides map[string]Override `json:"cfOverrides,omitempty"`
	IgnoredCFs  []string            `json:"ignoredCFs,omitempty"`
	MinScore    *float64            `json:"minScore,omitempty"` // pinned minFormatScore (nil = auto)
}

// ToInput reduces the session to the solver Input, resolving each title's
// quality rank and allowed flag from the cached quality data.
func (s *Session) ToInput() Input {
	allowed := map[string]bool{}
	for _, q := range s.QualityAllowedCache {
		allowed[q] = true
	}
	ignored := make(map[string]bool, len(s.IgnoredCFs))
	for _, c := range s.IgnoredCFs {
		ignored[c] = true
	}
	titles := make([]Title, 0, len(s.Titles))
	for _, t := range s.Titles {
		rank, known := s.QualityRanksCache[t.ParsedQuality]
		if !known {
			rank = -1 // unknown quality, flagged by the validator
		}
		cfs := make([]string, 0, len(t.MatchedCFs))
		for _, c := range t.MatchedCFs {
			if ignored[c.TrashID] {
				continue // ignored CFs take no part in the calculation
			}
			cfs = append(cfs, c.TrashID)
		}
		part := Wanted
		if t.Partition == "unwanted" {
			part = Unwanted
		}
		titles = append(titles, Title{
			ID:             t.ID,
			QualityRank:    rank,
			QualityAllowed: allowed[t.ParsedQuality],
			MatchedCFs:     cfs,
			PriorityGroup:  t.PriorityGroup,
			Partition:      part,
		})
	}
	bound := s.Settings.ScoreBound
	return Input{Titles: titles, Overrides: s.CFOverrides, ScoreBound: bound, MinScore: s.Settings.MinScore}
}
