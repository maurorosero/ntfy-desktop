// SPDX-License-Identifier: GPL-3.0-or-later

// Package config loads and validates the configuration of ntfy-desktop.
//
// The configuration is one TOML file inside the user config directory:
// os.UserConfigDir()/ntfy-desktop/config.toml, so XDG_CONFIG_HOME is honoured
// and the product never hardcodes ~/.config.
//
// Only the part of TOML this file needs is supported: comments, blank lines and
// `key = "value"` assignments. Anything else is rejected with its line number,
// so a typo in a key name is never silently ignored.
//
// The package never renders text: its errors carry a gettext msgid in Msg (see
// ValidationError and SyntaxError) and whoever shows them passes that msgid
// through tr.T, which is why every one of them exists in the catalogues under
// locales/.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Location of the configuration inside os.UserConfigDir().
const (
	dirName  = "ntfy-desktop"
	fileName = "config.toml"
)

// Keys of the configuration file.
const (
	keyServer   = "server"
	keyTopic    = "topic"
	keyUser     = "user"
	keyPassword = "password"
)

// User-facing messages. They are gettext msgids in English; the wiring layer
// renders them with tr.T, so they have to stay in sync with locales/*.po.
const (
	msgRequired      = "must not be empty"
	msgURL           = "must be an http or https URL"
	msgNoQuery       = "must not have a query or a fragment"
	msgTopic         = "must be 1 to 64 characters of letters, digits, dashes or underscores"
	msgNoColon       = "must not contain a colon"
	msgNoControl     = "must not contain control characters"
	msgNeedsUser     = "requires a user name"
	msgNeedsPassword = "requires a password"
)

// topics follow the ntfy rules: up to 64 characters, letters, digits, dashes
// and underscores.
var topicPattern = regexp.MustCompile(`^[-_A-Za-z0-9]{1,64}$`)

// AuthMode is how the client authenticates against the ntfy server.
type AuthMode int

const (
	// AuthNone subscribes to a public topic without credentials.
	AuthNone AuthMode = iota
	// AuthBasic sends the configured user and password as HTTP Basic.
	AuthBasic
)

// String names the authentication mode, for logs and diagnostics.
func (a AuthMode) String() string {
	switch a {
	case AuthNone:
		return "none"
	case AuthBasic:
		return "basic"
	default:
		return "unknown"
	}
}

// Config is the validated configuration of one subscription.
type Config struct {
	ServerURL string // base URL of the server, without the trailing slash
	Topic     string
	User      string
	Password  string
}

// AuthMode reports how the client has to authenticate: HTTP Basic when the
// configuration carries a user and a password, anonymous otherwise.
func (c Config) AuthMode() AuthMode {
	if c.User != "" && c.Password != "" {
		return AuthBasic
	}
	return AuthNone
}

// String implements fmt.Stringer without the password, so a configuration can
// be logged or printed without leaking the secret.
func (c Config) String() string {
	return fmt.Sprintf("config{server: %s, topic: %s, user: %q, auth: %s}",
		c.ServerURL, c.Topic, c.User, c.AuthMode())
}

// DefaultPath returns the configuration file of the current user.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("locating the user config directory: %w", err)
	}
	return filepath.Join(dir, dirName, fileName), nil
}

// Load reads, parses and validates the configuration file at path. A missing
// file keeps fs.ErrNotExist wrapped, so the caller can tell it apart and
// explain where the file goes.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	cfg, err := parse(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return cfg, nil
}

// SyntaxError reports a line that is not part of the supported TOML subset.
// Msg is a gettext msgid; Key is set when the offending key is known.
type SyntaxError struct {
	Line int
	Key  string
	Msg  string
}

func (e *SyntaxError) Error() string {
	if e.Key == "" {
		return fmt.Sprintf("line %d: %s", e.Line, e.Msg)
	}
	return fmt.Sprintf("line %d: %s %q", e.Line, e.Msg, e.Key)
}

// ValidationError reports a value that parsed fine but cannot be used. Msg is a
// gettext msgid and Field is the key it came from.
type ValidationError struct {
	Field string
	Msg   string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("%s: %s", e.Field, e.Msg)
}

// validate checks the values and normalises the server URL.
func (c *Config) validate() error {
	c.ServerURL = strings.TrimRight(c.ServerURL, "/")
	if c.ServerURL == "" {
		return &ValidationError{Field: keyServer, Msg: msgRequired}
	}
	u, err := url.Parse(c.ServerURL)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return &ValidationError{Field: keyServer, Msg: msgURL}
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return &ValidationError{Field: keyServer, Msg: msgNoQuery}
	}

	if c.Topic == "" {
		return &ValidationError{Field: keyTopic, Msg: msgRequired}
	}
	if !topicPattern.MatchString(c.Topic) {
		return &ValidationError{Field: keyTopic, Msg: msgTopic}
	}

	if strings.ContainsRune(c.User, ':') {
		return &ValidationError{Field: keyUser, Msg: msgNoColon}
	}
	if hasControl(c.User) {
		return &ValidationError{Field: keyUser, Msg: msgNoControl}
	}
	if hasControl(c.Password) {
		return &ValidationError{Field: keyPassword, Msg: msgNoControl}
	}

	// Half a pair is a mistake, not an intent: say which key is missing instead
	// of quietly subscribing without credentials.
	switch {
	case c.User == "" && c.Password == "":
	case c.User == "":
		return &ValidationError{Field: keyUser, Msg: msgNeedsUser}
	case c.Password == "":
		return &ValidationError{Field: keyPassword, Msg: msgNeedsPassword}
	}
	return nil
}

// hasControl reports whether s carries a control character, which no header
// field and no TOML string of this file should contain.
func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}
