package browser

import (
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

// Reader output is Markdown, never source HTML. Only HTTP(S) destinations survive.
func webURL(base *url.URL, raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	u, err := base.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return ""
	}
	return u.String()
}
func mdText(s string) string {
	r := strings.NewReplacer("\\", "\\\\", "[", "\\[", "]", "\\]", "*", "\\*", "_", "\\_", "`", "\\`", "<", "&lt;", ">", "&gt;", "#", "\\#", "!", "\\!")
	return r.Replace(s)
}
func mdURL(s string) string {
	return strings.NewReplacer("(", "%28", ")", "%29", " ", "%20", "<", "%3C", ">", "%3E", "\n", "%0A", "\r", "%0D").Replace(s)
}
func readerMarkdown(root *html.Node, base *url.URL) string {
	var render func(*html.Node) string
	render = func(n *html.Node) string {
		if n.Type == html.TextNode {
			return mdText(strings.Join(strings.Fields(n.Data), " "))
		}
		if n.Type == html.ElementNode && (skip[n.Data] || chrome[n.Data] || n.Data == "footer" || n.Data == "input" || hasAttr(n, "hidden") || attr(n, "aria-hidden") == "true") {
			return ""
		}
		var parts []string
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			if s := render(c); s != "" {
				parts = append(parts, s)
			}
		}
		body := strings.Join(parts, " ")
		switch n.Data {
		case "a":
			if u := webURL(base, attr(n, "href")); u != "" && body != "" {
				return "[" + strings.TrimSpace(body) + "](" + mdURL(u) + ")"
			}
			return body
		case "img":
			if u := webURL(base, attr(n, "src")); u != "" {
				return "\n\n![" + mdText(strings.Join(strings.Fields(attr(n, "alt")), " ")) + "](" + mdURL(u) + ")\n\n"
			}
			return ""
		case "h1", "h2", "h3", "h4", "h5", "h6":
			return "\n\n" + strings.Repeat("#", int(n.Data[1]-'0')) + " " + strings.TrimSpace(body) + "\n\n"
		case "strong", "b":
			return "**" + body + "**"
		case "em", "i":
			return "*" + body + "*"
		case "li":
			return "\n- " + strings.TrimSpace(body)
		case "pre":
			return "\n\n    " + strings.ReplaceAll(strings.TrimSpace(textOf(n)), "\n", "\n    ") + "\n\n"
		case "br":
			return "\n\n"
		}
		if block[n.Data] {
			return "\n\n" + strings.TrimSpace(body) + "\n\n"
		}
		return body
	}
	var out []string
	blank := false
	for _, line := range strings.Split(render(root), "\n") {
		line = strings.TrimSpace(line)
		if line == "" && blank {
			continue
		}
		blank = line == ""
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
}

// Score unmarked content containers by paragraph text, penalizing link-heavy navigation.
// Metrics are accumulated once per node rather than rescanning every ancestor's subtree.
func readableContent(root *html.Node) *html.Node {
	type metrics struct{ text, linked, paragraphs int }
	var best *html.Node
	bestScore := 0
	var visit func(*html.Node) metrics
	visit = func(n *html.Node) metrics {
		if n.Type == html.TextNode {
			return metrics{text: len(strings.TrimSpace(n.Data))}
		}
		if skip[n.Data] || chrome[n.Data] || n.Data == "footer" {
			return metrics{}
		}
		m := metrics{}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			v := visit(c)
			m.text += v.text
			m.linked += v.linked
			m.paragraphs += v.paragraphs
		}
		if n.Data == "a" {
			m.linked = m.text
		}
		if n.Data == "p" && m.text > 40 {
			m.paragraphs++
		}
		if n.Data == "div" || n.Data == "section" || n.Data == "article" || n.Data == "main" {
			score := m.text - 3*m.linked + 40*m.paragraphs
			if m.paragraphs > 0 && score > bestScore {
				best, bestScore = n, score
			}
		}
		return m
	}
	visit(root)
	return best
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}
