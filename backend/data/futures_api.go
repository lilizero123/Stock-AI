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

	"stock-ai/backend/models"
)

// FuturesAPI 期货数据API
type FuturesAPI struct {
	rm           *RequestManager
	futuresIndex int        // 期货数据源轮询索引
	futuresMu    sync.Mutex // 保护索引的互斥锁
}

// NewFuturesAPI 创建期货API实例
func NewFuturesAPI() *FuturesAPI {
	return &FuturesAPI{
		rm: GetRequestManager(),
	}
}

// 期货交易所代码映射
var futuresExchangeMap = map[string]string{
	"SHFE": "上期所",
	"DCE":  "大商所",
	"CZCE": "郑商所",
	"CFFEX": "中金所",
	"INE":  "能源中心",
}

// 主要期货品种列表
var mainFuturesProducts = []models.FuturesProduct{
	// 上期所 SHFE
	{Code: "AU", Name: "黄金", Exchange: "SHFE", Unit: "1000克/手", Margin: "8%"},
	{Code: "AG", Name: "白银", Exchange: "SHFE", Unit: "15千克/手", Margin: "9%"},
	{Code: "CU", Name: "铜", Exchange: "SHFE", Unit: "5吨/手", Margin: "10%"},
	{Code: "AL", Name: "铝", Exchange: "SHFE", Unit: "5吨/手", Margin: "10%"},
	{Code: "ZN", Name: "锌", Exchange: "SHFE", Unit: "5吨/手", Margin: "10%"},
	{Code: "RB", Name: "螺纹钢", Exchange: "SHFE", Unit: "10吨/手", Margin: "10%"},
	{Code: "HC", Name: "热轧卷板", Exchange: "SHFE", Unit: "10吨/手", Margin: "10%"},
	{Code: "RU", Name: "橡胶", Exchange: "SHFE", Unit: "10吨/手", Margin: "12%"},
	{Code: "FU", Name: "燃料油", Exchange: "SHFE", Unit: "10吨/手", Margin: "10%"},
	{Code: "NI", Name: "镍", Exchange: "SHFE", Unit: "1吨/手", Margin: "12%"},
	{Code: "SN", Name: "锡", Exchange: "SHFE", Unit: "1吨/手", Margin: "12%"},
	// 大商所 DCE
	{Code: "M", Name: "豆粕", Exchange: "DCE", Unit: "10吨/手", Margin: "8%"},
	{Code: "Y", Name: "豆油", Exchange: "DCE", Unit: "10吨/手", Margin: "8%"},
	{Code: "A", Name: "豆一", Exchange: "DCE", Unit: "10吨/手", Margin: "8%"},
	{Code: "P", Name: "棕榈油", Exchange: "DCE", Unit: "10吨/手", Margin: "8%"},
	{Code: "C", Name: "玉米", Exchange: "DCE", Unit: "10吨/手", Margin: "8%"},
	{Code: "I", Name: "铁矿石", Exchange: "DCE", Unit: "100吨/手", Margin: "12%"},
	{Code: "JM", Name: "焦煤", Exchange: "DCE", Unit: "60吨/手", Margin: "12%"},
	{Code: "J", Name: "焦炭", Exchange: "DCE", Unit: "100吨/手", Margin: "12%"},
	{Code: "PP", Name: "聚丙烯", Exchange: "DCE", Unit: "5吨/手", Margin: "8%"},
	{Code: "L", Name: "塑料", Exchange: "DCE", Unit: "5吨/手", Margin: "8%"},
	{Code: "EG", Name: "乙二醇", Exchange: "DCE", Unit: "10吨/手", Margin: "8%"},
	{Code: "PG", Name: "液化石油气", Exchange: "DCE", Unit: "20吨/手", Margin: "10%"},
	// 郑商所 CZCE
	{Code: "CF", Name: "棉花", Exchange: "CZCE", Unit: "5吨/手", Margin: "8%"},
	{Code: "SR", Name: "白糖", Exchange: "CZCE", Unit: "10吨/手", Margin: "8%"},
	{Code: "TA", Name: "PTA", Exchange: "CZCE", Unit: "5吨/手", Margin: "8%"},
	{Code: "MA", Name: "甲醇", Exchange: "CZCE", Unit: "10吨/手", Margin: "8%"},
	{Code: "OI", Name: "菜油", Exchange: "CZCE", Unit: "10吨/手", Margin: "8%"},
	{Code: "RM", Name: "菜粕", Exchange: "CZCE", Unit: "10吨/手", Margin: "8%"},
	{Code: "FG", Name: "玻璃", Exchange: "CZCE", Unit: "20吨/手", Margin: "10%"},
	{Code: "SA", Name: "纯碱", Exchange: "CZCE", Unit: "20吨/手", Margin: "10%"},
	{Code: "AP", Name: "苹果", Exchange: "CZCE", Unit: "10吨/手", Margin: "10%"},
	// 中金所 CFFEX
	{Code: "IF", Name: "沪深300", Exchange: "CFFEX", Unit: "300元/点", Margin: "12%"},
	{Code: "IC", Name: "中证500", Exchange: "CFFEX", Unit: "200元/点", Margin: "12%"},
	{Code: "IH", Name: "上证50", Exchange: "CFFEX", Unit: "300元/点", Margin: "12%"},
	{Code: "IM", Name: "中证1000", Exchange: "CFFEX", Unit: "200元/点", Margin: "12%"},
	{Code: "T", Name: "10年期国债", Exchange: "CFFEX", Unit: "10000元/张", Margin: "3%"},
	{Code: "TF", Name: "5年期国债", Exchange: "CFFEX", Unit: "10000元/张", Margin: "2%"},
	{Code: "TS", Name: "2年期国债", Exchange: "CFFEX", Unit: "20000元/张", Margin: "1%"},
	// 能源中心 INE
	{Code: "SC", Name: "原油", Exchange: "INE", Unit: "1000桶/手", Margin: "12%"},
	{Code: "NR", Name: "20号胶", Exchange: "INE", Unit: "10吨/手", Margin: "12%"},
	{Code: "LU", Name: "低硫燃料油", Exchange: "INE", Unit: "10吨/手", Margin: "12%"},
	{Code: "BC", Name: "国际铜", Exchange: "INE", Unit: "5吨/手", Margin: "10%"},
}

