package service

import (
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// seckillError keeps the machine-readable reason separate from the localized
// message. The HTTP gateway maps these reasons to stable problem codes.
func seckillError(code codes.Code, reason, publicMessage, requestID string) error {
	base := status.New(code, publicMessage)
	info := &errdetails.ErrorInfo{Reason: reason, Domain: "seckill-system"}
	if requestID != "" {
		info.Metadata = map[string]string{"request_id": requestID}
	}
	withDetails, err := base.WithDetails(info)
	if err != nil {
		return base.Err()
	}
	return withDetails.Err()
}
