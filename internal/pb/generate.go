// Package pb anchors protobuf code generation; the generated Go lives in the
// versioned subpackages (e.g. service/v1) produced by the directive below.
package pb

//go:generate go run github.com/bufbuild/buf/cmd/buf generate --template=../../proto/buf.gen.yaml ../../proto
