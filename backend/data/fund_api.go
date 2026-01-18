package data

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"stock-ai/backend/models"
)

const (
	fundAPIBaseURL = "https://fundmobapi.eastmoney.com/FundMNewApi"
)

// FundAPI 基金数据API
type FundAPI struct {
	client *http.Client
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

// NewFundAPI 创建基金API实例
func NewFundAPI() *FundAPI {
	return &FundAPI{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// GetFundPrice 获取基金估值（天天基金接口）
func (api *FundAPI) GetFundPrice(codes []string) (map[string]*models.FundPrice, error) {
	result := make(map[string]*models.FundPrice)

	for _, code := range codes {
		price, err := api.getFundEstimate(code)
		if err != nil {
			continue
		}
		result[code] = price
	}

	return result, nil
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
	}

	if err := api.doFundRequest("FundMNBasicInformation", map[string]string{"FCODE": code}, &resp); err != nil {
		return nil, err
	}

	if !resp.Success {
		if resp.ErrMsg != "" {
			return nil, fmt.Errorf(resp.ErrMsg)
		}
		return nil, fmt.Errorf("获取基金信息失败")
	}

	data := resp.Datas
	return &models.FundDetail{
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
	}, nil
}

// GetFundHistory 获取基金历史净值
func (api *FundAPI) GetFundHistory(code string, count int) ([]models.FundPerformancePoint, error) {
	if count <= 0 {
		count = 60
	}
	var resp struct {
		Datas []struct {
			FSRQ  string `json:"FSRQ"`
			DWJZ  string `json:"DWJZ"`
			LJJZ  string `json:"LJJZ"`
			JZZZL string `json:"JZZZL"`
		} `json:"Datas"`
		Success bool   `json:"Success"`
		ErrMsg  string `json:"ErrMsg"`
	}

	params := map[string]string{
		"FCODE":     code,
		"pageSize":  fmt.Sprintf("%d", count),
		"pageIndex": "1",
	}

	if err := api.doFundRequest("FundMNHisNetList", params, &resp); err != nil {
		return nil, err
	}
	if !resp.Success {
		if resp.ErrMsg != "" {
			return nil, fmt.Errorf(resp.ErrMsg)
		}
		return nil, fmt.Errorf("获取基金历史净值失败")
	}

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
}

// GetFundHoldings 获取基金持仓
func (api *FundAPI) GetFundHoldings(code string) ([]models.FundHolding, []models.FundHolding, error) {
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
	}

	if err := api.doFundRequest("FundMNInverstPosition", map[string]string{"FCODE": code}, &resp); err != nil {
		return nil, nil, err
	}
	if !resp.Success {
		if resp.ErrMsg != "" {
			return nil, nil, fmt.Errorf(resp.ErrMsg)
		}
		return nil, nil, fmt.Errorf("获取基金持仓失败")
	}

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

	return stockHoldings, bondHoldings, nil
}

// GetFundNotices 获取基金公告
func (api *FundAPI) GetFundNotices(code string, count int) ([]models.FundNotice, error) {
	if count <= 0 {
		count = 20
	}
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
	}

	params := map[string]string{
		"FCODE":     code,
		"pageIndex": "1",
		"pageSize":  fmt.Sprintf("%d", count),
	}

	if err := api.doFundRequest("FundMNNoticeList", params, &resp); err != nil {
		return nil, err
	}
	if !resp.Success {
		if resp.ErrMsg != "" {
			return nil, fmt.Errorf(resp.ErrMsg)
		}
		return nil, fmt.Errorf("获取基金公告失败")
	}

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
