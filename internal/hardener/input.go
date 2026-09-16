package hardener

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Inputs keeps file reads beneath one declared root.
// Input files and their parent directories must not be concurrently modified.
type Inputs struct{ root *os.Root }

func OpenInputs(directory string) (*Inputs, error) {
	if runtime.GOOS != "windows" && runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		return nil, errors.New("input confinement is supported only on Windows, Linux, and macOS")
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return nil, errors.New("invalid input root")
	}
	if err := rejectRootLinks(absolute); err != nil {
		return nil, err
	}
	r, err := os.OpenRoot(absolute)
	if err != nil {
		return nil, errors.New("cannot open input root")
	}
	return &Inputs{root: r}, nil
}

func rejectRootLinks(absolute string) error {
	for current := absolute; ; current = filepath.Dir(current) {
		info, err := os.Lstat(current)
		if err != nil {
			return errors.New("input root or parent is inaccessible")
		}
		if !info.IsDir() || linked(info) {
			return errors.New("input root and parents must be ordinary directories")
		}
		if filepath.Dir(current) == current {
			return nil
		}
	}
}

func (in *Inputs) Close() error { return in.root.Close() }

// Paths use portable slash-separated names, relative to the declared root.
func validPath(name string) bool {
	if name == "." || !fs.ValidPath(name) || strings.ContainsAny(name, "\\:<>\"|?*") {
		return false
	}
	for _, r := range name {
		if r < 32 || r == 127 {
			return false
		}
	}
	for _, part := range strings.Split(name, "/") {
		if strings.TrimRight(part, " .") != part {
			return false
		}
		base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		switch base {
		case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
			return false
		}
		if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) &&
			base[3] >= '1' && base[3] <= '9' {
			return false
		}
		for _, prefix := range []string{"COM", "LPT"} {
			if strings.HasPrefix(base, prefix) {
				switch strings.TrimPrefix(base, prefix) {
				case "¹", "²", "³":
					return false
				}
			}
		}
	}
	return true
}

func (in *Inputs) check(name string) (os.FileInfo, error) {
	if !validPath(name) {
		return nil, errors.New("path must be a portable relative path without traversal")
	}
	parts := strings.Split(name, "/")
	var info os.FileInfo
	for i := range parts {
		var err error
		info, err = in.root.Lstat(strings.Join(parts[:i+1], "/"))
		if err != nil {
			return nil, errors.New("input path is inaccessible")
		}
		if linked(info) {
			return nil, errors.New("symlinks and reparse points are not accepted")
		}
		if i < len(parts)-1 && !info.IsDir() {
			return nil, errors.New("input parent is not a directory")
		}
	}
	return info, nil
}

func (in *Inputs) Read(name string, limit int64) ([]byte, error) {
	before, err := in.check(name)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("input must be a regular file")
	}
	if before.Size() > limit {
		return nil, fmt.Errorf("input exceeds %d bytes", limit)
	}
	f, err := in.root.Open(name)
	if err != nil {
		return nil, errors.New("cannot open input file")
	}
	defer f.Close()
	opened, err := f.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, errors.New("input changed while opening")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, errors.New("cannot read input file")
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("input exceeds %d bytes", limit)
	}
	after, err := in.check(name)
	if err != nil || !os.SameFile(opened, after) || after.Size() != opened.Size() ||
		!after.ModTime().Equal(opened.ModTime()) {
		return nil, errors.New("input changed while reading")
	}
	return data, nil
}
