package data

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
	"stock-ai/backend/models"
)

const (
	fundAPIBaseURL    = "https://fundmobapi.eastmoney.com/FundMNewApi"
	fundAPIF10Referer = "https://fundf10.eastmoney.com/"
)

// FundAPI 基金数据API
type FundAPI struct {
	client           *http.Client
	priceSources     []fundPriceSource
	priceMu          sync.Mutex
	priceFirstLoad   bool
	priceSourceIndex int
}

type fundPriceSource struct {
	name  string
	fetch func([]string) (map[string]*models.FundPrice, error)
}

func (api *FundAPI) buildFundURL(endpoint string, params map[string]string) string {
	values := url.Values{}
	values.Set("deviceid", "Wap")
	values.Set("plat", "Wap")
	values.Set("product", "EFund")
	values.Set("version", "6.5.5")
	for k, v := range params {
		values.Set(k, v)
	}
	return fmt.Sprintf("%s/%s?%s", fundAPIBaseURL, endpoint, values.Encode())
}

func (api *FundAPI) doFundRequest(endpoint string, params map[string]string, target interface{}) error {
	req, err := http.NewRequest("GET", api.buildFundURL(endpoint, params), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Referer", "https://fund.eastmoney.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := api.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	return json.Unmarshal(body, target)
}

func shouldRetryFundAPI(errCode int, errMsg string) bool {
	if errCode == 404 || errCode == 429 || errCode >= 500 {
		return true
	}
	msg := strings.TrimSpace(errMsg)
	if msg == "" {
		return false
	}
	msgLower := strings.ToLower(msg)
	return strings.Contains(msg, "网络繁忙") ||
		strings.Contains(msg, "请稍后") ||
		strings.Contains(msg, "ç½‘ç»œç¹å¿™") ||
		strings.Contains(msgLower, "busy")
}

// NewFundAPI 创建基金API实例
func NewFundAPI() *FundAPI {
	api := &FundAPI{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
	api.priceSources = []fundPriceSource{
		{name: "eastmoney", fetch: api.fetchFundPriceFromEastmoney},
		{name: "tencent", fetch: api.fetchFundPriceFromTencent},
		{name: "sina", fetch: api.fetchFundPriceFromSina},
	}
	api.priceFirstLoad = true
	return api
}

// GetFundPrice 获取基金估值（多数据源）
func (api *FundAPI) GetFundPrice(codes []string) (map[string]*models.FundPrice, error) {
	cleanCodes := api.sanitizeFundCodes(codes)
	if len(cleanCodes) == 0 {
		return map[string]*models.FundPrice{}, nil
	}

	result, source, err := api.fetchFundPricesWithFallback(cleanCodes)
	if err != nil {
		return result, err
	}

	api.fillMissingFundPrices(result, cleanCodes, source)
	return result, nil
}

func (api *FundAPI) sanitizeFundCodes(codes []string) []string {
	unique := make(map[string]struct{})
	var result []string
	for _, code := range codes {
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		if _, ok := unique[code]; ok {
			continue
		}
		unique[code] = struct{}{}
		result = append(result, code)
	}
	return result
}

func (api *FundAPI) fetchFundPricesWithFallback(codes []string) (map[string]*models.FundPrice, string, error) {
	api.priceMu.Lock()
	firstLoad := api.priceFirstLoad
	api.priceMu.Unlock()

	if firstLoad {
		return api.parallelFetchFundPrices(codes)
	}
	return api.roundRobinFetchFundPrices(codes)
}

func (api *FundAPI) parallelFetchFundPrices(codes []string) (map[string]*models.FundPrice, string, error) {
	type result struct {
		data   map[string]*models.FundPrice
		source string
		err    error
	}

	resultCh := make(chan result, len(api.priceSources))

	for _, src := range api.priceSources {
		go func(source fundPriceSource) {
			data, err := source.fetch(codes)
			resultCh <- result{data: data, source: source.name, err: err}
		}(src)
	}

	var best result
	var lastErr error
	for i := 0; i < len(api.priceSources); i++ {
		res := <-resultCh
		if res.err != nil {
			lastErr = res.err
			continue
		}
		if len(res.data) > len(best.data) {
			best = res
		}
	}

	api.priceMu.Lock()
	api.priceFirstLoad = false
	api.priceMu.Unlock()

	if len(best.data) > 0 {
		api.updateNextSourceIndex(best.source)
		return best.data, best.source, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("所有数据源均不可用")
	}
	return nil, "", lastErr
}

func (api *FundAPI) roundRobinFetchFundPrices(codes []string) (map[string]*models.FundPrice, string, error) {
	if len(api.priceSources) == 0 {
		return nil, "", fmt.Errorf("无可用数据源")
	}

	api.priceMu.Lock()
	start := api.priceSourceIndex
	api.priceSourceIndex = (api.priceSourceIndex + 1) % len(api.priceSources)
	api.priceMu.Unlock()

	var lastErr error
	for i := 0; i < len(api.priceSources); i++ {
		idx := (start + i) % len(api.priceSources)
		src := api.priceSources[idx]
		data, err := src.fetch(codes)
		if err == nil && len(data) > 0 {
			return data, src.name, nil
		}
		if err != nil {
			lastErr = err
		}
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("所有数据源均不可用")
	}
	return nil, "", lastErr
}

func (api *FundAPI) updateNextSourceIndex(source string) {
	api.priceMu.Lock()
	defer api.priceMu.Unlock()
	for idx, src := range api.priceSources {
		if src.name == source {
			api.priceSourceIndex = (idx + 1) % len(api.priceSources)
			return
		}
	}
	api.priceSourceIndex = (api.priceSourceIndex + 1) % len(api.priceSources)
}

func (api *FundAPI) fillMissingFundPrices(result map[string]*models.FundPrice, codes []string, usedSource string) {
	if result == nil {
		result = make(map[string]*models.FundPrice)
	}

	missing := api.findMissingCodes(codes, result)
	if len(missing) == 0 {
		return
	}

	for _, src := range api.priceSources {
		if src.name == usedSource {
			continue
		}
		data, err := src.fetch(missing)
		if err != nil || len(data) == 0 {
			continue
		}
		for code, price := range data {
			if _, exists := result[code]; !exists && price != nil {
				result[code] = price
			}
		}
		missing = api.findMissingCodes(missing, result)
		if len(missing) == 0 {
			break
		}
	}
}

func (api *FundAPI) findMissingCodes(codes []string, current map[string]*models.FundPrice) []string {
	var missing []string
	for _, code := range codes {
		if _, ok := current[code]; !ok {
			missing = append(missing, code)
		}
	}
	return missing
}

func (api *FundAPI) fetchFundPriceFromEastmoney(codes []string) (map[string]*models.FundPrice, error) {
	result := make(map[string]*models.FundPrice)
	for _, code := range codes {
		price, err := api.getFundEstimate(code)
		if err != nil || price == nil {
			continue
		}
		result[code] = price
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("eastmoney: 未获取到有效基金估值")
	}
	return result, nil
}

func (api *FundAPI) fetchFundPriceFromTencent(codes []string) (map[string]*models.FundPrice, error) {
	if len(codes) == 0 {
		return map[string]*models.FundPrice{}, nil
	}

	var queryCodes []string
	for _, code := range codes {
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		queryCodes = append(queryCodes, "jj"+code)
	}
	if len(queryCodes) == 0 {
		return map[string]*models.FundPrice{}, nil
	}

	url := fmt.Sprintf("http://qt.gtimg.cn/q=%s", strings.Join(queryCodes, ","))
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := api.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	reader := transform.NewReader(resp.Body, simplifiedchinese.GBK.NewDecoder())
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*models.FundPrice)
	lines := strings.Split(string(body), ";")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		codePart := strings.TrimSpace(parts[0])
		code := strings.TrimPrefix(codePart, "v_jj")
		content := strings.Trim(parts[1], "\";")
		if content == "" {
			continue
		}
		fields := strings.Split(content, "~")
		if len(fields) < 9 {
			continue
		}

		nav := parseFloat(fields[5])
		if nav == 0 {
			nav = parseFloat(fields[2])
		}
		changePercent := parseFloat(fields[7])
		updateDate := fields[8]

		result[code] = &models.FundPrice{
			Code:          code,
			Name:          fields[1],
			Nav:           nav,
			Estimate:      nav,
			ChangePercent: changePercent,
			UpdateTime:    updateDate,
		}
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("tencent: 未获取到有效基金估值")
	}
	return result, nil
}

