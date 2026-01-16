package data

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"stock-ai/backend/models"
)

// GlobalMarketAPI 全球市场数据API
type GlobalMarketAPI struct {
	rm *RequestManager
}

// NewGlobalMarketAPI 创建全球市场API实例
func NewGlobalMarketAPI() *GlobalMarketAPI {
	return &GlobalMarketAPI{
		rm: GetRequestManager(),
	}
}

// 热门美股列表
var popularUSStocks = []models.USStock{
	{Symbol: "AAPL", Name: "Apple Inc.", NameCN: "苹果", Exchange: "NASDAQ", Sector: "科技"},
	{Symbol: "MSFT", Name: "Microsoft Corporation", NameCN: "微软", Exchange: "NASDAQ", Sector: "科技"},
	{Symbol: "GOOGL", Name: "Alphabet Inc.", NameCN: "谷歌", Exchange: "NASDAQ", Sector: "科技"},
	{Symbol: "AMZN", Name: "Amazon.com Inc.", NameCN: "亚马逊", Exchange: "NASDAQ", Sector: "消费"},
	{Symbol: "NVDA", Name: "NVIDIA Corporation", NameCN: "英伟达", Exchange: "NASDAQ", Sector: "科技"},
	{Symbol: "META", Name: "Meta Platforms Inc.", NameCN: "Meta", Exchange: "NASDAQ", Sector: "科技"},
	{Symbol: "TSLA", Name: "Tesla Inc.", NameCN: "特斯拉", Exchange: "NASDAQ", Sector: "汽车"},
	{Symbol: "BRK.B", Name: "Berkshire Hathaway", NameCN: "伯克希尔", Exchange: "NYSE", Sector: "金融"},
	{Symbol: "JPM", Name: "JPMorgan Chase & Co.", NameCN: "摩根大通", Exchange: "NYSE", Sector: "金融"},
	{Symbol: "V", Name: "Visa Inc.", NameCN: "Visa", Exchange: "NYSE", Sector: "金融"},
	{Symbol: "JNJ", Name: "Johnson & Johnson", NameCN: "强生", Exchange: "NYSE", Sector: "医疗"},
	{Symbol: "WMT", Name: "Walmart Inc.", NameCN: "沃尔玛", Exchange: "NYSE", Sector: "消费"},
	{Symbol: "PG", Name: "Procter & Gamble", NameCN: "宝洁", Exchange: "NYSE", Sector: "消费"},
	{Symbol: "MA", Name: "Mastercard Inc.", NameCN: "万事达", Exchange: "NYSE", Sector: "金融"},
	{Symbol: "UNH", Name: "UnitedHealth Group", NameCN: "联合健康", Exchange: "NYSE", Sector: "医疗"},
	{Symbol: "HD", Name: "The Home Depot", NameCN: "家得宝", Exchange: "NYSE", Sector: "消费"},
	{Symbol: "DIS", Name: "The Walt Disney", NameCN: "迪士尼", Exchange: "NYSE", Sector: "媒体"},
	{Symbol: "NFLX", Name: "Netflix Inc.", NameCN: "奈飞", Exchange: "NASDAQ", Sector: "媒体"},
	{Symbol: "AMD", Name: "Advanced Micro Devices", NameCN: "AMD", Exchange: "NASDAQ", Sector: "科技"},
	{Symbol: "INTC", Name: "Intel Corporation", NameCN: "英特尔", Exchange: "NASDAQ", Sector: "科技"},
	// 中概股
	{Symbol: "BABA", Name: "Alibaba Group", NameCN: "阿里巴巴", Exchange: "NYSE", Sector: "科技"},
	{Symbol: "PDD", Name: "PDD Holdings", NameCN: "拼多多", Exchange: "NASDAQ", Sector: "消费"},
	{Symbol: "JD", Name: "JD.com Inc.", NameCN: "京东", Exchange: "NASDAQ", Sector: "消费"},
	{Symbol: "BIDU", Name: "Baidu Inc.", NameCN: "百度", Exchange: "NASDAQ", Sector: "科技"},
	{Symbol: "NIO", Name: "NIO Inc.", NameCN: "蔚来", Exchange: "NYSE", Sector: "汽车"},
	{Symbol: "XPEV", Name: "XPeng Inc.", NameCN: "小鹏", Exchange: "NYSE", Sector: "汽车"},
	{Symbol: "LI", Name: "Li Auto Inc.", NameCN: "理想", Exchange: "NASDAQ", Sector: "汽车"},
	{Symbol: "BILI", Name: "Bilibili Inc.", NameCN: "哔哩哔哩", Exchange: "NASDAQ", Sector: "媒体"},
	{Symbol: "TME", Name: "Tencent Music", NameCN: "腾讯音乐", Exchange: "NYSE", Sector: "媒体"},
	{Symbol: "NTES", Name: "NetEase Inc.", NameCN: "网易", Exchange: "NASDAQ", Sector: "游戏"},
}

