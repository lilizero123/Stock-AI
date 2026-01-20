package data

import (
	"compress/gzip"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
	"stock-ai/backend/models"
)

// RequestManager 请求管理器
type RequestManager struct {
	client       *http.Client
	cache        *Cache
	config       *models.Config
	sourceStatus map[string]*SourceStatus
	rateLimiter  *RateLimiter // 新增：限流器
	proxyPool    *ProxyPoolState
	mu           sync.RWMutex
}

// SourceStatus 数据源状态
type SourceStatus struct {
	FailCount    int
	LastFailTime time.Time
	LastSuccess  time.Time
	LastChecked  time.Time
	LastLatency  time.Duration
	Disabled     bool
}

// ProxyPoolState 代理池状态
type ProxyPoolState struct {
	proxies   []string
	expiresAt time.Time
	current   int
	mu        sync.Mutex
	lastFetch time.Time
	lastError string
}

func (ps *ProxyPoolState) reset() {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.proxies = nil
	ps.expiresAt = time.Time{}
	ps.current = 0
	ps.lastFetch = time.Time{}
	ps.lastError = ""
}

// Cache 缓存管理
type Cache struct {
	data map[string]*CacheItem
	mu   sync.RWMutex
}

// CacheItem 缓存项
type CacheItem struct {
	Data      interface{}
	ExpireAt  time.Time
	CacheTime time.Duration
}

// User-Agent池
var userAgents = []string{
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:121.0) Gecko/20100101 Firefox/121.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:120.0) Gecko/20100101 Firefox/120.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0",
	"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/118.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
	"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.2 Safari/605.1.15",
}

// 缓存时间配置
const (
	CacheTimeQuote     = 30 * time.Second // 行情数据缓存30秒
	CacheTimeIndex     = 30 * time.Second // 指数数据缓存30秒
	CacheTimeIndustry  = 2 * time.Minute  // 行业排行缓存2分钟
	CacheTimeMoneyFlow = 2 * time.Minute  // 资金流向缓存2分钟
	CacheTimeNews      = 3 * time.Minute  // 新闻缓存3分钟
	CacheTimeReport    = 30 * time.Minute // 研报缓存30分钟
	CacheTimeNotice    = 30 * time.Minute // 公告缓存30分钟
	CacheTimeTiger     = 10 * time.Minute // 龙虎榜缓存10分钟
	CacheTimeHotTopic  = 5 * time.Minute  // 热门话题缓存5分钟

	// 数据清理周期配置
	CleanupIntervalQuote     = 5 * time.Minute // 实时行情缓存清理周期
	CleanupIntervalFinancial = 4 * time.Hour   // 财务数据缓存清理周期
	CleanupIntervalNews      = 6 * time.Hour   // 新闻缓存清理周期
	CleanupIntervalReport    = 12 * time.Hour  // 研报/公告缓存清理周期
)

var globalRequestManager *RequestManager
var once sync.Once

// GetRequestManager 获取全局请求管理器
func GetRequestManager() *RequestManager {
	once.Do(func() {
		globalRequestManager = NewRequestManager()
	})
	return globalRequestManager
}

// NewRequestManager 创建请求管理器
func NewRequestManager() *RequestManager {
	rm := &RequestManager{
		cache: &Cache{
			data: make(map[string]*CacheItem),
		},
		sourceStatus: make(map[string]*SourceStatus),
		rateLimiter:  GetRateLimiter(), // 初始化限流器
		proxyPool:    &ProxyPoolState{},
	}
	rm.initClient()

	// 启动缓存清理调度器
	go rm.startCacheCleanupScheduler()

	return rm
}

// initClient 初始化HTTP客户端
func (rm *RequestManager) initClient() {
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{},
		MaxIdleConns:    100,
		IdleConnTimeout: 90 * time.Second,
	}

	transport.Proxy = rm.proxySelectorFunc()

	rm.client = &http.Client{
		Timeout:   5 * time.Second, // 适当放宽超时，避免优质接口偶发超时
		Transport: transport,
	}
}

