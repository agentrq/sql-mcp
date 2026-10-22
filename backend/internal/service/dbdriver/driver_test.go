package dbdriver

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentrq/sql-mcp/backend/internal/service/sqlguard"
)

func TestSuggestPII(t *testing.T) {
	for _, col := range []string{
		"email", "EMAIL", "e_mail", "user_email", "email_verified", " phone ", "mobile", "ssn",
		"tax_id", "taxid", "date_of_birth", "dob", "address", "home_address", "zip", "postal_code",
		"first_name", "lastname", "full_name", "ip", "ip_address", "last_ip", "credit_card",
		"card_number", "iban", "password", "api_token", "client_secret", "salary", "gender",
		"latitude", "geo", "location",
	} {
		assert.True(t, SuggestPII(col), col)
	}
	for _, col := range []string{
		"id", "user_id", "created_at", "total", "status", "country", "brand",
		"emails_sent_count2", "zipper", "tip", "description", "shipping", "ipsum", "",
	} {
		assert.False(t, SuggestPII(col), col)
	}
}

// fakeDriver is an engine that only has an ID and availability.
type fakeDriver struct {
	Driver
	id        string
	available bool
}

func (f fakeDriver) ID() string      { return f.id }
func (f fakeDriver) Available() bool { return f.available }

func TestRegistry(t *testing.T) {
	pg := fakeDriver{id: "postgres", available: true}
	my := fakeDriver{id: "mysql"}
	r := NewRegistry(pg, my)
	assert.Equal(t, []Driver{pg, my}, r.All())

	got, err := r.Get("postgres")
	require.NoError(t, err)
	assert.Equal(t, pg, got)

	_, err = r.Get("mysql")
	assert.ErrorIs(t, err, ErrUnavailable)

	_, err = r.Get("oracle")
	assert.EqualError(t, err, "unknown database engine")

	assert.Empty(t, NewRegistry().All())
}

func TestMySQLIsUnavailable(t *testing.T) {
	d := NewMySQL()
	ctx := context.Background()
	assert.Equal(t, "mysql", d.ID())
	assert.Equal(t, "MySQL", d.Name())
	assert.False(t, d.Available())
	assert.ErrorIs(t, d.ValidateDSN("mysql://u:p@h/db"), ErrUnavailable)
	assert.Empty(t, d.MaskDSN("mysql://u:p@h/db"))
	_, err := d.Test(ctx, "dsn")
	assert.ErrorIs(t, err, ErrUnavailable)
	_, err = d.Schema(ctx, "dsn")
	assert.ErrorIs(t, err, ErrUnavailable)
	_, err = d.Query(ctx, "dsn", "select 1", 1, time.Second)
	assert.ErrorIs(t, err, ErrUnavailable)
	assert.ErrorIs(t, d.Guard(nil).Check("select 1"), ErrUnavailable)
}

type stringer struct{}

func (stringer) String() string { return "stringer" }

type badMarshaler struct{}

func (badMarshaler) MarshalJSON() ([]byte, error) { return nil, errors.New("no") }
func (badMarshaler) String() string               { return "bad" }

type goodMarshaler struct{}

func (goodMarshaler) MarshalJSON() ([]byte, error) { return []byte(`{"a":1}`), nil }

