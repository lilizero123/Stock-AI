<script setup>
import { ref, onMounted, onUnmounted, nextTick } from 'vue'
import {
  NCard,
  NDataTable,
  NButton,
  NInput,
  NInputNumber,
  NSpace,
  NTag,
  NModal,
  NForm,
  NFormItem,
  NTabs,
  NTabPane,
  NEmpty,
  NAlert,
  NSpin,
  NScrollbar,
  NDropdown,
  NRadioGroup,
  NRadio,
  NSelect,
  NSwitch,
  NList,
  NListItem,
  NThing,
  NPopconfirm,
  useMessage
} from 'naive-ui'
import { h } from 'vue'
import {
  GetStockList,
  AddStock,
  RemoveStock,
  GetStockPrice,
  GetResearchReports,
  GetStockNotices,
  GetTradingTimeInfo,
  OpenURL,
  AIAnalyzeStockStream,
  AIChatStream,
  AISummarizeContentStream,
  AIAnalyzeByTypeStream,
  GetConfig,
  GetPositionByStock,
  AddPosition,
  UpdatePosition,
  DeletePosition,
  GetStockAlerts,
  AddStockAlert,
  DeleteStockAlert,
  ToggleStockAlert,
  ResetStockAlert,
  CheckStockAlerts
} from '../../wailsjs/go/main/App'
import { EventsOn } from '../../wailsjs/runtime/runtime'

const message = useMessage()
const stocks = ref([])
const loading = ref(false)
const showAddModal = ref(false)
const showDetailModal = ref(false)
const showWebModal = ref(false)
const showAIModal = ref(false)
const webUrl = ref('')
const webTitle = ref('')
const newStockCode = ref('')
const selectedStock = ref(null)
const reports = ref([])
const notices = ref([])
const detailLoading = ref(false)
let refreshTimer = null

// AI相关
const aiEnabled = ref(false)
const aiLoading = ref(false)
const aiResponse = ref('')
const aiQuestion = ref('')
const aiMessages = ref([])
const aiScrollbarRef = ref(null)

// AI摘要相关
const showSummaryModal = ref(false)
const summaryTitle = ref('')
const summaryType = ref('')
const summaryContent = ref('')
const summaryLoading = ref(false)
const summaryScrollbarRef = ref(null)
const summaryUrl = ref('')
const manualContent = ref('')
const showManualInput = ref(false)

// 专业分析相关
const showProAnalysisModal = ref(false)
const proAnalysisType = ref('fundamental')
const proMasterStyle = ref('')
const proAnalysisContent = ref('')
const proAnalysisLoading = ref(false)
const proAnalysisScrollbarRef = ref(null)

// 持仓管理相关
const showPositionModal = ref(false)
const positionLoading = ref(false)
const currentPosition = ref(null)
const positionForm = ref({
  stockCode: '',
  stockName: '',
  buyPrice: null,
  buyDate: '',
  quantity: null,
  costPrice: null,
  targetPrice: null,
  stopLossPrice: null,
  notes: ''
})

// 提醒设置相关
const showAlertModal = ref(false)
const alertLoading = ref(false)
const stockAlerts = ref([])
const alertForm = ref({
  alertType: 'change',  // change: 涨跌提醒, price: 股价提醒
  targetValue: 3,       // 目标值
  condition: 'above'    // above: 高于, below: 低于
})
let alertCheckTimer = null
const eventOffFns = []

// 分析类型选项
const analysisTypeOptions = [
  { label: '基本面分析', value: 'fundamental', desc: '财务数据、估值指标、盈利能力' },
  { label: '技术面分析', value: 'technical', desc: 'K线形态、技术指标、趋势分析' },
  { label: '情绪面分析', value: 'sentiment', desc: '市场情绪、资金流向、舆情分析' },
  { label: '大师模式', value: 'master', desc: '模拟投资大师的选股风格' }
]

// 大师风格选项（按分析类型分类）
const fundamentalMasters = [
  { label: '巴菲特', value: 'buffett', desc: '价值投资、护城河、长期持有' },
  { label: '格雷厄姆', value: 'graham', desc: '安全边际、低估值、防御投资' },
  { label: '彼得·林奇', value: 'lynch', desc: '成长投资、PEG估值、生活选股' },
  { label: '查理·芒格', value: 'munger', desc: '多元思维、能力圈、逆向思考' },
  { label: '菲利普·费雪', value: 'fisher', desc: '成长股投资、闲聊法、长期持有' }
]

const technicalMasters = [
  { label: '利弗莫尔', value: 'livermore', desc: '趋势交易、关键点位、资金管理' },
  { label: '威廉·江恩', value: 'gann', desc: '江恩理论、时间周期、几何角度' },
  { label: '艾略特', value: 'elliott', desc: '波浪理论、市场周期、斐波那契' },
  { label: '约翰·墨菲', value: 'murphy', desc: '跨市场分析、技术指标、图表形态' }
]

const sentimentMasters = [
  { label: '索罗斯', value: 'soros', desc: '反身性理论、市场情绪、宏观对冲' },
  { label: '霍华德·马克斯', value: 'marks', desc: '周期思维、风险控制、逆向投资' },
  { label: '邓普顿', value: 'templeton', desc: '极度悲观点买入、全球视野、逆向投资' },
  { label: '科斯托拉尼', value: 'kostolany', desc: '市场心理、固执投资者、鸡蛋理论' }
]

// 所有大师选项（用于标题显示）
const allMasterOptions = [...fundamentalMasters, ...technicalMasters, ...sentimentMasters]

