package filterlists

const (
	cacheDBPrefix = "cache:intel/filterlists"

	// filterListCacheVersionKey is used to store the highest version
	// of a filterlists file (base, intermediate or urgent) in the
	// cache database. It's used to decide if the cache database and
	// bloomfilters need to be resetted and rebuilt.
	filterListCacheVersionKey = cacheDBPrefix + "/version"

	// filterListIndexKey is used to store the filterlists index.
	filterListIndexKey = cacheDBPrefix + "/index"

	// filterListKeyPrefix is the prefix inside that cache database
	// used for filter list entries.
	filterListKeyPrefix = cacheDBPrefix + "/lists/"
)

func makeBloomCacheKey(scope string) string {
	return cacheDBPrefix + "/bloom/" + scope
}

func makeListCacheKey(scope, key string) string {
	return filterListKeyPrefix + scope + "/" + key
}