// 全球主要指数
var globalIndices = []models.GlobalIndex{
	// 美国
	{Code: "DJI", Name: "Dow Jones Industrial Average", NameCN: "道琼斯工业指数", Region: "america", Country: "us"},
	{Code: "SPX", Name: "S&P 500", NameCN: "标普500", Region: "america", Country: "us"},
	{Code: "IXIC", Name: "NASDAQ Composite", NameCN: "纳斯达克综合指数", Region: "america", Country: "us"},
	{Code: "NDX", Name: "NASDAQ 100", NameCN: "纳斯达克100", Region: "america", Country: "us"},
	// 加拿大
	{Code: "TSX", Name: "S&P/TSX Composite", NameCN: "加拿大TSX综合指数", Region: "america", Country: "ca"},
	// 日本
	{Code: "N225", Name: "Nikkei 225", NameCN: "日经225", Region: "asia", Country: "jp"},
	// 中国香港
	{Code: "HSI", Name: "Hang Seng Index", NameCN: "恒生指数", Region: "asia", Country: "hk"},
	{Code: "HSCEI", Name: "Hang Seng China Enterprises", NameCN: "恒生国企指数", Region: "asia", Country: "hk"},
	// 韩国
	{Code: "KOSPI", Name: "KOSPI Composite", NameCN: "韩国综合指数", Region: "asia", Country: "kr"},
	// 中国台湾
	{Code: "TWII", Name: "Taiwan Weighted", NameCN: "台湾加权指数", Region: "asia", Country: "tw"},
	// 新加坡
	{Code: "STI", Name: "Straits Times Index", NameCN: "新加坡海峡时报指数", Region: "asia", Country: "sg"},
	// 印度
	{Code: "SENSEX", Name: "BSE SENSEX", NameCN: "印度孟买SENSEX", Region: "asia", Country: "in"},
	{Code: "NIFTY", Name: "Nifty 50", NameCN: "印度Nifty50", Region: "asia", Country: "in"},
	// 英国
	{Code: "FTSE", Name: "FTSE 100", NameCN: "英国富时100", Region: "europe", Country: "uk"},
	// 德国
	{Code: "GDAXI", Name: "DAX", NameCN: "德国DAX40", Region: "europe", Country: "de"},
	// 法国
	{Code: "FCHI", Name: "CAC 40", NameCN: "法国CAC40", Region: "europe", Country: "fr"},
	// 欧洲（多国）
	{Code: "STOXX50E", Name: "Euro Stoxx 50", NameCN: "欧洲斯托克50", Region: "europe", Country: "eu"},
	{Code: "AEX", Name: "AEX Index", NameCN: "荷兰AEX指数", Region: "europe", Country: "nl"},
	{Code: "IBEX", Name: "IBEX 35", NameCN: "西班牙IBEX35", Region: "europe", Country: "es"},
	// 澳大利亚
	{Code: "AXJO", Name: "S&P/ASX 200", NameCN: "澳大利亚ASX200", Region: "oceania", Country: "au"},
	{Code: "NZ50", Name: "S&P/NZX 50", NameCN: "新西兰NZX50", Region: "oceania", Country: "nz"},
}