func (rm *RequestManager) proxySelectorFunc() func(*http.Request) (*url.URL, error) {
	return func(req *http.Request) (*url.URL, error) {
		rm.mu.RLock()
		cfg := rm.config
		rm.mu.RUnlock()

		if hasProxyConfigured(cfg) {
			if proxyStr := rm.getProxyFromPool(cfg); proxyStr != "" {
				if proxyURL, err := url.Parse(proxyStr); err == nil {
					return proxyURL, nil
				}
				log.Printf("[ProxyPool] 解析代理失败: %v", proxyStr)
			}
		}

		return nil, nil
	}
}

func (rm *RequestManager) getProxyFromPool(cfg *models.Config) string {
	if cfg == nil {
		return ""
	}
	customPoolConfigured := strings.TrimSpace(cfg.ProxyPoolList) != "" || cfg.ProxyApiUrl != "" || cfg.ProxyApiKey != ""
	if !cfg.ProxyPoolEnabled && !customPoolConfigured {
		return strings.TrimSpace(cfg.ProxyUrl)
	}
	if rm.proxyPool == nil {
		rm.proxyPool = &ProxyPoolState{}
	}
	pp := rm.proxyPool
	pp.mu.Lock()
	defer pp.mu.Unlock()

	now := time.Now()
	if len(pp.proxies) == 0 || (!pp.expiresAt.IsZero() && now.After(pp.expiresAt)) {
		proxies, ttl, err := rm.fetchProxyList(cfg)
		if err != nil {
			log.Printf("[ProxyPool] 获取代理失败: %v", err)
			pp.proxies = nil
			pp.expiresAt = time.Time{}
			pp.current = 0
			pp.lastError = err.Error()
			return ""
		}
		if len(proxies) == 0 {
			log.Printf("[ProxyPool] 代理列表为空")
			pp.lastError = "empty proxy list"
			return ""
		}
		if ttl <= 0 {
			ttl = rm.getProxyPoolTTL(cfg)
		}
		pp.proxies = proxies
		pp.expiresAt = now.Add(ttl)
		pp.current = 0
		pp.lastFetch = now
		pp.lastError = ""
		log.Printf("[ProxyPool] 拉取到 %d 个代理，有效期 %s", len(proxies), ttl)
	}

	if len(pp.proxies) == 0 {
		return ""
	}

	proxy := pp.proxies[pp.current]
	pp.current = (pp.current + 1) % len(pp.proxies)
	return proxy
}

func (rm *RequestManager) fetchProxyList(cfg *models.Config) ([]string, time.Duration, error) {
	scheme := getProxyScheme(cfg)
	provider := strings.ToLower(cfg.ProxyProvider)
	manualList := strings.TrimSpace(cfg.ProxyPoolList)
	if provider == "custom_list" || manualList != "" {
		proxies := parseProxyText(manualList, scheme)
		if len(proxies) == 0 && cfg.ProxyUrl != "" {
			proxies = []string{cfg.ProxyUrl}
		}
		if len(proxies) == 0 {
			return nil, 0, fmt.Errorf("自定义代理列表为空")
		}
		return proxies, rm.getProxyPoolTTL(cfg), nil
	}

	apiURL := rm.buildProxyAPIURL(cfg)
	if apiURL != "" {
		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Get(apiURL)
		if err == nil {
			defer resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				if body, err := io.ReadAll(resp.Body); err == nil {
					if proxies, ttl := rm.parseProxyAPIResponse(body, scheme); len(proxies) > 0 {
						if ttl <= 0 {
							ttl = rm.getProxyPoolTTL(cfg)
						}
						return proxies, ttl, nil
					} else {
						log.Printf("[ProxyPool] 代理API未返回有效结果: %s", string(body))
					}
				}
			} else {
				log.Printf("[ProxyPool] 代理API状态码: %d", resp.StatusCode)
			}
		} else {
			log.Printf("[ProxyPool] 请求代理API失败: %v", err)
		}
	}

	if cfg.ProxyUrl != "" {
		return []string{cfg.ProxyUrl}, rm.getProxyPoolTTL(cfg), nil
	}

	return nil, 0, fmt.Errorf("未配置可用的代理源")
}

