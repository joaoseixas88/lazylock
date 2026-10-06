// Package export renders secrets as the files LazyLock writes out.
package export

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"regexp"
	"slices"
	"strings"

	"github.com/joaoseixas88/lazylock/internal/domain"
)

type Format uint8

const (
	Dotenv Format = iota
	JSON
	Example
)

var Formats = []Format{Dotenv, JSON, Example}

func (f Format) String() string {
	switch f {
	case JSON:
		return "JSON"
	case Example:
		return ".env.example"
	}
	return ".env"
}

func (f Format) FileName(envSlug string) string {
	switch f {
	case JSON:
		return envSlug + ".json"
	case Example:
		return ".env.example"
	}
	return envSlug + ".env"
}

func (f Format) Perm() fs.FileMode {
	if f == Example {
		return 0o644
	}
	return 0o600
}

func (f Format) CarriesValues() bool { return f != Example }

type Rendered struct {
	Data    []byte
	Count   int
	LeftOut []string
}

// Render writes secrets sorted by key, ignoring case, the way the Infisical web
// download does. Keys a .env file cannot hold are left out, and so are values
// the session cannot read, unless the format carries no values.
func Render(f Format, secrets []domain.Secret) Rendered {
	var r Rendered
	kept := make([]domain.Secret, 0, len(secrets))
	for _, s := range secrets {
		if !validKey(s.Key) || (s.Hidden && f.CarriesValues()) {
			r.LeftOut = append(r.LeftOut, s.Key)
			continue
		}
		kept = append(kept, s)
	}
	slices.SortStableFunc(kept, func(a, b domain.Secret) int {
		return strings.Compare(strings.ToLower(a.Key), strings.ToLower(b.Key))
	})
	r.Count = len(kept)
	if f == JSON {
		r.Data = renderJSON(kept)
	} else {
		r.Data = renderDotenv(kept, f.CarriesValues())
	}
	return r
}

// Line is one secret as a .env line, without its comment.
func Line(s domain.Secret) (string, bool) {
	if s.Hidden || !validKey(s.Key) {
		return "", false
	}
	return s.Key + "=" + quote(s.Value), true
}

var keyPattern = regexp.MustCompile(`^[^\n\r=]+$`)

func validKey(key string) bool { return keyPattern.MatchString(key) }

var escaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`)

// quote follows Infisical's backend dotenv writer, so a file LazyLock writes
// reads back the same through Infisical's own parser.
func quote(value string) string {
	if !strings.ContainsAny(value, "\n\r\"\\#=") && value == strings.TrimSpace(value) {
		return value
	}
	return `"` + escaper.Replace(value) + `"`
}

func renderDotenv(secrets []domain.Secret, values bool) []byte {
	var b strings.Builder
	for _, s := range secrets {
		for _, line := range commentLines(s.Comment) {
			b.WriteString(line)
			b.WriteByte('\n')
		}
		b.WriteString(s.Key)
		b.WriteByte('=')
		if values {
			b.WriteString(quote(s.Value))
		}
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

func commentLines(comment string) []string {
	if comment == "" {
		return nil
	}
	lines := strings.Split(comment, "\n")
	for i, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			lines[i] = "#"
		} else {
			lines[i] = "# " + line
		}
	}
	return lines
}

func renderJSON(secrets []domain.Secret) []byte {
	var b bytes.Buffer
	b.WriteByte('{')
	for i, s := range secrets {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString("\n  ")
		b.Write(jsonString(s.Key))
		b.WriteString(": ")
		b.Write(jsonString(s.Value))
	}
	if len(secrets) > 0 {
		b.WriteByte('\n')
	}
	b.WriteString("}\n")
	return b.Bytes()
}

func jsonString(s string) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}
