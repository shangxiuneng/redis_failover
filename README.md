### 项目介绍
golang实现的redis故障自动转移包，适用于对redis可用性要求比较高的场景。

### 原理介绍
1. 准备两个redis集群，一个为main集群，是业务主要使用的集群，一个是backup集群，main集群崩溃后故障转移的集群
2. 定时向main集群和backup集群发送心跳包，如果累计N次main集群的心跳包都没有通，则认为main集群已经挂掉，开始逐渐切换流量到backup集群上。
3. 后续继续保持对main集群的心跳包，如果累计N次心跳都正常，则证明main集群已经回复了，切流量到main集群上。
4. 应该注意的是，不管切换到main集群还是backup集群，均应该灰度进行。

### 实现说明

### 使用方法
```go
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
```