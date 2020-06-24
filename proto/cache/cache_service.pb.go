// Copyright 2018 The Goma Authors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.25.0
// 	protoc        v3.12.3
// source: cache/cache_service.proto

package cache

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

var File_cache_cache_service_proto protoreflect.FileDescriptor

var file_cache_cache_service_proto_rawDesc = []byte{
	0x0a, 0x19, 0x63, 0x61, 0x63, 0x68, 0x65, 0x2f, 0x63, 0x61, 0x63, 0x68, 0x65, 0x5f, 0x73, 0x65,
	0x72, 0x76, 0x69, 0x63, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x05, 0x63, 0x61, 0x63,
	0x68, 0x65, 0x1a, 0x11, 0x63, 0x61, 0x63, 0x68, 0x65, 0x2f, 0x63, 0x61, 0x63, 0x68, 0x65, 0x2e,
	0x70, 0x72, 0x6f, 0x74, 0x6f, 0x32, 0x5e, 0x0a, 0x0c, 0x43, 0x61, 0x63, 0x68, 0x65, 0x53, 0x65,
	0x72, 0x76, 0x69, 0x63, 0x65, 0x12, 0x26, 0x0a, 0x03, 0x47, 0x65, 0x74, 0x12, 0x0d, 0x2e, 0x63,
	0x61, 0x63, 0x68, 0x65, 0x2e, 0x47, 0x65, 0x74, 0x52, 0x65, 0x71, 0x1a, 0x0e, 0x2e, 0x63, 0x61,
	0x63, 0x68, 0x65, 0x2e, 0x47, 0x65, 0x74, 0x52, 0x65, 0x73, 0x70, 0x22, 0x00, 0x12, 0x26, 0x0a,
	0x03, 0x50, 0x75, 0x74, 0x12, 0x0d, 0x2e, 0x63, 0x61, 0x63, 0x68, 0x65, 0x2e, 0x50, 0x75, 0x74,
	0x52, 0x65, 0x71, 0x1a, 0x0e, 0x2e, 0x63, 0x61, 0x63, 0x68, 0x65, 0x2e, 0x50, 0x75, 0x74, 0x52,
	0x65, 0x73, 0x70, 0x22, 0x00, 0x42, 0x29, 0x5a, 0x27, 0x67, 0x6f, 0x2e, 0x63, 0x68, 0x72, 0x6f,
	0x6d, 0x69, 0x75, 0x6d, 0x2e, 0x6f, 0x72, 0x67, 0x2f, 0x67, 0x6f, 0x6d, 0x61, 0x2f, 0x73, 0x65,
	0x72, 0x76, 0x65, 0x72, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2f, 0x63, 0x61, 0x63, 0x68, 0x65,
	0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x33,
}

var file_cache_cache_service_proto_goTypes = []interface{}{
	(*GetReq)(nil),  // 0: cache.GetReq
	(*PutReq)(nil),  // 1: cache.PutReq
	(*GetResp)(nil), // 2: cache.GetResp
	(*PutResp)(nil), // 3: cache.PutResp
}
var file_cache_cache_service_proto_depIdxs = []int32{
	0, // 0: cache.CacheService.Get:input_type -> cache.GetReq
	1, // 1: cache.CacheService.Put:input_type -> cache.PutReq
	2, // 2: cache.CacheService.Get:output_type -> cache.GetResp
	3, // 3: cache.CacheService.Put:output_type -> cache.PutResp
	2, // [2:4] is the sub-list for method output_type
	0, // [0:2] is the sub-list for method input_type
	0, // [0:0] is the sub-list for extension type_name
	0, // [0:0] is the sub-list for extension extendee
	0, // [0:0] is the sub-list for field type_name
}

func init() { file_cache_cache_service_proto_init() }
func file_cache_cache_service_proto_init() {
	if File_cache_cache_service_proto != nil {
		return
	}
	file_cache_cache_proto_init()
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_cache_cache_service_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   0,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_cache_cache_service_proto_goTypes,
		DependencyIndexes: file_cache_cache_service_proto_depIdxs,
	}.Build()
	File_cache_cache_service_proto = out.File
	file_cache_cache_service_proto_rawDesc = nil
	file_cache_cache_service_proto_goTypes = nil
	file_cache_cache_service_proto_depIdxs = nil
}

// Reference imports to suppress errors if they are not otherwise used.
var _ context.Context
var _ grpc.ClientConnInterface

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion6

// CacheServiceClient is the client API for CacheService service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://godoc.org/google.golang.org/grpc#ClientConn.NewStream.
type CacheServiceClient interface {
	Get(ctx context.Context, in *GetReq, opts ...grpc.CallOption) (*GetResp, error)
	Put(ctx context.Context, in *PutReq, opts ...grpc.CallOption) (*PutResp, error)
}

type cacheServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewCacheServiceClient(cc grpc.ClientConnInterface) CacheServiceClient {
	return &cacheServiceClient{cc}
}

func (c *cacheServiceClient) Get(ctx context.Context, in *GetReq, opts ...grpc.CallOption) (*GetResp, error) {
	out := new(GetResp)
	err := c.cc.Invoke(ctx, "/cache.CacheService/Get", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (c *cacheServiceClient) Put(ctx context.Context, in *PutReq, opts ...grpc.CallOption) (*PutResp, error) {
	out := new(PutResp)
	err := c.cc.Invoke(ctx, "/cache.CacheService/Put", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CacheServiceServer is the server API for CacheService service.
type CacheServiceServer interface {
	Get(context.Context, *GetReq) (*GetResp, error)
	Put(context.Context, *PutReq) (*PutResp, error)
}

// UnimplementedCacheServiceServer can be embedded to have forward compatible implementations.
type UnimplementedCacheServiceServer struct {
}

func (*UnimplementedCacheServiceServer) Get(context.Context, *GetReq) (*GetResp, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Get not implemented")
}
func (*UnimplementedCacheServiceServer) Put(context.Context, *PutReq) (*PutResp, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Put not implemented")
}

func RegisterCacheServiceServer(s *grpc.Server, srv CacheServiceServer) {
	s.RegisterService(&_CacheService_serviceDesc, srv)
}

func _CacheService_Get_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetReq)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CacheServiceServer).Get(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/cache.CacheService/Get",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CacheServiceServer).Get(ctx, req.(*GetReq))
	}
	return interceptor(ctx, in, info, handler)
}

func _CacheService_Put_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(PutReq)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(CacheServiceServer).Put(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/cache.CacheService/Put",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(CacheServiceServer).Put(ctx, req.(*PutReq))
	}
	return interceptor(ctx, in, info, handler)
}

var _CacheService_serviceDesc = grpc.ServiceDesc{
	ServiceName: "cache.CacheService",
	HandlerType: (*CacheServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Get",
			Handler:    _CacheService_Get_Handler,
		},
		{
			MethodName: "Put",
			Handler:    _CacheService_Put_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "cache/cache_service.proto",
}
