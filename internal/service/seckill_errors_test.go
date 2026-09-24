package service

import (
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestSeckillErrorCarriesStableReasonAndRequestID(t *testing.T) {
	err := seckillError(codes.AlreadyExists, "ALREADY_PARTICIPATED", "已参与", "create:7:9")
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.AlreadyExists {
		t.Fatalf("unexpected gRPC status: %v", err)
	}
	if len(st.Details()) != 1 {
		t.Fatalf("missing ErrorInfo detail: %v", st.Details())
	}
	info, ok := st.Details()[0].(*errdetails.ErrorInfo)
	if !ok || info.Reason != "ALREADY_PARTICIPATED" || info.Metadata["request_id"] != "create:7:9" {
		t.Fatalf("unexpected ErrorInfo: %v", info)
	}
}
