package ioutils

import (
	"context"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
)

const (
	tmpPermissionForDirectory = os.FileMode(0644)
)

func AtomicDirCopy(ctx context.Context, src, dest string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	newTempDir := filepath.Join(filepath.Dir(dest), ".tmp-"+filepath.Base(dest))
	err = copy(ctx, src, dest, info)
	if err != nil {
		os.RemoveAll(newTempDir)
		return err
	}
	return os.Rename(newTempDir, dest)
}

// copy dispatches copy-funcs according to the mode.
// Because this "copy" could be called recursively,
// "info" MUST be given here, NOT nil.
func copy(ctx context.Context, src, dest string, info os.FileInfo) error {
	if info.IsDir() {
		return dcopy(ctx, src, dest, info)
	}
	return fcopy(src, dest, info)
}

// fcopy is for just a file,
// with considering existence of parent directory
// and file permission.
func fcopy(src, dest string, info os.FileInfo) error {

	if err := os.MkdirAll(filepath.Dir(dest), os.ModePerm); err != nil {
		return err
	}

	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer f.Close()

	if err = os.Chmod(f.Name(), info.Mode()); err != nil {
		return err
	}

	s, err := os.Open(src)
	if err != nil {
		return err
	}
	defer s.Close()

	_, err = io.Copy(f, s)
	return err
}

// dcopy is for a directory,
// with scanning contents inside the directory
// and pass everything to "copy" recursively.
func dcopy(ctx context.Context, srcdir, destdir string, info os.FileInfo) error {

	originalMode := info.Mode()

	// Make dest dir with 0755 so that everything writable.
	if err := os.MkdirAll(destdir, tmpPermissionForDirectory); err != nil {
		return err
	}
	// Recover dir mode with original one.
	defer os.Chmod(destdir, originalMode)

	contents, err := ioutil.ReadDir(srcdir)
	if err != nil {
		return err
	}

	for _, content := range contents {
		cs, cd := filepath.Join(srcdir, content.Name()), filepath.Join(destdir, content.Name())
		if err := copy(ctx, cs, cd, content); err != nil {
			// If any error, exit immediately
			return err
		}
		select {
		case <-ctx.Done():
			return errors.New("context canceled")
		default:
		}
	}

	return nil
}