// 热门港股
var popularHKStocks = []models.HKStock{
	{Code: "00700", Name: "Tencent Holdings", NameCN: "腾讯控股", Lot: 100},
	{Code: "09988", Name: "Alibaba Group", NameCN: "阿里巴巴-SW", Lot: 100},
	{Code: "03690", Name: "Meituan", NameCN: "美团-W", Lot: 100},
	{Code: "09618", Name: "JD.com", NameCN: "京东集团-SW", Lot: 50},
	{Code: "01810", Name: "Xiaomi Corporation", NameCN: "小米集团-W", Lot: 200},
	{Code: "09888", Name: "Baidu", NameCN: "百度集团-SW", Lot: 10},
	{Code: "00941", Name: "China Mobile", NameCN: "中国移动", Lot: 500},
	{Code: "00939", Name: "CCB", NameCN: "建设银行", Lot: 1000},
	{Code: "01398", Name: "ICBC", NameCN: "工商银行", Lot: 1000},
	{Code: "03988", Name: "Bank of China", NameCN: "中国银行", Lot: 1000},
	{Code: "00005", Name: "HSBC Holdings", NameCN: "汇丰控股", Lot: 400},
	{Code: "02318", Name: "Ping An Insurance", NameCN: "中国平安", Lot: 500},
	{Code: "00883", Name: "CNOOC", NameCN: "中国海洋石油", Lot: 1000},
	{Code: "00857", Name: "PetroChina", NameCN: "中国石油股份", Lot: 2000},
	{Code: "02628", Name: "China Life", NameCN: "中国人寿", Lot: 1000},
	{Code: "09999", Name: "NetEase", NameCN: "网易-S", Lot: 10},
	{Code: "09961", Name: "Trip.com", NameCN: "携程集团-S", Lot: 10},
	{Code: "01024", Name: "Kuaishou", NameCN: "快手-W", Lot: 100},
	{Code: "02015", Name: "Li Auto", NameCN: "理想汽车-W", Lot: 50},
	{Code: "09866", Name: "NIO", NameCN: "蔚来-SW", Lot: 10},
}

// GetPopularUSStocks 获取热门美股列表
func (api *GlobalMarketAPI) GetPopularUSStocks() []models.USStock {
	return popularUSStocks
}

// GetPopularHKStocks 获取热门港股列表
func (api *GlobalMarketAPI) GetPopularHKStocks() []models.HKStock {
	return popularHKStocks
}

// GetGlobalIndicesList 获取全球指数列表
func (api *GlobalMarketAPI) GetGlobalIndicesList() []models.GlobalIndex {
	return globalIndices
}

// GetUSStockPrice 获取美股实时行情（新浪接口）
func (api *GlobalMarketAPI) GetUSStockPrice(symbols []string) (map[string]*models.USStockPrice, error) {
	result := make(map[string]*models.USStockPrice)

	if len(symbols) == 0 {
		return result, nil
	}

	// 构建新浪美股代码
	var sinaCodeList []string
	for _, symbol := range symbols {
		sinaCodeList = append(sinaCodeList, "gb_"+strings.ToLower(symbol))
	}

	url := fmt.Sprintf("https://hq.sinajs.cn/list=%s", strings.Join(sinaCodeList, ","))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	api.rm.SetRequestHeaders(req, "https://finance.sina.com.cn/")

	resp, err := api.rm.DoRequestWithRateLimit("sina.com.cn", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 解析响应
	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		price := api.parseSinaUSStockLine(line)
		if price != nil {
			result[price.Symbol] = price
		}
	}

	return result, nil
}

