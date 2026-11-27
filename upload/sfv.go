package upload

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// See https://www.rfc-editor.org/rfc/rfc9651

const maxInteger = 999_999_999_999_999

var errMalformed = errors.New("malformed structured field")

// Dictionary is a Structured Field Dictionary (RFC 9651 Section 3.2), its
// members in field order with each key at most once.
type Dictionary []Member

// Member is one Dictionary member. Its Value is an int64 for an Integer, a
// bool for a Boolean, or a []byte for a Byte Sequence. [ParseDictionary]
// gives a well-formed member of any other type, an Inner List among them, a
// nil Value, so a key present with a type outside these three is told apart
// from an absent key. A member's parameters are not kept.
type Member struct {
	Key   string
	Value any
}

// ParseInteger parses field as an Item of type Integer (RFC 9651 Section
// 3.3.1) and reports whether it is one. A malformed field, or an Item of
// any other type, reports false. The Item's parameters are parsed and
// ignored.
func ParseInteger(field string) (int64, bool) {
	v, err := parseField(field, (*parser).item)
	n, ok := v.(int64)
	return n, err == nil && ok
}

// ParseBoolean parses field as an Item of type Boolean (RFC 9651 Section
// 3.3.6) and reports whether it is one. A malformed field, or an Item of
// any other type, reports false. The Item's parameters are parsed and
// ignored.
func ParseBoolean(field string) (bool, bool) {
	v, err := parseField(field, (*parser).item)
	b, ok := v.(bool)
	return b, err == nil && ok
}

// ParseDictionary parses field as a Dictionary (RFC 9651 Section 3.2) and
// reports whether it is well-formed; a malformed field reports false. A
// key that appears more than once keeps its first position and its last
// value. An empty field is an empty Dictionary.
func ParseDictionary(field string) (Dictionary, bool) {
	v, err := parseField(field, (*parser).dictionary)
	if err != nil {
		return nil, false
	}
	return v.(Dictionary), true
}

// FormatInteger serializes n as an Integer Item. An n outside
// ±999,999,999,999,999 cannot be serialized and returns an error.
func FormatInteger(n int64) (string, error) {
	if n < -maxInteger || n > maxInteger {
		return "", fmt.Errorf("upload: integer %d is out of the structured field range", n)
	}
	return strconv.FormatInt(n, 10), nil
}

// FormatBoolean serializes b as a Boolean Item: ?1 or ?0.
func FormatBoolean(b bool) string {
	if b {
		return "?1"
	}
	return "?0"
}

// FormatDictionary serializes d as a Dictionary, a true Boolean member as
// its bare key. A malformed or repeated key, an Integer out of range, or a
// Value of a type other than int64, bool, or []byte returns an error. An
// empty d serializes as the empty string, which a sender omits.
func FormatDictionary(d Dictionary) (string, error) {
	var b strings.Builder
	seen := make(map[string]bool, len(d))
	for i, m := range d {
		if !validKey(m.Key) {
			return "", fmt.Errorf("upload: dictionary key %q is not a structured field key", m.Key)
		}
		if seen[m.Key] {
			return "", fmt.Errorf("upload: dictionary key %q repeats", m.Key)
		}
		seen[m.Key] = true
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(m.Key)
		switch v := m.Value.(type) {
		case int64:
			s, err := FormatInteger(v)
			if err != nil {
				return "", err
			}
			b.WriteString("=" + s)
		case bool:
			if !v {
				b.WriteString("=?0")
			}
		case []byte:
			b.WriteString("=:" + base64.StdEncoding.EncodeToString(v) + ":")
		default:
			return "", fmt.Errorf("upload: dictionary member %q has a value of type %T", m.Key, m.Value)
		}
	}
	return b.String(), nil
}

func validKey(k string) bool {
	if k == "" || !isKeyStart(k[0]) {
		return false
	}
	for i := 1; i < len(k); i++ {
		if !isKeyChar(k[i]) {
			return false
		}
	}
	return true
}

type parser struct {
	s string
}