// GetFuturesProducts 获取期货品种列表
func (api *FuturesAPI) GetFuturesProducts() []models.FuturesProduct {
	return mainFuturesProducts
}

// GetFuturesPrice 获取期货实时行情（新浪接口）
func (api *FuturesAPI) GetFuturesPrice(codes []string) (map[string]*models.FuturesPrice, error) {
	result := make(map[string]*models.FuturesPrice)

	// 构建新浪期货代码
	var sinaCodeList []string
	for _, code := range codes {
		sinaCode := api.toSinaFuturesCode(code)
		if sinaCode != "" {
			sinaCodeList = append(sinaCodeList, sinaCode)
		}
	}

	if len(sinaCodeList) == 0 {
		return result, nil
	}

	// 新浪期货行情接口
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
		price := api.parseSinaFuturesLine(line)
		if price != nil {
			result[price.Code] = price
		}
	}

	return result, nil
}

// toSinaFuturesCode 转换为新浪期货代码
func (api *FuturesAPI) toSinaFuturesCode(code string) string {
	code = strings.ToUpper(code)
	// 新浪期货代码格式：品种代码+月份，如 AU2406 -> AU2406
	// 需要添加交易所前缀
	for _, product := range mainFuturesProducts {
		if strings.HasPrefix(code, product.Code) {
			switch product.Exchange {
			case "SHFE":
				return "nf_" + code // 上期所
			case "DCE":
				return "nf_" + code // 大商所
			case "CZCE":
				return "nf_" + code // 郑商所
			case "CFFEX":
				return "CFF_" + code // 中金所
			case "INE":
				return "nf_" + code // 能源中心
			}
		}
	}
	return ""
}

// parseSinaFuturesLine 解析新浪期货行情数据
func (api *FuturesAPI) parseSinaFuturesLine(line string) *models.FuturesPrice {
	// 格式: var hq_str_nf_AU2406="黄金2406,..."
	re := regexp.MustCompile(`var hq_str_(?:nf_|CFF_)([A-Za-z]+\d+)="(.+)"`)
	matches := re.FindStringSubmatch(line)
	if len(matches) < 3 {
		return nil
	}

	code := strings.ToUpper(matches[1])
	data := matches[2]
	if data == "" {
		return nil
	}

	parts := strings.Split(data, ",")
	if len(parts) < 15 {
		return nil
	}

	// 解析数据
	price := &models.FuturesPrice{
		Code:       code,
		Name:       parts[0],
		Open:       parseFloat(parts[2]),
		High:       parseFloat(parts[3]),
		Low:        parseFloat(parts[4]),
		PreClose:   parseFloat(parts[5]),
		PreSettle:  parseFloat(parts[6]),
		Price:      parseFloat(parts[8]),
		Settle:     parseFloat(parts[9]),
		Volume:     parseInt64(parts[14]),
		UpdateTime: time.Now().Format("15:04:05"),
	}

	// 计算涨跌
	if price.PreSettle > 0 {
		price.Change = price.Price - price.PreSettle
		price.ChangePercent = (price.Change / price.PreSettle) * 100
	}

	// 获取交易所
	for _, product := range mainFuturesProducts {
		if strings.HasPrefix(code, product.Code) {
			price.Exchange = product.Exchange
			break
		}
	}

	return price
}

