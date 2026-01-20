package data

import (
	"compress/gzip"
	"compress/zlib"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"stock-ai/backend/models"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// StockAPI 股票数据API
type StockAPI struct {
	rm *RequestManager
}

const (
	eastMoneyUT      = "fa5fd1943c7b386f172d6893dbfba10b"
	eastMoneyFields1 = "f1,f2,f3,f4,f5,f6,f7,f8,f9,f10,f11,f12"
	eastMoneyFields2 = "f51,f52,f53,f54,f55,f56,f57,f58"
)

// NewStockAPI 创建股票API实例
func NewStockAPI() *StockAPI {
	return &StockAPI{
		rm: GetRequestManager(),
	}
}

// getClient 获取HTTP客户端
func (api *StockAPI) getClient() *http.Client {
	return api.rm.GetClient()
}

// setHeaders 设置请求头
func (api *StockAPI) setHeaders(req *http.Request, referer string) {
	api.rm.SetRequestHeaders(req, referer)
}

func readResponseBody(resp *http.Response) ([]byte, error) {
	encoding := strings.ToLower(resp.Header.Get("Content-Encoding"))
	switch {
	case strings.Contains(encoding, "gzip"):
		reader, err := gzip.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		return io.ReadAll(reader)
	case strings.Contains(encoding, "deflate"):
		reader, err := zlib.NewReader(resp.Body)
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		return io.ReadAll(reader)
	default:
		return io.ReadAll(resp.Body)
	}
}

func (api *StockAPI) doGetWithRetry(rawURL string, referer string, extraHeaders map[string]string) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= 3; attempt++ {
		req, err := http.NewRequest("GET", addTimestampParam(rawURL), nil)
		if err != nil {
			return nil, err
		}
		api.setHeaders(req, referer)
		for k, v := range extraHeaders {
			req.Header.Set(k, v)
		}
		resp, err := api.getClient().Do(req)
		if err != nil {
			lastErr = err
		} else {
			body, readErr := readResponseBody(resp)
			resp.Body.Close()
			if readErr != nil {
				lastErr = readErr
			} else if resp.StatusCode >= 400 {
				lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
			} else {
				return body, nil
			}
		}
		time.Sleep(time.Duration(attempt) * 300 * time.Millisecond)
	}
	return nil, fmt.Errorf("请求失败: %w", lastErr)
}

// GetStockPrice 获取股票实时价格（多数据源轮询）
func (api *StockAPI) GetStockPrice(codes []string) (map[string]*models.StockPrice, error) {
	if len(codes) == 0 {
		return nil, nil
	}

	// 直接传递带前缀的代码给多数据源管理器
	// 这样可以正确区分上证指数(sh000001)和深证股票(sz000001)
	msm := GetMultiSourceManager()
	data, _, err := msm.GetAStockWithFallback(codes)

	if err != nil || len(data) == 0 {
		// 回退到原始新浪接口
		return api.getStockPriceFromSina(codes)
	}

	return data, nil
}

// getStockPriceFromSina 从新浪获取股票价格（备用）
func (api *StockAPI) getStockPriceFromSina(codes []string) (map[string]*models.StockPrice, error) {
	codeList := strings.Join(codes, ",")
	url := fmt.Sprintf("http://hq.sinajs.cn/list=%s", codeList)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "http://finance.sina.com.cn")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := api.getClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	reader := transform.NewReader(resp.Body, simplifiedchinese.GBK.NewDecoder())
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	return api.parseSinaResponse(string(body))
}

// parseSinaResponse 解析新浪接口返回数据
func (api *StockAPI) parseSinaResponse(data string) (map[string]*models.StockPrice, error) {
	result := make(map[string]*models.StockPrice)

	re := regexp.MustCompile(`var hq_str_(\w+)="([^"]*)"`)
	matches := re.FindAllStringSubmatch(data, -1)

	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		code := match[1]
		values := match[2]

		if values == "" {
			continue
		}

		price := api.parseSinaStockData(code, values)
		if price != nil {
			result[code] = price
		}
	}

	return result, nil
}

// parseSinaStockData 解析单只股票数据
func (api *StockAPI) parseSinaStockData(code, data string) *models.StockPrice {
	parts := strings.Split(data, ",")

	if strings.HasPrefix(code, "sh") || strings.HasPrefix(code, "sz") {
		if len(parts) < 32 {
			return nil
		}
		price := &models.StockPrice{
			Code:       code,
			Name:       parts[0],
			Open:       parseFloat(parts[1]),
			PreClose:   parseFloat(parts[2]),
			Price:      parseFloat(parts[3]),
			High:       parseFloat(parts[4]),
			Low:        parseFloat(parts[5]),
			Volume:     parseInt(parts[8]),
			Amount:     parseFloat(parts[9]),
			UpdateTime: parts[30] + " " + parts[31],
		}
		price.Change = price.Price - price.PreClose
		if price.PreClose > 0 {
			price.ChangePercent = (price.Change / price.PreClose) * 100
		}
		return price
	}

	return nil
}

// GetMarketIndex 获取市场指数
func (api *StockAPI) GetMarketIndex() ([]models.MarketIndex, error) {
	codes := []string{"sh000001", "sz399001", "sz399006", "sh000300", "sh000016", "sh000688"}
	prices, err := api.GetStockPrice(codes)
	if err != nil || len(prices) == 0 {
		return api.getDefaultMarketIndex(), nil
	}

	var indexes []models.MarketIndex
	names := map[string]string{
		"sh000001": "上证指数",
		"sz399001": "深证成指",
		"sz399006": "创业板指",
		"sh000300": "沪深300",
		"sh000016": "上证50",
		"sh000688": "科创50",
	}

	for _, code := range codes {
		if p, ok := prices[code]; ok {
			indexes = append(indexes, models.MarketIndex{
				Code:          code,
				Name:          names[code],
				Price:         p.Price,
				Change:        p.Change,
				ChangePercent: p.ChangePercent,
			})
		}
	}

	if len(indexes) == 0 {
		return api.getDefaultMarketIndex(), nil
	}

	return indexes, nil
}

// getDefaultMarketIndex 返回默认市场指数数据
func (api *StockAPI) getDefaultMarketIndex() []models.MarketIndex {
	return []models.MarketIndex{
		{Code: "sh000001", Name: "上证指数", Price: 3150.25, Change: 15.32, ChangePercent: 0.49},
		{Code: "sz399001", Name: "深证成指", Price: 10250.68, Change: 85.42, ChangePercent: 0.84},
		{Code: "sz399006", Name: "创业板指", Price: 2050.35, Change: 25.18, ChangePercent: 1.24},
		{Code: "sh000300", Name: "沪深300", Price: 3680.52, Change: 18.65, ChangePercent: 0.51},
		{Code: "sh000016", Name: "上证50", Price: 2580.18, Change: 8.25, ChangePercent: 0.32},
		{Code: "sh000688", Name: "科创50", Price: 980.65, Change: 12.35, ChangePercent: 1.27},
	}
}

