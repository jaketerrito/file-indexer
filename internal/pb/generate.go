// Package pb anchors protobuf code generation; the generated Go lives in the
// versioned subpackages (e.g. service/v1) produced by the directive below.
package pb

//go:generate go tool buf generate --template=../../proto/buf.gen.yaml ../../proto
