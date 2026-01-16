package data

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

// TushareClient Tushare数据客户端
type TushareClient struct {
	token      string
	baseURL    string
	client     *http.Client
	rateLimiter *RateLimiter
	cache      *DataCache
	mu         sync.RWMutex
}

// TushareRequest Tushare API请求
type TushareRequest struct {
	APIName string                 `json:"api_name"`
	Token   string                 `json:"token"`
	Params  map[string]interface{} `json:"params"`
	Fields  string                 `json:"fields,omitempty"`
}

// TushareResponse Tushare API响应
type TushareResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Fields []string        `json:"fields"`
		Items  [][]interface{} `json:"items"`
	} `json:"data"`
}

// FinancialData 财务数据
type FinancialData struct {
	Code            string  `json:"code"`            // 股票代码
	Name            string  `json:"name"`            // 股票名称
	ReportDate      string  `json:"reportDate"`      // 报告期
	Revenue         float64 `json:"revenue"`         // 营业收入（亿元）
	NetProfit       float64 `json:"netProfit"`       // 净利润（亿元）
	GrossMargin     float64 `json:"grossMargin"`     // 毛利率（%）
	NetMargin       float64 `json:"netMargin"`       // 净利率（%）
	ROE             float64 `json:"roe"`             // 净资产收益率（%）
	ROA             float64 `json:"roa"`             // 总资产收益率（%）
	DebtRatio       float64 `json:"debtRatio"`       // 资产负债率（%）
	CurrentRatio    float64 `json:"currentRatio"`    // 流动比率
	QuickRatio      float64 `json:"quickRatio"`      // 速动比率
	EPS             float64 `json:"eps"`             // 每股收益
	BPS             float64 `json:"bps"`             // 每股净资产
	PE              float64 `json:"pe"`              // 市盈率
	PB              float64 `json:"pb"`              // 市净率
	TotalAssets     float64 `json:"totalAssets"`     // 总资产（亿元）
	TotalLiab       float64 `json:"totalLiab"`       // 总负债（亿元）
	TotalEquity     float64 `json:"totalEquity"`     // 股东权益（亿元）
	OperatingCF     float64 `json:"operatingCF"`     // 经营现金流（亿元）
	InvestingCF     float64 `json:"investingCF"`     // 投资现金流（亿元）
	FinancingCF     float64 `json:"financingCF"`     // 筹资现金流（亿元）
	RevenueGrowth   float64 `json:"revenueGrowth"`   // 营收同比增长（%）
	ProfitGrowth    float64 `json:"profitGrowth"`    // 净利润同比增长（%）
}

// DataCache 数据缓存（用于Tushare和AKShare）
type DataCache struct {
	mu   sync.RWMutex
	data map[string]*CacheItem
}

var (
	globalTushareClient *TushareClient
	tushareClientOnce   sync.Once
)

// GetTushareClient 获取全局Tushare客户端
func GetTushareClient() *TushareClient {
	tushareClientOnce.Do(func() {
		globalTushareClient = NewTushareClient("")
	})
	return globalTushareClient
}

// NewTushareClient 创建Tushare客户端
func NewTushareClient(token string) *TushareClient {
	return &TushareClient{
		token:   token,
		baseURL: "http://api.tushare.pro",
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		rateLimiter: GetRateLimiter(),
		cache: &DataCache{
			data: make(map[string]*CacheItem),
		},
	}
}

// SetToken 设置Token
func (c *TushareClient) SetToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = token
}

// GetToken 获取Token
func (c *TushareClient) GetToken() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.token
}

// IsConfigured 检查是否已配置
func (c *TushareClient) IsConfigured() bool {
	return c.GetToken() != ""
}

// request 发送API请求（带限流保护）
func (c *TushareClient) request(apiName string, params map[string]interface{}, fields string) (*TushareResponse, error) {
	if !c.IsConfigured() {
		return nil, fmt.Errorf("Tushare Token未配置")
	}

	// 使用限流器保护
	var resp *TushareResponse
	var err error

	err = c.rateLimiter.ExecuteWithRateLimit("tushare.pro", func() error {
		resp, err = c.doRequest(apiName, params, fields)
		return err
	})

	return resp, err
}

