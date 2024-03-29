package endpoints

// Reason describes the reason why an endpoint has been
// permitted or blocked.
type Reason interface {
	// String should return a human readable string
	// describing the decision reason.
	String() string

	// Context returns the context that was used
	// for the decision.
	Context() interface{}
}

type reason struct {
	description string
	Filter      string
	Value       string
	Permitted   bool
	Extra       map[string]interface{}
}

func (r *reason) String() string {
	prefix := "denied by rule: "
	if r.Permitted {
		prefix = "allowed by rule: "
	}

	return prefix + r.description + " " + r.Filter[2:]
}

func (r *reason) Context() interface{} {
	return r
}
