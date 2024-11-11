package redis_failover

import (
	"context"
	"github.com/redis/go-redis/v9"
	"log"
	"math/rand"
	"sync"
	"time"
)

// RedisClient 包装 Redis 客户端
type RedisClient struct {
	Client *redis.Client
	Name   string
}

// FailoverConfig 是 SDK 配置项
type FailoverConfig struct {
	MainAddr          string        // 主 Redis 地址
	BackupAddr        string        // 备 Redis 地址
	HeartbeatInterval time.Duration // 心跳间隔
	FailureThreshold  int           // 故障阈值
	RecoverThreshold  int           // 恢复阈值
	GraySteps         int           // 灰度步骤数
	StepInterval      time.Duration // 灰度每步的间隔
}

// TrafficWeights 记录主备 Redis 的流量权重
type TrafficWeights struct {
	Main   float64
	Backup float64
	mu     sync.RWMutex
}

// FailoverManager 是 SDK 的核心结构体
type FailoverManager struct {
	main          *RedisClient
	backup        *RedisClient
	targetClient  *RedisClient
	currentActive string

	config         FailoverConfig
	failCount      int
	recoverCount   int
	weights        *TrafficWeights
	weightsUpdated chan struct{}
	isSwitching    bool
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.Mutex
}

// NewFailoverManager 初始化新的 FailoverManager 实例
func NewFailoverManager(config FailoverConfig) *FailoverManager {
	mainClient := &RedisClient{
		Client: redis.NewClient(&redis.Options{
			Addr: config.MainAddr,
		}),
		Name: "main",
	}

	backupClient := &RedisClient{
		Client: redis.NewClient(&redis.Options{
			Addr: config.BackupAddr,
		}),
		Name: "backup",
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &FailoverManager{
		main:           mainClient,
		backup:         backupClient,
		currentActive:  "main",
		config:         config,
		weights:        &TrafficWeights{Main: 1.0, Backup: 0.0},
		weightsUpdated: make(chan struct{}, 1),
		ctx:            ctx,
		cancel:         cancel,
		targetClient:   mainClient,
	}
}

// Start 启动灰度切换和流量调度
func (fm *FailoverManager) Start() {
	go fm.heartbeatChecker()
	go fm.trafficDispatcher()
}

// Stop 停止 FailoverManager 及所有相关协程
func (fm *FailoverManager) Stop() {
	fm.cancel()
}

// heartbeatChecker 检查主备心跳
func (fm *FailoverManager) heartbeatChecker() {
	ticker := time.NewTicker(fm.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-fm.ctx.Done():
			return
		case <-ticker.C:
			mainOK := fm.ping(fm.main)
			backupOK := fm.ping(fm.backup)

			fm.updateState(mainOK, backupOK)
		}
	}
}

// ping 测试 Redis 连接
func (fm *FailoverManager) ping(rc *RedisClient) bool {
	ctx, cancel := context.WithTimeout(fm.ctx, 500*time.Millisecond)
	defer cancel()
	_, err := rc.Client.Ping(ctx).Result()
	return err == nil
}

// updateState 更新主备状态及流量切换
func (fm *FailoverManager) updateState(mainOK, backupOK bool) {
	fm.weights.mu.Lock()
	defer fm.weights.mu.Unlock()

	if fm.currentActive == "main" && !mainOK {
		fm.failCount++
		if fm.failCount >= fm.config.FailureThreshold {
			fm.failCount = 0
			go fm.grayReleaseSwitch("backup")
		}
	} else if fm.currentActive == "backup" && mainOK {
		fm.recoverCount++
		if fm.recoverCount >= fm.config.RecoverThreshold {
			fm.recoverCount = 0
			go fm.grayReleaseSwitch("main")
		}
	} else {
		fm.failCount = 0
		fm.recoverCount = 0
	}
}

// grayReleaseSwitch 实现灰度切换
func (fm *FailoverManager) grayReleaseSwitch(target string) {
	fm.mu.Lock()
	if fm.isSwitching {
		fm.mu.Unlock()
		return
	}
	fm.isSwitching = true
	fm.mu.Unlock()

	defer func() {
		fm.mu.Lock()
		fm.isSwitching = false
		fm.mu.Unlock()
	}()

	steps := fm.config.GraySteps
	stepInterval := fm.config.StepInterval

	for i := 1; i <= steps; i++ {
		select {
		case <-fm.ctx.Done():
			return
		default:
			percentage := float64(i) / float64(steps)
			var newMain, newBackup float64
			if target == "backup" {
				newMain = 1.0 - percentage
				newBackup = percentage
			} else {
				newMain = percentage
				newBackup = 1.0 - percentage
			}

			fm.weights.mu.Lock()
			fm.weights.Main = newMain
			fm.weights.Backup = newBackup
			fm.weights.mu.Unlock()

			log.Printf("Gray release step %d/%d: main=%.2f, backup=%.2f", i, steps, newMain, newBackup)
			time.Sleep(stepInterval)
		}
	}

	fm.weights.mu.Lock()
	if target == "backup" {
		fm.currentActive = "backup"
		fm.weights.Main = 0.0
		fm.weights.Backup = 1.0
	} else {
		fm.currentActive = "main"
		fm.weights.Main = 1.0
		fm.weights.Backup = 0.0
	}
	fm.weights.mu.Unlock()

	log.Printf("Switched to %s", fm.currentActive)
}

// trafficDispatcher 按照流量权重分配请求
func (fm *FailoverManager) trafficDispatcher() {
	for {
		select {
		case <-fm.ctx.Done():
			return
		default:
			fm.weights.mu.RLock()
			mainWeight := fm.weights.Main
			fm.weights.mu.RUnlock()

			r := rand.Float64()
			var targetClient *RedisClient
			if r < mainWeight {
				targetClient = fm.main
			} else {
				targetClient = fm.backup
			}

			fm.targetClient = targetClient
		}
	}
}

func (fm *FailoverManager) RedisClient() *redis.Client {
	return fm.targetClient.Client
}