func (api *FundAPI) fetchFundPriceFromSina(codes []string) (map[string]*models.FundPrice, error) {
	if len(codes) == 0 {
		return map[string]*models.FundPrice{}, nil
	}

	var queryCodes []string
	for _, code := range codes {
		code = strings.TrimSpace(code)
		if code == "" {
			continue
		}
		queryCodes = append(queryCodes, "f_"+code)
	}
	if len(queryCodes) == 0 {
		return map[string]*models.FundPrice{}, nil
	}

	url := fmt.Sprintf("http://hq.sinajs.cn/list=%s", strings.Join(queryCodes, ","))
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://finance.sina.com.cn")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := api.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	reader := transform.NewReader(resp.Body, simplifiedchinese.GBK.NewDecoder())
	body, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}

	result := make(map[string]*models.FundPrice)
	lines := strings.Split(string(body), ";")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		code := strings.TrimPrefix(strings.TrimSpace(parts[0]), "var hq_str_f_")
		content := strings.Trim(parts[1], "\";")
		if content == "" {
			continue
		}

		fields := strings.Split(content, ",")
		if len(fields) < 5 {
			continue
		}

		nav := parseFloat(fields[1])
		prev := parseFloat(fields[3])
		changePercent := 0.0
		if prev > 0 {
			changePercent = (nav - prev) / prev * 100
		}

		result[code] = &models.FundPrice{
			Code:          code,
			Name:          fields[0],
			Nav:           nav,
			Estimate:      nav,
			ChangePercent: changePercent,
			UpdateTime:    fields[4],
		}
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("sina: 未获取到有效基金估值")
	}
	return result, nil
}