func (rm *RequestManager) getProxyPoolTTL(cfg *models.Config) time.Duration {
	ttl := time.Duration(cfg.ProxyPoolTTL) * time.Second
	if ttl <= 0 {
		ttl = 60 * time.Second
	}
	return ttl
}

func (rm *RequestManager) buildProxyAPIURL(cfg *models.Config) string {
	if cfg == nil {
		return ""
	}
	count := cfg.ProxyPoolSize
	if count <= 0 {
		count = 5
	}

	template := cfg.ProxyApiUrl
	switch strings.ToLower(cfg.ProxyProvider) {
	case "kuaidaili":
		if template == "" {
			template = "https://dps.kdlapi.com/api/getdps/?secret_id={apiKey}&signature={apiSecret}&num={num}&pt=1&format=json&sep=1"
		}
	case "qingguo":
		if template == "" {
			template = "https://proxy.qg.net/allocate?Key={apiKey}&Num={num}&Format=json"
		}
	case "custom_api", "":
		// 使用用户输入
	default:
		if template == "" {
			template = cfg.ProxyApiUrl
		}
	}

	return replaceProxyPlaceholders(template, cfg, count)
}

func (rm *RequestManager) parseProxyAPIResponse(body []byte, scheme string) ([]string, time.Duration) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return nil, 0
	}

	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		var data interface{}
		if err := json.Unmarshal(body, &data); err == nil {
			proxies := extractProxyStrings(data, scheme)
			ttl := extractProxyTTL(data)
			return proxies, ttl
		}
	}

	return parseProxyText(trimmed, scheme), 0
}

func replaceProxyPlaceholders(template string, cfg *models.Config, count int) string {
	if template == "" {
		return ""
	}
	replacer := strings.NewReplacer(
		"{apiKey}", url.QueryEscape(cfg.ProxyApiKey),
		"{token}", url.QueryEscape(cfg.ProxyApiKey),
		"{secret}", url.QueryEscape(cfg.ProxyApiSecret),
		"{apiSecret}", url.QueryEscape(cfg.ProxyApiSecret),
		"{signature}", url.QueryEscape(cfg.ProxyApiSecret),
		"{region}", url.QueryEscape(cfg.ProxyRegion),
		"{num}", fmt.Sprintf("%d", count),
	)
	return replacer.Replace(template)
}

func extractProxyStrings(data interface{}, scheme string) []string {
	switch v := data.(type) {
	case []interface{}:
		var result []string
		for _, item := range v {
			switch iv := item.(type) {
			case string:
				if proxy := normalizeProxyString(iv, scheme); proxy != "" {
					result = append(result, proxy)
				}
			case map[string]interface{}:
				if proxy := buildProxyFromMap(iv, scheme); proxy != "" {
					result = append(result, proxy)
					continue
				}
				result = append(result, extractProxyStrings(iv, scheme)...)
			default:
				result = append(result, extractProxyStrings(iv, scheme)...)
			}
		}
		return result
	case map[string]interface{}:
		if proxy := buildProxyFromMap(v, scheme); proxy != "" {
			return []string{proxy}
		}
		keys := []string{"proxy", "proxy_list", "proxyList", "data", "result", "results", "list", "ips"}
		for _, key := range keys {
			if child, ok := v[key]; ok {
				if result := extractProxyStrings(child, scheme); len(result) > 0 {
					return result
				}
			}
		}
	case string:
		return parseProxyText(v, scheme)
	}
	return nil
}

func buildProxyFromMap(data map[string]interface{}, scheme string) string {
	ip, ipOk := data["ip"]
	port, portOk := data["port"]
	if ipOk && portOk {
		return normalizeProxyString(fmt.Sprintf("%v:%v", ip, port), scheme)
	}
	return ""
}

func extractProxyTTL(data interface{}) time.Duration {
	switch v := data.(type) {
	case map[string]interface{}:
		keys := []string{"ttl", "expire", "timeout", "expiration", "expire_time"}
		for _, key := range keys {
			if val, ok := v[key]; ok {
				if ttl := numericToDuration(val); ttl > 0 {
					return ttl
				}
			}
		}
		for _, val := range v {
			if ttl := extractProxyTTL(val); ttl > 0 {
				return ttl
			}
		}
	case []interface{}:
		for _, item := range v {
			if ttl := extractProxyTTL(item); ttl > 0 {
				return ttl
			}
		}
	}
	return 0
}

