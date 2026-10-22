package console

import (
	"bufio"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Question ports Question, ConfirmationQuestion and ChoiceQuestion. Default is nil, a string or a bool.
type Question struct {
	Text       string
	Default    any
	Validator  func(answer any) (any, error)
	Normalizer func(answer any) any
	// StrictNormalizer is a normalizer that may throw, like the one #[Ask] sets for backed enums
	StrictNormalizer func(answer any) (any, error)
	MaxAttempts      int // 0 means unlimited
	Trimmable        bool
	confirm          bool
	choices          []string
	// keys are the PHP array keys of choices; nil means a list (keys are the indexes)
	keys []string
	// assoc tells if keys are strings, in which case the answer is the chosen key
	assoc bool
}

// choiceKey returns the PHP array key of the i-th choice.
func (q *Question) choiceKey(i int) string {
	if q.keys != nil {
		return q.keys[i]
	}
	return strconv.Itoa(i)
}

// NewQuestion creates a plain question.
func NewQuestion(text string, def any) *Question {
	return &Question{Text: text, Default: def, Trimmable: true}
}

var trueAnswer = regexp.MustCompile(`(?i)^y`)

// NewConfirmationQuestion ports ConfirmationQuestion with the default /^y/i true answer regex.
func NewConfirmationQuestion(text string, def bool) *Question {
	q := &Question{Text: text, Default: def, Trimmable: true, confirm: true}
	q.Normalizer = func(answer any) any {
		if b, ok := answer.(bool); ok {
			return b
		}
		s, _ := answer.(string)
		isTrue := trueAnswer.MatchString(s)
		if !def {
			return s != "" && s != "0" && isTrue
		}
		return s == "" || isTrue
	}
	return q
}

// NewChoiceQuestion ports ChoiceQuestion over a list of choices (keys are their indexes).
func NewChoiceQuestion(text string, choices []string, def any) *Question {
	q := &Question{Text: text, Default: def, Trimmable: true, choices: choices}
	q.Validator = func(selected any) (any, error) {
		value := strings.TrimSpace(answerString(selected))
		var results []string
		for i, c := range choices {
			if c == value {
				results = append(results, q.choiceKey(i))
			}
		}
		if len(results) > 1 {
			return nil, &InvalidArgumentError{Message: fmt.Sprintf(`The provided answer is ambiguous. Value should be one of "%s".`, strings.Join(results, `" or "`))}
		}
		for i, c := range choices {
			if c == value {
				if q.assoc {
					return q.choiceKey(i), nil
				}
				return c, nil
			}
		}
		for i := range choices {
			if q.choiceKey(i) == value {
				if q.assoc {
					return value, nil
				}
				return choices[i], nil
			}
		}
		return nil, &InvalidArgumentError{Message: fmt.Sprintf(`Value "%s" is invalid`, value)}
	}
	return q
}

// NewAssocChoiceQuestion ports ChoiceQuestion over choices with string keys: the answer is the chosen key.
func NewAssocChoiceQuestion(text string, keys, choices []string, def any) *Question {
	q := NewKeyedChoiceQuestion(text, keys, choices, def)
	q.assoc = true
	return q
}

// NewKeyedChoiceQuestion ports ChoiceQuestion over choices with integer keys that are not a list, like the result of
// array_flip(). Integer keys keep ChoiceQuestion's list semantics: the answer is the chosen value.
func NewKeyedChoiceQuestion(text string, keys, choices []string, def any) *Question {
	q := NewChoiceQuestion(text, choices, def)
	q.keys = keys
	return q
}

func answerString(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "1"
		}
		return ""
	}
	return fmt.Sprint(v)
}

// stdinReader reads answers the way QuestionHelper::doReadInput() does.
type stdinReader struct {
	r   *bufio.Reader
	eof bool
}

func (s *stdinReader) readLine() (string, bool) {
	if s.eof {
		return "", true
	}
	var ret strings.Builder
	for {
		c, err := s.r.ReadByte()
		if err != nil {
			s.eof = true
			if ret.Len() == 0 {
				return "", false
			}
			return ret.String(), true
		}
		if ret.Len() == 0 && c == '\n' {
			return "", true
		}
		ret.WriteByte(c)
		if c == '\n' {
			return ret.String(), true
		}
	}
}

// AskQuestion ports SymfonyStyle::askQuestion() with SymfonyQuestionHelper.
func (s *SymfonyStyle) AskQuestion(q *Question) (any, error) {
	if s.input.IsInteractive() {
		s.autoPrependBlock()
	}
	answer, err := s.ask(q)
	if err != nil {
		return nil, err
	}
	if s.input.IsInteractive() {
		s.NewLine(1)
		s.buffered.Write("\n", false, OutputNormal)
	}
	return answer, nil
}

func (s *SymfonyStyle) defaultAnswer(q *Question) (any, error) {
	if q.Default == nil {
		return nil, nil
	}
	if q.Validator != nil {
		return q.Validator(q.Default)
	}
	return q.Default, nil
}

