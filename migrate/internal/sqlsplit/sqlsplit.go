package sqlsplit

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Split splits sql into into a slice of strings each containing one SQL statement.
func Split(sql string) []string {
	l := &sqlLexer{
		src:     sql,
		stateFn: rawState,
	}

	for l.stateFn != nil {
		l.stateFn = l.stateFn(l)
	}

	if len(l.statements) == 0 {
		l.statements = []string{sql}
	}

	return l.statements
}

type sqlLexer struct {
	src     string
	start   int
	pos     int
	nested  int // multiline comment nesting level.
	stateFn stateFn

	statements []string
}

func (l *sqlLexer) addStatement(s string) {
	s = strings.TrimSpace(s)
	if len(s) > 0 {
		l.statements = append(l.statements, s)
	}
}

type stateFn func(*sqlLexer) stateFn

func rawState(l *sqlLexer) stateFn {
	for {
		r, width := utf8.DecodeRuneInString(l.src[l.pos:])
		l.pos += width

		switch r {
		case 'e', 'E':
			nextRune, width := utf8.DecodeRuneInString(l.src[l.pos:])
			if nextRune == '\'' {
				l.pos += width
				return escapeStringState
			}
		case '\'':
			return singleQuoteState
		case '"':
			return doubleQuoteState
		case '$':
			tag, ok := readDollarTag(l.src[l.pos:])
			if ok {
				l.pos += len(tag) + 1 // tag + "$"
				return dollarQuoteState(tag)
			}
		case ';':
			l.addStatement(l.src[l.start:l.pos])
			l.start = l.pos
			return rawState
		case '-':
			nextRune, width := utf8.DecodeRuneInString(l.src[l.pos:])
			if nextRune == '-' {
				l.pos += width
				return oneLineCommentState
			}
		case '/':
			nextRune, width := utf8.DecodeRuneInString(l.src[l.pos:])
			if nextRune == '*' {
				l.pos += width
				return multilineCommentState
			}
		case utf8.RuneError:
			if l.pos-l.start > 0 {
				l.addStatement(l.src[l.start:l.pos])
				l.start = l.pos
			}
			return nil
		}
	}
}

func singleQuoteState(l *sqlLexer) stateFn {
	for {
		r, width := utf8.DecodeRuneInString(l.src[l.pos:])
		l.pos += width

		switch r {
		case '\'':
			nextRune, width := utf8.DecodeRuneInString(l.src[l.pos:])
			if nextRune != '\'' {
				return rawState
			}
			l.pos += width
		case utf8.RuneError:
			if l.pos-l.start > 0 {
				l.addStatement(l.src[l.start:l.pos])
				l.start = l.pos
			}
			return nil
		}
	}
}

func doubleQuoteState(l *sqlLexer) stateFn {
	for {
		r, width := utf8.DecodeRuneInString(l.src[l.pos:])
		l.pos += width

		switch r {
		case '"':
			nextRune, width := utf8.DecodeRuneInString(l.src[l.pos:])
			if nextRune != '"' {
				return rawState
			}
			l.pos += width
		case utf8.RuneError:
			if l.pos-l.start > 0 {
				l.addStatement(l.src[l.start:l.pos])
				l.start = l.pos
			}
			return nil
		}
	}
}

func dollarQuoteState(openingTag string) func(l *sqlLexer) stateFn {
	return func(l *sqlLexer) stateFn {
		for {
			r, width := utf8.DecodeRuneInString(l.src[l.pos:])
			l.pos += width

			switch r {
			case '$':
				tag, ok := readDollarTag(l.src[l.pos:])
				if ok && tag == openingTag {
					l.pos += len(tag) + 1 // tag + "$"
					return rawState
				}
				l.pos += width
			case utf8.RuneError:
				if l.pos-l.start > 0 {
					l.addStatement(l.src[l.start:l.pos])
					l.start = l.pos
				}
				return nil
			}
		}
	}
}

func readDollarTag(src string) (tag string, ok bool) {
	nextRune, width := utf8.DecodeRuneInString(src)
	if nextRune == '$' {
		return "", true
	}

	if !unicode.IsLetter(nextRune) && nextRune != '_' {
		// Not a valid identifier. Perhaps it's a positional parameter like $1.
		return "", false
	}

	tagWidth := width
	for {
		nextRune, width := utf8.DecodeRuneInString(src[tagWidth:])
		if nextRune == '$' {
			return src[:tagWidth], true
		} else if unicode.IsLetter(nextRune) || nextRune == '_' || ('0' <= nextRune && nextRune <= '9') {
			tagWidth += width
		} else {
			// Unexpected rune or end of string. This is not a valid identifier, bail out.
			return "", false
		}
	}
}

func escapeStringState(l *sqlLexer) stateFn {
	for {
		r, width := utf8.DecodeRuneInString(l.src[l.pos:])
		l.pos += width

		switch r {
		case '\\':
			_, width = utf8.DecodeRuneInString(l.src[l.pos:])
			l.pos += width
		case '\'':
			nextRune, width := utf8.DecodeRuneInString(l.src[l.pos:])
			if nextRune != '\'' {
				return rawState
			}
			l.pos += width
		case utf8.RuneError:
			if l.pos-l.start > 0 {
				l.addStatement(l.src[l.start:l.pos])
				l.start = l.pos
			}
			return nil
		}
	}
}

func oneLineCommentState(l *sqlLexer) stateFn {
	for {
		r, width := utf8.DecodeRuneInString(l.src[l.pos:])
		l.pos += width

		switch r {
		case '\\':
			_, width = utf8.DecodeRuneInString(l.src[l.pos:])
			l.pos += width
		case '\n', '\r':
			return rawState
		case utf8.RuneError:
			if l.pos-l.start > 0 {
				l.addStatement(l.src[l.start:l.pos])
				l.start = l.pos
			}
			return nil
		}
	}
}

func multilineCommentState(l *sqlLexer) stateFn {
	for {
		r, width := utf8.DecodeRuneInString(l.src[l.pos:])
		l.pos += width

		switch r {
		case '/':
			nextRune, width := utf8.DecodeRuneInString(l.src[l.pos:])
			if nextRune == '*' {
				l.pos += width
				l.nested++
			}
		case '*':
			nextRune, width := utf8.DecodeRuneInString(l.src[l.pos:])
			if nextRune != '/' {
				continue
			}

			l.pos += width
			if l.nested == 0 {
				return rawState
			}
			l.nested--

		case utf8.RuneError:
			if l.pos-l.start > 0 {
				l.addStatement(l.src[l.start:l.pos])
				l.start = l.pos
			}
			return nil
		}
	}
}
