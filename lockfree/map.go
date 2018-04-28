package lockfree

import (
	"runtime"
	"sync/atomic"
	"time"
	"unsafe"

	"hack.systems/util/assert"
)

// This is a nearly wait-free hash map.  Strictly speaking, it's lock-free
// because of the possibility of chained resize operations, but operations
// outside of resize will see behavior equivalent to wait-free.
//
// Construct a map with NewMap.
//
// This design is borrowed from Cliff Click's lockfree hash table.  His code and
// presentation were the inspiration for a port of this map that I did to C++.
// That C++ implementation is the basis for this implementation.  Cliff's
// original code was put his code in the public domain, "... as explained at
// http://creativecommons.org/licenses/publicdomain"
// - Video from Cliff: https://www.youtube.com/watch?v=HJ-719EGIts
// - Code from Cliff: https://github.com/boundary/high-scale-lib
// - Code in C++: https://github.com/rescrv/e/blob/master/e/nwf_hash_map.h
type Map struct {
	helper     MapHelper
	table      unsafe.Pointer
	size       uint64
	lastResize int64
}

type MapHelper interface {
	HashKey(k interface{}) uint64
	KeysEqual(k1, k2 interface{}) bool
	ValuesEqual(v1, v2 interface{}) bool
}

func NewMap(helper MapHelper) *Map {
	m := &Map{
		helper: helper,
	}
	t := newTable(1, MAP_MIN_SIZE)
	atomic.StorePointer(&m.table, unsafe.Pointer(t))
	return m
}

func (m *Map) Empty() bool {
	return m.Size() == 0
}

func (m *Map) Size() uint64 {
	return atomic.LoadUint64(&m.size)
}

func (m *Map) Put(key, val interface{}) bool {
	obs := m.putIfMatch(toptr(key), NO_MATCH_OLD, toptr(val))
	assert.False(isPrimed(obs), "putIfMatch returned primed value")
	return true
}

func (m *Map) PutIfExist(key, val interface{}) bool {
	obs := m.putIfMatch(toptr(key), MATCH_ANY, toptr(val))
	assert.False(isPrimed(obs), "putIfMatch returned primed value")
	return obs != TOMBSTONE && obs != nil
}

func (m *Map) PutIfNotExist(key, val interface{}) bool {
	obs := m.putIfMatch(toptr(key), TOMBSTONE, toptr(val))
	assert.False(isPrimed(obs), "putIfMatch returned primed value")
	return obs == TOMBSTONE
}

func (m *Map) CompareAndSwap(key, cmp, val interface{}) bool {
	obs := m.putIfMatch(toptr(key), toptr(cmp), toptr(val))
	assert.False(isPrimed(obs), "putIfMatch returned primed value")
	return m.compareValues(toptr(cmp), obs)
}

func (m *Map) Delete(key interface{}) bool {
	obs := m.putIfMatch(toptr(key), NO_MATCH_OLD, TOMBSTONE)
	assert.False(isPrimed(obs), "putIfMatch returned primed value")
	return obs != TOMBSTONE
}

func (m *Map) DeleteIf(key, val interface{}) bool {
	obs := m.putIfMatch(toptr(key), toptr(val), TOMBSTONE)
	assert.False(isPrimed(obs), "putIfMatch returned primed value")
	return m.compareValues(toptr(val), obs)
}

func (m *Map) Has(key interface{}) bool {
	_, ok := m.Get(key)
	return ok
}

func (m *Map) Get(key interface{}) (interface{}, bool) {
	hash := m.hashKey(toptr(key))
	return m.get(m.getTable(), hash, toptr(key))
}

// implementation

const (
	MAP_REPROBE_LIMIT = 10
	MAP_MIN_SIZE_LOG  = 3
	MAP_MIN_SIZE      = 1 << MAP_MIN_SIZE_LOG
)

var _SENTINEL1 interface{}
var _SENTINEL2 interface{}
var _SENTINEL3 interface{}
var _SENTINEL4 interface{}
var NO_MATCH_OLD unsafe.Pointer = unsafe.Pointer(toptr(_SENTINEL1))
var MATCH_ANY unsafe.Pointer = unsafe.Pointer(toptr(_SENTINEL2))
var TOMBSTONE unsafe.Pointer = unsafe.Pointer(toptr(_SENTINEL3))
var TOMBPRIME unsafe.Pointer = unsafe.Pointer(toptr(_SENTINEL4))

func toptr(iface interface{}) unsafe.Pointer {
	return unsafe.Pointer(&iface)
}

type primer struct {
	ptr unsafe.Pointer
}

func prime(p unsafe.Pointer) unsafe.Pointer {
	var iface interface{} = primer{p}
	return unsafe.Pointer(&iface)
}

