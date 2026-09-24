package v1

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/CCDD2022/seckill-system/proto_output/seckill"
	"github.com/gin-gonic/gin"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type fakeSeckillClient struct {
	seckill.SeckillServiceClient
	executeResponse *seckill.SeckillResponse
	executeError    error
	statusResponse  *seckill.GetReservationStatusResponse
	statusError     error
}

func (f *fakeSeckillClient) ExecuteSeckill(_ context.Context, _ *seckill.SeckillRequest, _ ...grpc.CallOption) (*seckill.SeckillResponse, error) {
	return f.executeResponse, f.executeError
}

func (f *fakeSeckillClient) GetReservationStatus(_ context.Context, _ *seckill.GetReservationStatusRequest, _ ...grpc.CallOption) (*seckill.GetReservationStatusResponse, error) {
	return f.statusResponse, f.statusError
}

func seckillRequest(t *testing.T, client *fakeSeckillClient, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) { c.Set("user_id", int64(42)); c.Next() })
	NewSeckillHandler(client).RegisterRoutes(router.Group("/api/v1/seckill"))
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	return response
}

func decodeResponse(t *testing.T, response *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return body
}

func TestAcceptedReservationHasStatusResource(t *testing.T) {
	response := seckillRequest(t, &fakeSeckillClient{executeResponse: &seckill.SeckillResponse{Success: true}},
		http.MethodPost, "/api/v1/seckill/execute", `{"product_id":1003,"quantity":1}`)
	if response.Code != http.StatusAccepted || response.Header().Get("Location") != "/api/v1/seckill/requests/1003" {
		t.Fatalf("status=%d location=%q", response.Code, response.Header().Get("Location"))
	}
	body := decodeResponse(t, response)
	if body["request_id"] != "create:42:1003" || body["state"] != "processing" || body["status_url"] != "/api/v1/seckill/requests/1003" {
		t.Fatalf("unexpected accepted response: %v", body)
	}
	if _, hasOrderID := body["order_id"]; hasOrderID {
		t.Fatalf("accepted response exposed an uncreated order: %v", body)
	}
}

func TestSeckillBusinessErrorsAreProblems(t *testing.T) {
	withReason := func(code codes.Code, reason string) error {
		st, err := status.New(code, "do not parse this message").WithDetails(
			&errdetails.ErrorInfo{Reason: reason, Domain: "seckill-system"})
		if err != nil {
			t.Fatal(err)
		}
		return st.Err()
	}
	tests := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"duplicate", withReason(codes.AlreadyExists, "ALREADY_PARTICIPATED"), 409, "already_participated"},
		{"sold out", withReason(codes.ResourceExhausted, "SOLD_OUT"), 409, "sold_out"},
		{"campaign closed", withReason(codes.FailedPrecondition, "CAMPAIGN_INACTIVE"), 409, "campaign_inactive"},
		{"missing product", withReason(codes.NotFound, "PRODUCT_NOT_FOUND"), 404, "product_not_found"},
		{"unknown capacity", status.Error(codes.ResourceExhausted, "private broker detail"), 503, "service_unavailable"},
		{"timeout", status.Error(codes.DeadlineExceeded, "private broker detail"), 504, "outcome_unknown"},
		{"bare deadline", context.DeadlineExceeded, 504, "outcome_unknown"},
		{"unknown", errors.New("mysql password secret"), 500, "outcome_unknown"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := seckillRequest(t, &fakeSeckillClient{executeError: test.err}, http.MethodPost,
				"/api/v1/seckill/execute", `{"product_id":1003,"quantity":1}`)
			if response.Code != test.status || response.Header().Get("Content-Type") != "application/problem+json" {
				t.Fatalf("status=%d content-type=%q", response.Code, response.Header().Get("Content-Type"))
			}
			body := decodeResponse(t, response)
			if body["code"] != test.code || strings.Contains(response.Body.String(), "private") || strings.Contains(response.Body.String(), "secret") {
				t.Fatalf("unexpected problem: %v", body)
			}
			if test.code == "outcome_unknown" || test.code == "already_participated" {
				if response.Header().Get("Location") != "/api/v1/seckill/requests/1003" ||
					body["request_id"] != "create:42:1003" || body["status_url"] != "/api/v1/seckill/requests/1003" {
					t.Fatalf("problem lacks status resource: %v headers=%v", body, response.Header())
				}
			}
		})
	}
}

func TestReservationStatusReportsPersistedOrder(t *testing.T) {
	response := seckillRequest(t, &fakeSeckillClient{statusResponse: &seckill.GetReservationStatusResponse{
		State: "order_created", OrderId: 77, OrderStatus: 0,
	}}, http.MethodGet, "/api/v1/seckill/requests/1003", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	body := decodeResponse(t, response)
	if body["order_id"] != float64(77) || body["order_status"] != "pending_payment" {
		t.Fatalf("unexpected status resource: %v", body)
	}
}