const columns = [
  { title: '代码', key: 'code', width: 100 },
  { title: '名称', key: 'name', width: 100 },
  {
    title: '现价',
    key: 'price',
    width: 90,
    render: (row) => {
      const val = row.price
      if (!val) return '-'
      const color = row.changePercent > 0 ? '#f5222d' : row.changePercent < 0 ? '#52c41a' : '#fff'
      return h('span', { style: { color, fontWeight: 'bold' } }, val.toFixed(2))
    }
  },
  {
    title: '涨跌幅',
    key: 'changePercent',
    width: 90,
    render: (row) => {
      const val = row.changePercent
      if (val === undefined || val === null) return '-'
      const color = val > 0 ? '#f5222d' : val < 0 ? '#52c41a' : '#999'
      return h('span', { style: { color, fontWeight: 'bold' } }, (val > 0 ? '+' : '') + val.toFixed(2) + '%')
    }
  },
  {
    title: '涨跌额',
    key: 'change',
    width: 80,
    render: (row) => {
      const val = row.change
      if (val === undefined || val === null) return '-'
      const color = val > 0 ? '#f5222d' : val < 0 ? '#52c41a' : '#999'
      return h('span', { style: { color } }, (val > 0 ? '+' : '') + val.toFixed(2))
    }
  },
  { title: '今开', key: 'open', width: 80, render: (row) => row.open?.toFixed(2) || '-' },
  { title: '最高', key: 'high', width: 80, render: (row) => row.high?.toFixed(2) || '-' },
  { title: '最低', key: 'low', width: 80, render: (row) => row.low?.toFixed(2) || '-' },
  { title: '昨收', key: 'preClose', width: 80, render: (row) => row.preClose?.toFixed(2) || '-' },
  { title: '成交量', key: 'volume', width: 100, render: (row) => formatVolume(row.volume) },
  { title: '成交额', key: 'amount', width: 100, render: (row) => formatAmount(row.amount) },
  {
    title: '操作',
    key: 'actions',
    width: 400,
    render: (row) => h(NSpace, {}, {
      default: () => [
        h(NButton, {
          size: 'small',
          type: 'info',
          onClick: () => showDetail(row)
        }, { default: () => '详情' }),
        h(NButton, {
          size: 'small',
          type: 'tertiary',
          onClick: () => openPositionModal(row)
        }, { default: () => '持仓' }),
        h(NButton, {
          size: 'small',
          type: 'primary',
          onClick: () => openAlertModal(row)
        }, { default: () => '提醒' }),
        h(NButton, {
          size: 'small',
          type: 'warning',
          onClick: () => openAIAnalysis(row)
        }, { default: () => 'AI分析' }),
        h(NDropdown, {
          trigger: 'click',
          options: [
            { label: '基本面分析', key: 'fundamental', children: [
              { label: '标准分析', key: 'fundamental' },
              { type: 'divider', key: 'd-f1' },
              { label: '巴菲特视角', key: 'fundamental-buffett' },
              { label: '格雷厄姆视角', key: 'fundamental-graham' },
              { label: '彼得林奇视角', key: 'fundamental-lynch' },
              { label: '芒格视角', key: 'fundamental-munger' },
              { label: '费雪视角', key: 'fundamental-fisher' }
            ]},
            { label: '技术面分析', key: 'technical', children: [
              { label: '标准分析', key: 'technical' },
              { type: 'divider', key: 'd-t1' },
              { label: '利弗莫尔视角', key: 'technical-livermore' },
              { label: '江恩视角', key: 'technical-gann' },
              { label: '艾略特视角', key: 'technical-elliott' },
              { label: '墨菲视角', key: 'technical-murphy' }
            ]},
            { label: '情绪面分析', key: 'sentiment', children: [
              { label: '标准分析', key: 'sentiment' },
              { type: 'divider', key: 'd-s1' },
              { label: '索罗斯视角', key: 'sentiment-soros' },
              { label: '马克斯视角', key: 'sentiment-marks' },
              { label: '邓普顿视角', key: 'sentiment-templeton' },
              { label: '科斯托拉尼视角', key: 'sentiment-kostolany' }
            ]}
          ],
          onSelect: (key) => openProAnalysis(row, key)
        }, {
          default: () => h(NButton, {
            size: 'small',
            type: 'success'
          }, { default: () => '专业分析' })
        }),
        h(NButton, {
          size: 'small',
          type: 'error',
          onClick: () => handleRemove(row.code)
        }, { default: () => '删除' })
      ]
    })
  }
]

const reportColumns = [
  {
    title: '标题',
    key: 'title',
    ellipsis: { tooltip: true },
    render: (row) => h('a', {
      style: { color: '#18a058', cursor: 'pointer' },
      onClick: () => openWebPage(row.url, row.title)
    }, row.title)
  },
  { title: '机构', key: 'orgName', width: 100 },
  { title: '评级', key: 'rating', width: 80 },
  { title: '日期', key: 'publishDate', width: 100 },
  {
    title: '操作',
    key: 'actions',
    width: 80,
    render: (row) => h(NButton, {
      size: 'tiny',
      type: 'warning',
      onClick: () => openAISummary(row.title, 'report', row.url, row.infoCode, '')
    }, { default: () => 'AI解读' })
  }
]

const noticeColumns = [
  {
    title: '标题',
    key: 'title',
    ellipsis: { tooltip: true },
    render: (row) => h('a', {
      style: { color: '#18a058', cursor: 'pointer' },
      onClick: () => openWebPage(row.url, row.title)
    }, row.title)
  },
  { title: '类型', key: 'type', width: 100 },
  { title: '日期', key: 'date', width: 100 },
  {
    title: '操作',
    key: 'actions',
    width: 80,
    render: (row) => h(NButton, {
      size: 'tiny',
      type: 'warning',
      onClick: () => openAISummary(row.title, 'notice', row.url, '', row.artCode)
    }, { default: () => 'AI解读' })
  }
]

// 检查AI配置
const checkAIConfig = async () => {
  try {
    const config = await GetConfig()
    aiEnabled.value = config.aiEnabled && config.aiApiKey
  } catch (e) {
    console.error('获取配置失败:', e)
  }
}

// 打开AI分析弹窗
const openAIAnalysis = async (stock) => {
  selectedStock.value = stock
  showAIModal.value = true
  aiMessages.value = []
  aiResponse.value = ''

  if (!aiEnabled.value) {
    message.warning('AI功能未启用，请在设置中配置')
    return
  }

  // 自动开始分析
  startAIAnalysis()
}

// 开始AI分析
const startAIAnalysis = async () => {
  if (!selectedStock.value || aiLoading.value) return

  aiLoading.value = true
  aiResponse.value = ''

  // 添加用户消息
  aiMessages.value.push({
    role: 'user',
    content: `分析股票 ${selectedStock.value.name}(${selectedStock.value.code})`
  })

  // 添加AI响应占位
  aiMessages.value.push({
    role: 'assistant',
    content: ''
  })

  scrollToBottom()

  try {
    await AIAnalyzeStockStream(selectedStock.value.code)
  } catch (e) {
    message.error('AI分析失败: ' + e)
    aiLoading.value = false
  }
}

// 发送AI问题
const sendAIQuestion = async () => {
  if (!aiQuestion.value.trim() || aiLoading.value || !selectedStock.value) return

  const question = aiQuestion.value.trim()
  aiQuestion.value = ''

  aiLoading.value = true
  aiResponse.value = ''

  // 添加用户消息
  aiMessages.value.push({
    role: 'user',
    content: question
  })

  // 添加AI响应占位
  aiMessages.value.push({
    role: 'assistant',
    content: ''
  })

  scrollToBottom()

  try {
    await AIChatStream({
      message: question,
      sessionId: 'stock-' + selectedStock.value.code,
      stockCode: selectedStock.value.code
    })
  } catch (e) {
    message.error('发送失败: ' + e)
    aiLoading.value = false
  }
}

// 滚动到底部
const scrollToBottom = () => {
  nextTick(() => {
    if (aiScrollbarRef.value) {
      aiScrollbarRef.value.scrollTo({ top: 999999, behavior: 'smooth' })
    }
  })
}

// 处理AI流式响应
const handleAIStream = (content) => {
  aiResponse.value += content
  if (aiMessages.value.length > 0) {
    aiMessages.value[aiMessages.value.length - 1].content = aiResponse.value
  }
  scrollToBottom()
}

