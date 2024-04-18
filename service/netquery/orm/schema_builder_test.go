package orm

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSchemaBuilder(t *testing.T) {
	t.Parallel()

	cases := []struct {
		Name        string
		Model       interface{}
		ExpectedSQL string
	}{
		{
			"Simple",
			struct {
				ID    int         `sqlite:"id,primary,autoincrement"`
				Text  string      `sqlite:"text,nullable"`
				Int   *int        `sqlite:",not-null"`
				Float interface{} `sqlite:",float,nullable"`
			}{},
			`CREATE TABLE main.Simple ( id INTEGER PRIMARY KEY AUTOINCREMENT NOT NULL, text TEXT, Int INTEGER NOT NULL, Float REAL );`,
		},
		{
			"Varchar",
			struct {
				S string `sqlite:",varchar(10)"`
			}{},
			`CREATE TABLE main.Varchar ( S VARCHAR(10) NOT NULL );`,
		},
	}

	for idx := range cases {
		c := cases[idx]

		res, err := GenerateTableSchema(c.Name, c.Model)
		require.NoError(t, err)
		assert.Equal(t, c.ExpectedSQL, res.CreateStatement("main", false))
	}
}
