package main

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"sync"

	"log"

	"hack.systems/random/guacamole"
	"hack.systems/util/lockfree"
)

type op uint64

const (
	opPut op = iota
	opPutIfExist
	opPutIfNotExist
	opCompareAndSwap
	opDelete
	opHas
	opGet
	opSentinel
)

type parameters struct {
	Workers   uint64
	KeysPer   uint64
	WaitGroup *sync.WaitGroup
}

func work(p parameters, idx uint64, m *lockfree.Map) {
	defer p.WaitGroup.Done()
	g := guacamole.New()
	g.Seed(math.MaxUint64 / p.Workers * idx)
	truth := make(map[uint64]uint64)
	for {
		k := (g.Uint64()%p.KeysPer)*p.Workers + idx
		v := g.Uint64()
		op := op(g.Uint64()) % opSentinel
		if op != opSentinel &&
			op != opPut &&
			op != opPutIfExist &&
			op != opPutIfNotExist &&
			op != opCompareAndSwap &&
			op != opDelete &&
			op != opHas &&
			op != opGet &&
			op != opSentinel {
			continue
		}
		switch op {
		case opPut:
			truth[k] = v
			m.Put(k, v)
			//log.Printf("put: m[%d] = %d\n", k, v)
		case opPutIfExist:
			outcome := m.PutIfExist(k, v)
			//log.Printf("putie: m[%d] = %d\n", k, v)
			if _, ok := truth[k]; ok {
				truth[k] = v
				if !outcome {
					panic("PutIfExist failed when key existed")
				}
			} else {
				if outcome {
					panic("PutIfExist succeeded when key did not exist")
				}
			}
		case opPutIfNotExist:
			//log.Printf("putine: m[%d] = %d\n", k, v)
			outcome := m.PutIfNotExist(k, v)
			if _, ok := truth[k]; ok {
				if outcome {
					panic("PutIfNotExist succeeded when key existed")
				}
			} else {
				truth[k] = v
				if !outcome {
					panic("PutIfNotExist failed when key did not exist")
				}
			}
		case opCompareAndSwap:
			compare := g.Uint64()
			expect := false
			if existing, ok := truth[k]; ok {
				if g.Uint64()%5 != 0 {
					expect = true
					compare = existing
				} else {
					expect = compare == existing
				}
			}
			if expect {
				truth[k] = v
			}
			//log.Printf("cas: m[%d] = %d,%d %v\n", k, compare, v, expect)
			if m.CompareAndSwap(k, compare, v) {
				if !expect {
					panic("CompareAndSwap succeeded when it should fail")
				}
			} else {
				if expect {
					panic("CompareAndSwap failed when it should succeed")
				}
			}
		case opDelete:
			if _, ok := truth[k]; ok {
				delete(truth, k)
			}
			m.Delete(k)
			//log.Printf("del: m[%d]", k)
		case opHas:
			if _, ok := truth[k]; ok {
				if !m.Has(k) {
					panic("Has failed when it should succeed")
				}
			} else {
				if m.Has(k) {
					panic("Has succeeded when it should fail")
				}
			}
		case opGet:
			tval, tok := truth[k]
			if tok {
				//log.Printf("get: m[%d] = %d\n", k, tval)
			} else {
				//log.Printf("get: m[%d] not found\n", k)
			}
			lval, lok := m.Get(k)
			if tok && !lok {
				panic("Get failed to retrieve expected item")
			}
			if !tok && lok {
				panic("Get retrieved non-existent item")
			}
			if tok == lok && tok && tval != lval {
				panic("Get retrieved wrong value")
			}
		case opSentinel:
		default:
			panic("this was unexpected")
		}
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

func main() {
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds | log.Llongfile | log.LUTC)
	params := parameters{
		Workers:   10,
		KeysPer:   100,
		WaitGroup: &sync.WaitGroup{},
	}
	m := lockfree.NewMap(&Uint64Uint64Helper{})
	for i := uint64(0); i < params.Workers; i++ {
		params.WaitGroup.Add(1)
		go work(params, i, m)
	}
	params.WaitGroup.Wait()
}