const handleAIDone = () => {
  aiLoading.value = false
  aiResponse.value = ''
}

const handleAIError = (error) => {
  aiLoading.value = false
  message.error(error)
  if (aiMessages.value.length > 0 && aiMessages.value[aiMessages.value.length - 1].role === 'assistant') {
    aiMessages.value[aiMessages.value.length - 1].content = `错误: ${error}`
  }
}

// AI摘要相关
const summaryInfoCode = ref('')
const summaryArtCode = ref('')

const openAISummary = (title, type, url, infoCode = '', artCode = '') => {
  console.log('[AI摘要] 开始:', { title, type, url, infoCode, artCode })
  if (!aiEnabled.value) {
    message.warning('AI功能未启用，请在设置中配置')
    return
  }
  summaryTitle.value = title
  summaryType.value = type
  summaryUrl.value = url || ''
  summaryInfoCode.value = infoCode || ''
  summaryArtCode.value = artCode || ''
  summaryContent.value = ''
  summaryLoading.value = true
  showSummaryModal.value = true
  manualContent.value = ''
  showManualInput.value = false

  // 开始AI摘要，传递URL和代码以获取实际内容
  console.log('[AI摘要] 调用AISummarizeContentStream...')
  AISummarizeContentStream(title, type, url || '', infoCode || '', artCode || '', selectedStock.value?.code || '', '').then(() => {
    console.log('[AI摘要] AISummarizeContentStream调用成功')
  }).catch(e => {
    console.log('[AI摘要] AISummarizeContentStream调用失败:', e)
    message.error('AI摘要失败: ' + e)
    summaryLoading.value = false
  })
}

// 使用手动粘贴的内容重新分析
const analyzeWithManualContent = () => {
  if (!manualContent.value.trim()) {
    message.warning('请先粘贴内容')
    return
  }

  summaryContent.value = ''
  summaryLoading.value = true
  showManualInput.value = false

  // 传递手动内容进行分析
  AISummarizeContentStream(summaryTitle.value, summaryType.value, '', '', '', '', manualContent.value.trim()).catch(e => {
    message.error('AI摘要失败: ' + e)
    summaryLoading.value = false
  })
}

const handleSummaryStream = (content) => {
  console.log('[AI摘要] 收到流式内容:', content?.substring(0, 50))
  summaryContent.value += content
  nextTick(() => {
    if (summaryScrollbarRef.value) {
      summaryScrollbarRef.value.scrollTo({ top: 999999, behavior: 'smooth' })
    }
  })
}

const handleSummaryDone = () => {
  console.log('[AI摘要] 完成')
  summaryLoading.value = false
}

const handleSummaryError = (error) => {
  console.log('[AI摘要] 错误:', error)
  summaryLoading.value = false
  message.error(error)
  summaryContent.value = `错误: ${error}`
}

// 专业分析相关函数
const openProAnalysis = (stock, key) => {
  if (!aiEnabled.value) {
    message.warning('AI功能未启用，请在设置中配置')
    return
  }

  selectedStock.value = stock
  proAnalysisContent.value = ''
  proAnalysisLoading.value = true
  showProAnalysisModal.value = true

  // 解析key，格式: analysisType 或 analysisType-masterStyle
  const parts = key.split('-')
  proAnalysisType.value = parts[0] // fundamental, technical, sentiment
  proMasterStyle.value = parts.length > 1 ? parts.slice(1).join('-') : '' // 大师风格（如果有）

  // 开始分析
  startProAnalysis()
}

const startProAnalysis = async () => {
  if (!selectedStock.value || proAnalysisLoading.value) return

  proAnalysisContent.value = ''
  proAnalysisLoading.value = true

  try {
    await AIAnalyzeByTypeStream(
      selectedStock.value.code,
      proAnalysisType.value,
      proMasterStyle.value
    )
  } catch (e) {
    message.error('专业分析失败: ' + e)
    proAnalysisLoading.value = false
  }
}

const handleProAnalysisStream = (content) => {
  proAnalysisContent.value += content
  nextTick(() => {
    if (proAnalysisScrollbarRef.value) {
      proAnalysisScrollbarRef.value.scrollTo({ top: 999999, behavior: 'smooth' })
    }
  })
}

const handleProAnalysisDone = () => {
  proAnalysisLoading.value = false
}

const handleProAnalysisError = (error) => {
  proAnalysisLoading.value = false
  message.error(error)
  proAnalysisContent.value = `错误: ${error}`
}

// 持仓管理相关函数
const openPositionModal = async (stock) => {
  selectedStock.value = stock
  positionLoading.value = true
  showPositionModal.value = true

  // 重置表单
  positionForm.value = {
    stockCode: stock.code,
    stockName: stock.name,
    buyPrice: null,
    buyDate: new Date().toISOString().split('T')[0],
    quantity: null,
    costPrice: null,
    targetPrice: null,
    stopLossPrice: null,
    notes: ''
  }

  try {
    // 尝试获取已有持仓
    const position = await GetPositionByStock(stock.code)
    if (position && position.id) {
      currentPosition.value = position
      positionForm.value = {
        stockCode: position.stockCode,
        stockName: position.stockName,
        buyPrice: position.buyPrice,
        buyDate: position.buyDate,
        quantity: position.quantity,
        costPrice: position.costPrice,
        targetPrice: position.targetPrice,
        stopLossPrice: position.stopLossPrice,
        notes: position.notes
      }
    } else {
      currentPosition.value = null
    }
  } catch (e) {
    // 没有持仓记录
    currentPosition.value = null
  }

  positionLoading.value = false
}

const savePosition = async () => {
  if (!positionForm.value.buyPrice || !positionForm.value.quantity) {
    message.warning('请填写买入价格和持仓数量')
    return
  }

  positionLoading.value = true

  try {
    const data = {
      ...positionForm.value,
      buyPrice: parseFloat(positionForm.value.buyPrice) || 0,
      quantity: parseInt(positionForm.value.quantity) || 0,
      costPrice: parseFloat(positionForm.value.costPrice) || parseFloat(positionForm.value.buyPrice) || 0,
      targetPrice: parseFloat(positionForm.value.targetPrice) || 0,
      stopLossPrice: parseFloat(positionForm.value.stopLossPrice) || 0
    }

    if (currentPosition.value && currentPosition.value.id) {
      // 更新持仓
      await UpdatePosition({ ...currentPosition.value, ...data })
      message.success('持仓信息已更新')
    } else {
      // 添加持仓
      await AddPosition(data)
      message.success('持仓信息已保存')
    }

    showPositionModal.value = false
  } catch (e) {
    message.error('保存失败: ' + e)
  }

  positionLoading.value = false
}

const deleteCurrentPosition = async () => {
  if (!currentPosition.value || !currentPosition.value.id) {
    message.warning('没有持仓记录')
    return
  }

  positionLoading.value = true

  try {
    await DeletePosition(currentPosition.value.id)
    message.success('持仓记录已删除')
    showPositionModal.value = false
    currentPosition.value = null
  } catch (e) {
    message.error('删除失败: ' + e)
  }

  positionLoading.value = false
}

