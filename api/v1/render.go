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
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "serialization error"})
		return
	}
	c.Data(status, "application/json", b)
}
