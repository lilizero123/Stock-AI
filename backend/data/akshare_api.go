package data

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// AKShareClient AKShare数据客户端
// AKShare是Python库，通过本地HTTP服务调用
type AKShareClient struct {
	baseURL     string
	client      *http.Client
	rateLimiter *RateLimiter
	cache       *DataCache
	serverCmd   *exec.Cmd
	serverPort  int
	mu          sync.RWMutex
	running     bool
}

var (
	globalAKShareClient *AKShareClient
	akshareClientOnce   sync.Once
)

// GetAKShareClient 获取全局AKShare客户端
func GetAKShareClient() *AKShareClient {
	akshareClientOnce.Do(func() {
		globalAKShareClient = NewAKShareClient()
	})
	return globalAKShareClient
}

// NewAKShareClient 创建AKShare客户端
func NewAKShareClient() *AKShareClient {
	return &AKShareClient{
		serverPort: 8765,
		baseURL:    "http://127.0.0.1:8765",
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
		rateLimiter: GetRateLimiter(),
		cache: &DataCache{
			data: make(map[string]*CacheItem),
		},
	}
}

// IsRunning 检查服务是否运行
func (c *AKShareClient) IsRunning() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.running
}

// CheckServer 检查服务器是否可用
func (c *AKShareClient) CheckServer() bool {
	resp, err := c.client.Get(c.baseURL + "/health")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// StartServer 启动AKShare服务
func (c *AKShareClient) StartServer() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.running {
		return nil
	}

	// 检查Python是否安装
	pythonPath, err := findPython()
	if err != nil {
		return fmt.Errorf("未找到Python: %v", err)
	}

	// 创建服务脚本
	scriptPath, err := c.createServerScript()
	if err != nil {
		return fmt.Errorf("创建服务脚本失败: %v", err)
	}

	// 启动服务
	c.serverCmd = exec.Command(pythonPath, scriptPath, fmt.Sprintf("%d", c.serverPort))
	c.serverCmd.Stdout = os.Stdout
	c.serverCmd.Stderr = os.Stderr

	if err := c.serverCmd.Start(); err != nil {
		return fmt.Errorf("启动AKShare服务失败: %v", err)
	}

	// 等待服务启动
	for i := 0; i < 30; i++ {
		time.Sleep(time.Second)
		if c.CheckServer() {
			c.running = true
			log.Printf("[AKShare] 服务已启动，端口: %d", c.serverPort)
			return nil
		}
	}

	// 启动超时，杀掉进程
	if c.serverCmd.Process != nil {
		c.serverCmd.Process.Kill()
	}

	return fmt.Errorf("AKShare服务启动超时")
}

// StopServer 停止AKShare服务
func (c *AKShareClient) StopServer() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.serverCmd != nil && c.serverCmd.Process != nil {
		c.serverCmd.Process.Kill()
		c.serverCmd = nil
	}
	c.running = false
	log.Printf("[AKShare] 服务已停止")
}

// findPython 查找Python路径
func findPython() (string, error) {
	// 尝试不同的Python命令
	pythonCmds := []string{"python3", "python", "py"}

	if runtime.GOOS == "windows" {
		pythonCmds = []string{"python", "py", "python3"}
	}

	for _, cmd := range pythonCmds {
		path, err := exec.LookPath(cmd)
		if err == nil {
			// 验证版本
			out, err := exec.Command(path, "--version").Output()
			if err == nil {
				log.Printf("[AKShare] 找到Python: %s (%s)", path, string(out))
				return path, nil
			}
		}
	}

	return "", fmt.Errorf("未找到Python，请确保已安装Python 3.7+")
}