// 计算当前盈亏
const calculateProfit = () => {
  if (!selectedStock.value || !positionForm.value.costPrice || !positionForm.value.quantity) {
    return null
  }

  const currentPrice = selectedStock.value.price
  const costPrice = parseFloat(positionForm.value.costPrice)
  const quantity = parseInt(positionForm.value.quantity)

  if (!currentPrice || !costPrice || !quantity) return null

  const profitAmount = (currentPrice - costPrice) * quantity
  const profitPercent = ((currentPrice - costPrice) / costPrice) * 100

  return {
    amount: profitAmount,
    percent: profitPercent
  }
}

// 获取分析类型标题
const getAnalysisTypeLabel = () => {
  const typeLabels = {
    fundamental: '基本面分析',
    technical: '技术面分析',
    sentiment: '情绪面分析'
  }

  const baseLabel = typeLabels[proAnalysisType.value] || '专业分析'

  if (proMasterStyle.value) {
    const master = allMasterOptions.find(m => m.value === proMasterStyle.value)
    return master ? `${baseLabel} - ${master.label}视角` : baseLabel
  }

  return baseLabel
}

// ========== 提醒相关函数 ==========

// 打开提醒设置弹窗
const openAlertModal = async (stock) => {
  selectedStock.value = stock
  showAlertModal.value = true
  alertLoading.value = true

  // 重置表单
  alertForm.value = {
    alertType: 'change',
    targetValue: 3,
    condition: 'above'
  }

  try {
    // 加载该股票的提醒列表
    const alerts = await GetStockAlerts(stock.code)
    stockAlerts.value = alerts || []
  } catch (e) {
    console.error('加载提醒列表失败:', e)
    stockAlerts.value = []
  }

  alertLoading.value = false
}

// 添加提醒
const addAlert = async () => {
  if (!selectedStock.value) return

  if (!alertForm.value.targetValue || alertForm.value.targetValue <= 0) {
    message.warning('请输入有效的目标值')
    return
  }

  alertLoading.value = true

  try {
    await AddStockAlert({
      stockCode: selectedStock.value.code,
      stockName: selectedStock.value.name,
      alertType: alertForm.value.alertType,
      targetValue: alertForm.value.targetValue,
      condition: alertForm.value.condition
    })
    message.success('提醒添加成功')

    // 刷新列表
    const alerts = await GetStockAlerts(selectedStock.value.code)
    stockAlerts.value = alerts || []
  } catch (e) {
    message.error('添加提醒失败: ' + e)
  }

  alertLoading.value = false
}

// 删除提醒
const deleteAlert = async (id) => {
  try {
    await DeleteStockAlert(id)
    message.success('提醒已删除')

    // 刷新列表
    if (selectedStock.value) {
      const alerts = await GetStockAlerts(selectedStock.value.code)
      stockAlerts.value = alerts || []
    }
  } catch (e) {
    message.error('删除失败: ' + e)
  }
}

// 切换提醒启用状态
const toggleAlert = async (alert) => {
  try {
    await ToggleStockAlert(alert.id, !alert.enabled)
    alert.enabled = !alert.enabled
    message.success(alert.enabled ? '提醒已启用' : '提醒已禁用')
  } catch (e) {
    message.error('操作失败: ' + e)
  }
}

// 重置已触发的提醒
const resetAlert = async (alert) => {
  try {
    await ResetStockAlert(alert.id)
    alert.triggered = false
    alert.triggeredAt = null
    message.success('提醒已重置')
  } catch (e) {
    message.error('重置失败: ' + e)
  }
}

// 获取提醒类型文字
const getAlertTypeText = (alert) => {
  if (alert.alertType === 'change') {
    return alert.condition === 'above'
      ? `涨幅达 ${alert.targetValue}%`
      : `跌幅达 ${alert.targetValue}%`
  } else {
    return alert.condition === 'above'
      ? `股价高于 ${alert.targetValue} 元`
      : `股价低于 ${alert.targetValue} 元`
  }
}

// 处理提醒触发事件
const handleAlertTriggered = (notification) => {
  // 显示通知弹窗
  message.warning(notification.message, {
    duration: 10000,
    closable: true
  })
}

// 启动提醒检查定时器
const startAlertCheck = () => {
  // 每5秒检查一次提醒（使用本地缓存数据，不会有性能问题）
  const checkAlerts = async () => {
    try {
      await CheckStockAlerts()
    } catch (e) {
      console.error('检查提醒失败:', e)
    }
  }

  // 立即检查一次
  checkAlerts()

  // 设置定时器，每5秒检查一次
  alertCheckTimer = setInterval(checkAlerts, 5000)
}

// 打开网页
const openWebPage = (url, title) => {
  if (!url) {
    message.warning('暂无详情链接')
    return
  }
  webUrl.value = url
  webTitle.value = title
  showWebModal.value = true
}

// 在外部浏览器打开
const openInBrowser = () => {
  if (webUrl.value) {
    OpenURL(webUrl.value)
  }
}

const formatVolume = (val) => {
  if (!val) return '-'
  if (val >= 100000000) return (val / 100000000).toFixed(2) + '亿'
  if (val >= 10000) return (val / 10000).toFixed(2) + '万'
  return val.toString()
}

const formatAmount = (val) => {
  if (!val) return '-'
  if (val >= 100000000) return (val / 100000000).toFixed(2) + '亿'
  if (val >= 10000) return (val / 10000).toFixed(2) + '万'
  return val.toFixed(2)
}

const loadStocks = async () => {
  loading.value = true
  try {
    const list = await GetStockList()
    stocks.value = list || []
  } catch (e) {
    console.error('加载股票列表失败:', e)
  } finally {
    loading.value = false
  }
}

const refreshPrices = async () => {
  if (stocks.value.length === 0) return
  try {
    const codes = stocks.value.map(s => s.code)
    const prices = await GetStockPrice(codes)
    if (prices) {
      stocks.value = stocks.value.map(s => ({
        ...s,
        ...prices[s.code]
      }))
    }
  } catch (e) {
    console.error('刷新行情失败:', e)
  }
}

const handleAdd = async () => {
  if (!newStockCode.value.trim()) {
    message.warning('请输入股票代码')
    return
  }
  try {
    await AddStock(newStockCode.value.trim())
    message.success('添加成功')
    showAddModal.value = false
    newStockCode.value = ''
    await loadStocks()
    await refreshPrices()
  } catch (e) {
    message.error('添加失败: ' + e)
  }
}

const handleRemove = async (code) => {
  try {
    await RemoveStock(code)
    message.success('删除成功')
    await loadStocks()
  } catch (e) {
    message.error('删除失败: ' + e)
  }
}

