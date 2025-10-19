package main

import (
	"fmt"
	"time"
)

func main() {
	msg := time.Now().Format(time.DateTime)
	fmt.Println("hello:", msg)
}
