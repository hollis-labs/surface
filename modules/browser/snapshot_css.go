package browser

import (
	"net/url"
	"strings"

	"github.com/tdewolff/parse/v2"
	"github.com/tdewolff/parse/v2/css"
)

// An intentionally static subset. No custom properties, executable legacy properties,
// fonts, animation, arbitrary functions or browser-side resource resolution.
var snapshotProperties = words(`color background background-color background-image background-position background-size background-repeat background-origin background-clip
 border border-color border-width border-style border-radius border-top border-right border-bottom border-left border-collapse border-spacing
 box-shadow box-sizing display visibility opacity overflow overflow-x overflow-y
 width height min-width max-width min-height max-height margin margin-top margin-right margin-bottom margin-left padding padding-top padding-right padding-bottom padding-left
 font font-family font-size font-style font-weight font-variant line-height letter-spacing word-spacing text-align text-decoration text-transform text-indent text-overflow text-shadow white-space word-break overflow-wrap
 vertical-align list-style list-style-type list-style-position list-style-image table-layout caption-side empty-cells
 float clear position top right bottom left z-index object-fit object-position
 flex flex-basis flex-direction flex-flow flex-grow flex-shrink flex-wrap align-content align-items align-self justify-content justify-items justify-self order gap row-gap column-gap
 grid grid-template grid-template-columns grid-template-rows grid-template-areas grid-auto-columns grid-auto-rows grid-auto-flow grid-column grid-row grid-column-start grid-column-end grid-row-start grid-row-end grid-area
 columns column-count column-width column-rule break-before break-after break-inside`)
var snapshotFunctions = words("rgb( rgba( hsl( hsla( hwb( lab( lch( oklab( oklch( color( color-mix( calc( min( max( clamp( linear-gradient( radial-gradient( repeating-linear-gradient( repeating-radial-gradient( minmax( repeat( fit-content(")

func (b *snapshotBuilder) css(raw string, base *url.URL, inline bool, depth int) string {
	if len(raw) > maxCSS {
		b.result.Dropped++
		return ""
	}
	p := css.NewParser(parse.NewInputString(raw), inline)
	out := boundedBuffer{limit: maxSerializedBytes}
	// Each opened block remembers whether it is emitted. Unknown at-rules are skipped
	// as a unit; bad declarations never discard valid siblings.
	var stack []bool
	allowed := func() bool { return len(stack) == 0 || stack[len(stack)-1] }
	emit := func(s string) bool { _, err := out.WriteString(s); return err == nil }
	selector := ""
	for steps := 0; steps < maxCSSTokens; steps++ {
		if b.ctx.Err() != nil {
			return ""
		}
		g, _, data := p.Next()
		v := p.Values()
		switch g {
		case css.ErrorGrammar:
			if p.HasParseError() {
				b.result.Dropped++
				selector = ""
				continue
			}
			for _, active := range stack {
				if active {
					if !emit("}") {
						return ""
					}
				}
			}
			return out.String()
		case css.CommentGrammar: // comments convey no styling
		case css.QualifiedRuleGrammar:
			s, ok := safeSelector(v)
			if !ok {
				selector = "!"
			} else if selector != "!" {
				selector += s + ","
			}
		case css.BeginRulesetGrammar:
			s, ok := safeSelector(v)
			ok = ok && selector != "!" && allowed() && !inline
			if ok {
				ok = emit(selector + s + "{")
			}
			stack = append(stack, ok)
			selector = ""
			if !ok {
				b.result.Dropped++
			}
		case css.BeginAtRuleGrammar:
			name := strings.ToLower(string(data))
			value, ok := plainCSS(v)
			ok = ok && allowed() && !inline && (name == "@media" || name == "@supports" || name == "@layer")
			if ok {
				ok = emit(name + " " + value + "{")
			}
			stack = append(stack, ok)
			if !ok {
				b.result.Dropped++
			}
		case css.EndAtRuleGrammar, css.EndRulesetGrammar:
			if len(stack) > 0 {
				active := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if active && !emit("}") {
					return ""
				}
			}
		case css.AtRuleGrammar:
			if allowed() && !inline && strings.EqualFold(string(data), "@import") {
				rawURL, ok := importURL(v)
				if ok {
					if !emit(b.importSheet(base, rawURL, depth+1)) {
						return ""
					}
				} else {
					b.result.Dropped++
				}
			} else {
				b.result.Dropped++
			}
		case css.DeclarationGrammar:
			if !allowed() {
				continue
			}
			name := strings.ToLower(string(data))
			if !snapshotProperties[name] {
				b.result.Dropped++
				continue
			}
			value, ok := b.cssValue(v, base)
			if !ok {
				b.result.Dropped++
				continue
			}
			if !emit(name + ":" + value + ";") {
				b.result.Dropped++
				return ""
			}
		default:
			b.result.Dropped++
		}
		if len(stack) > maxSnapshotDepth {
			b.result.Dropped++
			return ""
		}
	}
	b.result.Dropped++
	return ""
}

