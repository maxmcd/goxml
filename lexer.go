package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

type Keyword string

const (
	kFunction Keyword = "function"
	kReturn           = "return"
	kVar              = "var"
)

// item represents a token returned from the scanner.
type item struct {
	typ itemType // Type, such as itemNumber.
	val string   // Value, such as "23.2".
}

const EOF = 0

func (i item) String() string {
	switch i.typ {
	case itemEOF:
		return "EOF"
	case itemError:
		return i.val
	}
	return fmt.Sprintf("%q", i.val)
}

// Err returns an error if the item is itemError
func (i item) Err() error {
	if i.typ == itemError {
		return fmt.Errorf("%s", i)
	}
	return nil
}

type result struct {
	item  item
	state stateFn
}

// lexer holds the state of the scanner
type lexer struct {
	name    string      // for error reporting
	input   string      // string being scanned
	start   int         // start position of item
	pos     int         // current position in input
	width   int         // width of last rune read from input
	results chan result // channel of (scanned items, next state being returned)
}

type stateFn func(*lexer) stateFn

func lex(name string, input string, startState stateFn) (*lexer, chan result) {
	l := &lexer{
		name:    name,
		input:   input,
		results: make(chan result, 2), // two items is sufficient
	}

	if startState == nil {
		startState = lexText
	}
	go l.run(lexText)
	return l, l.results
}

func (l *lexer) run(startState stateFn) {
	for state := startState; state != nil; {
		state = state(l)
	}
	close(l.results) // no more tokens will be delivered
}

func (l *lexer) emit(t itemType, next stateFn) {
	i := item{t, l.input[l.start:l.pos]}
	l.results <- result{i, next}
	l.start = l.pos
}

// lexIdentifier means we are reading an identifier
func lexIdentifier(l *lexer) stateFn {
	const alphanumeric = "ABCDEFGHIJKLMNOPQRSTUVQXYZabcdefghijklmnopqrstuvwxyz0123456789"
	run := l.acceptRun(alphanumeric)
	switch Keyword(run) {
	case kFunction:
		l.emit(itemFunc, lexText)
	case kReturn:
		l.emit(itemReturn, lexText)
	case kVar:
		l.emit(itemVar, lexText)
	default:
		l.emit(itemIdentifier, lexText)
	}
	return lexText
}

// next returns the next rune in the input
func (l *lexer) next() (r rune) {
	if l.pos >= len(l.input) {
		l.width = 0
		return EOF
	}
	r, l.width = utf8.DecodeRuneInString(l.input[l.pos:])
	l.pos += l.width
	return r
}

// ignore skips over the pending input before this point
func (l *lexer) ignore() {
	l.start = l.pos
}

// backup steps back one rune
// can only be called ONCE per call to next()
func (l *lexer) backup() {
	l.pos -= l.width
}

// peek returns but does not consume
// the next rune in the input.
func (l *lexer) peek() rune {
	r := l.next()
	l.backup()
	return r
}

// accept consumes the next rune
// if it's from the valid set
// and is a no-op if the rune is not in the valid set
func (l *lexer) accept(valid string) bool {
	if strings.IndexRune(valid, l.next()) >= 0 {
		return true
	}
	l.backup()
	return false
}

// acceptRun consumes a run of runes from the valid set
func (l *lexer) acceptRun(valid string) string {
	result := ""
	for next := l.next(); strings.IndexRune(valid, next) >= 0; next = l.next() {
		// accept
		result += string(next)
	}
	l.backup()
	return result
}

func lexNumber(l *lexer) stateFn {
	// optional leading sign
	p := l.peek() // store to check for consecutive operators
	leadingSign := l.accept("+-")

	// If there is a leading sign
	// we need to check if this is a number
	// or an operator
	if leadingSign {
		next := l.peek()
		switch {
		case isWhitespace(string(next)):
			_ = l.next()
			l.emit(itemOperatorPlus, lexText)
			return lexText
		case next == p:
			_ = l.next()
			if next == '+' {
				l.emit(itemIncrement, lexText)
				return lexText
			}
			if next == '-' {
				l.emit(itemIncrement, lexText)
			}
			return l.errorf("unreachable", string(next))
		case isNumeric(next):
			// We know that we're parsing an actual number
			break
		default:
			// e.g. `var a = "foo"+"bar"`
			_ = l.next()
			l.emit(itemOperatorPlus, lexText)
			return lexText
		}
	}

	// is it hex?
	digits := "0123456789"
	if l.accept("0") && l.accept("xX") {
		digits = "0123456789abcdefABCDEF"
	}
	l.acceptRun(digits)
	if l.accept(".") {
		l.acceptRun(digits)
	}

	l.emit(itemNumber, lexText)
	// TODO scientific notation
	return lexText
}

