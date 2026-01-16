package data

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"stock-ai/backend/models"
)

// SentimentAPI 市场情绪API
type SentimentAPI struct {
	rm *RequestManager
}

// NewSentimentAPI 创建市场情绪API实例
func NewSentimentAPI() *SentimentAPI {
	return &SentimentAPI{
		rm: GetRequestManager(),
	}
}

// MarketSentiment 市场情绪数据
type MarketSentiment struct {
	Value       float64            `json:"value"`       // 情绪值 0-100
	Level       string             `json:"level"`       // 情绪等级: extreme_fear, fear, neutral, greed, extreme_greed
	LevelCN     string             `json:"levelCn"`     // 中文等级
	Description string             `json:"description"` // 描述
	UpdateTime  string             `json:"updateTime"`  // 更新时间
	Components  []SentimentComponent `json:"components"` // 各指标分项
}

// SentimentComponent 情绪指标分项
type SentimentComponent struct {
	Name    string  `json:"name"`    // 指标名称
	NameCN  string  `json:"nameCn"`  // 中文名称
	Value   float64 `json:"value"`   // 指标值 0-100
	Weight  float64 `json:"weight"`  // 权重
	Data    string  `json:"data"`    // 原始数据描述
}

// GetAShareSentiment 获取A股市场情绪
// 综合以下指标计算：
// 1. 涨跌家数比 (30%) - 上涨股票数/下跌股票数
// 2. 涨停跌停比 (20%) - 涨停数/跌停数
// 3. 成交量变化 (15%) - 相对于5日均量
// 4. 北向资金流向 (15%) - 当日净流入
// 5. 主力资金流向 (10%) - 主力净流入
// 6. 指数位置 (10%) - 相对于20日均线
func (api *SentimentAPI) GetAShareSentiment() (*MarketSentiment, error) {
	cacheKey := "sentiment_ashare"
	if cached, ok := api.rm.GetCache(cacheKey); ok {
		return cached.(*MarketSentiment), nil
	}

	// 使用 sync.WaitGroup 并行获取所有数据
	var wg sync.WaitGroup
	var mu sync.Mutex

	// 结果变量
	var advDecline *AdvanceDeclineData
	var limitData *LimitCountData
	var northFlow float64
	var mainFlow float64
	var indexChange float64

	// 错误标记
	var advDeclineErr, limitErr, northErr, mainErr, indexErr error

	// 1. 并行获取涨跌家数
	wg.Add(1)
	go func() {
		defer wg.Done()
		advDecline, advDeclineErr = api.getAShareAdvanceDecline()
	}()

	// 2. 并行获取涨停跌停数
	wg.Add(1)
	go func() {
		defer wg.Done()
		limitData, limitErr = api.getAShareLimitCount()
	}()

	// 3. 并行获取北向资金
	wg.Add(1)
	go func() {
		defer wg.Done()
		northFlow, northErr = api.getNorthboundFlow()
	}()

	// 4. 并行获取主力资金
	wg.Add(1)
	go func() {
		defer wg.Done()
		mainFlow, mainErr = api.getMainMoneyFlow()
	}()

	// 5. 并行获取指数涨跌幅
	wg.Add(1)
	go func() {
		defer wg.Done()
		indexChange, indexErr = api.getMainIndexChange()
	}()

	// 等待所有请求完成
	wg.Wait()

	// 汇总结果
	components := make([]SentimentComponent, 0)
	totalWeight := 0.0
	weightedSum := 0.0

	mu.Lock()
	defer mu.Unlock()

	// 1. 处理涨跌家数
	if advDeclineErr == nil && advDecline != nil {
		value := api.calculateAdvanceDeclineScore(advDecline.AdvanceCount, advDecline.DeclineCount)
		weight := 0.30
		components = append(components, SentimentComponent{
			Name:   "advance_decline",
			NameCN: "涨跌家数",
			Value:  value,
			Weight: weight,
			Data:   fmt.Sprintf("上涨%d家 下跌%d家", advDecline.AdvanceCount, advDecline.DeclineCount),
		})
		weightedSum += value * weight
		totalWeight += weight
	}

	// 2. 处理涨停跌停数
	if limitErr == nil && limitData != nil {
		value := api.calculateLimitScore(limitData.LimitUpCount, limitData.LimitDownCount)
		weight := 0.20
		components = append(components, SentimentComponent{
			Name:   "limit_ratio",
			NameCN: "涨跌停比",
			Value:  value,
			Weight: weight,
			Data:   fmt.Sprintf("涨停%d家 跌停%d家", limitData.LimitUpCount, limitData.LimitDownCount),
		})
		weightedSum += value * weight
		totalWeight += weight
	}

	// 3. 处理北向资金
	if northErr == nil {
		value := api.calculateNorthFlowScore(northFlow)
		weight := 0.15
		components = append(components, SentimentComponent{
			Name:   "northbound_flow",
			NameCN: "北向资金",
			Value:  value,
			Weight: weight,
			Data:   fmt.Sprintf("净流入%.2f亿", northFlow/100000000),
		})
		weightedSum += value * weight
		totalWeight += weight
	}

	// 4. 处理主力资金
	if mainErr == nil {
		value := api.calculateMainFlowScore(mainFlow)
		weight := 0.15
		components = append(components, SentimentComponent{
			Name:   "main_flow",
			NameCN: "主力资金",
			Value:  value,
			Weight: weight,
			Data:   fmt.Sprintf("净流入%.2f亿", mainFlow/100000000),
		})
		weightedSum += value * weight
		totalWeight += weight
	}

	// 5. 处理指数涨跌幅
	if indexErr == nil {
		value := api.calculateIndexChangeScore(indexChange)
		weight := 0.20
		components = append(components, SentimentComponent{
			Name:   "index_change",
			NameCN: "指数表现",
			Value:  value,
			Weight: weight,
			Data:   fmt.Sprintf("上证%.2f%%", indexChange),
		})
		weightedSum += value * weight
		totalWeight += weight
	}

	// 计算最终情绪值
	var finalValue float64
	if totalWeight > 0 {
		finalValue = weightedSum / totalWeight
	} else {
		finalValue = 50 // 默认中性
	}

	sentiment := &MarketSentiment{
		Value:       finalValue,
		Level:       api.getSentimentLevel(finalValue),
		LevelCN:     api.getSentimentLevelCN(finalValue),
		Description: api.getSentimentDescription(finalValue),
		UpdateTime:  time.Now().Format("15:04:05"),
		Components:  components,
	}

	// 缓存60秒
	api.rm.SetCache(cacheKey, sentiment, 60*time.Second)

	return sentiment, nil
}

