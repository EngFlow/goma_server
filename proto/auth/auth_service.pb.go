// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.25.0
// 	protoc        v3.12.3
// source: auth/auth_service.proto

package auth

import (
	context "context"
	proto "github.com/golang/protobuf/proto"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

// This is a compile-time assertion that a sufficiently up-to-date version
// of the legacy proto package is being used.
const _ = proto.ProtoPackageIsVersion4

var File_auth_auth_service_proto protoreflect.FileDescriptor

var file_auth_auth_service_proto_rawDesc = []byte{
	0x0a, 0x17, 0x61, 0x75, 0x74, 0x68, 0x2f, 0x61, 0x75, 0x74, 0x68, 0x5f, 0x73, 0x65, 0x72, 0x76,
	0x69, 0x63, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x04, 0x61, 0x75, 0x74, 0x68, 0x1a,
	0x0f, 0x61, 0x75, 0x74, 0x68, 0x2f, 0x61, 0x75, 0x74, 0x68, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f,
	0x32, 0x36, 0x0a, 0x0b, 0x41, 0x75, 0x74, 0x68, 0x53, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x12,
	0x27, 0x0a, 0x04, 0x41, 0x75, 0x74, 0x68, 0x12, 0x0d, 0x2e, 0x61, 0x75, 0x74, 0x68, 0x2e, 0x41,
	0x75, 0x74, 0x68, 0x52, 0x65, 0x71, 0x1a, 0x0e, 0x2e, 0x61, 0x75, 0x74, 0x68, 0x2e, 0x41, 0x75,
	0x74, 0x68, 0x52, 0x65, 0x73, 0x70, 0x22, 0x00, 0x42, 0x28, 0x5a, 0x26, 0x67, 0x6f, 0x2e, 0x63,
	0x68, 0x72, 0x6f, 0x6d, 0x69, 0x75, 0x6d, 0x2e, 0x6f, 0x72, 0x67, 0x2f, 0x67, 0x6f, 0x6d, 0x61,
	0x2f, 0x73, 0x65, 0x72, 0x76, 0x65, 0x72, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f, 0x61, 0x75,
	0x74, 0x68, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var file_auth_auth_service_proto_goTypes = []interface{}{
	(*AuthReq)(nil),  // 0: auth.AuthReq
	(*AuthResp)(nil), // 1: auth.AuthResp
}
var file_auth_auth_service_proto_depIdxs = []int32{
	0, // 0: auth.AuthService.Auth:input_type -> auth.AuthReq
	1, // 1: auth.AuthService.Auth:output_type -> auth.AuthResp
	1, // [1:2] is the sub-list for method output_type
	0, // [0:1] is the sub-list for method input_type
	0, // [0:0] is the sub-list for extension type_name
	0, // [0:0] is the sub-list for extension extendee
	0, // [0:0] is the sub-list for field type_name
}

func init() { file_auth_auth_service_proto_init() }
func file_auth_auth_service_proto_init() {
	if File_auth_auth_service_proto != nil {
		return
	}
	file_auth_auth_proto_init()
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_auth_auth_service_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   0,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_auth_auth_service_proto_goTypes,
		DependencyIndexes: file_auth_auth_service_proto_depIdxs,
	}.Build()
	File_auth_auth_service_proto = out.File
	file_auth_auth_service_proto_rawDesc = nil
	file_auth_auth_service_proto_goTypes = nil
	file_auth_auth_service_proto_depIdxs = nil
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConnInterface

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion6

// AuthServiceClient is the client API for AuthService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://godoc.org/google.golang.org/grpc#ClientConn.NewStream.
type AuthServiceClient interface {
	Auth(ctx context.Context, in *AuthReq, opts ...grpc.CallOption) (*AuthResp, error)
}

type authServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewAuthServiceClient(cc grpc.ClientConnInterface) AuthServiceClient {
	return &authServiceClient{cc}
}

func (c *authServiceClient) Auth(ctx context.Context, in *AuthReq, opts ...grpc.CallOption) (*AuthResp, error) {
	out := new(AuthResp)
	err := c.cc.Invoke(ctx, "/auth.AuthService/Auth", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// AuthServiceServer is the server API for AuthService service.
type AuthServiceServer interface {
	Auth(context.Context, *AuthReq) (*AuthResp, error)
}

// UnimplementedAuthServiceServer can be embedded to have forward compatible implementations.
type UnimplementedAuthServiceServer struct {
}

func (*UnimplementedAuthServiceServer) Auth(context.Context, *AuthReq) (*AuthResp, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Auth not implemented")
}

func RegisterAuthServiceServer(s *grpc.Server, srv AuthServiceServer) {
	s.RegisterService(&_AuthService_serviceDesc, srv)
}

func _AuthService_Auth_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(AuthReq)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(AuthServiceServer).Auth(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/auth.AuthService/Auth",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(AuthServiceServer).Auth(ctx, req.(*AuthReq))
	}
	return interceptor(ctx, in, info, handler)
}

var _AuthService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "auth.AuthService",
	HandlerType: (*AuthServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Auth",
			Handler:    _AuthService_Auth_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "auth/auth_service.proto",
}
