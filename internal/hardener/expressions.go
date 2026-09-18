package hardener

import "strings"

const (
	prTitleReference = "github.event.pull_request.title"
	prBodyReference  = "github.event.pull_request.body"
)

// expressionParser reads a small expression subset without evaluating values.
// Its cursor only moves forward; strings and fallback chains need no recursion.
type expressionParser struct {
	text string
	pos  int
}

// parse starts just after ${{ and consumes the closing }} on success.
// Grammar: term ("||" term)*, where a term is a reference or quoted string.
func (p *expressionParser) parse() (title, body, supported bool) {
	for {
		p.space()
		if p.pos < len(p.text) && p.text[p.pos] == '\'' {
			if !p.quotedString() {
				return false, false, false
			}
		} else {
			reference, ok := p.reference()
			if !ok {
				return false, false, false
			}
			switch reference {
			case prTitleReference:
				title = true
			case prBodyReference:
				body = true
			default:
				// Preserve explicit coverage gaps for noncanonical PR path casing.
				if strings.EqualFold(reference, prTitleReference) || strings.EqualFold(reference, prBodyReference) {
					return false, false, false
				}
			}
		}
		p.space()
		if strings.HasPrefix(p.text[p.pos:], "}}") {
			p.pos += 2
			return title, body, true
		}
		if !strings.HasPrefix(p.text[p.pos:], "||") {
			return false, false, false
		}
		p.pos += 2
	}
}

func (p *expressionParser) space() {
	for p.pos < len(p.text) {
		switch p.text[p.pos] {
		case ' ', '\t', '\r', '\n':
			p.pos++
		default:
			return
		}
	}
}

// quotedString treats delimiters and PR paths inside a string as literal text.
// Two consecutive apostrophes escape an apostrophe; backslashes do not.
func (p *expressionParser) quotedString() bool {
	p.pos++ // Opening apostrophe, already checked by parse.
	for p.pos < len(p.text) {
		ch := p.text[p.pos]
		p.pos++
		if ch != '\'' {
			continue
		}
		if p.pos < len(p.text) && p.text[p.pos] == '\'' {
			p.pos++
			continue
		}
		return true
	}
	return false
}

func (p *expressionParser) reference() (string, bool) {
	start := p.pos
	if !p.identifier() {
		return "", false
	}
	switch p.text[start:p.pos] {
	case "github", "env", "vars", "secrets", "inputs", "steps", "needs", "matrix", "runner", "job", "strategy":
	default:
		return "", false
	}
	// Standalone context objects are outside this subset.
	if p.pos == len(p.text) || p.text[p.pos] != '.' {
		return "", false
	}
	for p.pos < len(p.text) && p.text[p.pos] == '.' {
		p.pos++
		if !p.identifier() {
			return "", false
		}
	}
	return p.text[start:p.pos], true
}

func (p *expressionParser) identifier() bool {
	if p.pos == len(p.text) || !identifierStart(p.text[p.pos]) {
		return false
	}
	p.pos++
	for p.pos < len(p.text) {
		ch := p.text[p.pos]
		if !identifierStart(ch) && !(ch >= '0' && ch <= '9') && ch != '-' {
			break
		}
		p.pos++
	}
	return true
}

func identifierStart(ch byte) bool {
	return ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch == '_'
}
