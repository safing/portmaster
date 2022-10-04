package profile

import (
	"fmt"
	"regexp"
	"strings"
)

// # Matching and Scores
//
// There are three levels:
//
// 1. Type: What matched?
//   1. Tag:            40.000 points
//   2. Env:            30.000 points
//   3. MatchingPath:   20.000 points
//   4. Path:           10.000 points
// 2. Operation: How was it mached?
//   1. Equals:          3.000 points
//   2. Prefix:          2.000 points
//   3. Regex:           1.000 points
// 3. How "strong" was the match?
//   1. Equals:                Length of path (irrelevant)
//   2. Prefix:                Length of prefix
//   3. Regex:                 Length of match

// ms-store:Microsoft.One.Note

// Path Match /path/to/file
// Tag MS-Store Match value
// Env Regex Key Value

// Fingerprint Type IDs.
const (
	FingerprintTypeTagID  = "tag"
	FingerprintTypeEnvID  = "env"
	FingerprintTypePathID = "path" // Matches both MatchingPath and Path.

	FingerprintOperationEqualsID = "equals"
	FingerprintOperationPrefixID = "prefix"
	FingerprintOperationRegexID  = "regex"

	tagMatchBaseScore          = 40_000
	envMatchBaseScore          = 30_000
	matchingPathMatchBaseScore = 20_000
	pathMatchBaseScore         = 10_000

	fingerprintEqualsBaseScore = 3_000
	fingerprintPrefixBaseScore = 2_000
	fingerprintRegexBaseScore  = 1_000

	maxMatchStrength = 499
)

type (
	// Fingerprint defines a way of matching a process.
	// The Key is only valid - but required - for some types.
	Fingerprint struct {
		Type      string
		Key       string // Key must always fully match.
		Operation string
		Value     string
	}

	// Tag represents a simple key/value kind of tag used in process metadata
	// and fingerprints.
	Tag struct {
		Key   string
		Value string
	}

	// MatchingData is an interface to fetching data in the matching process.
	MatchingData interface {
		Tags() []Tag
		Env() map[string]string
		Path() string
		MatchingPath() string
	}

	matchingFingerprint interface {
		MatchesKey(key string) bool
		Match(value string) (score int)
	}
)

// MatchesKey returns whether the optional fingerprint key (for some types
// only) matches the given key.
func (fp Fingerprint) MatchesKey(key string) bool {
	return key == fp.Key
}

// KeyInTags checks is the given key is in the tags.
func KeyInTags(tags []Tag, key string) bool {
	for _, tag := range tags {
		if key == tag.Key {
			return true
		}
	}
	return false
}

// KeyAndValueInTags checks is the given key/value pair is in the tags.
func KeyAndValueInTags(tags []Tag, key, value string) bool {
	for _, tag := range tags {
		if key == tag.Key && value == tag.Value {
			return true
		}
	}
	return false
}

type fingerprintEquals struct {
	Fingerprint
}

func (fp fingerprintEquals) Match(value string) (score int) {
	if value == fp.Value {
		return fingerprintEqualsBaseScore + checkMatchStrength(len(fp.Value))
	}
	return 0
}

type fingerprintPrefix struct {
	Fingerprint
}

func (fp fingerprintPrefix) Match(value string) (score int) {
	if strings.HasPrefix(value, fp.Value) {
		return fingerprintPrefixBaseScore + checkMatchStrength(len(fp.Value))
	}
	return 0
}

type fingerprintRegex struct {
	Fingerprint
	regex *regexp.Regexp
}

func (fp fingerprintRegex) Match(value string) (score int) {
	// Find best match.
	for _, match := range fp.regex.FindAllString(value, -1) {
		// Save match length if higher than score.
		// This will also ignore empty matches.
		if len(match) > score {
			score = len(match)
		}
	}

	// Add base score and return if anything was found.
	if score > 0 {
		return fingerprintRegexBaseScore + checkMatchStrength(score)
	}

	return 0
}

type parsedFingerprints struct {
	tagPrints  []matchingFingerprint
	envPrints  []matchingFingerprint
	pathPrints []matchingFingerprint
}

