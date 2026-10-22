package crawler

// literalMatcher is an Aho-Corasick automaton over ASCII-case-folded bytes. It reports whether any
// added literal occurs in a string, honouring per-literal anchors at the start and end of the text.

type anchoredLiteral struct {
	length     int
	start, end bool
}

type literalBuilder struct {
	children []map[byte]int32
	hit      []bool              // an unanchored literal ends at this state
	anchored [][]anchoredLiteral // anchored literals ending at this state
}

func newLiteralBuilder() *literalBuilder {
	return &literalBuilder{children: []map[byte]int32{{}}, hit: []bool{false}, anchored: [][]anchoredLiteral{nil}}
}

// add registers lit, which must be lowercase ASCII and non-empty.
func (b *literalBuilder) add(lit string, start, end bool) {
	s := int32(0)
	for i := 0; i < len(lit); i++ {
		c := lit[i]
		next, ok := b.children[s][c]
		if !ok {
			next = int32(len(b.children))
			b.children = append(b.children, map[byte]int32{})
			b.hit = append(b.hit, false)
			b.anchored = append(b.anchored, nil)
			b.children[s][c] = next
		}
		s = next
	}
	if start || end {
		b.anchored[s] = append(b.anchored[s], anchoredLiteral{length: len(lit), start: start, end: end})
	} else {
		b.hit[s] = true
	}
}

type literalMatcher struct {
	class    [256]uint8
	nclass   int
	next     []int32 // state*nclass + class
	hit      []bool
	anchored [][]anchoredLiteral
}

func (b *literalBuilder) build() *literalMatcher {
	m := &literalMatcher{hit: b.hit, anchored: b.anchored}
	// Byte classes: one per distinct literal byte, class 0 for every other byte.
	for _, ch := range b.children {
		for c := range ch {
			if m.class[c] == 0 {
				m.nclass++
				if m.nclass > 255 {
					panic("crawler: too many literal byte classes")
				}
				m.class[c] = uint8(m.nclass)
			}
		}
	}
	m.nclass++
	for c := 'A'; c <= 'Z'; c++ {
		m.class[c] = m.class[c-'A'+'a']
	}

	n := len(b.children)
	m.next = make([]int32, n*m.nclass)
	fail := make([]int32, n)
	queue := make([]int32, 0, n)
	for c, child := range b.children[0] {
		m.next[int(m.class[c])] = child
		queue = append(queue, child)
	}
	for len(queue) > 0 {
		s := queue[0]
		queue = queue[1:]
		f := fail[s]
		m.hit[s] = m.hit[s] || m.hit[f]
		m.anchored[s] = append(m.anchored[s], m.anchored[f]...)
		row, frow := int(s)*m.nclass, int(f)*m.nclass
		copy(m.next[row:row+m.nclass], m.next[frow:frow+m.nclass])
		for c, child := range b.children[s] {
			m.next[row+int(m.class[c])] = child
			fail[child] = m.next[frow+int(m.class[c])]
			queue = append(queue, child)
		}
	}
	return m
}

func (m *literalMatcher) matches(s string) bool {
	state := int32(0)
	for i := 0; i < len(s); i++ {
		state = m.next[int(state)*m.nclass+int(m.class[s[i]])]
		if m.hit[state] {
			return true
		}
		for _, a := range m.anchored[state] {
			if (!a.start || i+1 == a.length) && (!a.end || i+1 == len(s)) {
				return true
			}
		}
	}
	return false
}
