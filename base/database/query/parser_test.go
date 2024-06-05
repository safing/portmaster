package query

import (
	"reflect"
	"testing"

	"github.com/davecgh/go-spew/spew"
)

func TestExtractSnippets(t *testing.T) {
	t.Parallel()

	text1 := `query test: where ( "bananas" > 100 and monkeys.# <= "12")or(coconuts < 10 "and" area > 50) or name sameas Julian or name matches ^King\ `
	result1 := []*snippet{
		{text: "query", globalPosition: 1},
		{text: "test:", globalPosition: 7},
		{text: "where", globalPosition: 13},
		{text: "(", globalPosition: 19},
		{text: "bananas", globalPosition: 21},
		{text: ">", globalPosition: 31},
		{text: "100", globalPosition: 33},
		{text: "and", globalPosition: 37},
		{text: "monkeys.#", globalPosition: 41},
		{text: "<=", globalPosition: 51},
		{text: "12", globalPosition: 54},
		{text: ")", globalPosition: 58},
		{text: "or", globalPosition: 59},
		{text: "(", globalPosition: 61},
		{text: "coconuts", globalPosition: 62},
		{text: "<", globalPosition: 71},
		{text: "10", globalPosition: 73},
		{text: "and", globalPosition: 76},
		{text: "area", globalPosition: 82},
		{text: ">", globalPosition: 87},
		{text: "50", globalPosition: 89},
		{text: ")", globalPosition: 91},
		{text: "or", globalPosition: 93},
		{text: "name", globalPosition: 96},
		{text: "sameas", globalPosition: 101},
		{text: "Julian", globalPosition: 108},
		{text: "or", globalPosition: 115},
		{text: "name", globalPosition: 118},
		{text: "matches", globalPosition: 123},
		{text: "^King ", globalPosition: 131},
	}

	snippets, err := extractSnippets(text1)
	if err != nil {
		t.Errorf("failed to extract snippets: %s", err)
	}

	if !reflect.DeepEqual(result1, snippets) {
		t.Errorf("unexpected results:")
		for _, el := range snippets {
			t.Errorf("%+v", el)
		}
	}

	// t.Error(spew.Sprintf("%v", treeElement))
}

func testParsing(t *testing.T, queryText string, expectedResult *Query) {
	t.Helper()

	_, err := expectedResult.Check()
	if err != nil {
		t.Errorf("failed to create query: %s", err)
		return
	}

	q, err := ParseQuery(queryText)
	if err != nil {
		t.Errorf("failed to parse query: %s", err)
		return
	}

	if queryText != q.Print() {
		t.Errorf("string match failed: %s", q.Print())
		return
	}
	if !reflect.DeepEqual(expectedResult, q) {
		t.Error("deepqual match failed.")
		t.Error("got:")
		t.Error(spew.Sdump(q))
		t.Error("expected:")
		t.Error(spew.Sdump(expectedResult))
	}
}