// parseSinaUSStockLine 解析新浪美股行情数据
func (api *GlobalMarketAPI) parseSinaUSStockLine(line string) *models.USStockPrice {
	// 格式: var hq_str_gb_aapl="苹果,..."
	re := regexp.MustCompile(`var hq_str_gb_([a-z.]+)="(.+)"`)
	matches := re.FindStringSubmatch(line)
	if len(matches) < 3 {
		return nil
	}

	symbol := strings.ToUpper(matches[1])
	data := matches[2]
	if data == "" {
		return nil
	}

	parts := strings.Split(data, ",")
	if len(parts) < 20 {
		return nil
	}

	price := &models.USStockPrice{
		Symbol:        symbol,
		NameCN:        parts[0],
		Price:         parseFloat(parts[1]),
		ChangePercent: parseFloat(parts[2]),
		UpdateTime:    parts[3],
		Open:          parseFloat(parts[5]),
		High:          parseFloat(parts[6]),
		Low:           parseFloat(parts[7]),
		PreClose:      parseFloat(parts[26]),
	}

	// 计算涨跌额
	if price.PreClose > 0 {
		price.Change = price.Price - price.PreClose
	}

	// 查找英文名称和交易所
	for _, stock := range popularUSStocks {
		if stock.Symbol == symbol {
			price.Name = stock.Name
			price.Exchange = stock.Exchange
			break
		}
	}

	return price
}

// GetHKStockPrice 获取港股实时行情（新浪接口）
func (api *GlobalMarketAPI) GetHKStockPrice(codes []string) (map[string]*models.HKStockPrice, error) {
	result := make(map[string]*models.HKStockPrice)

	if len(codes) == 0 {
		return result, nil
	}

	// 构建新浪港股代码
	var sinaCodeList []string
	for _, code := range codes {
		sinaCodeList = append(sinaCodeList, "rt_hk"+code)
	}

	url := fmt.Sprintf("https://hq.sinajs.cn/list=%s", strings.Join(sinaCodeList, ","))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	api.rm.SetRequestHeaders(req, "https://finance.sina.com.cn/")

	resp, err := api.rm.DoRequestWithRateLimit("sina.com.cn", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 解析响应
	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		price := api.parseSinaHKStockLine(line)
		if price != nil {
			result[price.Code] = price
		}
	}

	return result, nil
}

// parseSinaHKStockLine 解析新浪港股行情数据
func (api *GlobalMarketAPI) parseSinaHKStockLine(line string) *models.HKStockPrice {
	// 格式: var hq_str_rt_hk00700="腾讯控股,..."
	re := regexp.MustCompile(`var hq_str_rt_hk(\d+)="(.+)"`)
	matches := re.FindStringSubmatch(line)
	if len(matches) < 3 {
		return nil
	}

	code := matches[1]
	data := matches[2]
	if data == "" {
		return nil
	}

	parts := strings.Split(data, ",")
	if len(parts) < 15 {
		return nil
	}

	price := &models.HKStockPrice{
		Code:          code,
		Name:          parts[1],
		Open:          parseFloat(parts[2]),
		PreClose:      parseFloat(parts[3]),
		High:          parseFloat(parts[4]),
		Low:           parseFloat(parts[5]),
		Price:         parseFloat(parts[6]),
		Change:        parseFloat(parts[7]),
		ChangePercent: parseFloat(parts[8]),
		Volume:        parseInt64(parts[12]),
		Amount:        parseFloat(parts[11]),
		UpdateTime:    time.Now().Format("15:04:05"),
	}

	return price
}

// GetGlobalIndices 获取全球指数行情（多数据源轮询）
func (api *GlobalMarketAPI) GetGlobalIndices() ([]models.GlobalIndex, error) {
	// 缓存检查
	cacheKey := "global_indices"
	if cached, ok := api.rm.GetCache(cacheKey); ok {
		return cached.([]models.GlobalIndex), nil
	}

	// 使用多数据源管理器获取数据
	msm := GetMultiSourceManager()
	indexData, _, err := msm.GetGlobalIndicesWithFallback()

	// 创建结果副本
	result := make([]models.GlobalIndex, len(globalIndices))
	copy(result, globalIndices)

	if err != nil || len(indexData) == 0 {
		// 所有数据源都失败，返回默认数据
		return api.getDefaultIndices(), nil
	}

	// 更新指数数据
	now := time.Now().Format("15:04:05")
	for i := range result {
		if data, ok := indexData[result[i].Code]; ok {
			result[i].Price = data.Price
			result[i].Change = data.Change
			result[i].ChangePercent = data.ChangePercent
			result[i].UpdateTime = now
		}
	}

	// 缓存60秒
	api.rm.SetCache(cacheKey, result, 60*time.Second)

	return result, nil
}

