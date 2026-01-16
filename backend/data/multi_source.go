package data

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
	"stock-ai/backend/models"
)

// DataSource 数据源类型
type DataSource int

const (
	SourceEastmoney DataSource = iota // 东方财富
	SourceSina                        // 新浪财经
	SourceTencent                     // 腾讯财经
	Source163                         // 网易财经
	SourceXueqiu                      // 雪球
	SourceTHS                         // 同花顺
)

// DataSourceInfo 数据源信息
type DataSourceInfo struct {
	Name        string
	Domain      string
	Priority    int
	FailCount   int
	LastFail    time.Time
	Disabled    bool
	LastSuccess time.Time
}

// MultiSourceManager 多数据源管理器
type MultiSourceManager struct {
	rm           *RequestManager
	sources      map[DataSource]*DataSourceInfo
	currentIndex int
	mu           sync.RWMutex
	// 首次加载标志 - 首次加载时并行请求所有数据源
	isFirstLoad  bool
	// 轮询间隔（秒）
	pollInterval int
}

var globalMultiSource *MultiSourceManager
var multiSourceOnce sync.Once

// GetMultiSourceManager 获取多数据源管理器单例
func GetMultiSourceManager() *MultiSourceManager {
	multiSourceOnce.Do(func() {
		globalMultiSource = NewMultiSourceManager()
	})
	return globalMultiSource
}

// NewMultiSourceManager 创建多数据源管理器
func NewMultiSourceManager() *MultiSourceManager {
	msm := &MultiSourceManager{
		rm: GetRequestManager(),
		sources: map[DataSource]*DataSourceInfo{
			SourceEastmoney: {Name: "东方财富", Domain: "eastmoney.com", Priority: 1},
			SourceSina:      {Name: "新浪财经", Domain: "sina.com.cn", Priority: 2},
			SourceTencent:   {Name: "腾讯财经", Domain: "qq.com", Priority: 3},
			Source163:       {Name: "网易财经", Domain: "163.com", Priority: 4},
			SourceXueqiu:    {Name: "雪球", Domain: "xueqiu.com", Priority: 5},
			SourceTHS:       {Name: "同花顺", Domain: "10jqka.com.cn", Priority: 6},
		},
		currentIndex: 0,
		isFirstLoad:  true,  // 初始为首次加载模式
		pollInterval: 10,    // 轮询间隔10秒
	}
	return msm
}

// GetNextSource 获取下一个可用数据源（轮询）
func (msm *MultiSourceManager) GetNextSource() DataSource {
	msm.mu.Lock()
	defer msm.mu.Unlock()

	sourceOrder := []DataSource{SourceEastmoney, SourceSina, SourceTencent, Source163, SourceXueqiu, SourceTHS}

	// 尝试找到下一个可用的数据源
	for i := 0; i < len(sourceOrder); i++ {
		idx := (msm.currentIndex + i) % len(sourceOrder)
		source := sourceOrder[idx]
		info := msm.sources[source]

		// 检查是否被禁用
		if info.Disabled {
			// 5分钟后自动恢复
			if time.Since(info.LastFail) > 5*time.Minute {
				info.Disabled = false
				info.FailCount = 0
			} else {
				continue
			}
		}

		msm.currentIndex = (idx + 1) % len(sourceOrder)
		return source
	}

	// 所有数据源都不可用，返回默认
	return SourceEastmoney
}

// MarkSourceFailed 标记数据源失败
func (msm *MultiSourceManager) MarkSourceFailed(source DataSource) {
	msm.mu.Lock()
	defer msm.mu.Unlock()

	if info, ok := msm.sources[source]; ok {
		info.FailCount++
		info.LastFail = time.Now()
		// 连续失败3次，禁用该数据源
		if info.FailCount >= 3 {
			info.Disabled = true
		}
	}
}

