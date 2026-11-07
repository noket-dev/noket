package noketd

import (
	"fmt"

	"google.golang.org/protobuf/proto"
)

// ProtoCodec matches noket-tunnel's arpc codec. Bodies are protobuf;
// google.rpc.Status is carried inside UpstreamReply and ForwardChunk.
type ProtoCodec struct{}

func (ProtoCodec) Marshal(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	if err, ok := v.(error); ok {
		return []byte(err.Error()), nil
	}
	msg, ok := v.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("arpc: %T is not a protobuf message", v)
	}
	return proto.Marshal(msg)
}

func (ProtoCodec) Unmarshal(data []byte, v any) error {
	if len(data) == 0 {
		return nil
	}
	msg, ok := v.(proto.Message)
	if !ok {
		return fmt.Errorf("arpc: %T is not a protobuf message", v)
	}
	return proto.Unmarshal(data, msg)
}
