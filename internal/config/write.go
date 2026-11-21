// SPDX-License-Identifier: GPL-3.0-or-later

package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Write validates the document with the loader and writes it to path
// atomically: the text goes to a temporary file in the same directory, is
// validated again through the loader reading that file, and is renamed over
// path. A failure leaves the file exactly as it was, so the configuration on
// disk is always one whole version and never half of two.
//
// The previous version is copied to path+".bak" and the file is left with mode
// 0600, because it may hold a password. The backup path is returned, empty
// when there was no file yet.
func (d *Document) Write(path string) (string, error) {
	if _, err := d.Check(); err != nil {
		return "", err
	}
	return writeFile(path, d.Bytes(), os.Rename)
}

// writeFile is Write with the rename as a parameter: the tests replace it to
// simulate a failure halfway without a package variable behind.
func writeFile(path string, data []byte, rename func(string, string) error) (string, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("creating %s: %w", dir, err)
	}

	temp, err := writeTemp(dir, filepath.Base(path), data)
	if err != nil {
		return "", err
	}

	// The candidate is checked by the loader over the file that was just
	// written on disk, not over the bytes in memory: what parses here is what
	// is going to be renamed.
	if _, err := Load(temp); err != nil {
		return "", errors.Join(fmt.Errorf("the new configuration is not usable: %w", err), removeFile(temp))
	}

	backup, err := backup(path)
	if err != nil {
		return "", errors.Join(err, removeFile(temp))
	}
	if err := rename(temp, path); err != nil {
		return "", errors.Join(fmt.Errorf("replacing %s: %w", path, err), removeFile(temp))
	}
	return backup, nil
}

// writeTemp writes data to a new file with mode 0600 next to the configuration
// and returns its name. Writing beside the target keeps the rename in the same
// file system, which is what makes it atomic.
func writeTemp(dir, base string, data []byte) (string, error) {
	f, err := os.CreateTemp(dir, "."+base+".tmp-*")
	if err != nil {
		return "", fmt.Errorf("creating a temporary file in %s: %w", dir, err)
	}
	name := f.Name()

	if err := f.Chmod(0o600); err != nil {
		return "", errors.Join(fmt.Errorf("setting 0600 on %s: %w", name, err), f.Close(), removeFile(name))
	}
	if _, err := f.Write(data); err != nil {
		return "", errors.Join(fmt.Errorf("writing %s: %w", name, err), f.Close(), removeFile(name))
	}
	if err := f.Sync(); err != nil {
		return "", errors.Join(fmt.Errorf("syncing %s: %w", name, err), f.Close(), removeFile(name))
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("closing %s: %w", name, err)
	}
	return name, nil
}

// backup copies the file at path to path+".bak" before it is replaced. A file
// that does not exist yet has nothing to keep, so it returns "".
func backup(path string) (string, error) {
	previous, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", path, err)
	}

	target := path + ".bak"
	temp, err := writeTemp(filepath.Dir(target), filepath.Base(target), previous)
	if err != nil {
		return "", err
	}
	if err := os.Rename(temp, target); err != nil {
		return "", errors.Join(fmt.Errorf("saving %s: %w", target, err), removeFile(temp))
	}
	return target, nil
}

// removeFile removes a file the operation created and no longer needs. A file
// that is not there is not a problem; any other failure is.
func removeFile(name string) error {
	if err := os.Remove(name); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("removing %s: %w", name, err)
	}
	return nil
}
