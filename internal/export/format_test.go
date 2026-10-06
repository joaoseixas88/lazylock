package export

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/joaoseixas88/lazylock/internal/domain"
)

func TestDotenvQuotesExactlyWhatInfisicalQuotes(t *testing.T) {
	cases := []struct{ value, want string }{
		{"plain", `K=plain`},
		{"inner space", `K=inner space`},
		{"", `K=`},
		{" leading", `K=" leading"`},
		{"trailing\t", "K=\"trailing\t\""},
		{"has#hash", `K="has#hash"`},
		{"a=b", `K="a=b"`},
		{`say "hi"`, `K="say \"hi\""`},
		{`back\slash`, `K="back\\slash"`},
		{"line1\nline2", `K="line1\nline2"`},
		{"carriage\rreturn", `K="carriage\rreturn"`},
		{"$HOME stays", `K=$HOME stays`},
	}
	for _, c := range cases {
		got := string(Render(Dotenv, []domain.Secret{{Key: "K", Value: c.value}}).Data)
		if got != c.want+"\n" {
			t.Errorf("value %q rendered %q, want %q", c.value, got, c.want+"\n")
		}
	}
}

func TestDotenvWritesEachCommentLineAboveItsKey(t *testing.T) {
	got := string(Render(Dotenv, []domain.Secret{{Key: "K", Value: "v", Comment: "first\n\nthird\r"}}).Data)
	if want := "# first\n#\n# third\nK=v\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestDotenvSortsKeysIgnoringCase(t *testing.T) {
	got := string(Render(Dotenv, []domain.Secret{{Key: "b", Value: "2"}, {Key: "A", Value: "1"}, {Key: "c", Value: "3"}}).Data)
	if want := "A=1\nb=2\nc=3\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestHiddenSecretsAreLeftOutOfFormatsWithValues(t *testing.T) {
	secrets := []domain.Secret{{Key: "OPEN", Value: "v"}, {Key: "SIGNING_KEY", Hidden: true}}
	for _, f := range []Format{Dotenv, JSON} {
		r := Render(f, secrets)
		if r.Count != 1 || !slices.Equal(r.LeftOut, []string{"SIGNING_KEY"}) || strings.Contains(string(r.Data), "SIGNING_KEY") {
			t.Fatalf("%s: count=%d leftOut=%v data=%q", f, r.Count, r.LeftOut, r.Data)
		}
	}
}

func TestExampleKeepsEveryKeyAndNoValue(t *testing.T) {
	r := Render(Example, []domain.Secret{
		{Key: "DATABASE_URL", Value: "postgres://secret", Comment: "primary database"},
		{Key: "SIGNING_KEY", Hidden: true},
	})
	if want := "# primary database\nDATABASE_URL=\nSIGNING_KEY=\n"; string(r.Data) != want {
		t.Fatalf("got %q, want %q", r.Data, want)
	}
	if r.Count != 2 || len(r.LeftOut) != 0 {
		t.Fatalf("count=%d leftOut=%v", r.Count, r.LeftOut)
	}
}

func TestJSONIsAFlatObjectInDotenvOrder(t *testing.T) {
	r := Render(JSON, []domain.Secret{{Key: "b", Value: "<a&b>"}, {Key: "A", Value: "line1\nline2"}})
	var got map[string]string
	if err := json.Unmarshal(r.Data, &got); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, r.Data)
	}
	if got["A"] != "line1\nline2" || got["b"] != "<a&b>" {
		t.Fatalf("values changed on the way: %v", got)
	}
	data := string(r.Data)
	if strings.Index(data, `"A"`) > strings.Index(data, `"b"`) || !strings.Contains(data, "<a&b>") {
		t.Fatalf("keys must sort like the .env and HTML must not be escaped:\n%s", data)
	}
	if empty := string(Render(JSON, nil).Data); empty != "{}\n" {
		t.Fatalf("empty object = %q", empty)
	}
}

func TestKeysADotenvCannotHoldAreLeftOut(t *testing.T) {
	r := Render(Dotenv, []domain.Secret{{Key: "BAD=KEY", Value: "v"}, {Key: "", Value: "v"}, {Key: "OK", Value: "v"}})
	if string(r.Data) != "OK=v\n" || len(r.LeftOut) != 2 {
		t.Fatalf("data=%q leftOut=%v", r.Data, r.LeftOut)
	}
}

func TestLineQuotesLikeTheFile(t *testing.T) {
	if line, ok := Line(domain.Secret{Key: "K", Value: "a b "}); !ok || line != `K="a b "` {
		t.Fatalf("Line = %q, %v", line, ok)
	}
	if _, ok := Line(domain.Secret{Key: "K", Hidden: true}); ok {
		t.Fatal("a hidden secret has no line")
	}
}

func TestFormatsNameTheirFilesAndModes(t *testing.T) {
	for f, want := range map[Format]struct {
		name string
		perm uint32
	}{Dotenv: {"dev.env", 0o600}, JSON: {"dev.json", 0o600}, Example: {".env.example", 0o644}} {
		if f.FileName("dev") != want.name || uint32(f.Perm()) != want.perm {
			t.Fatalf("%s: %s %#o", f, f.FileName("dev"), f.Perm())
		}
	}
}
