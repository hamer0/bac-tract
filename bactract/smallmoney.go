package bactract

import (
	"fmt"
)

// readSmallMoney reads the value for a small money column
func readSmallMoney(r *tReader, tc TableColumn) (ec ExtractedColumn, err error) {

	fn := "readSmallMoney"
	defSz := 4
	debOut(fmt.Sprintf("Func %s", fn))

	// Determine how many bytes to read
	ss, err := r.readStoredSize(tc, 1, defSz)
	if err != nil {
		return
	}

	// Check for nulls
	ec.IsNull = ss.isNull
	if ss.isNull {
		return
	}

	// Assert: If not null then the stored size is the default
	if ss.byteCount != defSz {
		err = fmt.Errorf("%s byteCount too large for column %q (%d vs %d)", fn, tc.ColName, ss.byteCount, defSz)
		return
	}

	// Read and translate the integer
	b, err := r.readBytes(fn, ss.byteCount)
	if err != nil {
		return
	}

	var z int32
	for i, sb := range stripTrailingNulls(b) {
		z |= int32(sb) << uint(8*i)
	}

	sb := []byte(fmt.Sprint(z))
	j := len(sb) - 4
	if j > 0 {
		ec.Str = fmt.Sprintf("%s.%s.", string(sb[0:j]), string(sb[j:]))
	} else if j == 0 {
		ec.Str = fmt.Sprintf("0.%s.", string(sb))
	}

	return
}