// BuildFallbackFundDetail 根据价格信息构建兜底的基金详情
func (api *FundAPI) BuildFallbackFundDetail(code string, price *models.FundPrice) *models.FundDetail {
	if price == nil {
		return nil
	}
	name := price.Name
	if name == "" {
		name = code
	}
	return &models.FundDetail{
		Code:      code,
		Name:      name,
		Type:      "",
		NavDate:   price.UpdateTime,
		Nav:       price.Nav,
		Estimate:  price.Estimate,
		RiskLevel: "",
	}
}

func (api *FundAPI) fetchFundDetailFromPingzhong(code string) (*models.FundDetail, error) {
	url := fmt.Sprintf("https://fund.eastmoney.com/pingzhongdata/%s.js", code)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://fund.eastmoney.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := api.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	js, err := decodeResponseBody(bodyBytes)
	if err != nil {
		return nil, err
	}
	detail := &models.FundDetail{
		Code: code,
		Name: extractJSString(js, "fS_name"),
	}
	if detail.Name == "" {
		detail.Name = code
	}

	detail.OneYearReturn = parseFloat(extractJSString(js, "syl_1n"))
	detail.ThreeYearReturn = parseFloat(extractJSString(js, "syl_3y"))

	if managersJSON := extractJSArray(js, "Data_currentFundManager"); managersJSON != "" {
		var managers []struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal([]byte(managersJSON), &managers); err == nil && len(managers) > 0 {
			detail.Manager = managers[0].Name
		}
	}

	type netTrendPoint struct {
		X int64   `json:"x"`
		Y float64 `json:"y"`
	}
	if trendJSON := extractJSArray(js, "Data_netWorthTrend"); trendJSON != "" {
		var trend []netTrendPoint
		if err := json.Unmarshal([]byte(trendJSON), &trend); err == nil && len(trend) > 0 {
			first := trend[0].Y
			last := trend[len(trend)-1].Y
			lastTime := time.UnixMilli(trend[len(trend)-1].X)
			detail.Nav = last
			detail.Estimate = last
			detail.NavDate = lastTime.Format("2006-01-02")
			if first > 0 {
				detail.SinceStartReturn = (last/first - 1) * 100
			}

			nowYear := time.Now().Year()
			var yearStart float64
			var yearStartFound bool
			cutoffYear := time.Now().AddDate(-1, 0, 0)
			var yearAgoVal float64
			var yearAgoFound bool
			cutoff3Year := time.Now().AddDate(-3, 0, 0)
			var threeYearVal float64
			var threeYearFound bool

			for _, point := range trend {
				pointTime := time.UnixMilli(point.X)
				if !yearStartFound && pointTime.Year() == nowYear {
					yearStart = point.Y
					yearStartFound = true
				}
				if !yearAgoFound && (pointTime.After(cutoffYear) || pointTime.Equal(cutoffYear)) {
					yearAgoVal = point.Y
					yearAgoFound = true
				}
				if !threeYearFound && (pointTime.After(cutoff3Year) || pointTime.Equal(cutoff3Year)) {
					threeYearVal = point.Y
					threeYearFound = true
				}
				if yearStartFound && yearAgoFound && threeYearFound {
					break
				}
			}

			if yearStartFound && yearStart > 0 {
				detail.ThisYearReturn = (last/yearStart - 1) * 100
			}
			if detail.OneYearReturn == 0 && yearAgoFound && yearAgoVal > 0 {
				detail.OneYearReturn = (last/yearAgoVal - 1) * 100
			}
			if detail.ThreeYearReturn == 0 && threeYearFound && threeYearVal > 0 {
				detail.ThreeYearReturn = (last/threeYearVal - 1) * 100
			}
		}
	}

	return detail, nil
}

