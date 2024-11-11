package redis_failover

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestNewFailoverManager(t *testing.T) {
	config := FailoverConfig{
		MainAddr:          "localhost:6379", // redis主机地址
		BackupAddr:        "localhost:6380", // redis备用机地址
		HeartbeatInterval: 1 * time.Second,  // 心跳检测时长
		FailureThreshold:  3,                // 故障阈值
		RecoverThreshold:  3,                // 恢复阈值
		GraySteps:         5,
		StepInterval:      2 * time.Second,
	}

	manager := NewFailoverManager(config)
	manager.Start()
	defer manager.Stop()

	ctx := context.Background()

	for i := 0; i < 100; i++ {
		statusCmd := manager.RedisClient().Set(ctx, "k1", "v1", 0)
		if statusCmd.Err() != nil {
			fmt.Printf("err = %v\n", statusCmd.Err())
		} else {
			fmt.Printf("active = %v\n", manager.currentActive)
		}
		time.Sleep(1 * time.Second)
	}
}
