package main

import (
	"container/list"
	"flag"

	"hack.systems/random/armnod"
	"hack.systems/random/guacamole"

	"hack.systems/util/caching/tiny_lfu"
	"hack.systems/util/ubench"
)

type cache interface {
	Warm() bool
	NextEviction(key string) string
	Insert(key string)
	Contains(key string) bool
	Evict(key string)
}

type parameters struct {
	// Workload generation
	Operations uint64  `number of cache operations to perform`
	ReadRatio  float64 `probability of an operation being a read`
	Objects    uint64  `number of cacheable objects`
	ZipfTheta  float64 `theta parameter to zipf distribution`
	Seed       uint64  `guacamole seed`
	// TinyLFU configuration
	UseTLFU bool
	Memory  uint64 `memory parameter to TinyLFU`
	Space   uint64 `space parameter to TinyLFU`
	// Cache configuration
	Algorithm string `cache eviction algorithm to simulate`
	CacheSize uint64 `objects that can fit in cache`
}

type result struct {
	Reads   uint64 `number of read operations performed`
	Hits    uint64 `number of cache hits`
	Inserts uint64 `number of cache misses that populated the entry`
	Writes  uint64 `number of writes/invalidations`
}

type RecentlyUsedCache struct {
	List  *list.List
	Items map[string]*list.Element
}

func NewRUC() *RecentlyUsedCache {
	c := &RecentlyUsedCache{
		List:  list.New(),
		Items: make(map[string]*list.Element),
	}
	return c
}

func (C *RecentlyUsedCache) MoveToFront(key string) {
	if elem, ok := C.Items[key]; ok {
		C.List.MoveToFront(elem)
	} else {
		panic("invariants broken")
	}
}

func (C *RecentlyUsedCache) InsertFront(key string) {
	if _, ok := C.Items[key]; ok {
		panic("invariants broken")
	}
	elem := C.List.PushFront(key)
	C.Items[key] = elem
}

func (C *RecentlyUsedCache) Remove(key string) {
	if elem, ok := C.Items[key]; ok {
		C.List.Remove(elem)
		delete(C.Items, elem.Value.(string))
	}
}

func (C *RecentlyUsedCache) Has(key string) bool {
	_, ok := C.Items[key]
	return ok
}

func (C *RecentlyUsedCache) Size() uint64 {
	return uint64(len(C.Items))
}

type FIFO struct {
	size uint64
	ruc  *RecentlyUsedCache
}

func NewFIFO(capacity uint64) cache {
	return &FIFO{
		size: capacity,
		ruc:  NewRUC(),
	}
}

func (F *FIFO) Warm() bool {
	return F.ruc.Size() >= F.size
}

func (F *FIFO) NextEviction(key string) string {
	if F.ruc.List.Len() > 0 {
		return F.ruc.List.Back().Value.(string)
	}
	return ""
}

func (F *FIFO) Insert(key string) {
	if !F.ruc.Has(key) {
		F.ruc.InsertFront(key)
		if F.ruc.Size() > F.size {
			F.ruc.Remove(F.NextEviction(key))
		}
	}
}

func (F *FIFO) Contains(key string) bool {
	return F.ruc.Has(key)
}

func (F *FIFO) Evict(key string) {
	if F.ruc.Has(key) {
		F.ruc.Remove(key)
	}
}

type LRU struct {
	size uint64
	ruc  *RecentlyUsedCache
}

func NewLRU(capacity uint64) cache {
	return &LRU{
		size: capacity,
		ruc:  NewRUC(),
	}
}

func (L *LRU) Warm() bool {
	return L.ruc.Size() >= L.size
}

func (L *LRU) NextEviction(key string) string {
	if L.ruc.List.Len() > 0 {
		return L.ruc.List.Back().Value.(string)
	}
	return ""
}

func (L *LRU) Insert(key string) {
	if L.ruc.Has(key) {
		L.ruc.MoveToFront(key)
	} else {
		L.ruc.InsertFront(key)
		if L.ruc.Size() > L.size {
			L.ruc.Remove(L.NextEviction(key))
		}
	}
}

func (L *LRU) Contains(key string) bool {
	return L.ruc.Has(key)
}

func (L *LRU) Evict(key string) {
	if L.ruc.Has(key) {
		L.ruc.Remove(key)
	}
}

type OPT struct {
	chosen map[string]struct{}
}

func NewOPT(capacity uint64, elements uint64) cache {
	config := armnod.Configuration{
		Charset:       armnod.Default,
		StringChooser: armnod.InitializeFixedSet(elements),
		LengthChooser: armnod.ConstantLengthChooser{8},
	}
	G := config.Generator()
	chosen := make(map[string]struct{})
	for i := uint64(0); i < capacity; i++ {
		s, ok := G.String()
		if !ok {
			panic("invariants broken")
		}
		chosen[s] = struct{}{}
	}
	return &OPT{
		chosen: chosen,
	}
}

func (O *OPT) Warm() bool {
	return true
}

func (O *OPT) NextEviction(key string) string {
	if _, ok := O.chosen[key]; ok {
		return ""
	}
	return key
}

func (O *OPT) Insert(key string) {
}

func (O *OPT) Contains(key string) bool {
	_, ok := O.chosen[key]
	return ok
}

func (O *OPT) Evict(key string) {
}

func simulate(params parameters) result {
	G := guacamole.New()
	zp := guacamole.ZipfTheta(params.Objects, params.ZipfTheta)
	A := armnod.Configuration{
		Charset:       armnod.Default,
		StringChooser: armnod.ChooseFromFixedSetZipf(zp),
		LengthChooser: armnod.ConstantLengthChooser{8},
	}.Generator()
	T := tiny_lfu.New64(params.Memory, params.Space)
	var C cache
	switch params.Algorithm {
	case "LRU":
		C = NewLRU(params.CacheSize)
	case "FIFO":
		C = NewFIFO(params.CacheSize)
	case "OPT":
		C = NewOPT(params.CacheSize, params.Objects)
	default:
		panic("unknown cache algorithm")
	}
	R := result{}
	G.Seed(params.Seed)
	A.Seed(params.Seed)
	for !C.Warm() {
		s, ok := A.String()
		if !ok {
			panic("bad configuation: Objects must never exhaust")
		}
		C.Insert(s)
	}
	for i := uint64(0); i < params.Operations; i++ {
		s, ok := A.String()
		if !ok {
			panic("bad configuation: Objects must never exhaust")
		}
		if G.Float64() < params.ReadRatio {
			T.Tally(s)
			if C.Contains(s) {
				R.Hits++
			} else if params.UseTLFU {
				victim := C.NextEviction(s)
				if T.ShouldReplace(victim, s) {
					R.Inserts++
					C.Insert(s)
				}
			} else {
				R.Inserts++
				C.Insert(s)
			}
			R.Reads++
		} else {
			C.Evict(s)
			R.Writes++
		}
	}
	return R
}

func main() {
	params := parameters{
		Operations: 4e6,
		ReadRatio:  0.99,
		Objects:    1e10,
		ZipfTheta:  0.9,
		Seed:       0,
		Memory:     1e6,
		Space:      1e8,
		Algorithm:  "LRU",
		CacheSize:  1e6,
	}
	var results result

	// setup
	ubench.AddFlags(&params)
	flag.Parse()
	ubench.PrintCommentString(params, results)
	// run without tlfu
	params.UseTLFU = false
	results = simulate(params)
	ubench.PrintResultString(params, results)
	// run with tlfu
	params.UseTLFU = true
	results = simulate(params)
	ubench.PrintResultString(params, results)
}
