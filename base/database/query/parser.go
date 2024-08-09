package query

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

type snippet struct {
	text           string
	globalPosition int
}

// ParseQuery parses a plaintext query. Special characters (that must be escaped with a '\') are: `\()` and any whitespaces.
//
//nolint:gocognit
func ParseQuery(query string) (*Query, error) {
	snippets, err := extractSnippets(query)
	if err != nil {
		return nil, err
	}
	snippetsPos := 0

	getSnippet := func() (*snippet, error) {
		// order is important, as parseAndOr will always consume one additional snippet.
		snippetsPos++
		if snippetsPos > len(snippets) {
			return nil, fmt.Errorf("unexpected end at position %d", len(query))
		}
		return snippets[snippetsPos-1], nil
	}
	remainingSnippets := func() int {
		return len(snippets) - snippetsPos
	}

	// check for query word
	queryWord, err := getSnippet()
	if err != nil {
		return nil, err
	}
	if queryWord.text != "query" {
		return nil, errors.New("queries must start with \"query\"")
	}

	// get prefix
	prefix, err := getSnippet()
	if err != nil {
		return nil, err
	}
	q := New(prefix.text)

	for remainingSnippets() > 0 {
		command, err := getSnippet()
		if err != nil {
			return nil, err
		}

		switch command.text {
		case "where":
			if q.where != nil {
				return nil, fmt.Errorf("duplicate \"%s\" clause found at position %d", command.text, command.globalPosition)
			}

			// parse conditions
			condition, err := parseAndOr(getSnippet, remainingSnippets, true)
			if err != nil {
				return nil, err
			}
			// go one back, as parseAndOr had to check if its done
			snippetsPos--

			q.Where(condition)
		case "orderby":
			if q.orderBy != "" {
				return nil, fmt.Errorf("duplicate \"%s\" clause found at position %d", command.text, command.globalPosition)
			}

			orderBySnippet, err := getSnippet()
			if err != nil {
				return nil, err
			}

			q.OrderBy(orderBySnippet.text)
		case "limit":
			if q.limit != 0 {
				return nil, fmt.Errorf("duplicate \"%s\" clause found at position %d", command.text, command.globalPosition)
			}

			limitSnippet, err := getSnippet()
			if err != nil {
				return nil, err
			}
			limit, err := strconv.ParseUint(limitSnippet.text, 10, 31)
			if err != nil {
				return nil, fmt.Errorf("could not parse integer (%s) at position %d", limitSnippet.text, limitSnippet.globalPosition)
			}

			q.Limit(int(limit))
		case "offset":
			if q.offset != 0 {
				return nil, fmt.Errorf("duplicate \"%s\" clause found at position %d", command.text, command.globalPosition)
			}

			offsetSnippet, err := getSnippet()
			if err != nil {
				return nil, err
			}
			offset, err := strconv.ParseUint(offsetSnippet.text, 10, 31)
			if err != nil {
				return nil, fmt.Errorf("could not parse integer (%s) at position %d", offsetSnippet.text, offsetSnippet.globalPosition)
			}

			q.Offset(int(offset))
		default:
			return nil, fmt.Errorf("unknown clause \"%s\" at position %d", command.text, command.globalPosition)
		}
	}

	return q.Check()
}

func extractSnippets(text string) (snippets []*snippet, err error) {
	skip := false
	start := -1
	inParenthesis := false
	var pos int
	var char rune

	for pos, char = range text {

		// skip
		if skip {
			skip = false
			continue
		}
		if char == '\\' {
			skip = true
		}

		// wait for parenthesis to be overs
		if inParenthesis {
			if char == '"' {
				snippets = append(snippets, &snippet{
					text:           prepToken(text[start+1 : pos]),
					globalPosition: start + 1,
				})
				start = -1
				inParenthesis = false
			}
			continue
		}

		// handle segments
		switch char {
		case '\t', '\n', '\r', ' ', '(', ')':
			if start >= 0 {
				snippets = append(snippets, &snippet{
					text:           prepToken(text[start:pos]),
					globalPosition: start + 1,
				})
				start = -1
			}
		default:
			if start == -1 {
				start = pos
			}
		}

		// handle special segment characters
		switch char {
		case '(', ')':
			snippets = append(snippets, &snippet{
				text:           text[pos : pos+1],
				globalPosition: pos + 1,
			})
		case '"':
			if start < pos {
				return nil, fmt.Errorf("parenthesis ('\"') may not be used within words, please escape with '\\' (position: %d)", pos+1)
			}
			inParenthesis = true
		}

	}

	// add last
	if start >= 0 {
		snippets = append(snippets, &snippet{
			text:           prepToken(text[start : pos+1]),
			globalPosition: start + 1,
		})
	}

	return snippets, nil
}