// 期货数据源列表
var futuresSources = []string{"eastmoney", "sina", "tencent", "hexun", "netease", "baidu", "xueqiu"}

// GetMainContracts 获取主力合约列表（循环轮询多个数据源）
func (api *FuturesAPI) GetMainContracts() ([]models.FuturesPrice, error) {
	// 缓存检查
	cacheKey := "futures_main_contracts"
	if cached, ok := api.rm.GetCache(cacheKey); ok {
		return cached.([]models.FuturesPrice), nil
	}

	// 获取当前数据源索引
	api.futuresMu.Lock()
	currentIndex := api.futuresIndex
	api.futuresIndex = (api.futuresIndex + 1) % len(futuresSources)
	api.futuresMu.Unlock()

	// 尝试所有数据源，从当前索引开始
	var lastErr error
	for i := 0; i < len(futuresSources); i++ {
		sourceIndex := (currentIndex + i) % len(futuresSources)
		source := futuresSources[sourceIndex]

		var result []models.FuturesPrice
		var err error

		switch source {
		case "eastmoney":
			result, err = api.getMainContractsFromEastMoney()
		case "sina":
			result, err = api.getMainContractsFromSina()
		case "tencent":
			result, err = api.getMainContractsFromTencent()
		case "hexun":
			result, err = api.getMainContractsFromHexun()
		case "netease":
			result, err = api.getMainContractsFromNetease()
		case "baidu":
			result, err = api.getMainContractsFromBaidu()
		case "xueqiu":
			result, err = api.getMainContractsFromXueqiu()
		}

		if err == nil && len(result) > 0 {
			api.rm.SetCache(cacheKey, result, 30*time.Second)
			return result, nil
		}
		lastErr = err
	}

	return nil, fmt.Errorf("所有期货数据源均失败: %v", lastErr)
}

