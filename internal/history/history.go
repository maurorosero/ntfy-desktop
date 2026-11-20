// SPDX-License-Identifier: GPL-3.0-or-later

// Package history keeps the messages that arrived while nobody was looking.
//
// The store is one append-only file of JSON Lines per topic under the user
// cache directory: every line is a whole entry, so storing a message never
// rewrites what is already there and a crash can cost the last line at most.
// Marking a message read appends the entry again with Read set; the reader
// folds the file keeping the last copy of every ID, which also makes a
// re-delivered message harmless — the client asks the server for everything
// after the last ID on every reconnect, so seeing one twice is normal.
//
// The file never grows without bound: when it holds more records than the
// limit plus a quarter, the store rewrites it with the entries it keeps, which
// is at most the limit.
//
// A file that is not a history is not a reason to refuse to start: the store
// reports it as a warning, starts that topic empty and replaces the file on the
// next write.
package history

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/maurorosero/ntfy-desktop/internal/ntfy"
)

const (
	// DefaultLimit is how many messages per topic are kept when the caller does
	// not say otherwise.
	DefaultLimit = 500

	// dirName is the product's directory inside os.UserCacheDir(), and subdir
	// is where the topics live: a cache clean-up deletes the history and the
	// program rebuilds it from the server.
	dirName = "ntfy-desktop"
	subdir  = "history"

	// fileSuffix names one topic file. The topic is the rest of the name.
	fileSuffix = ".jsonl"

	// The entries are what other people sent, so only the owner reads them:
	// 0700 for the directory, 0600 for the files.
	dirMode  os.FileMode = 0o700
	fileMode os.FileMode = 0o600
)

// topicPattern is the name a topic can have, which is also the file name it
// gets. A topic that does not match is refused instead of being turned into a
// path: it comes from the network, not from the user.
var topicPattern = regexp.MustCompile(`^[-_A-Za-z0-9]{1,64}$`)

// Entry is one stored message. The body is kept whole even though the listing
// only prints its first line: the rest is what reading it later is about.
type Entry struct {
	ID       string    `json:"id"`
	Topic    string    `json:"topic"`
	Time     time.Time `json:"time"`
	Title    string    `json:"title"`
	Body     string    `json:"body"`
	Click    string    `json:"click,omitempty"`
	Priority int       `json:"priority,omitempty"`
	Read     bool      `json:"read,omitempty"`
}

// CorruptionError reports a file that cannot be read as a history. The store
// already started that topic empty, so the caller only has to say so.
type CorruptionError struct {
	Path string
	Line int
	Err  error
}

func (e *CorruptionError) Error() string {
	return fmt.Sprintf("%s:%d: not a history file: %v", e.Path, e.Line, e.Err)
}

// Unwrap exposes the parse error, so callers can inspect it with errors.As.
func (e *CorruptionError) Unwrap() error { return e.Err }

// Filter narrows a listing.
type Filter struct {
	Topic  string // only this topic; empty is every topic
	Unread bool   // only what has not been read
	Limit  int    // at most this many, newest first; zero is everything
}

// Store is the history of one cache directory. It is safe for concurrent use:
// the daemon writes while a `history` command may be reading another process's
// view of the same files.
type Store struct {
	dir   string
	limit int

	mu     sync.Mutex
	topics map[string]*topicFile
	warn   []error
}

// topicFile is what the store knows about one topic: the entries it keeps,
// oldest first and one per ID, and how many records the file holds, which is
// what tells the store when to compact.
type topicFile struct {
	path    string
	entries []Entry
	records int
	corrupt bool // the file could not be read, so it is replaced on the next write
}

// Open prepares the store under dir, creating it when it is missing, and reads
// every topic file it finds. limit is how many messages of each topic are kept;
// zero or less means DefaultLimit.
//
// A file that cannot be parsed is not an error: Open reports it through
// Warnings and starts that topic empty. Only a directory that cannot be created
// or read fails.
func Open(dir string, limit int) (*Store, error) {
	if limit <= 0 {
		limit = DefaultLimit
	}
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return nil, fmt.Errorf("creating %s: %w", dir, err)
	}

	s := &Store{dir: dir, limit: limit, topics: make(map[string]*topicFile)}
	names, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", dir, err)
	}
	for _, name := range names {
		if name.IsDir() || !strings.HasSuffix(name.Name(), fileSuffix) {
			continue
		}
		topic := strings.TrimSuffix(name.Name(), fileSuffix)
		if _, err := s.load(topic); err != nil {
			s.warn = append(s.warn, err)
		}
	}
	return s, nil
}

