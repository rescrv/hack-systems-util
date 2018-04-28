package lockfree

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"hack.systems/random/guacamole"
)

func TestPrime(t *testing.T) {
	require := require.New(t)
	require.False(isPrimed(nil))
	require.Equal(unsafe.Pointer(nil), deprime(prime(nil)))
	require.True(isPrimed(prime(nil)))
	var v interface{} = 5
	p := toptr(v)
	require.False(isPrimed(p))
	require.True(isPrimed(prime(p)))
}

func TestWrapUnwrap(t *testing.T) {
	require := require.New(t)
	require.Equal(nil, unwrap(toptr(nil)))
	require.Equal("hi", unwrap(toptr("hi")))
	require.Equal(5, unwrap(toptr(5)))
}

type generalMap interface {
	Put(k, v uint64)
	Get(k uint64) uint64
}

const WARMUP = 1 << 16
const KEYSPACE = 1 << 24
const BATCHING = 1000

func warmup(g generalMap) {
	guac := guacamole.New()
	guac.Seed(0)
	for i := 0; i < WARMUP; i++ {
		k := guac.Uint64() % KEYSPACE
		v := guac.Uint64()
		g.Put(k, v)
	}
}

func worker(pb *testing.PB, incr uint64, count *uint64, probabilityRead float64, g generalMap) {
	guac := guacamole.New()
	guac.Seed(atomic.AddUint64(count, incr))
	for pb.Next() {
		for i := 0; i < BATCHING; i++ {
			if guac.Float64() < probabilityRead {
				k := guac.Uint64() % KEYSPACE
				g.Get(k)
			} else {
				k := guac.Uint64() % KEYSPACE
				v := guac.Uint64()
				g.Put(k, v)
			}
		}
	}
}

func benchmark(b *testing.B, goroutines uint64, probabilityRead float64, g generalMap) {
	warmup(g)
	b.SetParallelism(int(goroutines))
	b.SetBytes(BATCHING)
	b.ResetTimer()
	var count uint64
	b.RunParallel(func(pb *testing.PB) {
		worker(pb, math.MaxUint64/goroutines, &count, probabilityRead, g)
	})
}

type builtinMap struct {
	mtx sync.Mutex
	m   map[uint64]uint64
}

func (m *builtinMap) Put(k, v uint64) {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	m.m[k] = v
}

func (m *builtinMap) Get(k uint64) uint64 {
	m.mtx.Lock()
	defer m.mtx.Unlock()
	if v, ok := m.m[k]; ok {
		return v
	} else {
		return 0
	}
}

type syncMap struct {
	m sync.Map
}

func (m *syncMap) Put(k, v uint64) {
	m.m.Store(k, v)
}

func (m *syncMap) Get(k uint64) uint64 {
	v, ok := m.m.Load(k)
	if ok {
		return v.(uint64)
	} else {
		return 0
	}
}

type Uint64Uint64Helper struct {
}

func (h *Uint64Uint64Helper) HashKey(k interface{}) uint64 {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, k.(uint64))
	f := fnv.New64a()
	f.Write(buf)
	return f.Sum64()
}

func (h *Uint64Uint64Helper) KeysEqual(k1, k2 interface{}) bool {
	x := k1.(uint64)
	y := k2.(uint64)
	return x == y
}

func (h *Uint64Uint64Helper) ValuesEqual(v1, v2 interface{}) bool {
	return v1.(uint64) == v2.(uint64)
}

type lockfreeMap struct {
	lockfree *Map
}

func (m *lockfreeMap) Put(k, v uint64) {
	m.lockfree.Put(k, v)
}

func (m *lockfreeMap) Get(k uint64) uint64 {
	v, ok := m.lockfree.Get(k)
	if ok {
		return v.(uint64)
	} else {
		return 0
	}
}

func newBuiltin() generalMap {
	return &builtinMap{
		m: make(map[uint64]uint64),
	}
}

func newSync() generalMap {
	return &syncMap{}
}

func newLockfree() generalMap {
	return &lockfreeMap{
		lockfree: NewMap(&Uint64Uint64Helper{}),
	}
}

func BenchmarkBuiltin50r50w1g(b *testing.B)   { benchmark(b, 1, 0.50, newBuiltin()) }
func BenchmarkBuiltin95r5w1g(b *testing.B)    { benchmark(b, 1, 0.95, newBuiltin()) }
func BenchmarkBuiltin99r1w1g(b *testing.B)    { benchmark(b, 1, 0.99, newBuiltin()) }
func BenchmarkBuiltin50r50w16g(b *testing.B)  { benchmark(b, 1, 0.50, newBuiltin()) }
func BenchmarkBuiltin95r5w16g(b *testing.B)   { benchmark(b, 1, 0.95, newBuiltin()) }
func BenchmarkBuiltin99r1w16g(b *testing.B)   { benchmark(b, 1, 0.99, newBuiltin()) }
func BenchmarkBuiltin50r50w256g(b *testing.B) { benchmark(b, 1, 0.50, newBuiltin()) }
func BenchmarkBuiltin95r5w256g(b *testing.B)  { benchmark(b, 1, 0.95, newBuiltin()) }
func BenchmarkBuiltin99r1w256g(b *testing.B)  { benchmark(b, 1, 0.99, newBuiltin()) }

func BenchmarkSync50r50w1g(b *testing.B)   { benchmark(b, 1, 0.50, newSync()) }
func BenchmarkSync95r5w1g(b *testing.B)    { benchmark(b, 1, 0.95, newSync()) }
func BenchmarkSync99r1w1g(b *testing.B)    { benchmark(b, 1, 0.99, newSync()) }
func BenchmarkSync50r50w16g(b *testing.B)  { benchmark(b, 1, 0.50, newSync()) }
func BenchmarkSync95r5w16g(b *testing.B)   { benchmark(b, 1, 0.95, newSync()) }
func BenchmarkSync99r1w16g(b *testing.B)   { benchmark(b, 1, 0.99, newSync()) }
func BenchmarkSync50r50w256g(b *testing.B) { benchmark(b, 1, 0.50, newSync()) }
func BenchmarkSync95r5w256g(b *testing.B)  { benchmark(b, 1, 0.95, newSync()) }
func BenchmarkSync99r1w256g(b *testing.B)  { benchmark(b, 1, 0.99, newSync()) }

func BenchmarkLockfree50r50w1g(b *testing.B)   { benchmark(b, 1, 0.50, newLockfree()) }
func BenchmarkLockfree95r5w1g(b *testing.B)    { benchmark(b, 1, 0.95, newLockfree()) }
func BenchmarkLockfree99r1w1g(b *testing.B)    { benchmark(b, 1, 0.99, newLockfree()) }
func BenchmarkLockfree50r50w16g(b *testing.B)  { benchmark(b, 1, 0.50, newLockfree()) }
func BenchmarkLockfree95r5w16g(b *testing.B)   { benchmark(b, 1, 0.95, newLockfree()) }
func BenchmarkLockfree99r1w16g(b *testing.B)   { benchmark(b, 1, 0.99, newLockfree()) }
func BenchmarkLockfree50r50w256g(b *testing.B) { benchmark(b, 1, 0.50, newLockfree()) }
func BenchmarkLockfree95r5w256g(b *testing.B)  { benchmark(b, 1, 0.95, newLockfree()) }
func BenchmarkLockfree99r1w256g(b *testing.B)  { benchmark(b, 1, 0.99, newLockfree()) }