// AdvanceDeclineData 涨跌家数数据
type AdvanceDeclineData struct {
	AdvanceCount int // 上涨家数
	DeclineCount int // 下跌家数
	FlatCount    int // 平盘家数
}

// LimitCountData 涨跌停数据
type LimitCountData struct {
	LimitUpCount   int // 涨停家数
	LimitDownCount int // 跌停家数
}

// getAShareAdvanceDecline 获取A股涨跌家数（并行请求优化）
func (api *SentimentAPI) getAShareAdvanceDecline() (*AdvanceDeclineData, error) {
	var wg sync.WaitGroup
	var totalStocks, advanceCount, declineCount int
	var totalErr, advanceErr, declineErr error

	// 并行获取总数
	wg.Add(1)
	go func() {
		defer wg.Done()
		url := "https://push2.eastmoney.com/api/qt/clist/get?pn=1&pz=1&po=1&np=1&fltt=2&invt=2&fid=f3&fs=m:0+t:6,m:0+t:80,m:1+t:2,m:1+t:23&fields=f3"
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			totalErr = err
			return
		}
		req.Header.Set("Referer", "https://quote.eastmoney.com/")
		req.Header.Set("User-Agent", api.rm.GetRandomUA())

		resp, err := api.rm.DoRequestWithRateLimit("eastmoney.com", req)
		if err != nil {
			totalErr = err
			return
		}
		defer resp.Body.Close()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			totalErr = err
			return
		}

		var result struct {
			Data struct {
				Total int `json:"total"`
			} `json:"data"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			totalErr = err
			return
		}
		totalStocks = result.Data.Total
		if totalStocks == 0 {
			totalStocks = 5000
		}
	}()

	// 并行获取上涨家数
	wg.Add(1)
	go func() {
		defer wg.Done()
		advanceURL := "https://push2.eastmoney.com/api/qt/clist/get?pn=1&pz=1&po=1&np=1&fltt=2&invt=2&fid=f3&fs=m:0+t:6,m:0+t:80,m:1+t:2,m:1+t:23&fields=f3&fid0=f3&fv0=0"
		req, err := http.NewRequest("GET", advanceURL, nil)
		if err != nil {
			advanceErr = err
			return
		}
		req.Header.Set("Referer", "https://quote.eastmoney.com/")
		req.Header.Set("User-Agent", api.rm.GetRandomUA())

		resp, err := api.rm.DoRequestWithRateLimit("eastmoney.com", req)
		if err != nil {
			advanceErr = err
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		var result struct {
			Data struct {
				Total int `json:"total"`
			} `json:"data"`
		}
		json.Unmarshal(body, &result)
		advanceCount = result.Data.Total
	}()

	// 并行获取下跌家数
	wg.Add(1)
	go func() {
		defer wg.Done()
		declineURL := "https://push2.eastmoney.com/api/qt/clist/get?pn=1&pz=1&po=1&np=1&fltt=2&invt=2&fid=f3&fs=m:0+t:6,m:0+t:80,m:1+t:2,m:1+t:23&fields=f3&fid0=f3&fv0=-0.01&fid1=f3&fv1=-100"
		req, err := http.NewRequest("GET", declineURL, nil)
		if err != nil {
			declineErr = err
			return
		}
		req.Header.Set("Referer", "https://quote.eastmoney.com/")
		req.Header.Set("User-Agent", api.rm.GetRandomUA())

		resp, err := api.rm.DoRequestWithRateLimit("eastmoney.com", req)
		if err != nil {
			declineErr = err
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		var result struct {
			Data struct {
				Total int `json:"total"`
			} `json:"data"`
		}
		json.Unmarshal(body, &result)
		declineCount = result.Data.Total
	}()

	wg.Wait()

	// 检查错误
	if totalErr != nil && advanceErr != nil && declineErr != nil {
		return nil, totalErr
	}

	return &AdvanceDeclineData{
		AdvanceCount: advanceCount,
		DeclineCount: declineCount,
		FlatCount:    totalStocks - advanceCount - declineCount,
	}, nil
}

// getAShareLimitCount 获取涨跌停家数（并行请求优化）
func (api *SentimentAPI) getAShareLimitCount() (*LimitCountData, error) {
	var wg sync.WaitGroup
	var limitUpCount, limitDownCount int
	var limitUpErr, limitDownErr error

	// 并行获取涨停家数
	wg.Add(1)
	go func() {
		defer wg.Done()
		limitUpURL := "https://push2.eastmoney.com/api/qt/clist/get?pn=1&pz=1&po=1&np=1&fltt=2&invt=2&fid=f3&fs=m:0+t:6,m:0+t:80,m:1+t:2,m:1+t:23&fields=f3&fid0=f3&fv0=9.9"
		req, err := http.NewRequest("GET", limitUpURL, nil)
		if err != nil {
			limitUpErr = err
			return
		}
		req.Header.Set("Referer", "https://quote.eastmoney.com/")
		req.Header.Set("User-Agent", api.rm.GetRandomUA())

		resp, err := api.rm.DoRequestWithRateLimit("eastmoney.com", req)
		if err != nil {
			limitUpErr = err
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		var result struct {
			Data struct {
				Total int `json:"total"`
			} `json:"data"`
		}
		json.Unmarshal(body, &result)
		limitUpCount = result.Data.Total
	}()

	// 并行获取跌停家数
	wg.Add(1)
	go func() {
		defer wg.Done()
		limitDownURL := "https://push2.eastmoney.com/api/qt/clist/get?pn=1&pz=1&po=1&np=1&fltt=2&invt=2&fid=f3&fs=m:0+t:6,m:0+t:80,m:1+t:2,m:1+t:23&fields=f3&fid0=f3&fv0=-100&fid1=f3&fv1=-9.9"
		req, err := http.NewRequest("GET", limitDownURL, nil)
		if err != nil {
			limitDownErr = err
			return
		}
		req.Header.Set("Referer", "https://quote.eastmoney.com/")
		req.Header.Set("User-Agent", api.rm.GetRandomUA())

		resp, err := api.rm.DoRequestWithRateLimit("eastmoney.com", req)
		if err != nil {
			limitDownErr = err
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		var result struct {
			Data struct {
				Total int `json:"total"`
			} `json:"data"`
		}
		json.Unmarshal(body, &result)
		limitDownCount = result.Data.Total
	}()

	wg.Wait()

	// 检查错误
	if limitUpErr != nil && limitDownErr != nil {
		return nil, limitUpErr
	}

	return &LimitCountData{
		LimitUpCount:   limitUpCount,
		LimitDownCount: limitDownCount,
	}, nil
}

// getNorthboundFlow 获取北向资金净流入
func (api *SentimentAPI) getNorthboundFlow() (float64, error) {
	url := "https://push2.eastmoney.com/api/qt/kamt.rtmin/get?fields1=f1,f2,f3&fields2=f51,f52,f53,f54,f55,f56"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Referer", "https://data.eastmoney.com/")
	req.Header.Set("User-Agent", api.rm.GetRandomUA())

	resp, err := api.rm.DoRequestWithRateLimit("eastmoney.com", req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var result struct {
		Data struct {
			S2N struct {
				F52 float64 `json:"f52"` // 北向资金净流入
			} `json:"s2n"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	return result.Data.S2N.F52, nil
}