// createServerScript 创建AKShare服务脚本
func (c *AKShareClient) createServerScript() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		homeDir = "."
	}

	scriptDir := filepath.Join(homeDir, ".stock-ai")
	if err := os.MkdirAll(scriptDir, 0755); err != nil {
		return "", err
	}

	scriptPath := filepath.Join(scriptDir, "akshare_server.py")

	script := `#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
AKShare HTTP服务
为stock-ai提供财务数据接口
"""

import sys
import json
import traceback
from http.server import HTTPServer, BaseHTTPRequestHandler
from urllib.parse import urlparse, parse_qs

# 检查并安装akshare
try:
    import akshare as ak
    print(f"[AKShare] 版本: {ak.__version__}")
except ImportError:
    print("[AKShare] 正在安装akshare...")
    import subprocess
    subprocess.check_call([sys.executable, "-m", "pip", "install", "akshare", "-i", "https://pypi.tuna.tsinghua.edu.cn/simple"])
    import akshare as ak
    print(f"[AKShare] 安装完成，版本: {ak.__version__}")


class AKShareHandler(BaseHTTPRequestHandler):
    """AKShare请求处理器"""

    def log_message(self, format, *args):
        print(f"[AKShare] {args[0]}")

    def send_json(self, data, status=200):
        self.send_response(status)
        self.send_header('Content-Type', 'application/json; charset=utf-8')
        self.send_header('Access-Control-Allow-Origin', '*')
        self.end_headers()
        self.wfile.write(json.dumps(data, ensure_ascii=False, default=str).encode('utf-8'))

    def do_GET(self):
        parsed = urlparse(self.path)
        path = parsed.path
        params = parse_qs(parsed.query)

        # 获取单值参数
        params = {k: v[0] if len(v) == 1 else v for k, v in params.items()}

        try:
            if path == '/health':
                self.send_json({'status': 'ok', 'version': ak.__version__})

            elif path == '/financial':
                # 获取财务数据
                code = params.get('code', '')
                data = self.get_financial_data(code)
                self.send_json({'code': 0, 'data': data})

            elif path == '/balance':
                # 获取资产负债表
                code = params.get('code', '')
                data = self.get_balance_sheet(code)
                self.send_json({'code': 0, 'data': data})

            elif path == '/income':
                # 获取利润表
                code = params.get('code', '')
                data = self.get_income_statement(code)
                self.send_json({'code': 0, 'data': data})

            elif path == '/cashflow':
                # 获取现金流量表
                code = params.get('code', '')
                data = self.get_cashflow(code)
                self.send_json({'code': 0, 'data': data})

            elif path == '/indicators':
                # 获取财务指标
                code = params.get('code', '')
                data = self.get_financial_indicators(code)
                self.send_json({'code': 0, 'data': data})

            elif path == '/valuation':
                # 获取估值数据
                code = params.get('code', '')
                data = self.get_valuation(code)
                self.send_json({'code': 0, 'data': data})

            else:
                self.send_json({'code': -1, 'msg': 'Unknown endpoint'}, 404)

        except Exception as e:
            traceback.print_exc()
            self.send_json({'code': -1, 'msg': str(e)}, 500)

    def convert_code(self, code):
        """转换股票代码格式"""
        if code.startswith('sh') or code.startswith('sz') or code.startswith('bj'):
            return code[2:]
        return code

    def get_financial_data(self, code):
        """获取综合财务数据"""
        code = self.convert_code(code)
        result = {}

        try:
            # 获取财务指标
            df = ak.stock_financial_analysis_indicator(symbol=code)
            if df is not None and len(df) > 0:
                latest = df.iloc[0].to_dict()
                result['indicators'] = latest
        except Exception as e:
            print(f"[AKShare] 获取财务指标失败: {e}")

        try:
            # 获取主要财务指标
            df = ak.stock_financial_abstract(symbol=code)
            if df is not None and len(df) > 0:
                result['abstract'] = df.head(4).to_dict('records')
        except Exception as e:
            print(f"[AKShare] 获取财务摘要失败: {e}")

        return result

    def get_balance_sheet(self, code):
        """获取资产负债表"""
        code = self.convert_code(code)
        try:
            df = ak.stock_balance_sheet_by_report_em(symbol=code)
            if df is not None and len(df) > 0:
                # 只返回最近4期
                return df.head(4).to_dict('records')
        except Exception as e:
            print(f"[AKShare] 获取资产负债表失败: {e}")
        return []

    def get_income_statement(self, code):
        """获取利润表"""
        code = self.convert_code(code)
        try:
            df = ak.stock_profit_sheet_by_report_em(symbol=code)
            if df is not None and len(df) > 0:
                return df.head(4).to_dict('records')
        except Exception as e:
            print(f"[AKShare] 获取利润表失败: {e}")
        return []

    def get_cashflow(self, code):
        """获取现金流量表"""
        code = self.convert_code(code)
        try:
            df = ak.stock_cash_flow_sheet_by_report_em(symbol=code)
            if df is not None and len(df) > 0:
                return df.head(4).to_dict('records')
        except Exception as e:
            print(f"[AKShare] 获取现金流量表失败: {e}")
        return []

    def get_financial_indicators(self, code):
        """获取财务指标"""
        code = self.convert_code(code)
        try:
            df = ak.stock_financial_analysis_indicator(symbol=code)
            if df is not None and len(df) > 0:
                return df.head(4).to_dict('records')
        except Exception as e:
            print(f"[AKShare] 获取财务指标失败: {e}")
        return []

    def get_valuation(self, code):
        """获取估值数据"""
        code = self.convert_code(code)
        result = {}

        try:
            # 获取个股信息
            df = ak.stock_individual_info_em(symbol=code)
            if df is not None and len(df) > 0:
                info = {}
                for _, row in df.iterrows():
                    info[row['item']] = row['value']
                result['info'] = info
        except Exception as e:
            print(f"[AKShare] 获取个股信息失败: {e}")

        return result


def main():
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 8765
    server = HTTPServer(('127.0.0.1', port), AKShareHandler)
    print(f"[AKShare] 服务启动在 http://127.0.0.1:{port}")
    server.serve_forever()


if __name__ == '__main__':
    main()
`

	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		return "", err
	}

	return scriptPath, nil
}

