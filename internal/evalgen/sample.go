package evalgen

import (
	"math/rand"
)

// rngFor returns a deterministic *rand.Rand seeded from seed. All sampling in
// this package must go through a Rand created here so that a given seed yields
// a byte-identical item set across runs and machines.
func rngFor(seed int64) *rand.Rand {
	return rand.New(rand.NewSource(seed))
}

// sampleIndices returns k distinct indices in [0, n) drawn without replacement.
// The order is the permutation order produced by rng; callers that need a
// stable output order must sort the resulting items themselves.
func sampleIndices(rng *rand.Rand, n, k int) []int {
	if k <= 0 || n <= 0 {
		return nil
	}
	if k >= n {
		out := make([]int, n)
		for i := range out {
			out[i] = i
		}
		return out
	}
	return rng.Perm(n)[:k]
}

// shuffleStrings deterministically permutes s in place using rng.
func shuffleStrings(rng *rand.Rand, s []string) {
	rng.Shuffle(len(s), func(i, j int) { s[i], s[j] = s[j], s[i] })
}
