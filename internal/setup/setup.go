// SPDX-License-Identifier: GPL-3.0-or-later

// Package setup applies changes to the configuration file.
//
// It reads the file, builds a candidate in memory, validates it with the
// loader of internal/config and writes it atomically, keeping a .bak of the
// previous version. It knows nothing about a user interface or the network,
// so the text front end and the desktop one share the same engine: a caller
// that has to show the result before touching the disk asks for Text and only
// then for Write.
package setup

import (
	"fmt"

	"github.com/maurorosero/ntfy-desktop/internal/config"
)

// Draft is a configuration being edited: the file at path is loaded once and
// every change is applied to its text, line by line, so comments and the order
// of the untouched keys survive.
type Draft struct {
	path string
	doc  *config.Document
}

// Open reads the configuration at path into a draft. A file the loader does
// not accept is refused: an invalid file is never the base of an edit, so it
// is never replaced either.
func Open(path string) (*Draft, error) {
	doc, err := config.LoadDocument(path)
	if err != nil {
		return nil, err
	}
	return &Draft{path: path, doc: doc}, nil
}

// New starts a draft for a file that does not exist yet, with the keys the
// caller sets and no previous version to keep.
func New(path string) *Draft {
	return &Draft{path: path, doc: config.NewDocument(nil)}
}

// Path returns the file the draft belongs to.
func (d *Draft) Path() string { return d.path }

// Text returns the candidate as it would be written, so a front end can show
// it and ask before anything touches the disk.
func (d *Draft) Text() []byte { return d.doc.Bytes() }

// Masked returns the candidate as it would be written with the password
// replaced by placeholder: what a front end shows of a file that holds a
// secret.
func (d *Draft) Masked(placeholder string) []byte { return d.doc.Masked(placeholder) }

// Config returns the configuration the draft holds, validated by the loader.
func (d *Draft) Config() (*config.Config, error) { return d.doc.Check() }

// Set changes the value of key, or appends it when the file does not have it.
func (d *Draft) Set(key, value string) error { return d.doc.Set(key, value) }

// AddTopic adds a topic to the list, after the ones the file already has.
func (d *Draft) AddTopic(name string) error { return d.doc.AddTopic(name) }

// RemoveTopic removes a topic from the list, keeping the rest in order.
func (d *Draft) RemoveTopic(name string) error { return d.doc.RemoveTopic(name) }

// Result is what Write left on disk.
type Result struct {
	Path   string
	Backup string // previous version, "" when the file did not exist
	Config *config.Config
}

// Write validates the draft and writes it atomically. Nothing is written when
// the candidate does not load, so a failed edit leaves the file untouched.
func (d *Draft) Write() (Result, error) {
	cfg, err := d.doc.Check()
	if err != nil {
		return Result{}, fmt.Errorf("%s: %w", d.path, err)
	}
	backup, err := d.doc.Write(d.path)
	if err != nil {
		return Result{}, err
	}
	return Result{Path: d.path, Backup: backup, Config: cfg}, nil
}
