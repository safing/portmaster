package accessor

const (
	emptyString = ""
)

// Accessor provides an interface to supply the query matcher a method to retrieve values from an object.
type Accessor interface {
	Get(key string) (value interface{}, ok bool)
	GetString(key string) (value string, ok bool)
	GetStringArray(key string) (value []string, ok bool)
	GetInt(key string) (value int64, ok bool)
	GetFloat(key string) (value float64, ok bool)
	GetBool(key string) (value bool, ok bool)
	Exists(key string) bool
	Set(key string, value interface{}) error
	Type() string
}