// getDefaultIndices 返回默认指数数据（当API失败时）
func (api *GlobalMarketAPI) getDefaultIndices() []models.GlobalIndex {
	result := make([]models.GlobalIndex, len(globalIndices))
	copy(result, globalIndices)
	now := time.Now().Format("15:04:05")
	for i := range result {
		result[i].UpdateTime = now
	}
	return result
}

// SearchUSStock 搜索美股
func (api *GlobalMarketAPI) SearchUSStock(keyword string) ([]models.USStock, error) {
	keyword = strings.ToUpper(keyword)
	var result []models.USStock

	for _, stock := range popularUSStocks {
		if strings.Contains(strings.ToUpper(stock.Symbol), keyword) ||
			strings.Contains(strings.ToUpper(stock.Name), keyword) ||
			strings.Contains(stock.NameCN, keyword) {
			result = append(result, stock)
		}
	}

	return result, nil
}

// SearchHKStock 搜索港股
func (api *GlobalMarketAPI) SearchHKStock(keyword string) ([]models.HKStock, error) {
	keyword = strings.ToUpper(keyword)
	var result []models.HKStock

	for _, stock := range popularHKStocks {
		if strings.Contains(stock.Code, keyword) ||
			strings.Contains(strings.ToUpper(stock.Name), keyword) ||
			strings.Contains(stock.NameCN, keyword) {
			result = append(result, stock)
		}
	}

	return result, nil
}

// GetUSStockPriceFromEastmoney 从东方财富获取美股行情（备用接口）
func (api *GlobalMarketAPI) GetUSStockPriceFromEastmoney(symbols []string) (map[string]*models.USStockPrice, error) {
	result := make(map[string]*models.USStockPrice)

	for _, symbol := range symbols {
		price, err := api.getUSStockFromEastmoney(symbol)
		if err != nil {
			continue
		}
		result[symbol] = price
	}

	return result, nil
}

// getUSStockFromEastmoney 从东方财富获取单只美股行情
func (api *GlobalMarketAPI) getUSStockFromEastmoney(symbol string) (*models.USStockPrice, error) {
	url := fmt.Sprintf("https://push2.eastmoney.com/api/qt/stock/get?secid=105.%s&fields=f43,f44,f45,f46,f47,f48,f57,f58,f60,f169,f170", symbol)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	api.rm.SetRequestHeaders(req, "https://quote.eastmoney.com/")

	resp, err := api.rm.DoRequestWithRateLimit("eastmoney.com", req)
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
			F43  float64 `json:"f43"`  // 最新价*1000
			F44  float64 `json:"f44"`  // 最高价*1000
			F45  float64 `json:"f45"`  // 最低价*1000
			F46  float64 `json:"f46"`  // 开盘价*1000
			F47  int64   `json:"f47"`  // 成交量
			F48  float64 `json:"f48"`  // 成交额
			F57  string  `json:"f57"`  // 代码
			F58  string  `json:"f58"`  // 名称
			F60  float64 `json:"f60"`  // 昨收*1000
			F169 float64 `json:"f169"` // 涨跌额*1000
			F170 float64 `json:"f170"` // 涨跌幅*100
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	return &models.USStockPrice{
		Symbol:        data.Data.F57,
		Name:          data.Data.F58,
		Price:         data.Data.F43 / 1000,
		High:          data.Data.F44 / 1000,
		Low:           data.Data.F45 / 1000,
		Open:          data.Data.F46 / 1000,
		PreClose:      data.Data.F60 / 1000,
		Change:        data.Data.F169 / 1000,
		ChangePercent: data.Data.F170 / 100,
		Volume:        data.Data.F47,
		Amount:        data.Data.F48,
		UpdateTime:    time.Now().Format("15:04:05"),
	}, nil
}