func numericToDuration(value interface{}) time.Duration {
	switch v := value.(type) {
	case float64:
		if v > 0 {
			return time.Duration(int(v)) * time.Second
		}
	case int:
		if v > 0 {
			return time.Duration(v) * time.Second
		}
	case int64:
		if v > 0 {
			return time.Duration(v) * time.Second
		}
	case string:
		if v == "" {
			return 0
		}
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return time.Duration(n) * time.Second
		}
	}
	return 0
}

func parseProxyText(text string, scheme string) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	separators := func(r rune) bool {
		switch r {
		case '\n', '\r', '\t', ',', ';', '|':
			return true
		}
		return false
	}
	parts := strings.FieldsFunc(trimmed, separators)
	var result []string
	for _, part := range parts {
		if proxy := normalizeProxyString(part, scheme); proxy != "" {
			result = append(result, proxy)
		}
	}
	return result
}

func normalizeProxyString(value string, scheme string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "socks5://") {
		return value
	}
	if scheme == "" {
		scheme = "http"
	}
	if !strings.HasSuffix(scheme, "://") {
		scheme += "://"
	}
	return scheme + value
}

func getProxyScheme(cfg *models.Config) string {
	if cfg == nil || cfg.ProxyPoolProtocol == "" {
		return "http"
	}
	return strings.ToLower(cfg.ProxyPoolProtocol)
}

func hasProxyConfigured(cfg *models.Config) bool {
	if cfg == nil {
		return false
	}
	if cfg.ProxyPoolEnabled {
		return true
	}
	if strings.TrimSpace(cfg.ProxyPoolList) != "" {
		return true
	}
	if cfg.ProxyApiUrl != "" || cfg.ProxyApiKey != "" {
		return true
	}
	if cfg.ProxyUrl != "" {
		return true
	}
	return false
}

// UpdateConfig 更新配置
func (rm *RequestManager) UpdateConfig(config *models.Config) {
	rm.mu.Lock()
	defer rm.mu.Unlock()
	rm.config = config
	if rm.proxyPool != nil {
		rm.proxyPool.reset()
	}
	rm.initClient()
}

// GetRandomUA 获取随机User-Agent
func (rm *RequestManager) GetRandomUA() string {
	return userAgents[rand.Intn(len(userAgents))]
}

// SetRequestHeaders 设置完整的请求头
func (rm *RequestManager) SetRequestHeaders(req *http.Request, referer string) {
	req.Header.Set("User-Agent", rm.GetRandomUA())
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8")
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Cache-Control", "no-cache")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
}

// GetClient 获取HTTP客户端
func (rm *RequestManager) GetClient() *http.Client {
	return rm.client
}

// GetCache 获取缓存数据
func (rm *RequestManager) GetCache(key string) (interface{}, bool) {
	rm.cache.mu.RLock()
	defer rm.cache.mu.RUnlock()

	item, exists := rm.cache.data[key]
	if !exists {
		return nil, false
	}

	if time.Now().After(item.ExpireAt) {
		return nil, false
	}

	return item.Data, true
}

// SetCache 设置缓存数据
func (rm *RequestManager) SetCache(key string, data interface{}, duration time.Duration) {
	rm.cache.mu.Lock()
	defer rm.cache.mu.Unlock()

	rm.cache.data[key] = &CacheItem{
		Data:      data,
		ExpireAt:  time.Now().Add(duration),
		CacheTime: duration,
	}
}

// ClearCache 清除缓存
func (rm *RequestManager) ClearCache(key string) {
	rm.cache.mu.Lock()
	defer rm.cache.mu.Unlock()
	delete(rm.cache.data, key)
}

// ClearAllCache 清除所有缓存
func (rm *RequestManager) ClearAllCache() {
	rm.cache.mu.Lock()
	defer rm.cache.mu.Unlock()
	rm.cache.data = make(map[string]*CacheItem)
}

