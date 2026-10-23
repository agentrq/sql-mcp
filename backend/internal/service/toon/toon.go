// Package toon writes TOON (Token-Oriented Object Notation, spec v3), the
// text the MCP tool answers in. A table names its columns once in a header
// and then gives one comma-separated line per row, so a result costs about
// what CSV would while keeping null, numbers and strings apart.
//
// It is an encoder only, for the shapes the tool sends: objects whose values
// are primitives, nested objects or tables.
package toon

import (
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Field is one key and its value. The value is an Object, a Table or a
// primitive; anything else is written as its JSON text.
type Field struct {
	Key   string
	Value any
}

// Object is a TOON object, its fields in order.
type Object []Field

// Table is an array of rows that share the same columns. A row shorter than
// Columns is padded with null.
type Table struct {
	Columns []string
	Rows    [][]any
}

const indent = "  "

// Marshal writes o as a TOON document, without a trailing newline.
func Marshal(o Object) string {
	var b strings.Builder
	writeObject(&b, o, 0)
	return strings.TrimSuffix(b.String(), "\n")
}

func writeObject(b *strings.Builder, o Object, depth int) {
	pad := strings.Repeat(indent, depth)
	for _, f := range o {
		switch v := f.Value.(type) {
		case Object:
			b.WriteString(pad + key(f.Key) + ":\n")
			writeObject(b, v, depth+1)
		case Table:
			writeTable(b, f.Key, v, pad)
		default:
			b.WriteString(pad + key(f.Key) + ": " + value(v) + "\n")
		}
	}
}

func writeTable(b *strings.Builder, k string, t Table, pad string) {
	cols := make([]string, len(t.Columns))
	for i, c := range t.Columns {
		cols[i] = key(c)
	}
	fmt.Fprintf(b, "%s%s[%d]{%s}:\n", pad, key(k), len(t.Rows), strings.Join(cols, ","))
	cells := make([]string, len(t.Columns))
	for _, r := range t.Rows {
		for i := range cells {
			cells[i] = "null"
			if i < len(r) {
				cells[i] = value(r[i])
			}
		}
		b.WriteString(pad + indent + strings.Join(cells, ",") + "\n")
	}
}

var bareKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.]*$`)

func key(k string) string {
	if bareKey.MatchString(k) {
		return k
	}
	return quote(k)
}

// value writes a primitive. JSON (a json column, a Postgres numeric, an
// array) keeps a number, boolean or null as it is and is otherwise a string.
func value(v any) string {
	switch x := v.(type) {
	case nil:
		return "null"
	case bool:
		return strconv.FormatBool(x)
	case string:
		return str(x)
	case int:
		return strconv.Itoa(x)
	case int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprint(x)
	case float32:
		return float(float64(x), 32)
	case float64:
		return float(x, 64)
	case json.RawMessage:
		return raw(x)
	}
	if b, err := json.Marshal(v); err == nil {
		return raw(b)
	}
	return str(fmt.Sprint(v))
}

var plainNumber = regexp.MustCompile(`^-?\d+(\.\d+)?$`)

func raw(b []byte) string {
	s := strings.TrimSpace(string(b))
	var text string
	switch {
	case s == "null" || s == "true" || s == "false" || plainNumber.MatchString(s):
		return s
	case json.Unmarshal(b, &text) == nil:
		return str(text)
	}
	return str(s)
}

// float writes a number in canonical decimal form: no exponent inside
// [1e-6, 1e21), no -0, and null for what JSON cannot hold.
func float(f float64, bits int) string {
	switch {
	case math.IsNaN(f) || math.IsInf(f, 0):
		return "null"
	case f == 0:
		return "0"
	case math.Abs(f) >= 1e-6 && math.Abs(f) < 1e21:
		return strconv.FormatFloat(f, 'f', -1, bits)
	}
	return strconv.FormatFloat(f, 'e', -1, bits)
}

var numericLike = regexp.MustCompile(`(?i)^-?\d+(?:\.\d+)?(?:e[+-]?\d+)?$`)

// str quotes a string when it would otherwise read as something else: a
// number, a keyword, a delimiter or structure.
func str(s string) string {
	if s == "" || s != strings.TrimSpace(s) || s == "true" || s == "false" || s == "null" ||
		numericLike.MatchString(s) || strings.HasPrefix(s, "-") || strings.ContainsAny(s, `:"\[]{},`) ||
		strings.IndexFunc(s, func(r rune) bool { return r < 0x20 }) >= 0 {
		return quote(s)
	}
	return s
}

func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
