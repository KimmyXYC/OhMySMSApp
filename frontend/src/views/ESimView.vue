<script setup lang="ts">
import { ref, computed, watch, onMounted, onUnmounted, nextTick } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  Refresh,
  Search,
  CopyDocument,
  Edit,
  Plus,
  Delete,
  Camera,
} from '@element-plus/icons-vue'
import { useESimStore } from '@/stores/esim'
import type { ESimCard, ESimProfile } from '@/types/api'

const route = useRoute()
const router = useRouter()
const store = useESimStore()

// ─── 编辑昵称弹窗 ───
const nicknameDialogVisible = ref(false)
const nicknameDialogTitle = ref('')
const nicknameDialogValue = ref('')
const nicknameDialogHint = ref('')
const nicknameDialogLoading = ref(false)
let nicknameDialogCallback: ((val: string) => Promise<void>) | null = null

// ─── 添加 Profile 弹窗 ───
const addDialogVisible = ref(false)
const addMode = ref<'activation' | 'manual' | 'qr'>('activation')
const addActivationCode = ref('')
const addSMDPAddress = ref('')
const addMatchingID = ref('')
const addConfirmationCode = ref('')
const addDialogLoading = ref(false)
const qrVideoRef = ref<HTMLVideoElement | null>(null)
const qrScanning = ref(false)
let qrStream: MediaStream | null = null
let qrStop = false

// ─── 初始化 ───
onMounted(async () => {
  await store.fetchCards()
  // 从 query 恢复选中状态
  const qCard = route.query.card
  if (qCard) {
    const cardId = Number(qCard)
    if (!isNaN(cardId) && store.cards.find((c) => c.id === cardId)) {
      store.selectCard(cardId)
    }
  }
})

// ─── query 同步 ───
watch(
  () => store.selectedCardId,
  (id) => {
    const query = id !== null ? { card: String(id) } : {}
    router.replace({ query })
  },
)

// ─── 清理 ───
onUnmounted(() => {
  // 保留 store 状态，不 reset
  stopQrScan()
})

// ─── 移动端视图控制 ───
const showDetailOnMobile = computed(() => store.selectedCardId !== null)

// ─── Card 列表选中 ───
function handleSelectCard(card: ESimCard) {
  if (store.operatingCardId === card.id) return // 操作中不允许切换
  store.selectCard(card.id)
}

function handleBackToList() {
  store.selectCard(null)
}

// ─── Vendor 信息 ───
function vendorColor(vendor: string): string {
  switch (vendor?.toLowerCase()) {
    case '5ber':
      return 'var(--ohmysms-vendor-5ber)'
    case '9esim':
      return 'var(--ohmysms-vendor-9esim)'
    default:
      return 'var(--ohmysms-vendor-unknown)'
  }
}

function vendorBgColor(vendor: string): string {
  switch (vendor?.toLowerCase()) {
    case '5ber':
      return 'var(--ohmysms-vendor-5ber-bg)'
    case '9esim':
      return 'var(--ohmysms-vendor-9esim-bg)'
    default:
      return 'var(--ohmysms-vendor-unknown-bg)'
  }
}

function vendorLabel(vendor: string): string {
  switch (vendor?.toLowerCase()) {
    case '5ber':
      return '5ber'
    case '9esim':
      return '9eSIM'
    default:
      return vendor || '未知'
  }
}

// ─── EID 格式化 ───
function eidShort(eid: string): string {
  return eid ? '...' + eid.slice(-8) : '—'
}

// ─── ICCID 格式化 ───
function iccidShort(iccid: string): string {
  return iccid ? '...' + iccid.slice(-8) : '—'
}

function profileDisplayName(profile: ESimProfile): string {
  return profile.nickname || profile.service_provider || profile.profile_name || profile.iccid
}

// ─── NVM 格式化 ───
function formatNvm(bytes: number | null): string {
  if (bytes === null || bytes === undefined) return '—'
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}

// ─── Profile 数量 ───
function profileCount(card: ESimCard): string {
  // 如果有详情已加载且是当前卡
  if (store.selectedCardDetail?.id === card.id) {
    return `${store.profiles.length} 个 profile`
  }
  return card.active_iccid ? '≥1 个 profile' : '无活跃 profile'
}

// ─── 复制到剪贴板 ───
async function copyToClipboard(text: string) {
  try {
    await navigator.clipboard.writeText(text)
    ElMessage.success('已复制')
  } catch {
    ElMessage.warning('复制失败，请手动复制')
  }
}

// ─── 扫描发现 ───
async function handleDiscoverAll() {
  if (store.cards.length === 0) {
    // 没有 cards，提示用户
    ElMessage.info('正在扫描所有模块...')
    // 对"所有"卡片调 discover 没有意义，只能刷新列表
    await store.fetchCards()
    if (store.cards.length === 0) {
      ElMessage.warning('未发现任何 eSIM 卡片。请确保有 4G 模块连接且已插入 sticker eSIM。')
    }
    return
  }
  // 有 cards，逐个触发 discover
  for (const card of store.cards) {
    try {
      await store.doDiscover(card.id)
    } catch {
      // ignore single failures
    }
  }
  ElMessage.info('已触发扫描，2 秒后刷新')
  setTimeout(async () => {
    await store.fetchCards()
    if (store.selectedCardId !== null) {
      await store.fetchCardDetail(store.selectedCardId)
    }
  }, 2000)
}

