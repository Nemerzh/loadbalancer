package main

import "runtime"

func main() {
	runtime.GOMAXPROCS(runtime.NumCPU())

	for i := 1; i <= 5; i++ {

	}
}
