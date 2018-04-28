package assert

import (
	"fmt"
)

func True(predicate bool, format string, args ...interface{}) {
	if !predicate {
		panic(fmt.Sprintf(format, args...))
	}
}

func False(predicate bool, format string, args ...interface{}) {
	if predicate {
		panic(fmt.Sprintf(format, args...))
	}
}