// request 发送请求（带限流保护）
func (c *AKShareClient) request(endpoint string, params map[string]string) (map[string]interface{}, error) {
	if !c.IsRunning() && !c.CheckServer() {
		// 尝试启动服务
		if err := c.StartServer(); err != nil {
			return nil, fmt.Errorf("AKShare服务未运行: %v", err)
		}
	}

	var result map[string]interface{}
	var err error

	err = c.rateLimiter.ExecuteWithRateLimit("akshare.local", func() error {
		result, err = c.doRequest(endpoint, params)
		return err
	})

	return result, err
}

// doRequest 实际执行请求
func (c *AKShareClient) doRequest(endpoint string, params map[string]string) (map[string]interface{}, error) {
	u, err := url.Parse(c.baseURL + endpoint)
	if err != nil {
		return nil, err
	}

	q := u.Query()
	for k, v := range params {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()

	log.Printf("[AKShare] 请求: %s", u.String())

	resp, err := c.client.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %v", err)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("解析响应失败: %v", err)
	}

	if code, ok := result["code"].(float64); ok && code != 0 {
		msg := result["msg"]
		return nil, fmt.Errorf("API错误: %v", msg)
	}

	return result, nil
}

// getCache 获取缓存
func (c *AKShareClient) getCache(key string) (interface{}, bool) {
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
func (c *AKShareClient) setCache(key string, data interface{}, ttl time.Duration) {
	c.cache.mu.Lock()
	defer c.cache.mu.Unlock()

	c.cache.data[key] = &CacheItem{
		Data:     data,
		ExpireAt: time.Now().Add(ttl),
	}
}

// GetFinancialData 获取财务数据
func (c *AKShareClient) GetFinancialData(stockCode string) (*FinancialData, error) {
	cacheKey := fmt.Sprintf("akshare_financial_%s", stockCode)

	if cached, ok := c.getCache(cacheKey); ok {
		log.Printf("[AKShare] 使用缓存的财务数据: %s", stockCode)
		return cached.(*FinancialData), nil
	}

	result, err := c.request("/financial", map[string]string{"code": stockCode})
	if err != nil {
		return nil, err
	}

	data := &FinancialData{
		Code: stockCode,
	}

	// 解析返回的数据
	if dataMap, ok := result["data"].(map[string]interface{}); ok {
		if indicators, ok := dataMap["indicators"].(map[string]interface{}); ok {
			if v, ok := indicators["净资产收益率(%)"].(float64); ok {
				data.ROE = v
			}
			if v, ok := indicators["总资产收益率(%)"].(float64); ok {
				data.ROA = v
			}
			if v, ok := indicators["销售毛利率(%)"].(float64); ok {
				data.GrossMargin = v
			}
			if v, ok := indicators["销售净利率(%)"].(float64); ok {
				data.NetMargin = v
			}
			if v, ok := indicators["资产负债率(%)"].(float64); ok {
				data.DebtRatio = v
			}
			if v, ok := indicators["流动比率"].(float64); ok {
				data.CurrentRatio = v
			}
			if v, ok := indicators["速动比率"].(float64); ok {
				data.QuickRatio = v
			}
		}
	}

	c.setCache(cacheKey, data, time.Hour)
	return data, nil
}

// GetBalanceSheet 获取资产负债表
func (c *AKShareClient) GetBalanceSheet(stockCode string) ([]map[string]interface{}, error) {
	cacheKey := fmt.Sprintf("akshare_balance_%s", stockCode)

	if cached, ok := c.getCache(cacheKey); ok {
		return cached.([]map[string]interface{}), nil
	}

	result, err := c.request("/balance", map[string]string{"code": stockCode})
	if err != nil {
		return nil, err
	}

	var data []map[string]interface{}
	if dataList, ok := result["data"].([]interface{}); ok {
		for _, item := range dataList {
			if m, ok := item.(map[string]interface{}); ok {
				data = append(data, m)
			}
		}
	}

	c.setCache(cacheKey, data, time.Hour)
	return data, nil
}

// GetIncomeStatement 获取利润表
func (c *AKShareClient) GetIncomeStatement(stockCode string) ([]map[string]interface{}, error) {
	cacheKey := fmt.Sprintf("akshare_income_%s", stockCode)

	if cached, ok := c.getCache(cacheKey); ok {
		return cached.([]map[string]interface{}), nil
	}

	result, err := c.request("/income", map[string]string{"code": stockCode})
	if err != nil {
		return nil, err
	}

	var data []map[string]interface{}
	if dataList, ok := result["data"].([]interface{}); ok {
		for _, item := range dataList {
			if m, ok := item.(map[string]interface{}); ok {
				data = append(data, m)
			}
		}
	}

	c.setCache(cacheKey, data, time.Hour)
	return data, nil
}

// GetCashFlow 获取现金流量表
func (c *AKShareClient) GetCashFlow(stockCode string) ([]map[string]interface{}, error) {
	cacheKey := fmt.Sprintf("akshare_cashflow_%s", stockCode)

	if cached, ok := c.getCache(cacheKey); ok {
		return cached.([]map[string]interface{}), nil
	}

	result, err := c.request("/cashflow", map[string]string{"code": stockCode})
	if err != nil {
		return nil, err
	}

	var data []map[string]interface{}
	if dataList, ok := result["data"].([]interface{}); ok {
		for _, item := range dataList {
			if m, ok := item.(map[string]interface{}); ok {
				data = append(data, m)
			}
		}
	}

	c.setCache(cacheKey, data, time.Hour)
	return data, nil
}

// GetValuation 获取估值数据
func (c *AKShareClient) GetValuation(stockCode string) (map[string]interface{}, error) {
	cacheKey := fmt.Sprintf("akshare_valuation_%s", stockCode)

	if cached, ok := c.getCache(cacheKey); ok {
		return cached.(map[string]interface{}), nil
	}

	result, err := c.request("/valuation", map[string]string{"code": stockCode})
	if err != nil {
		return nil, err
	}

	var data map[string]interface{}
	if dataMap, ok := result["data"].(map[string]interface{}); ok {
		data = dataMap
	}

	c.setCache(cacheKey, data, 30*time.Minute)
	return data, nil
}