func deprime(p unsafe.Pointer) unsafe.Pointer {
	if p == nil {
		return p
	}
	iface := *(*interface{})(p)
	switch v := iface.(type) {
	case primer:
		return v.ptr
	default:
		return p
	}
}

func isPrimed(p unsafe.Pointer) bool {
	if p == TOMBPRIME {
		return true
	}
	if p == nil || isSpecial(p) {
		return false
	}
	iface := *(*interface{})(p)
	_, ok := iface.(primer)
	return ok
}

func isSpecial(p unsafe.Pointer) bool {
	switch p {
	case nil, NO_MATCH_OLD, MATCH_ANY, TOMBSTONE, TOMBPRIME:
		return true
	default:
		return false
	}
}

func unwrap(p unsafe.Pointer) interface{} {
	p = deprime(p)
	if p == nil {
		return nil
	}
	return *(*interface{})(p)
}

func reprobeLimit(capacity uint64) uint64 {
	return MAP_REPROBE_LIMIT + capacity>>2
}

func (m *Map) millis() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

func (m *Map) hashKey(key unsafe.Pointer) uint64 {
	return m.helper.HashKey(unwrap(key))
}

func (m *Map) compareKey(k1, k2 unsafe.Pointer) bool {
	if k1 == k2 {
		return true
	}
	return k1 != nil && k2 != nil && !isSpecial(k1) && !isSpecial(k2) &&
		m.helper.ValuesEqual(unwrap(k1), unwrap(k2))
}

func (m *Map) compareValues(v1, v2 unsafe.Pointer) bool {
	if v1 == v2 {
		return true
	}
	return v1 != nil && v2 != nil && !isSpecial(v1) && !isSpecial(v2) &&
		m.helper.ValuesEqual(unwrap(v1), unwrap(v2))
}

func (m *Map) getTable() *table {
	return (*table)(atomic.LoadPointer(&m.table))
}

func (m *Map) incSize() {
	atomic.AddUint64(&m.size, 1)
}

func (m *Map) decSize() {
	atomic.AddUint64(&m.size, ^uint64(0))
}

func (m *Map) get(t *table, hash uint64, key unsafe.Pointer) (interface{}, bool) {
	mask := t.capacity - 1
	idx := hash & mask
	var reprobes uint64
	for {
		k := atomic.LoadPointer(&t.nodes[idx].key)
		v := atomic.LoadPointer(&t.nodes[idx].val)
		if k == nil {
			return nil, false
		}
		nested := (*table)(atomic.LoadPointer(&t.next))
		if m.compareKey(key, k) {
			if !isPrimed(v) {
				if v == nil || v == TOMBSTONE {
					return nil, false
				}
				return unwrap(v), true
			}
			nested = t.copySlotAndCheck(m, idx, true)
			return m.get(nested, hash, key)
		}
		reprobes++
		if reprobes >= reprobeLimit(t.capacity) || k == TOMBSTONE {
			nested = (*table)(atomic.LoadPointer(&t.next))
			if nested != nil {
				nested = m.helpCopy(nested)
				return m.get(nested, hash, key)
			}
			return nil, false
		}
		idx = (idx + 1) & mask
	}
}

func (m *Map) putIfMatch(key, expVal, putVal unsafe.Pointer) unsafe.Pointer {
	assert.True(key != nil, "putIfMatch expects non-nil key")
	assert.True(expVal != nil, "putIfMatch expects non-nil expVal")
	assert.True(putVal != nil, "putIfMatch expects non-nil putVal")
	return m.putIfMatchTable(m.getTable(), key, expVal, putVal)
}