// MarkSourceFailed 标记数据源失败
func (rm *RequestManager) MarkSourceFailed(sourceName string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	status, exists := rm.sourceStatus[sourceName]
	if !exists {
		status = &SourceStatus{}
		rm.sourceStatus[sourceName] = status
	}

	status.FailCount++
	status.LastFailTime = time.Now()

	// 连续失败3次，禁用5分钟
	if status.FailCount >= 3 {
		status.Disabled = true
	}
}

// MarkSourceSuccess 标记数据源成功
func (rm *RequestManager) MarkSourceSuccess(sourceName string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	if status, exists := rm.sourceStatus[sourceName]; exists {
		status.FailCount = 0
		status.Disabled = false
	}
}

// IsSourceAvailable 检查数据源是否可用
func (rm *RequestManager) IsSourceAvailable(sourceName string) bool {
	rm.mu.RLock()
	status, exists := rm.sourceStatus[sourceName]
	if !exists {
		rm.mu.RUnlock()
		return true
	}
	disabled := status.Disabled
	lastFail := status.LastFailTime
	rm.mu.RUnlock()

	if !disabled {
		return true
	}

	// Reset disabled source once the cool-down has passed
	if time.Since(lastFail) > 5*time.Minute {
		rm.mu.Lock()
		if status, ok := rm.sourceStatus[sourceName]; ok {
			status.Disabled = false
			status.FailCount = 0
		}
		rm.mu.Unlock()
		return true
	}

	return false
}

// GetConfig 获取配置
func (rm *RequestManager) GetConfig() *models.Config {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.config
}

// IsTradingTime 检查是否为交易时间
func IsTradingTime() bool {
	now := time.Now()
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now = now.In(loc)

	// 检查是否为工作日（周一到周五）
	weekday := now.Weekday()
	if weekday == time.Saturday || weekday == time.Sunday {
		return false
	}

	// 检查时间段
	hour := now.Hour()
	minute := now.Minute()
	currentMinutes := hour*60 + minute

	// 上午交易时间：9:30 - 11:30
	morningStart := 9*60 + 30
	morningEnd := 11*60 + 30

	// 下午交易时间：13:00 - 15:00
	afternoonStart := 13 * 60
	afternoonEnd := 15 * 60

	if (currentMinutes >= morningStart && currentMinutes <= morningEnd) ||
		(currentMinutes >= afternoonStart && currentMinutes <= afternoonEnd) {
		return true
	}

	return false
}

// IsPreMarketTime 检查是否为盘前时间（9:00-9:30）
func IsPreMarketTime() bool {
	now := time.Now()
	loc, _ := time.LoadLocation("Asia/Shanghai")
	now = now.In(loc)

	weekday := now.Weekday()
	if weekday == time.Saturday || weekday == time.Sunday {
		return false
	}

	hour := now.Hour()
	minute := now.Minute()
	currentMinutes := hour*60 + minute

	preMarketStart := 9 * 60
	preMarketEnd := 9*60 + 30

	return currentMinutes >= preMarketStart && currentMinutes < preMarketEnd
}

// GetRefreshInterval 根据交易时间获取刷新间隔
func GetRefreshInterval(baseInterval int) int {
	if IsTradingTime() {
		return baseInterval // 交易时间使用配置的间隔
	} else if IsPreMarketTime() {
		return baseInterval * 2 // 盘前时间间隔翻倍
	}
	return 0 // 非交易时间不自动刷新
}

func init() {
	rand.Seed(time.Now().UnixNano())
}

