package core

import (
	"clonarr/internal/arr"
	"testing"
)



func TestFingerprintArrItems_ReverseEnabled(t *testing.T) {
	items := []arr.ArrQualityItem{
		arrQ(1, "A", true),
		arrQ(2, "B", true),
		arrQ(3, "C", false),
		arrQ(4, "D", false),
	}
	
	fpNormal := FingerprintArrItems(items, false)
	// Expected: enabled parts stay A then B. Disabled parts sorted (C, D)
	// A|B|C|D
	wantNormal := `Q:"A"=true|Q:"B"=true|Q:"C"=false|Q:"D"=false`
	
	if fpNormal != wantNormal {
		t.Errorf("normal fingerprint mismatch:\n  got:  %s\n  want: %s", fpNormal, wantNormal)
	}
	
	fpReverse := FingerprintArrItems(items, true)
	// Expected: enabled parts are reversed to B then A. Disabled parts sorted (C, D)
	// B|A|C|D
	wantReverse := `Q:"B"=true|Q:"A"=true|Q:"C"=false|Q:"D"=false`
	
	if fpReverse != wantReverse {
		t.Errorf("reverse fingerprint mismatch:\n  got:  %s\n  want: %s", fpReverse, wantReverse)
	}
}