// getMainContractsFromEastMoney 从东方财富获取主力合约
func (api *FuturesAPI) getMainContractsFromEastMoney() ([]models.FuturesPrice, error) {
	// 使用东方财富期货列表接口 m:113(上期所) m:114(大商所) m:115(郑商所)
	url := "https://push2.eastmoney.com/api/qt/clist/get?pn=1&pz=100&po=1&np=1&fltt=2&invt=2&fid=f3&fs=m:113,m:114,m:115&fields=f1,f2,f3,f4,f5,f6,f7,f12,f13,f14,f15,f16,f17,f18"

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
			Diff []struct {
				F2  float64 `json:"f2"`  // 最新价
				F3  float64 `json:"f3"`  // 涨跌幅
				F4  float64 `json:"f4"`  // 涨跌额
				F5  int64   `json:"f5"`  // 成交量
				F6  float64 `json:"f6"`  // 成交额
				F12 string  `json:"f12"` // 代码
				F13 int     `json:"f13"` // 市场代码
				F14 string  `json:"f14"` // 名称
				F15 float64 `json:"f15"` // 最高
				F16 float64 `json:"f16"` // 最低
				F17 float64 `json:"f17"` // 开盘
				F18 float64 `json:"f18"` // 昨收
			} `json:"diff"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	// 市场代码到交易所的映射
	marketToExchange := map[int]string{
		113: "SHFE",  // 上期所
		114: "DCE",   // 大商所
		115: "CZCE",  // 郑商所
		142: "CFFEX", // 中金所
		225: "INE",   // 能源中心
	}

	var result []models.FuturesPrice
	for _, item := range data.Data.Diff {
		// 只取主力合约（代码以M结尾或包含主连）
		code := item.F12
		name := item.F14
		if !strings.HasSuffix(strings.ToUpper(code), "M") && !strings.Contains(name, "主连") {
			continue
		}

		exchange := marketToExchange[item.F13]
		if exchange == "" {
			exchange = "UNKNOWN"
		}

		result = append(result, models.FuturesPrice{
			Code:          code,
			Name:          name,
			Price:         item.F2,
			Change:        item.F4,
			ChangePercent: item.F3,
			Open:          item.F17,
			High:          item.F15,
			Low:           item.F16,
			PreClose:      item.F18,
			Volume:        item.F5,
			Amount:        item.F6,
			Exchange:      exchange,
			UpdateTime:    time.Now().Format("15:04:05"),
		})
	}

	return result, nil
}

// getMainContractsFromSina 从新浪获取主力合约（备用）
func (api *FuturesAPI) getMainContractsFromSina() ([]models.FuturesPrice, error) {
	// 构建主力合约代码列表
	var codes []string
	for _, product := range mainFuturesProducts {
		// 使用品种代码+0表示主力合约
		codes = append(codes, product.Code+"0")
	}

	// 获取价格
	prices, err := api.GetFuturesPrice(codes)
	if err != nil {
		return nil, err
	}

	var result []models.FuturesPrice
	for _, price := range prices {
		if price != nil && price.Price > 0 {
			result = append(result, *price)
		}
	}

	return result, nil
}

// getMainContractsFromTencent 从腾讯获取主力合约
func (api *FuturesAPI) getMainContractsFromTencent() ([]models.FuturesPrice, error) {
	// 腾讯期货代码映射（主力合约）
	tencentCodes := map[string]string{
		"AU0": "nf_AU0",  // 黄金
		"AG0": "nf_AG0",  // 白银
		"CU0": "nf_CU0",  // 铜
		"AL0": "nf_AL0",  // 铝
		"ZN0": "nf_ZN0",  // 锌
		"RB0": "nf_RB0",  // 螺纹钢
		"HC0": "nf_HC0",  // 热轧卷板
		"RU0": "nf_RU0",  // 橡胶
		"FU0": "nf_FU0",  // 燃料油
		"NI0": "nf_NI0",  // 镍
		"M0":  "nf_M0",   // 豆粕
		"Y0":  "nf_Y0",   // 豆油
		"P0":  "nf_P0",   // 棕榈油
		"C0":  "nf_C0",   // 玉米
		"I0":  "nf_I0",   // 铁矿石
		"JM0": "nf_JM0",  // 焦煤
		"J0":  "nf_J0",   // 焦炭
		"PP0": "nf_PP0",  // 聚丙烯
		"L0":  "nf_L0",   // 塑料
		"CF0": "nf_CF0",  // 棉花
		"SR0": "nf_SR0",  // 白糖
		"TA0": "nf_TA0",  // PTA
		"MA0": "nf_MA0",  // 甲醇
		"OI0": "nf_OI0",  // 菜油
		"FG0": "nf_FG0",  // 玻璃
		"SA0": "nf_SA0",  // 纯碱
		"SC0": "nf_SC0",  // 原油
	}

	var codeList []string
	for _, code := range tencentCodes {
		codeList = append(codeList, code)
	}

	url := fmt.Sprintf("https://qt.gtimg.cn/q=%s", strings.Join(codeList, ","))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	api.rm.SetRequestHeaders(req, "https://gu.qq.com/")

	resp, err := api.rm.DoRequestWithRateLimit("qq.com", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result []models.FuturesPrice

	// 解析腾讯返回数据
	lines := strings.Split(string(body), ";")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// 提取代码
		for productCode, tencentCode := range tencentCodes {
			if !strings.Contains(line, tencentCode) {
				continue
			}

			// 提取数据
			start := strings.Index(line, "\"")
			end := strings.LastIndex(line, "\"")
			if start == -1 || end == -1 || start >= end {
				continue
			}

			data := line[start+1 : end]
			parts := strings.Split(data, "~")
			if len(parts) < 10 {
				continue
			}

			price := models.FuturesPrice{
				Code:       productCode,
				Name:       parts[1],
				Price:      parseFloat(parts[3]),
				PreClose:   parseFloat(parts[4]),
				Open:       parseFloat(parts[5]),
				High:       parseFloat(parts[33]),
				Low:        parseFloat(parts[34]),
				UpdateTime: time.Now().Format("15:04:05"),
			}

			if price.PreClose > 0 {
				price.Change = price.Price - price.PreClose
				price.ChangePercent = (price.Change / price.PreClose) * 100
			}

			// 获取交易所
			for _, product := range mainFuturesProducts {
				if strings.HasPrefix(productCode, product.Code) {
					price.Exchange = product.Exchange
					break
				}
			}

			if price.Price > 0 {
				result = append(result, price)
			}
			break
		}
	}

	return result, nil
}

// getMainContractsFromHexun 从和讯获取主力合约
func (api *FuturesAPI) getMainContractsFromHexun() ([]models.FuturesPrice, error) {
	// 和讯期货接口
	url := "https://api.hexun.com/futures/quotelist?type=main"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	api.rm.SetRequestHeaders(req, "https://futures.hexun.com/")

	resp, err := api.rm.DoRequestWithRateLimit("hexun.com", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data struct {
		Data []struct {
			Code          string  `json:"code"`
			Name          string  `json:"name"`
			Price         float64 `json:"price"`
			Change        float64 `json:"change"`
			ChangePercent float64 `json:"changepercent"`
			Open          float64 `json:"open"`
			High          float64 `json:"high"`
			Low           float64 `json:"low"`
			PreClose      float64 `json:"preclose"`
			Volume        int64   `json:"volume"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	var result []models.FuturesPrice
	for _, item := range data.Data {
		price := models.FuturesPrice{
			Code:          item.Code,
			Name:          item.Name,
			Price:         item.Price,
			Change:        item.Change,
			ChangePercent: item.ChangePercent,
			Open:          item.Open,
			High:          item.High,
			Low:           item.Low,
			PreClose:      item.PreClose,
			Volume:        item.Volume,
			UpdateTime:    time.Now().Format("15:04:05"),
		}

		// 获取交易所
		for _, product := range mainFuturesProducts {
			if strings.HasPrefix(item.Code, product.Code) {
				price.Exchange = product.Exchange
				break
			}
		}

		if price.Price > 0 {
			result = append(result, price)
		}
	}

	return result, nil
}

