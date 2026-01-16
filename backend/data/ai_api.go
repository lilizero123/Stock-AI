package data

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"stock-ai/backend/models"
)

// AIClient AI客户端
type AIClient struct {
	config *models.Config
	client *http.Client
}

// NewAIClient 创建AI客户端
func NewAIClient(config *models.Config) *AIClient {
	return &AIClient{
		config: config,
		client: &http.Client{
			Timeout: 60 * time.Second, // 60秒超时（流式响应需要较长时间）
		},
	}
}

// ChatMessage 聊天消息
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest 聊天请求
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Stream      bool          `json:"stream"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
}

// ChatResponse 聊天响应
type ChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error,omitempty"`
}

// getAPIConfig 获取API配置
func (c *AIClient) getAPIConfig() (baseURL, apiKey, model string) {
	switch c.config.AiModel {
	case "deepseek":
		baseURL = "https://api.deepseek.com/v1"
		model = "deepseek-chat"
	case "qwen", "aliyun":
		baseURL = "https://dashscope.aliyuncs.com/compatible-mode/v1"
		model = "qwen-turbo"
	case "glm", "zhipu":
		baseURL = "https://open.bigmodel.cn/api/paas/v4"
		model = "glm-4-flash"
	case "ernie", "wenxin":
		baseURL = "https://aip.baidubce.com/rpc/2.0/ai_custom/v1/wenxinworkshop/chat/completions"
		model = "ernie-speed-128k"
	case "siliconflow":
		baseURL = "https://api.siliconflow.cn/v1"
		model = "deepseek-ai/DeepSeek-V2.5"
	case "ollama":
		baseURL = "http://localhost:11434/v1"
		model = "qwen2.5:7b"
	case "openai":
		baseURL = "https://api.openai.com/v1"
		model = "gpt-4o-mini"
	default:
		baseURL = "https://api.deepseek.com/v1"
		model = "deepseek-chat"
	}

	// 使用自定义URL
	if c.config.AiApiUrl != "" {
		baseURL = c.config.AiApiUrl
	}

	apiKey = c.config.AiApiKey
	return
}

// Chat 发送聊天请求（非流式）
func (c *AIClient) Chat(messages []ChatMessage) (string, error) {
	baseURL, apiKey, model := c.getAPIConfig()

	reqBody := ChatRequest{
		Model:       model,
		Messages:    messages,
		Stream:      false,
		Temperature: 0.7,
		MaxTokens:   4096,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("序列化请求失败: %v", err)
	}

	req, err := http.NewRequest("POST", baseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return "", fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("请求失败: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("读取响应失败: %v", err)
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(body, &chatResp); err != nil {
		return "", fmt.Errorf("解析响应失败: %v, 原始响应: %s", err, string(body))
	}

	if chatResp.Error != nil {
		return "", fmt.Errorf("API错误: %s", chatResp.Error.Message)
	}

	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("没有返回结果")
	}

	return chatResp.Choices[0].Message.Content, nil
}

// ChatStream 发送聊天请求（流式）
func (c *AIClient) ChatStream(messages []ChatMessage) (<-chan string, error) {
	baseURL, apiKey, model := c.getAPIConfig()

	log.Printf("[AI] ChatStream开始: model=%s, baseURL=%s", model, baseURL)

	reqBody := ChatRequest{
		Model:       model,
		Messages:    messages,
		Stream:      true,
		Temperature: 0.7,
		MaxTokens:   4096,
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %v", err)
	}

	log.Printf("[AI] 请求体长度: %d", len(jsonData))

	// 流式请求不设置超时，由读取循环控制
	client := &http.Client{}

	req, err := http.NewRequest("POST", baseURL+"/chat/completions", bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "text/event-stream")

	log.Printf("[AI] 发送请求...")
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[AI] 请求失败: %v", err)
		return nil, fmt.Errorf("请求失败（请检查网络连接）: %v", err)
	}

	log.Printf("[AI] 响应状态码: %d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		log.Printf("[AI] API错误: %s", string(body))
		return nil, fmt.Errorf("API返回错误（状态码 %d）: %s", resp.StatusCode, string(body))
	}

	ch := make(chan string, 100)

	go func() {
		defer close(ch)
		defer resp.Body.Close()

		log.Printf("[AI] 开始读取流式响应...")
		reader := bufio.NewReader(resp.Body)
		contentCount := 0
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					log.Printf("[AI] 读取错误: %v", err)
					ch <- fmt.Sprintf("\n\n[读取响应出错: %v]", err)
				}
				log.Printf("[AI] 流式响应结束，共收到 %d 个内容块", contentCount)
				return
			}

			line = strings.TrimSpace(line)
			if line == "" || line == "data: [DONE]" {
				continue
			}

			if !strings.HasPrefix(line, "data: ") {
				continue
			}

			data := strings.TrimPrefix(line, "data: ")
			var streamResp ChatResponse
			if err := json.Unmarshal([]byte(data), &streamResp); err != nil {
				continue
			}

			if len(streamResp.Choices) > 0 {
				content := streamResp.Choices[0].Delta.Content
				if content != "" {
					contentCount++
					ch <- content
				}
			}
		}
	}()

	return ch, nil
}

