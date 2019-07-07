package debug

import "fmt"

var Enabled bool

func Printf(s string, a ...interface{}) {
	if Enabled {
		fmt.Printf(s, a...)
	}
}

func Println(a ...interface{}) {
	if Enabled {
		fmt.Println(a)
	}
}

func Print(a ...interface{}) {
	if Enabled {
		fmt.Print(a)
	}
}
