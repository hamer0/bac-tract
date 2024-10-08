// Extract one or more tables from an unzipped bacpac file and write to
// the corresponding pg_dump file(s)

package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"runtime/pprof"
	"strings"

	//
	bp "github.com/gsiems/bac-tract/bactract"
)

type params struct {
	baseDir           string
	output            string
	tableName         string
	tablesFile        string
	colExceptionsFile string
	rowLimit          uint64
	workers           int
	cpuprofile        string
	memprofile        string
	debug             bool
}

type workItem struct {
	ID     int
	V      params
	Tab    bp.Table
	doneBy int
}

type Worker struct {
	workerID int
	todo     chan workItem
	done     chan workItem
}

func newWorker(workerID int, todo chan workItem, done chan workItem) Worker {
	return Worker{
		workerID: workerID,
		todo:     todo,
		done:     done,
	}
}

func (w Worker) start() {
	go func() {
		for {
			select {
			case item := <-w.todo:
				item.doneBy = w.workerID
				mkFile(item.Tab, item.V)
				w.done <- item
			}
		}
	}()
}

func main() {

	var v params

	flag.StringVar(&v.baseDir, "b", "", "The directory containing the unzipped bacpac file.")
	flag.StringVar(&v.output, "o", "", "The output directory to write files into.")
	flag.StringVar(&v.tableName, "t", "", "The table to extract data from. When not specified then extract all tables")
	flag.StringVar(&v.colExceptionsFile, "e", "", "The column exceptions data file, should there be one")
	flag.StringVar(&v.tablesFile, "f", "", "The file to read the list of tables to extract from, one table per line")
	flag.Uint64Var(&v.rowLimit, "c", 0, "The number of rows to extract. When 0 extract all rows.")
	flag.IntVar(&v.workers, "w", 1, "The number of workers to use")
	flag.BoolVar(&v.debug, "debug", false, "Write debug information to STDOUT.")
	flag.StringVar(&v.cpuprofile, "cpuprofile", "", "The filename to write cpu profile information to")
	//flag.StringVar(&v.memprofile, "memprofile", "", The filename to write memory profile information to")

	flag.Parse()

	if v.cpuprofile != "" {
		f, err := os.Create(v.cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		err = pprof.StartCPUProfile(f)
		if err != nil {
			log.Fatal(err)
		}
		defer pprof.StopCPUProfile()
	}

	tables := getTables(v)

	// create the channels
	todo := make(chan workItem, len(tables))
	done := make(chan workItem, 1)

	// start the workers
	for i := 0; i < v.workers; i++ {
		worker := newWorker(i, todo, done)
		worker.start()
	}

	// feed the work queue
	for i, table := range tables {
		item := workItem{ID: i, V: v, Tab: table}
		todo <- item
	}

	// wait for/catch the results
	for range tables {
		select {
		case _ = <-done:
		}
	}

}

func getTables(v params) (l []bp.Table) {

	p, _ := bp.New(v.baseDir)

	p.SetDebug(v.debug)

	model, err := p.GetModel(v.colExceptionsFile)
	dieOnErrf("GetModel failed: %q", err)

	var tables []string
	if v.tableName != "" {
		tables = append(tables, v.tableName)
	} else if v.tablesFile != "" {

		content, err := ioutil.ReadFile(v.tablesFile)
		dieOnErrf("File read failed: %q", err)

		x := bytes.Split(content, []byte("\n"))
		for _, z := range x {
			tables = append(tables, string(z))
		}
	} else {
		tables, err = p.ExportedTables()
		dieOnErrf("ExportedTables failed: %q", err)
	}

	for _, table := range tables {
		t, ok := model.Tables[table]
		if ok {
			//hasBinary := false
			//for _, c := range t.Columns {
			//	if c.DataType == bp.Binary || c.DataType == bp.Varbinary {
			//		hasBinary = true
			//	}
			//}
			//
			//if hasBinary {
			//	log.Printf("Warning: \"%s.%s\" has possible binary data.\n", t.Schema, t.TabName)
			//}

			l = append(l, t)
		}
	}

	return
}

func mkFile(t bp.Table, v params) {

	colSep := []byte("\t")
	recSep := []byte("\n")
	nullMk := []byte("\\N")
	dmpEnd := []byte("\\.")

	// characters that require escaping
	escChar := map[byte][]byte{
		byte(0x08): []byte("\\b"),
		byte(0x09): []byte("\\t"),
		byte(0x0a): []byte("\\r"),
		byte(0x0b): []byte("\\v"),
		byte(0x0c): []byte("\\f"),
		byte(0x0d): []byte("\\n"),
		byte(0x5c): []byte("\\\\"),
	}

	var keys []byte
	for k := range escChar {
		keys = append(keys, k)
	}
	escStr := string(keys)

	r, err := t.DataReader()
	dieOnErrf("DataReader failed: %q", err)

	os.Mkdir(v.output, os.ModeDir)

	target := fmt.Sprintf("%s/%s.%s.dump", v.output, t.Schema, t.TabName)
	f := openOutput(target)
	defer deferredClose(f)
	w := bufio.NewWriter(f)

	writeHdr := true

	var i uint64
	for {

		i++
		if v.rowLimit > 0 && i > v.rowLimit {
			break
		}

		row, err := r.ReadNextRow()
		if err == io.EOF {
			break
		}
		if err != nil {
			log.Printf("Error: \"%s.%s\" (row %d): %s.\n", t.Schema, t.TabName, i, err)
			break
		}

		if writeHdr {
			var cols []string
			for _, ec := range row {
				cols = append(cols, ec.ColName)
			}

			hdr := fmt.Sprintf("COPY %s (%s) FROM stdin;\n", t.TabName, strings.Join(cols, ","))
			w.Write([]byte(hdr))
			writeHdr = false
		}

		for j, ec := range row {
			if j > 0 {
				w.Write(colSep)
			}

			if ec.DataType == bp.Varbinary || ec.IsNull {
				w.Write(nullMk)
			} else {

				b := ec.Str
				// escape some things as needed

				for len(b) > 0 {
					i := strings.IndexAny(b, escStr)
					if i < 0 {
						w.Write([]byte(b))
						break
					}

					if i > 0 {
						w.Write([]byte(b[:i]))
						b = b[i:]
					}

					if len(b) > 0 {
						rep, ok := escChar[b[0]]
						if ok {
							w.Write(rep)
							b = b[1:]
						}
					}
				}
			}
		}
		w.Write(recSep)
	}

	if i > 0 {
		w.Write(dmpEnd)
		w.Write([]byte("\n\n"))
	}

	w.Flush()
}

// openOutput opens the appropriate target for writing output, or dies trying
func openOutput(target string) (f *os.File) {

	var err error

	if target == "" || target == "-" {
		f = os.Stdout
	} else {
		f, err = os.OpenFile(target, os.O_CREATE|os.O_WRONLY, 0644)
		dieOnErrf("File open failed: %q", err)
	}
	return f
}

// deferredClose closes a file handle, or dies trying
func deferredClose(f *os.File) {
	err := f.Close()
	dieOnErrf("File close failed: %q", err)
}

func dieOnErrf(s string, err error) {
	if err != nil {
		log.Fatalf(s, err)
	}
}

func dieOnErr(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
