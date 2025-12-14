package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"time"
)

// SeckillRequest 秒杀请求结构体
type SeckillRequest struct {
	UserID    int64 `json:"user_id"`
	ProductID int64 `json:"product_id"`
	Quantity  int32 `json:"quantity"`
}

func main() {
	// 配置参数
	const (
		totalUsers  = 500000
		productID   = 1003
		outputFile  = "seckill_data.json"
		quantityMax = 2
	)

	// 创建输出文件
	file, err := os.Create(outputFile)
	if err != nil {
		panic(fmt.Errorf("创建文件失败: %w", err))
	}
	defer file.Close()

	// 创建带缓冲的 writer（提升性能）
	writer := os.NewFile(file.Fd(), outputFile)

	// 写入 JSON 数组开头
	_, err = writer.WriteString("[\n")
	if err != nil {
		panic(err)
	}

	// 初始化随机数生成器
	rand.New(rand.NewSource(time.Now().UnixNano()))

	// 批量生成数据
	startTime := time.Now()
	batchSize := 1000 // 每批写入数量，用于进度显示

	for i := int64(1); i <= totalUsers; i++ {
		request := SeckillRequest{
			UserID:    i,
			ProductID: productID,
			Quantity:  int32(rand.Intn(quantityMax) + 1), // 生成 1 或 2
		}

		// 编码为 JSON
		jsonBytes, err := json.Marshal(request)
		if err != nil {
			panic(fmt.Errorf("JSON 编码失败: %w", err))
		}

		// 写入文件（注意换行符）
		_, err = writer.Write(jsonBytes)
		if err != nil {
			panic(err)
		}

		// 如果不是最后一条，添加逗号和换行
		if i < totalUsers {
			_, err = writer.WriteString(",\n")
			if err != nil {
				panic(err)
			}
		}

		// 显示进度
		if i%int64(batchSize) == 0 {
			fmt.Printf("已生成: %d/%d 条 (%.2f%%)\n", i, totalUsers, float64(i)*100/float64(totalUsers))
		}
	}

	// 写入 JSON 数组结尾
	_, err = writer.WriteString("\n]")
	if err != nil {
		panic(err)
	}

	// 确保所有数据写入磁盘
	err = writer.Sync()
	if err != nil {
		panic(err)
	}

	fmt.Printf("\n✅ 生成完成！\n")
	fmt.Printf("文件: %s\n", outputFile)
	fmt.Printf("总记录数: %d 条\n", totalUsers)

	// 获取文件大小
	fileInfo, err := os.Stat(outputFile)
	if err != nil {
		panic(err)
	}
	fmt.Printf("文件大小: %.2f MB\n", float64(fileInfo.Size())/1024/1024)

	fmt.Printf("耗时: %v\n", time.Since(startTime))
}
