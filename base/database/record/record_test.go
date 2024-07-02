package record

import (
	"sync"
)

type TestRecord struct {
	Base
	sync.Mutex
}
