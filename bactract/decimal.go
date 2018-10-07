package bactract

import (
	"fmt"
	"strings"
)

// readDecimal reads the value for a decimal column
func readDecimal(r *tReader, tc TableColumn) (ec ExtractedColumn, err error) {

	debOut("Func readDecimal")

	// Determine how many bytes to read
	// TODO: can the default be determined by the scope/precision?
	ss, err := r.readStoredSize(tc, 1, 2)
	if err != nil {
		return ec, err
	}

	// Check for nulls
	ec.IsNull = ss.isNull
	if ss.isNull {
		return ec, err
	}

	// Read and translate the decimal
	b, err := r.readBytes("readDecimal", ss.byteCount)
	if err != nil {
		return ec, err
	}

	ec.Str = parseDecimal(b)

	return ec, err
}

func parseDecimal(b []byte) (str string) {
	//precision := b[0]
	scale := int(b[1])

	var sign string
	if int(b[2]) == 0x00 {
		sign = "-"
	}

	// Shift off the precision, scale, and sign
	b = b[3:]

	// Pop the padding zero bytes
	b = stripTrailingNulls(b)

	// Calculate the integer
	byteCount := len(b)
	var z uint64
	for i := 0; i < byteCount; i++ {
		z |= uint64(b[i]) << uint(8*i)
	}

	// Fix the decimal point
	c := []byte(fmt.Sprintf("%d", z))

	// If the length of "c" is too short, which it will be for numbers
	// less than 1, then we need to add the missing zeros to the front of
	// "c" (to include the leading zero)
	p := []byte("0")
	for len(c) <= scale {
		c = append(p, c...)
	}

	// Add the decimal point and sign
	n := c[0 : len(c)-scale]
	d := c[len(c)-scale:]
	str = strings.Join([]string{sign, string(n), ".", string(d)}, "")

	return str
}
