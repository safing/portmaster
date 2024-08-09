//nolint:unparam
package query

import (
	"testing"

	"github.com/safing/portmaster/base/database/record"
	"github.com/safing/structures/dsd"
)

// copied from https://github.com/tidwall/gjson/blob/master/gjson_test.go
var testJSON = `{"age":100, "name":{"here":"B\\\"R"},
  "noop":{"what is a wren?":"a bird"},
  "happy":true,"immortal":false,
  "items":[1,2,3,{"tags":[1,2,3],"points":[[1,2],[3,4]]},4,5,6,7],
  "arr":["1",2,"3",{"hello":"world"},"4",5],
  "vals":[1,2,3,{"sadf":sdf"asdf"}],"name":{"first":"tom","last":null},
  "created":"2014-05-16T08:28:06.989Z",
  "loggy":{
  	"programmers": [
    	    {
    	        "firstName": "Brett",
    	        "lastName": "McLaughlin",
    	        "email": "aaaa",
  			"tag": "good"
    	    },
    	    {
    	        "firstName": "Jason",
    	        "lastName": "Hunter",
    	        "email": "bbbb",
  			"tag": "bad"
    	    },
    	    {
    	        "firstName": "Elliotte",
    	        "lastName": "Harold",
    	        "email": "cccc",
  			"tag":, "good"
    	    },
  		{
  			"firstName": 1002.3,
  			"age": 101
  		}
    	]
  },
  "lastly":{"yay":"final"},
	"temperature": 120.413
}`

func testQuery(t *testing.T, r record.Record, shouldMatch bool, condition Condition) {
	t.Helper()

	q := New("test:").Where(condition).MustBeValid()
	// fmt.Printf("%s\n", q.Print())

	matched := q.Matches(r)
	switch {
	case !matched && shouldMatch:
		t.Errorf("should match: %s", q.Print())
	case matched && !shouldMatch:
		t.Errorf("should not match: %s", q.Print())
	}
}

func TestQuery(t *testing.T) {
	t.Parallel()

	// if !gjson.Valid(testJSON) {
	// 	t.Fatal("test json is invalid")
	// }
	r, err := record.NewWrapper("", nil, dsd.JSON, []byte(testJSON))
	if err != nil {
		t.Fatal(err)
	}

	testQuery(t, r, true, Where("age", Equals, 100))
	testQuery(t, r, true, Where("age", GreaterThan, uint8(99)))
	testQuery(t, r, true, Where("age", GreaterThanOrEqual, 99))
	testQuery(t, r, true, Where("age", GreaterThanOrEqual, 100))
	testQuery(t, r, true, Where("age", LessThan, 101))
	testQuery(t, r, true, Where("age", LessThanOrEqual, "101"))
	testQuery(t, r, true, Where("age", LessThanOrEqual, 100))

	testQuery(t, r, true, Where("temperature", FloatEquals, 120.413))
	testQuery(t, r, true, Where("temperature", FloatGreaterThan, 120))
	testQuery(t, r, true, Where("temperature", FloatGreaterThanOrEqual, 120))
	testQuery(t, r, true, Where("temperature", FloatGreaterThanOrEqual, 120.413))
	testQuery(t, r, true, Where("temperature", FloatLessThan, 121))
	testQuery(t, r, true, Where("temperature", FloatLessThanOrEqual, "121"))
	testQuery(t, r, true, Where("temperature", FloatLessThanOrEqual, "120.413"))

	testQuery(t, r, true, Where("lastly.yay", SameAs, "final"))
	testQuery(t, r, true, Where("lastly.yay", Contains, "ina"))
	testQuery(t, r, true, Where("lastly.yay", StartsWith, "fin"))
	testQuery(t, r, true, Where("lastly.yay", EndsWith, "nal"))
	testQuery(t, r, true, Where("lastly.yay", In, "draft,final"))
	testQuery(t, r, true, Where("lastly.yay", In, "final,draft"))

	testQuery(t, r, true, Where("happy", Is, true))
	testQuery(t, r, true, Where("happy", Is, "true"))
	testQuery(t, r, true, Where("happy", Is, "t"))
	testQuery(t, r, true, Not(Where("happy", Is, "0")))
	testQuery(t, r, true, And(
		Where("happy", Is, "1"),
		Not(Or(
			Where("happy", Is, false),
			Where("happy", Is, "f"),
		)),
	))

	testQuery(t, r, true, Where("happy", Exists, nil))

	testQuery(t, r, true, Where("created", Matches, "^2014-[0-9]{2}-[0-9]{2}T"))
}
