package lockfile

import (
	"errors"
	"os"
	"sync"

	"golang.org/x/sys/unix"
)

var mtx sync.Mutex
var locks map[key]struct{}

func init() {
	locks = make(map[key]struct{})
}

type key struct {
	dev uint64
	ino uint64
}

type Lockfile struct {
	f *os.File
	key
}

func Lock(path string) (*Lockfile, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}
	defer func() {
		if f != nil {
			f.Close()
		}
	}()
	var stbuf unix.Stat_t
	err = unix.Fstat(int(f.Fd()), &stbuf)
	if err != nil {
		return nil, err
	}
	k := key{
		dev: stbuf.Dev,
		ino: stbuf.Ino,
	}
	ft := unix.Flock_t{
		Type:   unix.F_WRLCK,
		Whence: int16(os.SEEK_SET),
	}
	err = unix.FcntlFlock(uintptr(f.Fd()), unix.F_SETLK, &ft)
	if err != nil {
		return nil, err
	}
	mtx.Lock()
	existed := false
	if _, ok := locks[k]; ok {
		existed = true
	}
	locks[k] = struct{}{}
	mtx.Unlock()
	if existed {
		return nil, errors.New("this process has already tried to acquire the lock")
	}
	L := &Lockfile{
		f:   f,
		key: k,
	}
	f = nil
	return L, nil
}

func (L *Lockfile) Unlock() error {
	if L.f == nil {
		return nil
	}
	mtx.Lock()
	delete(locks, L.key)
	mtx.Unlock()
	return L.f.Close()
}
