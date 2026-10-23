package toon

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMarshalObjectAndTable(t *testing.T) {
	got := Marshal(Object{
		{"table", "public.users"},
		{"columns", Table{Columns: []string{"name", "type", "pii"}, Rows: [][]any{
			{"id", "integer", false},
			{"email", "text", true},
		}}},
		{"meta", Object{{"rowCount", 2}, {"inner", Object{{"ok", true}}}}},
		{"empty", Table{Columns: []string{"a"}}},
	})
	assert.Equal(t, `table: public.users
columns[2]{name,type,pii}:
  id,integer,false
  email,text,true
meta:
  rowCount: 2
  inner:
    ok: true
empty[0]{a}:`, got)
}

func TestTableRowsAndKeys(t *testing.T) {
	got := Marshal(Object{
		{"rows", Table{Columns: []string{"id", "count(*)", "my col", "a.b"}, Rows: [][]any{{1, 2}}}},
		{"x-y", "z"},
	})
	assert.Equal(t, "rows[1]{id,\"count(*)\",\"my col\",a.b}:\n  1,2,null,null\n\"x-y\": z", got)
	assert.Empty(t, Marshal(nil))
}

func TestStrings(t *testing.T) {
	for in, want := range map[string]string{
		"alice":                "alice",
		"Customer 7":           "Customer 7",
		"":                     `""`,
		" pad":                 `" pad"`,
		"pad ":                 `"pad "`,
		"true":                 `"true"`,
		"false":                `"false"`,
		"null":                 `"null"`,
		"42":                   `"42"`,
		"-3.5e+2":              `"-3.5e+2"`,
		"05":                   `"05"`,
		"-":                    `"-"`,
		"-x":                   `"-x"`,
		"a,b":                  `"a,b"`,
		"2026-10-23T07:00:00Z": `"2026-10-23T07:00:00Z"`,
		`say "hi"`:             `"say \"hi\""`,
		`C:\dir`:               `"C:\\dir"`,
		"[1]":                  `"[1]"`,
		"{}":                   `"{}"`,
		"a\nb\rc\td":           `"a\nb\rc\td"`,
		"bell\x07":             `"bell\u0007"`,
		"héllo wörld":          "héllo wörld",
	} {
		assert.Equal(t, "v: "+want, Marshal(Object{{"v", in}}), in)
	}
}

type empty struct{}

func TestValues(t *testing.T) {
	for _, c := range []struct {
		in   any
		want string
	}{
		{nil, "null"},
		{true, "true"},
		{7, "7"},
		{int8(-8), "-8"},
		{int64(1) << 60, "1152921504606846976"},
		{uint16(9), "9"},
		{uint64(math.MaxUint64), "18446744073709551615"},
		{1.5, "1.5"},
		{float32(0.25), "0.25"},
		{math.Copysign(0, -1), "0"},
		{1e6, "1000000"},
		{0.000001, "0.000001"},
		{1e21, "1e+21"},
		{1e-7, "1e-07"},
		{math.NaN(), "null"},
		{math.Inf(-1), "null"},
		{json.RawMessage(`12.50`), "12.50"},
		{json.RawMessage(` true `), "true"},
		{json.RawMessage(`null`), "null"},
		{json.RawMessage(`"plain"`), "plain"},
		{json.RawMessage(`"7"`), `"7"`},
		{json.RawMessage(`{"a":[1,2]}`), `"{\"a\":[1,2]}"`},
		{[]any{1, "x"}, `"[1,\"x\"]"`},
		{map[string]int{"n": 1}, `"{\"n\":1}"`},
		{empty{}, `"{}"`},
	} {
		assert.Equal(t, "v: "+c.want, Marshal(Object{{"v", c.in}}), "%#v", c.in)
	}
	assert.Regexp(t, `^v: 0x[0-9a-f]+$`, Marshal(Object{{"v", func() {}}}), "a value JSON cannot hold is written as text")
}