async function handleDiscoverCard(cardId: number) {
  try {
    await store.doDiscover(cardId)
    ElMessage.info('已触发扫描，2 秒后刷新')
    setTimeout(async () => {
      await store.fetchCardDetail(cardId)
      await store.fetchCards()
    }, 2000)
  } catch (e: any) {
    const msg = e.response?.data?.error || e.message || '扫描触发失败'
    ElMessage.error(msg)
  }
}

function handleOpenAddProfile() {
  if (!store.selectedCardDetail) return
  addMode.value = 'activation'
  addActivationCode.value = ''
  addSMDPAddress.value = ''
  addMatchingID.value = ''
  addConfirmationCode.value = ''
  addDialogVisible.value = true
}

async function handleAddDialogClosed() {
  stopQrScan()
  addConfirmationCode.value = ''
}

async function handleAddProfile() {
  const card = store.selectedCardDetail
  if (!card) return
  const activationCode = addActivationCode.value.trim()
  const smdp = addSMDPAddress.value.trim()
  const matching = addMatchingID.value.trim()
  const confirmation = addConfirmationCode.value.trim()
  if (addMode.value === 'activation' || addMode.value === 'qr') {
    if (!activationCode) {
      ElMessage.warning('请输入或扫描 LPA 激活码')
      return
    }
  } else if (!smdp || !matching) {
    ElMessage.warning('请填写 SM-DP+ 地址和 Matching ID')
    return
  }

  try {
    await ElMessageBox.confirm(
      '将通过 lpac 下载并安装 profile 到当前 eUICC。请确保使用测试 9eSIM 卡，安装过程不要拔出设备。',
      '添加 eSIM Profile',
      { confirmButtonText: '开始添加', cancelButtonText: '取消', type: 'warning' },
    )
  } catch {
    return
  }

  addDialogLoading.value = true
  store.operatingCardId = card.id
  store.operatingText = '正在添加 profile...'
  try {
    await store.doAddProfile(card.id, addMode.value === 'manual'
      ? { smdp_address: smdp, matching_id: matching, confirmation_code: confirmation || undefined }
      : { activation_code: activationCode, confirmation_code: confirmation || undefined })
    ElMessage.success('Profile 已添加')
    addDialogVisible.value = false
    await store.fetchCards()
  } catch (e: any) {
    const msg = e.response?.data?.error || e.message || '添加 profile 失败'
    ElMessage.error(msg)
  } finally {
    addDialogLoading.value = false
    store.operatingCardId = null
    store.operatingText = ''
  }
}

async function startQrScan() {
  if (qrScanning.value) return
  const BarcodeDetectorCtor = (window as any).BarcodeDetector
  if (!BarcodeDetectorCtor) {
    ElMessage.warning('当前浏览器不支持 BarcodeDetector，请手动粘贴二维码内容')
    addMode.value = 'activation'
    return
  }
  try {
    qrStream = await navigator.mediaDevices.getUserMedia({ video: { facingMode: 'environment' } })
    await nextTick()
    if (!qrVideoRef.value) return
    qrVideoRef.value.srcObject = qrStream
    await qrVideoRef.value.play()
    qrScanning.value = true
    qrStop = false
    const detector = new BarcodeDetectorCtor({ formats: ['qr_code'] })
    const loop = async () => {
      if (qrStop || !qrVideoRef.value) return
      try {
        const codes = await detector.detect(qrVideoRef.value)
        const raw = codes?.[0]?.rawValue?.trim()
        if (raw) {
          addActivationCode.value = raw
          ElMessage.success('已扫描二维码')
          addMode.value = 'activation'
          stopQrScan()
          return
        }
      } catch {
        // ignore single frame detection errors
      }
      requestAnimationFrame(loop)
    }
    requestAnimationFrame(loop)
  } catch (e: any) {
    ElMessage.error(e?.message || '无法打开摄像头')
    addMode.value = 'activation'
  }
}

function stopQrScan() {
  qrStop = true
  qrScanning.value = false
  if (qrStream) {
    qrStream.getTracks().forEach((t) => t.stop())
    qrStream = null
  }
  if (qrVideoRef.value) {
    qrVideoRef.value.srcObject = null
  }
}

watch(addMode, async (mode) => {
  if (mode === 'qr') {
    await startQrScan()
  } else {
    stopQrScan()
  }
})

// ─── 编辑卡备注名 ───
function handleEditCardNickname(card: ESimCard) {
  nicknameDialogTitle.value = '修改卡片备注名'
  nicknameDialogValue.value = card.nickname ?? ''
  nicknameDialogHint.value = 'ℹ️ 仅保存在本系统'
  nicknameDialogCallback = async (val: string) => {
    await store.doSetCardNickname(card.id, val)
    ElMessage.success('备注名已更新')
  }
  nicknameDialogVisible.value = true
}

