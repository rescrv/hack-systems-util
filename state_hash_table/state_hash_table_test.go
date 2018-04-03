package state_hash_table_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"hack.systems/util/state_hash_table"
)

type TestState struct {
	hold bool
	idx  int
}

func (ts *TestState) Finished() bool {
	return !ts.hold
}

type TestParams struct {
}

func (tp TestParams) Hash(key interface{}) uint64 {
	return 42
}

func (tp TestParams) NewState(key interface{}) state_hash_table.State {
	return new(TestState)
}

func TestStateHashTable(t *testing.T) {
	require := require.New(t)
	const KEY = 1

	params := TestParams{}

	A := params.NewState(KEY).(*TestState)
	B := params.NewState(KEY).(*TestState)
	// use require.True because require derefs pointers
	require.True(A != B)

	table := state_hash_table.New(params)
	// not there
	state, release := table.GetState(KEY)
	require.Nil(state)
	require.NotNil(release)
	release()
	// make it there
	state, release = table.CreateState(KEY)
	require.NotNil(state)
	require.NotNil(release)
	state.(*TestState).hold = true // hold it in the table
	s1 := state.(*TestState)
	release()
	// trying to create it again fails because still in memory
	state, release = table.CreateState(KEY)
	require.Nil(state)
	require.NotNil(release)
	release()
	// getting it returns the right one
	state, release = table.GetState(KEY)
	require.NotNil(state)
	require.NotNil(release)
	require.True(s1 == state.(*TestState))
	release()
	// getting or creating it returns the right one
	state, release = table.GetOrCreateState(KEY)
	require.NotNil(state)
	require.NotNil(release)
	require.True(s1 == state.(*TestState))
	state.(*TestState).hold = false // stop holding it in the table
	release()
	// not there
	state, release = table.GetState(KEY)
	require.Nil(state)
	require.NotNil(release)
	release()
	// getting or creating it returns a new one
	state, release = table.GetOrCreateState(KEY)
	require.NotNil(state)
	require.NotNil(release)
	require.True(s1 != state.(*TestState))
	release()
}

type String1 string
type String2 string

func TestStateHashTableTypedKeys(t *testing.T) {
	require := require.New(t)

	table := state_hash_table.New(TestParams{})
	s1, release := table.CreateState(String1("key"))
	require.NotNil(s1)
	require.NotNil(release)
	defer release()
	s2, release := table.CreateState(String2("key"))
	require.NotNil(s2)
	require.NotNil(release)
	defer release()

	require.True(s1 != s2)
}

func TestStateHashTableIterate(t *testing.T) {
	require := require.New(t)

	table := state_hash_table.New(TestParams{})
	for i := 0; i < 100; i++ {
		s, release := table.CreateState(i)
		require.NotNil(s)
		require.NotNil(release)
		s.(*TestState).hold = true
		s.(*TestState).idx = i
		release()
	}

	it := table.Iterator()
	require.NotNil(it)
	seen := [100]bool{}
	for i := 0; i < 100; i++ {
		require.True(it.Valid())
		k := it.Key()
		s := it.State()
		require.NotNil(k)
		require.NotNil(s)
		require.Equal(k.(int), s.(*TestState).idx)
		idx := s.(*TestState).idx
		require.True(0 <= idx && idx < 100)
		seen[idx] = true
		it.Next()
	}
	for i := 0; i < 100; i++ {
		require.True(seen[i])
	}
}
