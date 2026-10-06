package main

import "github.com/spf13/cobra"

// constructors are filled by init() functions: a command lives in its own cmd_<name>.go file
// and calls register(newXxxCmd), so adding one never touches a shared file.
var constructors []func(*Env) *cobra.Command

func register(f func(*Env) *cobra.Command) {
	constructors = append(constructors, f)
}