func (api *FundAPI) fetchFundMetaFromSuggestion(code string) (*models.FundDetail, error) {
	url := fmt.Sprintf("https://fundsuggest.eastmoney.com/FundSearch/api/FundSearchAPI.ashx?m=1&key=%s", code)
	resp, err := api.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Datas []struct {
			Code         string `json:"CODE"`
			Name         string `json:"NAME"`
			FundBaseInfo struct {
				Code     string `json:"FCODE"`
				Short    string `json:"SHORTNAME"`
				Type     string `json:"FTYPE"`
				Manager  string `json:"JJJL"`
				Company  string `json:"JJGS"`
				Risk     string `json:"RISKLEVEL"`
				FundCode string `json:"_id"`
			} `json:"FundBaseInfo"`
		} `json:"Datas"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	for _, item := range result.Datas {
		base := item.FundBaseInfo
		if base.Code == "" {
			base.Code = item.Code
		}
		if strings.EqualFold(base.Code, code) || strings.EqualFold(item.Code, code) {
			return &models.FundDetail{
				Code:      base.Code,
				Name:      firstNonEmpty(base.Short, item.Name),
				Type:      base.Type,
				Manager:   base.Manager,
				Company:   base.Company,
				RiskLevel: base.Risk,
			}, nil
		}
	}
	return nil, fmt.Errorf("fund meta not found")
}

func firstNonEmpty(values ...string) string {
	for _, val := range values {
		if strings.TrimSpace(val) != "" {
			return val
		}
	}
	return ""
}

func extractF10Content(raw string) (string, error) {
	start := strings.Index(raw, `content:"`)
	if start == -1 {
		return "", fmt.Errorf("content not found")
	}
	start += len(`content:"`)
	end := strings.Index(raw[start:], `",records`)
	if end == -1 {
		end = strings.Index(raw[start:], `",arryear`)
	}
	if end == -1 {
		return "", fmt.Errorf("records marker not found")
	}
	segment := raw[start : start+end]
	unquoted, err := strconv.Unquote(`"` + segment + `"`)
	if err != nil {
		return "", err
	}
	return unquoted, nil
}

func stripHTMLTags(input string) string {
	tagRe := regexp.MustCompile(`(?s)<[^>]+>`)
	clean := tagRe.ReplaceAllString(input, "")
	clean = strings.ReplaceAll(clean, "&nbsp;", "")
	return strings.TrimSpace(html.UnescapeString(clean))
}

func decodeResponseBody(body []byte) (string, error) {
	if utf8.Valid(body) {
		return string(body), nil
	}

	reader := transform.NewReader(bytes.NewReader(body), simplifiedchinese.GBK.NewDecoder())
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	return string(decoded), nil
}

func parsePercentValue(val string) float64 {
	val = strings.ReplaceAll(val, "%", "")
	val = strings.ReplaceAll(val, ",", "")
	return parseFloat(val)
}

