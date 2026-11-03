package sld

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var attrRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// filterExpr converts an ogc:Filter element into a mapfile logical
// expression, e.g. ("[type]" = "school" AND [pop] > 1000).
func filterExpr(f *node) (string, error) {
	if f == nil || len(f.Nodes) == 0 {
		return "", fmt.Errorf("empty Filter")
	}
	if len(f.Nodes) > 1 {
		return "", fmt.Errorf("Filter must have one root operator")
	}
	return opExpr(&f.Nodes[0])
}

var compareOps = map[string]string{
	"PropertyIsEqualTo":              "=",
	"PropertyIsNotEqualTo":           "!=",
	"PropertyIsLessThan":             "<",
	"PropertyIsLessThanOrEqualTo":    "<=",
	"PropertyIsGreaterThan":          ">",
	"PropertyIsGreaterThanOrEqualTo": ">=",
}

func opExpr(n *node) (string, error) {
	switch op := n.name(); op {
	case "And", "Or":
		if len(n.Nodes) < 2 {
			return "", fmt.Errorf("%s needs at least two operands", op)
		}
		parts := make([]string, 0, len(n.Nodes))
		for i := range n.Nodes {
			p, err := opExpr(&n.Nodes[i])
			if err != nil {
				return "", err
			}
			parts = append(parts, p)
		}
		return "(" + strings.Join(parts, " "+strings.ToUpper(op)+" ") + ")", nil

	case "Not":
		if len(n.Nodes) != 1 {
			return "", fmt.Errorf("Not needs one operand")
		}
		p, err := opExpr(&n.Nodes[0])
		if err != nil {
			return "", err
		}
		return "(NOT " + p + ")", nil

	case "PropertyIsBetween":
		attr, err := propName(n)
		if err != nil {
			return "", err
		}
		lo, _ := n.child("LowerBoundary").expr()
		hi, _ := n.child("UpperBoundary").expr()
		if !isNumber(lo) || !isNumber(hi) {
			return "", fmt.Errorf("PropertyIsBetween needs numeric boundaries")
		}
		return fmt.Sprintf("([%s] >= %s AND [%s] <= %s)", attr, lo, attr, hi), nil

	case "PropertyIsLike":
		attr, err := propName(n)
		if err != nil {
			return "", err
		}
		lit := n.child("Literal").text()
		wild, single, esc := n.attr("wildCard"), n.attr("singleChar"), n.attr("escapeChar")
		if esc == "" {
			esc = n.attr("escape")
		}
		re := likeToRegex(lit, wild, single, esc)
		op := "~"
		if strings.EqualFold(n.attr("matchCase"), "false") {
			op = "~*"
		}
		return fmt.Sprintf(`("[%s]" %s %s)`, attr, op, quoteStr(re)), nil

	case "PropertyIsNull":
		attr, err := propName(n)
		if err != nil {
			return "", err
		}
		// MapServer has no NULL test; NULLs read as empty strings.
		return fmt.Sprintf(`("[%s]" = "")`, attr), nil

	default:
		sym, ok := compareOps[op]
		if !ok {
			return "", fmt.Errorf("filter operator %s is not supported", op)
		}
		return compareExpr(n, sym)
	}
}

func compareExpr(n *node, sym string) (string, error) {
	var attr, lit string
	var haveAttr, haveLit bool
	for i := range n.Nodes {
		c := &n.Nodes[i]
		switch c.name() {
		case "PropertyName":
			attr, haveAttr = c.text(), true
		case "Literal":
			lit, haveLit = c.text(), true
		default:
			return "", fmt.Errorf("%s: %s operands are not supported", n.name(), c.name())
		}
	}
	if !haveAttr || !haveLit {
		return "", fmt.Errorf("%s needs a PropertyName and a Literal", n.name())
	}
	if !attrRe.MatchString(attr) {
		return "", fmt.Errorf("property name %q is not supported", attr)
	}
	if isNumber(lit) {
		return fmt.Sprintf("([%s] %s %s)", attr, sym, strings.TrimSpace(lit)), nil
	}
	matchCase := !strings.EqualFold(n.attr("matchCase"), "false")
	if !matchCase && (sym == "=" || sym == "!=") {
		// Case-insensitive equality via an anchored regex.
		expr := fmt.Sprintf(`("[%s]" ~* %s)`, attr, quoteStr("^"+regexp.QuoteMeta(lit)+"$"))
		if sym == "!=" {
			expr = "(NOT " + expr + ")"
		}
		return expr, nil
	}
	return fmt.Sprintf(`("[%s]" %s %s)`, attr, sym, quoteStr(lit)), nil
}

func propName(n *node) (string, error) {
	attr := n.child("PropertyName").text()
	if !attrRe.MatchString(attr) {
		return "", fmt.Errorf("%s: property name %q is not supported", n.name(), attr)
	}
	return attr, nil
}

func isNumber(s string) bool {
	_, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return err == nil
}

// quoteStr makes a MapServer expression string literal.
func quoteStr(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}

// likeToRegex turns an SQL-LIKE pattern into an anchored regex.
func likeToRegex(pat, wild, single, esc string) string {
	if wild == "" {
		wild = "*"
	}
	if single == "" {
		single = "."
	}
	if esc == "" {
		esc = "!"
	}
	var b strings.Builder
	b.WriteString("^")
	rs := []rune(pat)
	for i := 0; i < len(rs); i++ {
		c := string(rs[i])
		switch {
		case c == esc && i+1 < len(rs):
			i++
			b.WriteString(regexp.QuoteMeta(string(rs[i])))
		case c == wild:
			b.WriteString(".*")
		case c == single:
			b.WriteString(".")
		default:
			b.WriteString(regexp.QuoteMeta(c))
		}
	}
	b.WriteString("$")
	return b.String()
}
