package data

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

// FinancialDataProvider 财务数据提供者接口
type FinancialDataProvider interface {
	GetFinancialData(stockCode string) (*FinancialData, error)
	IsAvailable() bool
	Name() string
}

// UnifiedFinancialClient 统一财务数据客户端
// 自动在多个数据源之间切换，优先使用可用的数据源
type UnifiedFinancialClient struct {
	tushare     *TushareClient
	akshare     *AKShareClient
	rateLimiter *RateLimiter
	cache       *DataCache
	mu          sync.RWMutex

	// 数据源优先级配置
	preferTushare bool
	// 失败计数，用于自动切换
	tushareFailCount int
	akshareFailCount int
	// 最大连续失败次数，超过后暂时禁用该数据源
	maxFailCount int
	// 禁用恢复时间
	tushareDisabledUntil time.Time
	akshareDisabledUntil time.Time
}

var (
	globalFinancialClient *UnifiedFinancialClient
	financialClientOnce   sync.Once
)

// GetFinancialClient 获取全局财务数据客户端
func GetFinancialClient() *UnifiedFinancialClient {
	financialClientOnce.Do(func() {
		globalFinancialClient = NewUnifiedFinancialClient()
	})
	return globalFinancialClient
}

// NewUnifiedFinancialClient 创建统一财务数据客户端
func NewUnifiedFinancialClient() *UnifiedFinancialClient {
	return &UnifiedFinancialClient{
		tushare:       GetTushareClient(),
		akshare:       GetAKShareClient(),
		rateLimiter:   GetRateLimiter(),
		preferTushare: true, // 默认优先使用Tushare（更稳定）
		maxFailCount:  3,    // 连续失败3次后暂时禁用
		cache: &DataCache{
			data: make(map[string]*CacheItem),
		},
	}
}

// SetTushareToken 设置Tushare Token
func (c *UnifiedFinancialClient) SetTushareToken(token string) {
	c.tushare.SetToken(token)
}

// SetPreferTushare 设置是否优先使用Tushare
func (c *UnifiedFinancialClient) SetPreferTushare(prefer bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.preferTushare = prefer
}

// isTushareAvailable 检查Tushare是否可用
func (c *UnifiedFinancialClient) isTushareAvailable() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// 检查是否配置了Token
	if !c.tushare.IsConfigured() {
		return false
	}

	// 检查是否被暂时禁用
	if time.Now().Before(c.tushareDisabledUntil) {
		return false
	}

	return true
}

// isAKShareAvailable 检查AKShare是否可用
func (c *UnifiedFinancialClient) isAKShareAvailable() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// 检查是否被暂时禁用
	if time.Now().Before(c.akshareDisabledUntil) {
		return false
	}

	return true
}

// recordTushareSuccess 记录Tushare成功
func (c *UnifiedFinancialClient) recordTushareSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tushareFailCount = 0
}

// recordTushareFailure 记录Tushare失败
func (c *UnifiedFinancialClient) recordTushareFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.tushareFailCount++
	if c.tushareFailCount >= c.maxFailCount {
		// 暂时禁用5分钟
		c.tushareDisabledUntil = time.Now().Add(5 * time.Minute)
		c.tushareFailCount = 0
		log.Printf("[Financial] Tushare连续失败%d次，暂时禁用5分钟", c.maxFailCount)
	}
}

// recordAKShareSuccess 记录AKShare成功
func (c *UnifiedFinancialClient) recordAKShareSuccess() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.akshareFailCount = 0
}

// recordAKShareFailure 记录AKShare失败
func (c *UnifiedFinancialClient) recordAKShareFailure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.akshareFailCount++
	if c.akshareFailCount >= c.maxFailCount {
		// 暂时禁用5分钟
		c.akshareDisabledUntil = time.Now().Add(5 * time.Minute)
		c.akshareFailCount = 0
		log.Printf("[Financial] AKShare连续失败%d次，暂时禁用5分钟", c.maxFailCount)
	}
}