func (api *FundAPI) fetchFundHoldingsFromF10(code string) ([]models.FundHolding, error) {
	url := fmt.Sprintf("https://fundf10.eastmoney.com/FundArchivesDatas.aspx?type=jjcc&code=%s&topline=10&year=&month=", code)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := api.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	content, err := decodeResponseBody(bodyBytes)
	if err != nil {
		return nil, err
	}

	htmlContent, err := extractF10Content(content)
	if err != nil {
		return nil, err
	}

	tbodyRe := regexp.MustCompile(`(?s)<tbody>(.*?)</tbody>`)
	rowRe := regexp.MustCompile(`(?s)<tr>(.*?)</tr>`)
	cellRe := regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`)

	tbodyMatch := tbodyRe.FindStringSubmatch(htmlContent)
	if len(tbodyMatch) < 2 {
		return nil, fmt.Errorf("持仓表格缺失")
	}

	var holdings []models.FundHolding
	rows := rowRe.FindAllStringSubmatch(tbodyMatch[1], -1)
	for _, row := range rows {
		cells := cellRe.FindAllStringSubmatch(row[1], -1)
		if len(cells) == 0 {
			continue
		}
		values := make([]string, len(cells))
		for i, cell := range cells {
			values[i] = stripHTMLTags(cell[1])
		}
		if len(values) < 4 {
			continue
		}
		codeVal := strings.TrimSpace(values[1])
		nameVal := strings.TrimSpace(values[2])
		var ratioVal float64
		for i := len(values) - 1; i >= 0; i-- {
			if strings.Contains(values[i], "%") {
				ratioVal = parsePercentValue(values[i])
				if ratioVal != 0 {
					break
				}
			}
		}
		if codeVal == "" || nameVal == "" || ratioVal == 0 {
			continue
		}
		holdings = append(holdings, models.FundHolding{
			Code:  codeVal,
			Name:  nameVal,
			Ratio: ratioVal,
			Type:  "stock",
		})
		if len(holdings) >= 10 {
			break
		}
	}

	if len(holdings) == 0 {
		return nil, fmt.Errorf("F10暂无持仓数据")
	}

	return holdings, nil
}

func (api *FundAPI) fillFundMetaFromSuggestion(detail *models.FundDetail) {
	if detail == nil {
		return
	}
	needMeta := detail.Type == "" || detail.Manager == "" || detail.Company == "" || detail.RiskLevel == "" || detail.Name == ""
	if !needMeta {
		return
	}
	if meta, err := api.fetchFundMetaFromSuggestion(detail.Code); err == nil && meta != nil {
		if detail.Name == "" && meta.Name != "" {
			detail.Name = meta.Name
		}
		if detail.Type == "" && meta.Type != "" {
			detail.Type = meta.Type
		}
		if detail.Manager == "" && meta.Manager != "" {
			detail.Manager = meta.Manager
		}
		if detail.Company == "" && meta.Company != "" {
			detail.Company = meta.Company
		}
		if detail.RiskLevel == "" && meta.RiskLevel != "" {
			detail.RiskLevel = meta.RiskLevel
		}
	}
}

func (api *FundAPI) enrichFundDetailWithPingzhong(detail *models.FundDetail) {
	if detail == nil {
		return
	}

	needPerformance := detail.OneYearReturn == 0 && detail.ThreeYearReturn == 0 &&
		detail.ThisYearReturn == 0 && detail.SinceStartReturn == 0
	needManager := detail.Manager == ""
	needNav := detail.Nav == 0 || detail.Estimate == 0 || detail.NavDate == ""

	if !needPerformance && !needManager && !needNav {
		api.fillFundMetaFromSuggestion(detail)
		return
	}

	fallback, err := api.fetchFundDetailFromPingzhong(detail.Code)
	if err != nil || fallback == nil {
		api.fillFundMetaFromSuggestion(detail)
		return
	}

	if needManager && fallback.Manager != "" {
		detail.Manager = fallback.Manager
	}
	if needNav {
		if detail.Nav == 0 && fallback.Nav != 0 {
			detail.Nav = fallback.Nav
		}
		if detail.Estimate == 0 && fallback.Estimate != 0 {
			detail.Estimate = fallback.Estimate
		}
		if detail.NavDate == "" && fallback.NavDate != "" {
			detail.NavDate = fallback.NavDate
		}
	}
	if needPerformance {
		if detail.OneYearReturn == 0 && fallback.OneYearReturn != 0 {
			detail.OneYearReturn = fallback.OneYearReturn
		}
		if detail.ThreeYearReturn == 0 && fallback.ThreeYearReturn != 0 {
			detail.ThreeYearReturn = fallback.ThreeYearReturn
		}
		if detail.ThisYearReturn == 0 && fallback.ThisYearReturn != 0 {
			detail.ThisYearReturn = fallback.ThisYearReturn
		}
		if detail.SinceStartReturn == 0 && fallback.SinceStartReturn != 0 {
			detail.SinceStartReturn = fallback.SinceStartReturn
		}
	}
	api.fillFundMetaFromSuggestion(detail)
}

func extractJSString(js, key string) string {
	re := regexp.MustCompile(fmt.Sprintf(`var\s+%s\s*=\s*"([^"]*)"`, regexp.QuoteMeta(key)))
	if match := re.FindStringSubmatch(js); len(match) == 2 {
		return match[1]
	}
	return ""
}

func extractJSArray(js, key string) string {
	marker := fmt.Sprintf("var %s", key)
	idx := strings.Index(js, marker)
	if idx == -1 {
		return ""
	}
	start := idx + len(marker)
	for start < len(js) && (js[start] == ' ' || js[start] == '=' || js[start] == '\t' || js[start] == '\r' || js[start] == '\n') {
		start++
	}
	if start >= len(js) || js[start] != '[' {
		return ""
	}
	depth := 0
	for i := start; i < len(js); i++ {
		switch js[i] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				return js[start : i+1]
			}
		}
	}
	return ""
}

// getFundEstimate 获取单只基金估值
func (api *FundAPI) getFundEstimate(code string) (*models.FundPrice, error) {
	url := fmt.Sprintf("https://fundgz.1234567.com.cn/js/%s.js?rt=%d", code, time.Now().UnixMilli())

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Referer", "https://fund.eastmoney.com/")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := api.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 解析JSONP响应: jsonpgz({"fundcode":"000001",...});
	re := regexp.MustCompile(`jsonpgz\((.+)\)`)
	matches := re.FindSubmatch(body)
	if len(matches) < 2 {
		return nil, fmt.Errorf("invalid response format")
	}

	var data struct {
		FundCode string `json:"fundcode"`
		Name     string `json:"name"`
		Dwjz     string `json:"dwjz"`  // 单位净值
		Gsz      string `json:"gsz"`   // 估算净值
		Gszzl    string `json:"gszzl"` // 估算涨跌幅
		Gztime   string `json:"gztime"`
	}

	if err := json.Unmarshal(matches[1], &data); err != nil {
		return nil, err
	}

	return &models.FundPrice{
		Code:          data.FundCode,
		Name:          data.Name,
		Nav:           parseFloat(data.Dwjz),
		Estimate:      parseFloat(data.Gsz),
		ChangePercent: parseFloat(data.Gszzl),
		UpdateTime:    data.Gztime,
	}, nil
}

