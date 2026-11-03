package sld

import (
	"fmt"
	"strings"
	"unicode"
)

// cqlToExpr converts a small CQL subset (used by the JSON style format) to
// a mapfile expression:
//
//	type = 'school' AND pop >= 1000
//	name LIKE 'Ko%' OR NOT (kind = 'x')      -- IN is not supported
//	height BETWEEN 10 AND 20, code IS NULL, code IS NOT NULL
func cqlToExpr(s string) (string, error) {
	toks, err := lexCQL(s)
	if err != nil {
		return "", err
	}
	p := &cqlParser{toks: toks}
	e, err := p.or()
	if err != nil {
		return "", err
	}
	if p.pos != len(p.toks) {
		return "", fmt.Errorf("unexpected %q in filter", p.toks[p.pos].v)
	}
	return e, nil
}

type tokKind int

const (
	tIdent tokKind = iota
	tString
	tNumber
	tOp
	tLParen
	tRParen
)

type tok struct {
	k tokKind
	v string
}

func lexCQL(s string) ([]tok, error) {
	var out []tok
	rs := []rune(s)
	for i := 0; i < len(rs); {
		c := rs[i]
		switch {
		case unicode.IsSpace(c):
			i++
		case c == '(':
			out = append(out, tok{tLParen, "("})
			i++
		case c == ')':
			out = append(out, tok{tRParen, ")"})
			i++
		case c == '\'':
			var b strings.Builder
			i++
			for {
				if i >= len(rs) {
					return nil, fmt.Errorf("unterminated string in filter")
				}
				if rs[i] == '\'' {
					if i+1 < len(rs) && rs[i+1] == '\'' { // '' escape
						b.WriteRune('\'')
						i += 2
						continue
					}
					i++
					break
				}
				b.WriteRune(rs[i])
				i++
			}
			out = append(out, tok{tString, b.String()})
		case c == '<' || c == '>' || c == '=' || c == '!':
			j := i + 1
			if j < len(rs) && (rs[j] == '=' || (c == '<' && rs[j] == '>')) {
				j++
			}
			out = append(out, tok{tOp, string(rs[i:j])})
			i = j
		case unicode.IsDigit(c) || c == '-' || c == '.':
			j := i + 1
			for j < len(rs) && (unicode.IsDigit(rs[j]) || rs[j] == '.' || rs[j] == 'e' || rs[j] == 'E') {
				j++
			}
			out = append(out, tok{tNumber, string(rs[i:j])})
			i = j
		case unicode.IsLetter(c) || c == '_' || c == '"':
			if c == '"' { // quoted identifier
				j := i + 1
				for j < len(rs) && rs[j] != '"' {
					j++
				}
				if j >= len(rs) {
					return nil, fmt.Errorf("unterminated quoted name in filter")
				}
				out = append(out, tok{tIdent, string(rs[i+1 : j])})
				i = j + 1
				continue
			}
			j := i + 1
			for j < len(rs) && (unicode.IsLetter(rs[j]) || unicode.IsDigit(rs[j]) || rs[j] == '_') {
				j++
			}
			out = append(out, tok{tIdent, string(rs[i:j])})
			i = j
		default:
			return nil, fmt.Errorf("unexpected character %q in filter", c)
		}
	}
	return out, nil
}

type cqlParser struct {
	toks []tok
	pos  int
}

func (p *cqlParser) peek() *tok {
	if p.pos < len(p.toks) {
		return &p.toks[p.pos]
	}
	return nil
}

func (p *cqlParser) keyword(kw string) bool {
	t := p.peek()
	if t != nil && t.k == tIdent && strings.EqualFold(t.v, kw) {
		p.pos++
		return true
	}
	return false
}

func (p *cqlParser) or() (string, error) {
	left, err := p.and()
	if err != nil {
		return "", err
	}
	for p.keyword("OR") {
		right, err := p.and()
		if err != nil {
			return "", err
		}
		left = "(" + left + " OR " + right + ")"
	}
	return left, nil
}

func (p *cqlParser) and() (string, error) {
	left, err := p.not()
	if err != nil {
		return "", err
	}
	for p.keyword("AND") {
		right, err := p.not()
		if err != nil {
			return "", err
		}
		left = "(" + left + " AND " + right + ")"
	}
	return left, nil
}

func (p *cqlParser) not() (string, error) {
	if p.keyword("NOT") {
		e, err := p.not()
		if err != nil {
			return "", err
		}
		return "(NOT " + e + ")", nil
	}
	return p.primary()
}

func (p *cqlParser) primary() (string, error) {
	t := p.peek()
	if t == nil {
		return "", fmt.Errorf("filter ends too early")
	}
	if t.k == tLParen {
		p.pos++
		e, err := p.or()
		if err != nil {
			return "", err
		}
		if t := p.peek(); t == nil || t.k != tRParen {
			return "", fmt.Errorf("missing ) in filter")
		}
		p.pos++
		return e, nil
	}
	if t.k != tIdent || !attrRe.MatchString(t.v) {
		return "", fmt.Errorf("expected an attribute name, got %q", t.v)
	}
	attr := t.v
	p.pos++

	switch {
	case p.keyword("IS"):
		neg := p.keyword("NOT")
		if !p.keyword("NULL") {
			return "", fmt.Errorf("expected NULL after IS")
		}
		e := fmt.Sprintf(`("[%s]" = "")`, attr)
		if neg {
			e = fmt.Sprintf(`("[%s]" != "")`, attr)
		}
		return e, nil
	case p.keyword("BETWEEN"):
		lo := p.peek()
		if lo == nil || lo.k != tNumber {
			return "", fmt.Errorf("BETWEEN needs numbers")
		}
		p.pos++
		if !p.keyword("AND") {
			return "", fmt.Errorf("expected AND in BETWEEN")
		}
		hi := p.peek()
		if hi == nil || hi.k != tNumber {
			return "", fmt.Errorf("BETWEEN needs numbers")
		}
		p.pos++
		return fmt.Sprintf("([%s] >= %s AND [%s] <= %s)", attr, lo.v, attr, hi.v), nil
	case p.keyword("LIKE"), p.keyword("ILIKE"):
		ci := strings.EqualFold(p.toks[p.pos-1].v, "ILIKE")
		v := p.peek()
		if v == nil || v.k != tString {
			return "", fmt.Errorf("LIKE needs a string")
		}
		p.pos++
		op := "~"
		if ci {
			op = "~*"
		}
		return fmt.Sprintf(`("[%s]" %s %s)`, attr, op, quoteStr(likeToRegex(v.v, "%", "_", "\\"))), nil
	}

	opTok := p.peek()
	if opTok == nil || opTok.k != tOp {
		return "", fmt.Errorf("expected a comparison after %s", attr)
	}
	p.pos++
	op := opTok.v
	if op == "<>" {
		op = "!="
	}
	switch op {
	case "=", "!=", "<", "<=", ">", ">=":
	default:
		return "", fmt.Errorf("operator %q not supported", opTok.v)
	}
	v := p.peek()
	if v == nil || (v.k != tString && v.k != tNumber) {
		return "", fmt.Errorf("expected a value after %s %s", attr, opTok.v)
	}
	p.pos++
	if v.k == tNumber {
		return fmt.Sprintf("([%s] %s %s)", attr, op, v.v), nil
	}
	return fmt.Sprintf(`("[%s]" %s %s)`, attr, op, quoteStr(v.v)), nil
}