func TestParseQuery(t *testing.T) {
	t.Parallel()

	text1 := `query test: where (bananas > 100 and monkeys.# <= 12) or not (coconuts < 10 and area not > 50) or name sameas Julian or name matches "^King " orderby name limit 10 offset 20`
	result1 := New("test:").Where(Or(
		And(
			Where("bananas", GreaterThan, 100),
			Where("monkeys.#", LessThanOrEqual, 12),
		),
		Not(And(
			Where("coconuts", LessThan, 10),
			Not(Where("area", GreaterThan, 50)),
		)),
		Where("name", SameAs, "Julian"),
		Where("name", Matches, "^King "),
	)).OrderBy("name").Limit(10).Offset(20)
	testParsing(t, text1, result1)

	testParsing(t, `query test: orderby name`, New("test:").OrderBy("name"))
	testParsing(t, `query test: limit 10`, New("test:").Limit(10))
	testParsing(t, `query test: offset 10`, New("test:").Offset(10))
	testParsing(t, `query test: where banana matches ^ban`, New("test:").Where(Where("banana", Matches, "^ban")))
	testParsing(t, `query test: where banana exists`, New("test:").Where(Where("banana", Exists, nil)))
	testParsing(t, `query test: where banana not exists`, New("test:").Where(Not(Where("banana", Exists, nil))))

	// test all operators
	testParsing(t, `query test: where banana == 1`, New("test:").Where(Where("banana", Equals, 1)))
	testParsing(t, `query test: where banana > 1`, New("test:").Where(Where("banana", GreaterThan, 1)))
	testParsing(t, `query test: where banana >= 1`, New("test:").Where(Where("banana", GreaterThanOrEqual, 1)))
	testParsing(t, `query test: where banana < 1`, New("test:").Where(Where("banana", LessThan, 1)))
	testParsing(t, `query test: where banana <= 1`, New("test:").Where(Where("banana", LessThanOrEqual, 1)))
	testParsing(t, `query test: where banana f== 1.1`, New("test:").Where(Where("banana", FloatEquals, 1.1)))
	testParsing(t, `query test: where banana f> 1.1`, New("test:").Where(Where("banana", FloatGreaterThan, 1.1)))
	testParsing(t, `query test: where banana f>= 1.1`, New("test:").Where(Where("banana", FloatGreaterThanOrEqual, 1.1)))
	testParsing(t, `query test: where banana f< 1.1`, New("test:").Where(Where("banana", FloatLessThan, 1.1)))
	testParsing(t, `query test: where banana f<= 1.1`, New("test:").Where(Where("banana", FloatLessThanOrEqual, 1.1)))
	testParsing(t, `query test: where banana sameas banana`, New("test:").Where(Where("banana", SameAs, "banana")))
	testParsing(t, `query test: where banana contains banana`, New("test:").Where(Where("banana", Contains, "banana")))
	testParsing(t, `query test: where banana startswith banana`, New("test:").Where(Where("banana", StartsWith, "banana")))
	testParsing(t, `query test: where banana endswith banana`, New("test:").Where(Where("banana", EndsWith, "banana")))
	testParsing(t, `query test: where banana in banana,coconut`, New("test:").Where(Where("banana", In, []string{"banana", "coconut"})))
	testParsing(t, `query test: where banana matches banana`, New("test:").Where(Where("banana", Matches, "banana")))
	testParsing(t, `query test: where banana is true`, New("test:").Where(Where("banana", Is, true)))
	testParsing(t, `query test: where banana exists`, New("test:").Where(Where("banana", Exists, nil)))

	// special
	testParsing(t, `query test: where banana not exists`, New("test:").Where(Not(Where("banana", Exists, nil))))
}

func testParseError(t *testing.T, queryText string, expectedErrorString string) {
	t.Helper()

	_, err := ParseQuery(queryText)
	if err == nil {
		t.Errorf("should fail to parse: %s", queryText)
		return
	}
	if err.Error() != expectedErrorString {
		t.Errorf("unexpected error for query: %s\nwanted: %s\n   got: %s", queryText, expectedErrorString, err)
	}
}

func TestParseErrors(t *testing.T) {
	t.Parallel()

	// syntax
	testParseError(t, `query`, `unexpected end at position 5`)
	testParseError(t, `query test: where`, `unexpected end at position 17`)
	testParseError(t, `query test: where (`, `unexpected end at position 19`)
	testParseError(t, `query test: where )`, `unknown clause ")" at position 19`)
	testParseError(t, `query test: where not`, `unexpected end at position 21`)
	testParseError(t, `query test: where banana`, `unexpected end at position 24`)
	testParseError(t, `query test: where banana >`, `unexpected end at position 26`)
	testParseError(t, `query test: where banana nope`, `unknown operator at position 26`)
	testParseError(t, `query test: where banana exists or`, `unexpected end at position 34`)
	testParseError(t, `query test: where banana exists and`, `unexpected end at position 35`)
	testParseError(t, `query test: where banana exists and (`, `unexpected end at position 37`)
	testParseError(t, `query test: where banana exists and banana is true or`, `you may not mix "and" and "or" (position: 52)`)
	testParseError(t, `query test: where banana exists or banana is true and`, `you may not mix "and" and "or" (position: 51)`)
	// testParseError(t, `query test: where banana exists and (`, ``)

	// value parsing error
	testParseError(t, `query test: where banana == banana`, `could not parse banana to int64: strconv.ParseInt: parsing "banana": invalid syntax (hint: use "sameas" to compare strings)`)
	testParseError(t, `query test: where banana f== banana`, `could not parse banana to float64: strconv.ParseFloat: parsing "banana": invalid syntax`)
	testParseError(t, `query test: where banana in banana`, `could not parse "banana" to []string`)
	testParseError(t, `query test: where banana matches [banana`, "could not compile regex \"[banana\": error parsing regexp: missing closing ]: `[banana`")
	testParseError(t, `query test: where banana is great`, `could not parse "great" to bool: strconv.ParseBool: parsing "great": invalid syntax`)
}
