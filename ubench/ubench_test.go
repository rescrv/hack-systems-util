package ubench_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"hack.systems/util/ubench"
)

type Parameters struct {
	P1 string
	P2 uint64
}

type Results struct {
	R1 float64
	R2 uint64
}

func TestCommentString(t *testing.T) {
	var params Parameters
	var results Results
	require.Equal(t, "#P1 P2 R1 R2", ubench.CommentString(params, results))
}

func TestResultString(t *testing.T) {
	params := Parameters{
		P1: "algo",
		P2: 5,
	}
	results := Results{
		R1: 3.14,
		R2: 42,
	}
	require.Equal(t, "algo 5 3.14 42", ubench.ResultString(params, results))
}

func TestFieldNamesToFlags(t *testing.T) {
	require := require.New(t)

	require.Equal("operations", ubench.FieldNameToFlag("Operations"))
	require.Equal("read-ratio", ubench.FieldNameToFlag("ReadRatio"))
	require.Equal("zipf-theta", ubench.FieldNameToFlag("ZipfTheta"))
	require.Equal("http-requests", ubench.FieldNameToFlag("HTTPRequests"))
}
