package main

import (
	"fmt"
	"unsafe"
)

const integerSize int = int(unsafe.Sizeof(0))

func isBigEndian() bool {
	var i int = 0x1
	bs := (*[integerSize]byte)(unsafe.Pointer(&i))
	if bs[0] == 0 {
		return true
	} else {
		return false
	}
}

func main() {
	if isBigEndian() {
		fmt.Println("System is Big Endian (Network Byte Order): uint16 0x1234 is 0x1234 in memory")
	} else {
		fmt.Println("System is Little Endian (Host Byte Order): uint16 0x1234 is 0x3412 in memory")
	}
}
