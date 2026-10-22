package phpx

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

// Expected records were produced by the upstream image with SplFileObject::fgetcsv(',', '"', ”), the way league/csv
// reads CSV files (the trailing null record SplFileObject returns at the end of the stream is left out).
func TestCSVReader(t *testing.T) {
	cases := []struct{ in, expected string }{
		{"a,b,c\n", `[["a","b","c"]]`},
		{"a,\"b\"c\"d\",  \"e\" ,f \n", `[["a","bc\"d\"","e ","f "]]`},
		{"\n", `[[null]]`},
		{"\"multi\nline\",x\r\n", `[["multi\nline","x"]]`},
		{"\"unterminated,x\nmore\n", `[["unterminated,x\nmore\n"]]`},
		{"a,\"\"\"q\"\"\",\n", `[["a","\"q\"",""]]`},
		{"  x  ,  \"y\"z,\r\n", `[["  x  ","yz",""]]`},
		{"\"q\"\"\n", `[["q\"\n"]]`},
		{"one\r\ntwo\r\n\r\n", `[["one"],["two"],[null]]`},
		{"\"a\"\"b\"c,d", `[["a\"bc","d"]]`},
		{",,\n", `[["","",""]]`},
		{" \"\" \n", `[[" "]]`},
	}
	for _, c := range cases {
		r := NewCSVReader(strings.NewReader(c.in))
		var records [][]*string
		for {
			rec, err := r.Read()
			if errors.Is(err, io.EOF) {
				break
			}
			records = append(records, rec)
		}
		actual, _ := json.Marshal(records)
		if string(actual) != c.expected {
			t.Errorf("%q: expected %s, got %s", c.in, c.expected, actual)
		}
	}
}