// DefaultDir is where the history of the current user lives:
// os.UserCacheDir()/ntfy-desktop/history, so XDG_CACHE_HOME is honoured.
func DefaultDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("locating the user cache directory: %w", err)
	}
	return filepath.Join(dir, dirName, subdir), nil
}

// Warnings returns what Open could not use, one error per file. The store
// already started those topics empty; the caller decides how to say it.
func (s *Store) Warnings() []error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]error(nil), s.warn...)
}

// Add stores m, replacing the copy of the same ID when it is already there.
// Old messages beyond the limit are dropped.
func (s *Store) Add(m ntfy.Message) error {
	if !topicPattern.MatchString(m.Topic) {
		return fmt.Errorf("%q is not a topic name", m.Topic)
	}
	if m.ID == "" {
		return errors.New("a message without an id cannot be stored")
	}
	return s.store(m.Topic, entryFrom(m))
}

// MarkRead marks the message id of topic as read. Asking for one that is not
// stored is not an error: the history is a cache and the limit may have dropped
// it already.
func (s *Store) MarkRead(topic, id string) error {
	if !topicPattern.MatchString(topic) {
		return fmt.Errorf("%q is not a topic name", topic)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tf, err := s.load(topic)
	if err != nil {
		return err
	}
	for i := range tf.entries {
		if tf.entries[i].ID != id {
			continue
		}
		if tf.entries[i].Read {
			return nil
		}
		read := tf.entries[i]
		read.Read = true
		return s.storeLocked(tf, read)
	}
	return nil
}

// List returns the stored messages, newest first.
func (s *Store) List(f Filter) []Entry {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []Entry
	for topic, tf := range s.topics {
		if f.Topic != "" && topic != f.Topic {
			continue
		}
		for _, e := range tf.entries {
			if f.Unread && e.Read {
				continue
			}
			out = append(out, e)
		}
	}
	// Newest first, and by ID when two messages share a second: the map the
	// entries come from has no order, so a tie needs a rule of its own.
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].Time.Equal(out[j].Time) {
			return out[i].Time.After(out[j].Time)
		}
		return out[i].ID > out[j].ID
	})
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out
}

// Latest returns the newest stored message of topic. Its ID is what a
// subscription can resume from; false means the topic has nothing stored.
func (s *Store) Latest(topic string) (Entry, bool) {
	entries := s.List(Filter{Topic: topic, Limit: 1})
	if len(entries) == 0 {
		return Entry{}, false
	}
	return entries[0], true
}

// Compact rewrites the topic file with the entries the store keeps, so the
// records that are no longer needed stop taking space. Add and MarkRead call it
// on their own once the file grows past the limit plus a quarter.
func (s *Store) Compact(topic string) error {
	if !topicPattern.MatchString(topic) {
		return fmt.Errorf("%q is not a topic name", topic)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	tf, err := s.load(topic)
	if err != nil {
		return err
	}
	return s.rewrite(tf)
}

// store adds e to the topic it belongs to.
func (s *Store) store(topic string, e Entry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tf, err := s.load(topic)
	if err != nil {
		return err
	}
	return s.storeLocked(tf, e)
}

// storeLocked writes e and keeps the memory in step with it. The caller holds
// the lock.
func (s *Store) storeLocked(tf *topicFile, e Entry) error {
	// A file that could not be read is not appended to: it is replaced by what
	// the store has, so the next start does not find the same garbage again.
	if tf.corrupt {
		tf.entries = trim(fold(tf.entries, e), s.limit)
		return s.rewrite(tf)
	}

	if err := s.append(tf, e); err != nil {
		return err
	}
	tf.records++
	tf.entries = trim(fold(tf.entries, e), s.limit)
	if tf.records > s.compactAt() {
		return s.rewrite(tf)
	}
	return nil
}

// load reads the topic file once and remembers it. A corrupt file yields an
// empty, usable topic and the error to report.
func (s *Store) load(topic string) (*topicFile, error) {
	if tf, ok := s.topics[topic]; ok {
		return tf, nil
	}
	tf := &topicFile{path: filepath.Join(s.dir, topic+fileSuffix)}
	s.topics[topic] = tf

	data, err := os.ReadFile(tf.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return tf, nil
		}
		return tf, fmt.Errorf("reading %s: %w", tf.path, err)
	}

	entries, records, err := parseLog(data)
	if err != nil {
		var corrupt *CorruptionError
		if errors.As(err, &corrupt) {
			corrupt.Path, tf.corrupt = tf.path, true
		}
		return tf, err
	}
	tf.entries, tf.records = entries, records
	return tf, nil
}

