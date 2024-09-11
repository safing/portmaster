# Query

## Control Flow

- Grouping with `(` and `)`
- Chaining with `and` and `or`
  - _NO_ mixing! Be explicit and use grouping.
- Negation with `not`
  - in front of expression for group: `not (...)`
  - inside expression for clause: `name not matches "^King "`

## Selectors

Supported by all feeders:
- root level field: `field`
- sub level field: `field.sub`
- array/slice/map access: `map.0`
- array/slice/map length: `map.#`

Please note that some feeders may have other special characters. It is advised to only use alphanumeric characters for keys.

## Operators

| Name                    | Textual            | Req. Type | Internal Type | Compared with             |
|-------------------------|--------------------|-----------|---------------|---------------------------|
| Equals                  | `==`               | int       | int64         | `==`                      |
| GreaterThan             | `>`                | int       | int64         | `>`                       |
| GreaterThanOrEqual      | `>=`               | int       | int64         | `>=`                      |
| LessThan                | `<`                | int       | int64         | `<`                       |
| LessThanOrEqual         | `<=`               | int       | int64         | `<=`                      |
| FloatEquals             | `f==`              | float     | float64       | `==`                      |
| FloatGreaterThan        | `f>`               | float     | float64       | `>`                       |
| FloatGreaterThanOrEqual | `f>=`              | float     | float64       | `>=`                      |
| FloatLessThan           | `f<`               | float     | float64       | `<`                       |
| FloatLessThanOrEqual    | `f<=`              | float     | float64       | `<=`                      |
| SameAs                  | `sameas`, `s==`    | string    | string        | `==`                      |
| Contains                | `contains`, `co`   | string    | string        | `strings.Contains()`      |
| StartsWith              | `startswith`, `sw` | string    | string        | `strings.HasPrefix()`     |
| EndsWith                | `endswith`, `ew`   | string    | string        | `strings.HasSuffix()`     |
| In                      | `in`               | string    | string        | for loop with `==`        |
| Matches                 | `matches`, `re`    | string    | string        | `regexp.Regexp.Matches()` |
| Is                      | `is`               | bool*     | bool          | `==`                      |
| Exists                  | `exists`, `ex`     | any       | n/a           | n/a                       |

\*accepts strings: 1, t, T, true, True, TRUE, 0, f, F, false, False, FALSE

## Escaping

If you need to use a control character within a value (ie. not for controlling), escape it with `\`.
It is recommended to wrap a word into parenthesis instead of escaping control characters, when possible.

| Location | Characters to be escaped |
|---|---|
| Within parenthesis (`"`) | `"`, `\` |
| Everywhere else | `(`, `)`, `"`, `\`, `\t`, `\r`, `\n`, ` ` (space) |