func lexText(l *lexer) stateFn {
	// accept leading whitespace
	for {
		switch r := l.next(); {
		case isSpace(r):
			l.ignore()
		case r == '{':
			l.emit(itemLeftBrace, lexText)
			return lexText
		case r == '}':
			l.emit(itemRightBrace, lexText)
			return lexText
		case r == '[':
			l.emit(itemLeftSquareBracket, lexText)
			return lexText
		case r == ']':
			l.emit(itemRightSquareBracket, lexText)
			return lexText
		case r == '"':
			return lexDoubleQuote
		case r == '\'':
			return lexSingleQuote
		case r == '=':
			l.emit(itemAssignment, lexText) // TODO '==' and '===' equality check
			return lexText
		case r == '+' || r == '-' || '0' <= r && r <= '9':
			l.backup()
			return lexNumber
		case r == EOF:
			l.emit(itemEOF, nil)
			return nil
		case isAlphaNumeric(r):
			l.backup()
			return lexIdentifier
		case r == ':':
			l.emit(itemColon, lexText)
			return lexText
		case r == ',':
			l.emit(itemComma, lexText)
			return lexText
		case r == '(':
			l.emit(itemLeftParen, lexText)
		case r == ')':
			l.emit(itemRightParen, lexText)
		case r == ';':
			l.emit(itemSemicolon, lexText)
		default:
			return l.errorf("unexpected token: %s", string(r))
		}
	}
}

// error returns an error token and terminates the scan
// by passing back a nil pointer that will be the next state
// thereby terminating l.run
func (l *lexer) errorf(format string, args ...interface{}) stateFn {
	l.results <- result{item{
		itemError,
		fmt.Sprintf(format, args...),
	}, nil}

	return nil
}

func lexDoubleQuote(l *lexer) stateFn {
	// TODO use a 'reject' function to simplify this
	// TODO account for unexpected EOF
	for {
		next := l.next()
		for next != '\\' && next != '"' && next != EOF {
			next = l.next()
		}

		// If the token we broke on
		// is an escape character,
		// accept the next one unconditionally
		if next == '\\' {
			l.next()
			continue
		}

		// If the token we broke on
		// is a quotation mark
		// we are done
		if next == '"' {
			break
		}

		if next == EOF {
			l.errorf("unexpected EOF inside quoted string")
			break
		}
	}
	l.emit(itemDoubleQuote, lexText)
	return lexText
}

func lexSingleQuote(l *lexer) stateFn {
	// TODO use a 'reject' function to simplify this
	// TODO account for unexpected EOF
	for {
		next := l.next()
		for next != '\\' && next != '\'' && next != EOF {
			next = l.next()
		}

		// If the token we broke on
		// is an escape character,
		// accept the next one unconditionally
		if next == '\\' {
			l.next()
			continue
		}

		// If the token we broke on
		// is a quotation mark
		// we are done
		if next == '\'' {
			break
		}

		if next == EOF {
			l.errorf("unexpected EOF inside quoted string")
		}
	}
	l.emit(itemSingleQuote, lexText)
	return lexText
}

func isSpace(r rune) bool {
	return len(strings.TrimSpace(string(r))) == 0

}

func isAlphaNumeric(r rune) bool {
	reg := regexp.MustCompile("[A-Za-z0-9]")
	s := string(r)
	return reg.Match([]byte(s))
}

func isNumeric(r rune) bool {
	reg := regexp.MustCompile("[0-9]")
	s := string(r)
	return reg.Match([]byte(s))
}

// isWhitespace is a convenience function
// that returns true if all characters in s
// are whitespace, as defined by Unicode
func isWhitespace(s string) bool {
	return len(strings.TrimSpace(s)) == 0
}

/*
func main() {
	_, results := lex("testLex", `{"a":5, b : 'foo' }`, nil)
	for result := range results {
        item := result.item
		if err := item.Err(); err != nil {
			log.Fatalf("error: %s", err)
		}
		log.Printf("Received %+v", item)
	}
}
*/
