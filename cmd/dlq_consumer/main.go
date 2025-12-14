package main

import (
	"fmt"
	"os"
	"time"

	"github.com/CCDD2022/seckill-system/internal/mq"
	"github.com/CCDD2022/seckill-system/pkg/app"
	"github.com/CCDD2022/seckill-system/pkg/logger"
)

const (
	dlqName = "order.create.dlq"
)

func main() {
	cfg := app.BootstrapApp()

	// 独立连接，避免影响主业务
	// 这里不需要绑定交换机，因为 setupDLQ 已经绑定好了，直接消费队列即可
	conn, ch, msgs, err := mq.NewConsumerChannel(&cfg.MQ, dlqName, "", "", true, 10, nil)
	if err != nil {
		logger.Fatal("DLQ consumer init failed", "err", err)
	}
	defer mq.CloseConsumer(conn, ch)

	logger.Info("DLQ Monitor started", "queue", dlqName)

	// 打开报警日志文件
	f, err := os.OpenFile("dlq_alarm.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		logger.Fatal("open dlq log file failed", "err", err)
	}
	defer f.Close()

	for d := range msgs {
		// 1. 记录报警日志
		logContent := fmt.Sprintf("[%s] ALARM: Dead Letter Received | MsgID: %s | Body: %s\n",
			time.Now().Format(time.DateTime),
			d.MessageId,
			string(d.Body))

		if _, err := f.WriteString(logContent); err != nil {
			logger.Error("write dlq log failed", "err", err)
		}

		// 2. 打印到控制台方便调试
		logger.Warn("ALARM: Dead letter received", "msg_id", d.MessageId)

		// 3. 确认消息（表示报警已处理，避免死信堆积）
		// 实际场景中可能需要人工确认后再Ack，或者转存到数据库
		_ = d.Ack(false)
	}
}