// ─── 编辑 Profile 昵称 ───
function handleEditProfileNickname(profile: ESimProfile) {
  nicknameDialogTitle.value = '修改 Profile 昵称'
  nicknameDialogValue.value = profile.nickname ?? ''
  nicknameDialogHint.value = '💾 此昵称将写入 eUICC 卡片硬件'
  nicknameDialogCallback = async (val: string) => {
    await store.doSetProfileNickname(profile.iccid, val)
    ElMessage.success('Profile 昵称已更新')
  }
  nicknameDialogVisible.value = true
}

async function handleNicknameConfirm() {
  if (!nicknameDialogCallback) return
  nicknameDialogLoading.value = true
  try {
    await nicknameDialogCallback(nicknameDialogValue.value)
    nicknameDialogVisible.value = false
  } catch (e: any) {
    const msg = e.response?.data?.error || e.message || '操作失败'
    ElMessage.error(msg)
  } finally {
    nicknameDialogLoading.value = false
  }
}

// ─── Enable Profile ───
async function handleEnableProfile(profile: ESimProfile) {
  const card = store.selectedCardDetail
  if (!card) return

  const currentOperator = card.active_profile_name || card.active_profile_name || '当前 profile'
  const targetOperator = profile.service_provider || profile.profile_name || profile.iccid

  try {
    await ElMessageBox.confirm(
      `确定要启用「${targetOperator}」？当前活跃的「${currentOperator}」会被自动禁用。`,
      '启用 Profile',
      {
        confirmButtonText: '确定启用',
        cancelButtonText: '取消',
        type: 'warning',
      },
    )
  } catch {
    return // 用户取消
  }

  store.operatingCardId = card.id
  store.operatingText = '正在切换 profile...'

  try {
    await store.doEnableProfile(profile.iccid)

    // 等待 2 秒后开始 poll
    await new Promise((r) => setTimeout(r, 2000))
    store.operatingText = '等待 profile 切换完成...'

    const success = await store.pollUntilProfileChange(card.id, profile.iccid, 30_000, 2_000)

    if (success) {
      ElMessage.success('Profile 已启用')
    } else {
      ElMessage.warning('操作已提交，但未在 30 秒内看到状态变化。请稍后手动刷新')
    }
  } catch (e: any) {
    const code = e.response?.data?.code
    if (code === 'no_change_needed') {
      ElMessage.error('该 profile 已经是启用状态')
    } else {
      const msg = e.response?.data?.error || e.message || '启用 profile 失败'
      ElMessage.error(msg)
    }
  } finally {
    store.operatingCardId = null
    store.operatingText = ''
    // 最终刷新
    await store.fetchCards()
    if (store.selectedCardId !== null) {
      await store.fetchCardDetail(store.selectedCardId)
    }
  }
}

// ─── Disable Profile ───
async function handleDisableProfile(profile: ESimProfile) {
  const card = store.selectedCardDetail
  if (!card) return

  const targetOperator = profile.service_provider || profile.profile_name || profile.iccid

  try {
    await ElMessageBox.confirm(
      `确定要禁用「${targetOperator}」？禁用后该模块将无可用 SIM，直到启用另一个 profile。`,
      '禁用 Profile',
      {
        confirmButtonText: '确定禁用',
        cancelButtonText: '取消',
        type: 'warning',
        confirmButtonClass: 'el-button--warning',
      },
    )
  } catch {
    return // 用户取消
  }

  store.operatingCardId = card.id
  store.operatingText = '正在禁用 profile...'

  try {
    await store.doDisableProfile(profile.iccid)

    await new Promise((r) => setTimeout(r, 2000))
    store.operatingText = '等待 profile 禁用完成...'

    const success = await store.pollUntilProfileChange(card.id, null, 30_000, 2_000)

    if (success) {
      ElMessage.success('Profile 已禁用')
    } else {
      ElMessage.warning('操作已提交，但未在 30 秒内看到状态变化。请稍后手动刷新')
    }
  } catch (e: any) {
    const code = e.response?.data?.code
    if (code === 'no_change_needed') {
      ElMessage.error('该 profile 已经是禁用状态')
    } else {
      const msg = e.response?.data?.error || e.message || '禁用 profile 失败'
      ElMessage.error(msg)
    }
  } finally {
    store.operatingCardId = null
    store.operatingText = ''
    await store.fetchCards()
    if (store.selectedCardId !== null) {
      await store.fetchCardDetail(store.selectedCardId)
    }
  }
}

