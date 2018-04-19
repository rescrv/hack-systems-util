package tiny_lfu_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"hack.systems/util/caching/tiny_lfu"
)

func TestTinyLFU32(t *testing.T) {
	require := require.New(t)

	c := tiny_lfu.New32(1e7, 1e8)
	require.NotNil(c)
	for i := 0; i < 10; i++ {
		c.Tally("hello")
	}
	c.Tally("goodbye")
	require.True(c.ShouldReplace("goodbye", "hello"))
	require.False(c.ShouldReplace("hello", "goodbye"))
}

func TestTinyLFU64(t *testing.T) {
	require := require.New(t)

	c := tiny_lfu.New64(1e7, 1e8)
	require.NotNil(c)
	for i := 0; i < 10; i++ {
		c.Tally("hello")
	}
	c.Tally("goodbye")
	require.True(c.ShouldReplace("goodbye", "hello"))
	require.False(c.ShouldReplace("hello", "goodbye"))
}