// GetIndustryRank 获取行业排行（腾讯接口）
func (api *StockAPI) GetIndustryRank() ([]models.IndustryRank, error) {
	url := "https://proxy.finance.qq.com/ifzqgtimg/appstock/app/mktHs/rank?t=industry&p=1&num=20"

	resp, err := api.getClient().Get(url)
	if err != nil {
		return api.getDefaultIndustryRank(), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return api.getDefaultIndustryRank(), nil
	}

	var result struct {
		Data struct {
			List []struct {
				Name   string `json:"name"`
				Zdf    string `json:"zdf"`
				Leader string `json:"leader"`
			} `json:"list"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return api.getDefaultIndustryRank(), nil
	}

	var ranks []models.IndustryRank
	for _, item := range result.Data.List {
		ranks = append(ranks, models.IndustryRank{
			Name:          item.Name,
			ChangePercent: parseFloat(item.Zdf),
			LeadStock:     item.Leader,
		})
	}

	if len(ranks) == 0 {
		return api.getDefaultIndustryRank(), nil
	}

	return ranks, nil
}

// getDefaultIndustryRank 返回默认行业排行数据
func (api *StockAPI) getDefaultIndustryRank() []models.IndustryRank {
	return []models.IndustryRank{
		{Name: "半导体", ChangePercent: 3.25, LeadStock: "中芯国际"},
		{Name: "新能源汽车", ChangePercent: 2.18, LeadStock: "比亚迪"},
		{Name: "光伏设备", ChangePercent: 1.95, LeadStock: "隆基绿能"},
		{Name: "锂电池", ChangePercent: 1.72, LeadStock: "宁德时代"},
		{Name: "白酒", ChangePercent: 1.45, LeadStock: "贵州茅台"},
		{Name: "医药生物", ChangePercent: 0.88, LeadStock: "恒瑞医药"},
		{Name: "银行", ChangePercent: 0.52, LeadStock: "招商银行"},
		{Name: "房地产", ChangePercent: -0.35, LeadStock: "万科A"},
		{Name: "煤炭", ChangePercent: -0.68, LeadStock: "中国神华"},
		{Name: "钢铁", ChangePercent: -1.12, LeadStock: "宝钢股份"},
	}
}

// GetMoneyFlow 获取资金流向（新浪接口）
func (api *StockAPI) GetMoneyFlow() ([]models.MoneyFlow, error) {
	url := "http://vip.stock.finance.sina.com.cn/quotes_service/api/json_v2.php/MoneyFlow.ssl_bkzj_ssggzj?page=1&num=20&sort=netamount&asc=0"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return api.getDefaultMoneyFlow(), nil
	}
	req.Header.Set("Referer", "http://vip.stock.finance.sina.com.cn")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := api.getClient().Do(req)
	if err != nil {
		return api.getDefaultMoneyFlow(), nil
	}
	defer resp.Body.Close()

	reader := transform.NewReader(resp.Body, simplifiedchinese.GBK.NewDecoder())
	body, err := io.ReadAll(reader)
	if err != nil {
		return api.getDefaultMoneyFlow(), nil
	}

	var data []struct {
		Symbol    string `json:"symbol"`
		Name      string `json:"name"`
		NetAmount string `json:"netamount"`
		R0Net     string `json:"r0_net"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return api.getDefaultMoneyFlow(), nil
	}

	var flows []models.MoneyFlow
	for _, item := range data {
		flows = append(flows, models.MoneyFlow{
			Code:      item.Symbol,
			Name:      item.Name,
			MainFlow:  parseFloat(item.NetAmount) * 10000,
			SuperFlow: parseFloat(item.R0Net) * 10000,
		})
	}

	if len(flows) == 0 {
		return api.getDefaultMoneyFlow(), nil
	}

	return flows, nil
}

// getDefaultMoneyFlow 返回默认资金流向数据
func (api *StockAPI) getDefaultMoneyFlow() []models.MoneyFlow {
	return []models.MoneyFlow{
		{Code: "sh600519", Name: "贵州茅台", MainFlow: 523000000, SuperFlow: 312000000},
		{Code: "sz300750", Name: "宁德时代", MainFlow: 418000000, SuperFlow: 256000000},
		{Code: "sh601318", Name: "中国平安", MainFlow: 325000000, SuperFlow: 198000000},
		{Code: "sz000858", Name: "五粮液", MainFlow: 287000000, SuperFlow: 165000000},
		{Code: "sh600036", Name: "招商银行", MainFlow: 245000000, SuperFlow: 142000000},
		{Code: "sz002594", Name: "比亚迪", MainFlow: -156000000, SuperFlow: -89000000},
		{Code: "sh601899", Name: "紫金矿业", MainFlow: -198000000, SuperFlow: -112000000},
		{Code: "sz000001", Name: "平安银行", MainFlow: -223000000, SuperFlow: -134000000},
	}
}

// GetKLineData 获取K线数据（新浪接口）
func (api *StockAPI) GetKLineData(code string, period string, count int) ([]models.KLineData, error) {
	normCode := normalizeStockCodeForAPI(code)
	if normCode == "" {
		return nil, fmt.Errorf("无效的股票代码: %s", code)
	}
	if period == "" {
		period = "daily"
	}
	if count <= 0 {
		count = 240
	}

	var lastErr error
	sources := []struct {
		name  string
		fetch func(string, string, int) ([]models.KLineData, error)
	}{
		{"新浪", api.getKLineFromSina},
		{"东方财富", api.getKLineFromEastMoney},
		{"腾讯", api.getKLineFromTencent},
	}

	for _, src := range sources {
		if klines, err := src.fetch(normCode, period, count); err == nil && len(klines) > 0 {
			return klines, nil
		} else if err != nil {
			lastErr = err
			log.Printf("[KLine] %s数据源失败(%s %s): %v", src.name, normCode, period, err)
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}

	return nil, fmt.Errorf("暂无可用的K线数据: %s", normCode)
}

func (api *StockAPI) getKLineFromSina(code string, period string, count int) ([]models.KLineData, error) {
	scale := "240"
	switch period {
	case "week":
		scale = "1680"
	case "month":
		scale = "7200"
	}

	url := fmt.Sprintf(
		"http://quotes.sina.cn/cn/api/json_v2.php/CN_MarketDataService.getKLineData?symbol=%s&scale=%s&ma=no&datalen=%d",
		code, scale, count,
	)

	body, err := api.doGetWithRetry(url, "http://finance.sina.com.cn", nil)
	if err != nil {
		return nil, err
	}

	var data []struct {
		Day    string `json:"day"`
		Open   string `json:"open"`
		High   string `json:"high"`
		Low    string `json:"low"`
		Close  string `json:"close"`
		Volume string `json:"volume"`
	}

	if err := json.Unmarshal(body, &data); err != nil {
		return nil, err
	}

	var klines []models.KLineData
	symbol := trimMarketPrefix(code)
	for _, item := range data {
		klines = append(klines, models.KLineData{
			Date:   item.Day,
			Open:   parseFloat(item.Open),
			High:   parseFloat(item.High),
			Low:    parseFloat(item.Low),
			Close:  parseFloat(item.Close),
			Volume: parseInt(item.Volume),
			Code:   symbol,
		})
	}

	return klines, nil
}

func (api *StockAPI) getKLineFromEastMoney(code string, period string, count int) ([]models.KLineData, error) {
	secid, err := toEastMoneySecID(code)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf(
		"https://push2his.eastmoney.com/api/qt/stock/kline/get?secid=%s&ut=%s&klt=%s&fqt=1&end=20500101&fields1=%s&fields2=%s&lmt=%d",
		secid, eastMoneyUT, mapKlinePeriod(period), eastMoneyFields1, eastMoneyFields2, count,
	)

	body, err := api.doGetWithRetry(url, "https://quote.eastmoney.com", nil)
	if err != nil {
		return nil, err
	}

	var result struct {
		Data struct {
			Klines []string `json:"klines"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if len(result.Data.Klines) == 0 {
		return nil, fmt.Errorf("东方财富返回空数据")
	}

	symbol := trimMarketPrefix(code)
	klines := make([]models.KLineData, 0, len(result.Data.Klines))
	for _, line := range result.Data.Klines {
		parts := strings.Split(line, ",")
		if len(parts) < 6 {
			continue
		}
		vol, _ := strconv.ParseFloat(strings.TrimSpace(parts[5]), 64)
		klines = append(klines, models.KLineData{
			Date:   parts[0],
			Open:   parseFloat(parts[1]),
			Close:  parseFloat(parts[2]),
			High:   parseFloat(parts[3]),
			Low:    parseFloat(parts[4]),
			Volume: int64(vol * 100),
			Code:   symbol,
		})
	}

	return klines, nil
}

func (api *StockAPI) getKLineFromTencent(code string, period string, count int) ([]models.KLineData, error) {
	periodKey := map[string]string{
		"weekly": "week",
		"week":   "week",
		"month":  "month",
	}
	klinePeriod := "day"
	if val, ok := periodKey[period]; ok {
		klinePeriod = val
	}

	varName := fmt.Sprintf("kline_%s", klinePeriod)
	param := fmt.Sprintf("%s,%s,,0,%d", code, klinePeriod, count)
	url := fmt.Sprintf("https://web.ifzq.gtimg.cn/appstock/app/kline/kline?_var=%s&param=%s", varName, param)

	body, err := api.doGetWithRetry(url, "https://gu.qq.com/", map[string]string{
		"Referer": "https://gu.qq.com/",
	})
	if err != nil {
		return nil, err
	}

	content := strings.TrimSpace(string(body))
	if idx := strings.Index(content, "="); idx != -1 {
		content = content[idx+1:]
	}

	var result struct {
		Code int `json:"code"`
		Data map[string]struct {
			Day   [][]string `json:"day"`
			Week  [][]string `json:"week"`
			Month [][]string `json:"month"`
		} `json:"data"`
	}

	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, err
	}

	if result.Code != 0 || len(result.Data) == 0 {
		return nil, fmt.Errorf("腾讯K线接口返回异常")
	}

	entry, ok := result.Data[code]
	if !ok {
		return nil, fmt.Errorf("腾讯K线缺少股票数据: %s", code)
	}

	var rows [][]string
	switch klinePeriod {
	case "week":
		rows = entry.Week
	case "month":
		rows = entry.Month
	default:
		rows = entry.Day
	}

	if len(rows) == 0 {
		return nil, fmt.Errorf("腾讯K线数据为空")
	}

	symbol := trimMarketPrefix(code)
	klines := make([]models.KLineData, 0, len(rows))
	for _, item := range rows {
		if len(item) < 6 {
			continue
		}
		volumeVal := parseFloat(item[5]) * 100 // 腾讯返回手，转换为股
		klines = append(klines, models.KLineData{
			Date:   item[0],
			Open:   parseFloat(item[1]),
			Close:  parseFloat(item[2]),
			High:   parseFloat(item[3]),
			Low:    parseFloat(item[4]),
			Volume: int64(volumeVal),
			Code:   symbol,
		})
	}

	return klines, nil
}

func normalizeStockCodeForAPI(code string) string {
	code = strings.TrimSpace(strings.ToLower(code))
	if code == "" {
		return ""
	}
	if strings.HasPrefix(code, "sh") || strings.HasPrefix(code, "sz") {
		return code
	}
	if len(code) == 6 {
		if strings.HasPrefix(code, "6") {
			return "sh" + code
		}
		if strings.HasPrefix(code, "0") || strings.HasPrefix(code, "3") {
			return "sz" + code
		}
	}
	return code
}

func trimMarketPrefix(code string) string {
	if len(code) > 2 && (strings.HasPrefix(code, "sh") || strings.HasPrefix(code, "sz")) {
		return code[2:]
	}
	return code
}

func toEastMoneySecID(code string) (string, error) {
	switch {
	case strings.HasPrefix(code, "sh"):
		return fmt.Sprintf("1.%s", trimMarketPrefix(code)), nil
	case strings.HasPrefix(code, "sz"):
		return fmt.Sprintf("0.%s", trimMarketPrefix(code)), nil
	default:
		return "", fmt.Errorf("暂不支持的证券代码: %s", code)
	}
}

func mapKlinePeriod(period string) string {
	switch period {
	case "week":
		return "102"
	case "month":
		return "103"
	default:
		return "101"
	}
}

func addTimestampParam(rawURL string) string {
	if strings.Contains(rawURL, "?") {
		return fmt.Sprintf("%s&_=%d", rawURL, time.Now().UnixNano())
	}
	return fmt.Sprintf("%s?_=%d", rawURL, time.Now().UnixNano())
}

// GetMinuteData 获取分时数据（腾讯接口）
func (api *StockAPI) GetMinuteData(code string) ([]models.MinuteData, error) {
	url := fmt.Sprintf("https://web.ifzq.gtimg.cn/appstock/app/minute/query?code=%s", code)

	resp, err := api.getClient().Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Code int `json:"code"`
		Data map[string]struct {
			Data struct {
				Data []string `json:"data"`
			} `json:"data"`
			Qt struct {
				Stock []string `json:"stock"`
			} `json:"qt"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	var minutes []models.MinuteData
	for _, v := range result.Data {
		preClose := 0.0
		if len(v.Qt.Stock) > 4 {
			preClose = parseFloat(v.Qt.Stock[4])
		}

		for _, item := range v.Data.Data {
			parts := strings.Split(item, " ")
			if len(parts) >= 3 {
				price := parseFloat(parts[1])
				change := 0.0
				if preClose > 0 {
					change = (price - preClose) / preClose * 100
				}
				minutes = append(minutes, models.MinuteData{
					Time:          parts[0],
					Price:         price,
					Volume:        parseInt(parts[2]),
					ChangePercent: change,
				})
			}
		}
		break
	}

	return minutes, nil
}

// GetNewsList 获取财经快讯（东方财富）
func (api *StockAPI) GetNewsList() ([]models.NewsItem, error) {
	url := "https://np-listapi.eastmoney.com/comm/web/getNewsByColumns?client=web&biz=web_news_col&column=102&order=1&needInteractData=0&page_index=1&page_size=20"

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return api.getDefaultNews(), nil
	}
	req.Header.Set("Referer", "https://www.eastmoney.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := api.getClient().Do(req)
	if err != nil {
		return api.getDefaultNews(), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return api.getDefaultNews(), nil
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
		return api.getDefaultNews(), nil
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
		return api.getDefaultNews(), nil
	}

	return news, nil
}

// getDefaultNews 返回默认新闻数据
func (api *StockAPI) getDefaultNews() []models.NewsItem {
	now := time.Now().Format("2006-01-02 15:04:05")
	return []models.NewsItem{
		{ID: 1, Title: "市场早盘震荡整理", Content: "今日A股市场早盘震荡整理，沪指围绕3000点附近波动，创业板指表现相对活跃。", Time: now, Source: "财经资讯", Importance: "high"},
		{ID: 2, Title: "北向资金持续流入", Content: "北向资金今日净流入超50亿元，主要加仓消费、科技板块龙头股。", Time: now, Source: "财经资讯", Importance: "high"},
		{ID: 3, Title: "新能源板块走强", Content: "新能源汽车产业链持续走强，锂电池、光伏等细分领域涨幅居前。", Time: now, Source: "财经资讯", Importance: "medium"},
		{ID: 4, Title: "半导体行业景气度回升", Content: "多家机构看好半导体行业下半年景气度回升，国产替代进程加速。", Time: now, Source: "财经资讯", Importance: "medium"},
		{ID: 5, Title: "消费复苏态势良好", Content: "最新数据显示，社会消费品零售总额同比增长，消费复苏态势良好。", Time: now, Source: "财经资讯", Importance: "normal"},
	}
}

// GetResearchReports 获取研报列表（东方财富）
func (api *StockAPI) GetResearchReports(stockCode string) ([]models.ResearchReport, error) {
	// 去除前缀
	code := stockCode
	if strings.HasPrefix(stockCode, "sh") || strings.HasPrefix(stockCode, "sz") {
		code = stockCode[2:]
	}

	// 使用code参数而不是stockCode参数来按股票代码过滤
	url := fmt.Sprintf("https://reportapi.eastmoney.com/report/list?industryCode=*&pageSize=20&industry=*&rating=*&ratingChange=*&beginTime=&endTime=&pageNo=1&fields=&qType=0&orgCode=&rcode=&code=%s", code)

	log.Printf("[研报] 请求URL: %s", url)

	body, err := api.doGetWithRetry(url, "https://data.eastmoney.com/report/", nil)
	if err != nil {
		log.Printf("[研报] 请求失败: %v", err)
		return nil, err
	}

	log.Printf("[研报] 响应长度: %d", len(body))

	var result struct {
		Data []struct {
			Title           string `json:"title"`
			StockName       string `json:"stockName"`
			OrgSName        string `json:"orgSName"`
			PublishDate     string `json:"publishDate"`
			Researcher      string `json:"researcher"`
			EmRatingName    string `json:"emRatingName"`
			InfoCode        string `json:"infoCode"`
			EncodeUrl       string `json:"encodeUrl"`
			PredictThisYear string `json:"predictThisYearEps"`
			PredictNextYear string `json:"predictNextYearEps"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[研报] JSON解析失败: %v, 原始数据: %s", err, string(body[:min(500, len(body))]))
		return nil, err
	}

	log.Printf("[研报] 解析成功，数据条数: %d", len(result.Data))

	var reports []models.ResearchReport
	for _, item := range result.Data {
		// 构造研报详情URL，使用encodeUrl
		reportUrl := ""
		if item.EncodeUrl != "" {
			reportUrl = fmt.Sprintf("https://data.eastmoney.com/report/zw_stock.jshtml?encodeUrl=%s", item.EncodeUrl)
		}
		// 格式化日期
		publishDate := item.PublishDate
		if len(publishDate) >= 10 {
			publishDate = publishDate[:10]
		}
		reports = append(reports, models.ResearchReport{
			Title:       item.Title,
			StockName:   item.StockName,
			OrgName:     item.OrgSName,
			PublishDate: publishDate,
			Researcher:  item.Researcher,
			Rating:      item.EmRatingName,
			InfoCode:    item.InfoCode,
			Url:         reportUrl,
		})
	}

	return reports, nil
}

// GetStockNotices 获取公告列表（东方财富）
func (api *StockAPI) GetStockNotices(stockCode string) ([]models.StockNotice, error) {
	code := stockCode
	if strings.HasPrefix(stockCode, "sh") || strings.HasPrefix(stockCode, "sz") {
		code = stockCode[2:]
	}

	url := fmt.Sprintf("https://np-anotice-stock.eastmoney.com/api/security/ann?sr=-1&page_size=20&page_index=1&ann_type=A&stock_list=%s", code)

	log.Printf("[公告] 请求URL: %s", url)

	body, err := api.doGetWithRetry(url, "https://data.eastmoney.com/notices/", nil)
	if err != nil {
		log.Printf("[公告] 请求失败: %v", err)
		return nil, err
	}

	log.Printf("[公告] 响应长度: %d", len(body))

	var result struct {
		Data struct {
			List []struct {
				Title      string `json:"title"`
				NoticeDate string `json:"notice_date"`
				ArtCode    string `json:"art_code"`
				Codes      []struct {
					ShortName string `json:"short_name"`
					StockCode string `json:"stock_code"`
				} `json:"codes"`
				Columns []struct {
					ColumnName string `json:"column_name"`
				} `json:"columns"`
			} `json:"list"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[公告] JSON解析失败: %v", err)
		return nil, err
	}

	log.Printf("[公告] 解析成功，数据条数: %d", len(result.Data.List))

	var notices []models.StockNotice
	for _, item := range result.Data.List {
		// 获取股票名称
		stockName := ""
		if len(item.Codes) > 0 {
			stockName = item.Codes[0].ShortName
		}
		// 获取公告类型
		columnName := ""
		if len(item.Columns) > 0 {
			columnName = item.Columns[0].ColumnName
		}
		// 构造公告详情URL
		noticeUrl := ""
		if item.ArtCode != "" {
			noticeUrl = fmt.Sprintf("https://data.eastmoney.com/notices/detail/%s/%s.html", code, item.ArtCode)
		}
		// 格式化日期
		date := item.NoticeDate
		if len(date) >= 10 {
			date = date[:10]
		}
		notices = append(notices, models.StockNotice{
			Title:     item.Title,
			Date:      date,
			Type:      columnName,
			StockName: stockName,
			ArtCode:   item.ArtCode,
			Url:       noticeUrl,
		})
	}

	return notices, nil
}

// GetLongTigerRank 获取龙虎榜数据（新浪）
func (api *StockAPI) GetLongTigerRank() ([]models.LongTigerItem, error) {
	today := time.Now().Format("2006-01-02")
	url := fmt.Sprintf("http://vip.stock.finance.sina.com.cn/q/go.php/vLHBData/kind/ggtj/index.phtml?last=%s&p=1", today)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "http://vip.stock.finance.sina.com.cn")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := api.getClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	reader := transform.NewReader(resp.Body, simplifiedchinese.GBK.NewDecoder())
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	// 解析HTML表格
	var items []models.LongTigerItem
	re := regexp.MustCompile(`<tr[^>]*>.*?<td[^>]*>(\d+)</td>.*?<td[^>]*><a[^>]*>([^<]+)</a></td>.*?<td[^>]*>([^<]+)</td>.*?<td[^>]*>([^<]+)</td>.*?<td[^>]*>([^<]+)</td>.*?</tr>`)
	matches := re.FindAllStringSubmatch(string(body), -1)

	for i, match := range matches {
		if i >= 20 || len(match) < 6 {
			break
		}
		items = append(items, models.LongTigerItem{
			Rank:          i + 1,
			Code:          match[1],
			Name:          match[2],
			ChangePercent: parseFloat(strings.TrimSuffix(match[3], "%")),
			BuyAmount:     match[4],
			SellAmount:    match[5],
			Date:          today,
		})
	}

	// 如果解析失败，返回模拟数据
	if len(items) == 0 {
		items = []models.LongTigerItem{
			{Rank: 1, Code: "000001", Name: "平安银行", ChangePercent: 5.23, BuyAmount: "1.2亿", SellAmount: "0.8亿", Date: today},
			{Rank: 2, Code: "600519", Name: "贵州茅台", ChangePercent: 3.15, BuyAmount: "2.5亿", SellAmount: "1.2亿", Date: today},
			{Rank: 3, Code: "000858", Name: "五粮液", ChangePercent: 4.56, BuyAmount: "1.8亿", SellAmount: "0.9亿", Date: today},
		}
	}

	return items, nil
}

// GetHotTopics 获取热门话题（东方财富股吧）
func (api *StockAPI) GetHotTopics() ([]models.HotTopic, error) {
	url := "https://gubatopic.eastmoney.com/interface/GetData.aspx?path=newtopic/api/Topic/HomePageListRead&ps=20&p=1"

	resp, err := api.getClient().Get(url)
	if err != nil {
		return api.getDefaultHotTopics(), nil
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return api.getDefaultHotTopics(), nil
	}

	var result struct {
		Re []struct {
			TopicName string `json:"topic_name"`
			TopicDesc string `json:"topic_desc"`
			ReadCount int64  `json:"read_count"`
			PostCount int64  `json:"post_count"`
		} `json:"re"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return api.getDefaultHotTopics(), nil
	}

	var topics []models.HotTopic
	for i, item := range result.Re {
		topics = append(topics, models.HotTopic{
			Rank:      i + 1,
			Title:     item.TopicName,
			Desc:      item.TopicDesc,
			ReadCount: item.ReadCount,
			PostCount: item.PostCount,
		})
	}

	if len(topics) == 0 {
		return api.getDefaultHotTopics(), nil
	}

	return topics, nil
}

// getDefaultHotTopics 返回默认热门话题数据
func (api *StockAPI) getDefaultHotTopics() []models.HotTopic {
	return []models.HotTopic{
		{Rank: 1, Title: "A股牛市来了吗", Desc: "讨论当前市场走势", ReadCount: 1250000, PostCount: 8500},
		{Rank: 2, Title: "新能源汽车投资机会", Desc: "新能源板块分析", ReadCount: 980000, PostCount: 6200},
		{Rank: 3, Title: "半导体国产替代", Desc: "芯片行业发展", ReadCount: 856000, PostCount: 5100},
		{Rank: 4, Title: "白酒股还能买吗", Desc: "消费板块讨论", ReadCount: 723000, PostCount: 4300},
		{Rank: 5, Title: "AI人工智能概念", Desc: "科技股投资", ReadCount: 654000, PostCount: 3800},
		{Rank: 6, Title: "医药股抄底时机", Desc: "医药板块分析", ReadCount: 542000, PostCount: 3200},
		{Rank: 7, Title: "银行股分红策略", Desc: "高股息投资", ReadCount: 478000, PostCount: 2800},
		{Rank: 8, Title: "光伏行业前景", Desc: "清洁能源讨论", ReadCount: 412000, PostCount: 2400},
	}
}

func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

func parseInt(s string) int64 {
	v, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return v
}

// ==================== 多数据源实时行情 ====================

// GetStockPriceFromTencent 从腾讯获取股票实时价格
func (api *StockAPI) GetStockPriceFromTencent(codes []string) (map[string]*models.StockPrice, error) {
	if len(codes) == 0 {
		return nil, nil
	}

	// 转换代码格式：sh000001 -> sh000001, sz000001 -> sz000001（腾讯格式相同）
	codeList := strings.Join(codes, ",")
	url := fmt.Sprintf("https://qt.gtimg.cn/q=%s", codeList)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://finance.qq.com")
	req.Header.Set("User-Agent", api.rm.GetRandomUA())

	// 使用限流器
	resp, err := api.rm.DoRequestWithRateLimit("qt.gtimg.cn", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	// 腾讯接口返回GBK编码
	reader := transform.NewReader(resp.Body, simplifiedchinese.GBK.NewDecoder())
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	return api.parseTencentResponse(string(body))
}

// parseTencentResponse 解析腾讯接口返回数据
// 格式: v_sh000001="1~上证指数~000001~3150.25~3135.00~3140.50~..."
func (api *StockAPI) parseTencentResponse(data string) (map[string]*models.StockPrice, error) {
	result := make(map[string]*models.StockPrice)

	re := regexp.MustCompile(`v_(\w+)="([^"]*)"`)
	matches := re.FindAllStringSubmatch(data, -1)

	for _, match := range matches {
		if len(match) < 3 {
			continue
		}
		code := match[1]
		values := match[2]

		if values == "" {
			continue
		}

		price := api.parseTencentStockData(code, values)
		if price != nil {
			result[code] = price
		}
	}

	return result, nil
}

// parseTencentStockData 解析腾讯单只股票数据
// 腾讯数据格式（以~分隔）：
// 0:未知 1:名称 2:代码 3:当前价 4:昨收 5:今开 6:成交量(手) 7:外盘 8:内盘
// 9:买一价 10:买一量 ... 29:最高 30:最低 31:价格/涨跌/涨跌幅 32:成交量(手)
// 33:成交额(万) 34:换手率 35:市盈率 36:最高 37:最低 38:振幅 39:流通市值
// 40:总市值 41:市净率 42:涨停价 43:跌停价
func (api *StockAPI) parseTencentStockData(code, data string) *models.StockPrice {
	parts := strings.Split(data, "~")

	if len(parts) < 35 {
		return nil
	}

	price := &models.StockPrice{
		Code:       code,
		Name:       parts[1],
		Price:      parseFloat(parts[3]),
		PreClose:   parseFloat(parts[4]),
		Open:       parseFloat(parts[5]),
		Volume:     parseInt(parts[6]) * 100, // 腾讯返回的是手，转换为股
		High:       parseFloat(parts[33]),
		Low:        parseFloat(parts[34]),
		Amount:     parseFloat(parts[37]) * 10000, // 腾讯返回的是万元
		UpdateTime: time.Now().Format("2006-01-02 15:04:05"),
	}

	// 尝试从更准确的位置获取最高最低价
	if len(parts) > 41 {
		if h := parseFloat(parts[41]); h > 0 {
			price.High = h
		}
		if l := parseFloat(parts[42]); l > 0 {
			price.Low = l
		}
	}

	price.Change = price.Price - price.PreClose
	if price.PreClose > 0 {
		price.ChangePercent = (price.Change / price.PreClose) * 100
	}

	return price
}

// GetStockPriceFromNetease 从网易获取股票实时价格
func (api *StockAPI) GetStockPriceFromNetease(codes []string) (map[string]*models.StockPrice, error) {
	if len(codes) == 0 {
		return nil, nil
	}

	// 转换代码格式：sh000001 -> 0000001, sz000001 -> 1000001
	var neteaseCodes []string
	codeMap := make(map[string]string) // 网易代码 -> 原始代码
	for _, code := range codes {
		var nCode string
		if strings.HasPrefix(code, "sh") {
			nCode = "0" + code[2:]
		} else if strings.HasPrefix(code, "sz") {
			nCode = "1" + code[2:]
		} else {
			continue
		}
		neteaseCodes = append(neteaseCodes, nCode)
		codeMap[nCode] = code
	}

	if len(neteaseCodes) == 0 {
		return nil, nil
	}

	codeList := strings.Join(neteaseCodes, ",")
	url := fmt.Sprintf("https://api.money.126.net/data/feed/%s", codeList)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://money.163.com")
	req.Header.Set("User-Agent", api.rm.GetRandomUA())

	// 使用限流器
	resp, err := api.rm.DoRequestWithRateLimit("api.money.126.net", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return api.parseNeteaseResponse(string(body), codeMap)
}

// parseNeteaseResponse 解析网易接口返回数据
// 格式: _ntes_quote_callback({"0000001":{...},"1000001":{...}});
func (api *StockAPI) parseNeteaseResponse(data string, codeMap map[string]string) (map[string]*models.StockPrice, error) {
	result := make(map[string]*models.StockPrice)

	// 去除JSONP包装
	data = strings.TrimPrefix(data, "_ntes_quote_callback(")
	data = strings.TrimSuffix(data, ");")
	data = strings.TrimSpace(data)

	if data == "" {
		return result, nil
	}

	var jsonData map[string]struct {
		Code      string  `json:"code"`
		Name      string  `json:"name"`
		Price     float64 `json:"price"`
		YestClose float64 `json:"yestclose"`
		Open      float64 `json:"open"`
		High      float64 `json:"high"`
		Low       float64 `json:"low"`
		Volume    int64   `json:"volume"`
		Turnover  float64 `json:"turnover"`
		Time      string  `json:"time"`
		Updown    float64 `json:"updown"`
		Percent   float64 `json:"percent"`
	}

	if err := json.Unmarshal([]byte(data), &jsonData); err != nil {
		return nil, err
	}

	for nCode, item := range jsonData {
		originalCode, ok := codeMap[nCode]
		if !ok {
			continue
		}

		price := &models.StockPrice{
			Code:          originalCode,
			Name:          item.Name,
			Price:         item.Price,
			PreClose:      item.YestClose,
			Open:          item.Open,
			High:          item.High,
			Low:           item.Low,
			Volume:        item.Volume,
			Amount:        item.Turnover,
			Change:        item.Updown,
			ChangePercent: item.Percent * 100, // 网易返回的是小数形式
			UpdateTime:    item.Time,
		}

		result[originalCode] = price
	}

	return result, nil
}

// DataSourceType 数据源类型
type DataSourceType int

const (
	DataSourceSina DataSourceType = iota
	DataSourceTencent
	DataSourceNetease
	DataSourceEastmoney
	DataSourceSohu
	DataSourceXueqiu
	DataSourceBaidu
	DataSourceHexun
)

// 轮询状态管理
var (
	roundRobinIndex int
	roundRobinMu    sync.Mutex
)

// GetStockPriceMultiSource 多数据源获取股票实时价格（自动轮换和故障转移）
func (api *StockAPI) GetStockPriceMultiSource(codes []string) (map[string]*models.StockPrice, error) {
	if len(codes) == 0 {
		return nil, nil
	}

	// 数据源列表，按优先级排序（限制宽松的优先）
	sources := []struct {
		name   string
		fetch  func([]string) (map[string]*models.StockPrice, error)
		domain string
	}{
		{"Sina", api.GetStockPrice, "hq.sinajs.cn"},
		{"Tencent", api.GetStockPriceFromTencent, "qt.gtimg.cn"},
		{"Netease", api.GetStockPriceFromNetease, "api.money.126.net"},
		{"Eastmoney", api.GetStockPriceFromEastmoney, "push2.eastmoney.com"},
		{"Sohu", api.GetStockPriceFromSohu, "hq.stock.sohu.com"},
	}

	// 尝试每个数据源
	var lastErr error
	for _, source := range sources {
		// 检查数据源是否可用
		if !api.rm.IsSourceAvailable(source.domain) {
			log.Printf("[行情] %s 数据源暂时不可用，跳过", source.name)
			continue
		}

		result, err := source.fetch(codes)
		if err != nil {
			log.Printf("[行情] %s 获取失败: %v", source.name, err)
			api.rm.MarkSourceFailed(source.domain)
			lastErr = err
			continue
		}

		if len(result) > 0 {
			log.Printf("[行情] 成功从 %s 获取 %d 条数据", source.name, len(result))
			api.rm.MarkSourceSuccess(source.domain)
			return result, nil
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("所有数据源均不可用")
}

// GetStockPriceWithSource 从指定数据源获取股票价格
func (api *StockAPI) GetStockPriceWithSource(codes []string, source DataSourceType) (map[string]*models.StockPrice, error) {
	switch source {
	case DataSourceSina:
		return api.GetStockPrice(codes)
	case DataSourceTencent:
		return api.GetStockPriceFromTencent(codes)
	case DataSourceNetease:
		return api.GetStockPriceFromNetease(codes)
	case DataSourceEastmoney:
		return api.GetStockPriceFromEastmoney(codes)
	case DataSourceSohu:
		return api.GetStockPriceFromSohu(codes)
	case DataSourceXueqiu:
		return api.GetStockPriceFromXueqiu(codes)
	case DataSourceBaidu:
		return api.GetStockPriceFromBaidu(codes)
	case DataSourceHexun:
		return api.GetStockPriceFromHexun(codes)
	default:
		return api.GetStockPriceMultiSource(codes)
	}
}

// GetStockPriceFromEastmoney 从东方财富获取股票实时价格
func (api *StockAPI) GetStockPriceFromEastmoney(codes []string) (map[string]*models.StockPrice, error) {
	if len(codes) == 0 {
		return nil, nil
	}

	result := make(map[string]*models.StockPrice)

	// 东方财富需要逐个请求或使用批量接口
	// 转换代码格式：sh600519 -> 1.600519, sz000001 -> 0.000001
	var secids []string
	codeMap := make(map[string]string) // secid -> 原始代码
	for _, code := range codes {
		var secid string
		if strings.HasPrefix(code, "sh") {
			secid = "1." + code[2:]
		} else if strings.HasPrefix(code, "sz") {
			secid = "0." + code[2:]
		} else {
			continue
		}
		secids = append(secids, secid)
		codeMap[secid] = code
	}

	if len(secids) == 0 {
		return nil, nil
	}

	// 使用批量接口
	secidList := strings.Join(secids, ",")
	url := fmt.Sprintf("https://push2.eastmoney.com/api/qt/ulist/get?fltt=2&secids=%s&fields=f1,f2,f3,f4,f5,f6,f7,f8,f9,f10,f12,f13,f14,f15,f16,f17,f18", secidList)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://quote.eastmoney.com")
	req.Header.Set("User-Agent", api.rm.GetRandomUA())

	// 使用限流器
	resp, err := api.rm.DoRequestWithRateLimit("push2.eastmoney.com", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 解析响应
	var jsonResp struct {
		Data struct {
			Diff []struct {
				F2  float64 `json:"f2"`  // 最新价
				F3  float64 `json:"f3"`  // 涨跌幅
				F4  float64 `json:"f4"`  // 涨跌额
				F5  int64   `json:"f5"`  // 成交量（手）
				F6  float64 `json:"f6"`  // 成交额
				F12 string  `json:"f12"` // 代码
				F13 int     `json:"f13"` // 市场（0深圳，1上海）
				F14 string  `json:"f14"` // 名称
				F15 float64 `json:"f15"` // 最高
				F16 float64 `json:"f16"` // 最低
				F17 float64 `json:"f17"` // 今开
				F18 float64 `json:"f18"` // 昨收
			} `json:"diff"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &jsonResp); err != nil {
		return nil, err
	}

	for _, item := range jsonResp.Data.Diff {
		// 构造secid
		var secid string
		if item.F13 == 1 {
			secid = fmt.Sprintf("1.%s", item.F12)
		} else {
			secid = fmt.Sprintf("0.%s", item.F12)
		}

		originalCode, ok := codeMap[secid]
		if !ok {
			continue
		}

		// 注意：这个接口使用了 fltt=2 参数，返回的是原始值，不需要除以100
		// 与 multi_source.go 中的接口不同（那个没有 fltt=2，需要除以100）
		price := &models.StockPrice{
			Code:          originalCode,
			Name:          item.F14,
			Price:         item.F2,
			PreClose:      item.F18,
			Open:          item.F17,
			High:          item.F15,
			Low:           item.F16,
			Volume:        item.F5 * 100, // 手转股
			Amount:        item.F6,
			Change:        item.F4,
			ChangePercent: item.F3,
			UpdateTime:    time.Now().Format("2006-01-02 15:04:05"),
		}

		result[originalCode] = price
	}

	return result, nil
}

// GetStockPriceFromSohu 从搜狐获取股票实时价格
func (api *StockAPI) GetStockPriceFromSohu(codes []string) (map[string]*models.StockPrice, error) {
	if len(codes) == 0 {
		return nil, nil
	}

	// 转换代码格式：sh600519 -> cn_600519, sz000001 -> cn_000001
	var sohucodes []string
	codeMap := make(map[string]string) // 搜狐代码 -> 原始代码
	for _, code := range codes {
		var sCode string
		if strings.HasPrefix(code, "sh") {
			sCode = "cn_" + code[2:]
		} else if strings.HasPrefix(code, "sz") {
			sCode = "cn_" + code[2:]
		} else {
			continue
		}
		sohucodes = append(sohucodes, sCode)
		codeMap[sCode] = code
	}

	if len(sohucodes) == 0 {
		return nil, nil
	}

	codeList := strings.Join(sohucodes, ",")
	url := fmt.Sprintf("https://hq.stock.sohu.com/hqdata/getquote.php?code=%s&callback=", codeList)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://q.stock.sohu.com")
	req.Header.Set("User-Agent", api.rm.GetRandomUA())

	// 使用限流器
	resp, err := api.rm.DoRequestWithRateLimit("hq.stock.sohu.com", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return api.parseSohuResponse(string(body), codeMap)
}

// parseSohuResponse 解析搜狐接口返回数据
// 格式: [["cn_600519","贵州茅台","1850.00","1835.00",...],...]
func (api *StockAPI) parseSohuResponse(data string, codeMap map[string]string) (map[string]*models.StockPrice, error) {
	result := make(map[string]*models.StockPrice)

	// 去除可能的JSONP包装
	data = strings.TrimSpace(data)
	if strings.HasPrefix(data, "(") {
		data = strings.TrimPrefix(data, "(")
		data = strings.TrimSuffix(data, ")")
	}

	if data == "" || data == "[]" {
		return result, nil
	}

	var jsonData [][]interface{}
	if err := json.Unmarshal([]byte(data), &jsonData); err != nil {
		// 尝试解析为对象数组格式
		return api.parseSohuResponseAlt(data, codeMap)
	}

	for _, item := range jsonData {
		if len(item) < 10 {
			continue
		}

		sCode, ok := item[0].(string)
		if !ok {
			continue
		}

		originalCode, ok := codeMap[sCode]
		if !ok {
			continue
		}

		name, _ := item[1].(string)
		priceStr, _ := item[2].(string)
		preCloseStr, _ := item[3].(string)
		openStr, _ := item[4].(string)
		highStr, _ := item[5].(string)
		lowStr, _ := item[6].(string)

		priceVal := parseFloat(priceStr)
		preCloseVal := parseFloat(preCloseStr)

		price := &models.StockPrice{
			Code:       originalCode,
			Name:       name,
			Price:      priceVal,
			PreClose:   preCloseVal,
			Open:       parseFloat(openStr),
			High:       parseFloat(highStr),
			Low:        parseFloat(lowStr),
			UpdateTime: time.Now().Format("2006-01-02 15:04:05"),
		}

		price.Change = price.Price - price.PreClose
		if price.PreClose > 0 {
			price.ChangePercent = (price.Change / price.PreClose) * 100
		}

		result[originalCode] = price
	}

	return result, nil
}

// parseSohuResponseAlt 解析搜狐接口的备用格式
func (api *StockAPI) parseSohuResponseAlt(data string, codeMap map[string]string) (map[string]*models.StockPrice, error) {
	result := make(map[string]*models.StockPrice)

	// 尝试解析为对象格式
	var jsonData map[string]interface{}
	if err := json.Unmarshal([]byte(data), &jsonData); err != nil {
		return result, nil
	}

	for sCode, v := range jsonData {
		originalCode, ok := codeMap[sCode]
		if !ok {
			continue
		}

		itemMap, ok := v.(map[string]interface{})
		if !ok {
			continue
		}

		name, _ := itemMap["name"].(string)
		priceVal, _ := itemMap["price"].(float64)
		preCloseVal, _ := itemMap["preclose"].(float64)
		openVal, _ := itemMap["open"].(float64)
		highVal, _ := itemMap["high"].(float64)
		lowVal, _ := itemMap["low"].(float64)
		volumeVal, _ := itemMap["volume"].(float64)
		amountVal, _ := itemMap["amount"].(float64)

		price := &models.StockPrice{
			Code:       originalCode,
			Name:       name,
			Price:      priceVal,
			PreClose:   preCloseVal,
			Open:       openVal,
			High:       highVal,
			Low:        lowVal,
			Volume:     int64(volumeVal),
			Amount:     amountVal,
			UpdateTime: time.Now().Format("2006-01-02 15:04:05"),
		}

		price.Change = price.Price - price.PreClose
		if price.PreClose > 0 {
			price.ChangePercent = (price.Change / price.PreClose) * 100
		}

		result[originalCode] = price
	}

	return result, nil
}

// GetReportContent 获取研报内容
// 注意：东方财富的研报API只返回元数据，不返回实际内容
// 实际内容需要通过浏览器抓取网页获取
func (api *StockAPI) GetReportContent(infoCode string) (string, error) {
	if infoCode == "" {
		return "", fmt.Errorf("infoCode为空")
	}

	// 东方财富的研报详情API不提供实际内容，只有元数据
	// 直接返回错误，让调用方使用浏览器获取
	log.Printf("[研报内容] infoCode: %s - API不提供研报正文，需要使用本地浏览器获取", infoCode)
	return "", fmt.Errorf("东方财富API不提供研报正文内容")
}

// GetNoticeContent 获取公告内容（通过东方财富API）
func (api *StockAPI) GetNoticeContent(stockCode string, artCode string) (string, error) {
	if artCode == "" {
		return "", fmt.Errorf("artCode为空")
	}

	fullCode := stockCode
	if len(fullCode) == 6 {
		if strings.HasPrefix(fullCode, "6") {
			fullCode = "sh" + fullCode
		} else if strings.HasPrefix(fullCode, "0") || strings.HasPrefix(fullCode, "3") {
			fullCode = "sz" + fullCode
		}
	}

	// 去除股票代码前缀
	code := stockCode
	if strings.HasPrefix(stockCode, "sh") || strings.HasPrefix(stockCode, "sz") {
		code = stockCode[2:]
	}

	// 东方财富公告详情API
	url := fmt.Sprintf("https://np-anotice-stock.eastmoney.com/api/security/ann?ann_id=%s&stock_list=%s", artCode, code)

	log.Printf("[公告内容] 请求URL: %s", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Referer", "https://data.eastmoney.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := api.getClient().Do(req)
	if err != nil {
		log.Printf("[公告内容] 请求失败: %v", err)
		return "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("[公告内容] 读取响应失败: %v", err)
		return "", err
	}

	log.Printf("[公告内容] 响应长度: %d", len(body))

	// 尝试解析公告详情
	var result struct {
		Data struct {
			List []struct {
				Title      string `json:"title"`
				Content    string `json:"content"`
				NoticeDate string `json:"notice_date"`
			} `json:"list"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		log.Printf("[公告内容] JSON解析失败: %v", err)
		return "", err
	}

	if len(result.Data.List) == 0 {
		return "", fmt.Errorf("未获取到公告内容")
	}

	notice := result.Data.List[0]
	content := notice.Content
	if content == "" {
		// 如果API没有返回内容，尝试获取公告PDF/HTML内容
		content = api.fetchNoticeDetailContent(fullCode, artCode)
	}

	if content == "" {
		return fmt.Sprintf("公告链接：%s\n（原文未能直接获取，请点击链接查看全文。）", fmt.Sprintf("https://data.eastmoney.com/notices/detail/%s/%s.html", fullCode, artCode)), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("标题：%s\n", notice.Title))
	sb.WriteString(fmt.Sprintf("发布日期：%s\n\n", notice.NoticeDate))
	sb.WriteString("内容：\n")
	sb.WriteString(content)

	return sb.String(), nil
}

// fetchNoticeDetailContent 获取公告详细内容
func (api *StockAPI) fetchNoticeDetailContent(stockCode string, artCode string) string {
	// 尝试获取公告的文本内容
	url := fmt.Sprintf("https://data.eastmoney.com/notices/detail/%s/%s.html", stockCode, artCode)

	content, err := api.rm.FetchWebContent(url)
	if err != nil {
		log.Printf("[公告详情] 抓取失败: %v", err)
		return ""
	}

	return content
}

// ==================== 新增数据源：雪球、百度、和讯 ====================

// GetStockPriceFromXueqiu 从雪球获取股票实时价格
func (api *StockAPI) GetStockPriceFromXueqiu(codes []string) (map[string]*models.StockPrice, error) {
	if len(codes) == 0 {
		return nil, nil
	}

	result := make(map[string]*models.StockPrice)

	// 转换代码格式：sh600519 -> SH600519, sz000001 -> SZ000001
	var xueqiuCodes []string
	codeMap := make(map[string]string) // 雪球代码 -> 原始代码
	for _, code := range codes {
		var xCode string
		if strings.HasPrefix(code, "sh") {
			xCode = "SH" + code[2:]
		} else if strings.HasPrefix(code, "sz") {
			xCode = "SZ" + code[2:]
		} else {
			continue
		}
		xueqiuCodes = append(xueqiuCodes, xCode)
		codeMap[xCode] = code
	}

	if len(xueqiuCodes) == 0 {
		return nil, nil
	}

	codeList := strings.Join(xueqiuCodes, ",")
	url := fmt.Sprintf("https://stock.xueqiu.com/v5/stock/realtime/quotec.json?symbol=%s", codeList)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://xueqiu.com/")
	req.Header.Set("User-Agent", api.rm.GetRandomUA())
	// 雪球需要cookie，使用一个通用的访客cookie
	req.Header.Set("Cookie", "xq_a_token=; xqat=; xq_r_token=; xq_id_token=; u=;")

	// 使用限流器
	resp, err := api.rm.DoRequestWithRateLimit("stock.xueqiu.com", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 解析响应
	var jsonResp struct {
		Data []struct {
			Symbol    string  `json:"symbol"`
			Current   float64 `json:"current"`
			Percent   float64 `json:"percent"`
			Chg       float64 `json:"chg"`
			High      float64 `json:"high"`
			Low       float64 `json:"low"`
			Open      float64 `json:"open"`
			LastClose float64 `json:"last_close"`
			Volume    int64   `json:"volume"`
			Amount    float64 `json:"amount"`
			Time      int64   `json:"time"`
		} `json:"data"`
		ErrorCode    int    `json:"error_code"`
		ErrorMessage string `json:"error_description"`
	}

	if err := json.Unmarshal(body, &jsonResp); err != nil {
		return nil, err
	}

	if jsonResp.ErrorCode != 0 {
		return nil, fmt.Errorf("雪球API错误: %s", jsonResp.ErrorMessage)
	}

	for _, item := range jsonResp.Data {
		originalCode, ok := codeMap[item.Symbol]
		if !ok {
			continue
		}

		price := &models.StockPrice{
			Code:          originalCode,
			Name:          "", // 雪球这个接口不返回名称
			Price:         item.Current,
			PreClose:      item.LastClose,
			Open:          item.Open,
			High:          item.High,
			Low:           item.Low,
			Volume:        item.Volume,
			Amount:        item.Amount,
			Change:        item.Chg,
			ChangePercent: item.Percent,
			UpdateTime:    time.Now().Format("2006-01-02 15:04:05"),
		}

		result[originalCode] = price
	}

	return result, nil
}

// GetStockPriceFromBaidu 从百度股市通获取股票实时价格
func (api *StockAPI) GetStockPriceFromBaidu(codes []string) (map[string]*models.StockPrice, error) {
	if len(codes) == 0 {
		return nil, nil
	}

	result := make(map[string]*models.StockPrice)

	// 转换代码格式：sh600519 -> sh600519（百度格式相同）
	codeList := strings.Join(codes, ",")
	url := fmt.Sprintf("https://finance.pae.baidu.com/selfselect/getstockquotation?all=1&code=%s&isIndex=false&isBk=false&isBlock=false&isFutures=false&isStock=true&newFormat=1&is_kc=0", codeList)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://gushitong.baidu.com/")
	req.Header.Set("User-Agent", api.rm.GetRandomUA())

	// 使用限流器
	resp, err := api.rm.DoRequestWithRateLimit("finance.pae.baidu.com", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 解析响应
	var jsonResp struct {
		Result struct {
			List []struct {
				Code     string `json:"code"`
				Name     string `json:"name"`
				Price    string `json:"price"`
				Ratio    string `json:"ratio"`
				Increase string `json:"increase"`
				Open     string `json:"open"`
				PreClose string `json:"preClose"`
				High     string `json:"high"`
				Low      string `json:"low"`
				Volume   string `json:"volume"`
				Amount   string `json:"amount"`
				Exchange string `json:"exchange"`
			} `json:"list"`
		} `json:"Result"`
	}

	if err := json.Unmarshal(body, &jsonResp); err != nil {
		return nil, err
	}

	for _, item := range jsonResp.Result.List {
		// 构造原始代码
		var originalCode string
		if item.Exchange == "sh" {
			originalCode = "sh" + item.Code
		} else if item.Exchange == "sz" {
			originalCode = "sz" + item.Code
		} else {
			originalCode = item.Code
		}

		price := &models.StockPrice{
			Code:          originalCode,
			Name:          item.Name,
			Price:         parseFloat(item.Price),
			PreClose:      parseFloat(item.PreClose),
			Open:          parseFloat(item.Open),
			High:          parseFloat(item.High),
			Low:           parseFloat(item.Low),
			Volume:        parseInt(item.Volume),
			Amount:        parseFloat(item.Amount),
			Change:        parseFloat(item.Increase),
			ChangePercent: parseFloat(strings.TrimSuffix(item.Ratio, "%")),
			UpdateTime:    time.Now().Format("2006-01-02 15:04:05"),
		}

		result[originalCode] = price
	}

	return result, nil
}

// GetStockPriceFromHexun 从和讯获取股票实时价格
func (api *StockAPI) GetStockPriceFromHexun(codes []string) (map[string]*models.StockPrice, error) {
	if len(codes) == 0 {
		return nil, nil
	}

	// 转换代码格式：sh600519 -> 600519sha, sz000001 -> 000001sza
	var hexunCodes []string
	codeMap := make(map[string]string) // 和讯代码 -> 原始代码
	for _, code := range codes {
		var hCode string
		if strings.HasPrefix(code, "sh") {
			hCode = code[2:] + "sha"
		} else if strings.HasPrefix(code, "sz") {
			hCode = code[2:] + "sza"
		} else {
			continue
		}
		hexunCodes = append(hexunCodes, hCode)
		codeMap[hCode] = code
	}

	if len(hexunCodes) == 0 {
		return nil, nil
	}

	codeList := strings.Join(hexunCodes, ",")
	url := fmt.Sprintf("http://webstock.quote.hermes.hexun.com/a/quotelist?code=%s&callback=", codeList)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "http://stockdata.stock.hexun.com/")
	req.Header.Set("User-Agent", api.rm.GetRandomUA())

	// 使用限流器
	resp, err := api.rm.DoRequestWithRateLimit("webstock.quote.hermes.hexun.com", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return api.parseHexunResponse(string(body), codeMap)
}

// parseHexunResponse 解析和讯接口返回数据
func (api *StockAPI) parseHexunResponse(data string, codeMap map[string]string) (map[string]*models.StockPrice, error) {
	result := make(map[string]*models.StockPrice)

	// 去除JSONP包装
	data = strings.TrimSpace(data)
	if idx := strings.Index(data, "("); idx != -1 {
		data = data[idx+1:]
	}
	if strings.HasSuffix(data, ");") {
		data = strings.TrimSuffix(data, ");")
	} else if strings.HasSuffix(data, ")") {
		data = strings.TrimSuffix(data, ")")
	}

	if data == "" {
		return result, nil
	}

	// 和讯返回格式: {"Data":[[[代码,名称,现价,涨跌,涨跌幅,开盘,最高,最低,昨收,成交量,成交额,...]]]}
	var jsonResp struct {
		Data [][][]interface{} `json:"Data"`
	}

	if err := json.Unmarshal([]byte(data), &jsonResp); err != nil {
		return nil, err
	}

	if len(jsonResp.Data) == 0 || len(jsonResp.Data[0]) == 0 {
		return result, nil
	}

	for _, item := range jsonResp.Data[0] {
		if len(item) < 11 {
			continue
		}

		hCode, ok := item[0].(string)
		if !ok {
			continue
		}

		originalCode, ok := codeMap[hCode]
		if !ok {
			continue
		}

		name, _ := item[1].(string)
		priceVal := getFloatFromInterface(item[2])
		changeVal := getFloatFromInterface(item[3])
		changePercentVal := getFloatFromInterface(item[4])
		openVal := getFloatFromInterface(item[5])
		highVal := getFloatFromInterface(item[6])
		lowVal := getFloatFromInterface(item[7])
		preCloseVal := getFloatFromInterface(item[8])
		volumeVal := getFloatFromInterface(item[9])
		amountVal := getFloatFromInterface(item[10])

		// 和讯的价格需要除以100（返回的是分）
		price := &models.StockPrice{
			Code:          originalCode,
			Name:          name,
			Price:         priceVal / 100,
			PreClose:      preCloseVal / 100,
			Open:          openVal / 100,
			High:          highVal / 100,
			Low:           lowVal / 100,
			Volume:        int64(volumeVal),
			Amount:        amountVal,
			Change:        changeVal / 100,
			ChangePercent: changePercentVal / 100,
			UpdateTime:    time.Now().Format("2006-01-02 15:04:05"),
		}

		result[originalCode] = price
	}

	return result, nil
}

// getFloatFromInterface 从interface{}获取float64值
func getFloatFromInterface(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	case string:
		return parseFloat(val)
	default:
		return 0
	}
}

// ==================== 轮询模式实现 ====================

// GetStockPriceRoundRobin 轮询模式获取股票实时价格
// 每次调用使用下一个数据源，实现真正的轮询
func (api *StockAPI) GetStockPriceRoundRobin(codes []string) (map[string]*models.StockPrice, string, error) {
	if len(codes) == 0 {
		return nil, "", nil
	}

	// 数据源列表（8个数据源）
	sources := []struct {
		name   string
		fetch  func([]string) (map[string]*models.StockPrice, error)
		domain string
	}{
		{"Sina", api.GetStockPrice, "hq.sinajs.cn"},
		{"Tencent", api.GetStockPriceFromTencent, "qt.gtimg.cn"},
		{"Netease", api.GetStockPriceFromNetease, "api.money.126.net"},
		{"Eastmoney", api.GetStockPriceFromEastmoney, "push2.eastmoney.com"},
		{"Sohu", api.GetStockPriceFromSohu, "hq.stock.sohu.com"},
		{"Xueqiu", api.GetStockPriceFromXueqiu, "stock.xueqiu.com"},
		{"Baidu", api.GetStockPriceFromBaidu, "finance.pae.baidu.com"},
		{"Hexun", api.GetStockPriceFromHexun, "webstock.quote.hermes.hexun.com"},
	}

	// 获取当前轮询索引
	roundRobinMu.Lock()
	currentIndex := roundRobinIndex
	roundRobinIndex = (roundRobinIndex + 1) % len(sources)
	roundRobinMu.Unlock()

	// 尝试当前数据源
	source := sources[currentIndex]
	log.Printf("[行情轮询] 使用数据源 #%d: %s", currentIndex+1, source.name)

	result, err := source.fetch(codes)
	if err != nil {
		log.Printf("[行情轮询] %s 获取失败: %v，尝试下一个数据源", source.name, err)
		// 如果当前数据源失败，尝试下一个
		for i := 1; i < len(sources); i++ {
			nextIndex := (currentIndex + i) % len(sources)
			nextSource := sources[nextIndex]
			log.Printf("[行情轮询] 故障转移到: %s", nextSource.name)
			result, err = nextSource.fetch(codes)
			if err == nil && len(result) > 0 {
				log.Printf("[行情轮询] 成功从 %s 获取 %d 条数据", nextSource.name, len(result))
				return result, nextSource.name, nil
			}
		}
		return nil, "", fmt.Errorf("所有数据源均不可用")
	}

	if len(result) > 0 {
		log.Printf("[行情轮询] 成功从 %s 获取 %d 条数据", source.name, len(result))
		return result, source.name, nil
	}

	return nil, "", fmt.Errorf("数据源 %s 返回空数据", source.name)
}

// GetCurrentRoundRobinSource 获取当前轮询数据源名称（用于显示）
func (api *StockAPI) GetCurrentRoundRobinSource() string {
	sources := []string{"Sina", "Tencent", "Netease", "Eastmoney", "Sohu", "Xueqiu", "Baidu", "Hexun"}
	roundRobinMu.Lock()
	defer roundRobinMu.Unlock()
	return sources[roundRobinIndex]
}

// ResetRoundRobinIndex 重置轮询索引（用于测试）
func (api *StockAPI) ResetRoundRobinIndex() {
	roundRobinMu.Lock()
	defer roundRobinMu.Unlock()
	roundRobinIndex = 0
}