// MarkSourceSuccess 标记数据源成功
func (msm *MultiSourceManager) MarkSourceSuccess(source DataSource) {
	msm.mu.Lock()
	defer msm.mu.Unlock()

	if info, ok := msm.sources[source]; ok {
		info.FailCount = 0
		info.Disabled = false
		info.LastSuccess = time.Now()
	}
}

// IsSourceAvailable 检查数据源是否可用
func (msm *MultiSourceManager) IsSourceAvailable(source DataSource) bool {
	msm.mu.RLock()
	defer msm.mu.RUnlock()

	if info, ok := msm.sources[source]; ok {
		if info.Disabled && time.Since(info.LastFail) < 30*time.Second {
			// 减少禁用时间到30秒，加快恢复
			return false
		}
		return true
	}
	return false
}

// IsFirstLoad 检查是否为首次加载
func (msm *MultiSourceManager) IsFirstLoad() bool {
	msm.mu.RLock()
	defer msm.mu.RUnlock()
	return msm.isFirstLoad
}

// SetFirstLoadComplete 标记首次加载完成，切换到轮询模式
func (msm *MultiSourceManager) SetFirstLoadComplete() {
	msm.mu.Lock()
	defer msm.mu.Unlock()
	msm.isFirstLoad = false
}

// GetPollInterval 获取轮询间隔（秒）
func (msm *MultiSourceManager) GetPollInterval() int {
	msm.mu.RLock()
	defer msm.mu.RUnlock()
	return msm.pollInterval
}

// ==================== 全球指数多数据源获取 ====================

