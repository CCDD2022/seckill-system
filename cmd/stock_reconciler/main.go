package main

import (
	"context"
	"log"
	"strconv"
	"time"

	"github.com/CCDD2022/seckill-system/internal/dao/mysql"
	redisinit "github.com/CCDD2022/seckill-system/internal/dao/redis"
	"github.com/CCDD2022/seckill-system/pkg/app"
	"github.com/CCDD2022/seckill-system/pkg/logger"
	"github.com/redis/go-redis/v9"
)

const (
	dirtySetKey    = "product:stock:dirty:set"
	stockKeyPrefix = "product:stock:product:id"
	flushBatch     = 1000
	flushInterval  = 100 * time.Millisecond
)

func stockKey(id int64) string { return stockKeyPrefix + strconv.FormatInt(id, 10) }

func main() {
	cfg := app.BootstrapApp()

	// DB
	db, err := mysql.InitDB(&cfg.Database.Mysql)
	if err != nil {
		logger.Error("连接Mysql数据库失败: ", "err", err)
		log.Fatal(err)
	}

	// Redis
	rdb, err := redisinit.InitRedis(&cfg.Database.Redis)
	if err != nil {
		logger.Error("连接Redis失败: ", "err", err)
		log.Fatal(err)
	}

	logger.Info("Stock Reconciler started")
	ctx := context.Background()

	ticker := time.NewTicker(flushInterval)
	defer ticker.Stop()

	for range ticker.C {
		ids, err := popDirty(ctx, rdb, flushBatch)
		if err != nil {
			logger.Error("pop dirty failed", "err", err)
			continue
		}
		if len(ids) == 0 {
			continue
		}

		// Pipeline读取Redis库存
		type kv struct {
			id  int64
			cmd *redis.StringCmd
		}
		kvcmds := make([]kv, 0, len(ids))
		_, _ = rdb.Pipelined(ctx, func(pipe redis.Pipeliner) error {
			for _, id := range ids {
				cmd := pipe.Get(ctx, stockKey(id))
				kvcmds = append(kvcmds, kv{id: id, cmd: cmd})
			}
			return nil
		})

		// 组装批量更新
		pairs := make([]struct {
			id    int64
			stock int
		}, 0, len(kvcmds))
		for _, kv := range kvcmds {
			val, err := kv.cmd.Result()
			if err != nil {
				if err == redis.Nil {
					continue
				}
				logger.Error("redis get stock failed", "product_id", kv.id, "err", err)
				continue
			}
			st, err := strconv.Atoi(val)
			if err != nil {
				logger.Error("invalid stock value", "product_id", kv.id, "val", val)
				continue
			}
			pairs = append(pairs, struct {
				id    int64
				stock int
			}{id: kv.id, stock: st})
		}

		if len(pairs) == 0 {
			continue
		}

		// 构造单条 SQL 批量更新
		// UPDATE products SET stock = CASE id WHEN ? THEN ? ... END, updated_at = ? WHERE id IN (...)
		sql := "UPDATE products SET stock = CASE id"
		args := make([]interface{}, 0, len(pairs)*2+len(pairs)+1)
		idsWhere := make([]interface{}, 0, len(pairs))
		now := time.Now()
		for _, p := range pairs {
			sql += " WHEN ? THEN ?"
			args = append(args, p.id, p.stock)
			idsWhere = append(idsWhere, p.id)
		}
		sql += " END, updated_at = ? WHERE id IN ("
		args = append(args, now)
		for i := range idsWhere {
			if i > 0 {
				sql += ","
			}
			sql += "?"
			args = append(args, idsWhere[i])
		}
		sql += ")"

		if res := db.Exec(sql, args...); res.Error != nil {
			logger.Error("mysql batch update stock failed", "err", res.Error)
		} else {
			logger.Debug("batch stock reconciled", "count", len(pairs))
		}
	}
}

func popDirty(ctx context.Context, rdb redis.UniversalClient, n int) ([]int64, error) {
	// 使用 SPOP 批量弹出一批待对账商品ID，避免重复处理
	members, err := rdb.SPopN(ctx, dirtySetKey, int64(n)).Result()
	if err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(members))
	for _, m := range members {
		id, err := strconv.ParseInt(m, 10, 64)
		if err == nil {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