// FetchWebContent 抓取网页内容
func (rm *RequestManager) FetchWebContent(pageURL string) (string, error) {
	if pageURL == "" {
		return "", nil
	}

	// 创建带超时的客户端
	client := &http.Client{
		Timeout: 10 * time.Second, // 10秒超时
	}

	req, err := http.NewRequest("GET", pageURL, nil)
	if err != nil {
		return "", err
	}

	rm.SetRequestHeaders(req, "")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// 读取响应体，处理gzip压缩
	var reader io.Reader = resp.Body
	if resp.Header.Get("Content-Encoding") == "gzip" {
		gzReader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return "", err
		}
		defer gzReader.Close()
		reader = gzReader
	}

	body, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	// 检测并转换编码
	charset := detectCharset(resp.Header.Get("Content-Type"), body)
	if charset == "gbk" || charset == "gb2312" || charset == "gb18030" {
		// 将GBK编码转换为UTF-8
		decoder := simplifiedchinese.GBK.NewDecoder()
		utf8Body, _, err := transform.Bytes(decoder, body)
		if err == nil {
			body = utf8Body
		}
	}

	// 提取正文内容
	content := extractTextContent(string(body))
	return content, nil
}

// detectCharset 检测网页编码
func detectCharset(contentType string, body []byte) string {
	// 1. 从Content-Type头检测
	contentType = strings.ToLower(contentType)
	if strings.Contains(contentType, "charset=") {
		parts := strings.Split(contentType, "charset=")
		if len(parts) > 1 {
			charset := strings.TrimSpace(parts[1])
			charset = strings.Split(charset, ";")[0]
			charset = strings.Trim(charset, "\"'")
			return strings.ToLower(charset)
		}
	}

	// 2. 从HTML meta标签检测
	htmlStr := string(body)
	// 检测 <meta charset="xxx">
	metaCharsetRe := regexp.MustCompile(`(?i)<meta[^>]+charset=["']?([^"'\s>]+)`)
	if matches := metaCharsetRe.FindStringSubmatch(htmlStr); len(matches) > 1 {
		return strings.ToLower(matches[1])
	}

	// 检测 <meta http-equiv="Content-Type" content="text/html; charset=xxx">
	metaContentTypeRe := regexp.MustCompile(`(?i)<meta[^>]+content=["'][^"']*charset=([^"'\s;]+)`)
	if matches := metaContentTypeRe.FindStringSubmatch(htmlStr); len(matches) > 1 {
		return strings.ToLower(matches[1])
	}

	// 3. 尝试检测是否为GBK编码（通过检测常见的GBK字节序列）
	// 如果包含大量高位字节且不是有效的UTF-8，可能是GBK
	if !isValidUTF8(body) && hasGBKPattern(body) {
		return "gbk"
	}

	// 默认返回utf-8
	return "utf-8"
}

// isValidUTF8 检查是否为有效的UTF-8编码
func isValidUTF8(data []byte) bool {
	// 检查是否包含无效的UTF-8序列
	invalidCount := 0
	i := 0
	for i < len(data) {
		if data[i] < 0x80 {
			i++
			continue
		}
		// 多字节UTF-8序列
		var size int
		if data[i]&0xE0 == 0xC0 {
			size = 2
		} else if data[i]&0xF0 == 0xE0 {
			size = 3
		} else if data[i]&0xF8 == 0xF0 {
			size = 4
		} else {
			invalidCount++
			i++
			continue
		}
		// 检查后续字节
		if i+size > len(data) {
			invalidCount++
			i++
			continue
		}
		valid := true
		for j := 1; j < size; j++ {
			if data[i+j]&0xC0 != 0x80 {
				valid = false
				break
			}
		}
		if !valid {
			invalidCount++
			i++
		} else {
			i += size
		}
	}
	// 如果无效序列超过一定比例，认为不是有效UTF-8
	return invalidCount < len(data)/100
}

// hasGBKPattern 检查是否包含GBK编码模式
func hasGBKPattern(data []byte) bool {
	// GBK编码的中文字符范围：第一字节0x81-0xFE，第二字节0x40-0xFE
	gbkCount := 0
	for i := 0; i < len(data)-1; i++ {
		if data[i] >= 0x81 && data[i] <= 0xFE {
			if data[i+1] >= 0x40 && data[i+1] <= 0xFE {
				gbkCount++
				i++ // 跳过第二字节
			}
		}
	}
	// 如果有足够多的GBK模式字符，认为是GBK编码
	return gbkCount > 10
}