const showDetail = async (stock) => {
  selectedStock.value = stock
  showDetailModal.value = true
  detailLoading.value = true

  try {
    const [reportData, noticeData] = await Promise.all([
      GetResearchReports(stock.code),
      GetStockNotices(stock.code)
    ])
    reports.value = reportData || []
    notices.value = noticeData || []
  } catch (e) {
    console.error('加载详情失败:', e)
  } finally {
    detailLoading.value = false
  }
}

// 在详情弹窗中打开AI分析
const openAIFromDetail = () => {
  showDetailModal.value = false
  openAIAnalysis(selectedStock.value)
}

onMounted(async () => {
  await checkAIConfig()
  await loadStocks()
  await refreshPrices()
  startSmartRefresh()

  // 启动提醒检查
  startAlertCheck()

  // 监听事件并记录取消函数
  eventOffFns.push(EventsOn('ai-chat-stream', handleAIStream))
  eventOffFns.push(EventsOn('ai-chat-done', handleAIDone))
  eventOffFns.push(EventsOn('ai-chat-error', handleAIError))

  eventOffFns.push(EventsOn('ai-summary-stream', handleSummaryStream))
  eventOffFns.push(EventsOn('ai-summary-done', handleSummaryDone))
  eventOffFns.push(EventsOn('ai-summary-error', handleSummaryError))

  eventOffFns.push(EventsOn('ai-analysis-stream', handleProAnalysisStream))
  eventOffFns.push(EventsOn('ai-analysis-done', handleProAnalysisDone))
  eventOffFns.push(EventsOn('ai-analysis-error', handleProAnalysisError))

  eventOffFns.push(EventsOn('stock-alert-triggered', handleAlertTriggered))
})

const startSmartRefresh = async () => {
  const checkAndRefresh = async () => {
    try {
      const timeInfo = await GetTradingTimeInfo()
      if (timeInfo.refreshInterval > 0) {
        await refreshPrices()
        refreshTimer = setTimeout(checkAndRefresh, timeInfo.refreshInterval * 1000)
      } else {
        refreshTimer = setTimeout(checkAndRefresh, 60000)
      }
    } catch (e) {
      console.error('刷新失败:', e)
      refreshTimer = setTimeout(checkAndRefresh, 30000)
    }
  }
  checkAndRefresh()
}

onUnmounted(() => {
  if (refreshTimer) {
    clearTimeout(refreshTimer)
  }
  if (alertCheckTimer) {
    clearInterval(alertCheckTimer)
  }
  eventOffFns.forEach((off) => {
    if (typeof off === 'function') {
      off()
    }
  })
  eventOffFns.length = 0
})

