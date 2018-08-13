// Copyright Safing ICS Technologies GmbH. Use of this source code is governed by the AGPL license that can be found in the LICENSE file.

package algs

import (
	"strings"
)

func LmsScoreOfDomain(domain string) float64 {
	var totalScore float64
	domain = strings.ToLower(domain)
	subjects := strings.Split(domain, ".")
	// ignore the last two parts
	if len(subjects) <= 3 {
		return 100
	}
	subjects = subjects[:len(subjects)-3]
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

func LmsScore(subject string) float64 {
	lms_start := -1
	lms_stop := -1
	longest_lms := 0

	for i, c := range subject {
		if int(c) >= int('a') && int(c) <= int('z') {
			if lms_start == -1 {
				lms_start = i
			}
		} else {
			if lms_start > -1 {
				lms_stop = i
				if lms_stop-lms_start > longest_lms {
					longest_lms = lms_stop - lms_start
				}
				lms_start = -1
			}
		}
	}
	if lms_stop == -1 {
		longest_lms = len(subject)
	}
	// fmt.Printf("algs: lms score of %s is %.2f\n", subject, (float64(longest_lms) * 100.0 / float64(len(subject))))
	return (float64(longest_lms) * 100.0 / float64(len(subject)))
}
