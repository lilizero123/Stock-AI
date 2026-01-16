<script setup>
import { ref, onMounted } from 'vue'
import {
  NCard,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NSwitch,
  NButton,
  NSpace,
  NDivider,
  NSelect,
  NAlert,
  NCollapse,
  NCollapseItem,
  useMessage
} from 'naive-ui'
import { GetConfig, SaveConfig } from '../../wailsjs/go/main/App'

const message = useMessage()
const loading = ref(false)

const config = ref({
  id: 1,
  refreshInterval: 15,
  proxyUrl: '',
  aiEnabled: false,
  aiModel: 'deepseek',
  aiApiKey: '',
  aiApiUrl: '',
  browserPath: '',
  // 付费API配置
  paidApiEnabled: false,
  paidApiProvider: '',
  paidApiKey: '',
  paidApiSecret: '',
  paidApiUrl: '',
  // Tushare配置
  tushareToken: '',
  tushareEnabled: false,
  // AKShare配置
  akshareEnabled: false,
  // 数据源优先级
  dataSourcePriority: 'tushare'
})

const aiModelOptions = [
  { label: 'DeepSeek', value: 'deepseek' },
  { label: 'OpenAI (GPT-4)', value: 'openai' },
  { label: 'Ollama (本地部署)', value: 'ollama' },
  { label: '硅基流动', value: 'siliconflow' },
  { label: '阿里云百炼', value: 'aliyun' },
  { label: '火山方舟', value: 'volcengine' }
]

const paidApiOptions = [
  { label: '东方财富开放平台', value: 'eastmoney' },
  { label: '同花顺iFinD', value: 'ths' },
  { label: '聚合数据', value: 'juhe' },
  { label: '万得Wind', value: 'wind' },
  { label: '自定义API', value: 'custom' }
]

const dataSourcePriorityOptions = [
  { label: 'Tushare 优先', value: 'tushare' },
  { label: 'AKShare 优先', value: 'akshare' }
]

const loadConfig = async () => {
  try {
    const data = await GetConfig()
    if (data) {
      config.value = { ...config.value, ...data }
    }
  } catch (e) {
    console.error('加载配置失败:', e)
  }
}

const saveConfig = async () => {
  loading.value = true
  try {
    await SaveConfig(config.value)
    message.success('保存成功')
  } catch (e) {
    message.error('保存失败: ' + e)
  } finally {
    loading.value = false
  }
}

const resetConfig = () => {
  config.value = {
    id: config.value.id,
    refreshInterval: 15,
    proxyUrl: '',
    aiEnabled: false,
    aiModel: 'deepseek',
    aiApiKey: '',
    aiApiUrl: '',
    browserPath: '',
    paidApiEnabled: false,
    paidApiProvider: '',
    paidApiKey: '',
    paidApiSecret: '',
    paidApiUrl: '',
    tushareToken: '',
    tushareEnabled: false,
    akshareEnabled: false,
    dataSourcePriority: 'tushare'
  }
  message.info('已重置为默认值，请点击保存')
}

onMounted(() => {
  loadConfig()
})
</script>