func (s *SymfonyStyle) ask(q *Question) (any, error) {
	if !s.input.IsInteractive() {
		return s.defaultAnswer(q)
	}
	var result any
	var err error
	if q.Validator == nil {
		result, err = s.doAsk(q)
	} else {
		result, err = s.validateAttempts(q)
	}
	if _, missing := err.(*MissingInputError); missing {
		s.input.SetInteractive(false)
		fallback, ferr := s.defaultAnswer(q)
		if ferr != nil {
			return nil, ferr
		}
		if fallback == nil {
			return nil, err
		}
		return fallback, nil
	}
	return result, err
}

func (s *SymfonyStyle) validateAttempts(q *Question) (any, error) {
	var lastErr error
	attempts := q.MaxAttempts
	for q.MaxAttempts == 0 || attempts > 0 {
		attempts--
		if lastErr != nil {
			s.NewLine(1)
			s.Error(lastErr.Error())
		}
		value, err := s.doAsk(q)
		if err != nil {
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, err
		}
		v, err := q.Validator(value)
		if err == nil {
			return v, nil
		}
		if ex, ok := err.(Exception); ok && ex.IsConsoleException() {
			if _, isRuntime := err.(*consoleError); isRuntime {
				return nil, err
			}
		}
		lastErr = err
	}
	return nil, lastErr
}

func (s *SymfonyStyle) doAsk(q *Question) (any, error) {
	s.writePrompt(q)
	line, ok := s.reader().readLine()
	if !ok {
		return nil, &MissingInputError{Message: "Aborted."}
	}
	ret := line
	if q.Trimmable {
		ret = strings.Trim(ret, " \t\n\r\x00\x0B")
	}
	var answer any = ret
	if ret == "" {
		answer = q.Default
	}
	if q.Normalizer != nil {
		return q.Normalizer(answer), nil
	}
	if q.StrictNormalizer != nil {
		return q.StrictNormalizer(answer)
	}
	return answer, nil
}

var sharedStdin *stdinReader

func (s *SymfonyStyle) reader() *stdinReader {
	if sharedStdin == nil {
		sharedStdin = &stdinReader{r: s.stdin}
	}
	return sharedStdin
}

// writePrompt ports SymfonyQuestionHelper::writePrompt().
func (s *SymfonyStyle) writePrompt(q *Question) {
	text := EscapeTrailingBackslash(q.Text)
	switch {
	case q.Default == nil:
		text = fmt.Sprintf(" <info>%s</info>:", text)
	case q.confirm:
		d := "no"
		if b, _ := q.Default.(bool); b {
			d = "yes"
		}
		text = fmt.Sprintf(" <info>%s (yes/no)</info> [<comment>%s</comment>]:", text, d)
	case q.choices != nil:
		def := answerString(q.Default)
		for i := range q.choices {
			if q.choiceKey(i) == def {
				def = q.choices[i]
				break
			}
		}
		text = fmt.Sprintf(" <info>%s</info> [<comment>%s</comment>]:", text, def)
	default:
		text = fmt.Sprintf(" <info>%s</info> [<comment>%s</comment>]:", text, Escape(answerString(q.Default)))
	}
	s.Writeln(text, OutputNormal)
	prompt := " > "
	if q.choices != nil {
		maxWidth := 0
		for i := range q.choices {
			maxWidth = max(maxWidth, Width(q.choiceKey(i)))
		}
		for i, c := range q.choices {
			key := q.choiceKey(i)
			s.Writeln(fmt.Sprintf("  [<comment>%s%s</comment>] %s", key, strings.Repeat(" ", maxWidth-Width(key)), c), OutputNormal)
		}
	}
	s.Write(prompt, false, OutputNormal)
}

// Ask ports SymfonyStyle::ask(); a nil default asks without default.
func (s *SymfonyStyle) Ask(question string, def *string, validator func(any) (any, error)) (any, error) {
	var d any
	if def != nil {
		d = *def
	}
	q := NewQuestion(question, d)
	q.Validator = validator
	return s.AskQuestion(q)
}

// Confirm ports SymfonyStyle::confirm().
func (s *SymfonyStyle) Confirm(question string, def bool) (bool, error) {
	v, err := s.AskQuestion(NewConfirmationQuestion(question, def))
	b, _ := v.(bool)
	return b, err
}

// Choice ports SymfonyStyle::choice() for a list of choices; def is a choice value or nil.
func (s *SymfonyStyle) Choice(question string, choices []string, def *string) (string, error) {
	var d any
	if def != nil {
		d = *def
		for i, c := range choices {
			if c == *def {
				d = strconv.Itoa(i)
			}
		}
	}
	v, err := s.AskQuestion(NewChoiceQuestion(question, choices, d))
	str, _ := v.(string)
	return str, err
}