// GetGlobalNews 获取国际财经新闻（按国家/地区）
func (api *GlobalMarketAPI) GetGlobalNews(country string) ([]models.NewsItem, error) {
	// 缓存检查
	cacheKey := "global_news_" + country
	if cached, ok := api.rm.GetCache(cacheKey); ok {
		return cached.([]models.NewsItem), nil
	}

	// 根据国家获取对应的新闻分类
	columnID := api.getNewsColumnByCountry(country)

	url := fmt.Sprintf("https://np-listapi.eastmoney.com/comm/web/getNewsByColumns?client=web&biz=web_news_col&column=%s&order=1&needInteractData=0&page_index=1&page_size=20", columnID)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return api.getDefaultGlobalNews(country), nil
	}
	req.Header.Set("Referer", "https://www.eastmoney.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := api.rm.DoRequestWithRateLimit("eastmoney.com", req)
	if err != nil {
		return api.getDefaultGlobalNews(country), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return api.getDefaultGlobalNews(country), nil
	}

	var result struct {
		Data struct {
			List []struct {
				Code      string `json:"code"`
				Title     string `json:"title"`
				ShowTime  string `json:"showTime"`
				Digest    string `json:"digest"`
				MediaName string `json:"mediaName"`
			} `json:"list"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return api.getDefaultGlobalNews(country), nil
	}

	var news []models.NewsItem
	for i, item := range result.Data.List {
		importance := "normal"
		if i < 3 {
			importance = "high"
		} else if i < 8 {
			importance = "medium"
		}

		content := item.Digest
		if content == "" {
			content = item.Title
		}

		news = append(news, models.NewsItem{
			ID:         int64(i + 1),
			Title:      item.Title,
			Content:    content,
			Time:       item.ShowTime,
			Source:     item.MediaName,
			Importance: importance,
		})
	}

	if len(news) == 0 {
		return api.getDefaultGlobalNews(country), nil
	}

	// 缓存5分钟
	api.rm.SetCache(cacheKey, news, 5*time.Minute)

	return news, nil
}

// getNewsColumnByCountry 根据国家获取新闻分类ID
func (api *GlobalMarketAPI) getNewsColumnByCountry(country string) string {
	// 东方财富新闻分类
	// 350 - 美股新闻
	// 351 - 港股新闻
	// 352 - 全球市场
	// 102 - 财经要闻
	switch country {
	case "us":
		return "350" // 美股新闻
	case "hk":
		return "351" // 港股新闻
	case "jp", "kr", "tw", "sg", "in", "au":
		return "352" // 全球市场（亚太）
	case "uk", "de", "fr", "ca":
		return "352" // 全球市场（欧美）
	default:
		return "352" // 全球市场
	}
}

// getDefaultGlobalNews 返回默认国际新闻
func (api *GlobalMarketAPI) getDefaultGlobalNews(country string) []models.NewsItem {
	now := time.Now().Format("2006-01-02 15:04:05")
	countryName := api.getCountryName(country)

	return []models.NewsItem{
		{ID: 1, Title: countryName + "股市早盘动态", Content: countryName + "股市今日开盘表现平稳，主要指数小幅波动。", Time: now, Source: "国际财经", Importance: "high"},
		{ID: 2, Title: "全球市场关注美联储动向", Content: "投资者密切关注美联储货币政策走向，全球股市受此影响波动。", Time: now, Source: "国际财经", Importance: "high"},
		{ID: 3, Title: countryName + "经济数据发布", Content: countryName + "最新经济数据显示，经济运行总体平稳。", Time: now, Source: "国际财经", Importance: "medium"},
		{ID: 4, Title: "国际油价走势分析", Content: "国际原油价格受地缘政治因素影响，近期波动加剧。", Time: now, Source: "国际财经", Importance: "medium"},
		{ID: 5, Title: "全球科技股表现", Content: "全球科技股近期表现分化，AI概念股持续受到关注。", Time: now, Source: "国际财经", Importance: "normal"},
	}
}

// getCountryName 获取国家中文名称
func (api *GlobalMarketAPI) getCountryName(country string) string {
	names := map[string]string{
		"us": "美国",
		"jp": "日本",
		"kr": "韩国",
		"hk": "香港",
		"tw": "台湾",
		"uk": "英国",
		"de": "德国",
		"fr": "法国",
		"au": "澳大利亚",
		"in": "印度",
		"sg": "新加坡",
		"ca": "加拿大",
	}
	if name, ok := names[country]; ok {
		return name
	}
	return "国际"
}
