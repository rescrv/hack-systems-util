package tiny_lfu

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"sync"
	"sync/atomic"

	"hack.systems/util/bloom"
)

type TinyLFU32 struct {
	memory  uint32
	counter uint32
	epoch   uint64
	keys    uint
	counts  []uint32
	mtx     sync.Mutex
}

func New32(memory uint32, space uint64) *TinyLFU32 {
	N := float64(memory)
	M := float64(space) / 4
	P := bloom.BloomParamsP(N, M)
	if P < minP {
		P = minP
		M = bloom.BloomParamsM(N, P)
		if M*4 > float64(space) {
			panic("error in bloom parameter calculations")
		}
	}
	K := bloom.KeysForProbability(P)
	counts := uint64(M)
	return &TinyLFU32{
		memory: memory,
		keys:   uint(math.Ceil(K)),
		counts: make([]uint32, counts),
	}
}

func (t *TinyLFU32) Tally(key string) {
	h := t.hash(key)
	for i := uint(0); i < t.keys; i++ {
		atomic.AddUint32(&t.counts[h[i]], 1)
	}
	if atomic.AddUint32(&t.counter, 1) == t.memory {
		t.decimate()
	}
}

func (t *TinyLFU32) ShouldReplace(victim, candidate string) bool {
	hv := t.hash(victim)
	hc := t.hash(candidate)
	// TODO(rescrv):  This will spin during a decimation to keep results
	// correct.  Maybe allow it to give incorrect results in constant time.
	for {
		vCount, vEpoch := t.read(hv)
		cCount, cEpoch := t.read(hc)
		if vEpoch == cEpoch {
			// this is the conditional to not take for incorrect results
			if cEpoch&0x1 == 1 {
				continue
			}
			return vCount < cCount
		}
	}
}

func (t *TinyLFU32) hash(key string) [4]uint64 {
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

func (t *TinyLFU32) read(hashes [4]uint64) (uint32, uint64) {
	for {
		epoch := atomic.LoadUint64(&t.epoch)
		count := ^uint32(0)
		for i := uint(0); i < t.keys; i++ {
			x := atomic.LoadUint32(&t.counts[hashes[i]])
			if x < count {
				count = x
			}
		}
		if epoch == atomic.LoadUint64(&t.epoch) {
			return count, epoch
		}
	}
}

func (t *TinyLFU32) decimate() {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	atomic.AddUint64(&t.epoch, 1)
	divideTwo(&t.counter)
	for i := 0; i < len(t.counts); i++ {
		divideTwo(&t.counts[i])
	}
	atomic.AddUint64(&t.epoch, 1)
}

func divideTwo(p *uint32) {
	for {
		value := atomic.LoadUint32(p)
		if atomic.CompareAndSwapUint32(p, value, value/2) {
			break
		}
	}
}
