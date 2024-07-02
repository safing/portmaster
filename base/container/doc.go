// Package container gives you a []byte slice on steroids, allowing for quick data appending, prepending and fetching as well as transparent error transportation.
//
// A Container is basically a [][]byte slice that just appends new []byte slices and only copies things around when necessary.
//
// Byte slices added to the Container are not changed or appended, to not corrupt any other data that may be before and after the given slice.
// If interested, consider the following example to understand why this is important:
//
//	package main
//
//	import (
//		"fmt"
//	)
//
//	func main() {
//		a := []byte{0, 1,2,3,4,5,6,7,8,9}
//		fmt.Printf("a: %+v\n", a)
//		fmt.Printf("\nmaking changes...\n(we are not changing a directly)\n\n")
//		b := a[2:6]
//		c := append(b, 10, 11)
//		fmt.Printf("b: %+v\n", b)
//		fmt.Printf("c: %+v\n", c)
//		fmt.Printf("a: %+v\n", a)
//	}
//
// run it here: https://play.golang.org/p/xu1BXT3QYeE
package container
