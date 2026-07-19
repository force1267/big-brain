// Package main builds the wrapper executable. main() only bridges the OS
// to the program: it parses command-line arguments, installs signal
// handling, and calls app.New().Run. All initialization lives in
// internal/, per this repo's rules and Effective Go's guidance that a
// main package should be thin glue over library code.
package main