// FetchGlobalIndicesFromSina 从新浪获取全球指数
func (msm *MultiSourceManager) FetchGlobalIndicesFromSina() (map[string]*IndexData, error) {
	result := make(map[string]*IndexData)

	// 新浪全球指数代码 - 包含更多指数
	codes := "int_dji,int_nasdaq,int_sp500,int_hangseng,int_nikkei,b_FTSE,b_DAX,b_HSI,b_SPX,b_KOSPI,b_TWII,b_STI,b_SENSEX,b_AXJO,b_GSPTSE,b_FCHI"
	url := fmt.Sprintf("https://hq.sinajs.cn/list=%s", codes)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://finance.sina.com.cn/")
	req.Header.Set("User-Agent", msm.rm.GetRandomUA())

	resp, err := msm.rm.DoRequestWithRateLimit("sina.com.cn", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 转换GBK到UTF-8
	decoder := simplifiedchinese.GBK.NewDecoder()
	utf8Body, _, _ := transform.Bytes(decoder, body)

	lines := strings.Split(string(utf8Body), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		data := msm.parseSinaIndexLine(line)
		if data != nil {
			result[data.Code] = data
		}
	}

	return result, nil
}

// IndexData 指数数据
type IndexData struct {
	Code          string
	Name          string
	Price         float64
	Change        float64
	ChangePercent float64
}

// parseSinaIndexLine 解析新浪指数行
func (msm *MultiSourceManager) parseSinaIndexLine(line string) *IndexData {
	// 格式: var hq_str_int_dji="道琼斯,46247.29,299.97,0.65";
	// 或: var hq_str_b_KOSPI="韩国KOSPI指数,4797.55,74.45,1.58,..."
	re := regexp.MustCompile(`var hq_str_([a-zA-Z_0-9]+)="([^"]*)"`)
	matches := re.FindStringSubmatch(line)
	if len(matches) < 3 || matches[2] == "" {
		return nil
	}

	code := matches[1]
	data := matches[2]
	parts := strings.Split(data, ",")

	if len(parts) < 4 {
		return nil
	}

	price, _ := strconv.ParseFloat(parts[1], 64)
	change, _ := strconv.ParseFloat(parts[2], 64)
	changePercent, _ := strconv.ParseFloat(parts[3], 64)

	// 映射代码 - 扩展支持更多指数
	codeMap := map[string]string{
		"int_dji":      "DJI",
		"int_nasdaq":   "IXIC",
		"int_sp500":    "SPX",
		"int_hangseng": "HSI",
		"int_nikkei":   "N225",
		"b_FTSE":       "FTSE",
		"b_DAX":        "GDAXI",
		"b_HSI":        "HSI",
		"b_SPX":        "SPX",
		"b_KOSPI":      "KOSPI",
		"b_TWII":       "TWII",
		"b_STI":        "STI",
		"b_SENSEX":     "SENSEX",
		"b_AXJO":       "AXJO",
		"b_GSPTSE":     "TSX",
		"b_FCHI":       "FCHI",
		"b_CAC40":      "FCHI",
	}

	mappedCode := code
	if mc, ok := codeMap[code]; ok {
		mappedCode = mc
	}

	return &IndexData{
		Code:          mappedCode,
		Name:          parts[0],
		Price:         price,
		Change:        change,
		ChangePercent: changePercent,
	}
}

// FetchGlobalIndicesFromTencent 从腾讯获取全球指数
func (msm *MultiSourceManager) FetchGlobalIndicesFromTencent() (map[string]*IndexData, error) {
	result := make(map[string]*IndexData)

	// 腾讯全球指数代码 - 扩展更多指数
	codes := "usDJI,usIXIC,usSPX,hkHSI,jpN225,ukFTSE,deDAX,frCAC,krKOSPI,twTWSE,sgSTI,inSENSEX,auASX,caTSX"
	url := fmt.Sprintf("https://qt.gtimg.cn/q=%s", codes)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://gu.qq.com/")
	req.Header.Set("User-Agent", msm.rm.GetRandomUA())

	resp, err := msm.rm.DoRequestWithRateLimit("qq.com", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 转换GBK到UTF-8
	decoder := simplifiedchinese.GBK.NewDecoder()
	utf8Body, _, _ := transform.Bytes(decoder, body)

	lines := strings.Split(string(utf8Body), ";")
	for _, line := range lines {
		if line == "" {
			continue
		}
		data := msm.parseTencentIndexLine(line)
		if data != nil {
			result[data.Code] = data
		}
	}

	return result, nil
}

// parseTencentIndexLine 解析腾讯指数行
func (msm *MultiSourceManager) parseTencentIndexLine(line string) *IndexData {
	// 格式: v_usDJI="200~道琼斯~DJI~46247.29~..."
	re := regexp.MustCompile(`v_([a-zA-Z]+)="([^"]*)"`)
	matches := re.FindStringSubmatch(line)
	if len(matches) < 3 || matches[2] == "" {
		return nil
	}

	code := matches[1]
	data := matches[2]
	parts := strings.Split(data, "~")

	if len(parts) < 10 {
		return nil
	}

	price, _ := strconv.ParseFloat(parts[3], 64)
	preClose, _ := strconv.ParseFloat(parts[4], 64)
	change := price - preClose
	changePercent := 0.0
	if preClose > 0 {
		changePercent = (change / preClose) * 100
	}

	// 映射代码 - 扩展支持更多指数
	codeMap := map[string]string{
		"usDJI":    "DJI",
		"usIXIC":   "IXIC",
		"usSPX":    "SPX",
		"hkHSI":    "HSI",
		"jpN225":   "N225",
		"ukFTSE":   "FTSE",
		"deDAX":    "GDAXI",
		"frCAC":    "FCHI",
		"krKOSPI":  "KOSPI",
		"twTWSE":   "TWII",
		"sgSTI":    "STI",
		"inSENSEX": "SENSEX",
		"auASX":    "AXJO",
		"caTSX":    "TSX",
	}

	mappedCode := code
	if mc, ok := codeMap[code]; ok {
		mappedCode = mc
	}

	return &IndexData{
		Code:          mappedCode,
		Name:          parts[1],
		Price:         price,
		Change:        change,
		ChangePercent: changePercent,
	}
}

// FetchGlobalIndicesFromEastmoney 从东方财富获取全球指数
func (msm *MultiSourceManager) FetchGlobalIndicesFromEastmoney() (map[string]*IndexData, error) {
	result := make(map[string]*IndexData)

	// 东方财富代码映射 - 返回的代码 -> 我们的代码
	indexCodeMap := map[string]string{
		"DJIA":   "DJI",
		"SPX":    "SPX",
		"NDX":    "IXIC",
		"N225":   "N225",
		"HSI":    "HSI",
		"HSCEI":  "HSCEI",
		"KS11":   "KOSPI",
		"TWII":   "TWII",
		"FTSE":   "FTSE",
		"GDAXI":  "GDAXI",
		"FCHI":   "FCHI",
		"AXJO":   "AXJO",
		"GSPTSE": "TSX",
		"SENSEX": "SENSEX",
		"NSEI":   "NIFTY",
		"STI":    "STI",
	}

	// 构建请求 - 添加更多指数
	var codeList []string
	for emCode := range indexCodeMap {
		codeList = append(codeList, "i:100."+emCode)
	}

	url := fmt.Sprintf("https://push2.eastmoney.com/api/qt/clist/get?pn=1&pz=50&fs=%s&fields=f2,f3,f4,f12,f14&_=%d",
		strings.Join(codeList, ","), time.Now().UnixMilli())

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://quote.eastmoney.com/")
	req.Header.Set("User-Agent", msm.rm.GetRandomUA())

	resp, err := msm.rm.DoRequestWithRateLimit("eastmoney.com", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data struct {
		Rc   int `json:"rc"`
		Data struct {
			Total int                    `json:"total"`
			Diff  map[string]interface{} `json:"diff"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	for _, v := range data.Data.Diff {
		item, ok := v.(map[string]interface{})
		if !ok {
			continue
		}

		emCode, _ := item["f12"].(string)
		f2, _ := item["f2"].(float64)
		f3, _ := item["f3"].(float64)
		f4, _ := item["f4"].(float64)
		name, _ := item["f14"].(string)

		if mappedCode, ok := indexCodeMap[emCode]; ok {
			result[mappedCode] = &IndexData{
				Code:          mappedCode,
				Name:          name,
				Price:         f2 / 100,
				Change:        f4 / 100,
				ChangePercent: f3 / 100,
			}
		}
	}

	return result, nil
}

// GetGlobalIndicesWithFallback 获取全球指数
// 首次加载：并行请求所有数据源，取最快返回的
// 后续轮询：按顺序轮询单个数据源
func (msm *MultiSourceManager) GetGlobalIndicesWithFallback() (map[string]*IndexData, DataSource, error) {
	// 数据源列表
	sources := []struct {
		source DataSource
		fetch  func() (map[string]*IndexData, error)
	}{
		{SourceEastmoney, msm.FetchGlobalIndicesFromEastmoney},
		{SourceSina, msm.FetchGlobalIndicesFromSina},
		{SourceTencent, msm.FetchGlobalIndicesFromTencent},
	}

	// 首次加载：并行请求所有数据源
	if msm.IsFirstLoad() {
		return msm.parallelFetchGlobalIndices(sources)
	}

	// 后续轮询：按顺序轮询单个数据源
	return msm.pollFetchGlobalIndices(sources)
}

// parallelFetchGlobalIndices 并行获取全球指数（首次加载使用）
func (msm *MultiSourceManager) parallelFetchGlobalIndices(sources []struct {
	source DataSource
	fetch  func() (map[string]*IndexData, error)
}) (map[string]*IndexData, DataSource, error) {
	type result struct {
		data   map[string]*IndexData
		source DataSource
		err    error
	}

	resultChan := make(chan result, len(sources))

	// 并行启动所有数据源请求
	for _, s := range sources {
		go func(src DataSource, fetch func() (map[string]*IndexData, error)) {
			data, err := fetch()
			resultChan <- result{data: data, source: src, err: err}
		}(s.source, s.fetch)
	}

	// 等待第一个成功的结果，或者所有都失败
	var lastErr error
	for i := 0; i < len(sources); i++ {
		res := <-resultChan
		if res.err == nil && len(res.data) > 0 {
			// 找到有效数据，立即返回
			msm.MarkSourceSuccess(res.source)
			return res.data, res.source, nil
		}
		if res.err != nil {
			lastErr = res.err
			msm.MarkSourceFailed(res.source)
		}
	}

	return nil, SourceEastmoney, fmt.Errorf("all data sources failed: %v", lastErr)
}

// pollFetchGlobalIndices 轮询获取全球指数（后续刷新使用）
func (msm *MultiSourceManager) pollFetchGlobalIndices(sources []struct {
	source DataSource
	fetch  func() (map[string]*IndexData, error)
}) (map[string]*IndexData, DataSource, error) {
	// 获取下一个数据源
	nextSource := msm.GetNextSource()

	// 找到对应的fetch函数
	for _, s := range sources {
		if s.source == nextSource {
			data, err := s.fetch()
			if err == nil && len(data) > 0 {
				msm.MarkSourceSuccess(s.source)
				return data, s.source, nil
			}
			if err != nil {
				msm.MarkSourceFailed(s.source)
			}
			break
		}
	}

	// 当前数据源失败，尝试下一个
	for _, s := range sources {
		if s.source != nextSource && msm.IsSourceAvailable(s.source) {
			data, err := s.fetch()
			if err == nil && len(data) > 0 {
				msm.MarkSourceSuccess(s.source)
				return data, s.source, nil
			}
			if err != nil {
				msm.MarkSourceFailed(s.source)
			}
		}
	}

	return nil, SourceEastmoney, fmt.Errorf("all data sources failed")
}

// ==================== A股数据多数据源获取 ====================

// FetchAStockFromSina 从新浪获取A股数据
func (msm *MultiSourceManager) FetchAStockFromSina(codes []string) (map[string]*models.StockPrice, error) {
	result := make(map[string]*models.StockPrice)

	if len(codes) == 0 {
		return result, nil
	}

	// 构建新浪代码
	// 如果代码已经带有 sh/sz 前缀，直接使用
	// 如果没有前缀，根据代码开头判断：6开头->sh，其他->sz
	var sinaCodes []string
	codeMap := make(map[string]string) // 新浪代码 -> 原始代码
	for _, code := range codes {
		var sinaCode string
		if strings.HasPrefix(code, "sh") || strings.HasPrefix(code, "sz") {
			// 已经带前缀，直接使用
			sinaCode = code
		} else if strings.HasPrefix(code, "6") {
			sinaCode = "sh" + code
		} else {
			sinaCode = "sz" + code
		}
		sinaCodes = append(sinaCodes, sinaCode)
		codeMap[sinaCode] = code
	}

	url := fmt.Sprintf("https://hq.sinajs.cn/list=%s", strings.Join(sinaCodes, ","))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://finance.sina.com.cn/")
	req.Header.Set("User-Agent", msm.rm.GetRandomUA())

	resp, err := msm.rm.DoRequestWithRateLimit("sina.com.cn", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 转换GBK到UTF-8
	decoder := simplifiedchinese.GBK.NewDecoder()
	utf8Body, _, _ := transform.Bytes(decoder, body)

	lines := strings.Split(string(utf8Body), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		price := msm.parseSinaStockLine(line)
		if price != nil {
			result[price.Code] = price
		}
	}

	return result, nil
}

// parseSinaStockLine 解析新浪A股行情
func (msm *MultiSourceManager) parseSinaStockLine(line string) *models.StockPrice {
	// 格式: var hq_str_sh600000="浦发银行,10.50,10.48,10.52,10.55,10.45,10.51,10.52,..."
	re := regexp.MustCompile(`var hq_str_(s[hz]\d+)="([^"]*)"`)
	matches := re.FindStringSubmatch(line)
	if len(matches) < 3 || matches[2] == "" {
		return nil
	}

	fullCode := matches[1] // 保留完整代码（带sh/sz前缀）
	data := matches[2]
	parts := strings.Split(data, ",")

	if len(parts) < 32 {
		return nil
	}

	open, _ := strconv.ParseFloat(parts[1], 64)
	preClose, _ := strconv.ParseFloat(parts[2], 64)
	price, _ := strconv.ParseFloat(parts[3], 64)
	high, _ := strconv.ParseFloat(parts[4], 64)
	low, _ := strconv.ParseFloat(parts[5], 64)
	volume, _ := strconv.ParseInt(parts[8], 10, 64)
	amount, _ := strconv.ParseFloat(parts[9], 64)

	change := price - preClose
	changePercent := 0.0
	if preClose > 0 {
		changePercent = (change / preClose) * 100
	}

	return &models.StockPrice{
		Code:          fullCode, // 使用完整代码（带前缀）
		Name:          parts[0],
		Price:         price,
		Open:          open,
		High:          high,
		Low:           low,
		PreClose:      preClose,
		Change:        change,
		ChangePercent: changePercent,
		Volume:        volume,
		Amount:        amount,
		UpdateTime:    parts[31],
	}
}

// FetchAStockFromTencent 从腾讯获取A股数据
func (msm *MultiSourceManager) FetchAStockFromTencent(codes []string) (map[string]*models.StockPrice, error) {
	result := make(map[string]*models.StockPrice)

	if len(codes) == 0 {
		return result, nil
	}

	// 构建腾讯代码
	// 如果代码已经带有 sh/sz 前缀，直接使用
	// 如果没有前缀，根据代码开头判断：6开头->sh，其他->sz
	var qqCodes []string
	for _, code := range codes {
		var qqCode string
		if strings.HasPrefix(code, "sh") || strings.HasPrefix(code, "sz") {
			// 已经带前缀，直接使用
			qqCode = code
		} else if strings.HasPrefix(code, "6") {
			qqCode = "sh" + code
		} else {
			qqCode = "sz" + code
		}
		qqCodes = append(qqCodes, qqCode)
	}

	url := fmt.Sprintf("https://qt.gtimg.cn/q=%s", strings.Join(qqCodes, ","))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://gu.qq.com/")
	req.Header.Set("User-Agent", msm.rm.GetRandomUA())

	resp, err := msm.rm.DoRequestWithRateLimit("qq.com", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 转换GBK到UTF-8
	decoder := simplifiedchinese.GBK.NewDecoder()
	utf8Body, _, _ := transform.Bytes(decoder, body)

	lines := strings.Split(string(utf8Body), ";")
	for _, line := range lines {
		if line == "" {
			continue
		}
		price := msm.parseTencentStockLine(line)
		if price != nil {
			result[price.Code] = price
		}
	}

	return result, nil
}

// parseTencentStockLine 解析腾讯A股行情
func (msm *MultiSourceManager) parseTencentStockLine(line string) *models.StockPrice {
	// 格式: v_sh600000="1~浦发银行~600000~10.52~10.48~10.50~..."
	re := regexp.MustCompile(`v_(s[hz]\d+)="([^"]*)"`)
	matches := re.FindStringSubmatch(line)
	if len(matches) < 3 || matches[2] == "" {
		return nil
	}

	fullCode := matches[1] // 保留完整代码（带sh/sz前缀）
	data := matches[2]
	parts := strings.Split(data, "~")

	if len(parts) < 35 {
		return nil
	}

	price, _ := strconv.ParseFloat(parts[3], 64)
	preClose, _ := strconv.ParseFloat(parts[4], 64)
	open, _ := strconv.ParseFloat(parts[5], 64)
	volume, _ := strconv.ParseInt(parts[6], 10, 64)
	amount, _ := strconv.ParseFloat(parts[37], 64)
	high, _ := strconv.ParseFloat(parts[33], 64)
	low, _ := strconv.ParseFloat(parts[34], 64)

	change := price - preClose
	changePercent := 0.0
	if preClose > 0 {
		changePercent = (change / preClose) * 100
	}

	return &models.StockPrice{
		Code:          fullCode, // 使用完整代码（带前缀）
		Name:          parts[1],
		Price:         price,
		Open:          open,
		High:          high,
		Low:           low,
		PreClose:      preClose,
		Change:        change,
		ChangePercent: changePercent,
		Volume:        volume,
		Amount:        amount,
		UpdateTime:    parts[30],
	}
}

// FetchAStockFromEastmoney 从东方财富获取A股数据
func (msm *MultiSourceManager) FetchAStockFromEastmoney(codes []string) (map[string]*models.StockPrice, error) {
	result := make(map[string]*models.StockPrice)

	if len(codes) == 0 {
		return result, nil
	}

	// 构建东方财富代码
	// 如果代码已经带有 sh/sz 前缀，根据前缀判断
	// sh -> 1.xxx, sz -> 0.xxx
	// 如果没有前缀，根据代码开头判断：6开头->1.，其他->0.
	var emCodes []string
	codeMap := make(map[string]string) // 东方财富代码 -> 原始代码
	for _, code := range codes {
		var emCode string
		var pureCode string
		if strings.HasPrefix(code, "sh") {
			pureCode = code[2:]
			emCode = "1." + pureCode
		} else if strings.HasPrefix(code, "sz") {
			pureCode = code[2:]
			emCode = "0." + pureCode
		} else if strings.HasPrefix(code, "6") {
			pureCode = code
			emCode = "1." + code
		} else {
			pureCode = code
			emCode = "0." + code
		}
		emCodes = append(emCodes, emCode)
		codeMap[pureCode] = code // 纯代码 -> 原始代码（带前缀）
	}

	url := fmt.Sprintf("https://push2.eastmoney.com/api/qt/ulist.np/get?secids=%s&fields=f2,f3,f4,f5,f6,f12,f14,f15,f16,f17,f18&_=%d",
		strings.Join(emCodes, ","), time.Now().UnixMilli())

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://quote.eastmoney.com/")
	req.Header.Set("User-Agent", msm.rm.GetRandomUA())

	resp, err := msm.rm.DoRequestWithRateLimit("eastmoney.com", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data struct {
		Data struct {
			Diff []struct {
				F2  float64 `json:"f2"`  // 最新价*100
				F3  float64 `json:"f3"`  // 涨跌幅*100
				F4  float64 `json:"f4"`  // 涨跌额*100
				F5  int64   `json:"f5"`  // 成交量
				F6  float64 `json:"f6"`  // 成交额
				F12 string  `json:"f12"` // 代码
				F14 string  `json:"f14"` // 名称
				F15 float64 `json:"f15"` // 最高*100
				F16 float64 `json:"f16"` // 最低*100
				F17 float64 `json:"f17"` // 开盘*100
				F18 float64 `json:"f18"` // 昨收*100
			} `json:"diff"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	for _, item := range data.Data.Diff {
		// 东方财富所有数据都需要除以100
		price := item.F2 / 100
		change := item.F4 / 100
		changePercent := item.F3 / 100
		high := item.F15 / 100
		low := item.F16 / 100
		open := item.F17 / 100
		preClose := item.F18 / 100

		// 数据合理性检查：价格不能为0或负数
		if price <= 0 {
			continue
		}

		// 使用原始代码（带前缀）
		originalCode := item.F12
		if origCode, ok := codeMap[item.F12]; ok {
			originalCode = origCode
		}

		result[originalCode] = &models.StockPrice{
			Code:          originalCode,
			Name:          item.F14,
			Price:         price,
			Change:        change,
			ChangePercent: changePercent,
			Volume:        item.F5,
			Amount:        item.F6,
			High:          high,
			Low:           low,
			Open:          open,
			PreClose:      preClose,
			UpdateTime:    time.Now().Format("15:04:05"),
		}
	}

	return result, nil
}

// GetAStockWithFallback 获取A股数据
// 首次加载：并行请求所有数据源，取最快返回的
// 后续轮询：按顺序轮询单个数据源
func (msm *MultiSourceManager) GetAStockWithFallback(codes []string) (map[string]*models.StockPrice, DataSource, error) {
	sources := []struct {
		source DataSource
		fetch  func([]string) (map[string]*models.StockPrice, error)
	}{
		{SourceEastmoney, msm.FetchAStockFromEastmoney},
		{SourceSina, msm.FetchAStockFromSina},
		{SourceTencent, msm.FetchAStockFromTencent},
	}

	// 首次加载：并行请求所有数据源
	if msm.IsFirstLoad() {
		return msm.parallelFetchAStock(sources, codes)
	}

	// 后续轮询：按顺序轮询单个数据源
	return msm.pollFetchAStock(sources, codes)
}

// parallelFetchAStock 并行获取A股数据（首次加载使用）
func (msm *MultiSourceManager) parallelFetchAStock(sources []struct {
	source DataSource
	fetch  func([]string) (map[string]*models.StockPrice, error)
}, codes []string) (map[string]*models.StockPrice, DataSource, error) {
	type result struct {
		data   map[string]*models.StockPrice
		source DataSource
		err    error
	}

	resultChan := make(chan result, len(sources))

	// 并行启动所有数据源请求
	for _, s := range sources {
		go func(src DataSource, fetch func([]string) (map[string]*models.StockPrice, error)) {
			data, err := fetch(codes)
			resultChan <- result{data: data, source: src, err: err}
		}(s.source, s.fetch)
	}

	// 收集所有结果，选择数据最完整的
	var bestResult result
	var lastErr error
	for i := 0; i < len(sources); i++ {
		res := <-resultChan
		if res.err != nil {
			lastErr = res.err
			msm.MarkSourceFailed(res.source)
			continue
		}
		if len(res.data) > len(bestResult.data) {
			bestResult = res
		}
	}

	if len(bestResult.data) > 0 {
		msm.MarkSourceSuccess(bestResult.source)
		return bestResult.data, bestResult.source, nil
	}

	return nil, SourceEastmoney, fmt.Errorf("all data sources failed: %v", lastErr)
}

// pollFetchAStock 轮询获取A股数据（后续刷新使用）
func (msm *MultiSourceManager) pollFetchAStock(sources []struct {
	source DataSource
	fetch  func([]string) (map[string]*models.StockPrice, error)
}, codes []string) (map[string]*models.StockPrice, DataSource, error) {
	// 获取下一个数据源
	nextSource := msm.GetNextSource()

	// 找到对应的fetch函数
	for _, s := range sources {
		if s.source == nextSource {
			data, err := s.fetch(codes)
			if err == nil && len(data) > 0 {
				msm.MarkSourceSuccess(s.source)
				return data, s.source, nil
			}
			if err != nil {
				msm.MarkSourceFailed(s.source)
			}
			break
		}
	}

	// 当前数据源失败，尝试下一个
	for _, s := range sources {
		if s.source != nextSource && msm.IsSourceAvailable(s.source) {
			data, err := s.fetch(codes)
			if err == nil && len(data) > 0 {
				msm.MarkSourceSuccess(s.source)
				return data, s.source, nil
			}
			if err != nil {
				msm.MarkSourceFailed(s.source)
			}
		}
	}

	return nil, SourceEastmoney, fmt.Errorf("all data sources failed")
}

// GetSourceStats 获取数据源统计信息
func (msm *MultiSourceManager) GetSourceStats() map[string]interface{} {
	msm.mu.RLock()
	defer msm.mu.RUnlock()

	stats := make(map[string]interface{})
	for source, info := range msm.sources {
		stats[info.Name] = map[string]interface{}{
			"domain":      info.Domain,
			"priority":    info.Priority,
			"failCount":   info.FailCount,
			"disabled":    info.Disabled,
			"lastFail":    info.LastFail.Format("2006-01-02 15:04:05"),
			"lastSuccess": info.LastSuccess.Format("2006-01-02 15:04:05"),
			"sourceId":    int(source),
		}
	}
	return stats
}