// extractTextContent 从HTML中提取文本内容
func extractTextContent(html string) string {
	// 移除script标签
	scriptRe := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	html = scriptRe.ReplaceAllString(html, "")

	// 移除style标签
	styleRe := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	html = styleRe.ReplaceAllString(html, "")

	// 移除HTML注释
	commentRe := regexp.MustCompile(`<!--[\s\S]*?-->`)
	html = commentRe.ReplaceAllString(html, "")

	// 移除所有HTML标签
	tagRe := regexp.MustCompile(`<[^>]+>`)
	text := tagRe.ReplaceAllString(html, " ")

	// 解码HTML实体
	text = decodeHTMLEntities(text)

	// 清理多余空白
	spaceRe := regexp.MustCompile(`\s+`)
	text = spaceRe.ReplaceAllString(text, " ")

	// 去除首尾空白
	text = strings.TrimSpace(text)

	// 限制长度，避免内容过长
	if len(text) > 8000 {
		text = text[:8000] + "..."
	}

	return text
}

// decodeHTMLEntities 解码常见HTML实体
func decodeHTMLEntities(text string) string {
	entities := map[string]string{
		"&nbsp;":   " ",
		"&lt;":     "<",
		"&gt;":     ">",
		"&amp;":    "&",
		"&quot;":   "\"",
		"&apos;":   "'",
		"&#39;":    "'",
		"&ldquo;":  "\"",
		"&rdquo;":  "\"",
		"&lsquo;":  "'",
		"&rsquo;":  "'",
		"&mdash;":  "-",
		"&ndash;":  "-",
		"&hellip;": "...",
	}
	for entity, char := range entities {
		text = strings.ReplaceAll(text, entity, char)
	}
	// 处理数字实体
	numEntityRe := regexp.MustCompile(`&#(\d+);`)
	text = numEntityRe.ReplaceAllStringFunc(text, func(match string) string {
		var num int
		fmt.Sscanf(match, "&#%d;", &num)
		if num > 0 && num < 65536 {
			return string(rune(num))
		}
		return match
	})
	return text
}

// DoRequestWithRateLimit 带限流的HTTP请求
// domain: 用于限流的域名标识（如 "eastmoney.com", "sina.com.cn"）
func (rm *RequestManager) DoRequestWithRateLimit(domain string, req *http.Request) (*http.Response, error) {
	var resp *http.Response
	var err error

	// 首次加载时跳过限流延迟，直接请求
	msm := GetMultiSourceManager()
	if msm.IsFirstLoad() {
		// 首次加载：直接请求，不等待限流
		resp, err = rm.client.Do(req)
		// 仍然记录请求，用于后续限流计算
		rm.rateLimiter.RecordRequest(domain)
		return resp, err
	}

	// 后续请求：使用限流
	err = rm.rateLimiter.ExecuteWithRateLimit(domain, func() error {
		resp, err = rm.client.Do(req)
		return err
	})

	return resp, err
}

// GetWithRateLimit 带限流的GET请求
func (rm *RequestManager) GetWithRateLimit(domain string, url string) (*http.Response, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	rm.SetRequestHeaders(req, "")
	return rm.DoRequestWithRateLimit(domain, req)
}

// GetRateLimiterStats 获取限流器统计信息
func (rm *RequestManager) GetRateLimiterStats(domain string) map[string]interface{} {
	return rm.rateLimiter.GetStats(domain)
}

// extractDomainFromURL 从URL中提取域名
func extractDomainFromURL(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return "default"
	}
	host := u.Host
	// 移除端口号
	if idx := strings.LastIndex(host, ":"); idx != -1 {
		host = host[:idx]
	}
	return host
}

// ==================== 缓存清理功能 ====================

// startCacheCleanupScheduler 启动缓存清理调度器
func (rm *RequestManager) startCacheCleanupScheduler() {
	// 每分钟检查一次过期缓存
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		rm.cleanupExpiredCache()
	}
}

// cleanupExpiredCache 清理过期的缓存
func (rm *RequestManager) cleanupExpiredCache() {
	rm.cache.mu.Lock()
	defer rm.cache.mu.Unlock()

	now := time.Now()
	expiredKeys := make([]string, 0)

	for key, item := range rm.cache.data {
		if now.After(item.ExpireAt) {
			expiredKeys = append(expiredKeys, key)
		}
	}

	for _, key := range expiredKeys {
		delete(rm.cache.data, key)
	}
}

