package debug

import "log"

var Enabled bool

func Printf(s string, a ...interface{}) {
	if Enabled {
		log.Printf(s, a...)
	}
}

func Println(a ...interface{}) {
	if Enabled {
		log.Println(a...)
	}
}

func Print(a ...interface{}) {
	if Enabled {
		log.Print(a...)
	}
}
