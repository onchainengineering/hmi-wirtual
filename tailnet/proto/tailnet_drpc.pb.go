// Code generated by protoc-gen-go-drpc. DO NOT EDIT.
// protoc-gen-go-drpc version: (devel)
// source: tailnet/proto/tailnet.proto

package proto

import (
	context "context"
	errors "errors"
	protojson "google.golang.org/protobuf/encoding/protojson"
	proto "google.golang.org/protobuf/proto"
	drpc "storj.io/drpc"
	drpcerr "storj.io/drpc/drpcerr"
)

type drpcEncoding_File_tailnet_proto_tailnet_proto struct{}

func (drpcEncoding_File_tailnet_proto_tailnet_proto) Marshal(msg drpc.Message) ([]byte, error) {
	return proto.Marshal(msg.(proto.Message))
}

func (drpcEncoding_File_tailnet_proto_tailnet_proto) MarshalAppend(buf []byte, msg drpc.Message) ([]byte, error) {
	return proto.MarshalOptions{}.MarshalAppend(buf, msg.(proto.Message))
}

func (drpcEncoding_File_tailnet_proto_tailnet_proto) Unmarshal(buf []byte, msg drpc.Message) error {
	return proto.Unmarshal(buf, msg.(proto.Message))
}

func (drpcEncoding_File_tailnet_proto_tailnet_proto) JSONMarshal(msg drpc.Message) ([]byte, error) {
	return protojson.Marshal(msg.(proto.Message))
}

func (drpcEncoding_File_tailnet_proto_tailnet_proto) JSONUnmarshal(buf []byte, msg drpc.Message) error {
	return protojson.Unmarshal(buf, msg.(proto.Message))
}

type DRPCTailnetClient interface {
	DRPCConn() drpc.Conn

	PostTelemetry(ctx context.Context, in *TelemetryRequest) (*TelemetryResponse, error)
	StreamDERPMaps(ctx context.Context, in *StreamDERPMapsRequest) (DRPCTailnet_StreamDERPMapsClient, error)
	RefreshResumeToken(ctx context.Context, in *RefreshResumeTokenRequest) (*RefreshResumeTokenResponse, error)
	Coordinate(ctx context.Context) (DRPCTailnet_CoordinateClient, error)
	WorkspaceUpdates(ctx context.Context, in *WorkspaceUpdatesRequest) (DRPCTailnet_WorkspaceUpdatesClient, error)
}

type drpcTailnetClient struct {
	cc drpc.Conn
}

func NewDRPCTailnetClient(cc drpc.Conn) DRPCTailnetClient {
	return &drpcTailnetClient{cc}
}

func (c *drpcTailnetClient) DRPCConn() drpc.Conn { return c.cc }