// append writes one record at the end of the file.
func (s *Store) append(tf *topicFile, e Entry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("encoding the entry %s: %w", e.ID, err)
	}
	line = append(line, '\n')

	f, err := os.OpenFile(tf.path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, fileMode)
	if err != nil {
		return fmt.Errorf("opening %s: %w", tf.path, err)
	}
	defer f.Close()

	n, err := f.Write(line)
	if err != nil {
		return fmt.Errorf("appending to %s: %w", tf.path, err)
	}
	if n != len(line) {
		return fmt.Errorf("appending to %s: wrote %d of %d bytes", tf.path, n, len(line))
	}
	return nil
}

// rewrite replaces the topic file with the entries it keeps. The new content
// goes to a temporary file that is renamed over the old one, so a reader never
// sees half of it and a failed write leaves the previous file untouched.
func (s *Store) rewrite(tf *topicFile) error {
	var buf bytes.Buffer
	for _, e := range tf.entries {
		line, err := json.Marshal(e)
		if err != nil {
			return fmt.Errorf("encoding the entry %s: %w", e.ID, err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}

	// CreateTemp opens the file with 0600, the mode the history needs.
	f, err := os.CreateTemp(filepath.Dir(tf.path), filepath.Base(tf.path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating a temporary file for %s: %w", tf.path, err)
	}
	name := f.Name()
	// The rename below takes the file away; when something fails before it,
	// this removes the leftover. Failing to clean a temporary file in a cache
	// directory is not worth hiding the error that got here.
	defer func() { _ = os.Remove(name) }()

	if _, err := f.Write(buf.Bytes()); err != nil {
		f.Close()
		return fmt.Errorf("writing %s: %w", name, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("closing %s: %w", name, err)
	}
	if err := os.Rename(name, tf.path); err != nil {
		return fmt.Errorf("replacing %s: %w", tf.path, err)
	}
	tf.records, tf.corrupt = len(tf.entries), false
	return nil
}

// compactAt is the number of records a file may hold before it is rewritten:
// the limit, plus a quarter so the rewrite is amortised instead of happening
// on every message once the limit is reached.
func (s *Store) compactAt() int {
	slack := s.limit / 4
	if slack == 0 {
		slack = 1
	}
	return s.limit + slack
}

// parseLog reads the records of one topic file. A last line without its
// newline is a write that was cut short: the entries before it are kept, since
// one message lost to a crash is not a reason to drop the whole history.
func parseLog(data []byte) (entries []Entry, records int, err error) {
	text := string(data)
	complete := strings.HasSuffix(text, "\n")
	lines := strings.Split(text, "\n")

	for i, line := range lines {
		if line == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			if i == len(lines)-1 && !complete {
				return entries, records, nil
			}
			return nil, 0, &CorruptionError{Line: i + 1, Err: err}
		}
		if e.ID == "" {
			return nil, 0, &CorruptionError{Line: i + 1, Err: errors.New("entry without an id")}
		}
		entries = fold(entries, e)
		records++
	}
	return entries, records, nil
}

// fold puts e in its place: a record for an ID that is already there replaces
// it and keeps the position the message arrived in.
func fold(entries []Entry, e Entry) []Entry {
	for i := range entries {
		if entries[i].ID == e.ID {
			entries[i] = e
			return entries
		}
	}
	return append(entries, e)
}

// trim drops the oldest entries beyond limit.
func trim(entries []Entry, limit int) []Entry {
	if len(entries) <= limit {
		return entries
	}
	return entries[len(entries)-limit:]
}

// entryFrom is the message as it is stored. The time comes from the server in
// Unix seconds and is kept in UTC, so the file reads the same everywhere.
func entryFrom(m ntfy.Message) Entry {
	return Entry{
		ID:       m.ID,
		Topic:    m.Topic,
		Time:     time.Unix(m.Time, 0).UTC(),
		Title:    m.Title,
		Body:     m.Message,
		Click:    m.Click,
		Priority: m.Priority,
	}
}