// getMainMoneyFlow 获取主力资金净流入
func (api *SentimentAPI) getMainMoneyFlow() (float64, error) {
	url := "https://push2.eastmoney.com/api/qt/clist/get?pn=1&pz=1&po=1&np=1&fltt=2&invt=2&fid=f62&fs=m:0+t:6,m:0+t:80,m:1+t:2,m:1+t:23&fields=f62"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Referer", "https://data.eastmoney.com/")
	req.Header.Set("User-Agent", api.rm.GetRandomUA())

	resp, err := api.rm.DoRequestWithRateLimit("eastmoney.com", req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	// 解析主力净流入总额
	var result struct {
		Data struct {
			Diff []struct {
				F62 float64 `json:"f62"`
			} `json:"diff"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	// 计算总主力净流入
	var totalFlow float64
	for _, item := range result.Data.Diff {
		totalFlow += item.F62
	}

	return totalFlow, nil
}

// getMainIndexChange 获取主要指数涨跌幅
func (api *SentimentAPI) getMainIndexChange() (float64, error) {
	url := "https://push2.eastmoney.com/api/qt/stock/get?secid=1.000001&fields=f3"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Referer", "https://quote.eastmoney.com/")
	req.Header.Set("User-Agent", api.rm.GetRandomUA())

	resp, err := api.rm.DoRequestWithRateLimit("eastmoney.com", req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	var result struct {
		Data struct {
			F3 float64 `json:"f3"` // 涨跌幅*100
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return 0, err
	}

	return result.Data.F3 / 100, nil
}

// calculateAdvanceDeclineScore 计算涨跌家数得分
func (api *SentimentAPI) calculateAdvanceDeclineScore(advance, decline int) float64 {
	if decline == 0 {
		return 100
	}
	ratio := float64(advance) / float64(decline)
	// ratio: 0.2 -> 0, 1 -> 50, 5 -> 100
	if ratio <= 0.2 {
		return 0
	}
	if ratio >= 5 {
		return 100
	}
	if ratio < 1 {
		return (ratio - 0.2) / 0.8 * 50
	}
	return 50 + (ratio-1)/4*50
}

// calculateLimitScore 计算涨跌停得分
func (api *SentimentAPI) calculateLimitScore(limitUp, limitDown int) float64 {
	if limitDown == 0 && limitUp == 0 {
		return 50
	}
	if limitDown == 0 {
		return 100
	}
	ratio := float64(limitUp) / float64(limitDown)
	// ratio: 0.1 -> 0, 1 -> 50, 10 -> 100
	if ratio <= 0.1 {
		return 0
	}
	if ratio >= 10 {
		return 100
	}
	if ratio < 1 {
		return (ratio - 0.1) / 0.9 * 50
	}
	return 50 + (ratio-1)/9*50
}

// calculateNorthFlowScore 计算北向资金得分
func (api *SentimentAPI) calculateNorthFlowScore(flow float64) float64 {
	// flow单位是元，转换为亿
	flowBillion := flow / 100000000
	// -100亿 -> 0, 0 -> 50, +100亿 -> 100
	if flowBillion <= -100 {
		return 0
	}
	if flowBillion >= 100 {
		return 100
	}
	return 50 + flowBillion/2
}

// calculateMainFlowScore 计算主力资金得分
func (api *SentimentAPI) calculateMainFlowScore(flow float64) float64 {
	flowBillion := flow / 100000000
	// -500亿 -> 0, 0 -> 50, +500亿 -> 100
	if flowBillion <= -500 {
		return 0
	}
	if flowBillion >= 500 {
		return 100
	}
	return 50 + flowBillion/10
}

// calculateIndexChangeScore 计算指数涨跌得分
func (api *SentimentAPI) calculateIndexChangeScore(change float64) float64 {
	// -5% -> 0, 0 -> 50, +5% -> 100
	if change <= -5 {
		return 0
	}
	if change >= 5 {
		return 100
	}
	return 50 + change*10
}

// getSentimentLevel 获取情绪等级
func (api *SentimentAPI) getSentimentLevel(value float64) string {
	if value < 20 {
		return "extreme_fear"
	}
	if value < 40 {
		return "fear"
	}
	if value < 60 {
		return "neutral"
	}
	if value < 80 {
		return "greed"
	}
	return "extreme_greed"
}

// getSentimentLevelCN 获取中文情绪等级
func (api *SentimentAPI) getSentimentLevelCN(value float64) string {
	if value < 20 {
		return "极度恐慌"
	}
	if value < 40 {
		return "恐慌"
	}
	if value < 60 {
		return "中性"
	}
	if value < 80 {
		return "贪婪"
	}
	return "极度贪婪"
}

// getSentimentDescription 获取情绪描述
func (api *SentimentAPI) getSentimentDescription(value float64) string {
	if value < 20 {
		return "市场极度恐慌，可能存在超卖机会"
	}
	if value < 40 {
		return "市场情绪偏悲观，投资者较为谨慎"
	}
	if value < 60 {
		return "市场情绪中性，多空力量相对平衡"
	}
	if value < 80 {
		return "市场情绪偏乐观，投资者较为积极"
	}
	return "市场极度贪婪，需警惕回调风险"
}

// GetGlobalMarketSentiment 获取全球市场情绪
func (api *SentimentAPI) GetGlobalMarketSentiment(country string) (*MarketSentiment, error) {
	cacheKey := "sentiment_" + country
	if cached, ok := api.rm.GetCache(cacheKey); ok {
		return cached.(*MarketSentiment), nil
	}

	// 对于国际市场，我们使用指数涨跌幅和VIX（如果可用）来计算
	components := make([]SentimentComponent, 0)
	totalWeight := 0.0
	weightedSum := 0.0

	// 尝试从缓存获取指数数据，避免重复请求
	var indices []models.GlobalIndex
	if cachedIndices, ok := api.rm.GetCache("global_indices"); ok {
		indices = cachedIndices.([]models.GlobalIndex)
	} else {
		// 缓存没有，快速获取（使用较短超时）
		globalAPI := NewGlobalMarketAPI()
		var err error
		indices, err = globalAPI.GetGlobalIndices()
		if err != nil {
			indices = nil
		}
	}

	if indices != nil {
		var countryIndices []float64
		for _, idx := range indices {
			if idx.Country == country && idx.ChangePercent != 0 {
				countryIndices = append(countryIndices, idx.ChangePercent)
			}
		}

		if len(countryIndices) > 0 {
			// 计算平均涨跌幅
			var sum float64
			for _, v := range countryIndices {
				sum += v
			}
			avgChange := sum / float64(len(countryIndices))
			value := api.calculateIndexChangeScore(avgChange)
			weight := 0.50
			components = append(components, SentimentComponent{
				Name:   "index_performance",
				NameCN: "指数表现",
				Value:  value,
				Weight: weight,
				Data:   fmt.Sprintf("平均涨跌%.2f%%", avgChange),
			})
			weightedSum += value * weight
			totalWeight += weight
		}
	}

	// 对于美国市场，尝试获取VIX
	if country == "us" {
		vix, err := api.getVIXIndex()
		if err == nil && vix > 0 {
			value := api.calculateVIXScore(vix)
			weight := 0.50
			components = append(components, SentimentComponent{
				Name:   "vix",
				NameCN: "恐慌指数VIX",
				Value:  value,
				Weight: weight,
				Data:   fmt.Sprintf("VIX: %.2f", vix),
			})
			weightedSum += value * weight
			totalWeight += weight
		}
	}

	// 如果没有足够数据，使用默认值
	var finalValue float64
	if totalWeight > 0 {
		finalValue = weightedSum / totalWeight
	} else {
		finalValue = 50
	}

	sentiment := &MarketSentiment{
		Value:       finalValue,
		Level:       api.getSentimentLevel(finalValue),
		LevelCN:     api.getSentimentLevelCN(finalValue),
		Description: api.getSentimentDescription(finalValue),
		UpdateTime:  time.Now().Format("15:04:05"),
		Components:  components,
	}

	// 缓存60秒
	api.rm.SetCache(cacheKey, sentiment, 60*time.Second)

	return sentiment, nil
}

// getVIXIndex 获取VIX恐慌指数
func (api *SentimentAPI) getVIXIndex() (float64, error) {
	// 从新浪获取VIX
	url := "https://hq.sinajs.cn/list=int_vix"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Referer", "https://finance.sina.com.cn/")
	req.Header.Set("User-Agent", api.rm.GetRandomUA())

	resp, err := api.rm.DoRequestWithRateLimit("sina.com.cn", req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}

	// 解析: var hq_str_int_vix="VIX恐慌指数,18.50,..."
	line := string(body)
	if !strings.Contains(line, "=") {
		return 0, fmt.Errorf("invalid response")
	}

	parts := strings.Split(line, "\"")
	if len(parts) < 2 {
		return 0, fmt.Errorf("invalid format")
	}

	data := strings.Split(parts[1], ",")
	if len(data) < 2 {
		return 0, fmt.Errorf("insufficient data")
	}

	vix, err := strconv.ParseFloat(data[1], 64)
	if err != nil {
		return 0, err
	}

	return vix, nil
}

// calculateVIXScore 计算VIX得分
// VIX越高表示恐慌越强，得分越低
func (api *SentimentAPI) calculateVIXScore(vix float64) float64 {
	// VIX: 10 -> 100 (极度贪婪), 20 -> 50 (中性), 40 -> 0 (极度恐慌)
	if vix <= 10 {
		return 100
	}
	if vix >= 40 {
		return 0
	}
	if vix <= 20 {
		return 100 - (vix-10)*5
	}
	return 50 - (vix-20)*2.5
}
