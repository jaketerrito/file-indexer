//go:build ignore

package main

//go:generate go run github.com/bufbuild/buf/cmd/buf@v1.70.0 generate --template=../../proto/buf.gen.yaml ../../proto
