package inspectutils

type Reason struct {
	Message string
	Details interface{}
}

func (r *Reason) String() string       { return r.Message }
func (r *Reason) Context() interface{} { return r.Details }
