package main

import (
	"fmt"

	_ "github.com/whatap/gointernal/lang/license"
	_ "github.com/whatap/gointernal/net/secure"
	_ "github.com/whatap/gointernal/util/crypto"
	_ "github.com/whatap/gointernal/util/oidutil"
)

func main() {
	fmt.Println("Whatap Golang internal library")
}
