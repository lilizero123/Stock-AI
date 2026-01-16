package data

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"stock-ai/backend/models"
)

// PersistentCache 持久化缓存管理器
// 用于在应用重启后快速加载上次的数据
type PersistentCache struct {
	cacheDir string
	mu       sync.RWMutex
}

// CachedData 缓存的数据结构
type CachedData struct {
	// 市场指数
	MarketIndex []models.MarketIndex `json:"market_index"`
	// 行业排行
	IndustryRank []models.IndustryRank `json:"industry_rank"`
	// 资金流向
	MoneyFlow []models.MoneyFlow `json:"money_flow"`
	// 新闻列表
	NewsList []models.NewsItem `json:"news_list"`
	// 龙虎榜
	LongTigerRank []models.LongTigerItem `json:"long_tiger_rank"`
	// 热门话题
	HotTopics []models.HotTopic `json:"hot_topics"`
	// A股情绪
	AShareSentiment *MarketSentiment `json:"ashare_sentiment"`
	// 全球指数
	GlobalIndices map[string]*IndexData `json:"global_indices"`
	// 全球指数列表（用于前端）
	GlobalIndicesList []models.GlobalIndex `json:"global_indices_list"`
	// 各国新闻缓存
	GlobalNews map[string][]models.NewsItem `json:"global_news"`
	// 各国市场情绪缓存
	GlobalSentiment map[string]*MarketSentiment `json:"global_sentiment"`
	// 缓存时间
	CacheTime time.Time `json:"cache_time"`
}

var (
	globalPersistentCache *PersistentCache
	persistentCacheOnce   sync.Once
)

// GetPersistentCache 获取持久化缓存单例
func GetPersistentCache() *PersistentCache {
	persistentCacheOnce.Do(func() {
		// 获取用户数据目录
		homeDir, err := os.UserHomeDir()
		if err != nil {
			homeDir = "."
		}
		cacheDir := filepath.Join(homeDir, ".stock-ai", "cache")

		// 确保目录存在
		os.MkdirAll(cacheDir, 0755)

		globalPersistentCache = &PersistentCache{
			cacheDir: cacheDir,
		}
	})
	return globalPersistentCache
}

// getCacheFilePath 获取缓存文件路径
func (pc *PersistentCache) getCacheFilePath() string {
	return filepath.Join(pc.cacheDir, "market_data.json")
}

// SaveCache 保存缓存到文件
func (pc *PersistentCache) SaveCache(data *CachedData) error {
	pc.mu.Lock()
	defer pc.mu.Unlock()

	data.CacheTime = time.Now()

	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		log.Printf("[PersistentCache] 序列化缓存数据失败: %v", err)
		return err
	}

	filePath := pc.getCacheFilePath()
	err = os.WriteFile(filePath, jsonData, 0644)
	if err != nil {
		log.Printf("[PersistentCache] 写入缓存文件失败: %v", err)
		return err
	}

	log.Printf("[PersistentCache] 缓存已保存到 %s", filePath)
	return nil
}

// LoadCache 从文件加载缓存
func (pc *PersistentCache) LoadCache() (*CachedData, error) {
	pc.mu.RLock()
	defer pc.mu.RUnlock()

	filePath := pc.getCacheFilePath()

	// 检查文件是否存在
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		log.Printf("[PersistentCache] 缓存文件不存在: %s", filePath)
		return nil, err
	}

	jsonData, err := os.ReadFile(filePath)
	if err != nil {
		log.Printf("[PersistentCache] 读取缓存文件失败: %v", err)
		return nil, err
	}

	var data CachedData
	err = json.Unmarshal(jsonData, &data)
	if err != nil {
		log.Printf("[PersistentCache] 解析缓存数据失败: %v", err)
		return nil, err
	}

	// 检查缓存是否过期（超过24小时认为过期）
	if time.Since(data.CacheTime) > 24*time.Hour {
		log.Printf("[PersistentCache] 缓存已过期 (缓存时间: %v)", data.CacheTime)
		return nil, nil
	}

	log.Printf("[PersistentCache] 成功加载缓存 (缓存时间: %v)", data.CacheTime)
	return &data, nil
}

// HasValidCache 检查是否有有效的缓存
func (pc *PersistentCache) HasValidCache() bool {
	data, err := pc.LoadCache()
	return err == nil && data != nil
}

