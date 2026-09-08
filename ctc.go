package fusionsolar

import "math"

type beamState struct {
	prefix  []int
	pb, pnb float64
}

func logSumExp(xs ...float64) float64 {
	m := math.Inf(-1)
	for _, x := range xs {
		if x > m {
			m = x
		}
	}
	if math.IsInf(m, -1) {
		return m
	}
	s := 0.0
	for _, x := range xs {
		s += math.Exp(x - m)
	}
	return m + math.Log(s)
}
func cloneAppend(a []int, x int) []int {
	b := make([]int, len(a)+1)
	copy(b, a)
	b[len(a)] = x
	return b
}
func keyPrefix(a []int) string {
	var b []byte
	for _, x := range a {
		b = append(b, byte(x+1))
	}
	return string(b)
}
func DecodeCTC(probs [][]float64, beamSize, blank int) ([]int, float64) {
	if beamSize <= 0 {
		beamSize = 100
	}
	if len(probs) == 0 {
		return nil, 0
	}
	beam := map[string]beamState{"": {prefix: nil, pb: 0, pnb: math.Inf(-1)}}
	for _, row := range probs {
		next := map[string]beamState{}
		for s, p0 := range row {
			p := math.Log(p0)
			for _, st := range beam {
				k := keyPrefix(st.prefix)
				n := next[k]
				if n.prefix == nil && k != "" {
					n.prefix = append([]int(nil), st.prefix...)
				}
				if s == blank {
					n.pb = logSumExp(n.pb, st.pb+p, st.pnb+p)
					next[k] = n
					continue
				}
				end := -1
				if len(st.prefix) > 0 {
					end = st.prefix[len(st.prefix)-1]
				}
				np := cloneAppend(st.prefix, s)
				nk := keyPrefix(np)
				nn := next[nk]
				if nn.prefix == nil {
					nn.prefix = np
				}
				if s != end {
					nn.pnb = logSumExp(nn.pnb, st.pb+p, st.pnb+p)
				} else {
					nn.pnb = logSumExp(nn.pnb, st.pb+p)
				}
				next[nk] = nn
				if s == end {
					n = next[k]
					n.pnb = logSumExp(n.pnb, st.pnb+p)
					next[k] = n
				}
			}
		}
		arr := make([]beamState, 0, len(next))
		for _, v := range next {
			arr = append(arr, v)
		}
		for i := 0; i < len(arr); i++ {
			for j := i + 1; j < len(arr); j++ {
				if logSumExp(arr[j].pb, arr[j].pnb) > logSumExp(arr[i].pb, arr[i].pnb) {
					arr[i], arr[j] = arr[j], arr[i]
				}
			}
		}
		if len(arr) > beamSize {
			arr = arr[:beamSize]
		}
		beam = map[string]beamState{}
		for _, v := range arr {
			beam[keyPrefix(v.prefix)] = v
		}
	}
	var best beamState
	first := true
	for _, v := range beam {
		if first || logSumExp(v.pb, v.pnb) > logSumExp(best.pb, best.pnb) {
			best = v
			first = false
		}
	}
	return best.prefix, -logSumExp(best.pb, best.pnb)
}
