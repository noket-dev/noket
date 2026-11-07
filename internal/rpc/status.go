package rpc

import (
	"errors"
	"fmt"

	statuspb "google.golang.org/genproto/googleapis/rpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"

	tunnelv1 "github.com/noket-dev/noket/gen/noket/tunnel/v1"
)

// Canonical google.rpc.Code values used across the tunnel.
const (
	CodeOK                 int32 = 0
	CodeInvalidArgument    int32 = 3
	CodeDeadlineExceeded   int32 = 4
	CodeNotFound           int32 = 5
	CodeAlreadyExists      int32 = 6
	CodePermissionDenied   int32 = 7
	CodeResourceExhausted  int32 = 8
	CodeFailedPrecondition int32 = 9
	CodeInternal           int32 = 13
	CodeUnavailable        int32 = 14
	CodeUnauthenticated    int32 = 16
)

// Error carries a google.rpc.Code and a message safe to return to the cloud.
type Error struct {
	Code    int32
	Message string
}

func (e *Error) Error() string { return e.Message }

func Errorf(code int32, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Status builds a google.rpc.Status.
func Status(code int32, message string) *statuspb.Status {
	return &statuspb.Status{Code: code, Message: message}
}

func OK() *statuspb.Status { return &statuspb.Status{} }

// FromError maps a Go error to a status, defaulting to INTERNAL.
func FromError(err error) *statuspb.Status {
	if err == nil {
		return OK()
	}
	var rpcErr *Error
	if errors.As(err, &rpcErr) {
		return Status(rpcErr.Code, rpcErr.Message)
	}
	return Status(CodeInternal, err.Error())
}

// Reply packs a success response for an upstream method.
func Reply(msg proto.Message) *tunnelv1.UpstreamReply {
	reply := &tunnelv1.UpstreamReply{Status: OK()}
	if msg == nil {
		return reply
	}
	data, err := anypb.New(msg)
	if err != nil {
		return &tunnelv1.UpstreamReply{Status: Status(CodeInternal, "encode response: "+err.Error())}
	}
	reply.Data = data
	return reply
}

// ErrorReply packs a failure response for an upstream method.
func ErrorReply(err error) *tunnelv1.UpstreamReply {
	return &tunnelv1.UpstreamReply{Status: FromError(err)}
}