func TestJSONValue(t *testing.T) {
	ts := time.Date(2026, 1, 2, 3, 4, 5, 6, time.UTC)
	ch := make(chan int)
	for _, tc := range []struct {
		name string
		in   any
		want any
	}{
		{"nil", nil, nil},
		{"bool", true, true},
		{"string", "s", "s"},
		{"int", 1, 1},
		{"int8", int8(-8), int8(-8)},
		{"int16", int16(16), int16(16)},
		{"int32", int32(32), int32(32)},
		{"int64", int64(64), int64(64)},
		{"uint8", uint8(8), uint8(8)},
		{"uint16", uint16(16), uint16(16)},
		{"uint32", uint32(32), uint32(32)},
		{"uint64", uint64(64), uint64(64)},
		{"float32", float32(1.5), float32(1.5)},
		{"float64", 2.5, 2.5},
		{"uuid", [16]byte{0x00, 0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}, "00112233-4455-6677-8899-aabbccddeeff"},
		{"time", ts, "2026-01-02T03:04:05.000000006Z"},
		{"utf-8 bytes", []byte("héllo"), "héllo"},
		{"binary bytes", []byte{0xff, 0x00}, "/wA="},
		{"marshaler", goodMarshaler{}, json.RawMessage(`{"a":1}`)},
		{"failing marshaler", badMarshaler{}, "bad"},
		{"stringer", stringer{}, "stringer"},
		{"encodable", map[string]any{"a": []any{1.0}}, map[string]any{"a": []any{1.0}}},
		{"not encodable", ch, ""}, // checked below
		{"nan", math.NaN(), nil},  // checked below
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := JSONValue(tc.in)
			switch tc.name {
			case "not encodable":
				assert.IsType(t, "", got)
				assert.Regexp(t, `^0x[0-9a-f]+$`, got)
			case "nan":
				assert.True(t, math.IsNaN(got.(float64)))
			default:
				assert.Equal(t, tc.want, got)
			}
		})
	}
}

func TestPostgresBasics(t *testing.T) {
	d := NewPostgres()
	assert.Equal(t, "postgres", d.ID())
	assert.Equal(t, "PostgreSQL", d.Name())
	assert.True(t, d.Available())

	g := d.Guard([]sqlguard.Column{{Table: "users", Column: "email"}})
	assert.Error(t, g.Check("select email from users"))
	assert.NoError(t, g.Check("select id from users"))
}

func TestValidateDSN(t *testing.T) {
	d := NewPostgres()
	for _, dsn := range []string{
		"postgres://u:p@localhost:5432/db?sslmode=disable",
		"postgresql://localhost/db",
		"host=localhost user=u password=p dbname=db",
	} {
		assert.NoError(t, d.ValidateDSN(dsn), dsn)
	}
	for _, dsn := range []string{"", "   ", "postgres://u:p@h:notaport/db", "host=h sslmode=bogus", "mysql://u@h/db"} {
		assert.Error(t, d.ValidateDSN(dsn), dsn)
	}
}

func TestMaskDSN(t *testing.T) {
	d := NewPostgres()
	for _, tc := range []struct{ in, want string }{
		{"postgres://u:secret@h:5432/db?sslmode=disable", "postgres://u:****@h:5432/db?sslmode=disable"},
		{"postgresql://u:secret@h/db", "postgresql://u:****@h/db"},
		{"postgres://u@h/db", "postgres://u@h/db"},
		{"postgres://h/db", "postgres://h/db"},
		{"postgres://u@h/db?password=secret&sslmode=require", "postgres://u@h/db?password=****&sslmode=require"},
		{"postgres://u:a%40b@h/db?password=c", "postgres://u:****@h/db?password=****"},
		{"host=h user=u password=secret dbname=db", "host=h user=u password=**** dbname=db"},
		{"host=h  PASSWORD=secret", "host=h  PASSWORD=****"},
		{"host=h passwordx=keep", "host=h passwordx=keep"},
		{"", ""},
	} {
		got := d.MaskDSN(tc.in)
		assert.Equal(t, tc.want, got, tc.in)
		assert.NotContains(t, got, "secret", tc.in)
	}
}

// A key=value password may be quoted or spaced around "=", as libpq and pgx
// both accept; the mask must still hide all of it.
func TestMaskDSNQuotedOrSpacedPassword(t *testing.T) {
	d := NewPostgres()
	for _, dsn := range []string{
		"host=h password='my secret' dbname=db",
		"host=h password = secret dbname=db",
	} {
		require.NoError(t, d.ValidateDSN(dsn), dsn)
		assert.NotContains(t, d.MaskDSN(dsn), "secret", dsn)
	}
}
