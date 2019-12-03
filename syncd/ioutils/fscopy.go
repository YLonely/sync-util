package ioutils

import (
	"context"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
)

const (
	tmpPermissionForDirectory = os.FileMode(0644)
)

// Copy copies src to dest, doesn't matter if src is a directory or a file
func Copy(ctx context.Context, src, dest string) error {
	defer func() {
		select {
		case <-ctx.Done():
			os.RemoveAll(dest)
		default:
		}
	}()
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	return copy(ctx, src, dest, info)
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
			return nil
		default:
		}
	}

	return nil
}