//nolint:gocognit
func parseAndOr(getSnippet func() (*snippet, error), remainingSnippets func() int, rootCondition bool) (Condition, error) {
	var (
		isOr          = false
		typeSet       = false
		wrapInNot     = false
		expectingMore = true
		conditions    []Condition
	)

	for {
		if !expectingMore && rootCondition && remainingSnippets() == 0 {
			// advance snippetsPos by one, as it will be set back by 1
			_, _ = getSnippet()
			if len(conditions) == 1 {
				return conditions[0], nil
			}
			if isOr {
				return Or(conditions...), nil
			}
			return And(conditions...), nil
		}

		firstSnippet, err := getSnippet()
		if err != nil {
			return nil, err
		}

		if !expectingMore && rootCondition {
			switch firstSnippet.text {
			case "orderby", "limit", "offset":
				if len(conditions) == 1 {
					return conditions[0], nil
				}
				if isOr {
					return Or(conditions...), nil
				}
				return And(conditions...), nil
			}
		}

		switch firstSnippet.text {
		case "(":
			condition, err := parseAndOr(getSnippet, remainingSnippets, false)
			if err != nil {
				return nil, err
			}
			if wrapInNot {
				conditions = append(conditions, Not(condition))
				wrapInNot = false
			} else {
				conditions = append(conditions, condition)
			}
			expectingMore = true
		case ")":
			if len(conditions) == 1 {
				return conditions[0], nil
			}
			if isOr {
				return Or(conditions...), nil
			}
			return And(conditions...), nil
		case "and":
			if typeSet && isOr {
				return nil, fmt.Errorf("you may not mix \"and\" and \"or\" (position: %d)", firstSnippet.globalPosition)
			}
			isOr = false
			typeSet = true
			expectingMore = true
		case "or":
			if typeSet && !isOr {
				return nil, fmt.Errorf("you may not mix \"and\" and \"or\" (position: %d)", firstSnippet.globalPosition)
			}
			isOr = true
			typeSet = true
			expectingMore = true
		case "not":
			wrapInNot = true
			expectingMore = true
		default:
			condition, err := parseCondition(firstSnippet, getSnippet)
			if err != nil {
				return nil, err
			}
			if wrapInNot {
				conditions = append(conditions, Not(condition))
				wrapInNot = false
			} else {
				conditions = append(conditions, condition)
			}
			expectingMore = false
		}
	}
}

func parseCondition(firstSnippet *snippet, getSnippet func() (*snippet, error)) (Condition, error) {
	wrapInNot := false

	// get operator name
	opName, err := getSnippet()
	if err != nil {
		return nil, err
	}
	// negate?
	if opName.text == "not" {
		wrapInNot = true
		opName, err = getSnippet()
		if err != nil {
			return nil, err
		}
	}

	// get operator
	operator, ok := operatorNames[opName.text]
	if !ok {
		return nil, fmt.Errorf("unknown operator at position %d", opName.globalPosition)
	}

	// don't need a value for "exists"
	if operator == Exists {
		if wrapInNot {
			return Not(Where(firstSnippet.text, operator, nil)), nil
		}
		return Where(firstSnippet.text, operator, nil), nil
	}

	// get value
	value, err := getSnippet()
	if err != nil {
		return nil, err
	}
	if wrapInNot {
		return Not(Where(firstSnippet.text, operator, value.text)), nil
	}
	return Where(firstSnippet.text, operator, value.text), nil
}

var escapeReplacer = regexp.MustCompile(`\\([^\\])`)

// prepToken removes surrounding parenthesis and escape characters.
func prepToken(text string) string {
	return escapeReplacer.ReplaceAllString(strings.Trim(text, "\""), "$1")
}

// escapeString correctly escapes a snippet for printing.
func escapeString(token string) string {
	// check if token contains characters that need to be escaped
	if strings.ContainsAny(token, "()\"\\\t\r\n ") {
		// put the token in parenthesis and only escape \ and "
		return fmt.Sprintf("\"%s\"", strings.ReplaceAll(token, "\"", "\\\""))
	}
	return token
}
