package service

import (
	"context"
	"testing"

	"github.com/CCDD2022/seckill-system/pkg/e"
	"github.com/CCDD2022/seckill-system/proto_output/auth"
)

func TestPublicRegistrationRejectsReservedAdminName(t *testing.T) {
	service := NewAuthService(nil, "test-secret", 1)
	for _, username := range []string{"admin", "ADMIN", " admin "} {
		response, err := service.Register(context.Background(), &auth.RegisterRequest{Username: username, Password: "password"})
		if err != nil || response.GetCode() != e.INVALID_PARAMS {
			t.Fatalf("username=%q response=%v err=%v", username, response, err)
		}
	}
}
