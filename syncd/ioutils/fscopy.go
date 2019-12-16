package ioutils

import (
	"context"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"
)

const (
	tmpPermissionForDirectory = os.FileMode(0644)
)

func AtomicDirCopy(ctx context.Context, src, dest string) error {
	var (
		info       os.FileInfo
		newTempDir string
		err        error
	)
	defer func() {
		if err != nil && newTempDir != "" {
			os.RemoveAll(newTempDir)
		}
	}()
	if info, err = os.Lstat(src); err != nil {
		return err
	}
	if _, err = os.Stat(dest); err == nil {
		// already exists
		return nil
	} else if os.IsNotExist(err) {
		err = nil
	} else {
		return err
	}
	newTempDir = filepath.Join(filepath.Dir(dest), ".tmp-"+filepath.Base(dest))
	if err = copy(ctx, src, newTempDir, info); err != nil {
		return err
	}

	return os.Rename(newTempDir, dest)
}

func copy(ctx context.Context, src, dest string, info os.FileInfo) error {
	fileMode := info.Mode()
	switch {
	case fileMode&os.ModeSymlink != 0:
		return copyLink(src, dest, info)
	case fileMode&os.ModeDir != 0:
		return copyDir(ctx, src, dest, info)
	case fileMode&os.ModeDevice != 0:
		return copyDevice(src, dest, info)
	case fileMode&os.ModeNamedPipe != 0:
		return copyNamedPipe(src, dest, info)
	case fileMode&os.ModeSocket != 0:
		// do nothing about socket
		return nil
	default:
		return copyRegularFile(src, dest, info)
	}
}

func copyNamedPipe(src, dest string, info os.FileInfo) error {
	if err := os.MkdirAll(filepath.Dir(dest), os.ModePerm); err != nil {
		return err
	}
	if err := syscall.Mkfifo(dest, 0644); err != nil && !os.IsExist(err) {
		return err
	}
	return os.Chmod(dest, info.Mode())
}

func copyDevice(src, dest string, info os.FileInfo) error {
	if err := os.MkdirAll(filepath.Dir(dest), os.ModePerm); err != nil {
		return err
	}
	stat := info.Sys().(*syscall.Stat_t)
	dev := stat.Rdev
	var err error
	if info.Mode()&os.ModeCharDevice != 0 {
		err = syscall.Mknod(dest, syscall.S_IFCHR|0644, int(dev))
	} else {
		err = syscall.Mknod(dest, syscall.S_IFBLK|0644, int(dev))
	}
	if os.IsExist(err) {
		err = nil
	}
	return os.Chmod(dest, info.Mode())
}

func copyRegularFile(src, dest string, info os.FileInfo) error {
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

func copyDir(ctx context.Context, srcdir, destdir string, info os.FileInfo) error {

	originalMode := info.Mode()

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

func copyLink(src, dest string, info os.FileInfo) error {
	src, err := os.Readlink(src)
	if err != nil {
		return err
	}
	if err = os.Symlink(src, dest); os.IsExist(err) {
		err = nil
	}
	return err
}