// doRequest 实际执行请求
func (c *TushareClient) doRequest(apiName string, params map[string]interface{}, fields string) (*TushareResponse, error) {
	reqBody := TushareRequest{
		APIName: apiName,
		Token:   c.GetToken(),
		Params:  params,
		Fields:  fields,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %v", err)
	}

	log.Printf("[Tushare] 请求API: %s, 参数: %v", apiName, params)

	req, err := http.NewRequest("POST", c.baseURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	var tushareResp TushareResponse
	if err := json.Unmarshal(body, &tushareResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	if tushareResp.Code != 0 {
		return nil, fmt.Errorf("API错误: %s (code: %d)", tushareResp.Msg, tushareResp.Code)
	}

	log.Printf("[Tushare] API %s 返回 %d 条数据", apiName, len(tushareResp.Data.Items))
	return &tushareResp, nil
}

// getCache 获取缓存
func (c *TushareClient) getCache(key string) (interface{}, bool) {
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
func (c *TushareClient) setCache(key string, data interface{}, ttl time.Duration) {
	c.cache.mu.Lock()
	defer c.cache.mu.Unlock()

	c.cache.data[key] = &CacheItem{
		Data:     data,
		ExpireAt: time.Now().Add(ttl),
	}
}

// ClearCache 清理所有缓存
func (c *TushareClient) ClearCache() int {
	c.cache.mu.Lock()
	defer c.cache.mu.Unlock()

	count := len(c.cache.data)
	c.cache.data = make(map[string]*CacheItem)
	return count
}

// ClearExpiredCache 清理过期缓存
func (c *TushareClient) ClearExpiredCache() int {
	c.cache.mu.Lock()
	defer c.cache.mu.Unlock()

	now := time.Now()
	count := 0
	for key, item := range c.cache.data {
		if now.After(item.ExpireAt) {
			delete(c.cache.data, key)
			count++
		}
	}
	return count
}

// GetCacheStats 获取缓存统计
func (c *TushareClient) GetCacheStats() map[string]interface{} {
	c.cache.mu.RLock()
	defer c.cache.mu.RUnlock()

	return map[string]interface{}{
		"totalCount": len(c.cache.data),
	}
}

// convertTsCode 转换股票代码格式（sz000001 -> 000001.SZ）
func convertTsCode(code string) string {
	if len(code) < 2 {
		return code
	}

	prefix := code[:2]
	number := code[2:]

	switch prefix {
	case "sh":
		return number + ".SH"
	case "sz":
		return number + ".SZ"
	case "bj":
		return number + ".BJ"
	default:
		// 如果已经是 000001.SZ 格式，直接返回
		if len(code) > 3 && code[len(code)-3] == '.' {
			return code
		}
		return code + ".SZ" // 默认深圳
	}
}

// convertFromTsCode 从Tushare代码格式转换回来（000001.SZ -> sz000001）
func convertFromTsCode(tsCode string) string {
	if len(tsCode) < 4 {
		return tsCode
	}

	// 找到点的位置
	dotIdx := -1
	for i, c := range tsCode {
		if c == '.' {
			dotIdx = i
			break
		}
	}

	if dotIdx == -1 {
		return tsCode
	}

	number := tsCode[:dotIdx]
	suffix := tsCode[dotIdx+1:]

	switch suffix {
	case "SH":
		return "sh" + number
	case "SZ":
		return "sz" + number
	case "BJ":
		return "bj" + number
	default:
		return tsCode
	}
}

// GetFinancialData 获取财务数据
func (c *TushareClient) GetFinancialData(stockCode string) (*FinancialData, error) {
	tsCode := convertTsCode(stockCode)
	cacheKey := fmt.Sprintf("financial_%s", tsCode)

	// 检查缓存（财务数据缓存1小时）
	if cached, ok := c.getCache(cacheKey); ok {
		log.Printf("[Tushare] 使用缓存的财务数据: %s", stockCode)
		return cached.(*FinancialData), nil
	}

	// 获取最新财务指标
	indicators, err := c.getFinancialIndicators(tsCode)
	if err != nil {
		log.Printf("[Tushare] 获取财务指标失败: %v", err)
	}

	// 获取资产负债表
	balance, err := c.getBalanceSheet(tsCode)
	if err != nil {
		log.Printf("[Tushare] 获取资产负债表失败: %v", err)
	}

	// 获取现金流量表
	cashflow, err := c.getCashFlow(tsCode)
	if err != nil {
		log.Printf("[Tushare] 获取现金流量表失败: %v", err)
	}

	// 获取每日指标（PE、PB等）
	daily, err := c.getDailyBasic(tsCode)
	if err != nil {
		log.Printf("[Tushare] 获取每日指标失败: %v", err)
	}

	// 合并数据
	data := &FinancialData{
		Code: stockCode,
	}

	if indicators != nil {
		data.ReportDate = indicators.ReportDate
		data.ROE = indicators.ROE
		data.ROA = indicators.ROA
		data.GrossMargin = indicators.GrossMargin
		data.NetMargin = indicators.NetMargin
		data.EPS = indicators.EPS
		data.BPS = indicators.BPS
		data.CurrentRatio = indicators.CurrentRatio
		data.QuickRatio = indicators.QuickRatio
		data.DebtRatio = indicators.DebtRatio
		data.RevenueGrowth = indicators.RevenueGrowth
		data.ProfitGrowth = indicators.ProfitGrowth
	}

	if balance != nil {
		data.TotalAssets = balance.TotalAssets
		data.TotalLiab = balance.TotalLiab
		data.TotalEquity = balance.TotalEquity
	}

	if cashflow != nil {
		data.OperatingCF = cashflow.OperatingCF
		data.InvestingCF = cashflow.InvestingCF
		data.FinancingCF = cashflow.FinancingCF
	}

	if daily != nil {
		data.PE = daily.PE
		data.PB = daily.PB
	}

	// 缓存数据
	c.setCache(cacheKey, data, time.Hour)

	return data, nil
}

// FinancialIndicators 财务指标
type FinancialIndicators struct {
	ReportDate    string
	ROE           float64
	ROA           float64
	GrossMargin   float64
	NetMargin     float64
	EPS           float64
	BPS           float64
	CurrentRatio  float64
	QuickRatio    float64
	DebtRatio     float64
	RevenueGrowth float64
	ProfitGrowth  float64
}

// getFinancialIndicators 获取财务指标
func (c *TushareClient) getFinancialIndicators(tsCode string) (*FinancialIndicators, error) {
	params := map[string]interface{}{
		"ts_code": tsCode,
		"limit":   1,
	}

	fields := "ts_code,ann_date,end_date,roe,roa,grossprofit_margin,netprofit_margin,eps,bps,current_ratio,quick_ratio,debt_to_assets,or_yoy,netprofit_yoy"

	resp, err := c.request("fina_indicator", params, fields)
	if err != nil {
		return nil, err
	}

	if len(resp.Data.Items) == 0 {
		return nil, fmt.Errorf("无财务指标数据")
	}

	item := resp.Data.Items[0]
	fieldMap := makeFieldMap(resp.Data.Fields)

	return &FinancialIndicators{
		ReportDate:    getStringValue(item, fieldMap, "end_date"),
		ROE:           getFloatValue(item, fieldMap, "roe"),
		ROA:           getFloatValue(item, fieldMap, "roa"),
		GrossMargin:   getFloatValue(item, fieldMap, "grossprofit_margin"),
		NetMargin:     getFloatValue(item, fieldMap, "netprofit_margin"),
		EPS:           getFloatValue(item, fieldMap, "eps"),
		BPS:           getFloatValue(item, fieldMap, "bps"),
		CurrentRatio:  getFloatValue(item, fieldMap, "current_ratio"),
		QuickRatio:    getFloatValue(item, fieldMap, "quick_ratio"),
		DebtRatio:     getFloatValue(item, fieldMap, "debt_to_assets"),
		RevenueGrowth: getFloatValue(item, fieldMap, "or_yoy"),
		ProfitGrowth:  getFloatValue(item, fieldMap, "netprofit_yoy"),
	}, nil
}

// BalanceSheet 资产负债表
type BalanceSheet struct {
	TotalAssets float64
	TotalLiab   float64
	TotalEquity float64
}

// getBalanceSheet 获取资产负债表
func (c *TushareClient) getBalanceSheet(tsCode string) (*BalanceSheet, error) {
	params := map[string]interface{}{
		"ts_code": tsCode,
		"limit":   1,
	}

	fields := "ts_code,end_date,total_assets,total_liab,total_hldr_eqy_exc_min_int"

	resp, err := c.request("balancesheet", params, fields)
	if err != nil {
		return nil, err
	}

	if len(resp.Data.Items) == 0 {
		return nil, fmt.Errorf("无资产负债表数据")
	}

	item := resp.Data.Items[0]
	fieldMap := makeFieldMap(resp.Data.Fields)

	return &BalanceSheet{
		TotalAssets: getFloatValue(item, fieldMap, "total_assets") / 100000000, // 转换为亿元
		TotalLiab:   getFloatValue(item, fieldMap, "total_liab") / 100000000,
		TotalEquity: getFloatValue(item, fieldMap, "total_hldr_eqy_exc_min_int") / 100000000,
	}, nil
}

// CashFlow 现金流量
type CashFlow struct {
	OperatingCF float64
	InvestingCF float64
	FinancingCF float64
}

// getCashFlow 获取现金流量表
func (c *TushareClient) getCashFlow(tsCode string) (*CashFlow, error) {
	params := map[string]interface{}{
		"ts_code": tsCode,
		"limit":   1,
	}

	fields := "ts_code,end_date,n_cashflow_act,n_cashflow_inv_act,n_cash_flows_fnc_act"

	resp, err := c.request("cashflow", params, fields)
	if err != nil {
		return nil, err
	}

	if len(resp.Data.Items) == 0 {
		return nil, fmt.Errorf("无现金流量表数据")
	}

	item := resp.Data.Items[0]
	fieldMap := makeFieldMap(resp.Data.Fields)

	return &CashFlow{
		OperatingCF: getFloatValue(item, fieldMap, "n_cashflow_act") / 100000000,
		InvestingCF: getFloatValue(item, fieldMap, "n_cashflow_inv_act") / 100000000,
		FinancingCF: getFloatValue(item, fieldMap, "n_cash_flows_fnc_act") / 100000000,
	}, nil
}

// DailyBasic 每日指标
type DailyBasic struct {
	PE float64
	PB float64
}

// getDailyBasic 获取每日指标
func (c *TushareClient) getDailyBasic(tsCode string) (*DailyBasic, error) {
	// 获取最近交易日
	today := time.Now().Format("20060102")

	params := map[string]interface{}{
		"ts_code":    tsCode,
		"trade_date": today,
	}

	fields := "ts_code,trade_date,pe,pb"

	resp, err := c.request("daily_basic", params, fields)
	if err != nil {
		return nil, err
	}

	// 如果今天没数据，尝试获取最近的
	if len(resp.Data.Items) == 0 {
		params = map[string]interface{}{
			"ts_code": tsCode,
			"limit":   1,
		}
		resp, err = c.request("daily_basic", params, fields)
		if err != nil {
			return nil, err
		}
	}

	if len(resp.Data.Items) == 0 {
		return nil, fmt.Errorf("无每日指标数据")
	}

	item := resp.Data.Items[0]
	fieldMap := makeFieldMap(resp.Data.Fields)

	return &DailyBasic{
		PE: getFloatValue(item, fieldMap, "pe"),
		PB: getFloatValue(item, fieldMap, "pb"),
	}, nil
}

// GetIncomeStatement 获取利润表数据
func (c *TushareClient) GetIncomeStatement(stockCode string) ([]map[string]interface{}, error) {
	tsCode := convertTsCode(stockCode)
	cacheKey := fmt.Sprintf("income_%s", tsCode)

	if cached, ok := c.getCache(cacheKey); ok {
		return cached.([]map[string]interface{}), nil
	}

	params := map[string]interface{}{
		"ts_code": tsCode,
		"limit":   4, // 最近4个季度
	}

	fields := "ts_code,end_date,revenue,operate_profit,total_profit,n_income,basic_eps"

	resp, err := c.request("income", params, fields)
	if err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, 0)
	fieldMap := makeFieldMap(resp.Data.Fields)

	for _, item := range resp.Data.Items {
		data := map[string]interface{}{
			"reportDate":    getStringValue(item, fieldMap, "end_date"),
			"revenue":       getFloatValue(item, fieldMap, "revenue") / 100000000,
			"operateProfit": getFloatValue(item, fieldMap, "operate_profit") / 100000000,
			"totalProfit":   getFloatValue(item, fieldMap, "total_profit") / 100000000,
			"netIncome":     getFloatValue(item, fieldMap, "n_income") / 100000000,
			"eps":           getFloatValue(item, fieldMap, "basic_eps"),
		}
		result = append(result, data)
	}

	c.setCache(cacheKey, result, time.Hour)
	return result, nil
}

// 辅助函数

func makeFieldMap(fields []string) map[string]int {
	m := make(map[string]int)
	for i, f := range fields {
		m[f] = i
	}
	return m
}

func getStringValue(item []interface{}, fieldMap map[string]int, field string) string {
	idx, ok := fieldMap[field]
	if !ok || idx >= len(item) {
		return ""
	}
	if item[idx] == nil {
		return ""
	}
	if s, ok := item[idx].(string); ok {
		return s
	}
	return fmt.Sprintf("%v", item[idx])
}

func getFloatValue(item []interface{}, fieldMap map[string]int, field string) float64 {
	idx, ok := fieldMap[field]
	if !ok || idx >= len(item) {
		return 0
	}
	if item[idx] == nil {
		return 0
	}
	switch v := item[idx].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return 0
	}
}
