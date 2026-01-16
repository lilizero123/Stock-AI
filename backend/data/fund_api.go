package data

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"time"

	"stock-ai/backend/models"
)

// FundAPI 基金数据API
type FundAPI struct {
	client *http.Client
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