func (c *drpcTailnetClient) PostTelemetry(ctx context.Context, in *TelemetryRequest) (*TelemetryResponse, error) {
	out := new(TelemetryResponse)
	err := c.cc.Invoke(ctx, "/coder.tailnet.v2.Tailnet/PostTelemetry", drpcEncoding_File_tailnet_proto_tailnet_proto{}, in, out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *drpcTailnetClient) StreamDERPMaps(ctx context.Context, in *StreamDERPMapsRequest) (DRPCTailnet_StreamDERPMapsClient, error) {
	stream, err := c.cc.NewStream(ctx, "/coder.tailnet.v2.Tailnet/StreamDERPMaps", drpcEncoding_File_tailnet_proto_tailnet_proto{})
	if err != nil {
		return nil, err
	}
	x := &drpcTailnet_StreamDERPMapsClient{stream}
	if err := x.MsgSend(in, drpcEncoding_File_tailnet_proto_tailnet_proto{}); err != nil {
		return nil, err
	}
	if err := x.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type DRPCTailnet_StreamDERPMapsClient interface {
	drpc.Stream
	Recv() (*DERPMap, error)
}

type drpcTailnet_StreamDERPMapsClient struct {
	drpc.Stream
}

func (x *drpcTailnet_StreamDERPMapsClient) GetStream() drpc.Stream {
	return x.Stream
}

func (x *drpcTailnet_StreamDERPMapsClient) Recv() (*DERPMap, error) {
	m := new(DERPMap)
	if err := x.MsgRecv(m, drpcEncoding_File_tailnet_proto_tailnet_proto{}); err != nil {
		return nil, err
	}
	return m, nil
}

func (x *drpcTailnet_StreamDERPMapsClient) RecvMsg(m *DERPMap) error {
	return x.MsgRecv(m, drpcEncoding_File_tailnet_proto_tailnet_proto{})
}

func (c *drpcTailnetClient) RefreshResumeToken(ctx context.Context, in *RefreshResumeTokenRequest) (*RefreshResumeTokenResponse, error) {
	out := new(RefreshResumeTokenResponse)
	err := c.cc.Invoke(ctx, "/coder.tailnet.v2.Tailnet/RefreshResumeToken", drpcEncoding_File_tailnet_proto_tailnet_proto{}, in, out)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *drpcTailnetClient) Coordinate(ctx context.Context) (DRPCTailnet_CoordinateClient, error) {
	stream, err := c.cc.NewStream(ctx, "/coder.tailnet.v2.Tailnet/Coordinate", drpcEncoding_File_tailnet_proto_tailnet_proto{})
	if err != nil {
		return nil, err
	}
	x := &drpcTailnet_CoordinateClient{stream}
	return x, nil
}

type DRPCTailnet_CoordinateClient interface {
	drpc.Stream
	Send(*CoordinateRequest) error
	Recv() (*CoordinateResponse, error)
}

type drpcTailnet_CoordinateClient struct {
	drpc.Stream
}

func (x *drpcTailnet_CoordinateClient) GetStream() drpc.Stream {
	return x.Stream
}

func (x *drpcTailnet_CoordinateClient) Send(m *CoordinateRequest) error {
	return x.MsgSend(m, drpcEncoding_File_tailnet_proto_tailnet_proto{})
}

func (x *drpcTailnet_CoordinateClient) Recv() (*CoordinateResponse, error) {
	m := new(CoordinateResponse)
	if err := x.MsgRecv(m, drpcEncoding_File_tailnet_proto_tailnet_proto{}); err != nil {
		return nil, err
	}
	return m, nil
}

func (x *drpcTailnet_CoordinateClient) RecvMsg(m *CoordinateResponse) error {
	return x.MsgRecv(m, drpcEncoding_File_tailnet_proto_tailnet_proto{})
}

func (c *drpcTailnetClient) WorkspaceUpdates(ctx context.Context, in *WorkspaceUpdatesRequest) (DRPCTailnet_WorkspaceUpdatesClient, error) {
	stream, err := c.cc.NewStream(ctx, "/coder.tailnet.v2.Tailnet/WorkspaceUpdates", drpcEncoding_File_tailnet_proto_tailnet_proto{})
	if err != nil {
		return nil, err
	}
	x := &drpcTailnet_WorkspaceUpdatesClient{stream}
	if err := x.MsgSend(in, drpcEncoding_File_tailnet_proto_tailnet_proto{}); err != nil {
		return nil, err
	}
	if err := x.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type DRPCTailnet_WorkspaceUpdatesClient interface {
	drpc.Stream
	Recv() (*WorkspaceUpdate, error)
}

type drpcTailnet_WorkspaceUpdatesClient struct {
	drpc.Stream
}

func (x *drpcTailnet_WorkspaceUpdatesClient) GetStream() drpc.Stream {
	return x.Stream
}

func (x *drpcTailnet_WorkspaceUpdatesClient) Recv() (*WorkspaceUpdate, error) {
	m := new(WorkspaceUpdate)
	if err := x.MsgRecv(m, drpcEncoding_File_tailnet_proto_tailnet_proto{}); err != nil {
		return nil, err
	}
	return m, nil
}

func (x *drpcTailnet_WorkspaceUpdatesClient) RecvMsg(m *WorkspaceUpdate) error {
	return x.MsgRecv(m, drpcEncoding_File_tailnet_proto_tailnet_proto{})
}

type DRPCTailnetServer interface {
	PostTelemetry(context.Context, *TelemetryRequest) (*TelemetryResponse, error)
	StreamDERPMaps(*StreamDERPMapsRequest, DRPCTailnet_StreamDERPMapsStream) error
	RefreshResumeToken(context.Context, *RefreshResumeTokenRequest) (*RefreshResumeTokenResponse, error)
	Coordinate(DRPCTailnet_CoordinateStream) error
	WorkspaceUpdates(*WorkspaceUpdatesRequest, DRPCTailnet_WorkspaceUpdatesStream) error
}

type DRPCTailnetUnimplementedServer struct{}

func (s *DRPCTailnetUnimplementedServer) PostTelemetry(context.Context, *TelemetryRequest) (*TelemetryResponse, error) {
	return nil, drpcerr.WithCode(errors.New("Unimplemented"), drpcerr.Unimplemented)
}

func (s *DRPCTailnetUnimplementedServer) StreamDERPMaps(*StreamDERPMapsRequest, DRPCTailnet_StreamDERPMapsStream) error {
	return drpcerr.WithCode(errors.New("Unimplemented"), drpcerr.Unimplemented)
}

func (s *DRPCTailnetUnimplementedServer) RefreshResumeToken(context.Context, *RefreshResumeTokenRequest) (*RefreshResumeTokenResponse, error) {
	return nil, drpcerr.WithCode(errors.New("Unimplemented"), drpcerr.Unimplemented)
}

func (s *DRPCTailnetUnimplementedServer) Coordinate(DRPCTailnet_CoordinateStream) error {
	return drpcerr.WithCode(errors.New("Unimplemented"), drpcerr.Unimplemented)
}

func (s *DRPCTailnetUnimplementedServer) WorkspaceUpdates(*WorkspaceUpdatesRequest, DRPCTailnet_WorkspaceUpdatesStream) error {
	return drpcerr.WithCode(errors.New("Unimplemented"), drpcerr.Unimplemented)
}

type DRPCTailnetDescription struct{}

func (DRPCTailnetDescription) NumMethods() int { return 5 }

func (DRPCTailnetDescription) Method(n int) (string, drpc.Encoding, drpc.Receiver, interface{}, bool) {
	switch n {
	case 0:
		return "/coder.tailnet.v2.Tailnet/PostTelemetry", drpcEncoding_File_tailnet_proto_tailnet_proto{},
			func(srv interface{}, ctx context.Context, in1, in2 interface{}) (drpc.Message, error) {
				return srv.(DRPCTailnetServer).
					PostTelemetry(
						ctx,
						in1.(*TelemetryRequest),
					)
			}, DRPCTailnetServer.PostTelemetry, true
	case 1:
		return "/coder.tailnet.v2.Tailnet/StreamDERPMaps", drpcEncoding_File_tailnet_proto_tailnet_proto{},
			func(srv interface{}, ctx context.Context, in1, in2 interface{}) (drpc.Message, error) {
				return nil, srv.(DRPCTailnetServer).
					StreamDERPMaps(
						in1.(*StreamDERPMapsRequest),
						&drpcTailnet_StreamDERPMapsStream{in2.(drpc.Stream)},
					)
			}, DRPCTailnetServer.StreamDERPMaps, true
	case 2:
		return "/coder.tailnet.v2.Tailnet/RefreshResumeToken", drpcEncoding_File_tailnet_proto_tailnet_proto{},
			func(srv interface{}, ctx context.Context, in1, in2 interface{}) (drpc.Message, error) {
				return srv.(DRPCTailnetServer).
					RefreshResumeToken(
						ctx,
						in1.(*RefreshResumeTokenRequest),
					)
			}, DRPCTailnetServer.RefreshResumeToken, true
	case 3:
		return "/coder.tailnet.v2.Tailnet/Coordinate", drpcEncoding_File_tailnet_proto_tailnet_proto{},
			func(srv interface{}, ctx context.Context, in1, in2 interface{}) (drpc.Message, error) {
				return nil, srv.(DRPCTailnetServer).
					Coordinate(
						&drpcTailnet_CoordinateStream{in1.(drpc.Stream)},
					)
			}, DRPCTailnetServer.Coordinate, true
	case 4:
		return "/coder.tailnet.v2.Tailnet/WorkspaceUpdates", drpcEncoding_File_tailnet_proto_tailnet_proto{},
			func(srv interface{}, ctx context.Context, in1, in2 interface{}) (drpc.Message, error) {
				return nil, srv.(DRPCTailnetServer).
					WorkspaceUpdates(
						in1.(*WorkspaceUpdatesRequest),
						&drpcTailnet_WorkspaceUpdatesStream{in2.(drpc.Stream)},
					)
			}, DRPCTailnetServer.WorkspaceUpdates, true
	default:
		return "", nil, nil, nil, false
	}
}

func DRPCRegisterTailnet(mux drpc.Mux, impl DRPCTailnetServer) error {
	return mux.Register(impl, DRPCTailnetDescription{})
}

type DRPCTailnet_PostTelemetryStream interface {
	drpc.Stream
	SendAndClose(*TelemetryResponse) error
}

type drpcTailnet_PostTelemetryStream struct {
	drpc.Stream
}

func (x *drpcTailnet_PostTelemetryStream) SendAndClose(m *TelemetryResponse) error {
	if err := x.MsgSend(m, drpcEncoding_File_tailnet_proto_tailnet_proto{}); err != nil {
		return err
	}
	return x.CloseSend()
}

type DRPCTailnet_StreamDERPMapsStream interface {
	drpc.Stream
	Send(*DERPMap) error
}

type drpcTailnet_StreamDERPMapsStream struct {
	drpc.Stream
}

func (x *drpcTailnet_StreamDERPMapsStream) Send(m *DERPMap) error {
	return x.MsgSend(m, drpcEncoding_File_tailnet_proto_tailnet_proto{})
}

type DRPCTailnet_RefreshResumeTokenStream interface {
	drpc.Stream
	SendAndClose(*RefreshResumeTokenResponse) error
}

type drpcTailnet_RefreshResumeTokenStream struct {
	drpc.Stream
}

func (x *drpcTailnet_RefreshResumeTokenStream) SendAndClose(m *RefreshResumeTokenResponse) error {
	if err := x.MsgSend(m, drpcEncoding_File_tailnet_proto_tailnet_proto{}); err != nil {
		return err
	}
	return x.CloseSend()
}

type DRPCTailnet_CoordinateStream interface {
	drpc.Stream
	Send(*CoordinateResponse) error
	Recv() (*CoordinateRequest, error)
}

type drpcTailnet_CoordinateStream struct {
	drpc.Stream
}

func (x *drpcTailnet_CoordinateStream) Send(m *CoordinateResponse) error {
	return x.MsgSend(m, drpcEncoding_File_tailnet_proto_tailnet_proto{})
}

func (x *drpcTailnet_CoordinateStream) Recv() (*CoordinateRequest, error) {
	m := new(CoordinateRequest)
	if err := x.MsgRecv(m, drpcEncoding_File_tailnet_proto_tailnet_proto{}); err != nil {
		return nil, err
	}
	return m, nil
}

func (x *drpcTailnet_CoordinateStream) RecvMsg(m *CoordinateRequest) error {
	return x.MsgRecv(m, drpcEncoding_File_tailnet_proto_tailnet_proto{})
}

type DRPCTailnet_WorkspaceUpdatesStream interface {
	drpc.Stream
	Send(*WorkspaceUpdate) error
}

type drpcTailnet_WorkspaceUpdatesStream struct {
	drpc.Stream
}

func (x *drpcTailnet_WorkspaceUpdatesStream) Send(m *WorkspaceUpdate) error {
	return x.MsgSend(m, drpcEncoding_File_tailnet_proto_tailnet_proto{})
}