// BuildStockAnalysisPrompt 构建股票分析提示词
func BuildStockAnalysisPrompt(stock *models.StockPrice, klines []models.KLineData, reports []models.ResearchReport, notices []models.StockNotice) string {
	var sb strings.Builder

	sb.WriteString("请分析以下股票的投资价值：\n\n")

	// 基本信息
	sb.WriteString(fmt.Sprintf("## 股票信息\n"))
	sb.WriteString(fmt.Sprintf("- 代码：%s\n", stock.Code))
	sb.WriteString(fmt.Sprintf("- 名称：%s\n", stock.Name))
	sb.WriteString(fmt.Sprintf("- 现价：%.2f\n", stock.Price))
	sb.WriteString(fmt.Sprintf("- 涨跌幅：%.2f%%\n", stock.ChangePercent))
	sb.WriteString(fmt.Sprintf("- 成交量：%d\n", stock.Volume))
	sb.WriteString(fmt.Sprintf("- 成交额：%.2f\n\n", stock.Amount))

	// K线数据
	if len(klines) > 0 {
		sb.WriteString("## 近期K线数据（最近10天）\n")
		count := len(klines)
		if count > 10 {
			count = 10
		}
		for i := len(klines) - count; i < len(klines); i++ {
			k := klines[i]
			sb.WriteString(fmt.Sprintf("- %s: 开%.2f 高%.2f 低%.2f 收%.2f 量%d\n",
				k.Date, k.Open, k.High, k.Low, k.Close, k.Volume))
		}
		sb.WriteString("\n")
	}

	// 研报信息
	if len(reports) > 0 {
		sb.WriteString("## 最新研报\n")
		count := len(reports)
		if count > 5 {
			count = 5
		}
		for i := 0; i < count; i++ {
			r := reports[i]
			sb.WriteString(fmt.Sprintf("- [%s] %s - %s（%s）\n", r.PublishDate, r.Title, r.OrgName, r.Rating))
		}
		sb.WriteString("\n")
	}

	// 公告信息
	if len(notices) > 0 {
		sb.WriteString("## 最新公告\n")
		count := len(notices)
		if count > 5 {
			count = 5
		}
		for i := 0; i < count; i++ {
			n := notices[i]
			sb.WriteString(fmt.Sprintf("- [%s] %s（%s）\n", n.Date, n.Title, n.Type))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(`
请从以下几个方面进行分析：
1. **技术面分析**：根据K线走势分析短期趋势
2. **基本面分析**：根据研报和公告分析公司基本面
3. **风险提示**：指出潜在风险
4. **综合评估**：给出看多/中性/看空的分析观点及理由

重要声明：以上分析由AI生成，仅供学习研究参考，不构成任何投资建议。投资有风险，入市需谨慎。
`)

	return sb.String()
}

// BuildChatSystemPrompt 构建聊天系统提示词
func BuildChatSystemPrompt() string {
	return `你是一个股票数据分析助手，具有以下能力：
1. 分析股票的技术面和基本面数据
2. 解读财经新闻和公告内容
3. 提供数据分析参考和风险提示
4. 回答股票相关的各种问题

请注意：
- 你的所有分析仅供学习研究参考，不构成任何投资建议
- 你不具备证券投资咨询资格，不提供投资咨询服务
- 投资有风险，入市需谨慎
- 回答要客观、有理有据
- 使用中文回答`
}

// BuildSummaryPrompt 构建摘要提示词
func BuildSummaryPrompt(content string, contentType string) string {
	var typeDesc string
	switch contentType {
	case "report":
		typeDesc = "研报"
	case "notice":
		typeDesc = "公告"
	default:
		typeDesc = "内容"
	}

	return fmt.Sprintf(`请对以下%s进行摘要，提取关键信息：

%s

请按以下格式输出：
1. **核心观点**：一句话概括主要内容
2. **关键信息**：列出3-5个要点
3. **影响分析**：对股价可能的影响（利好/利空/中性）
`, typeDesc, content)
}

// BuildRecommendPrompt 构建市场分析提示词
func BuildRecommendPrompt(indexes []models.MarketIndex, industries []models.IndustryRank, moneyFlow []models.MoneyFlow) string {
	var sb strings.Builder

	sb.WriteString("请根据以下市场数据，分析当前市场热点方向：\n\n")

	// 市场指数
	if len(indexes) > 0 {
		sb.WriteString("## 市场指数\n")
		for _, idx := range indexes {
			sb.WriteString(fmt.Sprintf("- %s: %.2f (%.2f%%)\n", idx.Name, idx.Price, idx.ChangePercent))
		}
		sb.WriteString("\n")
	}

	// 行业排行
	if len(industries) > 0 {
		sb.WriteString("## 行业涨幅排行（前10）\n")
		count := len(industries)
		if count > 10 {
			count = 10
		}
		for i := 0; i < count; i++ {
			ind := industries[i]
			sb.WriteString(fmt.Sprintf("- %s: %.2f%% (领涨股: %s)\n", ind.Name, ind.ChangePercent, ind.LeadStock))
		}
		sb.WriteString("\n")
	}

	// 资金流向
	if len(moneyFlow) > 0 {
		sb.WriteString("## 主力资金流入（前10）\n")
		count := len(moneyFlow)
		if count > 10 {
			count = 10
		}
		for i := 0; i < count; i++ {
			mf := moneyFlow[i]
			sb.WriteString(fmt.Sprintf("- %s(%s): 主力净流入%.2f亿\n", mf.Name, mf.Code, mf.MainFlow/100000000))
		}
		sb.WriteString("\n")
	}

	sb.WriteString(`
请分析：
1. **市场整体情况**：当前市场处于什么状态
2. **热点板块**：哪些行业表现活跃
3. **资金动向**：主力资金在布局什么方向
4. **风险提示**：当前市场需要注意的风险

重要声明：以上分析由AI生成，仅供学习研究参考，不构成任何投资建议，不作为买卖依据。投资有风险，入市需谨慎。
`)

	return sb.String()
}
