package bactract

// readVarchar reads the value for a varchar column
func readVarchar(r *tReader, tc TableColumn) (ec ExtractedColumn, err error) {

	debOut("Func readVarchar")

	// Determine how many bytes to read
	ss, err := r.readStoredSize(tc, 2, 0)
	if err != nil {
		return ec, err
	}

	// Check for nulls
	ec.IsNull = ss.isNull
	if ss.isNull {
		return ec, err
	}

	// Read the chars
	b, err := r.readBytes("readVarchar", ss.byteCount)
	if err != nil {
		return ec, err
	}

	ec.Str = string(toRunes(b))
	return ec, err
}
