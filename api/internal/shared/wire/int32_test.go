package wire

import (
	"math"
	"testing"
)

func TestInt32Saturates(t *testing.T) {
	for _, tc := range []struct {
		in   int64
		want int32
	}{
		{0, 0},
		{16, 16},
		{-5, -5},
		{math.MaxInt32, math.MaxInt32},
		{math.MaxInt32 + 1, math.MaxInt32},
		{math.MinInt32 - 1, math.MinInt32},
		{math.MaxInt64, math.MaxInt32},
	} {
		if got := Int32(tc.in); got != tc.want {
			t.Errorf("Int32(%d) = %d, want %d", tc.in, got, tc.want)
		}
	}
	if got := Int32(int(65535)); got != 65535 {
		t.Errorf("Int32(int) = %d", got)
	}
}