func parseField(field string, parse func(*parser) (any, error)) (any, error) {
	p := &parser{s: strings.Trim(field, " ")}
	v, err := parse(p)
	if err != nil {
		return nil, err
	}
	if p.s != "" {
		return nil, errMalformed
	}
	return v, nil
}

func (p *parser) peek() (byte, bool) {
	if p.s == "" {
		return 0, false
	}
	return p.s[0], true
}

func (p *parser) skipSP() {
	p.s = strings.TrimLeft(p.s, " ")
}

func (p *parser) skipOWS() {
	p.s = strings.TrimLeft(p.s, " \t")
}

func (p *parser) dictionary() (any, error) {
	d := Dictionary{}
	index := map[string]int{}
	for p.s != "" {
		key, err := p.key()
		if err != nil {
			return nil, err
		}
		var value any = true
		if c, _ := p.peek(); c == '=' {
			p.s = p.s[1:]
			value, err = p.itemOrInnerList()
		} else {
			err = p.parameters()
		}
		if err != nil {
			return nil, err
		}
		if i, ok := index[key]; ok {
			d[i].Value = value
		} else {
			index[key] = len(d)
			d = append(d, Member{Key: key, Value: value})
		}
		p.skipOWS()
		if p.s == "" {
			break
		}
		if p.s[0] != ',' {
			return nil, errMalformed
		}
		p.s = p.s[1:]
		p.skipOWS()
		if p.s == "" {
			return nil, errMalformed
		}
	}
	return d, nil
}

func (p *parser) itemOrInnerList() (any, error) {
	if c, _ := p.peek(); c == '(' {
		return nil, p.innerList()
	}
	return p.item()
}

func (p *parser) innerList() error {
	p.s = p.s[1:]
	for p.s != "" {
		p.skipSP()
		if c, _ := p.peek(); c == ')' {
			p.s = p.s[1:]
			return p.parameters()
		}
		if _, err := p.item(); err != nil {
			return err
		}
		if c, ok := p.peek(); !ok || (c != ' ' && c != ')') {
			return errMalformed
		}
	}
	return errMalformed
}

func (p *parser) item() (any, error) {
	v, err := p.bareItem()
	if err != nil {
		return nil, err
	}
	return v, p.parameters()
}

func (p *parser) parameters() error {
	for {
		if c, _ := p.peek(); c != ';' {
			return nil
		}
		p.s = p.s[1:]
		p.skipSP()
		if _, err := p.key(); err != nil {
			return err
		}
		if c, _ := p.peek(); c == '=' {
			p.s = p.s[1:]
			if _, err := p.bareItem(); err != nil {
				return err
			}
		}
	}
}

func (p *parser) key() (string, error) {
	c, ok := p.peek()
	if !ok || !isKeyStart(c) {
		return "", errMalformed
	}
	i := 1
	for i < len(p.s) && isKeyChar(p.s[i]) {
		i++
	}
	k := p.s[:i]
	p.s = p.s[i:]
	return k, nil
}

func (p *parser) bareItem() (any, error) {
	c, ok := p.peek()
	switch {
	case !ok:
		return nil, errMalformed
	case c == '-' || isDigit(c):
		return p.number()
	case c == '"':
		return nil, p.str()
	case c == '*' || isAlpha(c):
		p.token()
		return nil, nil
	case c == ':':
		return p.byteSequence()
	case c == '?':
		return p.boolean()
	case c == '@':
		return nil, p.date()
	case c == '%':
		return nil, p.displayString()
	}
	return nil, errMalformed
}

func (p *parser) number() (any, error) {
	neg := false
	if p.s[0] == '-' {
		neg = true
		p.s = p.s[1:]
	}
	if c, ok := p.peek(); !ok || !isDigit(c) {
		return nil, errMalformed
	}
	i, dot := 0, -1
	for ; i < len(p.s); i++ {
		c := p.s[i]
		if c == '.' && dot < 0 {
			if i > 12 {
				return nil, errMalformed
			}
			dot = i
		} else if !isDigit(c) {
			break
		}
		if dot < 0 && i+1 > 15 || dot >= 0 && i+1 > 16 {
			return nil, errMalformed
		}
	}
	digits := p.s[:i]
	p.s = p.s[i:]
	if dot >= 0 {
		if frac := len(digits) - dot - 1; frac < 1 || frac > 3 {
			return nil, errMalformed
		}
		return nil, nil
	}
	n, err := strconv.ParseInt(digits, 10, 64)
	if err != nil {
		return nil, errMalformed
	}
	if neg {
		n = -n
	}
	return n, nil
}

