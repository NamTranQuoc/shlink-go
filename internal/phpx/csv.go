package phpx

import (
	"bufio"
	"io"
)

// CSVReader ports PHP's fgetcsv() (php_fgetcsv() with no escape character) over a stream read line by line, the way
// SplFileObject feeds it: enclosed fields may span several lines, leading blanks before an enclosure are dropped,
// text after a closing enclosure is kept, and a blank line is a record with a single null field.
type CSVReader struct {
	r         *bufio.Reader
	Delimiter byte
	Enclosure byte
}

// NewCSVReader creates a reader with PHP's default "," delimiter and '"' enclosure.
func NewCSVReader(r io.Reader) *CSVReader {
	return &CSVReader{r: bufio.NewReader(r), Delimiter: ',', Enclosure: '"'}
}

// readLine ports php_stream_get_line(): the next line, including its "\n", or false at the end of the stream.
func (c *CSVReader) readLine() ([]byte, bool) {
	line, err := c.r.ReadBytes('\n')
	if len(line) == 0 && err != nil {
		return nil, false
	}
	return line, true
}

// trailingLineEnd ports php_fgetcsv_lookup_trailing_spaces(): the position where a trailing "\r\n", "\n" or "\r"
// starts.
func trailingLineEnd(b []byte) int {
	n := len(b)
	switch {
	case n >= 2 && b[n-2] == '\r' && b[n-1] == '\n':
		return n - 2
	case n >= 1 && (b[n-1] == '\n' || b[n-1] == '\r'):
		return n - 1
	}
	return n
}

func isCSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
}

// Read returns the next record. A nil field is PHP's null (blank lines). io.EOF is returned at the end of the stream.
func (c *CSVReader) Read() ([]*string, error) {
	buf, ok := c.readLine()
	if !ok {
		return nil, io.EOF
	}
	delimiter, enclosure := c.Delimiter, c.Enclosure
	limit := trailingLineEnd(buf)
	lineEnd := buf[limit:]
	bptr := 0
	var record []*string
	first := true
	for {
		var field []byte
		// Leading blanks are dropped when an enclosure follows them
		if bptr < limit {
			tmp := bptr
			for tmp < len(buf) && buf[tmp] != delimiter && isCSpace(buf[tmp]) {
				tmp++
			}
			if tmp < limit && buf[tmp] == enclosure {
				bptr = tmp
			}
		}
		if first && bptr == limit {
			return []*string{nil}, nil
		}
		first = false
		more := false
		if bptr < limit && buf[bptr] == enclosure {
			bptr++
			hunk := bptr
			state := 0
		enclosed:
			for {
				if bptr >= limit {
					switch state {
					case 2:
						field = append(field, buf[hunk:bptr-1]...)
						hunk = bptr
						break enclosed
					case 1:
						field = append(field, buf[hunk:bptr]...)
						hunk = bptr
					}
					if hunk != limit {
						field = append(field, buf[hunk:bptr]...)
					}
					// The field goes on in the next line
					field = append(field, lineEnd...)
					next, ok := c.readLine()
					if !ok {
						hunk = bptr
						break enclosed
					}
					buf = next
					bptr, hunk, state = 0, 0, 0
					limit = trailingLineEnd(buf)
					lineEnd = buf[limit:]
					continue
				}
				switch state {
				case 2:
					if buf[bptr] != enclosure {
						field = append(field, buf[hunk:bptr-1]...)
						hunk = bptr
						break enclosed
					}
					field = append(field, buf[hunk:bptr]...)
					bptr++
					hunk = bptr
					state = 0
				default:
					if buf[bptr] == enclosure {
						state = 2
					}
					bptr++
				}
			}
			// Whatever follows the closing enclosure, up to the delimiter, is kept
			for bptr < limit && buf[bptr] != delimiter {
				bptr++
			}
			field = append(field, buf[hunk:bptr]...)
			if bptr < limit {
				bptr++
				more = true
			}
		} else {
			hunk := bptr
			for bptr < limit && buf[bptr] != delimiter {
				bptr++
			}
			field = append(field, buf[hunk:bptr]...)
			field = field[:trailingLineEnd(field)]
			if bptr < limit {
				bptr++
				more = true
			}
		}
		s := string(field)
		record = append(record, &s)
		if !more {
			return record, nil
		}
	}
}
