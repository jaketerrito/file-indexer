//go:build generate

// This file anchors project-wide code generation. The `generate` build tag is
// set by `go generate` but not by normal builds, so this file is only ever seen
// during generation and never compiled into binaries or tests.
//
// The mockery directive lives at the module root so it runs from there, letting
// the per-package `dir` paths in .mockery.yaml resolve correctly.
package main

//go:generate go tool mockery --config .mockery.yaml
