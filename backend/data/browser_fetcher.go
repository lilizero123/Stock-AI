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

const (
	browserTypeChromium = "chromium"
	browserTypeFirefox  = "firefox"
)

// BrowserFetcher 使用系统浏览器抓取网页内容（Edge/Chrome/Firefox/QQ浏览器等）
type BrowserFetcher struct {
	browserPath string
	browserName string
	browserType string
}

// NewBrowserFetcher 根据用户配置或自动检测创建抓取器
func NewBrowserFetcher(customPath string) *BrowserFetcher {
	path, name, browserType := detectBrowser(customPath)
	if path != "" {
		log.Printf("[BrowserFetcher] 使用浏览器: %s (%s)", name, path)
	} else {
		log.Printf("[BrowserFetcher] 未找到可用浏览器")
	}
	return &BrowserFetcher{
		browserPath: path,
		browserName: name,
		browserType: browserType,
	}
}

// Name 返回检测到的浏览器名称
func (f *BrowserFetcher) Name() string {
	if f.browserName != "" {
		return f.browserName
	}
	return "浏览器"
}

// IsAvailable 检查浏览器是否可用
func (f *BrowserFetcher) IsAvailable() bool {
	return f.browserPath != ""
}

// FetchContent 使用浏览器抓取网页内容
func (f *BrowserFetcher) FetchContent(pageURL string) (string, error) {
	if !f.IsAvailable() {
		return "", fmt.Errorf("未检测到可用浏览器")
	}

	if pageURL == "" {
		return "", fmt.Errorf("URL为空")
	}

	log.Printf("[BrowserFetcher] 开始抓取 (%s): %s", f.Name(), pageURL)

	tempDir, err := os.MkdirTemp("", "browser-fetch-*")
	if err != nil {
		return "", fmt.Errorf("创建临时目录失败: %v", err)
	}
	defer os.RemoveAll(tempDir)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	args := f.buildArgs(pageURL, tempDir, false)
	cmd := exec.CommandContext(ctx, f.browserPath, args...)
	output, err := cmd.Output()

	if len(output) > 0 {
		content := extractTextFromHTML(string(output))
		if content != "" && len(content) > 100 {
			log.Printf("[BrowserFetcher] 成功抓取内容，长度: %d", len(content))
			return content, nil
		}
	}

	if err != nil {
		log.Printf("[BrowserFetcher] 抓取失败: %v，输出长度: %d", err, len(output))
		if f.browserType == browserTypeChromium {
			return f.fetchWithVirtualTime(ctx, pageURL, tempDir)
		}
		return "", fmt.Errorf("%s 抓取失败: %w", f.Name(), err)
	}

	return "", fmt.Errorf("未能提取到页面内容")
}

func (f *BrowserFetcher) fetchWithVirtualTime(ctx context.Context, pageURL string, tempDir string) (string, error) {
	if f.browserType != browserTypeChromium {
		return "", fmt.Errorf("%s 不支持虚拟时间重试", f.Name())
	}

	args := f.buildArgs(pageURL, tempDir, true)
	cmd := exec.CommandContext(ctx, f.browserPath, args...)
	output, _ := cmd.Output()

	if len(output) > 0 {
		content := extractTextFromHTML(string(output))
		if content != "" && len(content) > 100 {
			log.Printf("[BrowserFetcher] 二次尝试成功，内容长度: %d", len(content))
			return content, nil
		}
	}

	return "", fmt.Errorf("二次抓取失败")
}

func (f *BrowserFetcher) buildArgs(pageURL string, tempDir string, useVirtualTime bool) []string {
	switch f.browserType {
	case browserTypeFirefox:
		args := []string{"--headless"}
		if tempDir != "" {
			args = append(args, "-profile", tempDir)
		}
		args = append(args, "--dump-dom", pageURL)
		return args
	default:
		args := []string{
			"--headless",
			"--disable-gpu",
			"--no-sandbox",
			"--disable-dev-shm-usage",
			"--disable-extensions",
			"--disable-plugins",
		}
		if tempDir != "" {
			args = append(args, "--user-data-dir="+tempDir)
		}
		if useVirtualTime {
			args = append(args, "--virtual-time-budget=5000")
		}
		args = append(args, "--dump-dom", pageURL)
		return args
	}
}

