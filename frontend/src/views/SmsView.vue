<script setup lang="ts">
import { ref, computed, watch, onMounted } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useSmsStore } from '@/stores/sms'
import { useModemsStore } from '@/stores/modems'
import { useSimsStore } from '@/stores/sims'
import { ElMessage } from 'element-plus'
import SmsThread from '@/components/SmsThread.vue'
import { Search, Position } from '@element-plus/icons-vue'
import { modemLabel } from '@/utils/modemLabel'
import type { ThreadRow } from '@/types/api'

const route = useRoute()
const router = useRouter()
const smsStore = useSmsStore()
const modemsStore = useModemsStore()
const simsStore = useSimsStore()

// 筛选 — 空字符串 = 全部模块
const selectedDeviceId = ref<string>('')
const searchQuery = ref('')

// 是否由路由参数初始化（防止 watch 重复拉取）
const initializedFromRoute = ref(false)

// 当前会话 — thread 的完整 key 是 (sim_id, peer)
const activePeer = ref<string | null>(null)
const activeSimId = ref<number | null>(null)
const showDetail = ref(false)

// 发送框的目标 modem device_id（从选中 thread 的 sim_id 自动推导，也可手动切换）
const sendDeviceId = ref<string>('')

// 发送
const sendText = ref('')

// 响应式：小屏模式
const isMobile = ref(window.innerWidth < 768)
window.addEventListener('resize', () => {
  isMobile.value = window.innerWidth < 768
})

/** 生成 thread 的复合唯一 key */
function threadKey(peer: string, simId: number | null | undefined): string {
  return `${simId ?? ''}:${peer}`
}

// 过滤后的 threads
const filteredThreads = computed(() => {
  let list = smsStore.threads
  if (searchQuery.value) {
    const q = searchQuery.value.toLowerCase()
    list = list.filter(
      (t) =>
        t.peer.toLowerCase().includes(q) ||
        t.last_text.toLowerCase().includes(q),
    )
  }
  return list
})

// modem 选项 — 第一项是"全部模块"
const modemOptions = computed(() => {
  const items = modemsStore.modems
    .filter((m) => m.present)
    .map((m) => ({
      label: modemLabel(m),
      value: m.device_id,
    }))
  return items
})

/** 通过 sim_id 找到绑定它的 modem 的 device_id */
function deviceIdForSim(simId: number | null | undefined): string {
  if (simId == null) return ''
  const modem = modemsStore.modems.find((m) => m.sim?.id === simId)
  return modem?.device_id ?? ''
}

/** 通过 device_id 找到 modem 绑定的 sim_id */
function simIdForDevice(deviceId: string): number | undefined {
  if (!deviceId) return undefined
  const modem = modemsStore.modems.find((m) => m.device_id === deviceId)
  return modem?.sim?.id ?? undefined
}

function formatTime(ts: string): string {
  const d = new Date(ts)
  const now = new Date()
  const isToday = d.toDateString() === now.toDateString()
  if (isToday) {
    return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
  }
  return d.toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit' })
}

function truncate(s: string, len: number): string {
  return s.length > len ? s.slice(0, len) + '...' : s
}

async function loadThreads() {
  // 后端 /api/sms/threads 只接受 sim_id，不接受 device_id
  const params: { sim_id?: number } = {}
  if (selectedDeviceId.value) {
    const sid = simIdForDevice(selectedDeviceId.value)
    if (sid != null) params.sim_id = sid
  }
  await smsStore.fetchThreads(params)
}

async function selectThread(thread: ThreadRow) {
  activePeer.value = thread.peer
  activeSimId.value = thread.sim_id ?? null
  showDetail.value = true
  smsStore.clearUnread(threadKey(thread.peer, thread.sim_id))

  // 自动填充发送框的目标 modem
  sendDeviceId.value = deviceIdForSim(thread.sim_id)

  // 使用 thread 自身的 sim_id 查询消息，完全独立于顶部过滤器
  await smsStore.fetchMessages({
    peer: thread.peer,
    sim_id: thread.sim_id ?? undefined,
    limit: 200,
  })
}

async function handleSend() {
  if (!sendText.value.trim() || !activePeer.value) return
  if (!sendDeviceId.value) {
    ElMessage.warning('请先选择一个模块才能发送')
    return
  }
  try {
    await smsStore.sendSms({
      device_id: sendDeviceId.value,
      peer: activePeer.value,
      body: sendText.value.trim(),
    })
    sendText.value = ''
    ElMessage.success('已发送')
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || '发送失败')
  }
}

function handleDeleteMessage(id: number) {
  smsStore.deleteMessage(id).then(() => {
    ElMessage.success('已删除')
  }).catch((e: any) => {
    ElMessage.error(e.response?.data?.error || '删除失败')
  })
}