async function handleDeleteProfile(profile: ESimProfile) {
  const card = store.selectedCardDetail
  if (!card) return
  if (profile.state === 'enabled') {
    ElMessage.warning('请先禁用该 profile，再删除')
    return
  }
  const name = profileDisplayName(profile)
  try {
    await ElMessageBox.prompt(
      `删除后无法恢复。请输入配置名称「${name}」确认删除。`,
      '删除 Profile',
      {
        confirmButtonText: '确认删除',
        cancelButtonText: '取消',
        inputPattern: new RegExp(`^${escapeRegExp(name)}$`),
        inputErrorMessage: '输入的配置名称不匹配',
        type: 'warning',
        confirmButtonClass: 'el-button--danger',
      },
    )
  } catch {
    return
  }

  store.operatingCardId = card.id
  store.operatingText = '正在删除 profile...'
  try {
    await store.doDeleteProfile(profile.iccid, name)
    ElMessage.success('Profile 已删除')
    await store.fetchCards()
  } catch (e: any) {
    const code = e.response?.data?.code
    if (code === 'profile_active') {
      ElMessage.error('该 profile 仍处于启用状态，请先禁用')
    } else {
      const msg = e.response?.data?.error || e.message || '删除 profile 失败'
      ElMessage.error(msg)
    }
  } finally {
    store.operatingCardId = null
    store.operatingText = ''
  }
}