// getMainContractsFromNetease 从网易获取主力合约
func (api *FuturesAPI) getMainContractsFromNetease() ([]models.FuturesPrice, error) {
	// 网易期货接口 - 获取主力合约
	// 构建期货代码列表
	var codes []string
	for _, product := range mainFuturesProducts {
		codes = append(codes, "nf_"+product.Code+"0")
	}

	url := fmt.Sprintf("https://api.money.126.net/data/feed/%s?callback=cb", strings.Join(codes, ","))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	api.rm.SetRequestHeaders(req, "https://money.163.com/")

	resp, err := api.rm.DoRequestWithRateLimit("163.com", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 解析JSONP: cb({...})
	bodyStr := string(body)
	start := strings.Index(bodyStr, "(")
	end := strings.LastIndex(bodyStr, ")")
	if start == -1 || end == -1 || start >= end {
		return nil, fmt.Errorf("invalid response format")
	}

	jsonData := bodyStr[start+1 : end]

	var data map[string]struct {
		Name    string  `json:"name"`
		Price   float64 `json:"price"`
		Percent float64 `json:"percent"`
		Updown  float64 `json:"updown"`
		Open    float64 `json:"open"`
		High    float64 `json:"high"`
		Low     float64 `json:"low"`
		Yestclose float64 `json:"yestclose"`
		Volume  int64   `json:"volume"`
	}

	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return nil, err
	}

	var result []models.FuturesPrice
	for code, item := range data {
		// 提取品种代码，如 nf_AU0 -> AU0
		productCode := strings.TrimPrefix(code, "nf_")

		price := models.FuturesPrice{
			Code:          productCode,
			Name:          item.Name,
			Price:         item.Price,
			Change:        item.Updown,
			ChangePercent: item.Percent * 100,
			Open:          item.Open,
			High:          item.High,
			Low:           item.Low,
			PreClose:      item.Yestclose,
			Volume:        item.Volume,
			UpdateTime:    time.Now().Format("15:04:05"),
		}

		// 获取交易所
		for _, product := range mainFuturesProducts {
			if strings.HasPrefix(productCode, product.Code) {
				price.Exchange = product.Exchange
				break
			}
		}

		if price.Price > 0 {
			result = append(result, price)
		}
	}

	return result, nil
}