// GetFundDetail 获取基金基本信息
func (api *FundAPI) GetFundDetail(code string) (*models.FundDetail, error) {
	const maxRetries = 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		var resp struct {
			Datas struct {
				FCODE     string `json:"FCODE"`
				SHORTNAME string `json:"SHORTNAME"`
				FTYPE     string `json:"FTYPE"`
				RISKLEVEL string `json:"RISKLEVEL"`
				JJJL      string `json:"JJJL"`
				JJGS      string `json:"JJGS"`
				FEGM      string `json:"FEGM"`
				FEGMRQ    string `json:"FEGMRQ"`
				ESTABDATE string `json:"ESTABDATE"`
				FSRQ      string `json:"FSRQ"`
				DWJZ      string `json:"DWJZ"`
				ESTDIFF   string `json:"ESTDIFF"`
				RZDF      string `json:"RZDF"`
				SYL_1N    string `json:"SYL_1N"`
				SYL_3N    string `json:"SYL_3N"`
				SYL_JN    string `json:"SYL_JN"`
				SYL_LN    string `json:"SYL_LN"`
				SHARP1    string `json:"SHARP1"`
				MAXRETRA1 string `json:"MAXRETRA1"`
			} `json:"Datas"`
			Success bool   `json:"Success"`
			ErrMsg  string `json:"ErrMsg"`
			ErrCode int    `json:"ErrCode"`
		}

		if err := api.doFundRequest("FundMNBasicInformation", map[string]string{"FCODE": code}, &resp); err != nil {
			lastErr = err
		} else if resp.Success {
			data := resp.Datas
			detail := &models.FundDetail{
				Code:             data.FCODE,
				Name:             data.SHORTNAME,
				Type:             data.FTYPE,
				RiskLevel:        data.RISKLEVEL,
				Manager:          data.JJJL,
				Company:          data.JJGS,
				Scale:            parseFloat(data.FEGM),
				ScaleDate:        data.FEGMRQ,
				InceptionDate:    data.ESTABDATE,
				NavDate:          data.FSRQ,
				Nav:              parseFloat(data.DWJZ),
				Estimate:         parseFloat(data.ESTDIFF),
				OneDayReturn:     parseFloat(data.RZDF),
				OneYearReturn:    parseFloat(data.SYL_1N),
				ThreeYearReturn:  parseFloat(data.SYL_3N),
				ThisYearReturn:   parseFloat(data.SYL_JN),
				SinceStartReturn: parseFloat(data.SYL_LN),
				SharpRatio:       parseFloat(data.SHARP1),
				MaxDrawdown:      parseFloat(data.MAXRETRA1),
			}
			api.enrichFundDetailWithPingzhong(detail)
			return detail, nil
		} else {
			msg := resp.ErrMsg
			if msg == "" {
				msg = "获取基金信息失败"
			}
			lastErr = fmt.Errorf(msg)
			if !shouldRetryFundAPI(resp.ErrCode, resp.ErrMsg) {
				break
			}
		}

		if attempt < maxRetries-1 {
			time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
		}
	}

	if fallback, err := api.fetchFundDetailFromPingzhong(code); err == nil && fallback != nil {
		api.fillFundMetaFromSuggestion(fallback)
		return fallback, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("获取基金信息失败")
	}
	return nil, lastErr
}

