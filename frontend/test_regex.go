package main

import (
	"fmt"
	"strings"
)

func main() {
	msg := "pam_unix(sudo:session): session opened for user root(uid=0) by (uid=1000)"
	if strings.Contains(msg, "pam_unix(sudo:session)") || strings.Contains(msg, "sudo") {
		fmt.Println("Filtered")
	}
}