// extractTextFromHTML 从HTML中提取文本
func extractTextFromHTML(html string) string {
	scriptRe := regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	html = scriptRe.ReplaceAllString(html, "")

	styleRe := regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	html = styleRe.ReplaceAllString(html, "")

	commentRe := regexp.MustCompile(`<!--[\s\S]*?-->`)
	html = commentRe.ReplaceAllString(html, "")

	tagRe := regexp.MustCompile(`<[^>]+>`)
	text := tagRe.ReplaceAllString(html, " ")

	text = decodeHTMLEntitiesSimple(text)

	spaceRe := regexp.MustCompile(`\s+`)
	text = spaceRe.ReplaceAllString(text, " ")

	text = strings.TrimSpace(text)

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

type browserCandidate struct {
	name    string
	typ     string
	paths   []string
	lookups []string
}

func detectBrowser(customPath string) (string, string, string) {
	if path, browserType := validateCustomBrowser(customPath); path != "" {
		return path, guessBrowserNameFromPath(path), browserType
	}

	candidates := buildBrowserCandidates()
	for _, candidate := range candidates {
		for _, p := range candidate.paths {
			if pathExists(p) {
				return p, candidate.name, candidate.typ
			}
		}
		for _, exeName := range candidate.lookups {
			if exeName == "" {
				continue
			}
			if found, err := exec.LookPath(exeName); err == nil {
				return found, candidate.name, candidate.typ
			}
		}
	}

	return "", "", ""
}

func validateCustomBrowser(path string) (string, string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", ""
	}

	path = os.ExpandEnv(path)
	if !filepath.IsAbs(path) {
		if found, err := exec.LookPath(path); err == nil {
			path = found
		}
	}

	if !pathExists(path) {
		log.Printf("[BrowserFetcher] 自定义浏览器路径无效: %s", path)
		return "", ""
	}

	return path, guessBrowserTypeFromPath(path)
}

func guessBrowserNameFromPath(path string) string {
	lower := strings.ToLower(filepath.Base(path))
	switch {
	case strings.Contains(lower, "msedge"):
		return "Edge"
	case strings.Contains(lower, "chrome") && strings.Contains(lower, "360"):
		return "360浏览器"
	case strings.Contains(lower, "chrome"):
		return "Chrome"
	case strings.Contains(lower, "firefox"):
		return "Firefox"
	case strings.Contains(lower, "qqbrowser"):
		return "QQ浏览器"
	case strings.Contains(lower, "quark"):
		return "夸克浏览器"
	default:
		return "自定义浏览器"
	}
}

func guessBrowserTypeFromPath(path string) string {
	lower := strings.ToLower(path)
	if strings.Contains(lower, "firefox") {
		return browserTypeFirefox
	}
	return browserTypeChromium
}

func pathExists(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func buildBrowserCandidates() []browserCandidate {
	var candidates []browserCandidate

	candidates = append(candidates, browserCandidate{
		name: "Edge",
		typ:  browserTypeChromium,
		paths: []string{
			`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
			`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
			joinEnvPath("LOCALAPPDATA", `Microsoft\Edge\Application\msedge.exe`),
			joinEnvPath("PROGRAMFILES(X86)", `Microsoft\Edge\Application\msedge.exe`),
			joinEnvPath("PROGRAMFILES", `Microsoft\Edge\Application\msedge.exe`),
		},
		lookups: []string{"msedge.exe"},
	})

	candidates = append(candidates, browserCandidate{
		name: "Chrome",
		typ:  browserTypeChromium,
		paths: []string{
			`C:\Program Files\Google\Chrome\Application\chrome.exe`,
			`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
			joinEnvPath("LOCALAPPDATA", `Google\Chrome\Application\chrome.exe`),
		},
		lookups: []string{"chrome.exe"},
	})

	candidates = append(candidates, browserCandidate{
		name: "Firefox",
		typ:  browserTypeFirefox,
		paths: []string{
			`C:\Program Files\Mozilla Firefox\firefox.exe`,
			`C:\Program Files (x86)\Mozilla Firefox\firefox.exe`,
			joinEnvPath("PROGRAMFILES", `Mozilla Firefox\firefox.exe`),
			joinEnvPath("PROGRAMFILES(X86)", `Mozilla Firefox\firefox.exe`),
		},
		lookups: []string{"firefox.exe"},
	})

	candidates = append(candidates, browserCandidate{
		name: "QQ浏览器",
		typ:  browserTypeChromium,
		paths: []string{
			`C:\Program Files\Tencent\QQBrowser\QQBrowser.exe`,
			`C:\Program Files (x86)\Tencent\QQBrowser\QQBrowser.exe`,
			joinEnvPath("PROGRAMFILES", `Tencent\QQBrowser\QQBrowser.exe`),
			joinEnvPath("PROGRAMFILES(X86)", `Tencent\QQBrowser\QQBrowser.exe`),
		},
		lookups: []string{"QQBrowser.exe"},
	})

	candidates = append(candidates, browserCandidate{
		name: "360浏览器",
		typ:  browserTypeChromium,
		paths: []string{
			`C:\Program Files\360\360Chrome\Chrome\Application\360chrome.exe`,
			`C:\Program Files (x86)\360\360Chrome\Chrome\Application\360chrome.exe`,
			joinEnvPath("LOCALAPPDATA", `360Chrome\Chrome\Application\360chrome.exe`),
		},
		lookups: []string{"360chrome.exe"},
	})

	candidates = append(candidates, browserCandidate{
		name: "夸克浏览器",
		typ:  browserTypeChromium,
		paths: []string{
			`C:\Program Files\QuarkBrowser\Application\quark.exe`,
			`C:\Program Files (x86)\QuarkBrowser\Application\quark.exe`,
			joinEnvPath("LOCALAPPDATA", `QuarkBrowser\Application\quark.exe`),
		},
		lookups: []string{"quark.exe"},
	})

	return candidates
}

func joinEnvPath(envKey string, rel string) string {
	base := os.Getenv(envKey)
	if base == "" {
		return ""
	}
	return filepath.Join(base, rel)
}