// 格式化内容
const formatContent = (content) => {
  if (!content) return ''
  let html = content
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
  html = html.replace(/```(\w*)\n([\s\S]*?)```/g, '<pre><code>$2</code></pre>')
  html = html.replace(/`([^`]+)`/g, '<code>$1</code>')
  html = html.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>')
  html = html.replace(/^### (.+)$/gm, '<h4>$1</h4>')
  html = html.replace(/^## (.+)$/gm, '<h3>$1</h3>')
  html = html.replace(/^# (.+)$/gm, '<h2>$1</h2>')
  html = html.replace(/^- (.+)$/gm, '<li>$1</li>')
  html = html.replace(/\n/g, '<br>')
  return html
}
</script>

<template>
  <div class="stock-page">
    <n-card title="自选股票" :bordered="false">
      <template #header-extra>
        <n-space>
          <n-tag type="info">{{ stocks.length }} 只股票</n-tag>
          <n-button type="primary" @click="showAddModal = true">添加股票</n-button>
          <n-button @click="refreshPrices">刷新行情</n-button>
        </n-space>
      </template>

      <n-alert v-if="stocks.length === 0 && !loading" type="info" style="margin-bottom: 16px;">
        暂无自选股票，点击"添加股票"开始添加。股票代码格式：sh600000（上海）、sz000001（深圳）
      </n-alert>

      <n-data-table
        :columns="columns"
        :data="stocks"
        :loading="loading"
        :bordered="false"
        :single-line="false"
        striped
        size="small"
      />
    </n-card>

    <!-- 添加股票弹窗 -->
    <n-modal v-model:show="showAddModal" preset="dialog" title="添加股票" style="width: 500px;">
      <n-form>
        <n-form-item label="股票代码">
          <n-input
            v-model:value="newStockCode"
            placeholder="请输入股票代码"
            @keyup.enter="handleAdd"
          />
        </n-form-item>
        <n-alert type="info" style="margin-top: 8px;">
          <p>代码格式说明：</p>
          <p>上海A股：sh600000、sh601318</p>
          <p>深圳A股：sz000001、sz000858</p>
          <p>创业板：sz300750</p>
          <p>科创板：sh688001</p>
        </n-alert>
      </n-form>
      <template #action>
        <n-space>
          <n-button @click="showAddModal = false">取消</n-button>
          <n-button type="primary" @click="handleAdd">确定</n-button>
        </n-space>
      </template>
    </n-modal>

    <!-- 股票详情弹窗 -->
    <n-modal v-model:show="showDetailModal" preset="card" :title="selectedStock?.name + ' (' + selectedStock?.code + ')'" style="width: 900px;">
      <template #header-extra>
        <n-button type="warning" size="small" @click="openAIFromDetail">AI分析</n-button>
      </template>
      <div v-if="selectedStock" class="stock-detail">
        <div class="price-info">
          <span class="current-price" :class="{ up: selectedStock.changePercent > 0, down: selectedStock.changePercent < 0 }">
            {{ selectedStock.price?.toFixed(2) }}
          </span>
          <span class="change-info" :class="{ up: selectedStock.changePercent > 0, down: selectedStock.changePercent < 0 }">
            {{ (selectedStock.changePercent > 0 ? '+' : '') + selectedStock.changePercent?.toFixed(2) }}%
            ({{ (selectedStock.change > 0 ? '+' : '') + selectedStock.change?.toFixed(2) }})
          </span>
        </div>

        <n-tabs type="line" animated style="margin-top: 16px;">
          <n-tab-pane name="report" tab="研报">
            <n-data-table
              :columns="reportColumns"
              :data="reports"
              :loading="detailLoading"
              :bordered="false"
              size="small"
              :max-height="300"
            />
            <n-empty v-if="reports.length === 0 && !detailLoading" description="暂无研报数据" />
          </n-tab-pane>
          <n-tab-pane name="notice" tab="公告">
            <n-data-table
              :columns="noticeColumns"
              :data="notices"
              :loading="detailLoading"
              :bordered="false"
              size="small"
              :max-height="300"
            />
            <n-empty v-if="notices.length === 0 && !detailLoading" description="暂无公告数据" />
          </n-tab-pane>
        </n-tabs>
      </div>
    </n-modal>

    <!-- AI分析弹窗 -->
    <n-modal v-model:show="showAIModal" preset="card" :title="'AI分析 - ' + (selectedStock?.name || '')" style="width: 900px;">
      <div class="ai-analysis-container">
        <n-alert v-if="!aiEnabled" type="warning" style="margin-bottom: 16px;">
          AI功能未启用，请前往「设置」页面配置AI服务。
        </n-alert>

        <!-- 免责声明 -->
        <n-alert type="warning" :bordered="false" class="disclaimer-alert">
          <template #icon>
            <span style="font-size: 16px;">⚠️</span>
          </template>
          <div class="disclaimer-text">
            <strong>免责声明：</strong>AI分析仅供参考，不构成投资建议。投资有风险，入市需谨慎，盈亏自负。
          </div>
        </n-alert>

        <!-- 对话区域 -->
        <div class="ai-chat-container">
          <n-scrollbar ref="aiScrollbarRef" style="max-height: 400px;">
            <div class="ai-messages">
              <div v-if="aiMessages.length === 0" class="ai-empty-tip">
                点击下方按钮开始AI分析，或直接输入问题进行对话
              </div>
              <div
                v-for="(msg, index) in aiMessages"
                :key="index"
                :class="['ai-message', msg.role]"
              >
                <div class="ai-message-role">{{ msg.role === 'user' ? '我' : 'AI' }}</div>
                <div class="ai-message-content">
                  <n-spin v-if="msg.role === 'assistant' && aiLoading && index === aiMessages.length - 1 && !msg.content" size="small" />
                  <div v-else class="markdown-content" v-html="formatContent(msg.content)"></div>
                </div>
              </div>
            </div>
          </n-scrollbar>
        </div>

        <!-- 输入区域 -->
        <div class="ai-input-area">
          <n-space vertical style="width: 100%;">
            <n-space>
              <n-button type="primary" :disabled="!aiEnabled || aiLoading" @click="startAIAnalysis">
                重新分析
              </n-button>
            </n-space>
            <n-input
              v-model:value="aiQuestion"
              type="textarea"
              placeholder="输入问题继续对话，如：这只股票的风险点是什么？"
              :autosize="{ minRows: 2, maxRows: 3 }"
              :disabled="!aiEnabled || aiLoading"
              @keyup.enter.exact="sendAIQuestion"
            />
            <n-button type="info" :loading="aiLoading" :disabled="!aiEnabled || !aiQuestion.trim()" @click="sendAIQuestion">
              发送问题
            </n-button>
          </n-space>
        </div>
      </div>
    </n-modal>

    <!-- 网页查看弹窗 -->
    <n-modal v-model:show="showWebModal" preset="card" :title="webTitle" style="width: 90vw; max-width: 1200px;">
      <template #header-extra>
        <n-button size="small" @click="openInBrowser">在浏览器中打开</n-button>
      </template>
      <div class="web-container">
        <iframe :src="webUrl" frameborder="0" class="web-iframe"></iframe>
      </div>
    </n-modal>

    <!-- AI摘要弹窗 -->
    <n-modal v-model:show="showSummaryModal" preset="card" :title="'AI解读 - ' + (summaryType === 'report' ? '研报' : '公告')" style="width: 700px;">
      <div class="ai-summary-container">
        <!-- 免责声明 -->
        <n-alert type="warning" :bordered="false" class="disclaimer-alert">
          <template #icon>
            <span style="font-size: 16px;">⚠️</span>
          </template>
          <div class="disclaimer-text">
            <strong>免责声明：</strong>AI解读仅供参考，不构成投资建议。投资有风险，入市需谨慎，盈亏自负。
          </div>
        </n-alert>

        <div class="summary-title">{{ summaryTitle }}</div>
        <div class="summary-content-area">
          <n-scrollbar ref="summaryScrollbarRef" style="max-height: 300px;">
            <n-spin v-if="summaryLoading && !summaryContent" size="small" style="display: block; margin: 40px auto;" />
            <div v-else class="markdown-content" v-html="formatContent(summaryContent)"></div>
          </n-scrollbar>
        </div>
        <div v-if="summaryLoading" class="summary-loading-tip">AI正在分析中...</div>

        <!-- 手动粘贴内容区域 -->
        <div class="manual-input-section">
          <n-button
            v-if="!showManualInput && !summaryLoading"
            size="small"
            type="info"
            dashed
            @click="showManualInput = true"
          >
            内容抓取不完整？点击手动粘贴
          </n-button>

          <div v-if="showManualInput" class="manual-input-area">
            <n-input
              v-model:value="manualContent"
              type="textarea"
              placeholder="从浏览器复制研报/公告内容粘贴到这里，AI将基于此内容进行分析"
              :autosize="{ minRows: 4, maxRows: 8 }"
            />
            <n-space style="margin-top: 8px;">
              <n-button type="primary" size="small" :disabled="!manualContent.trim()" @click="analyzeWithManualContent">
                使用此内容分析
              </n-button>
              <n-button size="small" @click="showManualInput = false; manualContent = ''">
                取消
              </n-button>
            </n-space>
          </div>
        </div>
      </div>
    </n-modal>

    <!-- 专业分析弹窗 -->
    <n-modal v-model:show="showProAnalysisModal" preset="card" :title="getAnalysisTypeLabel() + ' - ' + (selectedStock?.name || '')" style="width: 800px;">
      <div class="pro-analysis-container">
        <!-- 免责声明 -->
        <n-alert type="warning" :bordered="false" class="disclaimer-alert">
          <template #icon>
            <span style="font-size: 16px;">⚠️</span>
          </template>
          <div class="disclaimer-text">
            <strong>免责声明：</strong>以下分析由AI生成，仅供参考，不构成任何投资建议。投资有风险，入市需谨慎。请勿盲目相信AI分析结果，投资决策需自行判断，盈亏自负。
          </div>
        </n-alert>

        <!-- 分析内容区域 -->
        <div class="pro-analysis-content-area">
          <n-scrollbar ref="proAnalysisScrollbarRef" style="max-height: 400px;">
            <n-spin v-if="proAnalysisLoading && !proAnalysisContent" size="small" style="display: block; margin: 40px auto;" />
            <div v-else class="markdown-content" v-html="formatContent(proAnalysisContent)"></div>
          </n-scrollbar>
        </div>
        <div v-if="proAnalysisLoading" class="pro-analysis-loading-tip">AI正在分析中，请稍候...</div>

        <!-- 切换分析类型 -->
        <div class="pro-analysis-actions">
          <n-space>
            <n-dropdown
              trigger="click"
              :options="[
                { label: '基本面分析', key: 'fundamental', children: [
                  { label: '标准分析', key: 'fundamental' },
                  { type: 'divider', key: 'd-f1' },
                  { label: '巴菲特视角', key: 'fundamental-buffett' },
                  { label: '格雷厄姆视角', key: 'fundamental-graham' },
                  { label: '彼得林奇视角', key: 'fundamental-lynch' },
                  { label: '芒格视角', key: 'fundamental-munger' },
                  { label: '费雪视角', key: 'fundamental-fisher' }
                ]},
                { label: '技术面分析', key: 'technical', children: [
                  { label: '标准分析', key: 'technical' },
                  { type: 'divider', key: 'd-t1' },
                  { label: '利弗莫尔视角', key: 'technical-livermore' },
                  { label: '江恩视角', key: 'technical-gann' },
                  { label: '艾略特视角', key: 'technical-elliott' },
                  { label: '墨菲视角', key: 'technical-murphy' }
                ]},
                { label: '情绪面分析', key: 'sentiment', children: [
                  { label: '标准分析', key: 'sentiment' },
                  { type: 'divider', key: 'd-s1' },
                  { label: '索罗斯视角', key: 'sentiment-soros' },
                  { label: '马克斯视角', key: 'sentiment-marks' },
                  { label: '邓普顿视角', key: 'sentiment-templeton' },
                  { label: '科斯托拉尼视角', key: 'sentiment-kostolany' }
                ]}
              ]"
              @select="(key) => { openProAnalysis(selectedStock, key) }"
            >
              <n-button type="primary" :disabled="proAnalysisLoading">切换分析类型</n-button>
            </n-dropdown>
            <n-button @click="startProAnalysis" :disabled="proAnalysisLoading">重新分析</n-button>
          </n-space>
        </div>
      </div>
    </n-modal>

    <!-- 持仓管理弹窗 -->
    <n-modal v-model:show="showPositionModal" preset="card" :title="'持仓管理 - ' + (selectedStock?.name || '')" style="width: 600px;">
      <n-spin :show="positionLoading">
        <div class="position-container">
          <!-- 当前盈亏显示 -->
          <div v-if="currentPosition && selectedStock" class="profit-display">
            <div class="profit-label">当前盈亏</div>
            <div class="profit-value" :class="{ profit: calculateProfit()?.percent >= 0, loss: calculateProfit()?.percent < 0 }">
              <template v-if="calculateProfit()">
                {{ calculateProfit().percent >= 0 ? '+' : '' }}{{ calculateProfit().percent.toFixed(2) }}%
                <span class="profit-amount">
                  （{{ calculateProfit().amount >= 0 ? '+' : '' }}{{ calculateProfit().amount.toFixed(2) }} 元）
                </span>
              </template>
              <template v-else>--</template>
            </div>
          </div>

          <n-form label-placement="left" label-width="80px">
            <n-form-item label="买入价格">
              <n-input-number
                v-model:value="positionForm.buyPrice"
                placeholder="买入价格"
                :precision="2"
                :min="0"
                style="width: 100%;"
              />
            </n-form-item>

            <n-form-item label="买入日期">
              <n-input
                v-model:value="positionForm.buyDate"
                placeholder="YYYY-MM-DD"
                style="width: 100%;"
              />
            </n-form-item>

            <n-form-item label="持仓数量">
              <n-input-number
                v-model:value="positionForm.quantity"
                placeholder="持仓股数"
                :min="0"
                :step="100"
                style="width: 100%;"
              />
            </n-form-item>

            <n-form-item label="成本价">
              <n-input-number
                v-model:value="positionForm.costPrice"
                placeholder="含手续费的成本价（可选）"
                :precision="2"
                :min="0"
                style="width: 100%;"
              />
            </n-form-item>

            <n-form-item label="目标价">
              <n-input-number
                v-model:value="positionForm.targetPrice"
                placeholder="止盈目标价（可选）"
                :precision="2"
                :min="0"
                style="width: 100%;"
              />
            </n-form-item>

            <n-form-item label="止损价">
              <n-input-number
                v-model:value="positionForm.stopLossPrice"
                placeholder="止损价格（可选）"
                :precision="2"
                :min="0"
                style="width: 100%;"
              />
            </n-form-item>

            <n-form-item label="备注">
              <n-input
                v-model:value="positionForm.notes"
                type="textarea"
                placeholder="买入理由、投资逻辑等（可选，AI分析时会参考）"
                :autosize="{ minRows: 2, maxRows: 4 }"
              />
            </n-form-item>
          </n-form>

          <n-space justify="space-between" style="margin-top: 16px;">
            <n-button v-if="currentPosition" type="error" @click="deleteCurrentPosition">
              删除持仓
            </n-button>
            <div v-else></div>
            <n-space>
              <n-button @click="showPositionModal = false">取消</n-button>
              <n-button type="primary" @click="savePosition">
                {{ currentPosition ? '更新持仓' : '保存持仓' }}
              </n-button>
            </n-space>
          </n-space>

          <n-alert type="info" style="margin-top: 16px;">
            <template #icon>
              <span>💡</span>
            </template>
            保存持仓信息后，AI分析时会自动带入您的持仓数据，给出更有针对性的操作建议。
          </n-alert>
        </div>
      </n-spin>
    </n-modal>

    <!-- 提醒设置弹窗 -->
    <n-modal v-model:show="showAlertModal" preset="card" :title="'价格提醒 - ' + (selectedStock?.name || '')" style="width: 600px;">
      <n-spin :show="alertLoading">
        <div class="alert-container">
          <!-- 当前价格信息 -->
          <div v-if="selectedStock" class="current-price-info">
            <span class="price-label">当前价格：</span>
            <span class="price-value" :class="{ up: selectedStock.changePercent > 0, down: selectedStock.changePercent < 0 }">
              {{ selectedStock.price?.toFixed(2) }} 元
            </span>
            <span class="change-value" :class="{ up: selectedStock.changePercent > 0, down: selectedStock.changePercent < 0 }">
              {{ (selectedStock.changePercent > 0 ? '+' : '') + selectedStock.changePercent?.toFixed(2) }}%
            </span>
          </div>

          <!-- 添加提醒表单 -->
          <n-card size="small" title="添加新提醒" style="margin-bottom: 16px;">
            <n-form label-placement="left" label-width="80px">
              <n-form-item label="提醒类型">
                <n-radio-group v-model:value="alertForm.alertType">
                  <n-radio value="change">涨跌提醒</n-radio>
                  <n-radio value="price">股价提醒</n-radio>
                </n-radio-group>
              </n-form-item>

              <n-form-item label="触发条件">
                <n-space align="center">
                  <n-select
                    v-model:value="alertForm.condition"
                    :options="alertForm.alertType === 'change' ? [
                      { label: '涨幅达到', value: 'above' },
                      { label: '跌幅达到', value: 'below' }
                    ] : [
                      { label: '股价高于', value: 'above' },
                      { label: '股价低于', value: 'below' }
                    ]"
                    style="width: 120px;"
                  />
                  <n-input-number
                    v-model:value="alertForm.targetValue"
                    :min="0.01"
                    :step="alertForm.alertType === 'change' ? 0.5 : 0.1"
                    :precision="2"
                    style="width: 120px;"
                  />
                  <span>{{ alertForm.alertType === 'change' ? '%' : '元' }}</span>
                </n-space>
              </n-form-item>

              <n-form-item>
                <n-button type="primary" @click="addAlert">添加提醒</n-button>
              </n-form-item>
            </n-form>
          </n-card>

          <!-- 已设置的提醒列表 -->
          <n-card size="small" title="已设置的提醒">
            <n-empty v-if="stockAlerts.length === 0" description="暂无提醒" />
            <n-list v-else>
              <n-list-item v-for="alert in stockAlerts" :key="alert.id">
                <n-thing>
                  <template #header>
                    <n-space align="center">
                      <n-tag :type="alert.alertType === 'change' ? 'warning' : 'info'" size="small">
                        {{ alert.alertType === 'change' ? '涨跌' : '股价' }}
                      </n-tag>
                      <span>{{ getAlertTypeText(alert) }}</span>
                      <n-tag v-if="alert.triggered" type="success" size="small">已触发</n-tag>
                    </n-space>
                  </template>
                  <template #header-extra>
                    <n-space>
                      <n-switch
                        :value="alert.enabled"
                        @update:value="() => toggleAlert(alert)"
                        size="small"
                      />
                      <n-button
                        v-if="alert.triggered"
                        size="tiny"
                        type="info"
                        @click="resetAlert(alert)"
                      >
                        重置
                      </n-button>
                      <n-popconfirm @positive-click="deleteAlert(alert.id)">
                        <template #trigger>
                          <n-button size="tiny" type="error">删除</n-button>
                        </template>
                        确定删除这个提醒吗？
                      </n-popconfirm>
                    </n-space>
                  </template>
                  <template #description>
                    <span v-if="alert.triggered" style="color: #18a058; font-size: 12px;">
                      触发时间：{{ new Date(alert.triggeredAt).toLocaleString() }}
                      | 触发价格：{{ alert.triggeredPrice?.toFixed(2) }} 元
                    </span>
                    <span v-else style="color: #999; font-size: 12px;">
                      创建时间：{{ new Date(alert.createdAt).toLocaleString() }}
                    </span>
                  </template>
                </n-thing>
              </n-list-item>
            </n-list>
          </n-card>

          <n-alert type="info" style="margin-top: 16px;">
            <template #icon>
              <span>💡</span>
            </template>
            提醒每5秒检查一次本地数据，触发后会弹窗通知。已触发的提醒可以重置后再次使用。
          </n-alert>
        </div>
      </n-spin>
    </n-modal>
  </div>
</template>

<style scoped>
.stock-page {
  height: 100%;
}

.stock-detail {
  padding: 0;
}

.price-info {
  display: flex;
  align-items: baseline;
  gap: 16px;
}

.current-price {
  font-size: 32px;
  font-weight: bold;
}

.change-info {
  font-size: 16px;
}

.up {
  color: #f5222d;
}

.down {
  color: #52c41a;
}

.web-container {
  width: 100%;
  height: 70vh;
  background: #fff;
  border-radius: 4px;
  overflow: hidden;
}

.web-iframe {
  width: 100%;
  height: 100%;
  border: none;
}

/* AI分析样式 */
.ai-analysis-container {
  display: flex;
  flex-direction: column;
  gap: 16px;
}

.ai-chat-container {
  background: rgba(0, 0, 0, 0.1);
  border-radius: 8px;
  padding: 16px;
  min-height: 200px;
}

.ai-messages {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.ai-empty-tip {
  text-align: center;
  color: #999;
  padding: 40px;
}

.ai-message {
  display: flex;
  gap: 12px;
}

.ai-message.user {
  flex-direction: row-reverse;
}

.ai-message-role {
  width: 28px;
  height: 28px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-size: 12px;
  flex-shrink: 0;
}

.ai-message.user .ai-message-role {
  background: #18a058;
  color: white;
}

.ai-message.assistant .ai-message-role {
  background: #2080f0;
  color: white;
}

.ai-message-content {
  max-width: 80%;
  padding: 10px 14px;
  border-radius: 8px;
  line-height: 1.6;
  font-size: 14px;
}

.ai-message.user .ai-message-content {
  background: #18a058;
  color: white;
  border-bottom-right-radius: 0;
}

.ai-message.assistant .ai-message-content {
  background: rgba(255, 255, 255, 0.1);
  border-bottom-left-radius: 0;
}

.markdown-content :deep(h2),
.markdown-content :deep(h3),
.markdown-content :deep(h4) {
  margin: 10px 0 6px 0;
}

.markdown-content :deep(strong) {
  color: #18a058;
}

.markdown-content :deep(li) {
  margin: 4px 0;
  margin-left: 16px;
}

.ai-input-area {
  padding-top: 8px;
  border-top: 1px solid rgba(255, 255, 255, 0.1);
}

/* AI摘要样式 */
.ai-summary-container {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.summary-title {
  font-size: 14px;
  font-weight: bold;
  color: #18a058;
  padding: 8px 12px;
  background: rgba(24, 160, 88, 0.1);
  border-radius: 4px;
}

.summary-content-area {
  background: rgba(0, 0, 0, 0.1);
  border-radius: 8px;
  padding: 16px;
  min-height: 200px;
}

.summary-loading-tip {
  text-align: center;
  color: #999;
  font-size: 12px;
}

.manual-input-section {
  margin-top: 12px;
  padding-top: 12px;
  border-top: 1px solid rgba(255, 255, 255, 0.1);
}

.manual-input-area {
  margin-top: 8px;
}

/* 免责声明样式 */
.disclaimer-alert {
  margin-bottom: 12px;
  background: rgba(250, 173, 20, 0.1) !important;
  border: 1px solid rgba(250, 173, 20, 0.3) !important;
}

.disclaimer-text {
  font-size: 12px;
  line-height: 1.5;
  color: #faad14;
}

.disclaimer-text strong {
  color: #fa8c16;
}

/* 专业分析样式 */
.pro-analysis-container {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.pro-analysis-content-area {
  background: rgba(0, 0, 0, 0.1);
  border-radius: 8px;
  padding: 16px;
  min-height: 250px;
}

.pro-analysis-loading-tip {
  text-align: center;
  color: #999;
  font-size: 12px;
}

.pro-analysis-actions {
  padding-top: 12px;
  border-top: 1px solid rgba(255, 255, 255, 0.1);
}

/* 持仓管理样式 */
.position-container {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.profit-display {
  background: rgba(0, 0, 0, 0.2);
  border-radius: 8px;
  padding: 16px;
  text-align: center;
  margin-bottom: 8px;
}

.profit-label {
  font-size: 12px;
  color: #999;
  margin-bottom: 4px;
}

.profit-value {
  font-size: 24px;
  font-weight: bold;
}

.profit-value.profit {
  color: #f5222d;
}

.profit-value.loss {
  color: #52c41a;
}

.profit-amount {
  font-size: 14px;
  font-weight: normal;
}

/* 提醒设置样式 */
.alert-container {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.current-price-info {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  background: rgba(0, 0, 0, 0.2);
  border-radius: 8px;
  margin-bottom: 8px;
}

.price-label {
  color: #999;
  font-size: 14px;
}

.price-value {
  font-size: 20px;
  font-weight: bold;
}

.change-value {
  font-size: 14px;
  font-weight: bold;
}
</style>