// CleanupCacheByPrefix 按前缀清理缓存
func (rm *RequestManager) CleanupCacheByPrefix(prefix string) int {
	rm.cache.mu.Lock()
	defer rm.cache.mu.Unlock()

	count := 0
	for key := range rm.cache.data {
		if strings.HasPrefix(key, prefix) {
			delete(rm.cache.data, key)
			count++
		}
	}
	return count
}

// CleanupQuoteCache 清理行情缓存（5分钟前的数据）
func (rm *RequestManager) CleanupQuoteCache() int {
	return rm.CleanupCacheByPrefix("quote_")
}

// CleanupNewsCache 清理新闻缓存
func (rm *RequestManager) CleanupNewsCache() int {
	return rm.CleanupCacheByPrefix("news_")
}

// CleanupReportCache 清理研报缓存
func (rm *RequestManager) CleanupReportCache() int {
	return rm.CleanupCacheByPrefix("report_")
}

// CleanupNoticeCache 清理公告缓存
func (rm *RequestManager) CleanupNoticeCache() int {
	return rm.CleanupCacheByPrefix("notice_")
}

// GetProxyStatus 返回代理池状态
func (rm *RequestManager) GetProxyStatus() models.ProxyStatus {
	rm.mu.RLock()
	cfg := rm.config
	rm.mu.RUnlock()

	status := models.ProxyStatus{
		Enabled:     hasProxyConfigured(cfg),
		Provider:    "",
		PoolEnabled: false,
	}

	if cfg != nil {
		status.Provider = cfg.ProxyProvider
		status.PoolEnabled = cfg.ProxyPoolEnabled || strings.TrimSpace(cfg.ProxyPoolList) != "" || cfg.ProxyApiUrl != "" || cfg.ProxyApiKey != ""
	}

	if rm.proxyPool != nil {
		rm.proxyPool.mu.Lock()
		status.ActiveProxies = len(rm.proxyPool.proxies)
		if !rm.proxyPool.expiresAt.IsZero() {
			status.ExpiresAt = rm.proxyPool.expiresAt.Format(time.RFC3339)
			seconds := int(time.Until(rm.proxyPool.expiresAt).Seconds())
			if seconds < 0 {
				seconds = 0
			}
			status.ExpiresInSeconds = seconds
		}
		if !rm.proxyPool.lastFetch.IsZero() {
			status.LastFetch = rm.proxyPool.lastFetch.Format(time.RFC3339)
		}
		status.LastError = rm.proxyPool.lastError
		rm.proxyPool.mu.Unlock()
	}

	return status
}

// GetCacheStats 获取缓存统计信息
func (rm *RequestManager) GetCacheStats() map[string]interface{} {
	rm.cache.mu.RLock()
	defer rm.cache.mu.RUnlock()

	totalCount := len(rm.cache.data)
	expiredCount := 0
	now := time.Now()

	// 按类型统计
	typeCount := make(map[string]int)
	for key, item := range rm.cache.data {
		if now.After(item.ExpireAt) {
			expiredCount++
		}

		// 提取缓存类型
		cacheType := "other"
		if strings.HasPrefix(key, "quote_") {
			cacheType = "quote"
		} else if strings.HasPrefix(key, "news_") {
			cacheType = "news"
		} else if strings.HasPrefix(key, "report_") {
			cacheType = "report"
		} else if strings.HasPrefix(key, "notice_") {
			cacheType = "notice"
		} else if strings.HasPrefix(key, "financial_") {
			cacheType = "financial"
		} else if strings.HasPrefix(key, "index_") {
			cacheType = "index"
		} else if strings.HasPrefix(key, "industry_") {
			cacheType = "industry"
		}
		typeCount[cacheType]++
	}

	return map[string]interface{}{
		"totalCount":   totalCount,
		"expiredCount": expiredCount,
		"typeCount":    typeCount,
	}
}
