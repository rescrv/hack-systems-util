package bloom_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"hack.systems/util/bloom"
)

func TestBloom(t *testing.T) {
	require := require.New(t)

	type BloomParams struct {
		N float64
		P float64
		M float64
		K float64
	}
	params := []BloomParams{
		{100, 0.01, 958, 6.64385618977},
		{1000, 0.001, 14377, 9.96578428466},
		{2718281, 0.0314159, 19578296, 4.9923612789},
	}
	for _, param := range params {
		k := bloom.KeysForProbability(param.P)
		require.InEpsilon(param.K, k, 0.01)
		m := bloom.BloomParamsM(param.N, param.P)
		require.InEpsilon(param.M, m, 0.01)
		p := bloom.BloomParamsP(param.N, param.M)
		require.InEpsilon(param.P, p, 0.01)
	}
}
