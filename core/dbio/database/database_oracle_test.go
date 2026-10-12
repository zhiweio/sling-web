package database

import (
	"database/sql/driver"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// The array-DML path binds values straight onto a raw go-ora connection, so
// oracleDriverValue must produce exactly the types database/sql's default
// converter would have produced on the cursor path.
func TestOracleDriverValue(t *testing.T) {
	ts := time.Now()
	cases := []struct {
		in   interface{}
		want driver.Value
	}{
		{nil, nil},
		{"txt", "txt"},
		{[]byte("b"), []byte("b")},
		{int64(5), int64(5)},
		{int(7), int64(7)},
		{int32(3), int64(3)},
		{float32(1.5), float64(1.5)},
		{float64(2.5), float64(2.5)},
		{true, true},
		{false, false},
		{ts, ts},
	}
	for _, c := range cases {
		got, err := oracleDriverValue(c.in)
		assert.NoError(t, err, "value %#v", c.in)
		if b1, ok := c.want.([]byte); ok {
			assert.Equal(t, b1, got.([]byte))
		} else {
			assert.Equal(t, c.want, got, "value %#v", c.in)
		}
	}

	// unsupported types must error, not silently become NULL
	if _, err := oracleDriverValue(struct{ X int }{1}); err == nil {
		t.Fatal("struct value must fail conversion")
	}
}
