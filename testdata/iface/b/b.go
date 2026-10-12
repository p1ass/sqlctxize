package b

import "example.com/testdata/iface/a"

type Namer interface {
	Name() string
}

func Greet() string {
	var n Namer = a.User{ID: 1}
	return "hello " + n.Name()
}
