// Modified for dircue: close loose object files when reader initialization fails.

package dotgit

import (
	"fmt"
	"io"
	"os"

	billy "github.com/go-git/go-billy/v5"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/format/objfile"
	"github.com/go-git/go-git/v5/utils/ioutil"
)

var _ (plumbing.EncodedObject) = &EncodedObject{}

type EncodedObject struct {
	dir          *DotGit
	h            plumbing.Hash
	t            plumbing.ObjectType
	sz           int64
	readObserver func(int)
}

func (e *EncodedObject) Hash() plumbing.Hash {
	return e.h
}

func (e *EncodedObject) Reader() (io.ReadCloser, error) {
	f, err := e.dir.Object(e.h)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, plumbing.ErrObjectNotFound
		}

		return nil, err
	}
	if e.readObserver != nil {
		f = &observedReadFile{File: f, observe: e.readObserver}
	}
	r, err := objfile.NewReader(f)
	if err != nil {
		_ = f.Close()
		return nil, err
	}

	t, size, err := r.Header()
	if err != nil {
		_ = r.Close()
		_ = f.Close()
		return nil, err
	}
	if t != e.t {
		_ = r.Close()
		_ = f.Close()
		return nil, objfile.ErrHeader
	}
	if size != e.sz {
		_ = r.Close()
		_ = f.Close()
		return nil, objfile.ErrHeader
	}
	return ioutil.NewReadCloserWithCloser(r, f.Close), nil
}

func (e *EncodedObject) SetType(plumbing.ObjectType) {}

func (e *EncodedObject) Type() plumbing.ObjectType {
	return e.t
}

func (e *EncodedObject) Size() int64 {
	return e.sz
}

func (e *EncodedObject) SetSize(int64) {}

func (e *EncodedObject) Writer() (io.WriteCloser, error) {
	return nil, fmt.Errorf("not supported")
}

func NewEncodedObject(dir *DotGit, h plumbing.Hash, t plumbing.ObjectType, size int64) *EncodedObject {
	return NewEncodedObjectWithReadObserver(dir, h, t, size, nil)
}

// NewEncodedObjectWithReadObserver creates a lazy loose object whose physical
// file reads are reported to observe. A nil observer preserves the default path.
func NewEncodedObjectWithReadObserver(dir *DotGit, h plumbing.Hash, t plumbing.ObjectType, size int64, observe func(int)) *EncodedObject {
	return &EncodedObject{
		dir:          dir,
		h:            h,
		t:            t,
		sz:           size,
		readObserver: observe,
	}
}

type observedReadFile struct {
	billy.File
	observe func(int)
}

func (f *observedReadFile) Read(p []byte) (int, error) {
	n, err := f.File.Read(p)
	f.observe(n)
	return n, err
}

func (f *observedReadFile) ReadAt(p []byte, offset int64) (int, error) {
	n, err := f.File.ReadAt(p, offset)
	f.observe(n)
	return n, err
}