// getMainContractsFromBaidu 从百度获取主力合约
func (api *FuturesAPI) getMainContractsFromBaidu() ([]models.FuturesPrice, error) {
	// 百度股市通期货接口
	url := "https://gushitong.baidu.com/opendata?resource_id=5352&query=期货&type=futures&market=futures"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	api.rm.SetRequestHeaders(req, "https://gushitong.baidu.com/")

	resp, err := api.rm.DoRequestWithRateLimit("baidu.com", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var data struct {
		Result struct {
			List []struct {
				Code          string `json:"code"`
				Name          string `json:"name"`
				Price         string `json:"price"`
				Change        string `json:"change"`
				ChangePercent string `json:"changepercent"`
				Open          string `json:"open"`
				High          string `json:"high"`
				Low           string `json:"low"`
				PreClose      string `json:"preclose"`
				Volume        string `json:"volume"`
			} `json:"list"`
		} `json:"Result"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	var result []models.FuturesPrice
	for _, item := range data.Result.List {
		price := models.FuturesPrice{
			Code:          item.Code,
			Name:          item.Name,
			Price:         parseFloat(item.Price),
			Change:        parseFloat(item.Change),
			ChangePercent: parseFloat(item.ChangePercent),
			Open:          parseFloat(item.Open),
			High:          parseFloat(item.High),
			Low:           parseFloat(item.Low),
			PreClose:      parseFloat(item.PreClose),
			Volume:        parseInt64(item.Volume),
			UpdateTime:    time.Now().Format("15:04:05"),
		}

		// 获取交易所
		for _, product := range mainFuturesProducts {
			if strings.HasPrefix(item.Code, product.Code) {
				price.Exchange = product.Exchange
				break
			}
		}

		if price.Price > 0 {
			result = append(result, price)
		}
	}

	return result, nil
}

// getMainContractsFromXueqiu 从雪球获取主力合约
func (api *FuturesAPI) getMainContractsFromXueqiu() ([]models.FuturesPrice, error) {
	// 雪球期货接口 - 构建主力合约代码
	var symbols []string
	for _, product := range mainFuturesProducts {
		// 雪球期货代码格式
		symbols = append(symbols, product.Code+"0")
	}

	url := fmt.Sprintf("https://stock.xueqiu.com/v5/stock/batch/quote.json?symbol=%s", strings.Join(symbols, ","))

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	api.rm.SetRequestHeaders(req, "https://xueqiu.com/")
	// 雪球需要cookie，使用一个通用的访客cookie
	req.Header.Set("Cookie", "xq_a_token=; xqat=; xq_r_token=; xq_id_token=; u=;")

	resp, err := api.rm.DoRequestWithRateLimit("xueqiu.com", req)
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
			Items []struct {
				Quote struct {
					Symbol  string  `json:"symbol"`
					Name    string  `json:"name"`
					Current float64 `json:"current"`
					Chg     float64 `json:"chg"`
					Percent float64 `json:"percent"`
					Open    float64 `json:"open"`
					High    float64 `json:"high"`
					Low     float64 `json:"low"`
					PreClose float64 `json:"last_close"`
					Volume  int64   `json:"volume"`
				} `json:"quote"`
			} `json:"items"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	var result []models.FuturesPrice
	for _, item := range data.Data.Items {
		price := models.FuturesPrice{
			Code:          item.Quote.Symbol,
			Name:          item.Quote.Name,
			Price:         item.Quote.Current,
			Change:        item.Quote.Chg,
			ChangePercent: item.Quote.Percent,
			Open:          item.Quote.Open,
			High:          item.Quote.High,
			Low:           item.Quote.Low,
			PreClose:      item.Quote.PreClose,
			Volume:        item.Quote.Volume,
			UpdateTime:    time.Now().Format("15:04:05"),
		}

		// 获取交易所
		for _, product := range mainFuturesProducts {
			if strings.HasPrefix(item.Quote.Symbol, product.Code) {
				price.Exchange = product.Exchange
				break
			}
		}

		if price.Price > 0 {
			result = append(result, price)
		}
	}

	return result, nil
}

// SearchFutures 搜索期货合约
func (api *FuturesAPI) SearchFutures(keyword string) ([]models.Futures, error) {
	keyword = strings.ToUpper(keyword)
	var result []models.Futures

	for _, product := range mainFuturesProducts {
		if strings.Contains(product.Code, keyword) || strings.Contains(product.Name, keyword) {
			result = append(result, models.Futures{
				Code:     product.Code,
				Name:     product.Name,
				Exchange: product.Exchange,
				Product:  product.Code,
			})
		}
	}

	return result, nil
}

// parseInt64 解析int64
func parseInt64(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	val, _ := strconv.ParseInt(s, 10, 64)
	return val
}