// GetFundHistory 获取基金历史净值
func (api *FundAPI) GetFundHistory(code string, count int) ([]models.FundPerformancePoint, error) {
	if count <= 0 {
		count = 60
	}
	params := map[string]string{
		"FCODE":     code,
		"pageSize":  fmt.Sprintf("%d", count),
		"pageIndex": "1",
	}

	const maxRetries = 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		var resp struct {
			Datas []struct {
				FSRQ  string `json:"FSRQ"`
				DWJZ  string `json:"DWJZ"`
				LJJZ  string `json:"LJJZ"`
				JZZZL string `json:"JZZZL"`
			} `json:"Datas"`
			Success bool   `json:"Success"`
			ErrMsg  string `json:"ErrMsg"`
			ErrCode int    `json:"ErrCode"`
		}

		if err := api.doFundRequest("FundMNHisNetList", params, &resp); err != nil {
			lastErr = err
		} else if resp.Success {
			history := make([]models.FundPerformancePoint, 0, len(resp.Datas))
			for _, item := range resp.Datas {
				history = append(history, models.FundPerformancePoint{
					Date:          item.FSRQ,
					Nav:           parseFloat(item.DWJZ),
					AccNav:        parseFloat(item.LJJZ),
					ChangePercent: parseFloat(item.JZZZL),
				})
			}
			return history, nil
		} else {
			msg := resp.ErrMsg
			if msg == "" {
				msg = "获取基金历史净值失败"
			}
			lastErr = fmt.Errorf(msg)
			if !shouldRetryFundAPI(resp.ErrCode, resp.ErrMsg) {
				break
			}
		}

		if attempt < maxRetries-1 {
			time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
		}
	}

	if history, err := api.fetchFundHistoryFromAPI(code, count); err == nil && len(history) > 0 {
		return history, nil
	} else if err != nil && lastErr == nil {
		lastErr = err
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("获取基金历史净值失败")
	}
	return nil, lastErr
}

// GetFundHoldings 获取基金持仓
func (api *FundAPI) GetFundHoldings(code string) ([]models.FundHolding, []models.FundHolding, error) {
	const maxRetries = 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		var resp struct {
			Datas struct {
				Stocks []struct {
					Code     string `json:"GPDM"`
					Name     string `json:"GPJC"`
					Ratio    string `json:"JZBL"`
					Industry string `json:"INDEXNAME"`
					Trend    string `json:"PCTNVCHGTYPE"`
					Change   string `json:"PCTNVCHG"`
				} `json:"fundStocks"`
				Bonds []struct {
					Code  string `json:"ZQDM"`
					Name  string `json:"ZQMC"`
					Ratio string `json:"ZJZBL"`
				} `json:"fundboods"`
			} `json:"Datas"`
			Success bool   `json:"Success"`
			ErrMsg  string `json:"ErrMsg"`
			ErrCode int    `json:"ErrCode"`
		}

		if err := api.doFundRequest("FundMNInverstPosition", map[string]string{"FCODE": code}, &resp); err != nil {
			lastErr = err
		} else if resp.Success {
			var stockHoldings []models.FundHolding
			for _, item := range resp.Datas.Stocks {
				stockHoldings = append(stockHoldings, models.FundHolding{
					Code:     item.Code,
					Name:     item.Name,
					Ratio:    parseFloat(item.Ratio),
					Industry: item.Industry,
					Trend:    item.Trend,
					Change:   parseFloat(item.Change),
					Type:     "stock",
				})
			}

			var bondHoldings []models.FundHolding
			for _, item := range resp.Datas.Bonds {
				bondHoldings = append(bondHoldings, models.FundHolding{
					Code:  item.Code,
					Name:  item.Name,
					Ratio: parseFloat(item.Ratio),
					Type:  "bond",
				})
			}

			if len(stockHoldings) > 0 || len(bondHoldings) > 0 {
				return stockHoldings, bondHoldings, nil
			}
			lastErr = fmt.Errorf("暂无持仓数据")
		} else {
			msg := resp.ErrMsg
			if msg == "" {
				msg = "获取基金持仓失败"
			}
			lastErr = fmt.Errorf(msg)
			if !shouldRetryFundAPI(resp.ErrCode, resp.ErrMsg) {
				break
			}
		}

		if attempt < maxRetries-1 {
			time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
		}
	}

	if fallbackStocks, err := api.fetchFundHoldingsFromF10(code); err == nil && len(fallbackStocks) > 0 {
		return fallbackStocks, nil, nil
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("获取基金持仓失败")
	}
	return nil, nil, lastErr
}

// GetFundNotices 获取基金公告
func (api *FundAPI) GetFundNotices(code string, count int) ([]models.FundNotice, error) {
	if count <= 0 {
		count = 20
	}
	params := map[string]string{
		"FCODE":     code,
		"pageIndex": "1",
		"pageSize":  fmt.Sprintf("%d", count),
	}

	const maxRetries = 3
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		var resp struct {
			Datas []struct {
				ID          string `json:"ID"`
				Title       string `json:"TITLE"`
				Category    string `json:"NEWCATEGORY"`
				PublishDate string `json:"PUBLISHDATE"`
				URL         string `json:"URL"`
			} `json:"Datas"`
			Success bool   `json:"Success"`
			ErrMsg  string `json:"ErrMsg"`
			ErrCode int    `json:"ErrCode"`
		}

		if err := api.doFundRequest("FundMNNoticeList", params, &resp); err != nil {
			lastErr = err
		} else if resp.Success {
			notices := make([]models.FundNotice, 0, len(resp.Datas))
			for _, item := range resp.Datas {
				notices = append(notices, models.FundNotice{
					ID:       item.ID,
					Title:    item.Title,
					Category: item.Category,
					Date:     item.PublishDate,
					Url:      item.URL,
				})
			}
			return notices, nil
		} else {
			msg := resp.ErrMsg
			if msg == "" {
				msg = "获取基金公告失败"
			}
			lastErr = fmt.Errorf(msg)
			if !shouldRetryFundAPI(resp.ErrCode, resp.ErrMsg) {
				break
			}
		}

		if attempt < maxRetries-1 {
			time.Sleep(time.Duration(attempt+1) * 500 * time.Millisecond)
		}
	}

	if notices, err := api.fetchFundNoticesFromAPI(code, count); err == nil && len(notices) > 0 {
		return notices, nil
	} else if err != nil && lastErr == nil {
		lastErr = err
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("获取基金公告失败")
	}
	return nil, lastErr
}

