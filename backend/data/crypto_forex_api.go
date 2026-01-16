package data

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"stock-ai/backend/models"
)

// ForexAPI 外汇数据API
type CryptoForexAPI struct {
	rm         *RequestManager
	forexIndex int // 外汇数据源轮询索引
	forexMu    sync.Mutex
}

// NewCryptoForexAPI 创建外汇API实例
func NewCryptoForexAPI() *CryptoForexAPI {
	return &CryptoForexAPI{
		rm: GetRequestManager(),
	}
}

// 主要外汇货币对
var mainForexPairs = []models.ForexRate{
	{Pair: "USDCNY", Name: "美元/人民币"},
	{Pair: "EURUSD", Name: "欧元/美元"},
	{Pair: "GBPUSD", Name: "英镑/美元"},
	{Pair: "USDJPY", Name: "美元/日元"},
	{Pair: "AUDUSD", Name: "澳元/美元"},
	{Pair: "USDCAD", Name: "美元/加元"},
	{Pair: "USDCHF", Name: "美元/瑞郎"},
	{Pair: "NZDUSD", Name: "纽元/美元"},
	{Pair: "EURCNY", Name: "欧元/人民币"},
	{Pair: "GBPCNY", Name: "英镑/人民币"},
	{Pair: "JPYCNY", Name: "日元/人民币"},
	{Pair: "HKDCNY", Name: "港币/人民币"},
}

// GetMainForexPairs 获取主要外汇货币对列表
func (api *CryptoForexAPI) GetMainForexPairs() []models.ForexRate {
	return mainForexPairs
}

// 外汇数据源列表
var forexSources = []string{"sina", "eastmoney", "tencent", "hexun", "netease", "baidu", "xueqiu"}

// GetForexRates 获取外汇汇率（循环轮询多个数据源）
func (api *CryptoForexAPI) GetForexRates() ([]models.ForexRate, error) {
	// 缓存检查
	cacheKey := "forex_rates"
	if cached, ok := api.rm.GetCache(cacheKey); ok {
		return cached.([]models.ForexRate), nil
	}

	// 获取当前数据源索引
	api.forexMu.Lock()
	currentIndex := api.forexIndex
	api.forexIndex = (api.forexIndex + 1) % len(forexSources)
	api.forexMu.Unlock()

	// 尝试所有数据源，从当前索引开始
	var lastErr error
	for i := 0; i < len(forexSources); i++ {
		sourceIndex := (currentIndex + i) % len(forexSources)
		source := forexSources[sourceIndex]

		var result []models.ForexRate
		var err error

		switch source {
		case "sina":
			result, err = api.getForexRatesFromSina()
		case "eastmoney":
			result, err = api.getForexRatesFromEastMoney()
		case "tencent":
			result, err = api.getForexRatesFromTencent()
		case "hexun":
			result, err = api.getForexRatesFromHexun()
		case "netease":
			result, err = api.getForexRatesFromNetease()
		case "baidu":
			result, err = api.getForexRatesFromBaidu()
		case "xueqiu":
			result, err = api.getForexRatesFromXueqiu()
		}

		if err == nil && len(result) > 0 && result[0].Rate > 0 {
			api.rm.SetCache(cacheKey, result, 60*time.Second)
			return result, nil
		}
		lastErr = err
	}

	// 都失败了，返回基础数据
	return mainForexPairs, lastErr
}