func (m *Map) putIfMatchTable(t *table, key, expVal, putVal unsafe.Pointer) unsafe.Pointer {
	assert.True(!isPrimed(expVal), "putIfMatch expects non-nil expVal")
	assert.True(!isPrimed(putVal), "putIfMatch expects non-nil putVal")
	hash := m.hashKey(key)
	mask := t.capacity - 1
	idx := hash & mask
	var reprobes uint64

	// Protect against forever recursing on old tables.  When a table is
	// installed, it is guaranteed all slots have been copied and tombstoned, so
	// trying to put into that table is guaranteed to fail.  That failure is
	// expensive compared to a regular put, and a highly churning table can let
	// that key get permanently far behind.
	if table := m.getTable(); table.depth > t.depth {
		return m.putIfMatchTable(table, key, expVal, putVal)
	}

	var k unsafe.Pointer
	var v unsafe.Pointer
	var nested *table

	for {
		k = atomic.LoadPointer(&t.nodes[idx].key)
		v = atomic.LoadPointer(&t.nodes[idx].val)
		if k == nil {
			if putVal == TOMBSTONE {
				return putVal
			}
			if atomic.CompareAndSwapPointer(&t.nodes[idx].key, nil, key) {
				t.incSlots()
				break
			}
			k = atomic.LoadPointer(&t.nodes[idx].key)
		}
		nested = (*table)(atomic.LoadPointer(&t.next))
		if m.compareKey(key, k) {
			break
		}
		reprobes++
		if reprobes >= reprobeLimit(t.capacity) || k == TOMBSTONE {
			nested = t.resize(m)
			if expVal != nil {
				m.helpCopy(nested)
			}
			return m.putIfMatchTable(nested, key, expVal, putVal)
		}
		idx = (idx + 1) & mask
	}

	// if v is primed, then we are copying, have copied this value, and cannot
	// check that it's equal within this table as the next table could have
	// overwritten it.
	if !isPrimed(v) && putVal == v {
		return v
	}

	if nested == nil && ((v == nil && t.tableIsFull(reprobes)) || isPrimed(v)) {
		nested = t.resize(m)
	}
	if nested != nil {
		nested = t.copySlotAndCheck(m, idx, expVal != nil)
		return m.putIfMatchTable(nested, key, expVal, putVal)
	}

	for {
		assert.False(isPrimed(v), "unexpectedly primed value")
		if expVal != NO_MATCH_OLD &&
			v != expVal &&
			(expVal != MATCH_ANY || v == TOMBSTONE || v == nil) &&
			!(v == nil && expVal == TOMBSTONE) &&
			(expVal == nil || !m.compareValues(expVal, v)) {
			return v
		}
		if atomic.CompareAndSwapPointer(&t.nodes[idx].val, v, putVal) {
			if expVal != nil {
				if (v == nil || v == TOMBSTONE) && putVal != TOMBSTONE {
					t.incSize()
					m.incSize()
				}
				if !(v == nil || v == TOMBSTONE) && putVal == TOMBSTONE {
					t.decSize()
					m.decSize()
				}
				if v == nil {
					return TOMBSTONE
				}
			}
			return v
		}
		v = atomic.LoadPointer(&t.nodes[idx].val)
		if isPrimed(v) {
			nested = t.copySlotAndCheck(m, idx, expVal != nil)
			return m.putIfMatchTable(nested, key, expVal, putVal)
		}
	}
}

func (m *Map) helpCopy(t *table) *table {
	top := m.getTable()
	if (*table)(atomic.LoadPointer(&top.next)) == nil {
		return t
	}
	top.helpCopy(m, false)
	return t
}

type table struct {
	capacity uint64
	depth    uint64
	slots    uint64
	elems    uint64
	copyIdx  uint64
	copyDone uint64
	next     unsafe.Pointer
	nodes    []node
}

type node struct {
	key unsafe.Pointer
	val unsafe.Pointer
}

func newTable(depth, capacity uint64) *table {
	assert.True(capacity > 0 && (capacity&(capacity-1)) == 0,
		"capacity must be a power of two")
	return &table{
		capacity: capacity,
		depth:    depth,
		nodes:    make([]node, capacity),
	}
}

func (t *table) incSlots() {
	atomic.AddUint64(&t.slots, 1)
}

func (t *table) size() uint64 {
	return atomic.LoadUint64(&t.elems)
}

func (t *table) incSize() {
	atomic.AddUint64(&t.elems, 1)
}

func (t *table) decSize() {
	atomic.AddUint64(&t.elems, ^uint64(0))
}

func (t *table) tableIsFull(reprobes uint64) bool {
	return reprobes >= MAP_REPROBE_LIMIT && atomic.LoadUint64(&t.slots) >= t.capacity>>2
}

func (t *table) resize(m *Map) *table {
	nested := (*table)(atomic.LoadPointer(&t.next))
	if nested != nil {
		return nested
	}

	oldSize := t.size()
	newSize := oldSize

	if oldSize >= t.capacity>>2 {
		newSize = t.capacity << 1
		if oldSize >= t.capacity>>1 {
			newSize = t.capacity << 2
		}
	}

	millis := m.millis()
	if newSize < t.capacity &&
		millis <= atomic.LoadInt64(&m.lastResize)+1000 &&
		atomic.LoadUint64(&t.slots) >= oldSize<<1 {
		newSize = t.capacity << 1
	}
	if newSize < t.capacity {
		newSize = t.capacity
	}

	var log2 uint64
	for log2 = MAP_MIN_SIZE_LOG; 1<<log2 < newSize; log2++ {
	}

	assert.True(newSize >= t.capacity, "table should always grow in size")
	assert.True(1<<log2 >= t.capacity, "table should always grow in size")

	nested = (*table)(atomic.LoadPointer(&t.next))
	if nested != nil {
		return nested
	}

	nt := newTable(t.depth+1, 1<<log2)
	nested = (*table)(atomic.LoadPointer(&t.next))
	if nested != nil {
		return nested
	}
	if atomic.CompareAndSwapPointer(&t.next, nil, unsafe.Pointer(nt)) {
		nested = nt
	} else {
		nested = (*table)(atomic.LoadPointer(&t.next))
	}
	assert.True(nested != nil, "nested must not be nil")
	return nested
}

