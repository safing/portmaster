//go:generate protoc -I ./ ./decider.proto ./network.proto ./base.proto ./reporter.proto ./config.proto ./notification.proto ./plugins.proto ./resolver.proto --go-grpc_out=../ --go_out=../
package proto
