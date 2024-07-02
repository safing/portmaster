package query

var (
	operatorNames = map[string]uint8{
		"==":         Equals,
		">":          GreaterThan,
		">=":         GreaterThanOrEqual,
		"<":          LessThan,
		"<=":         LessThanOrEqual,
		"f==":        FloatEquals,
		"f>":         FloatGreaterThan,
		"f>=":        FloatGreaterThanOrEqual,
		"f<":         FloatLessThan,
		"f<=":        FloatLessThanOrEqual,
		"sameas":     SameAs,
		"s==":        SameAs,
		"contains":   Contains,
		"co":         Contains,
		"startswith": StartsWith,
		"sw":         StartsWith,
		"endswith":   EndsWith,
		"ew":         EndsWith,
		"in":         In,
		"matches":    Matches,
		"re":         Matches,
		"is":         Is,
		"exists":     Exists,
		"ex":         Exists,
	}

	primaryNames = make(map[uint8]string)
)

func init() {
	for opName, opID := range operatorNames {
		name, ok := primaryNames[opID]
		if ok {
			if len(name) < len(opName) {
				primaryNames[opID] = opName
			}
		} else {
			primaryNames[opID] = opName
		}
	}
}

func getOpName(operator uint8) string {
	name, ok := primaryNames[operator]
	if ok {
		return name
	}
	return "[unknown]"
}
