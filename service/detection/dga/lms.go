package dga

import (
	"strings"
)

// LmsScoreOfDomain calculates the mean longest meaningful substring of a domain.
// It follows some special rules to increase accuracy. It returns a value between
// 0 and 100, representing the length-based percentage of the meaningful substring.
func LmsScoreOfDomain(domain string) float64 {
	var totalScore float64
	domain = strings.ToLower(domain)
	subjects := strings.Split(domain, ".")
	var totalLength int
	for _, subject := range subjects {
		totalLength += len(subject)
	}
	for _, subject := range subjects {
		// calculate score, weigh it and add it
		if len(subject) > 0 {
			totalScore += LmsScore(subject) * (float64(len(subject)) / float64(totalLength))
		}
	}
	return totalScore
}

// LmsScore calculates the longest meaningful substring of a domain. It returns a
// value between 0 and 100, representing the length-based percentage of the
// meaningful substring.
func LmsScore(subject string) float64 {
	lmsStart := -1
	lmsStop := -1
	longestLms := 0

	for i, c := range subject {
		if int(c) >= int('a') && int(c) <= int('z') {
			if lmsStart == -1 {
				lmsStart = i
			}
		} else {
			if lmsStart > -1 {
				lmsStop = i
				if lmsStop-lmsStart > longestLms {
					longestLms = lmsStop - lmsStart
				}
				lmsStart = -1
			}
		}
	}
	if lmsStop == -1 {
		longestLms = len(subject)
	}
	// fmt.Printf("algs: lms score of %s is %.2f\n", subject, (float64(longest_lms) * 100.0 / float64(len(subject))))
	return (float64(longestLms) * 100.0 / float64(len(subject)))
}
