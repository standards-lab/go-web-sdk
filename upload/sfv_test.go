package upload_test

import (
	"bytes"
	"encoding/base32"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk/upload"
)

const vectors = "testdata/structured-field-tests"

type vector struct {
	Name       string   `json:"name"`
	Raw        []string `json:"raw"`
	HeaderType string   `json:"header_type"`
	Expected   any      `json:"expected"`
	MustFail   bool     `json:"must_fail"`
	CanFail    bool     `json:"can_fail"`
	Canonical  []string `json:"canonical"`
}

func loadVectors(t *testing.T, pattern string) map[string][]vector {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(vectors, pattern))
	if err != nil || len(files) == 0 {
		t.Fatalf("no vector files match %s: %v", pattern, err)
	}
	sets := map[string][]vector{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var vs []vector
		if err := dec.Decode(&vs); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		sets[filepath.Base(f)] = vs
	}
	return sets
}

func bareValue(t *testing.T, v any) (value any, inSubset bool) {
	t.Helper()
	switch v := v.(type) {
	case json.Number:
		if strings.ContainsAny(string(v), ".eE") {
			return nil, false
		}
		n, err := v.Int64()
		if err != nil {
			t.Fatalf("integer %s: %v", v, err)
		}
		return n, true
	case bool:
		return v, true
	case map[string]any:
		if v["__type"] != "binary" {
			return nil, false
		}
		b, err := base32.StdEncoding.DecodeString(v["value"].(string))
		if err != nil {
			t.Fatalf("binary %v: %v", v["value"], err)
		}
		return b, true
	}
	return nil, false
}

func itemOf(t *testing.T, expected any) (value any, inSubset, hasParams bool) {
	t.Helper()
	pair := expected.([]any)
	value, inSubset = bareValue(t, pair[0])
	return value, inSubset, len(pair[1].([]any)) > 0
}

func dictionaryOf(t *testing.T, expected any) (d upload.Dictionary, inSubset, hasParams bool) {
	t.Helper()
	d = upload.Dictionary{}
	inSubset = true
	for _, m := range expected.([]any) {
		kv := m.([]any)
		member := kv[1].([]any)
		var value any
		if _, inner := member[0].([]any); inner {
			inSubset = false
		} else {
			var ok bool
			value, ok = bareValue(t, member[0])
			inSubset = inSubset && ok
		}
		if len(member[1].([]any)) > 0 {
			hasParams = true
		}
		d = append(d, upload.Member{Key: kv[0].(string), Value: value})
	}
	return d, inSubset, hasParams
}

func canonical(v vector) string {
	if v.Canonical != nil {
		if len(v.Canonical) == 0 {
			return ""
		}
		return v.Canonical[0]
	}
	return v.Raw[0]
}

func TestParseVectors(t *testing.T) {
	for file, vs := range loadVectors(t, "*.json") {
		for _, v := range vs {
			t.Run(file+"/"+v.Name, func(t *testing.T) {
				field := strings.Join(v.Raw, ", ")
				switch v.HeaderType {
				case "item":
					checkItem(t, v, field)
				case "dictionary":
					checkDictionary(t, v, field)
				default:
					t.Skipf("a %s field is outside the package's subset", v.HeaderType)
				}
			})
		}
	}
}

func checkItem(t *testing.T, v vector, field string) {
	var want any
	var hasParams bool
	if !v.MustFail {
		want, _, hasParams = itemOf(t, v.Expected)
	}

	n, ok := upload.ParseInteger(field)
	wantInt, isInt := want.(int64)
	switch {
	case ok && isInt && n != wantInt:
		t.Errorf("ParseInteger(%q) = %d, want %d", field, n, wantInt)
	case ok != isInt && !v.CanFail:
		t.Errorf("ParseInteger(%q) ok = %v, want %v", field, ok, isInt)
	case ok && isInt && !hasParams:
		if s, err := upload.FormatInteger(n); err != nil || s != canonical(v) {
			t.Errorf("FormatInteger(%d) = %q, %v; want %q", n, s, err, canonical(v))
		}
	}

	b, ok := upload.ParseBoolean(field)
	wantBool, isBool := want.(bool)
	switch {
	case ok && isBool && b != wantBool:
		t.Errorf("ParseBoolean(%q) = %v, want %v", field, b, wantBool)
	case ok != isBool && !v.CanFail:
		t.Errorf("ParseBoolean(%q) ok = %v, want %v", field, ok, isBool)
	case ok && isBool && !hasParams:
		if s := upload.FormatBoolean(b); s != canonical(v) {
			t.Errorf("FormatBoolean(%v) = %q, want %q", b, s, canonical(v))
		}
	}
}

