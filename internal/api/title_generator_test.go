package api

import (
	"encoding/json"
	"testing"

	"clonarr/internal/core"
)

func TestExtractLiteral(t *testing.T) {
	cases := []struct {
		rx   string
		want string
		ok   bool
	}{
		{`^(YIFY)$`, "YIFY", true},
		{`\b(GalaxyRG)\b`, "GalaxyRG", true},
		{`-4P\b`, "-4P", true},
		{`\[rarbg\]`, "[rarbg]", true},
		{`^(BAT)$`, "BAT", true},
		{`\b(-CAKES|-GGEZ|-GLHF)\b`, "-CAKES", true},
		// First alt is a lookahead with no literal; second yields one.
		{`^(?=.*(\b\d{3,4}p\b).*([_. ]WEB[_. ])(?!DL)\b)|\b(-EDITH|-ETHEL)`, "-EDITH", true},
		// Regex debris (`.*`) is rejected, so the real CF falls through to its
		// next, clean spec (`^(BAT)$`).
		{`^(alfaHD.*)$`, "", false},
		{`.`, "", false},
		{`10[.-]?bit`, "", false}, // metachars -> rejected (curated elsewhere)
		// Nested / structured patterns must NOT be mined for a fragment like "and"
		// (this is the Black and White Editions shape).
		{`(?<=\b[12]\d{3}\b).*\b((B(lack)?[ ._-]?(out|(and|[n&])?[ ._-]?W))\b)`, "", false},
	}
	for _, c := range cases {
		got, ok := extractLiteral(c.rx)
		if ok != c.ok {
			t.Errorf("extractLiteral(%q) ok=%v, want %v (got %q)", c.rx, ok, c.ok, got)
			continue
		}
		if ok && got != c.want {
			t.Errorf("extractLiteral(%q) = %q, want %q", c.rx, got, c.want)
		}
	}
}

func TestParseQualityName(t *testing.T) {
	cases := []struct {
		name    string
		src     string
		res     string
		ok      bool
	}{
		{"Bluray-1080p", "Bluray", "1080p", true},
		{"Bluray-2160p", "Bluray", "2160p", true},
		{"Remux-2160p", "Remux", "2160p", true},
		{"WEB 1080p", "WEB-DL", "1080p", true},
		{"WEBDL-2160p", "WEB-DL", "2160p", true},
		{"WEBRip-1080p", "WEBRip", "1080p", true},
		{"HDTV-720p", "HDTV", "720p", true},
		{"DVD", "", "", false},   // no resolution
		{"SDTV", "", "", false},  // not generated
		{"Raw-HD", "", "", false}, // no resolution token
	}
	for _, c := range cases {
		q, ok := parseQualityName(c.name)
		if ok != c.ok {
			t.Errorf("parseQualityName(%q) ok=%v, want %v", c.name, ok, c.ok)
			continue
		}
		if ok && (q.Source != c.src || q.Resolution != c.res) {
			t.Errorf("parseQualityName(%q) = {%s,%s}, want {%s,%s}", c.name, q.Source, q.Resolution, c.src, c.res)
		}
	}
}

func spec(impl string, negate bool, value string) core.CFSpecification {
	return core.CFSpecification{
		Implementation: impl,
		Negate:         negate,
		Fields:         json.RawMessage(`{"value":` + mustJSON(value) + `}`),
	}
}

func mustJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestUnwantedDecoration(t *testing.T) {
	// No-RlsGroup: negated catch-all -> group-less.
	if u, ok := unwantedDecoration(&core.TrashCF{Name: "No-RlsGroup", Specifications: []core.CFSpecification{
		spec("ReleaseGroupSpecification", true, "."),
	}}); !ok || !u.NoGroup {
		t.Errorf("No-RlsGroup should be NoGroup, got %+v ok=%v", u, ok)
	}

	// LQ: release-group name -> "-NAME" suffix.
	if u, ok := unwantedDecoration(&core.TrashCF{Name: "LQ", Specifications: []core.CFSpecification{
		spec("ReleaseGroupSpecification", false, `^(YIFY)$`),
	}}); !ok || u.Suffix != "-YIFY" {
		t.Errorf("LQ should be -YIFY, got %+v ok=%v", u, ok)
	}

	// Obfuscated: title suffix already starts with '-'.
	if u, ok := unwantedDecoration(&core.TrashCF{Name: "Obfuscated", Specifications: []core.CFSpecification{
		spec("ReleaseTitleSpecification", false, `-Rakuv\b`),
	}}); !ok || u.Suffix != "-Rakuv" {
		t.Errorf("Obfuscated should be -Rakuv, got %+v ok=%v", u, ok)
	}

	// Retags: bracket tag -> spaced suffix.
	if u, ok := unwantedDecoration(&core.TrashCF{Name: "Retags", Specifications: []core.CFSpecification{
		spec("ReleaseTitleSpecification", false, `\[rarbg\]`),
	}}); !ok || u.Suffix != " [rarbg]" {
		t.Errorf("Retags should be ' [rarbg]', got %+v ok=%v", u, ok)
	}

	// Mixed group + context-gated title specs (Generated Dynamic HDR shape):
	// title-based with no clean literal -> skipped, so we don't emit a false
	// bare group token like "-BiTOR".
	if _, ok := unwantedDecoration(&core.TrashCF{Name: "Generated Dynamic HDR", Specifications: []core.CFSpecification{
		spec("ReleaseGroupSpecification", false, `^(BiTOR)$`),
		spec("ReleaseTitleSpecification", false, `(?<=\b[12]\d{3}\b)(?=.*\b(HEVC)\b)(?=.*\b(DV|HDR)\b)`),
	}}); ok {
		t.Error("mixed group + context CF should be skipped, not emit a bare group token")
	}

	// Curated tech tokens.
	if u, _ := unwantedDecoration(&core.TrashCF{Name: "AV1"}); u.Codec != "AV1" {
		t.Errorf("AV1 should set Codec, got %+v", u)
	}
	if u, _ := unwantedDecoration(&core.TrashCF{Name: "BR-DISK"}); u.Body == "" {
		t.Errorf("BR-DISK should set Body, got %+v", u)
	}
}
