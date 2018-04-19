package tiny_lfu

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"sync/atomic"

	"hack.systems/util/bloom"
)

type TinyLFU64 struct {
	keys   uint
	counts []uint64
}

func New64(memory uint64, space uint64) *TinyLFU64 {
	N := float64(memory)
	M := float64(space) / 8
	P := bloom.BloomParamsP(N, M)
	if P < minP {
		P = minP
		M = bloom.BloomParamsM(N, P)
		if M*8 > float64(space) {
			panic("error in bloom parameter calculations")
		}
	}
	K := bloom.KeysForProbability(P)
	counts := uint64(M)
	return &TinyLFU64{
		keys:   uint(math.Ceil(K)),
		counts: make([]uint64, counts),
	}
}

func (t *TinyLFU64) Tally(key string) {
	h := t.hash(key)
	for i := uint(0); i < t.keys; i++ {
		atomic.AddUint64(&t.counts[h[i]], 1)
	}
}

func (t *TinyLFU64) ShouldReplace(victim, candidate string) bool {
	hv := t.hash(victim)
	hc := t.hash(candidate)
	vCount := t.read(hv)
	cCount := t.read(hc)
	return vCount < cCount
}

func (t *TinyLFU64) hash(key string) [4]uint64 {
	buf := make([]byte, 0, 32)
	h := [4]uint64{}
	h1 := fnv.New128()
	h1.Write([]byte(key))
	h2 := fnv.New128a()
	h2.Write([]byte(key))
	buf = h1.Sum(buf)
	buf = h2.Sum(buf)
	mod := uint64(len(t.counts))
	h[0] = binary.BigEndian.Uint64(buf[:8]) % mod
	h[1] = binary.BigEndian.Uint64(buf[8:16]) % mod
	h[2] = binary.BigEndian.Uint64(buf[16:24]) % mod
	h[3] = binary.BigEndian.Uint64(buf[24:32]) % mod
	return h
}

func (t *TinyLFU64) read(hashes [4]uint64) uint64 {
	count := ^uint64(0)
	for i := uint(0); i < t.keys; i++ {
		x := atomic.LoadUint64(&t.counts[hashes[i]])
		if x < count {
			count = x
		}
	}
	return count
}
