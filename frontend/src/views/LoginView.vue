<script setup lang="ts">
import { ref, reactive, computed, onMounted } from 'vue'
import { useAuth } from '@/composables/useAuth'
import { useBackendStore, type KnownBackend } from '@/stores/backend'
import { rebuildClient } from '@/api/client'
import { ElMessage } from 'element-plus'
import { Lock, User, Link, Monitor } from '@element-plus/icons-vue'
import type { FormInstance, FormRules } from 'element-plus'

const { login } = useAuth()
const backendStore = useBackendStore()

const formRef = ref<FormInstance>()
const loading = ref(false)
const form = reactive({
  backend: backendStore.current || '',
  username: '',
  password: '',
})

const rules: FormRules = {
  username: [{ required: true, message: '请输入用户名', trigger: 'blur' }],
  password: [{ required: true, message: '请输入密码', trigger: 'blur' }],
}

const recentBackends = computed(() => backendStore.recentBackends)
const hasRecent = computed(() => recentBackends.value.length > 0)

function useSameOrigin() {
  form.backend = ''
}

function selectRecent(item: KnownBackend) {
  form.backend = item.url
  form.username = item.username
}

async function handleSubmit() {
  const valid = await formRef.value?.validate().catch(() => false)
  if (!valid) return

  loading.value = true
  try {
    // 1. Set backend URL
    backendStore.setCurrent(form.backend)
    // 2. Rebuild HTTP client for new backend
    rebuildClient()
    // 3. Login
    await login({ username: form.username, password: form.password })
    ElMessage.success('登录成功')
  } catch (e: any) {
    ElMessage.error(e.response?.data?.error || e.message || '登录失败，请检查后端地址、用户名和密码')
  } finally {
    loading.value = false
  }
}

onMounted(() => {
  // If there's a stored backend, fill it in
  if (backendStore.current) {
    form.backend = backendStore.current
  }
})
</script>

<template>
  <div class="login-view">
    <!-- Decorative background layers -->
    <div class="login-view__bg" />
    <div class="login-view__grid" />

    <div class="login-card">
      <div class="login-card__header">
        <img src="/favicon.svg" alt="OhMySMS" width="44" height="44" class="login-card__logo" />
        <h1 class="login-card__title">OhMySMS</h1>
        <p class="login-card__subtitle">短信管理系统</p>
      </div>

      <el-form
        ref="formRef"
        :model="form"
        :rules="rules"
        size="large"
        @submit.prevent="handleSubmit"
      >
        <!-- 后端地址 -->
        <el-form-item prop="backend">
          <el-input
            v-model="form.backend"
            placeholder="后端地址 (留空使用当前站点)"
            :prefix-icon="Link"
            clearable
          >
            <template #append>
              <el-tooltip content="使用当前站点 (同源)" placement="top">
                <el-button :icon="Monitor" @click="useSameOrigin" />
              </el-tooltip>
            </template>
          </el-input>
        </el-form-item>

        <el-form-item prop="username">
          <el-input
            v-model="form.username"
            placeholder="用户名"
            :prefix-icon="User"
          />
        </el-form-item>

        <el-form-item prop="password">
          <el-input
            v-model="form.password"
            type="password"
            placeholder="密码"
            :prefix-icon="Lock"
            show-password
            @keyup.enter="handleSubmit"
          />
        </el-form-item>

        <el-form-item>
          <el-button
            type="primary"
            :loading="loading"
            class="login-card__submit"
            @click="handleSubmit"
          >
            连接并登录
          </el-button>
        </el-form-item>
      </el-form>

      <!-- 已保存的后端 -->
      <div v-if="hasRecent" class="login-card__recent">
        <div class="login-card__recent-title">已保存的后端</div>
        <div
          v-for="item in recentBackends"
          :key="item.url"
          class="login-card__recent-item"
          @click="selectRecent(item)"
        >
          <div class="login-card__recent-info">
            <span class="login-card__recent-url">{{ item.url || '同源模式' }}</span>
            <span class="login-card__recent-user">{{ item.username }}</span>
          </div>
          <el-icon class="login-card__recent-arrow"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><path d="M9 18l6-6-6-6"/></svg></el-icon>
        </div>
      </div>

      <!-- CORS 提示 -->
      <div class="login-card__hint">
        如果连接失败，请检查后端 <code>server.allowed_origins</code> 是否包含当前页面地址
      </div>
    </div>
  </div>
</template>