// getForexRatesFromSina 从新浪获取外汇汇率
func (api *CryptoForexAPI) getForexRatesFromSina() ([]models.ForexRate, error) {
	// 新浪外汇代码映射
	forexCodeMap := map[string]string{
		"USDCNY": "fx_susdcny",
		"EURUSD": "fx_seurusd",
		"GBPUSD": "fx_sgbpusd",
		"USDJPY": "fx_susdjpy",
		"AUDUSD": "fx_saudusd",
		"USDCAD": "fx_susdcad",
		"USDCHF": "fx_susdchf",
		"NZDUSD": "fx_snzdusd",
		"EURCNY": "fx_seurcny",
		"GBPCNY": "fx_sgbpcny",
		"JPYCNY": "fx_sjpycny",
		"HKDCNY": "fx_shkdcny",
	}

	var sinaCodeList []string
	for _, pair := range mainForexPairs {
		if sinaCode, ok := forexCodeMap[pair.Pair]; ok {
			sinaCodeList = append(sinaCodeList, sinaCode)
		}
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
	result := make([]models.ForexRate, len(mainForexPairs))
	copy(result, mainForexPairs)

	lines := strings.Split(string(body), "\n")
	for _, line := range lines {
		if line == "" {
			continue
		}
		api.parseSinaForexLine(line, result, forexCodeMap)
	}

	return result, nil
}

// getForexRatesFromEastMoney 从东方财富获取外汇汇率
func (api *CryptoForexAPI) getForexRatesFromEastMoney() ([]models.ForexRate, error) {
	// 东方财富外汇接口
	url := "https://push2.eastmoney.com/api/qt/clist/get?pn=1&pz=50&po=1&np=1&fltt=2&invt=2&fid=f3&fs=m:119,m:120&fields=f1,f2,f3,f4,f12,f13,f14"

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
				F12 string  `json:"f12"` // 代码
				F14 string  `json:"f14"` // 名称
			} `json:"diff"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	// 东方财富代码到标准代码的映射
	codeMap := map[string]string{
		"USDCNH": "USDCNY",
		"EURUSD": "EURUSD",
		"GBPUSD": "GBPUSD",
		"USDJPY": "USDJPY",
		"AUDUSD": "AUDUSD",
		"USDCAD": "USDCAD",
		"USDCHF": "USDCHF",
		"NZDUSD": "NZDUSD",
	}

	result := make([]models.ForexRate, len(mainForexPairs))
	copy(result, mainForexPairs)

	for _, item := range data.Data.Diff {
		standardCode, ok := codeMap[item.F12]
		if !ok {
			continue
		}

		for i := range result {
			if result[i].Pair == standardCode {
				result[i].Rate = item.F2
				result[i].Change = item.F4
				result[i].ChangePercent = item.F3
				result[i].UpdateTime = time.Now().Format("15:04:05")
				break
			}
		}
	}

	return result, nil
}

// getForexRatesFromTencent 从腾讯财经获取外汇汇率
func (api *CryptoForexAPI) getForexRatesFromTencent() ([]models.ForexRate, error) {
	// 腾讯外汇代码映射
	tencentCodeMap := map[string]string{
		"USDCNY": "fx_susdcnh",
		"EURUSD": "fx_seurusd",
		"GBPUSD": "fx_sgbpusd",
		"USDJPY": "fx_susdjpy",
		"AUDUSD": "fx_saudusd",
		"USDCAD": "fx_susdcad",
		"USDCHF": "fx_susdchf",
		"NZDUSD": "fx_snzdusd",
		"EURCNY": "fx_seurcnh",
		"GBPCNY": "fx_sgbpcnh",
		"JPYCNY": "fx_sjpycnh",
		"HKDCNY": "fx_shkdcnh",
	}

	var codeList []string
	for _, pair := range mainForexPairs {
		if code, ok := tencentCodeMap[pair.Pair]; ok {
			codeList = append(codeList, code)
		}
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

	result := make([]models.ForexRate, len(mainForexPairs))
	copy(result, mainForexPairs)

	// 解析腾讯返回数据，格式: v_fx_susdcnh="1~美元离岸人民币~USDCNH~7.2650~..."
	lines := strings.Split(string(body), ";")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		for pair, tencentCode := range tencentCodeMap {
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
			if len(parts) < 5 {
				continue
			}

			// 更新汇率数据
			for i := range result {
				if result[i].Pair == pair {
					result[i].Rate = parseFloat(parts[3])
					if len(parts) > 31 {
						result[i].Change = parseFloat(parts[31])
						result[i].ChangePercent = parseFloat(parts[32])
					}
					if len(parts) > 33 {
						result[i].High = parseFloat(parts[33])
						result[i].Low = parseFloat(parts[34])
					}
					result[i].UpdateTime = time.Now().Format("15:04:05")
					break
				}
			}
			break
		}
	}

	return result, nil
}

// getForexRatesFromHexun 从和讯获取外汇汇率
func (api *CryptoForexAPI) getForexRatesFromHexun() ([]models.ForexRate, error) {
	// 和讯外汇接口
	url := "https://api.hexun.com/forex/quotelist?code=USDCNY,EURUSD,GBPUSD,USDJPY,AUDUSD,USDCAD,USDCHF,NZDUSD,EURCNY,GBPCNY,JPYCNY,HKDCNY"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	api.rm.SetRequestHeaders(req, "https://forex.hexun.com/")

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
			High          float64 `json:"high"`
			Low           float64 `json:"low"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	result := make([]models.ForexRate, len(mainForexPairs))
	copy(result, mainForexPairs)

	for _, item := range data.Data {
		for i := range result {
			if result[i].Pair == item.Code {
				result[i].Rate = item.Price
				result[i].Change = item.Change
				result[i].ChangePercent = item.ChangePercent
				result[i].High = item.High
				result[i].Low = item.Low
				result[i].UpdateTime = time.Now().Format("15:04:05")
				break
			}
		}
	}

	return result, nil
}