func parseFingerprints(raw []Fingerprint, deprecatedLinkedPath string) (parsed *parsedFingerprints, firstErr error) {
	parsed = &parsedFingerprints{}

	// Add deprecated linked path to fingerprints.
	if deprecatedLinkedPath != "" {
		parsed.pathPrints = append(parsed.pathPrints, &fingerprintEquals{
			Fingerprint: Fingerprint{
				Type:      FingerprintTypePathID,
				Operation: FingerprintOperationEqualsID,
				Value:     deprecatedLinkedPath,
			},
		})
	}

	// Parse all fingerprints.
	// Do not fail when one fails, instead return the first encountered error.
	for _, entry := range raw {
		// Check type and required key.
		switch entry.Type {
		case FingerprintTypeTagID, FingerprintTypeEnvID:
			if entry.Key == "" {
				if firstErr == nil {
					firstErr = fmt.Errorf("%s fingerprint is missing key", entry.Type)
				}
				continue
			}
		case FingerprintTypePathID:
			// Don't need a key.
		default:
			// Unknown type.
			if firstErr == nil {
				firstErr = fmt.Errorf("unknown fingerprint type: %q", entry.Type)
			}
			continue
		}

		// Create and/or collect operation match functions.
		switch entry.Operation {
		case FingerprintOperationEqualsID:
			parsed.addMatchingFingerprint(entry, fingerprintEquals{entry})

		case FingerprintOperationPrefixID:
			parsed.addMatchingFingerprint(entry, fingerprintPrefix{entry})

		case FingerprintOperationRegexID:
			regex, err := regexp.Compile(entry.Value)
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("failed to compile regex fingerprint: %s", entry.Value)
				}
			} else {
				parsed.addMatchingFingerprint(entry, fingerprintRegex{
					Fingerprint: entry,
					regex:       regex,
				})
			}

		default:
			if firstErr == nil {
				firstErr = fmt.Errorf("unknown fingerprint operation: %q", entry.Type)
			}
		}
	}

	return parsed, firstErr
}

func (parsed *parsedFingerprints) addMatchingFingerprint(fp Fingerprint, matchingPrint matchingFingerprint) {
	switch fp.Type {
	case FingerprintTypeTagID:
		parsed.tagPrints = append(parsed.tagPrints, matchingPrint)
	case FingerprintTypeEnvID:
		parsed.envPrints = append(parsed.envPrints, matchingPrint)
	case FingerprintTypePathID:
		parsed.pathPrints = append(parsed.pathPrints, matchingPrint)
	default:
		// This should never happen, as the types are checked already.
		panic(fmt.Sprintf("unknown fingerprint type: %q", fp.Type))
	}
}

// MatchFingerprints returns the highest matching score of the given
// fingerprints and matching data.
func MatchFingerprints(prints *parsedFingerprints, md MatchingData) (highestScore int) {
	// Check tags.
	for _, tagPrint := range prints.tagPrints {
		for _, tag := range md.Tags() {
			// Check if tag key matches.
			if !tagPrint.MatchesKey(tag.Key) {
				continue
			}

			// Try matching the tag value.
			score := tagPrint.Match(tag.Value)
			if score > highestScore {
				highestScore = score
			}
		}
	}
	// If something matched, add base score and return.
	if highestScore > 0 {
		return tagMatchBaseScore + highestScore
	}

	// Check env.
	for _, envPrint := range prints.envPrints {
		for key, value := range md.Env() {
			// Check if env key matches.
			if !envPrint.MatchesKey(key) {
				continue
			}

			// Try matching the env value.
			score := envPrint.Match(value)
			if score > highestScore {
				highestScore = score
			}
		}
	}
	// If something matched, add base score and return.
	if highestScore > 0 {
		return envMatchBaseScore + highestScore
	}

	// Check matching path.
	matchingPath := md.MatchingPath()
	if matchingPath != "" {
		for _, pathPrint := range prints.pathPrints {
			// Try matching the path value.
			score := pathPrint.Match(matchingPath)
			if score > highestScore {
				highestScore = score
			}
		}
		// If something matched, add base score and return.
		if highestScore > 0 {
			return matchingPathMatchBaseScore + highestScore
		}
	}

	// Check path.
	path := md.Path()
	if path != "" {
		for _, pathPrint := range prints.pathPrints {
			// Try matching the path value.
			score := pathPrint.Match(path)
			if score > highestScore {
				highestScore = score
			}
		}
		// If something matched, add base score and return.
		if highestScore > 0 {
			return pathMatchBaseScore + highestScore
		}
	}

	// Nothing matched.
	return 0
}

func checkMatchStrength(value int) int {
	if value > maxMatchStrength {
		return maxMatchStrength
	}
	if value < -maxMatchStrength {
		return -maxMatchStrength
	}
	return value
}