<style scoped lang="scss">
.login-view {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
  position: relative;
  overflow: hidden;
  background: var(--ohmysms-login-bg, linear-gradient(145deg, #dbeafe 0%, #e0e7ff 40%, #f0f9ff 100%));

  &__bg {
    position: absolute;
    inset: 0;
    background:
      radial-gradient(ellipse at 20% 50%, rgba(96, 165, 250, 0.15) 0%, transparent 50%),
      radial-gradient(ellipse at 80% 20%, rgba(147, 197, 253, 0.2) 0%, transparent 50%),
      radial-gradient(ellipse at 60% 80%, rgba(191, 219, 254, 0.15) 0%, transparent 50%);
    pointer-events: none;
  }

  &__grid {
    position: absolute;
    inset: 0;
    background-image:
      linear-gradient(rgba(148, 163, 184, 0.06) 1px, transparent 1px),
      linear-gradient(90deg, rgba(148, 163, 184, 0.06) 1px, transparent 1px);
    background-size: 40px 40px;
    pointer-events: none;
  }
}

// Deep: dark mode override
:global(html.dark) .login-view {
  background: linear-gradient(145deg, #0f172a 0%, #1e293b 50%, #0f172a 100%);

  .login-view__bg {
    background:
      radial-gradient(ellipse at 20% 50%, rgba(56, 189, 248, 0.06) 0%, transparent 50%),
      radial-gradient(ellipse at 80% 20%, rgba(96, 165, 250, 0.08) 0%, transparent 50%),
      radial-gradient(ellipse at 60% 80%, rgba(125, 211, 252, 0.04) 0%, transparent 50%);
  }

  .login-view__grid {
    background-image:
      linear-gradient(rgba(148, 163, 184, 0.03) 1px, transparent 1px),
      linear-gradient(90deg, rgba(148, 163, 184, 0.03) 1px, transparent 1px);
  }
}

.login-card {
  background: var(--el-bg-color);
  border-radius: 20px;
  padding: 40px 36px 28px;
  width: 100%;
  max-width: 420px;
  box-shadow:
    0 0 0 1px rgba(148, 163, 184, 0.1),
    0 20px 50px -12px rgba(0, 0, 0, 0.08),
    0 4px 16px -2px rgba(0, 0, 0, 0.04);
  position: relative;
  z-index: 1;
  animation: cardIn 0.5s cubic-bezier(0.16, 1, 0.3, 1) both;

  &__header {
    text-align: center;
    margin-bottom: 28px;
  }

  &__logo {
    margin-bottom: 12px;
    filter: drop-shadow(0 2px 8px rgba(96, 165, 250, 0.3));
  }

  &__title {
    font-size: 26px;
    font-weight: 700;
    color: var(--el-text-color-primary);
    margin-bottom: 4px;
    letter-spacing: -0.5px;
  }

  &__subtitle {
    font-size: 14px;
    color: var(--el-text-color-secondary);
  }

  &__submit {
    width: 100%;
    font-weight: 600;
    letter-spacing: 0.3px;
  }

  &__recent {
    margin-top: 20px;
    padding-top: 16px;
    border-top: 1px solid var(--el-border-color-lighter);
  }

  &__recent-title {
    font-size: 12px;
    color: var(--el-text-color-placeholder);
    text-transform: uppercase;
    letter-spacing: 0.5px;
    margin-bottom: 8px;
  }

  &__recent-item {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 8px 10px;
    border-radius: 8px;
    cursor: pointer;
    transition: background 0.15s;

    &:hover {
      background: var(--el-fill-color-light);
    }
  }

  &__recent-info {
    display: flex;
    flex-direction: column;
    min-width: 0;
  }

  &__recent-url {
    font-size: 13px;
    font-weight: 500;
    color: var(--el-text-color-primary);
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }

  &__recent-user {
    font-size: 11px;
    color: var(--el-text-color-secondary);
  }

  &__recent-arrow {
    color: var(--el-text-color-placeholder);
    flex-shrink: 0;
  }

  &__hint {
    margin-top: 16px;
    padding-top: 12px;
    border-top: 1px solid var(--el-border-color-extra-light);
    font-size: 11px;
    color: var(--el-text-color-placeholder);
    line-height: 1.6;
    text-align: center;

    code {
      background: var(--el-fill-color);
      padding: 1px 5px;
      border-radius: 3px;
      font-size: 10.5px;
    }
  }
}

@keyframes cardIn {
  from {
    opacity: 0;
    transform: translateY(16px) scale(0.98);
  }
  to {
    opacity: 1;
    transform: translateY(0) scale(1);
  }
}
</style>