// getForexRatesFromNetease 从网易获取外汇汇率
func (api *CryptoForexAPI) getForexRatesFromNetease() ([]models.ForexRate, error) {
	// 网易外汇接口
	url := "https://api.money.126.net/data/feed/FX_SUSDCNY,FX_SEURUSD,FX_SGBPUSD,FX_SUSDJPY,FX_SAUDUSD,FX_SUSDCAD,FX_SUSDCHF,FX_SNZDUSD,FX_SEURCNY,FX_SGBPCNY,FX_SJPYCNY,FX_SHKDCNY?callback=cb"

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
		Price   float64 `json:"price"`
		Percent float64 `json:"percent"`
		Updown  float64 `json:"updown"`
		High    float64 `json:"high"`
		Low     float64 `json:"low"`
		Name    string  `json:"name"`
	}

	if err := json.Unmarshal([]byte(jsonData), &data); err != nil {
		return nil, err
	}

	// 网易代码到标准代码的映射
	codeMap := map[string]string{
		"FX_SUSDCNY": "USDCNY",
		"FX_SEURUSD": "EURUSD",
		"FX_SGBPUSD": "GBPUSD",
		"FX_SUSDJPY": "USDJPY",
		"FX_SAUDUSD": "AUDUSD",
		"FX_SUSDCAD": "USDCAD",
		"FX_SUSDCHF": "USDCHF",
		"FX_SNZDUSD": "NZDUSD",
		"FX_SEURCNY": "EURCNY",
		"FX_SGBPCNY": "GBPCNY",
		"FX_SJPYCNY": "JPYCNY",
		"FX_SHKDCNY": "HKDCNY",
	}

	result := make([]models.ForexRate, len(mainForexPairs))
	copy(result, mainForexPairs)

	for neteaseCode, item := range data {
		standardCode, ok := codeMap[neteaseCode]
		if !ok {
			continue
		}

		for i := range result {
			if result[i].Pair == standardCode {
				result[i].Rate = item.Price
				result[i].Change = item.Updown
				result[i].ChangePercent = item.Percent * 100
				result[i].High = item.High
				result[i].Low = item.Low
				result[i].UpdateTime = time.Now().Format("15:04:05")
				break
			}
		}
	}

	return result, nil
}

