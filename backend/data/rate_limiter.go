package data

import (
	"crypto/rand"
	"log"
	"math/big"
	"sync"
	"time"
)

// RateLimiter 请求限流器 - 防止IP被封禁的核心组件
type RateLimiter struct {
	mu sync.Mutex

	// 每个域名的请求记录
	domainRequests map[string][]time.Time

	// 每个域名的配置
	domainConfigs map[string]*DomainConfig

	// 全局请求队列
	requestQueue chan *QueuedRequest

	// 是否正在处理队列
	processing bool
}

// DomainConfig 域名配置
type DomainConfig struct {
	// 每分钟最大请求数
	MaxRequestsPerMinute int
	// 每小时最大请求数
	MaxRequestsPerHour int
	// 最小请求间隔（毫秒）
	MinIntervalMs int
	// 最大请求间隔（毫秒）- 用于随机延迟
	MaxIntervalMs int
	// 是否启用随机延迟
	RandomDelay bool
	// 连续请求后的冷却时间（秒）
	CooldownAfterBurst int
	// 触发冷却的连续请求数
	BurstThreshold int
	// 上次请求时间
	LastRequestTime time.Time
	// 连续请求计数
	ConsecutiveRequests int
	// 是否在冷却中
	InCooldown bool
	// 冷却结束时间
	CooldownEndTime time.Time
}

// QueuedRequest 队列中的请求
type QueuedRequest struct {
	Domain   string
	Callback func() error
	Result   chan error
}

// 默认域名配置
var defaultDomainConfigs = map[string]*DomainConfig{
	// 东方财富 - 最严格的限制
	"eastmoney.com": {
		MaxRequestsPerMinute: 10,  // 每分钟最多10次
		MaxRequestsPerHour:   100, // 每小时最多100次
		MinIntervalMs:        3000, // 最小间隔3秒
		MaxIntervalMs:        8000, // 最大间隔8秒
		RandomDelay:          true,
		CooldownAfterBurst:   60,  // 连续请求后冷却60秒
		BurstThreshold:       5,   // 连续5次请求后触发冷却
	},
	// 新浪财经 - 实时行情数据源
	"sina.com.cn": {
		MaxRequestsPerMinute: 20,
		MaxRequestsPerHour:   200,
		MinIntervalMs:        1500,
		MaxIntervalMs:        4000,
		RandomDelay:          true,
		CooldownAfterBurst:   30,
		BurstThreshold:       10,
	},
	// 新浪行情接口 - 专门用于实时行情
	"hq.sinajs.cn": {
		MaxRequestsPerMinute: 30,  // 行情接口限制相对宽松
		MaxRequestsPerHour:   300,
		MinIntervalMs:        1000,
		MaxIntervalMs:        3000,
		RandomDelay:          true,
		CooldownAfterBurst:   20,
		BurstThreshold:       15,
	},
	// 腾讯财经 - 实时行情数据源
	"qt.gtimg.cn": {
		MaxRequestsPerMinute: 30,  // 腾讯行情接口限制宽松
		MaxRequestsPerHour:   300,
		MinIntervalMs:        1000,
		MaxIntervalMs:        3000,
		RandomDelay:          true,
		CooldownAfterBurst:   20,
		BurstThreshold:       15,
	},
	// 腾讯财经分时数据
	"web.ifzq.gtimg.cn": {
		MaxRequestsPerMinute: 20,
		MaxRequestsPerHour:   200,
		MinIntervalMs:        1500,
		MaxIntervalMs:        4000,
		RandomDelay:          true,
		CooldownAfterBurst:   30,
		BurstThreshold:       10,
	},
	// 网易财经 - 实时行情数据源
	"api.money.126.net": {
		MaxRequestsPerMinute: 25,  // 网易接口限制适中
		MaxRequestsPerHour:   250,
		MinIntervalMs:        1200,
		MaxIntervalMs:        3500,
		RandomDelay:          true,
		CooldownAfterBurst:   25,
		BurstThreshold:       12,
	},
	// 东方财富行情推送接口 - 限制较严格
	"push2.eastmoney.com": {
		MaxRequestsPerMinute: 15,  // 东方财富限制较严
		MaxRequestsPerHour:   150,
		MinIntervalMs:        2000,
		MaxIntervalMs:        5000,
		RandomDelay:          true,
		CooldownAfterBurst:   45,
		BurstThreshold:       8,
	},
	// 搜狐财经 - 老牌接口，限制宽松
	"hq.stock.sohu.com": {
		MaxRequestsPerMinute: 25,
		MaxRequestsPerHour:   250,
		MinIntervalMs:        1500,
		MaxIntervalMs:        4000,
		RandomDelay:          true,
		CooldownAfterBurst:   30,
		BurstThreshold:       12,
	},
	// 雪球 - 需要cookie，限制适中
	"stock.xueqiu.com": {
		MaxRequestsPerMinute: 20,
		MaxRequestsPerHour:   200,
		MinIntervalMs:        2000,
		MaxIntervalMs:        5000,
		RandomDelay:          true,
		CooldownAfterBurst:   40,
		BurstThreshold:       10,
	},
	// 百度股市通 - 限制相对宽松
	"finance.pae.baidu.com": {
		MaxRequestsPerMinute: 25,
		MaxRequestsPerHour:   250,
		MinIntervalMs:        1500,
		MaxIntervalMs:        4000,
		RandomDelay:          true,
		CooldownAfterBurst:   30,
		BurstThreshold:       12,
	},
	// 和讯财经 - 老牌接口，限制宽松
	"webstock.quote.hermes.hexun.com": {
		MaxRequestsPerMinute: 25,
		MaxRequestsPerHour:   250,
		MinIntervalMs:        1500,
		MaxIntervalMs:        4000,
		RandomDelay:          true,
		CooldownAfterBurst:   30,
		BurstThreshold:       12,
	},
	// Tushare - 有官方限制，但相对宽松
	"tushare.pro": {
		MaxRequestsPerMinute: 60,  // 官方限制较宽松
		MaxRequestsPerHour:   500,
		MinIntervalMs:        500,
		MaxIntervalMs:        1500,
		RandomDelay:          true,
		CooldownAfterBurst:   10,
		BurstThreshold:       20,
	},
	// AKShare - 本地Python服务，限制可以宽松
	"akshare.local": {
		MaxRequestsPerMinute: 30,
		MaxRequestsPerHour:   300,
		MinIntervalMs:        1000,
		MaxIntervalMs:        2000,
		RandomDelay:          true,
		CooldownAfterBurst:   15,
		BurstThreshold:       10,
	},
	// 默认配置 - 用于未知域名
	"default": {
		MaxRequestsPerMinute: 8,
		MaxRequestsPerHour:   80,
		MinIntervalMs:        5000,
		MaxIntervalMs:        10000,
		RandomDelay:          true,
		CooldownAfterBurst:   120,
		BurstThreshold:       3,
	},
}