<template>
  <div class="settings-page">
    <n-card title="系统设置" :bordered="false">
      <n-form label-placement="left" label-width="140">
        <n-divider title-placement="left">基础设置</n-divider>

        <n-form-item label="行情刷新间隔">
          <n-input-number v-model:value="config.refreshInterval" :min="5" :max="120" style="width: 200px;">
            <template #suffix>秒</template>
          </n-input-number>
          <span style="margin-left: 12px; color: #999;">建议15秒以上，避免请求过于频繁</span>
        </n-form-item>

        <n-form-item label="HTTP代理">
          <n-input v-model:value="config.proxyUrl" placeholder="如 http://127.0.0.1:7890 或 socks5://127.0.0.1:1080" style="width: 400px;" />
        </n-form-item>

        <n-form-item label="浏览器路径">
          <n-input v-model:value="config.browserPath" placeholder="用于chromedp抓取数据，留空自动检测Edge/Chrome" style="width: 400px;" />
        </n-form-item>

        <n-alert type="info" style="margin-bottom: 16px;">
          <p>1. 行情数据仅在交易时间（周一至周五 9:30-11:30, 13:00-15:00）自动刷新</p>
          <p>2. 配置HTTP代理可以提高数据获取的稳定性</p>
          <p>3. 浏览器路径用于抓取部分需要JavaScript渲染的页面数据</p>
        </n-alert>

        <n-divider title-placement="left">财务数据源配置（Tushare / AKShare）</n-divider>

        <n-alert type="info" style="margin-bottom: 16px;">
          <p>Tushare 和 AKShare 是两个免费的财务数据源，用于获取股票的财务指标、资产负债表、现金流等数据。</p>
          <p>系统会自动在两个数据源之间切换，确保数据获取的稳定性。</p>
        </n-alert>

        <n-form-item label="启用 Tushare">
          <n-switch v-model:value="config.tushareEnabled" />
          <span style="margin-left: 12px; color: #999;">Tushare Pro 提供专业的财务数据</span>
        </n-form-item>

        <template v-if="config.tushareEnabled">
          <n-form-item label="Tushare Token">
            <n-input v-model:value="config.tushareToken" type="password" show-password-on="click" placeholder="访问 tushare.pro 注册获取Token" style="width: 400px;" />
          </n-form-item>
        </template>

        <n-form-item label="启用 AKShare">
          <n-switch v-model:value="config.akshareEnabled" />
          <span style="margin-left: 12px; color: #999;">AKShare 开源免费，无需Token（需要Python环境）</span>
        </n-form-item>

        <template v-if="config.tushareEnabled || config.akshareEnabled">
          <n-form-item label="数据源优先级">
            <n-select v-model:value="config.dataSourcePriority" :options="dataSourcePriorityOptions" style="width: 200px;" />
            <span style="margin-left: 12px; color: #999;">优先使用哪个数据源获取财务数据</span>
          </n-form-item>
        </template>

        <n-collapse style="margin-bottom: 16px;">
          <n-collapse-item title="Tushare / AKShare 配置说明" name="financial-guide">
            <div class="api-guide">
              <p><strong>Tushare Pro：</strong></p>
              <p>1. 访问 <a href="https://tushare.pro" target="_blank">tushare.pro</a> 注册账号</p>
              <p>2. 完成实名认证后获取 Token</p>
              <p>3. 免费用户每分钟可调用 60 次，足够日常使用</p>
              <p style="margin-top: 8px;"><strong>AKShare：</strong></p>
              <p>1. 需要本地安装 Python 3.7+ 环境</p>
              <p>2. 首次使用会自动安装 akshare 库</p>
              <p>3. 完全免费，但请求频率需要控制</p>
              <p style="margin-top: 8px; color: #18a058;"><strong>防封禁说明：</strong>系统已内置智能限流机制，会自动控制请求频率、添加随机延迟，最大程度保护您的IP不被封禁。</p>
            </div>
          </n-collapse-item>
        </n-collapse>

        <n-divider title-placement="left">付费数据API（可选）</n-divider>

        <n-form-item label="启用付费API">
          <n-switch v-model:value="config.paidApiEnabled" />
          <span style="margin-left: 12px; color: #999;">使用付费API可获得更稳定、更全面的数据</span>
        </n-form-item>

        <template v-if="config.paidApiEnabled">
          <n-form-item label="数据服务商">
            <n-select v-model:value="config.paidApiProvider" :options="paidApiOptions" placeholder="选择数据服务商" style="width: 300px;" />
          </n-form-item>

          <n-form-item label="API Key / Token">
            <n-input v-model:value="config.paidApiKey" type="password" show-password-on="click" placeholder="填写API Key或Token" style="width: 400px;" />
          </n-form-item>

          <n-form-item label="API Secret (可选)">
            <n-input v-model:value="config.paidApiSecret" type="password" show-password-on="click" placeholder="部分服务需要Secret" style="width: 400px;" />
          </n-form-item>

          <n-form-item label="API URL (可选)">
            <n-input v-model:value="config.paidApiUrl" placeholder="自定义API地址，留空使用默认地址" style="width: 400px;" />
          </n-form-item>

          <n-collapse>
            <n-collapse-item title="各服务商申请指南" name="guide">
              <div class="api-guide">
                <p><strong>东方财富开放平台：</strong>访问 data.eastmoney.com 注册申请</p>
                <p><strong>同花顺iFinD：</strong>访问 ft.10jqka.com.cn 申请机构账号</p>
                <p><strong>Tushare Pro：</strong>访问 tushare.pro 注册获取Token（有免费额度）</p>
                <p><strong>AKShare：</strong>开源免费，无需API Key，但建议配合代理使用</p>
                <p><strong>聚合数据：</strong>访问 juhe.cn 注册申请股票数据API</p>
                <p><strong>万得Wind：</strong>需要购买Wind终端或API服务</p>
              </div>
            </n-collapse-item>
          </n-collapse>

          <n-alert type="success" style="margin: 16px 0;">
            使用付费API的优势：请求频率限制更宽松、数据更全面准确、不易被封禁IP
          </n-alert>
        </template>

        <n-divider title-placement="left">AI 智能分析</n-divider>

        <n-form-item label="启用AI分析">
          <n-switch v-model:value="config.aiEnabled" />
          <span style="margin-left: 12px; color: #999;">开启后可使用AI分析股票</span>
        </n-form-item>

        <template v-if="config.aiEnabled">
          <n-form-item label="AI模型">
            <n-select v-model:value="config.aiModel" :options="aiModelOptions" style="width: 300px;" />
          </n-form-item>

          <n-form-item label="API Key">
            <n-input v-model:value="config.aiApiKey" type="password" show-password-on="click" placeholder="填写对应AI服务的API Key" style="width: 400px;" />
          </n-form-item>

          <n-form-item label="API URL (可选)">
            <n-input v-model:value="config.aiApiUrl" placeholder="自定义API地址，留空使用默认地址" style="width: 400px;" />
          </n-form-item>
        </template>

        <n-alert type="warning" style="margin-bottom: 16px;">
          所有API Key和密钥均存储在本地数据库中，不会上传到任何服务器。请妥善保管您的密钥。
        </n-alert>

        <n-divider />

        <n-form-item>
          <n-space>
            <n-button type="primary" :loading="loading" @click="saveConfig">保存设置</n-button>
            <n-button @click="resetConfig">恢复默认</n-button>
          </n-space>
        </n-form-item>
      </n-form>
    </n-card>
  </div>
</template>

<style scoped>
.settings-page {
  max-width: 800px;
}

.api-guide {
  font-size: 13px;
  line-height: 2;
  color: #999;
}

.api-guide p {
  margin: 4px 0;
}

.api-guide strong {
  color: #18a058;
}
</style>