function escapeRegExp(value: string): string {
  return value.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

// ─── 是否当前卡正在操作 ───
function isCardOperating(cardId: number): boolean {
  return store.operatingCardId === cardId
}
</script>

<template>
  <div class="page-container esim-page">
    <!-- 页头 -->
    <div class="esim-page__header">
      <h2>eSIM 卡片管理</h2>
      <div class="esim-page__header-actions">
        <el-button
          type="primary"
          :icon="Search"
          @click="handleDiscoverAll"
          :loading="store.loading"
        >
          扫描发现
        </el-button>
        <el-button
          :icon="Refresh"
          circle
          @click="store.fetchCards()"
          :loading="store.loading"
        />
      </div>
    </div>

    <!-- 错误提示 -->
    <el-alert
      v-if="store.error"
      type="error"
      :title="store.error"
      show-icon
      closable
      style="margin-bottom: 16px"
      @close="store.error = null"
    />

    <!-- Empty State -->
    <div v-if="!store.loading && store.cards.length === 0" class="esim-empty">
      <div class="esim-empty__icon">🪪</div>
      <h3 class="esim-empty__title">还未发现任何 eSIM</h3>
      <el-button type="primary" size="large" @click="handleDiscoverAll">
        立即扫描
      </el-button>
      <p class="esim-empty__hint">
        请确保至少有一个 4G 模块插有 sticker eSIM（如 5ber / 9eSIM），<br />
        然后点击扫描触发发现。首次发现可能需要 10-30 秒。
      </p>
    </div>

    <!-- 主体：列表 + 详情 -->
    <div
      v-if="store.cards.length > 0 || store.loading"
      v-loading="store.loading && store.cards.length === 0"
      class="esim-body"
      :class="{ 'esim-body--detail-open': showDetailOnMobile }"
    >
      <!-- 左侧 Card 列表 -->
      <aside class="esim-sidebar" :class="{ 'esim-sidebar--hidden': showDetailOnMobile }">
        <div class="esim-sidebar__list">
          <div
            v-for="card in store.cards"
            :key="card.id"
            class="esim-card-item"
            :class="{
              'esim-card-item--active': store.selectedCardId === card.id,
              'esim-card-item--operating': isCardOperating(card.id),
            }"
            @click="handleSelectCard(card)"
          >
            <!-- Vendor 色条 -->
            <div
              class="esim-card-item__accent"
              :style="{ backgroundColor: vendorColor(card.vendor) }"
            />

            <div class="esim-card-item__content">
              <div class="esim-card-item__top">
                <span
                  class="esim-card-item__vendor"
                  :style="{ color: vendorColor(card.vendor) }"
                >
                  {{ vendorLabel(card.vendor) }}
                </span>
                <el-tag
                  v-if="card.active_profile_name"
                  size="small"
                  type="success"
                  effect="plain"
                  round
                >
                  {{ card.active_profile_name }}
                </el-tag>
              </div>

              <!-- 备注名 -->
              <div class="esim-card-item__name">
                {{ card.nickname || vendorLabel(card.vendor) }}
                <el-icon
                  class="esim-card-item__edit-icon"
                  @click.stop="handleEditCardNickname(card)"
                >
                  <Edit />
                </el-icon>
              </div>

              <!-- EID 简要 -->
              <div class="esim-card-item__meta">
                <el-tooltip :content="card.eid" placement="top" :show-after="300">
                  <span class="mono">EID {{ eidShort(card.eid) }}</span>
                </el-tooltip>
              </div>

              <!-- Modem 信息 -->
              <div v-if="card.modem_model || card.modem_device_id" class="esim-card-item__meta">
                📡 {{ card.modem_model || card.modem_device_id }}
              </div>

              <!-- Profile 信息 -->
              <div class="esim-card-item__bottom">
                <el-tag
                  v-if="card.active_iccid"
                  size="small"
                  :style="{
                    backgroundColor: 'var(--ohmysms-profile-enabled-bg)',
                    color: 'var(--ohmysms-profile-enabled)',
                    borderColor: 'transparent',
                  }"
                >
                  🟢 活跃
                </el-tag>
                <el-tag v-else size="small" type="info" effect="plain">
                  ⚪ 无活跃
                </el-tag>
              </div>
            </div>

            <!-- 操作中遮罩 -->
            <div v-if="isCardOperating(card.id)" class="esim-card-item__overlay">
              <el-icon class="is-loading"><Refresh /></el-icon>
            </div>
          </div>
        </div>
      </aside>

      <!-- 右侧详情面板 -->
      <main
        class="esim-detail"
        :class="{ 'esim-detail--visible': showDetailOnMobile }"
      >
        <!-- 未选中状态 -->
        <div v-if="store.selectedCardId === null" class="esim-detail__empty">
          <div class="esim-detail__empty-icon">👈</div>
          <p>选择一张 eSIM 卡查看详情</p>
        </div>

        <!-- 详情内容 -->
        <div
          v-else
          v-loading="store.loadingDetail || isCardOperating(store.selectedCardId)"
          :element-loading-text="isCardOperating(store.selectedCardId) ? store.operatingText : '加载中...'"
          class="esim-detail__content"
        >
          <!-- 移动端返回 -->
          <el-button
            class="esim-detail__back"
            text
            @click="handleBackToList"
          >
            ← 返回列表
          </el-button>

          <template v-if="store.selectedCardDetail">
            <!-- Card 信息头 -->
            <div class="esim-detail__card-header">
              <div class="esim-detail__card-title">
                <div
                  class="esim-detail__vendor-badge"
                  :style="{
                    backgroundColor: vendorBgColor(store.selectedCardDetail.vendor),
                    color: vendorColor(store.selectedCardDetail.vendor),
                    borderColor: vendorColor(store.selectedCardDetail.vendor),
                  }"
                >
                  {{ vendorLabel(store.selectedCardDetail.vendor) }}
                </div>
                <h3>{{ store.selectedCardDetail.nickname || vendorLabel(store.selectedCardDetail.vendor) }}</h3>
                <el-button
                  text
                  size="small"
                  :icon="Edit"
                  @click="handleEditCardNickname(store.selectedCardDetail)"
                />
              </div>
              <el-button
                type="primary"
                plain
                size="small"
                :icon="Refresh"
                @click="handleDiscoverCard(store.selectedCardDetail.id)"
              >
                重新扫描
              </el-button>
              <el-button
                type="success"
                plain
                size="small"
                :icon="Plus"
                @click="handleOpenAddProfile"
                :disabled="isCardOperating(store.selectedCardDetail.id)"
              >
                添加 Profile
              </el-button>
            </div>

            <!-- Card 详细信息 -->
            <div class="esim-detail__info-grid">
              <div class="esim-detail__info-item">
                <span class="esim-detail__info-label">EID</span>
                <span class="esim-detail__info-value mono">
                  {{ store.selectedCardDetail.eid }}
                  <el-button
                    text
                    size="small"
                    :icon="CopyDocument"
                    @click="copyToClipboard(store.selectedCardDetail!.eid)"
                  />
                </span>
              </div>
              <div class="esim-detail__info-item">
                <span class="esim-detail__info-label">厂商</span>
                <span class="esim-detail__info-value">
                  {{ vendorLabel(store.selectedCardDetail.vendor) }}
                </span>
              </div>
              <div v-if="store.selectedCardDetail.euicc_firmware" class="esim-detail__info-item">
                <span class="esim-detail__info-label">固件版本</span>
                <span class="esim-detail__info-value mono">
                  {{ store.selectedCardDetail.euicc_firmware }}
                </span>
              </div>
              <div v-if="store.selectedCardDetail.free_nvm !== null" class="esim-detail__info-item">
                <span class="esim-detail__info-label">剩余空间</span>
                <span class="esim-detail__info-value">
                  {{ formatNvm(store.selectedCardDetail.free_nvm) }}
                </span>
              </div>
              <div v-if="store.selectedCardDetail.modem_model || store.selectedCardDetail.modem_device_id" class="esim-detail__info-item">
                <span class="esim-detail__info-label">承载模块</span>
                <span class="esim-detail__info-value">
                  {{ store.selectedCardDetail.modem_model || store.selectedCardDetail.modem_device_id }}
                  <el-tag
                    v-if="store.selectedCardDetail.transport"
                    size="small"
                    effect="plain"
                    round
                    style="margin-left: 6px"
                  >
                    {{ store.selectedCardDetail.transport?.toUpperCase() }}
                  </el-tag>
                </span>
              </div>
              <div v-if="store.selectedCardDetail.last_seen_at" class="esim-detail__info-item">
                <span class="esim-detail__info-label">最后发现</span>
                <span class="esim-detail__info-value">
                  {{ new Date(store.selectedCardDetail.last_seen_at).toLocaleString('zh-CN') }}
                </span>
              </div>
            </div>

            <!-- Profiles 区块 -->
            <el-divider>
              <span style="font-size: 14px; font-weight: 500">Profile 列表</span>
            </el-divider>

            <div v-if="store.profiles.length === 0" class="esim-detail__no-profiles">
              <p style="color: var(--el-text-color-secondary)">暂无 profile 数据</p>
              <div class="esim-detail__empty-actions">
                <el-button
                  type="primary"
                  plain
                  size="small"
                  @click="handleDiscoverCard(store.selectedCardDetail!.id)"
                >
                  扫描发现 profile
                </el-button>
                <el-button type="success" plain size="small" :icon="Plus" @click="handleOpenAddProfile">
                  添加 Profile
                </el-button>
              </div>
            </div>

            <div v-else class="esim-profiles">
              <div
                v-for="profile in store.profiles"
                :key="profile.iccid"
                class="esim-profile"
                :class="{
                  'esim-profile--enabled': profile.state === 'enabled',
                  'esim-profile--disabled': profile.state === 'disabled',
                }"
              >
                <div class="esim-profile__main">
                  <!-- 状态指示 + 运营商 -->
                  <div class="esim-profile__top">
                    <span
                      class="esim-profile__status-dot"
                      :class="profile.state === 'enabled' ? 'esim-profile__status-dot--enabled' : 'esim-profile__status-dot--disabled'"
                    />
                    <span class="esim-profile__operator">
                      {{ profile.service_provider || profile.profile_name || '未知运营商' }}
                    </span>
                    <el-tag
                      :type="profile.state === 'enabled' ? 'success' : 'info'"
                      size="small"
                      effect="dark"
                      round
                    >
                      {{ profile.state === 'enabled' ? '已启用' : '未启用' }}
                    </el-tag>
                  </div>

                  <!-- Profile 详情 -->
                  <div class="esim-profile__details">
                    <div class="esim-profile__detail-row">
                      <span class="esim-profile__detail-label">ICCID</span>
                      <el-tooltip :content="profile.iccid" placement="top" :show-after="300">
                        <span class="mono">{{ iccidShort(profile.iccid) }}</span>
                      </el-tooltip>
                    </div>
                    <div class="esim-profile__detail-row">
                      <span class="esim-profile__detail-label">昵称</span>
                      <span v-if="profile.nickname" class="esim-profile__nickname">
                        💾 {{ profile.nickname }}
                      </span>
                      <span v-else style="color: var(--el-text-color-disabled)">
                        —
                        <el-button
                          text
                          size="small"
                          :icon="Edit"
                          @click="handleEditProfileNickname(profile)"
                          :disabled="isCardOperating(store.selectedCardId!)"
                          style="margin-left: 2px"
                        />
                      </span>
                    </div>
                    <div v-if="profile.profile_class" class="esim-profile__detail-row">
                      <span class="esim-profile__detail-label">类型</span>
                      <span>{{ profile.profile_class }}</span>
                    </div>
                  </div>
                </div>

                <!-- 操作按钮 -->
                <div class="esim-profile__actions">
                  <el-button
                    v-if="profile.state === 'disabled'"
                    type="primary"
                    size="small"
                    @click="handleEnableProfile(profile)"
                    :disabled="isCardOperating(store.selectedCardId!)"
                  >
                    启用
                  </el-button>
                  <el-button
                    v-if="profile.state === 'enabled'"
                    type="warning"
                    size="small"
                    plain
                    @click="handleDisableProfile(profile)"
                    :disabled="isCardOperating(store.selectedCardId!)"
                  >
                    禁用
                  </el-button>
                  <el-button
                    text
                    size="small"
                    :icon="Edit"
                    @click="handleEditProfileNickname(profile)"
                    :disabled="isCardOperating(store.selectedCardId!)"
                  >
                    昵称
                  </el-button>
                  <el-button
                    text
                    type="danger"
                    size="small"
                    :icon="Delete"
                    @click="handleDeleteProfile(profile)"
                    :disabled="isCardOperating(store.selectedCardId!) || profile.state === 'enabled'"
                  >
                    删除
                  </el-button>
                </div>
              </div>
            </div>
          </template>
        </div>
      </main>
    </div>

    <!-- 昵称编辑弹窗 -->
    <el-dialog
      v-model="nicknameDialogVisible"
      :title="nicknameDialogTitle"
      width="400px"
      :close-on-click-modal="false"
    >
      <el-alert
        v-if="nicknameDialogHint"
        :title="nicknameDialogHint"
        type="info"
        :closable="false"
        show-icon
        style="margin-bottom: 16px"
      />
      <el-input
        v-model="nicknameDialogValue"
        placeholder="输入昵称（留空清除）"
        maxlength="50"
        show-word-limit
        clearable
        @keyup.enter="handleNicknameConfirm"
      />
      <template #footer>
        <el-button @click="nicknameDialogVisible = false">取消</el-button>
        <el-button
          type="primary"
          @click="handleNicknameConfirm"
          :loading="nicknameDialogLoading"
        >
          保存
        </el-button>
      </template>
    </el-dialog>

    <!-- 添加 Profile 弹窗 -->
    <el-dialog
      v-model="addDialogVisible"
      title="添加 eSIM Profile"
      width="560px"
      :close-on-click-modal="false"
      @closed="handleAddDialogClosed"
    >
      <el-alert
        title="可粘贴 LPA 二维码内容，或手动填写 SM-DP+ / Matching ID。"
        type="info"
        :closable="false"
        show-icon
        style="margin-bottom: 16px"
      />
      <el-radio-group v-model="addMode" style="margin-bottom: 16px">
        <el-radio-button label="activation">LPA 激活码</el-radio-button>
        <el-radio-button label="manual">手动信息</el-radio-button>
        <el-radio-button label="qr">扫描二维码</el-radio-button>
      </el-radio-group>

      <div v-if="addMode === 'activation'" class="esim-add-form">
        <el-input
          v-model="addActivationCode"
          type="textarea"
          :rows="3"
          placeholder="LPA:1$<SM-DP+ 地址>$<Matching ID>"
        />
      </div>
      <div v-else-if="addMode === 'manual'" class="esim-add-form">
        <el-input v-model="addSMDPAddress" placeholder="SM-DP+ 地址" />
        <el-input v-model="addMatchingID" placeholder="Matching ID / Activation Code" />
      </div>
      <div v-else class="esim-add-form">
        <video ref="qrVideoRef" class="esim-add-form__video" muted playsinline />
        <div class="esim-add-form__qr-actions">
          <el-button :icon="Camera" @click="startQrScan" :loading="qrScanning">重新打开摄像头</el-button>
          <el-button @click="stopQrScan">停止扫描</el-button>
        </div>
        <el-input
          v-model="addActivationCode"
          type="textarea"
          :rows="2"
          placeholder="扫描后会自动填入，也可手动粘贴"
        />
      </div>
      <el-input
        v-model="addConfirmationCode"
        placeholder="Confirmation Code（如运营商要求，可选）"
        show-password
        clearable
        style="margin-top: 12px"
      />
      <template #footer>
        <el-button @click="addDialogVisible = false">取消</el-button>
        <el-button type="primary" @click="handleAddProfile" :loading="addDialogLoading">
          添加
        </el-button>
      </template>
    </el-dialog>
  </div>