// getForexRatesFromBaidu 从百度获取外汇汇率
func (api *CryptoForexAPI) getForexRatesFromBaidu() ([]models.ForexRate, error) {
	// 百度股市通外汇接口
	url := "https://gushitong.baidu.com/opendata?resource_id=5352&query=外汇&code=USDCNY,EURUSD,GBPUSD,USDJPY,AUDUSD,USDCAD,USDCHF,NZDUSD&market=forex"

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
				High          string `json:"high"`
				Low           string `json:"low"`
			} `json:"list"`
		} `json:"Result"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	result := make([]models.ForexRate, len(mainForexPairs))
	copy(result, mainForexPairs)

	for _, item := range data.Result.List {
		for i := range result {
			if result[i].Pair == item.Code {
				result[i].Rate = parseFloat(item.Price)
				result[i].Change = parseFloat(item.Change)
				result[i].ChangePercent = parseFloat(item.ChangePercent)
				result[i].High = parseFloat(item.High)
				result[i].Low = parseFloat(item.Low)
				result[i].UpdateTime = time.Now().Format("15:04:05")
				break
			}
		}
	}

	return result, nil
}

// getForexRatesFromXueqiu 从雪球获取外汇汇率
func (api *CryptoForexAPI) getForexRatesFromXueqiu() ([]models.ForexRate, error) {
	// 雪球外汇接口
	url := "https://stock.xueqiu.com/v5/stock/batch/quote.json?symbol=USDCNY,EURUSD,GBPUSD,USDJPY,AUDUSD,USDCAD,USDCHF,NZDUSD,EURCNY,GBPCNY,JPYCNY,HKDCNY"

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
					High    float64 `json:"high"`
					Low     float64 `json:"low"`
				} `json:"quote"`
			} `json:"items"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	result := make([]models.ForexRate, len(mainForexPairs))
	copy(result, mainForexPairs)

	for _, item := range data.Data.Items {
		for i := range result {
			if result[i].Pair == item.Quote.Symbol {
				result[i].Rate = item.Quote.Current
				result[i].Change = item.Quote.Chg
				result[i].ChangePercent = item.Quote.Percent
				result[i].High = item.Quote.High
				result[i].Low = item.Quote.Low
				result[i].UpdateTime = time.Now().Format("15:04:05")
				break
			}
		}
	}

	return result, nil
}

// parseSinaForexLine 解析新浪外汇行情数据
func (api *CryptoForexAPI) parseSinaForexLine(line string, rates []models.ForexRate, codeMap map[string]string) {
	// 格式: var hq_str_fx_susdcny="时间,买入价,卖出价,最新价,成交量,最高,最低,昨收,名称,涨跌,涨跌额,涨跌幅,..."
	for pair, sinaCode := range codeMap {
		if !strings.Contains(line, sinaCode) {
			continue
		}

		// 提取数据
		start := strings.Index(line, "\"")
		end := strings.LastIndex(line, "\"")
		if start == -1 || end == -1 || start >= end {
			continue
		}

		data := line[start+1 : end]
		parts := strings.Split(data, ",")
		if len(parts) < 8 {
			continue
		}

		// 更新汇率数据
		// 新浪外汇格式: 时间(0),买入价(1),卖出价(2),最新价(3),成交量(4),最高(5),最低(6),昨收(7),名称(8),...
		for i := range rates {
			if rates[i].Pair == pair {
				rates[i].Rate = parseFloat(parts[3])     // 最新价
				rates[i].High = parseFloat(parts[5])     // 最高
				rates[i].Low = parseFloat(parts[6])      // 最低
				preClose := parseFloat(parts[7])         // 昨收
				if preClose > 0 {
					rates[i].Change = rates[i].Rate - preClose
					rates[i].ChangePercent = (rates[i].Change / preClose) * 100
				}
				rates[i].UpdateTime = time.Now().Format("15:04:05")
				break
			}
		}
		break
	}
}
