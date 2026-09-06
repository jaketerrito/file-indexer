// Package validate provides the shared gRPC server interceptor that enforces
// buf.validate (protovalidate) annotations on request messages.
package validate

import (
	"context"
	"errors"

	"buf.build/go/protovalidate"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// UnaryInterceptor returns a grpc.UnaryServerInterceptor that validates every
// incoming request message against its buf.validate field rules, rejecting
// violations with InvalidArgument. Messages without rules pass through.
func UnaryInterceptor() (grpc.UnaryServerInterceptor, error) {
	validator, err := protovalidate.New()
	if err != nil {
		return nil, err
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		msg, ok := req.(proto.Message)
		if !ok {
			return handler(ctx, req)
		}
		if err := validator.Validate(msg); err != nil {
			var valErr *protovalidate.ValidationError
			if errors.As(err, &valErr) {
				return nil, status.Error(codes.InvalidArgument, err.Error())
			}
			return nil, status.Error(codes.Internal, err.Error())
		}
		return handler(ctx, req)
	}, nil
}
