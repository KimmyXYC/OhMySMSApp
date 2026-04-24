<script setup lang="ts">
import { ref, computed, onMounted, nextTick } from 'vue'
import { useModemsStore } from '@/stores/modems'
import { initiateUssd, respondUssd, cancelUssd, listUssdSessions } from '@/api/ussd'
import { ElMessage } from 'element-plus'
import type { UssdResponse, USSDRow, UssdTurn } from '@/types/api'

const modemsStore = useModemsStore()

// 选择 modem
const selectedDeviceId = ref<string>('')

// 当前会话
const sessionId = ref<string | null>(null)
const sessionState = ref<string>('idle')
const transcript = ref<UssdTurn[]>([])
const loading = ref(false)

// 输入
const ussdCommand = ref('*101#')
const responseInput = ref('')

// 历史
const historyLoading = ref(false)
const historySessions = ref<USSDRow[]>([])
const showHistory = ref(false)

const transcriptRef = ref<HTMLElement>()

// modem options
const modemOptions = computed(() =>
  modemsStore.modems
    .filter((m) => m.present)
    .map((m) => ({
      label: `${m.manufacturer ?? ''} ${m.model ?? ''} (${m.device_id.slice(-6)})`,
      value: m.device_id,
    })),
)

const isActive = computed(
  () => sessionState.value === 'active' || sessionState.value === 'user_response',
)

function scrollToBottom() {
  nextTick(() => {
    if (transcriptRef.value) {
      transcriptRef.value.scrollTop = transcriptRef.value.scrollHeight
    }
  })
}

async function handleInitiate() {
  if (!selectedDeviceId.value) {
    ElMessage.warning('请选择一个模块')
    return
  }
  if (!ussdCommand.value.trim()) {
    ElMessage.warning('请输入 USSD 指令')
    return
  }
  loading.value = true
  try {
    const { data } = await initiateUssd({
      device_id: selectedDeviceId.value,
      command: ussdCommand.value.trim(),
    })
    sessionId.value = data.session_id
    sessionState.value = data.state || 'terminated'
    transcript.value = [
      { dir: 'out', ts: new Date().toISOString(), text: ussdCommand.value.trim() },
    ]
    if (data.reply) {
      transcript.value.push({
        dir: 'in',
        ts: new Date().toISOString(),
        text: data.reply,
      })
    }
    scrollToBottom()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || 'USSD 请求失败')
  } finally {
    loading.value = false
  }
}

async function handleRespond() {
  if (!sessionId.value || !responseInput.value.trim()) return
  loading.value = true
  try {
    transcript.value.push({
      dir: 'out',
      ts: new Date().toISOString(),
      text: responseInput.value.trim(),
    })
    const { data } = await respondUssd(sessionId.value, responseInput.value.trim())
    responseInput.value = ''
    sessionState.value = data.state || 'terminated'
    if (data.reply) {
      transcript.value.push({
        dir: 'in',
        ts: new Date().toISOString(),
        text: data.reply,
      })
    }
    scrollToBottom()
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || '回复失败')
  } finally {
    loading.value = false
  }
}

async function handleCancel() {
  if (!sessionId.value) return
  try {
    await cancelUssd(sessionId.value)
    sessionState.value = 'terminated'
    ElMessage.info('会话已取消')
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || '取消失败')
  }
}

function handleNewSession() {
  sessionId.value = null
  sessionState.value = 'idle'
  transcript.value = []
  responseInput.value = ''
}

async function loadHistory() {
  historyLoading.value = true
  try {
    const { data } = await listUssdSessions(50)
    historySessions.value = data.items ?? []
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || '加载历史失败')
  } finally {
    historyLoading.value = false
  }
}

function viewHistorySession(session: USSDRow) {
  sessionId.value = null // 历史模式
  sessionState.value = session.state
  // transcript 可能是 JSON string 或 array
  let turns: UssdTurn[] = []
  if (Array.isArray(session.transcript)) {
    turns = session.transcript
  } else if (typeof session.transcript === 'string') {
    try {
      turns = JSON.parse(session.transcript as unknown as string)
    } catch {
      turns = []
    }
  }
  transcript.value = turns
  showHistory.value = false
  scrollToBottom()
}

function formatTime(ts: string): string {
  return new Date(ts).toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  })
}

onMounted(async () => {
  await modemsStore.fetchModems()
  if (modemsStore.modems.length > 0) {
    const first = modemsStore.modems.find((m) => m.present) || modemsStore.modems[0]
    selectedDeviceId.value = first.device_id
  }
})
</script>