func (api *FundAPI) fetchJSONWithReferer(reqURL string, target interface{}) error {
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Referer", fundAPIF10Referer)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")
	resp, err := api.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	return json.Unmarshal(body, target)
}

func (api *FundAPI) fetchFundHistoryFromAPI(code string, count int) ([]models.FundPerformancePoint, error) {
	if count <= 0 {
		count = 60
	}
	reqURL := fmt.Sprintf("https://api.fund.eastmoney.com/f10/lsjz?fundCode=%s&pageIndex=1&pageSize=%d", code, count)
	var resp struct {
		ErrCode int    `json:"ErrCode"`
		ErrMsg  string `json:"ErrMsg"`
		Data    struct {
			History []struct {
				Date   string `json:"FSRQ"`
				Nav    string `json:"DWJZ"`
				AccNav string `json:"LJJZ"`
				Change string `json:"JZZZL"`
			} `json:"LSJZList"`
		} `json:"Data"`
	}

	if err := api.fetchJSONWithReferer(reqURL, &resp); err != nil {
		return nil, err
	}
	if resp.ErrCode != 0 {
		msg := resp.ErrMsg
		if msg == "" {
			msg = "获取基金历史净值失败"
		}
		return nil, fmt.Errorf(msg)
	}
	history := make([]models.FundPerformancePoint, 0, len(resp.Data.History))
	for _, item := range resp.Data.History {
		history = append(history, models.FundPerformancePoint{
			Date:          item.Date,
			Nav:           parseFloat(item.Nav),
			AccNav:        parseFloat(item.AccNav),
			ChangePercent: parseFloat(item.Change),
		})
	}
	return history, nil
}

func (api *FundAPI) fetchFundNoticesFromAPI(code string, count int) ([]models.FundNotice, error) {
	if count <= 0 {
		count = 20
	}
	reqURL := fmt.Sprintf("https://api.fund.eastmoney.com/f10/JJGG?fundcode=%s&pageIndex=1&pageSize=%d&type=0", code, count)
	var resp struct {
		ErrCode int    `json:"ErrCode"`
		ErrMsg  string `json:"ErrMsg"`
		Data    []struct {
			ID       string `json:"ID"`
			Title    string `json:"TITLE"`
			Category string `json:"NEWCATEGORY"`
			Date     string `json:"PUBLISHDATEDesc"`
		} `json:"Data"`
	}

	if err := api.fetchJSONWithReferer(reqURL, &resp); err != nil {
		return nil, err
	}
	if resp.ErrCode != 0 {
		msg := resp.ErrMsg
		if msg == "" {
			msg = "获取基金公告失败"
		}
		return nil, fmt.Errorf(msg)
	}

	notices := make([]models.FundNotice, 0, len(resp.Data))
	for _, item := range resp.Data {
		notices = append(notices, models.FundNotice{
			ID:       item.ID,
			Title:    item.Title,
			Category: item.Category,
			Date:     item.Date,
			Url:      fmt.Sprintf("http://fund.eastmoney.com/gonggao/%s,%s.html", code, item.ID),
		})
	}
	return notices, nil
}

// SearchFund 搜索基金（东方财富接口）
func (api *FundAPI) SearchFund(keyword string) ([]models.Fund, error) {
	url := fmt.Sprintf("https://fundsuggest.eastmoney.com/FundSearch/api/FundSearchAPI.ashx?m=1&key=%s", keyword)

	resp, err := api.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Datas []struct {
			Code string `json:"CODE"`
			Name string `json:"NAME"`
			Type string `json:"FundBaseInfo"`
		} `json:"Datas"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	var funds []models.Fund
	for _, item := range result.Datas {
		funds = append(funds, models.Fund{
			Code: item.Code,
			Name: item.Name,
			Type: item.Type,
		})
	}

	return funds, nil
}
