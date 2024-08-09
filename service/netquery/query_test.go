package netquery

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/safing/portmaster/service/netquery/orm"
)

func TestUnmarshalQuery(t *testing.T) { //nolint:tparallel
	t.Parallel()

	cases := []struct {
		Name     string
		Input    string
		Expected Query
		Error    error
	}{
		{
			"Parse a simple query",
			`{ "domain": ["example.com", "example.at"] }`,
			Query{
				"domain": []Matcher{
					{
						Equal: "example.com",
					},
					{
						Equal: "example.at",
					},
				},
			},
			nil,
		},
		{
			"Parse a more complex query",
			`
			{
				"domain": [
					{
						"$in": [
							"example.at",
							"example.com"
						]
					},
					{
						"$like": "microsoft.%"
					}
				],
				"path": [
					"/bin/ping",
					{
						"$notin": [
							"/sbin/ping",
							"/usr/sbin/ping"
						]
					}
				]
			}
			`,
			Query{
				"domain": []Matcher{
					{
						In: []interface{}{
							"example.at",
							"example.com",
						},
					},
					{
						Like: "microsoft.%",
					},
				},
				"path": []Matcher{
					{
						Equal: "/bin/ping",
					},
					{
						NotIn: []interface{}{
							"/sbin/ping",
							"/usr/sbin/ping",
						},
					},
				},
			},
			nil,
		},
	}

	for _, testCase := range cases { //nolint:paralleltest
		t.Run(testCase.Name, func(t *testing.T) {
			var q Query
			err := json.Unmarshal([]byte(testCase.Input), &q)

			if testCase.Error != nil {
				if assert.Error(t, err) {
					assert.Equal(t, testCase.Error.Error(), err.Error())
				}
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.Expected, q)
			}
		})
	}
}

func TestQueryBuilder(t *testing.T) { //nolint:tparallel
	t.Parallel()

	now := time.Now()
	cases := []struct {
		N string
		Q Query
		R string
		P map[string]interface{}
		E error
	}{
		{
			"No filter",
			nil,
			"",
			nil,
			nil,
		},
		{
			"Simple, one-column filter",
			Query{"domain": []Matcher{
				{
					Equal: "example.com",
				},
				{
					Equal: "example.at",
				},
			}},
			"( domain = :domain0eq0 OR domain = :domain1eq0 )",
			map[string]interface{}{
				":domain0eq0": "example.com",
				":domain1eq0": "example.at",
			},
			nil,
		},
		{
			"Two column filter",
			Query{
				"domain": []Matcher{
					{
						Equal: "example.com",
					},
				},
				"path": []Matcher{
					{
						Equal: "/bin/curl",
					},
					{
						Equal: "/bin/ping",
					},
				},
			},
			"( domain = :domain0eq0 ) AND ( path = :path0eq0 OR path = :path1eq0 )",
			map[string]interface{}{
				":domain0eq0": "example.com",
				":path0eq0":   "/bin/curl",
				":path1eq0":   "/bin/ping",
			},
			nil,
		},
		{
			"Time based filter",
			Query{
				"started": []Matcher{
					{
						Equal: now.Format(time.RFC3339),
					},
				},
			},
			"( started = :started0eq0 )",
			map[string]interface{}{
				":started0eq0": now.In(time.UTC).Format(orm.SqliteTimeFormat),
			},
			nil,
		},
		{
			"Invalid column access",
			Query{
				"forbiddenField": []Matcher{{}},
			},
			"",
			nil,
			fmt.Errorf("1 error occurred:\n\t* column forbiddenField is not allowed\n\n"), //nolint:golint
		},
		{
			"Complex example",
			Query{
				"domain": []Matcher{
					{
						In: []interface{}{"example.at", "example.com"},
					},
					{
						Like: "microsoft.%",
					},
				},
				"path": []Matcher{
					{
						NotIn: []interface{}{
							"/bin/ping",
							"/sbin/ping",
							"/usr/bin/ping",
						},
					},
				},
			},
			"( domain IN ( :domain0in0, :domain0in1 ) OR domain LIKE :domain1like0 ) AND ( path NOT IN ( :path0notin0, :path0notin1, :path0notin2 ) )",
			map[string]interface{}{
				":domain0in0":   "example.at",
				":domain0in1":   "example.com",
				":domain1like0": "microsoft.%",
				":path0notin0":  "/bin/ping",
				":path0notin1":  "/sbin/ping",
				":path0notin2":  "/usr/bin/ping",
			},
			nil,
		},
	}

	tbl, err := orm.GenerateTableSchema("connections", Conn{})
	require.NoError(t, err)

	for cID, testCase := range cases { //nolint:paralleltest
		t.Run(testCase.N, func(t *testing.T) {
			str, params, err := testCase.Q.toSQLWhereClause(context.TODO(), "", tbl, orm.DefaultEncodeConfig)

			if testCase.E != nil {
				if assert.Error(t, err) {
					assert.Equal(t, testCase.E.Error(), err.Error(), "test case %d", cID)
				}
			} else {
				require.NoError(t, err, "test case %d", cID)
				assert.Equal(t, testCase.P, params, "test case %d", cID)
				assert.Equal(t, testCase.R, str, "test case %d", cID)
			}
		})
	}
}
