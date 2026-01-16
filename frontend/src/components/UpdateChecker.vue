<script setup>
import { ref, onMounted } from 'vue'
import {
  NModal,
  NCard,
  NButton,
  NSpace,
  NTag,
  NAlert
} from 'naive-ui'
import { CheckUpdate, OpenURL } from '../../wailsjs/go/main/App'

const showModal = ref(false)
const updateInfo = ref(null)

const checkForUpdate = async () => {
  try {
    const info = await CheckUpdate()
    if (info && info.hasUpdate) {
      updateInfo.value = info
      showModal.value = true
    }
  } catch (e) {
    console.error('检查更新失败:', e)
  }
}

const openDownload = () => {
  if (updateInfo.value?.downloadUrl) {
    OpenURL(updateInfo.value.downloadUrl)
  }
}

const openRelease = () => {
  if (updateInfo.value?.releaseUrl) {
    OpenURL(updateInfo.value.releaseUrl)
  }
}

const closeModal = () => {
  showModal.value = false
}

onMounted(() => {
  // 延迟 2 秒检查更新，避免影响启动速度
  setTimeout(checkForUpdate, 2000)
})
</script>

<template>
  <n-modal v-model:show="showModal" :mask-closable="false">
    <n-card
      title="发现新版本"
      :bordered="false"
      style="width: 480px;"
      role="dialog"
    >
      <template #header-extra>
        <n-tag type="success">{{ updateInfo?.version }}</n-tag>
      </template>

      <div class="update-content">
        <div class="version-compare">
          <span class="current">当前版本: {{ updateInfo?.currentVersion }}</span>
          <span class="arrow">→</span>
          <span class="new">最新版本: {{ updateInfo?.version }}</span>
        </div>

        <n-alert type="info" style="margin: 16px 0;" v-if="updateInfo?.releaseDate">
          发布日期: {{ updateInfo.releaseDate }}
        </n-alert>

        <div class="description" v-if="updateInfo?.description">
          <h4>更新内容:</h4>
          <p>{{ updateInfo.description }}</p>
        </div>
      </div>

      <template #footer>
        <n-space justify="end">
          <n-button @click="closeModal">稍后提醒</n-button>
          <n-button @click="openRelease" v-if="updateInfo?.releaseUrl">
            查看详情
          </n-button>
          <n-button type="primary" @click="openDownload">
            立即下载
          </n-button>
        </n-space>
      </template>
    </n-card>
  </n-modal>
</template>

<style scoped>
.update-content {
  padding: 8px 0;
}

.version-compare {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 16px;
  padding: 16px;
  background: rgba(255, 255, 255, 0.05);
  border-radius: 8px;
  margin-bottom: 16px;
}

.version-compare .current {
  color: #999;
}

.version-compare .arrow {
  color: #18a058;
  font-size: 20px;
}

.version-compare .new {
  color: #18a058;
  font-weight: 600;
}

.description h4 {
  margin: 0 0 8px 0;
  font-size: 14px;
  color: #fff;
}

.description p {
  margin: 0;
  color: #999;
  font-size: 14px;
  line-height: 1.6;
  white-space: pre-wrap;
}
</style>
