// Package wire holds conversions from domain values to protobuf wire
// types that every RPC layer needs the same way.
package wire

import "math"

// Int32 converts n to an int32 proto field, saturating at the int32
// bounds instead of wrapping. Counts on the wire (channels, ports,
// limits, durations in seconds) are bounded far below these by
// validation; saturating makes that impossible to get wrong — a wrapped
// value would turn a large count into a negative one.
func Int32[T ~int | ~int64](n T) int32 {
	switch {
	case int64(n) > math.MaxInt32:
		return math.MaxInt32
	case int64(n) < math.MinInt32:
		return math.MinInt32
	default:
		return int32(n)
	}
}