<template>
  <div class="page-container ussd-page">
    <div class="ussd-header">
      <h2>USSD</h2>
      <el-button text type="primary" @click="showHistory = true; loadHistory()">
        历史记录
      </el-button>
    </div>

    <!-- Modem 选择 + 指令输入 -->
    <el-card shadow="never" class="ussd-input-card">
      <div class="ussd-form">
        <el-select
          v-model="selectedDeviceId"
          placeholder="选择模块"
          style="width: 220px"
        >
          <el-option
            v-for="opt in modemOptions"
            :key="opt.value"
            :label="opt.label"
            :value="opt.value"
          />
        </el-select>

        <el-input
          v-model="ussdCommand"
          placeholder="USSD 指令（如 *101#）"
          style="flex: 1"
          :disabled="isActive"
          @keyup.enter="handleInitiate"
        />

        <el-button
          type="primary"
          :loading="loading"
          :disabled="isActive"
          @click="handleInitiate"
        >
          发起
        </el-button>
      </div>
    </el-card>

    <!-- 对话区域 -->
    <el-card shadow="never" class="ussd-transcript-card">
      <div ref="transcriptRef" class="ussd-transcript">
        <div
          v-for="(turn, idx) in transcript"
          :key="idx"
          class="ussd-bubble"
          :class="{
            'ussd-bubble--out': turn.dir === 'out' || turn.dir === 'request',
            'ussd-bubble--in': turn.dir === 'in' || turn.dir === 'response' || turn.dir === 'notification',
          }"
        >
          <div class="ussd-bubble__label">
            {{ (turn.dir === 'out' || turn.dir === 'request') ? '你' : '网络' }}
          </div>
          <div class="ussd-bubble__text">{{ turn.text }}</div>
          <div class="ussd-bubble__time">{{ formatTime(turn.ts) }}</div>
        </div>

        <el-empty
          v-if="transcript.length === 0"
          description="输入 USSD 指令开始交互"
          :image-size="80"
        />
      </div>

      <!-- 回复输入 (user_response 状态) -->
      <div v-if="sessionState === 'user_response'" class="ussd-respond">
        <el-input
          v-model="responseInput"
          placeholder="输入回复..."
          @keyup.enter="handleRespond"
        />
        <el-button type="primary" :loading="loading" @click="handleRespond">
          回复
        </el-button>
        <el-button @click="handleCancel">取消</el-button>
      </div>

      <!-- 会话结束操作 -->
      <div v-else-if="transcript.length > 0 && !isActive" class="ussd-ended">
        <el-tag type="info" size="small">会话已结束</el-tag>
        <el-button type="primary" size="small" @click="handleNewSession">
          发起新的 USSD
        </el-button>
      </div>

      <!-- 活跃中且可取消 -->
      <div v-else-if="isActive && sessionState === 'active'" class="ussd-ended">
        <el-tag type="warning" size="small">等待网络响应...</el-tag>
        <el-button size="small" @click="handleCancel">取消</el-button>
      </div>
    </el-card>

    <!-- 历史 Drawer -->
    <el-drawer v-model="showHistory" title="USSD 历史记录" direction="rtl" size="400px">
      <div v-loading="historyLoading">
        <div
          v-for="session in historySessions"
          :key="session.id"
          class="history-item"
          @click="viewHistorySession(session)"
        >
          <div class="history-item__top">
            <span class="history-item__command">{{ session.initial_request }}</span>
            <el-tag :type="session.state === 'terminated' ? 'info' : 'warning'" size="small">
              {{ session.state }}
            </el-tag>
          </div>
          <div class="history-item__time">{{ formatTime(session.started_at) }}</div>
        </div>
        <el-empty v-if="historySessions.length === 0" description="暂无历史记录" />
      </div>
    </el-drawer>
  </div>
</template>

<style scoped lang="scss">
.ussd-page {
  max-width: 800px;
}

.ussd-header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-bottom: 16px;

  h2 {
    font-size: 22px;
    font-weight: 600;
  }
}

.ussd-input-card {
  border-radius: 12px;
  margin-bottom: 16px;
}

.ussd-form {
  display: flex;
  gap: 12px;
  align-items: center;
  flex-wrap: wrap;
}

.ussd-transcript-card {
  border-radius: 12px;
}

.ussd-transcript {
  max-height: 400px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 8px 0;
}

.ussd-bubble {
  max-width: 80%;
  padding: 10px 14px;
  border-radius: 12px;
  font-size: 14px;

  &--out {
    align-self: flex-end;
    background: var(--el-color-primary-light-7);
    border-bottom-right-radius: 4px;
  }

  &--in {
    align-self: flex-start;
    background: var(--el-fill-color-light);
    border-bottom-left-radius: 4px;
  }

  &__label {
    font-size: 11px;
    font-weight: 600;
    color: var(--el-text-color-secondary);
    margin-bottom: 4px;
  }

  &__text {
    white-space: pre-wrap;
    word-break: break-word;
    line-height: 1.5;
  }

  &__time {
    font-size: 11px;
    color: var(--el-text-color-placeholder);
    margin-top: 4px;
    text-align: right;
  }
}

.ussd-respond,
.ussd-ended {
  display: flex;
  gap: 8px;
  align-items: center;
  padding-top: 16px;
  border-top: 1px solid var(--el-border-color-lighter);
  margin-top: 16px;
}

.ussd-respond .el-input {
  flex: 1;
}

.ussd-ended {
  justify-content: center;
}

.history-item {
  padding: 12px 16px;
  border-bottom: 1px solid var(--el-border-color-extra-light);
  cursor: pointer;
  transition: background 0.15s;

  &:hover {
    background: var(--el-fill-color-light);
  }

  &__top {
    display: flex;
    justify-content: space-between;
    align-items: center;
    margin-bottom: 4px;
  }

  &__command {
    font-family: 'SF Mono', 'Fira Code', monospace;
    font-size: 14px;
    font-weight: 500;
  }

  &__time {
    font-size: 12px;
    color: var(--el-text-color-placeholder);
  }
}
</style>
