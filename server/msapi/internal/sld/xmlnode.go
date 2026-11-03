// Package sld converts SLD 1.0 / SE 1.1 (and our JSON style shortcut) into
// mapfile CLASS blocks (PLAN.md §6). Styles are parsed into a small model
// (model.go) and rendered per layer type (render.go).
package sld

import (
	"bytes"
	"encoding/xml"
	"strings"
)

// node is a generic XML element. Namespaces are ignored (SLD 1.0 uses sld:/
// ogc:, SE 1.1 uses se:; the local names are what matter) and child order
// is kept, which matters for symbolizers.
type node struct {
	XMLName xml.Name
	Attrs   []xml.Attr `xml:",any,attr"`
	Nodes   []node     `xml:",any"`
	Text    string     `xml:",chardata"`
	Inner   []byte     `xml:",innerxml"`
}

func parseXML(data []byte) (*node, error) {
	var n node
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = true
	if err := dec.Decode(&n); err != nil {
		return nil, err
	}
	return &n, nil
}

func (n *node) name() string { return n.XMLName.Local }

// child returns the first direct child with the local name, or nil.
func (n *node) child(name string) *node {
	if n == nil {
		return nil
	}
	for i := range n.Nodes {
		if n.Nodes[i].XMLName.Local == name {
			return &n.Nodes[i]
		}
	}
	return nil
}

// children returns all direct children with the local name.
func (n *node) children(name string) []*node {
	if n == nil {
		return nil
	}
	var out []*node
	for i := range n.Nodes {
		if n.Nodes[i].XMLName.Local == name {
			out = append(out, &n.Nodes[i])
		}
	}
	return out
}

// path follows nested children: n.path("Graphic", "Mark").
func (n *node) path(names ...string) *node {
	cur := n
	for _, nm := range names {
		cur = cur.child(nm)
		if cur == nil {
			return nil
		}
	}
	return cur
}

func (n *node) attr(name string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.Attrs {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// text is the trimmed text of the element and its descendants.
func (n *node) text() string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	var walk func(*node)
	walk = func(x *node) {
		b.WriteString(x.Text)
		for i := range x.Nodes {
			walk(&x.Nodes[i])
		}
	}
	walk(n)
	return strings.TrimSpace(b.String())
}

// expr is the value of an SE "ParameterValueType": plain text, ogc:Literal,
// or ogc:PropertyName (returned as an attribute binding "[name]").
// dynamic reports whether a PropertyName or function was used.
func (n *node) expr() (value string, dynamic bool) {
	if n == nil {
		return "", false
	}
	if len(n.Nodes) == 0 {
		return strings.TrimSpace(n.Text), false
	}
	var parts []string
	for i := range n.Nodes {
		c := &n.Nodes[i]
		switch c.name() {
		case "Literal":
			parts = append(parts, c.text())
		case "PropertyName":
			parts = append(parts, "["+c.text()+"]")
			dynamic = true
		default:
			dynamic = true // functions etc.: caller warns
			parts = append(parts, c.text())
		}
	}
	if t := strings.TrimSpace(n.Text); t != "" {
		parts = append([]string{t}, parts...)
	}
	return strings.Join(parts, ""), dynamic
}

// params collects CssParameter / SvgParameter children by name.
func (n *node) params() map[string]*node {
	out := map[string]*node{}
	if n == nil {
		return out
	}
	for i := range n.Nodes {
		c := &n.Nodes[i]
		if c.name() == "CssParameter" || c.name() == "SvgParameter" {
			out[c.attr("name")] = c
		}
	}
	return out
}
