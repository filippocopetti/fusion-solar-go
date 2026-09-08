package fusionsolar

import "testing"

func TestDecodeCTC(t *testing.T) {
	// Strong sequence: 1, blank, 2, 2. CTC should decode to [1,2].
	p := [][]float64{
		{0.01, 0.97, 0.01},
		{0.97, 0.01, 0.01},
		{0.01, 0.01, 0.97},
		{0.01, 0.01, 0.97},
	}
	got, _ := DecodeCTC(p, 10, 0)
	if len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("got %v, want [1 2]", got)
	}
}