var (
	globalRateLimiter *RateLimiter
	rateLimiterOnce   sync.Once
)

// GetRateLimiter 获取全局限流器实例
func GetRateLimiter() *RateLimiter {
	rateLimiterOnce.Do(func() {
		globalRateLimiter = NewRateLimiter()
	})
	return globalRateLimiter
}

// NewRateLimiter 创建新的限流器
func NewRateLimiter() *RateLimiter {
	rl := &RateLimiter{
		domainRequests: make(map[string][]time.Time),
		domainConfigs:  make(map[string]*DomainConfig),
		requestQueue:   make(chan *QueuedRequest, 100),
	}

	// 复制默认配置
	for domain, config := range defaultDomainConfigs {
		rl.domainConfigs[domain] = &DomainConfig{
			MaxRequestsPerMinute: config.MaxRequestsPerMinute,
			MaxRequestsPerHour:   config.MaxRequestsPerHour,
			MinIntervalMs:        config.MinIntervalMs,
			MaxIntervalMs:        config.MaxIntervalMs,
			RandomDelay:          config.RandomDelay,
			CooldownAfterBurst:   config.CooldownAfterBurst,
			BurstThreshold:       config.BurstThreshold,
		}
	}

	// 启动队列处理器
	go rl.processQueue()

	return rl
}

// getConfig 获取域名配置
func (rl *RateLimiter) getConfig(domain string) *DomainConfig {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// 尝试匹配域名
	for key, config := range rl.domainConfigs {
		if key != "default" && containsDomain(domain, key) {
			return config
		}
	}

	// 返回默认配置
	return rl.domainConfigs["default"]
}