// GetFinancialData 获取财务数据（自动选择数据源）
func (c *UnifiedFinancialClient) GetFinancialData(stockCode string) (*FinancialData, error) {
	// 检查缓存
	cacheKey := fmt.Sprintf("unified_financial_%s", stockCode)
	if cached, ok := c.getCache(cacheKey); ok {
		log.Printf("[Financial] 使用缓存的财务数据: %s", stockCode)
		return cached.(*FinancialData), nil
	}

	var data *FinancialData
	var err error
	var source string

	// 根据优先级尝试获取数据
	if c.preferTushare {
		// 优先Tushare
		if c.isTushareAvailable() {
			data, err = c.tushare.GetFinancialData(stockCode)
			if err == nil {
				c.recordTushareSuccess()
				source = "Tushare"
			} else {
				log.Printf("[Financial] Tushare获取失败: %v，尝试AKShare", err)
				c.recordTushareFailure()
			}
		}

		// Tushare失败或不可用，尝试AKShare
		if data == nil && c.isAKShareAvailable() {
			data, err = c.akshare.GetFinancialData(stockCode)
			if err == nil {
				c.recordAKShareSuccess()
				source = "AKShare"
			} else {
				log.Printf("[Financial] AKShare获取失败: %v", err)
				c.recordAKShareFailure()
			}
		}
	} else {
		// 优先AKShare
		if c.isAKShareAvailable() {
			data, err = c.akshare.GetFinancialData(stockCode)
			if err == nil {
				c.recordAKShareSuccess()
				source = "AKShare"
			} else {
				log.Printf("[Financial] AKShare获取失败: %v，尝试Tushare", err)
				c.recordAKShareFailure()
			}
		}

		// AKShare失败或不可用，尝试Tushare
		if data == nil && c.isTushareAvailable() {
			data, err = c.tushare.GetFinancialData(stockCode)
			if err == nil {
				c.recordTushareSuccess()
				source = "Tushare"
			} else {
				log.Printf("[Financial] Tushare获取失败: %v", err)
				c.recordTushareFailure()
			}
		}
	}

	if data == nil {
		return nil, fmt.Errorf("所有数据源均不可用")
	}

	log.Printf("[Financial] 成功从 %s 获取财务数据: %s", source, stockCode)

	// 缓存数据（1小时）
	c.setCache(cacheKey, data, time.Hour)

	return data, nil
}

// getCache 获取缓存
func (c *UnifiedFinancialClient) getCache(key string) (interface{}, bool) {
	c.cache.mu.RLock()
	defer c.cache.mu.RUnlock()

	item, ok := c.cache.data[key]
	if !ok {
		return nil, false
	}

	if time.Now().After(item.ExpireAt) {
		return nil, false
	}

	return item.Data, true
}

// setCache 设置缓存
func (c *UnifiedFinancialClient) setCache(key string, data interface{}, ttl time.Duration) {
	c.cache.mu.Lock()
	defer c.cache.mu.Unlock()

	c.cache.data[key] = &CacheItem{
		Data:     data,
		ExpireAt: time.Now().Add(ttl),
	}
}

// GetDataSourceStatus 获取数据源状态
func (c *UnifiedFinancialClient) GetDataSourceStatus() map[string]interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()

	now := time.Now()

	return map[string]interface{}{
		"tushare": map[string]interface{}{
			"configured":    c.tushare.IsConfigured(),
			"available":     c.isTushareAvailable(),
			"failCount":     c.tushareFailCount,
			"disabledUntil": c.tushareDisabledUntil.Format(time.RFC3339),
			"isDisabled":    now.Before(c.tushareDisabledUntil),
		},
		"akshare": map[string]interface{}{
			"running":       c.akshare.IsRunning(),
			"available":     c.isAKShareAvailable(),
			"failCount":     c.akshareFailCount,
			"disabledUntil": c.akshareDisabledUntil.Format(time.RFC3339),
			"isDisabled":    now.Before(c.akshareDisabledUntil),
		},
		"preferTushare": c.preferTushare,
	}
}