</template>

<style scoped lang="scss">
// ─── Page ───
.esim-page {
  max-width: 1400px;

  &__header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 20px;

    h2 {
      font-size: 22px;
      font-weight: 600;
    }

    &-actions {
      display: flex;
      align-items: center;
      gap: 8px;
    }
  }
}

// ─── Empty State ───
.esim-empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  padding: 80px 20px;
  text-align: center;

  &__icon {
    font-size: 56px;
    margin-bottom: 16px;
    filter: grayscale(0.2);
  }

  &__title {
    font-size: 18px;
    font-weight: 500;
    color: var(--ohmysms-text-primary);
    margin-bottom: 20px;
  }

  &__hint {
    margin-top: 16px;
    font-size: 13px;
    line-height: 1.8;
    color: var(--ohmysms-text-secondary);
  }
}

// ─── Body (列表 + 详情) ───
.esim-body {
  display: flex;
  gap: 20px;
  min-height: 500px;
}

// ─── 侧栏 Card 列表 ───
.esim-sidebar {
  width: 320px;
  min-width: 320px;
  max-height: calc(100vh - 180px);
  overflow-y: auto;
  flex-shrink: 0;

  &__list {
    display: flex;
    flex-direction: column;
    gap: 12px;
  }
}

// ─── Card 列表项 ───
.esim-card-item {
  position: relative;
  background: var(--ohmysms-bg-card);
  border-radius: 12px;
  padding: 16px 16px 16px 20px;
  cursor: pointer;
  border: 1.5px solid transparent;
  overflow: hidden;
  transition:
    transform 0.2s ease,
    box-shadow 0.2s ease,
    border-color 0.2s ease;

  &:hover {
    transform: translateY(-1px);
    box-shadow: 0 4px 12px rgba(0, 0, 0, 0.06);
  }

  &--active {
    border-color: var(--ohmysms-primary);
    box-shadow: 0 2px 12px var(--ohmysms-primary-light);
  }

  &--operating {
    pointer-events: none;
    opacity: 0.7;
  }

  // 左侧 vendor 色条
  &__accent {
    position: absolute;
    left: 0;
    top: 0;
    bottom: 0;
    width: 4px;
    border-radius: 12px 0 0 12px;
  }

  &__content {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }

  &__top {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
  }

  &__vendor {
    font-size: 12px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
  }

  &__name {
    font-size: 15px;
    font-weight: 600;
    color: var(--ohmysms-text-primary);
    display: flex;
    align-items: center;
    gap: 6px;
  }

  &__edit-icon {
    font-size: 13px;
    color: var(--ohmysms-text-secondary);
    opacity: 0;
    transition: opacity 0.15s;
    cursor: pointer;

    &:hover {
      color: var(--ohmysms-primary);
    }
  }

  &:hover &__edit-icon {
    opacity: 1;
  }

  &__meta {
    font-size: 12px;
    color: var(--ohmysms-text-secondary);
  }

  &__bottom {
    display: flex;
    align-items: center;
    gap: 6px;
    margin-top: 2px;
  }

  // 操作中遮罩
  &__overlay {
    position: absolute;
    inset: 0;
    background: rgba(255, 255, 255, 0.7);
    display: flex;
    align-items: center;
    justify-content: center;
    font-size: 24px;
    color: var(--ohmysms-primary);
    border-radius: 12px;

    :global(html.dark) & {
      background: rgba(30, 41, 59, 0.7);
    }
  }
}