// GetCacheAge 获取缓存年龄
func (pc *PersistentCache) GetCacheAge() time.Duration {
	data, err := pc.LoadCache()
	if err != nil || data == nil {
		return 0
	}
	return time.Since(data.CacheTime)
}

// SaveMarketIndex 保存市场指数
func (pc *PersistentCache) SaveMarketIndex(data []models.MarketIndex) {
	cached, _ := pc.LoadCache()
	if cached == nil {
		cached = &CachedData{}
	}
	cached.MarketIndex = data
	pc.SaveCache(cached)
}

// SaveIndustryRank 保存行业排行
func (pc *PersistentCache) SaveIndustryRank(data []models.IndustryRank) {
	cached, _ := pc.LoadCache()
	if cached == nil {
		cached = &CachedData{}
	}
	cached.IndustryRank = data
	pc.SaveCache(cached)
}

// SaveMoneyFlow 保存资金流向
func (pc *PersistentCache) SaveMoneyFlow(data []models.MoneyFlow) {
	cached, _ := pc.LoadCache()
	if cached == nil {
		cached = &CachedData{}
	}
	cached.MoneyFlow = data
	pc.SaveCache(cached)
}

// SaveNewsList 保存新闻列表
func (pc *PersistentCache) SaveNewsList(data []models.NewsItem) {
	cached, _ := pc.LoadCache()
	if cached == nil {
		cached = &CachedData{}
	}
	cached.NewsList = data
	pc.SaveCache(cached)
}

// SaveLongTigerRank 保存龙虎榜
func (pc *PersistentCache) SaveLongTigerRank(data []models.LongTigerItem) {
	cached, _ := pc.LoadCache()
	if cached == nil {
		cached = &CachedData{}
	}
	cached.LongTigerRank = data
	pc.SaveCache(cached)
}

// SaveHotTopics 保存热门话题
func (pc *PersistentCache) SaveHotTopics(data []models.HotTopic) {
	cached, _ := pc.LoadCache()
	if cached == nil {
		cached = &CachedData{}
	}
	cached.HotTopics = data
	pc.SaveCache(cached)
}

// SaveAShareSentiment 保存A股情绪
func (pc *PersistentCache) SaveAShareSentiment(data *MarketSentiment) {
	cached, _ := pc.LoadCache()
	if cached == nil {
		cached = &CachedData{}
	}
	cached.AShareSentiment = data
	pc.SaveCache(cached)
}

// SaveGlobalIndices 保存全球指数
func (pc *PersistentCache) SaveGlobalIndices(data map[string]*IndexData) {
	cached, _ := pc.LoadCache()
	if cached == nil {
		cached = &CachedData{}
	}
	cached.GlobalIndices = data
	pc.SaveCache(cached)
}

// SaveGlobalIndicesList 保存全球指数列表
func (pc *PersistentCache) SaveGlobalIndicesList(data []models.GlobalIndex) {
	cached, _ := pc.LoadCache()
	if cached == nil {
		cached = &CachedData{}
	}
	cached.GlobalIndicesList = data
	pc.SaveCache(cached)
}

// SaveGlobalNews 保存某国新闻
func (pc *PersistentCache) SaveGlobalNews(country string, data []models.NewsItem) {
	cached, _ := pc.LoadCache()
	if cached == nil {
		cached = &CachedData{}
	}
	if cached.GlobalNews == nil {
		cached.GlobalNews = make(map[string][]models.NewsItem)
	}
	cached.GlobalNews[country] = data
	pc.SaveCache(cached)
}

// SaveGlobalSentiment 保存某国市场情绪
func (pc *PersistentCache) SaveGlobalSentiment(country string, data *MarketSentiment) {
	cached, _ := pc.LoadCache()
	if cached == nil {
		cached = &CachedData{}
	}
	if cached.GlobalSentiment == nil {
		cached.GlobalSentiment = make(map[string]*MarketSentiment)
	}
	cached.GlobalSentiment[country] = data
	pc.SaveCache(cached)
}

// SaveAllData 一次性保存所有数据
func (pc *PersistentCache) SaveAllData(data *CachedData) error {
	return pc.SaveCache(data)
}
