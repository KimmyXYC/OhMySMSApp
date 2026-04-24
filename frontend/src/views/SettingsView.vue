<script setup lang="ts">
import { ref, onMounted, computed } from 'vue'
import { getTelegramSettings, putTelegramSettings, testTelegramPush } from '@/api/settings'
import { changePassword } from '@/api/auth'
import { ElMessage, ElMessageBox } from 'element-plus'

const loading = ref(true)
const saving = ref(false)
const testSending = ref(false)

// Telegram 表单
const hasToken = ref(false)
const source = ref('')
const form = ref({
  bot_token: '',
  chat_id: 0 as number,
  push_sms: false,
})

// 密码表单
const passwordForm = ref({
  current_password: '',
  new_password: '',
  confirm_password: '',
})
const passwordSaving = ref(false)

// 密码验证
const passwordValid = computed(() => {
  const { current_password, new_password, confirm_password } = passwordForm.value
  return (
    current_password.length > 0 &&
    new_password.length >= 6 &&
    new_password === confirm_password
  )
})

const passwordMismatch = computed(() => {
  const { new_password, confirm_password } = passwordForm.value
  return confirm_password.length > 0 && new_password !== confirm_password
})

const passwordTooShort = computed(() => {
  const { new_password } = passwordForm.value
  return new_password.length > 0 && new_password.length < 6
})

async function loadSettings() {
  loading.value = true
  try {
    const { data } = await getTelegramSettings()
    hasToken.value = data.has_token
    source.value = data.source
    form.value.chat_id = data.chat_id
    form.value.push_sms = data.push_sms
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || '加载设置失败')
  } finally {
    loading.value = false
  }
}

async function handleSave() {
  saving.value = true
  try {
    const payload: Record<string, any> = {
      chat_id: form.value.chat_id,
      push_sms: form.value.push_sms,
    }
    if (form.value.bot_token) {
      payload.bot_token = form.value.bot_token
    }
    const { data } = await putTelegramSettings(payload)
    hasToken.value = data.has_token
    source.value = data.source
    form.value.bot_token = ''
    ElMessage.success('设置已保存')
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || '保存失败')
  } finally {
    saving.value = false
  }
}

async function handleTestPush() {
  try {
    const { value } = await ElMessageBox.prompt('输入测试消息内容', '测试 Telegram 推送', {
      confirmButtonText: '发送',
      cancelButtonText: '取消',
      inputValue: 'Hello from ohmysmsapp',
      inputPlaceholder: '测试消息内容...',
    })
    testSending.value = true
    try {
      await testTelegramPush(value || undefined)
      ElMessage.success('测试消息已发送，请查看 Telegram')
    } catch (e: any) {
      const status = e.response?.status
      if (status === 412) {
        ElMessage.warning('请先配置 Bot Token 和 Chat ID')
      } else {
        ElMessage.error(e.response?.data?.error || '发送失败')
      }
    } finally {
      testSending.value = false
    }
  } catch {
    // 用户取消
  }
}

async function handleChangePassword() {
  if (!passwordValid.value) return
  passwordSaving.value = true
  try {
    await changePassword(passwordForm.value.current_password, passwordForm.value.new_password)
    ElMessage.success('密码已更新')
    // 清空表单
    passwordForm.value = {
      current_password: '',
      new_password: '',
      confirm_password: '',
    }
  } catch (e: any) {
    const status = e.response?.status
    if (status === 401) {
      ElMessage.error('当前密码错误')
    } else {
      ElMessage.error(e.response?.data?.error || '修改密码失败')
    }
  } finally {
    passwordSaving.value = false
  }
}

onMounted(() => {
  loadSettings()
})
</script>

<template>
  <div class="page-container" style="max-width: 800px">
    <h2 style="margin-bottom: 24px">设置</h2>

    <!-- Telegram 推送 -->
    <el-card shadow="never" class="settings-card" v-loading="loading">
      <template #header>
        <div class="settings-card__header">
          <span class="settings-card__title">Telegram 推送</span>
          <el-tag v-if="source" size="small" type="info">
            来源: {{ source === 'settings' ? '数据库' : '配置文件' }}
          </el-tag>
        </div>
      </template>

      <el-form label-width="120px" label-position="left">
        <el-form-item label="Bot Token">
          <el-input
            v-model="form.bot_token"
            type="password"
            show-password
            :placeholder="hasToken ? '已配置（输入新值可覆盖）' : '输入 Bot Token'"
            style="max-width: 400px"
          />
          <span v-if="hasToken" class="field-hint">
            <el-tag type="success" size="small" effect="plain">已配置</el-tag>
          </span>
        </el-form-item>

        <el-form-item label="Chat ID">
          <el-input-number
            v-model="form.chat_id"
            :controls="false"
            style="max-width: 200px"
            placeholder="Telegram Chat ID"
          />
        </el-form-item>

        <el-form-item label="推送短信">
          <el-switch v-model="form.push_sms" />
          <span class="field-hint" style="margin-left: 8px; color: var(--el-text-color-secondary); font-size: 13px">
            收到新短信时推送到 Telegram
          </span>
        </el-form-item>

        <el-form-item>
          <el-button type="primary" :loading="saving" @click="handleSave">
            保存
          </el-button>
          <el-button
            :loading="testSending"
            :disabled="!hasToken"
            @click="handleTestPush"
          >
            测试发送
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>

    <!-- 账户设置 -->
    <el-card shadow="never" class="settings-card" style="margin-top: 20px">
      <template #header>
        <span class="settings-card__title">账户</span>
      </template>

      <el-form label-width="120px" label-position="left">
        <el-form-item label="当前密码">
          <el-input
            v-model="passwordForm.current_password"
            type="password"
            show-password
            placeholder="输入当前密码"
            style="max-width: 320px"
          />
        </el-form-item>

        <el-form-item label="新密码" :error="passwordTooShort ? '密码至少 6 个字符' : ''">
          <el-input
            v-model="passwordForm.new_password"
            type="password"
            show-password
            placeholder="输入新密码（至少 6 位）"
            style="max-width: 320px"
          />
        </el-form-item>

        <el-form-item label="确认密码" :error="passwordMismatch ? '两次密码不一致' : ''">
          <el-input
            v-model="passwordForm.confirm_password"
            type="password"
            show-password
            placeholder="再次输入新密码"
            style="max-width: 320px"
            @keyup.enter="handleChangePassword"
          />
        </el-form-item>

        <el-form-item>
          <el-button
            type="primary"
            :loading="passwordSaving"
            :disabled="!passwordValid"
            @click="handleChangePassword"
          >
            修改登录密码
          </el-button>
        </el-form-item>
      </el-form>
    </el-card>
  </div>
</template>

<style scoped lang="scss">
.settings-card {
  border-radius: 12px;

  &__header {
    display: flex;
    align-items: center;
    justify-content: space-between;
  }

  &__title {
    font-size: 16px;
    font-weight: 600;
  }
}

.field-hint {
  margin-left: 12px;
}
</style>
