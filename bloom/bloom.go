package bloom

import (
	"math"
)

const (
	ln2_2 float64 = 0.4804530139182014 // ln^2 2
)

func KeysForProbability(P float64) float64 {
	return 0 - math.Log2(P)
}

func BloomParamsM(N float64, P float64) float64 {
	return 0 - (N*math.Log(P))/ln2_2
}

func BloomParamsP(N float64, M float64) float64 {
	return math.Pow(math.E, 0-ln2_2*M/N)
}