// containsDomain 检查域名是否包含指定的关键字
func containsDomain(domain, key string) bool {
	return len(domain) >= len(key) && (domain == key ||
		domain[len(domain)-len(key):] == key ||
		(len(domain) > len(key) && domain[len(domain)-len(key)-1:len(domain)-len(key)] == "." && domain[len(domain)-len(key):] == key))
}

// CanRequest 检查是否可以发起请求
func (rl *RateLimiter) CanRequest(domain string) (bool, time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	config := rl.getConfigUnsafe(domain)
	now := time.Now()

	// 检查是否在冷却中
	if config.InCooldown && now.Before(config.CooldownEndTime) {
		waitTime := config.CooldownEndTime.Sub(now)
		log.Printf("[RateLimiter] 域名 %s 在冷却中，还需等待 %.1f 秒", domain, waitTime.Seconds())
		return false, waitTime
	}

	// 冷却结束，重置状态
	if config.InCooldown && now.After(config.CooldownEndTime) {
		config.InCooldown = false
		config.ConsecutiveRequests = 0
	}

	// 清理过期的请求记录
	rl.cleanOldRequests(domain)

	requests := rl.domainRequests[domain]

	// 检查每分钟限制
	minuteAgo := now.Add(-time.Minute)
	minuteCount := 0
	for _, t := range requests {
		if t.After(minuteAgo) {
			minuteCount++
		}
	}
	if minuteCount >= config.MaxRequestsPerMinute {
		waitTime := time.Minute - now.Sub(requests[len(requests)-config.MaxRequestsPerMinute])
		log.Printf("[RateLimiter] 域名 %s 达到每分钟限制 (%d/%d)，需等待 %.1f 秒",
			domain, minuteCount, config.MaxRequestsPerMinute, waitTime.Seconds())
		return false, waitTime
	}

	// 检查每小时限制
	hourAgo := now.Add(-time.Hour)
	hourCount := 0
	for _, t := range requests {
		if t.After(hourAgo) {
			hourCount++
		}
	}
	if hourCount >= config.MaxRequestsPerHour {
		waitTime := time.Hour - now.Sub(requests[len(requests)-config.MaxRequestsPerHour])
		log.Printf("[RateLimiter] 域名 %s 达到每小时限制 (%d/%d)，需等待 %.1f 秒",
			domain, hourCount, config.MaxRequestsPerHour, waitTime.Seconds())
		return false, waitTime
	}

	// 检查最小间隔
	if !config.LastRequestTime.IsZero() {
		elapsed := now.Sub(config.LastRequestTime)
		minInterval := time.Duration(config.MinIntervalMs) * time.Millisecond
		if elapsed < minInterval {
			waitTime := minInterval - elapsed
			return false, waitTime
		}
	}

	return true, 0
}

// getConfigUnsafe 获取配置（不加锁，内部使用）
func (rl *RateLimiter) getConfigUnsafe(domain string) *DomainConfig {
	for key, config := range rl.domainConfigs {
		if key != "default" && containsDomain(domain, key) {
			return config
		}
	}
	return rl.domainConfigs["default"]
}

// cleanOldRequests 清理过期的请求记录
func (rl *RateLimiter) cleanOldRequests(domain string) {
	hourAgo := time.Now().Add(-time.Hour)
	requests := rl.domainRequests[domain]

	// 找到第一个在一小时内的请求
	startIdx := 0
	for i, t := range requests {
		if t.After(hourAgo) {
			startIdx = i
			break
		}
		startIdx = i + 1
	}

	if startIdx > 0 && startIdx <= len(requests) {
		rl.domainRequests[domain] = requests[startIdx:]
	}
}

// RecordRequest 记录一次请求
func (rl *RateLimiter) RecordRequest(domain string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	config := rl.getConfigUnsafe(domain)

	// 记录请求时间
	rl.domainRequests[domain] = append(rl.domainRequests[domain], now)
	config.LastRequestTime = now
	config.ConsecutiveRequests++

	// 检查是否需要触发冷却
	if config.ConsecutiveRequests >= config.BurstThreshold {
		config.InCooldown = true
		config.CooldownEndTime = now.Add(time.Duration(config.CooldownAfterBurst) * time.Second)
		config.ConsecutiveRequests = 0
		log.Printf("[RateLimiter] 域名 %s 触发冷却，将在 %d 秒后恢复", domain, config.CooldownAfterBurst)
	}
}