// FormatFinancialDataForAI 格式化财务数据供AI分析使用
func FormatFinancialDataForAI(data *FinancialData) string {
	if data == nil {
		return ""
	}

	var sb strings.Builder

	sb.WriteString("\n## 财务数据\n\n")

	// 盈利能力
	sb.WriteString("### 盈利能力\n")
	if data.ROE != 0 {
		sb.WriteString(fmt.Sprintf("- 净资产收益率(ROE): %.2f%%\n", data.ROE))
	}
	if data.ROA != 0 {
		sb.WriteString(fmt.Sprintf("- 总资产收益率(ROA): %.2f%%\n", data.ROA))
	}
	if data.GrossMargin != 0 {
		sb.WriteString(fmt.Sprintf("- 毛利率: %.2f%%\n", data.GrossMargin))
	}
	if data.NetMargin != 0 {
		sb.WriteString(fmt.Sprintf("- 净利率: %.2f%%\n", data.NetMargin))
	}
	if data.EPS != 0 {
		sb.WriteString(fmt.Sprintf("- 每股收益(EPS): %.2f元\n", data.EPS))
	}

	// 估值指标
	sb.WriteString("\n### 估值指标\n")
	if data.PE != 0 {
		sb.WriteString(fmt.Sprintf("- 市盈率(PE): %.2f\n", data.PE))
	}
	if data.PB != 0 {
		sb.WriteString(fmt.Sprintf("- 市净率(PB): %.2f\n", data.PB))
	}
	if data.BPS != 0 {
		sb.WriteString(fmt.Sprintf("- 每股净资产(BPS): %.2f元\n", data.BPS))
	}

	// 偿债能力
	sb.WriteString("\n### 偿债能力\n")
	if data.DebtRatio != 0 {
		sb.WriteString(fmt.Sprintf("- 资产负债率: %.2f%%\n", data.DebtRatio))
	}
	if data.CurrentRatio != 0 {
		sb.WriteString(fmt.Sprintf("- 流动比率: %.2f\n", data.CurrentRatio))
	}
	if data.QuickRatio != 0 {
		sb.WriteString(fmt.Sprintf("- 速动比率: %.2f\n", data.QuickRatio))
	}

	// 资产负债
	if data.TotalAssets != 0 || data.TotalLiab != 0 || data.TotalEquity != 0 {
		sb.WriteString("\n### 资产负债\n")
		if data.TotalAssets != 0 {
			sb.WriteString(fmt.Sprintf("- 总资产: %.2f亿元\n", data.TotalAssets))
		}
		if data.TotalLiab != 0 {
			sb.WriteString(fmt.Sprintf("- 总负债: %.2f亿元\n", data.TotalLiab))
		}
		if data.TotalEquity != 0 {
			sb.WriteString(fmt.Sprintf("- 股东权益: %.2f亿元\n", data.TotalEquity))
		}
	}

	// 现金流
	if data.OperatingCF != 0 || data.InvestingCF != 0 || data.FinancingCF != 0 {
		sb.WriteString("\n### 现金流\n")
		if data.OperatingCF != 0 {
			sb.WriteString(fmt.Sprintf("- 经营现金流: %.2f亿元\n", data.OperatingCF))
		}
		if data.InvestingCF != 0 {
			sb.WriteString(fmt.Sprintf("- 投资现金流: %.2f亿元\n", data.InvestingCF))
		}
		if data.FinancingCF != 0 {
			sb.WriteString(fmt.Sprintf("- 筹资现金流: %.2f亿元\n", data.FinancingCF))
		}
	}

	// 成长能力
	if data.RevenueGrowth != 0 || data.ProfitGrowth != 0 {
		sb.WriteString("\n### 成长能力\n")
		if data.RevenueGrowth != 0 {
			sb.WriteString(fmt.Sprintf("- 营收同比增长: %.2f%%\n", data.RevenueGrowth))
		}
		if data.ProfitGrowth != 0 {
			sb.WriteString(fmt.Sprintf("- 净利润同比增长: %.2f%%\n", data.ProfitGrowth))
		}
	}

	if data.ReportDate != "" {
		sb.WriteString(fmt.Sprintf("\n*数据报告期: %s*\n", data.ReportDate))
	}

	return sb.String()
}

// StartAKShareServer 启动AKShare服务
func (c *UnifiedFinancialClient) StartAKShareServer() error {
	return c.akshare.StartServer()
}

// StopAKShareServer 停止AKShare服务
func (c *UnifiedFinancialClient) StopAKShareServer() {
	c.akshare.StopServer()
}
