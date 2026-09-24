package v1

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// JSONProto 把protobuf的消息用json格式返回给客户端
func JSONProto(c *gin.Context, status int, m proto.Message) {
	mo := protojson.MarshalOptions{EmitUnpopulated: true, UseProtoNames: true}
	b, err := mo.Marshal(m)
	if err != nil {
		WriteProblem(c, http.StatusInternalServerError, "serialization_error", "服务器暂时无法处理请求")
		return
	}
	c.Data(status, "application/json", b)
}