// GetRandomDelay 获取随机延迟时间
func (rl *RateLimiter) GetRandomDelay(domain string) time.Duration {
	config := rl.getConfig(domain)

	if !config.RandomDelay {
		return time.Duration(config.MinIntervalMs) * time.Millisecond
	}

	// 生成随机延迟
	rangeMs := config.MaxIntervalMs - config.MinIntervalMs
	if rangeMs <= 0 {
		return time.Duration(config.MinIntervalMs) * time.Millisecond
	}

	randomMs, err := rand.Int(rand.Reader, big.NewInt(int64(rangeMs)))
	if err != nil {
		return time.Duration(config.MinIntervalMs) * time.Millisecond
	}

	delay := config.MinIntervalMs + int(randomMs.Int64())
	return time.Duration(delay) * time.Millisecond
}

// WaitForSlot 等待可用的请求槽位
func (rl *RateLimiter) WaitForSlot(domain string) {
	for {
		canRequest, waitTime := rl.CanRequest(domain)
		if canRequest {
			// 添加随机延迟
			delay := rl.GetRandomDelay(domain)
			log.Printf("[RateLimiter] 域名 %s 等待随机延迟 %.1f 秒", domain, delay.Seconds())
			time.Sleep(delay)
			return
		}

		// 等待指定时间后重试
		log.Printf("[RateLimiter] 域名 %s 需要等待 %.1f 秒", domain, waitTime.Seconds())
		time.Sleep(waitTime)
	}
}

// ExecuteWithRateLimit 带限流执行请求
func (rl *RateLimiter) ExecuteWithRateLimit(domain string, fn func() error) error {
	// 等待可用槽位
	rl.WaitForSlot(domain)

	// 记录请求
	rl.RecordRequest(domain)

	// 执行请求
	return fn()
}

// QueueRequest 将请求加入队列（异步执行）
func (rl *RateLimiter) QueueRequest(domain string, fn func() error) <-chan error {
	result := make(chan error, 1)

	rl.requestQueue <- &QueuedRequest{
		Domain:   domain,
		Callback: fn,
		Result:   result,
	}

	return result
}

// processQueue 处理请求队列
func (rl *RateLimiter) processQueue() {
	for req := range rl.requestQueue {
		err := rl.ExecuteWithRateLimit(req.Domain, req.Callback)
		req.Result <- err
		close(req.Result)
	}
}

// ResetCooldown 重置指定域名的冷却状态（仅用于测试）
func (rl *RateLimiter) ResetCooldown(domain string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	config := rl.getConfigUnsafe(domain)
	config.InCooldown = false
	config.ConsecutiveRequests = 0
	config.CooldownEndTime = time.Time{}
}

// GetStats 获取限流统计信息
func (rl *RateLimiter) GetStats(domain string) map[string]interface{} {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	config := rl.getConfigUnsafe(domain)
	requests := rl.domainRequests[domain]

	now := time.Now()
	minuteAgo := now.Add(-time.Minute)
	hourAgo := now.Add(-time.Hour)

	minuteCount := 0
	hourCount := 0
	for _, t := range requests {
		if t.After(minuteAgo) {
			minuteCount++
		}
		if t.After(hourAgo) {
			hourCount++
		}
	}

	return map[string]interface{}{
		"domain":               domain,
		"requestsLastMinute":   minuteCount,
		"requestsLastHour":     hourCount,
		"maxPerMinute":         config.MaxRequestsPerMinute,
		"maxPerHour":           config.MaxRequestsPerHour,
		"inCooldown":           config.InCooldown,
		"cooldownRemaining":    config.CooldownEndTime.Sub(now).Seconds(),
		"consecutiveRequests":  config.ConsecutiveRequests,
	}
}

// UpdateConfig 更新域名配置
func (rl *RateLimiter) UpdateConfig(domain string, config *DomainConfig) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.domainConfigs[domain] = config
}
