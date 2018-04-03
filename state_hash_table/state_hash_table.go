package state_hash_table

import (
	"sync"
)

type Params interface {
	Hash(key interface{}) uint64
	NewState(key interface{}) State
}

type State interface {
	Finished() bool
}

type Releaser func()

type Iterator struct {
	table   *Table
	keys    []interface{}
	idx     int
	primed  bool
	key     interface{}
	state   State
	release Releaser
}

func New(params Params) *Table {
	return &Table{
		params: params,
		table:  make(map[interface{}]*wrapper),
	}
}

func (t *Table) CreateState(key interface{}) (State, Releaser) {
	ref := &reference{}
	state := t.newState(key)
	ref.acquire(t, state)
	if t.insert(state) {
		ref.unlock()
		return ref.Get(), ref.releaser()
	}
	ref.release()
	return nil, nop
}

func (t *Table) GetState(key interface{}) (State, Releaser) {
	ref := &reference{}
	for {
		state := t.lookup(key)
		if state == nil {
			return nil, nop
		}
		ref.acquire(t, state)
		if state.garbage {
			ref.release()
			continue
		}
		ref.unlock()
		return ref.Get(), ref.releaser()
	}
}

func (t *Table) GetOrCreateState(key interface{}) (State, Releaser) {
	ref := &reference{}
	for {
		state := t.lookup(key)
		if state != nil {
			ref.acquire(t, state)
		} else {
			state = t.newState(key)
			ref.acquire(t, state)
			if !t.insert(state) {
				ref.release()
				continue
			}
		}
		if state.garbage {
			ref.release()
			continue
		}
		ref.unlock()
		return ref.Get(), ref.releaser()
	}
}

func (t *Table) Iterator() *Iterator {
	iter := &Iterator{
		table: t,
	}
	t.mtx.Lock()
	defer t.mtx.Unlock()
	for k, _ := range t.table {
		iter.keys = append(iter.keys, k)
	}
	return iter
}

func (it *Iterator) Valid() bool {
	it.prime()
	return it.state != nil
}

func (it *Iterator) Next() {
	it.primed = false
	it.prime()
}

func (it *Iterator) Key() interface{} {
	return it.key
}

func (it *Iterator) State() State {
	return it.state
}

func (it *Iterator) Release() {
	if it.release != nil {
		it.release()
	}
	it.release = nop
}

// implementation

type wrapper struct {
	key      interface{}
	state    State
	mtx      sync.Mutex
	acquires uint64
	garbage  bool
}

type Table struct {
	params Params
	mtx    sync.Mutex
	table  map[interface{}]*wrapper
}

type reference struct {
	table  *Table
	state  *wrapper
	locked bool
}

func nop() {}

func (t *Table) newState(key interface{}) *wrapper {
	return &wrapper{
		key:   key,
		state: t.params.NewState(key),
	}
}

func (t *Table) insert(s *wrapper) bool {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	if _, ok := t.table[s.key]; ok {
		return false
	}
	t.table[s.key] = s
	return true
}

func (t *Table) remove(s *wrapper) {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	if x, ok := t.table[s.key]; ok && s == x {
		delete(t.table, s.key)
	}
}

func (t *Table) lookup(key interface{}) *wrapper {
	t.mtx.Lock()
	defer t.mtx.Unlock()
	if s, ok := t.table[key]; ok {
		return s
	}
	return nil
}

func (r *reference) Get() State {
	return r.state.state
}

func (r *reference) acquire(t *Table, s *wrapper) {
	if r.table != nil || r.state != nil || r.locked {
		r.release()
	}
	r.table = t
	r.state = s
	r.state.mtx.Lock()
	r.locked = true
	r.state.acquires++
}

func (r *reference) releaser() Releaser {
	return func() { r.release() }
}

func (r *reference) release() {
	if r.table == nil || r.state == nil {
		if r.table != nil || r.state != nil || r.locked {
			panic("invariants violated")
		}
		return
	}
	if !r.locked {
		r.state.mtx.Lock()
		r.locked = true
	}
	if r.state.acquires <= 0 {
		panic("invariants violated")
	}
	r.state.acquires--
	if r.state.acquires == 0 && !r.state.garbage && r.state.state.Finished() {
		r.state.garbage = true
		r.table.remove(r.state)
	}
	r.state.mtx.Unlock()
	r.table = nil
	r.state = nil
	r.locked = false
}

func (r *reference) unlock() {
	if r.locked {
		r.locked = false
		r.state.mtx.Unlock()
	}
}

func (it *Iterator) next() (interface{}, State, Releaser) {
	for it.idx < len(it.keys) {
		k := it.keys[it.idx]
		it.idx++
		s, r := it.table.GetState(k)
		if s != nil {
			return k, s, r
		}
	}
	it.idx = 0
	it.keys = nil
	return nil, nil, nil
}

func (it *Iterator) prime() {
	if it.primed {
		return
	}
	if it.release != nil {
		it.release()
	}
	it.key, it.state, it.release = it.next()
	it.primed = true
}