func (t *table) helpCopy(m *Map, copyAll bool) {
	nested := (*table)(atomic.LoadPointer(&t.next))
	assert.True(nested != nil, "cannot help copy to empty table")
	MIN_COPY_WORK := t.capacity
	if MIN_COPY_WORK > 1024 {
		MIN_COPY_WORK = 1024
	}
	shouldPanic := false
	var idx uint64

	for atomic.LoadUint64(&t.copyDone) < t.capacity {
		if !shouldPanic {
			idx = atomic.LoadUint64(&t.copyIdx)
			for idx < t.capacity<<1 {
				idx = atomic.LoadUint64(&t.copyIdx)
				if atomic.CompareAndSwapUint64(&t.copyIdx, idx, idx+MIN_COPY_WORK) {
					break
				}
			}
			if idx >= t.capacity<<1 {
				shouldPanic = true
			}
		}
		var workDone uint64
		for i := uint64(0); i < MIN_COPY_WORK; i++ {
			if t.copySlot(m, (idx+i)&(t.capacity-1), nested) {
				workDone++
			}
		}
		if workDone > 0 {
			t.copyCheckAndPromote(m, workDone)
		}
		idx += MIN_COPY_WORK
		if !copyAll && !shouldPanic {
			return
		}
		runtime.Gosched()
	}

	t.copyCheckAndPromote(m, 0)
}

func (t *table) copySlotAndCheck(m *Map, idx uint64, shouldHelp bool) *table {
	nested := (*table)(atomic.LoadPointer(&t.next))
	assert.True(nested != nil, "cannot help copy to empty table")
	if t.copySlot(m, idx, nested) {
		t.copyCheckAndPromote(m, 1)
	}
	if shouldHelp {
		return m.helpCopy(nested)
	} else {
		return nested
	}
}

func (t *table) copyCheckAndPromote(m *Map, workDone uint64) {
	done := atomic.LoadUint64(&t.copyDone)
	assert.True(done+workDone <= t.capacity, "work done cannot exceed capacity")
	if workDone > 0 {
		done = atomic.AddUint64(&t.copyDone, workDone)
		assert.True(done <= t.capacity, "total work done cannot exceed capacity")
	}
	nested := (*table)(atomic.LoadPointer(&t.next))
	if done >= t.capacity && m.getTable() == t &&
		(*table)(atomic.LoadPointer(&m.table)) == t &&
		atomic.CompareAndSwapPointer(&m.table, unsafe.Pointer(t), unsafe.Pointer(nested)) {
		atomic.StoreInt64(&m.lastResize, m.millis())
	}
}

func (t *table) copySlot(m *Map, idx uint64, newTable *table) bool {
	kwitness := atomic.LoadPointer(&t.nodes[idx].key)
	for kwitness == nil {
		if atomic.CompareAndSwapPointer(&t.nodes[idx].key, nil, TOMBSTONE) {
			tmp := atomic.LoadPointer(&t.nodes[idx].val)
			for !atomic.CompareAndSwapPointer(&t.nodes[idx].val, tmp, TOMBPRIME) {
				tmp = atomic.LoadPointer(&t.nodes[idx].val)
			}
			return true
		}
		kwitness = atomic.LoadPointer(&t.nodes[idx].key)
	}
	if kwitness == TOMBSTONE {
		return false
	}
	oldVal := atomic.LoadPointer(&t.nodes[idx].val)
	for !isPrimed(oldVal) {
		box := TOMBPRIME
		if oldVal != nil && oldVal != TOMBSTONE {
			box = prime(oldVal)
		}
		if atomic.CompareAndSwapPointer(&t.nodes[idx].val, oldVal, box) {
			if box == TOMBPRIME {
				return true
			}
			oldVal = box
			break
		}
		oldVal = atomic.LoadPointer(&t.nodes[idx].val)
	}
	if oldVal == TOMBPRIME {
		return false
	}
	key := atomic.LoadPointer(&t.nodes[idx].key)
	oldUnboxed := deprime(oldVal)
	assert.True(oldUnboxed != TOMBSTONE, "old value should not be TOMBSTONE")
	newTable.incSize()
	m.putIfMatchTable(newTable, key, nil, oldUnboxed)
	for !atomic.CompareAndSwapPointer(&t.nodes[idx].val, oldVal, TOMBPRIME) {
		oldVal = atomic.LoadPointer(&t.nodes[idx].val)
		if oldVal == TOMBPRIME {
			break
		}
	}
	if oldVal == TOMBPRIME {
		newTable.decSize()
		return false
	}
	return true
}