// ─── 详情面板 ───
.esim-detail {
  flex: 1;
  min-width: 0;
  background: var(--ohmysms-bg-card);
  border-radius: 12px;
  overflow: hidden;

  &__empty {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    height: 400px;
    color: var(--ohmysms-text-secondary);

    &-icon {
      font-size: 40px;
      margin-bottom: 12px;
      opacity: 0.5;
    }

    p {
      font-size: 14px;
    }
  }

  &__content {
    padding: 24px;
    min-height: 400px;
  }

  &__back {
    display: none;
    margin-bottom: 16px;
    font-size: 14px;
  }

  // Card 信息头
  &__card-header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 20px;
    gap: 12px;
    flex-wrap: wrap;
  }

  &__card-title {
    display: flex;
    align-items: center;
    gap: 10px;

    h3 {
      font-size: 18px;
      font-weight: 600;
    }
  }

  &__vendor-badge {
    display: inline-flex;
    align-items: center;
    padding: 4px 10px;
    border-radius: 6px;
    font-size: 12px;
    font-weight: 600;
    letter-spacing: 0.5px;
    text-transform: uppercase;
    border: 1px solid;
  }

  // 信息网格
  &__info-grid {
    display: grid;
    grid-template-columns: repeat(auto-fill, minmax(280px, 1fr));
    gap: 12px 24px;
    margin-bottom: 8px;
  }

  &__info-item {
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: 8px 0;
  }

  &__info-label {
    font-size: 12px;
    color: var(--ohmysms-text-secondary);
    font-weight: 500;
  }

  &__info-value {
    font-size: 14px;
    color: var(--ohmysms-text-primary);
    display: flex;
    align-items: center;
    gap: 4px;
    word-break: break-all;
  }

  // 无 profile 提示
  &__no-profiles {
    display: flex;
    flex-direction: column;
    align-items: center;
    gap: 12px;
    padding: 32px 0;
    text-align: center;
  }

  &__empty-actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    justify-content: center;
  }
}