function goBack() {
  showDetail.value = false
  activePeer.value = null
  activeSimId.value = null
  sendDeviceId.value = ''
}

// 设备切换时刷新 thread 列表（跳过初始路由初始化阶段）
// 不再重新拉取右侧消息 — 右侧会话完全由 selectThread 控制
watch(selectedDeviceId, () => {
  if (initializedFromRoute.value) {
    initializedFromRoute.value = false
    return
  }
  loadThreads()
})

// 初始化
onMounted(async () => {
  await Promise.all([modemsStore.fetchModems(), simsStore.fetchSims()])

  // 读取路由参数预选 modem
  const queryDeviceId = route.query.device_id as string | undefined
  const querySimId = route.query.sim_id as string | undefined

  if (queryDeviceId) {
    // 直接使用 device_id
    const found = modemsStore.modems.find((m) => m.device_id === queryDeviceId)
    if (found) {
      initializedFromRoute.value = true
      selectedDeviceId.value = found.device_id
    }
  } else if (querySimId) {
    // 通过 sim_id 找到对应 modem
    const simIdNum = parseInt(querySimId, 10)
    if (!isNaN(simIdNum)) {
      const found = modemsStore.modems.find((m) => m.sim?.id === simIdNum)
      if (found) {
        initializedFromRoute.value = true
        selectedDeviceId.value = found.device_id
      }
    }
  }
  // else: selectedDeviceId 保持空 → 拉取全部

  await loadThreads()

  // 如果 URL 带了 peer query，自动打开
  const queryPeer = route.query.peer as string
  if (queryPeer) {
    // 尝试从已加载的 threads 里找到匹配 thread 以获取 sim_id
    const matchThread = smsStore.threads.find((t) => t.peer === queryPeer)
    activePeer.value = queryPeer
    activeSimId.value = matchThread?.sim_id ?? null
    showDetail.value = true
    smsStore.clearUnread(threadKey(queryPeer, activeSimId.value))
    sendDeviceId.value = deviceIdForSim(activeSimId.value)
    await smsStore.fetchMessages({
      peer: queryPeer,
      sim_id: activeSimId.value ?? undefined,
    })
  }
})
</script>

<template>
  <div class="page-container sms-page">
    <!-- 顶部工具条 -->
    <div class="sms-toolbar">
      <h2>短信</h2>
      <div class="sms-toolbar__filters">
        <el-select
          v-model="selectedDeviceId"
          placeholder="全部模块"
          clearable
          style="width: 220px"
          size="default"
        >
          <el-option
            label="📡 全部模块"
            value=""
          />
          <el-option
            v-for="opt in modemOptions"
            :key="opt.value"
            :label="opt.label"
            :value="opt.value"
          />
        </el-select>
      </div>
    </div>

    <!-- 主体两栏 -->
    <div class="sms-body">
      <!-- 左侧会话列表 -->
      <div
        class="sms-threads"
        :class="{ 'sms-threads--hidden': isMobile && showDetail }"
      >
        <div class="sms-threads__search">
          <el-input
            v-model="searchQuery"
            placeholder="搜索号码或内容..."
            :prefix-icon="Search"
            clearable
            size="default"
          />
        </div>

        <div v-loading="smsStore.loading && smsStore.threads.length === 0" class="sms-threads__list">
          <div
            v-for="thread in filteredThreads"
            :key="thread.peer + '-' + (thread.sim_id ?? '')"
            class="thread-item"
            :class="{ 'thread-item--active': activePeer === thread.peer && activeSimId === (thread.sim_id ?? null) }"
            @click="selectThread(thread)"
          >
            <div class="thread-item__avatar">
              {{ thread.peer.slice(-2) }}
            </div>
            <div class="thread-item__content">
              <div class="thread-item__top">
                <span class="thread-item__peer">{{ thread.peer }}</span>
                <span class="thread-item__time">{{ formatTime(thread.last_time) }}</span>
              </div>
              <div class="thread-item__bottom">
                <span class="thread-item__preview">{{ truncate(thread.last_text, 40) }}</span>
                <el-badge
                  v-if="(smsStore.unreadMap[threadKey(thread.peer, thread.sim_id)] ?? 0) > 0"
                  :value="smsStore.unreadMap[threadKey(thread.peer, thread.sim_id)]"
                  :max="99"
                  class="thread-item__badge"
                />
                <span v-else-if="thread.count > 1" class="thread-item__count">
                  {{ thread.count }}
                </span>
              </div>
            </div>
          </div>

          <el-empty
            v-if="!smsStore.loading && filteredThreads.length === 0"
            description="暂无会话"
            :image-size="60"
          />
        </div>
      </div>

      <!-- 右侧消息详情 -->
      <div
        class="sms-detail"
        :class="{ 'sms-detail--hidden': isMobile && !showDetail }"
      >
        <template v-if="activePeer">
          <!-- 手机端返回按钮 -->
          <div v-if="isMobile" class="sms-detail__back">
            <el-button text @click="goBack">← 返回</el-button>
          </div>

          <div class="sms-detail__thread">
            <SmsThread
              :messages="smsStore.currentMessages"
              :peer="activePeer"
              @delete="handleDeleteMessage"
            />
          </div>

          <!-- 发送框 -->
          <div class="sms-detail__send">
            <el-select
              v-model="sendDeviceId"
              placeholder="选择模块"
              size="default"
              style="width: 160px; flex-shrink: 0"
            >
              <el-option
                v-for="opt in modemOptions"
                :key="opt.value"
                :label="opt.label"
                :value="opt.value"
              />
            </el-select>
            <el-input
              v-model="sendText"
              placeholder="输入消息..."
              :autosize="{ minRows: 1, maxRows: 4 }"
              type="textarea"
              resize="none"
              @keydown.enter.exact.prevent="handleSend"
            />
            <el-button
              type="primary"
              :icon="Position"
              :loading="smsStore.sending"
              :disabled="!sendText.trim() || !sendDeviceId"
              @click="handleSend"
            >
              发送
            </el-button>
          </div>
        </template>

        <el-empty v-else description="选择左侧会话开始查看" :image-size="100" />
      </div>
    </div>
  </div>
