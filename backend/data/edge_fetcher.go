package data

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// EdgeFetcher 使用Edge浏览器抓取网页内容
type EdgeFetcher struct {
	edgePath string
}

// NewEdgeFetcher 创建Edge抓取器
func NewEdgeFetcher() *EdgeFetcher {
	return &EdgeFetcher{
		edgePath: findEdgePath(),
	}
}

// findEdgePath 查找Edge浏览器路径
func findEdgePath() string {
	// Windows上Edge的常见安装路径
	paths := []string{
		`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
		`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
		filepath.Join(os.Getenv("LOCALAPPDATA"), `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(os.Getenv("PROGRAMFILES(X86)"), `Microsoft\Edge\Application\msedge.exe`),
		filepath.Join(os.Getenv("PROGRAMFILES"), `Microsoft\Edge\Application\msedge.exe`),
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			log.Printf("[Edge] 找到Edge浏览器: %s", p)
			return p
		}
	}

	log.Printf("[Edge] 未找到Edge浏览器")
	return ""
}

// IsAvailable 检查Edge是否可用
func (f *EdgeFetcher) IsAvailable() bool {
	return f.edgePath != ""
}

// FetchContent 使用Edge抓取网页内容
func (f *EdgeFetcher) FetchContent(pageURL string) (string, error) {
	if !f.IsAvailable() {
		return "", fmt.Errorf("Edge浏览器不可用")
	}

	if pageURL == "" {
		return "", fmt.Errorf("URL为空")
	}

	log.Printf("[Edge] 开始抓取: %s", pageURL)

	// 创建临时目录用于存储用户数据
	tempDir, err := os.MkdirTemp("", "edge-fetch-*")
	if err != nil {
		return "", fmt.Errorf("创建临时目录失败: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 使用Edge的headless模式
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Edge命令行参数
	args := []string{
		"--headless",
		"--disable-gpu",
		"--no-sandbox",
		"--disable-dev-shm-usage",
		"--disable-extensions",
		"--disable-plugins",
		"--user-data-dir=" + tempDir,
		"--dump-dom",
		pageURL,
	}

	cmd := exec.CommandContext(ctx, f.edgePath, args...)
	// 使用CombinedOutput来获取stdout，即使有错误也能获取输出
	output, err := cmd.Output()

	// Edge可能返回非零退出码但仍有有效输出
	// 只要有输出内容就尝试解析
	if len(output) > 0 {
		content := extractTextFromHTML(string(output))
		if content != "" && len(content) > 100 {
			log.Printf("[Edge] 成功抓取内容，长度: %d", len(content))
			return content, nil
		}
	}

	if err != nil {
		log.Printf("[Edge] dump-dom失败: %v, 输出长度: %d", err, len(output))
		// 尝试第二种方式
		return f.fetchWithVirtualTime(ctx, pageURL, tempDir)
	}

	return "", fmt.Errorf("未能提取到页面内容")
}

// fetchWithVirtualTime 使用virtual-time-budget等待JS执行
func (f *EdgeFetcher) fetchWithVirtualTime(ctx context.Context, pageURL string, tempDir string) (string, error) {
	args := []string{
		"--headless",
		"--disable-gpu",
		"--no-sandbox",
		"--disable-dev-shm-usage",
		"--user-data-dir=" + tempDir,
		"--virtual-time-budget=5000",
		"--dump-dom",
		pageURL,
	}

	cmd := exec.CommandContext(ctx, f.edgePath, args...)
	output, _ := cmd.Output()

	if len(output) > 0 {
		content := extractTextFromHTML(string(output))
		if content != "" && len(content) > 100 {
			log.Printf("[Edge] 第二次尝试成功，内容长度: %d", len(content))
			return content, nil
		}
	}

	return "", fmt.Errorf("Edge抓取失败")
}

// extractTextFromHTML 从HTML中提取文本
func extractTextFromHTML(html string) string {
	// 移除script标签
	scriptRe := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	html = scriptRe.ReplaceAllString(html, "")

	// 移除style标签
	styleRe := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	html = styleRe.ReplaceAllString(html, "")

	// 移除HTML注释
	commentRe := regexp.MustCompile(`<!--[\s\S]*?-->`)
	html = commentRe.ReplaceAllString(html, "")

	// 移除所有HTML标签
	tagRe := regexp.MustCompile(`<[^>]+>`)
	text := tagRe.ReplaceAllString(html, " ")

	// 解码HTML实体
	text = decodeHTMLEntitiesSimple(text)

	// 清理多余空白
	spaceRe := regexp.MustCompile(`\s+`)
	text = spaceRe.ReplaceAllString(text, " ")

	// 去除首尾空白
	text = strings.TrimSpace(text)

	// 限制长度
	if len(text) > 10000 {
		text = text[:10000] + "..."
	}

	return text
}

// decodeHTMLEntitiesSimple 解码常见HTML实体
func decodeHTMLEntitiesSimple(text string) string {
	entities := map[string]string{
		"&nbsp;":   " ",
		"&lt;":     "<",
		"&gt;":     ">",
		"&amp;":    "&",
		"&quot;":   "\"",
		"&apos;":   "'",
		"&#39;":    "'",
		"&ldquo;":  "\"",
		"&rdquo;":  "\"",
		"&lsquo;":  "'",
		"&rsquo;":  "'",
		"&mdash;":  "-",
		"&ndash;":  "-",
		"&hellip;": "...",
	}
	for entity, char := range entities {
		text = strings.ReplaceAll(text, entity, char)
	}
	return text
}
