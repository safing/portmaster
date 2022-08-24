// Code generated by protoc-gen-go. DO NOT EDIT.
// versions:
// 	protoc-gen-go v1.28.0
// 	protoc        v3.21.2
// source: base.proto

package proto

import (
	protoreflect "google.golang.org/protobuf/reflect/protoreflect"
	protoimpl "google.golang.org/protobuf/runtime/protoimpl"
	reflect "reflect"
	sync "sync"
)

const (
	// Verify that this generated code is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(20 - protoimpl.MinVersion)
	// Verify that runtime/protoimpl is sufficiently up-to-date.
	_ = protoimpl.EnforceVersion(protoimpl.MaxVersion - 20)
)

type ConfigureRequest struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields

	// The directory of the portmaster installation.
	BaseDirectory string `protobuf:"bytes,1,opt,name=base_directory,json=baseDirectory,proto3" json:"base_directory,omitempty"`
	// The name of the plugin as specified by the user.
	PluginName    string `protobuf:"bytes,2,opt,name=plugin_name,json=pluginName,proto3" json:"plugin_name,omitempty"`
	BackchannelId uint32 `protobuf:"varint,3,opt,name=backchannel_id,json=backchannelId,proto3" json:"backchannel_id,omitempty"`
	StaticConfig  []byte `protobuf:"bytes,4,opt,name=static_config,json=staticConfig,proto3" json:"static_config,omitempty"`
}

func (x *ConfigureRequest) Reset() {
	*x = ConfigureRequest{}
	if protoimpl.UnsafeEnabled {
		mi := &file_base_proto_msgTypes[0]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *ConfigureRequest) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ConfigureRequest) ProtoMessage() {}