func (p *parser) str() error {
	for i := 1; i < len(p.s); i++ {
		switch c := p.s[i]; {
		case c == '\\':
			i++
			if i == len(p.s) || (p.s[i] != '"' && p.s[i] != '\\') {
				return errMalformed
			}
		case c == '"':
			p.s = p.s[i+1:]
			return nil
		case c < 0x20 || c > 0x7e:
			return errMalformed
		}
	}
	return errMalformed
}

func (p *parser) token() {
	i := 1
	for i < len(p.s) && (isTChar(p.s[i]) || p.s[i] == ':' || p.s[i] == '/') {
		i++
	}
	p.s = p.s[i:]
}

func (p *parser) byteSequence() (any, error) {
	end := strings.IndexByte(p.s[1:], ':')
	if end < 0 {
		return nil, errMalformed
	}
	enc := p.s[1 : end+1]
	for i := 0; i < len(enc); i++ {
		if !isBase64Char(enc[i]) {
			return nil, errMalformed
		}
	}
	b, err := base64.StdEncoding.Strict().DecodeString(enc)
	if err != nil {
		return nil, errMalformed
	}
	p.s = p.s[end+2:]
	return b, nil
}

func (p *parser) boolean() (any, error) {
	if len(p.s) < 2 || (p.s[1] != '0' && p.s[1] != '1') {
		return nil, errMalformed
	}
	b := p.s[1] == '1'
	p.s = p.s[2:]
	return b, nil
}

func (p *parser) date() error {
	p.s = p.s[1:]
	if c, ok := p.peek(); !ok || (c != '-' && !isDigit(c)) {
		return errMalformed
	}
	v, err := p.number()
	if err != nil {
		return err
	}
	if _, ok := v.(int64); !ok {
		return errMalformed
	}
	return nil
}

func (p *parser) displayString() error {
	if len(p.s) < 2 || p.s[1] != '"' {
		return errMalformed
	}
	var b []byte
	for i := 2; i < len(p.s); i++ {
		switch c := p.s[i]; {
		case c == '%':
			if i+2 >= len(p.s) || !isLCHex(p.s[i+1]) || !isLCHex(p.s[i+2]) {
				return errMalformed
			}
			n, _ := strconv.ParseUint(p.s[i+1:i+3], 16, 8)
			b = append(b, byte(n))
			i += 2
		case c == '"':
			if !utf8.Valid(b) {
				return errMalformed
			}
			p.s = p.s[i+1:]
			return nil
		case c < 0x20 || c > 0x7e:
			return errMalformed
		default:
			b = append(b, c)
		}
	}
	return errMalformed
}

func isDigit(c byte) bool   { return '0' <= c && c <= '9' }
func isLCAlpha(c byte) bool { return 'a' <= c && c <= 'z' }
func isAlpha(c byte) bool   { return isLCAlpha(c) || 'A' <= c && c <= 'Z' }
func isLCHex(c byte) bool   { return isDigit(c) || 'a' <= c && c <= 'f' }

func isKeyStart(c byte) bool { return isLCAlpha(c) || c == '*' }

func isBase64Char(c byte) bool {
	return isAlpha(c) || isDigit(c) || c == '+' || c == '/' || c == '='
}

func isKeyChar(c byte) bool {
	return isLCAlpha(c) || isDigit(c) || c == '_' || c == '-' || c == '.' || c == '*'
}

func isTChar(c byte) bool {
	return isAlpha(c) || isDigit(c) || strings.IndexByte("!#$%&'*+-.^_`|~", c) >= 0
}
