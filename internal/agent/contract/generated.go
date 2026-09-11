package contract

import (
	agentpb "panel/internal/agent/pb"

	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/descriptorpb"
)

const agentService = "panel.agent.v1.AgentService"

func CurrentContract() Contract {
	file := protodesc.ToFileDescriptorProto(agentpb.File_agent_proto)
	methods := methodsFromDescriptor(agentpb.File_agent_proto.Services())
	return Contract{ProtoFile: file, Methods: methods}
}

func methodsFromDescriptor(services protoreflect.ServiceDescriptors) []Method {
	var methods []Method
	for i := 0; i < services.Len(); i++ {
		service := services.Get(i)
		for j := 0; j < service.Methods().Len(); j++ {
			method := service.Methods().Get(j)
			methods = append(methods, Method{
				ID:       string(method.Name()),
				Service:  string(service.FullName()),
				RPC:      string(method.Name()),
				Request:  &Schema{Type: string(method.Input().FullName())},
				Response: &Schema{Type: string(method.Output().FullName())},
			})
		}
	}
	return methods
}

var _ *descriptorpb.FileDescriptorProto