func (x *ConfigureRequest) ProtoReflect() protoreflect.Message {
	mi := &file_base_proto_msgTypes[0]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ConfigureRequest.ProtoReflect.Descriptor instead.
func (*ConfigureRequest) Descriptor() ([]byte, []int) {
	return file_base_proto_rawDescGZIP(), []int{0}
}

func (x *ConfigureRequest) GetBaseDirectory() string {
	if x != nil {
		return x.BaseDirectory
	}
	return ""
}

func (x *ConfigureRequest) GetPluginName() string {
	if x != nil {
		return x.PluginName
	}
	return ""
}

func (x *ConfigureRequest) GetBackchannelId() uint32 {
	if x != nil {
		return x.BackchannelId
	}
	return 0
}

func (x *ConfigureRequest) GetStaticConfig() []byte {
	if x != nil {
		return x.StaticConfig
	}
	return nil
}

type ConfigureResponse struct {
	state         protoimpl.MessageState
	sizeCache     protoimpl.SizeCache
	unknownFields protoimpl.UnknownFields
}

func (x *ConfigureResponse) Reset() {
	*x = ConfigureResponse{}
	if protoimpl.UnsafeEnabled {
		mi := &file_base_proto_msgTypes[1]
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		ms.StoreMessageInfo(mi)
	}
}

func (x *ConfigureResponse) String() string {
	return protoimpl.X.MessageStringOf(x)
}

func (*ConfigureResponse) ProtoMessage() {}

func (x *ConfigureResponse) ProtoReflect() protoreflect.Message {
	mi := &file_base_proto_msgTypes[1]
	if protoimpl.UnsafeEnabled && x != nil {
		ms := protoimpl.X.MessageStateOf(protoimpl.Pointer(x))
		if ms.LoadMessageInfo() == nil {
			ms.StoreMessageInfo(mi)
		}
		return ms
	}
	return mi.MessageOf(x)
}

// Deprecated: Use ConfigureResponse.ProtoReflect.Descriptor instead.
func (*ConfigureResponse) Descriptor() ([]byte, []int) {
	return file_base_proto_rawDescGZIP(), []int{1}
}

var File_base_proto protoreflect.FileDescriptor

var file_base_proto_rawDesc = []byte{
	0x0a, 0x0a, 0x62, 0x61, 0x73, 0x65, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x12, 0x1e, 0x73, 0x61,
	0x66, 0x69, 0x6e, 0x67, 0x2e, 0x70, 0x6f, 0x72, 0x74, 0x6d, 0x61, 0x73, 0x74, 0x65, 0x72, 0x2e,
	0x70, 0x6c, 0x75, 0x67, 0x69, 0x6e, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x22, 0xa6, 0x01, 0x0a,
	0x10, 0x43, 0x6f, 0x6e, 0x66, 0x69, 0x67, 0x75, 0x72, 0x65, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73,
	0x74, 0x12, 0x25, 0x0a, 0x0e, 0x62, 0x61, 0x73, 0x65, 0x5f, 0x64, 0x69, 0x72, 0x65, 0x63, 0x74,
	0x6f, 0x72, 0x79, 0x18, 0x01, 0x20, 0x01, 0x28, 0x09, 0x52, 0x0d, 0x62, 0x61, 0x73, 0x65, 0x44,
	0x69, 0x72, 0x65, 0x63, 0x74, 0x6f, 0x72, 0x79, 0x12, 0x1f, 0x0a, 0x0b, 0x70, 0x6c, 0x75, 0x67,
	0x69, 0x6e, 0x5f, 0x6e, 0x61, 0x6d, 0x65, 0x18, 0x02, 0x20, 0x01, 0x28, 0x09, 0x52, 0x0a, 0x70,
	0x6c, 0x75, 0x67, 0x69, 0x6e, 0x4e, 0x61, 0x6d, 0x65, 0x12, 0x25, 0x0a, 0x0e, 0x62, 0x61, 0x63,
	0x6b, 0x63, 0x68, 0x61, 0x6e, 0x6e, 0x65, 0x6c, 0x5f, 0x69, 0x64, 0x18, 0x03, 0x20, 0x01, 0x28,
	0x0d, 0x52, 0x0d, 0x62, 0x61, 0x63, 0x6b, 0x63, 0x68, 0x61, 0x6e, 0x6e, 0x65, 0x6c, 0x49, 0x64,
	0x12, 0x23, 0x0a, 0x0d, 0x73, 0x74, 0x61, 0x74, 0x69, 0x63, 0x5f, 0x63, 0x6f, 0x6e, 0x66, 0x69,
	0x67, 0x18, 0x04, 0x20, 0x01, 0x28, 0x0c, 0x52, 0x0c, 0x73, 0x74, 0x61, 0x74, 0x69, 0x63, 0x43,
	0x6f, 0x6e, 0x66, 0x69, 0x67, 0x22, 0x13, 0x0a, 0x11, 0x43, 0x6f, 0x6e, 0x66, 0x69, 0x67, 0x75,
	0x72, 0x65, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x32, 0x81, 0x01, 0x0a, 0x0b, 0x42,
	0x61, 0x73, 0x65, 0x53, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x12, 0x72, 0x0a, 0x09, 0x43, 0x6f,
	0x6e, 0x66, 0x69, 0x67, 0x75, 0x72, 0x65, 0x12, 0x30, 0x2e, 0x73, 0x61, 0x66, 0x69, 0x6e, 0x67,
	0x2e, 0x70, 0x6f, 0x72, 0x74, 0x6d, 0x61, 0x73, 0x74, 0x65, 0x72, 0x2e, 0x70, 0x6c, 0x75, 0x67,
	0x69, 0x6e, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2e, 0x43, 0x6f, 0x6e, 0x66, 0x69, 0x67, 0x75,
	0x72, 0x65, 0x52, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x1a, 0x31, 0x2e, 0x73, 0x61, 0x66, 0x69,
	0x6e, 0x67, 0x2e, 0x70, 0x6f, 0x72, 0x74, 0x6d, 0x61, 0x73, 0x74, 0x65, 0x72, 0x2e, 0x70, 0x6c,
	0x75, 0x67, 0x69, 0x6e, 0x2e, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x2e, 0x43, 0x6f, 0x6e, 0x66, 0x69,
	0x67, 0x75, 0x72, 0x65, 0x52, 0x65, 0x73, 0x70, 0x6f, 0x6e, 0x73, 0x65, 0x22, 0x00, 0x42, 0x09,
	0x5a, 0x07, 0x2e, 0x2f, 0x70, 0x72, 0x6f, 0x74, 0x6f, 0x62, 0x06, 0x70, 0x72, 0x6f, 0x74, 0x6f,
	0x33,
}

var (
	file_base_proto_rawDescOnce sync.Once
	file_base_proto_rawDescData = file_base_proto_rawDesc
)

func file_base_proto_rawDescGZIP() []byte {
	file_base_proto_rawDescOnce.Do(func() {
		file_base_proto_rawDescData = protoimpl.X.CompressGZIP(file_base_proto_rawDescData)
	})
	return file_base_proto_rawDescData
}

var file_base_proto_msgTypes = make([]protoimpl.MessageInfo, 2)
var file_base_proto_goTypes = []interface{}{
	(*ConfigureRequest)(nil),  // 0: safing.portmaster.plugin.proto.ConfigureRequest
	(*ConfigureResponse)(nil), // 1: safing.portmaster.plugin.proto.ConfigureResponse
}
var file_base_proto_depIdxs = []int32{
	0, // 0: safing.portmaster.plugin.proto.BaseService.Configure:input_type -> safing.portmaster.plugin.proto.ConfigureRequest
	1, // 1: safing.portmaster.plugin.proto.BaseService.Configure:output_type -> safing.portmaster.plugin.proto.ConfigureResponse
	1, // [1:2] is the sub-list for method output_type
	0, // [0:1] is the sub-list for method input_type
	0, // [0:0] is the sub-list for extension type_name
	0, // [0:0] is the sub-list for extension extendee
	0, // [0:0] is the sub-list for field type_name
}

func init() { file_base_proto_init() }
func file_base_proto_init() {
	if File_base_proto != nil {
		return
	}
	if !protoimpl.UnsafeEnabled {
		file_base_proto_msgTypes[0].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*ConfigureRequest); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
		file_base_proto_msgTypes[1].Exporter = func(v interface{}, i int) interface{} {
			switch v := v.(*ConfigureResponse); i {
			case 0:
				return &v.state
			case 1:
				return &v.sizeCache
			case 2:
				return &v.unknownFields
			default:
				return nil
			}
		}
	}
	type x struct{}
	out := protoimpl.TypeBuilder{
		File: protoimpl.DescBuilder{
			GoPackagePath: reflect.TypeOf(x{}).PkgPath(),
			RawDescriptor: file_base_proto_rawDesc,
			NumEnums:      0,
			NumMessages:   2,
			NumExtensions: 0,
			NumServices:   1,
		},
		GoTypes:           file_base_proto_goTypes,
		DependencyIndexes: file_base_proto_depIdxs,
		MessageInfos:      file_base_proto_msgTypes,
	}.Build()
	File_base_proto = out.File
	file_base_proto_rawDesc = nil
	file_base_proto_goTypes = nil
	file_base_proto_depIdxs = nil
}
