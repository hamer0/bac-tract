package bactract

// Read/parse the bacpac BCP data files.

import (
	//"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	//
	"github.com/gsiems/go-read-wrap/srw"
)

type tReader struct {
	reader io.Reader
	Rownum int
	table  Table
}

// ExtractedColumn contains the data/metadata for a column extracted from a row of data.
type ExtractedColumn struct {
	ColName    string
	DataType   int
	DtStr      string
	Length     int
	Scale      int
	Precision  int
	IsNullable bool
	IsNull     bool
	Str        string
}

type storedSize struct {
	byteCount int
	isNull    bool
	sizeBytes []byte
}

// DataReader creates a multi-reader on the data files for the specified table
func (t *Table) DataReader() (reader tReader, err error) {

	var readers []io.Reader

	files, err := ioutil.ReadDir(t.dataDir)
	if err == os.ErrNotExist {
		return reader, nil
	}
	if err != nil {
		return reader, err
	}

	for _, f := range files {
		filename := catDir([]string{t.dataDir, f.Name()})
		r, err := os.Open(filename)
		if err != nil {
			// TODO: do we want to expand on the err to include the file being opened?
			return reader, err
		}
		readers = append(readers, r)
	}

	reader.reader = srw.BuffMultiReader(0, readers...)
	reader.table = *t

	return reader, err
}

// ReadNextRow reads the next table row from the BCP file and ...
func (r *tReader) ReadNextRow() (row []ExtractedColumn, err error) {

	type fn func(r *tReader, tc TableColumn) (ec ExtractedColumn, err error)

	dt := map[int]fn{
		BigInt: readBigInt,
		//Binary:           readBinary,
		Bit:       readBit,
		Char:      readChar,
		Datetime2: readDatetime2,
		Datetime:  readDatetime,
		Decimal:   readDecimal,
		Float:     readFloat,
		//Geography:        readGeography,
		Int: readInt,
		//NChar:            readNChar,
		NText:         readNText,
		Numeric:       readDecimal,
		NVarchar:      readNVarchar,
		Real:          readReal,
		SmallDatetime: readSmallDatetime,
		SmallInt:      readSmallInt,
		SmallMoney:    readSmallMoney,
		//SQLVariant:       readSQLVariant,
		Text:    readText,
		TinyInt: readTinyInt,
		//UniqueIdentifier: readUniqueIdentifier,
		Varbinary: readVarbinary,
		Varchar:   readVarchar,
	}

	for _, tc := range r.table.Columns {

		if debugFlag {
			debOut(fmt.Sprintf("%q %s %d, %d, %d, %v", tc.ColName, tc.DtStr, tc.Length, tc.Scale, tc.Precision, tc.IsNullable))
		}

		fcn, ok := dt[tc.DataType]
		if ok {
			ec, err := fcn(r, tc)
			if err != nil {

				if err == io.EOF {
					debOut("\nEOF")
				}
				return row, err
			}

			ec.ColName = tc.ColName
			ec.DataType = tc.DataType

			ec.Length = tc.Length
			ec.Scale = tc.Scale
			ec.Precision = tc.Precision
			ec.IsNullable = tc.IsNullable
			ec.DtStr = tc.DtStr

			if debugFlag {
				debOut(fmt.Sprintf("IsNull: %v", ec.IsNull))
				debOut(fmt.Sprintf("Str: %s", ec.Str))
				debOut("")
			}

			row = append(row, ec)
		} else {
			err = fmt.Errorf("ReadNextRow: No parser defined for %q", tc.ColName)
			return row, err
		}
	}

	return row, nil
}

// readBytes reads the specified number of bytes from the reader
func (r *tReader) readBytes(label string, n int) (b []byte, err error) {

	debOut(fmt.Sprintf("%s: Attempting to read %d bytes", label, n))

	b = make([]byte, n)
	_, err = r.reader.Read(b)
	debHextOut("Bytes", b)
	return b, err
}

// readStoredSize reads the specified number of bytes to determine the
// number of bytes used to store the value for the associated field.
// For example, a null int uses 0 bytes of storage while a non-null int
// uses 4 bytes.
func (r *tReader) readStoredSize(tc TableColumn, n, def int) (s storedSize, err error) {

	s.isNull = tc.IsNullable
	if !tc.IsNullable && def > 0 {
		// Just return the default for not-null columns
		s.byteCount = def
		return s, err
	}

	// So either the column is nullable or there is no default size--
	// therfore read n bytes to determine how many data bytes to read
	s.sizeBytes, err = r.readBytes("readStoredSize", n)
	if err != nil {
		return s, err
	}

	// Discard trailing nulls when calculating the storage byte count.
	// For example, if a varchar uses 2 bytes to store the byteCount
	// but the varchar is only, say, 4 bytes long then the second
	// "storage size" byte is 0x00 and should not enter into the
	// byteCount calculation.
	b := stripTrailingNulls(s.sizeBytes)
	if len(b) == 0 {
		return s, err
	}

	for i := 0; i < len(b); i++ {
		s.byteCount |= int(b[i]) << uint(8*i)
		s.isNull = false
	}

	// Determine is the difference between a null column and column that has an empty string
	if b[0] == 0xff {
		s.isNull = tc.IsNullable
		s.byteCount = 0
		return s, err
	}

	return s, err
}
