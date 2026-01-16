<script setup>
import { ref, onMounted } from 'vue'
import {
  NCard,
  NDataTable,
  NButton,
  NInput,
  NSpace,
  NModal,
  NForm,
  NFormItem,
  useMessage
} from 'naive-ui'
import { h } from 'vue'
import { GetFundList, AddFund, RemoveFund, GetFundPrice } from '../../wailsjs/go/main/App'

const message = useMessage()
const funds = ref([])
const loading = ref(false)
const showAddModal = ref(false)
const newFundCode = ref('')

const columns = [
  { title: '代码', key: 'code', width: 100 },
  { title: '名称', key: 'name', width: 200 },
  { title: '净值', key: 'nav', width: 100, render: (row) => row.nav?.toFixed(4) || '-' },
  { title: '估值', key: 'estimate', width: 100, render: (row) => row.estimate?.toFixed(4) || '-' },
  {
    title: '估算涨跌',
    key: 'changePercent',
    width: 100,
    render: (row) => {
      const val = row.changePercent
      if (val === undefined || val === null) return '-'
      const color = val > 0 ? '#f5222d' : val < 0 ? '#52c41a' : '#999'
      return h('span', { style: { color } }, (val > 0 ? '+' : '') + val.toFixed(2) + '%')
    }
  },
  { title: '更新时间', key: 'updateTime', width: 150 },
  {
    title: '操作',
    key: 'actions',
    width: 100,
    render: (row) => h(NButton, {
      size: 'small',
      type: 'error',
      onClick: () => handleRemove(row.code)
    }, { default: () => '删除' })
  }
]

const loadFunds = async () => {
  loading.value = true
  try {
    const list = await GetFundList()
    funds.value = list || []
  } catch (e) {
    console.error('加载基金列表失败:', e)
  } finally {
    loading.value = false
  }
}

const refreshPrices = async () => {
  if (funds.value.length === 0) return
  try {
    const codes = funds.value.map(f => f.code)
    const prices = await GetFundPrice(codes)
    if (prices) {
      funds.value = funds.value.map(f => ({
        ...f,
        ...prices[f.code]
      }))
    }
  } catch (e) {
    console.error('刷新基金估值失败:', e)
  }
}

const handleAdd = async () => {
  if (!newFundCode.value.trim()) {
    message.warning('请输入基金代码')
    return
  }
  try {
    await AddFund(newFundCode.value.trim())
    message.success('添加成功')
    showAddModal.value = false
    newFundCode.value = ''
    await loadFunds()
    await refreshPrices()
  } catch (e) {
    message.error('添加失败: ' + e)
  }
}

const handleRemove = async (code) => {
  try {
    await RemoveFund(code)
    message.success('删除成功')
    await loadFunds()
  } catch (e) {
    message.error('删除失败: ' + e)
  }
}

onMounted(async () => {
  await loadFunds()
  await refreshPrices()
})
</script>

<template>
  <div class="fund-page">
    <n-card title="自选基金" :bordered="false">
      <template #header-extra>
        <n-space>
          <n-button type="primary" @click="showAddModal = true">添加基金</n-button>
          <n-button @click="refreshPrices">刷新估值</n-button>
        </n-space>
      </template>

      <n-data-table
        :columns="columns"
        :data="funds"
        :loading="loading"
        :bordered="false"
        :single-line="false"
        striped
      />
    </n-card>

    <n-modal v-model:show="showAddModal" preset="dialog" title="添加基金">
      <n-form>
        <n-form-item label="基金代码">
          <n-input v-model:value="newFundCode" placeholder="请输入基金代码，如 000001" />
        </n-form-item>
      </n-form>
      <template #action>
        <n-space>
          <n-button @click="showAddModal = false">取消</n-button>
          <n-button type="primary" @click="handleAdd">确定</n-button>
        </n-space>
      </template>
    </n-modal>
  </div>
</template>

<style scoped>
.fund-page {
  height: 100%;
}
</style>
