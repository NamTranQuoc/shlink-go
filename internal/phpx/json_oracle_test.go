package phpx

import "testing"

// Expected values were produced by json_encode() in the shlinkio/shlink:5.1.8 image (PHP 8.4).
func TestEncodeMatchesPHP(t *testing.T) {
	cases := []struct {
		flags    int
		value    string
		expected string
	}{
		{DiactorosJSONFlags, "<a href='x'>&\"</a> \u00e9 \u2028 \x01 \U0001f600", "{\"a\":\"\\u003Ca href=\\u0027x\\u0027\\u003E\\u0026\\u0022\\u003C/a\\u003E \\u00e9 \\u2028 \\u0001 \\ud83d\\ude00\"}"},
		{0, "\u00e9 \u2028 \x7f", "{\"a\":\"\\u00e9 \\u2028 \x7f\"}"},
		{ProblemDetailsJSONFlags, "\u00e9 \u2028", "{\"a\":\"\u00e9 \\u2028\"}"},
	}
	for _, c := range cases {
		if got := string(Encode(Map{{Key: "a", Value: c.value}}, c.flags)); got != c.expected {
			t.Errorf("flags %d: expected %s, got %s", c.flags, c.expected, got)
		}
	}
}