</template>

<style scoped lang="scss">
.sms-page {
  height: calc(100vh - 56px - 40px);
  display: flex;
  flex-direction: column;
  max-width: 100%;
  padding-bottom: 0;
}

.sms-toolbar {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;
  flex-shrink: 0;
  flex-wrap: wrap;
  gap: 12px;

  h2 {
    font-size: 22px;
    font-weight: 600;
  }

  &__filters {
    display: flex;
    gap: 8px;
    align-items: center;
  }
}

.sms-body {
  flex: 1;
  display: flex;
  gap: 16px;
  min-height: 0;
  overflow: hidden;
}

// 左侧会话列表
.sms-threads {
  width: 340px;
  min-width: 280px;
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  overflow: hidden;

  &__search {
    padding: 12px;
    border-bottom: 1px solid var(--el-border-color-lighter);
    flex-shrink: 0;
  }

  &__list {
    flex: 1;
    overflow-y: auto;
    min-height: 0;
  }
}

.thread-item {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 16px;
  cursor: pointer;
  transition: background-color 0.15s;
  border-bottom: 1px solid var(--el-border-color-extra-light);

  &:hover {
    background: var(--el-fill-color-light);
  }

  &--active {
    background: var(--el-color-primary-light-9);
  }

  &__avatar {
    width: 40px;
    height: 40px;
    border-radius: 50%;
    background: var(--el-color-primary-light-7);
    color: var(--el-color-primary);
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 14px;
    font-weight: 600;
    flex-shrink: 0;
  }

  &__content {
    flex: 1;
    min-width: 0;
  }

  &__top {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 4px;
  }

  &__peer {
    font-weight: 500;
    font-size: 14px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  &__time {
    font-size: 12px;
    color: var(--el-text-color-placeholder);
    flex-shrink: 0;
    margin-left: 8px;
  }

  &__bottom {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }

  &__preview {
    font-size: 13px;
    color: var(--el-text-color-secondary);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
    flex: 1;
    min-width: 0;
  }

  &__count {
    font-size: 11px;
    color: var(--el-text-color-placeholder);
    background: var(--el-fill-color);
    padding: 1px 6px;
    border-radius: 10px;
    flex-shrink: 0;
    margin-left: 8px;
  }

  &__badge {
    flex-shrink: 0;
    margin-left: 8px;
  }
}

// 右侧消息详情
.sms-detail {
  flex: 1;
  display: flex;
  flex-direction: column;
  background: var(--el-bg-color);
  border: 1px solid var(--el-border-color-light);
  border-radius: 12px;
  overflow: hidden;
  min-width: 0;

  &__back {
    padding: 8px 12px;
    border-bottom: 1px solid var(--el-border-color-lighter);
    flex-shrink: 0;
  }

  &__thread {
    flex: 1;
    padding: 16px;
    overflow: hidden;
    display: flex;
    flex-direction: column;
    min-height: 0;
  }

  &__send {
    display: flex;
    gap: 8px;
    padding: 12px 16px;
    border-top: 1px solid var(--el-border-color-lighter);
    flex-shrink: 0;
    align-items: flex-end;

    .el-input,
    .el-textarea {
      flex: 1;
    }
  }
}

/* 手机端切换 */
@media (max-width: 767px) {
  .sms-body {
    gap: 0;
  }

  .sms-threads {
    width: 100%;
    min-width: 0;
    border-radius: 0;

    &--hidden {
      display: none;
    }
  }

  .sms-detail {
    border-radius: 0;

    &--hidden {
      display: none;
    }
  }
}
</style>