func checkDictionary(t *testing.T, v vector, field string) {
	d, ok := upload.ParseDictionary(field)
	if ok != !v.MustFail {
		if !v.CanFail {
			t.Errorf("ParseDictionary(%q) ok = %v, want %v", field, ok, !v.MustFail)
		}
		return
	}
	if !ok {
		return
	}
	want, inSubset, hasParams := dictionaryOf(t, v.Expected)
	if !reflect.DeepEqual(d, want) {
		t.Errorf("ParseDictionary(%q) = %#v, want %#v", field, d, want)
	}
	if inSubset && !hasParams {
		if s, err := upload.FormatDictionary(d); err != nil || s != canonical(v) {
			t.Errorf("FormatDictionary(%#v) = %q, %v; want %q", d, s, err, canonical(v))
		}
	}
}

func TestFormatVectors(t *testing.T) {
	for file, vs := range loadVectors(t, "serialisation-tests/*.json") {
		for _, v := range vs {
			t.Run(file+"/"+v.Name, func(t *testing.T) {
				var s string
				var err error
				switch v.HeaderType {
				case "item":
					value, inSubset, _ := itemOf(t, v.Expected)
					n, isInt := value.(int64)
					if !inSubset || !isInt {
						t.Skip("only an Integer Item is in the package's subset")
					}
					s, err = upload.FormatInteger(n)
				case "dictionary":
					d, inSubset, hasParams := dictionaryOf(t, v.Expected)
					if !inSubset || hasParams {
						t.Skip("the dictionary's members are outside the package's subset")
					}
					s, err = upload.FormatDictionary(d)
				default:
					t.Skipf("a %s field is outside the package's subset", v.HeaderType)
				}
				switch {
				case v.MustFail && err == nil:
					t.Errorf("serialized %q, want an error", s)
				case !v.MustFail && (err != nil || s != canonical(v)):
					t.Errorf("serialized %q, %v; want %q", s, err, canonical(v))
				}
			})
		}
	}
}

func TestParseDictionaryOutsideSubset(t *testing.T) {
	d, ok := upload.ParseDictionary(`max-size=100, name="x";a=1, ints=(1 2), ratio=1.5`)
	if !ok {
		t.Fatal("ParseDictionary reported a well-formed field malformed")
	}
	want := upload.Dictionary{
		{Key: "max-size", Value: int64(100)},
		{Key: "name"},
		{Key: "ints"},
		{Key: "ratio"},
	}
	if !reflect.DeepEqual(d, want) {
		t.Errorf("ParseDictionary = %#v, want %#v", d, want)
	}
}

func TestFormatDictionaryRefuses(t *testing.T) {
	cases := map[string]upload.Dictionary{
		"repeated key":         {{Key: "a", Value: int64(1)}, {Key: "a", Value: int64(2)}},
		"uppercase key":        {{Key: "A", Value: int64(1)}},
		"empty key":            {{Key: "", Value: int64(1)}},
		"unsupported type":     {{Key: "a", Value: "text"}},
		"nil value":            {{Key: "a"}},
		"integer out of range": {{Key: "a", Value: int64(1_000_000_000_000_000)}},
	}
	for name, d := range cases {
		t.Run(name, func(t *testing.T) {
			if s, err := upload.FormatDictionary(d); err == nil {
				t.Errorf("FormatDictionary(%#v) = %q, want an error", d, s)
			}
		})
	}
}

func TestFormatDictionaryMembers(t *testing.T) {
	d := upload.Dictionary{
		{Key: "max-size", Value: int64(1 << 30)},
		{Key: "on", Value: true},
		{Key: "off", Value: false},
		{Key: "sha-256", Value: []byte{0xde, 0xad}},
	}
	const want = "max-size=1073741824, on, off=?0, sha-256=:3q0=:"
	s, err := upload.FormatDictionary(d)
	if err != nil || s != want {
		t.Fatalf("FormatDictionary = %q, %v; want %q", s, err, want)
	}
	back, ok := upload.ParseDictionary(s)
	if !ok || !reflect.DeepEqual(back, d) {
		t.Errorf("ParseDictionary(%q) = %#v, %v; want %#v", s, back, ok, d)
	}
}