// Escaped identifiers are rejected, rather than normalized differently from a browser.
// This also makes raw-text serialization and import URLs unambiguous.
func plainCSS(tokens []css.Token) (string, bool) {
	var out strings.Builder
	for _, t := range tokens {
		s := string(t.Data)
		if strings.ContainsAny(s, "\\\x00<>{};@") {
			return "", false
		}
		switch t.TokenType {
		case css.URLToken, css.BadURLToken, css.BadStringToken, css.FunctionToken:
			return "", false
		default:
		}
		out.WriteString(s)
	}
	return out.String(), true
}
func safeSelector(tokens []css.Token) (string, bool) {
	s, ok := plainCSS(tokens)
	return s, ok && strings.TrimSpace(s) != ""
}
func importURL(tokens []css.Token) (string, bool) {
	var meaningful []css.Token
	for _, t := range tokens {
		if t.TokenType != css.WhitespaceToken {
			meaningful = append(meaningful, t)
		}
	}
	// Media/layer/support import qualifiers are deliberately not expanded.
	if len(meaningful) != 1 {
		return "", false
	}
	t := meaningful[0]
	if strings.ContainsAny(string(t.Data), "\\\x00\r\n") {
		return "", false
	}
	if t.TokenType == css.StringToken {
		s := string(t.Data)
		if len(s) >= 2 {
			return s[1 : len(s)-1], true
		}
	}
	if t.TokenType == css.URLToken {
		return cssURL(string(t.Data))
	}
	return "", false
}
func cssURL(s string) (string, bool) {
	if len(s) < 5 || !strings.EqualFold(s[:4], "url(") || s[len(s)-1] != ')' || strings.ContainsAny(s, "\\\x00\r\n") {
		return "", false
	}
	s = strings.TrimSpace(s[4 : len(s)-1])
	if len(s) >= 2 && ((s[0] == '\'' && s[len(s)-1] == '\'') || (s[0] == '"' && s[len(s)-1] == '"')) {
		s = s[1 : len(s)-1]
	}
	return s, s != ""
}
func (b *snapshotBuilder) cssValue(tokens []css.Token, base *url.URL) (string, bool) {
	var out strings.Builder
	for _, t := range tokens {
		s := string(t.Data)
		if strings.ContainsAny(s, "\\\x00<>{};@") {
			return "", false
		}
		switch t.TokenType {
		case css.URLToken:
			u, ok := cssURL(s)
			if !ok {
				return "", false
			}
			img := b.raster(base, u)
			if img == "" {
				return "", false
			}
			s = `url("` + img + `")`
		case css.FunctionToken:
			if !snapshotFunctions[strings.ToLower(s)] {
				return "", false
			}
		case css.BadURLToken, css.BadStringToken, css.AtKeywordToken, css.CustomPropertyNameToken:
			return "", false
		default:
		}
		if out.Len()+len(s) > maxCSS {
			return "", false
		}
		out.WriteString(s)
	}
	return out.String(), true
}