.esim-add-form {
  display: flex;
  flex-direction: column;
  gap: 12px;

  &__video {
    width: 100%;
    min-height: 260px;
    background: #000;
    border-radius: 8px;
    object-fit: cover;
  }

  &__qr-actions {
    display: flex;
    gap: 8px;
  }
}

// ─── Profile 列表 ───
.esim-profiles {
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.esim-profile {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  padding: 16px;
  border-radius: 10px;
  border: 1px solid var(--el-border-color-light);
  transition:
    background-color 0.15s,
    border-color 0.15s;

  &--enabled {
    background: var(--ohmysms-profile-enabled-bg);
    border-color: var(--ohmysms-profile-enabled);
  }

  &--disabled {
    background: var(--ohmysms-profile-disabled-bg);
  }

  &__main {
    flex: 1;
    min-width: 0;
    display: flex;
    flex-direction: column;
    gap: 8px;
  }

  &__top {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }

  &__status-dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    flex-shrink: 0;

    &--enabled {
      background: var(--ohmysms-profile-enabled);
      box-shadow: 0 0 6px rgba(16, 185, 129, 0.4);
    }

    &--disabled {
      background: var(--ohmysms-profile-disabled);
    }
  }

  &__operator {
    font-size: 16px;
    font-weight: 600;
    color: var(--ohmysms-text-primary);
  }

  &__details {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  &__detail-row {
    display: flex;
    align-items: center;
    gap: 8px;
    font-size: 13px;
  }

  &__detail-label {
    color: var(--ohmysms-text-secondary);
    min-width: 44px;
    font-size: 12px;
  }

  &__nickname {
    color: var(--ohmysms-text-primary);
  }

  &__actions {
    display: flex;
    flex-direction: column;
    gap: 6px;
    flex-shrink: 0;
    align-items: flex-end;
  }
}

// ─── Mono 字体 ───
.mono {
  font-family: 'SF Mono', 'Fira Code', 'Consolas', monospace;
  font-size: 12px;
}

// ─── 响应式：平板 ───
@media (max-width: 1024px) {
  .esim-sidebar {
    width: 260px;
    min-width: 260px;
  }
}

// ─── 响应式：手机 ───
@media (max-width: 768px) {
  .esim-body {
    flex-direction: column;
    gap: 0;
  }

  .esim-sidebar {
    width: 100%;
    min-width: 0;
    max-height: none;

    &--hidden {
      display: none;
    }
  }

  .esim-detail {
    display: none;

    &--visible {
      display: block;
    }

    &__back {
      display: inline-flex;
    }
  }

  .esim-profile {
    flex-direction: column;

    &__actions {
      flex-direction: row;
      align-items: flex-start;
      width: 100%;
    }
  }
}
</style>
