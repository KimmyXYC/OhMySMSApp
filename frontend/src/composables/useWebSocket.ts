import { ref } from 'vue'
import { ElNotification } from 'element-plus'
import { useAuthStore } from '@/stores/auth'
import { useBackendStore } from '@/stores/backend'
import { useModemsStore } from '@/stores/modems'
import { useSmsStore } from '@/stores/sms'
import { useSimsStore } from '@/stores/sims'
import type { WsMessage, ModemState, SignalSample } from '@/types/api'
import router from '@/router'

const RECONNECT_DELAYS = [1000, 2000, 5000, 10000, 30000]

/** 全局单例 */
let ws: WebSocket | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
let reconnectAttempt = 0

export type WsStatus = 'connected' | 'reconnecting' | 'disconnected'

const status = ref<WsStatus>('disconnected')
const connected = ref(false)

export function useWebSocket() {
  function getWsUrl(): string {
    const backendStore = useBackendStore()
    const authStore = useAuthStore()
    const wsBase = backendStore.resolvedWsBase
    const token = authStore.token
    return `${wsBase}/ws?token=${encodeURIComponent(token || '')}`
  }

  function connect() {
    if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
      return
    }

    const url = getWsUrl()
    ws = new WebSocket(url)
    status.value = 'reconnecting'

    ws.onopen = () => {
      connected.value = true
      status.value = 'connected'
      reconnectAttempt = 0
      console.info('[WS] connected')
    }

    ws.onclose = (e) => {
      connected.value = false
      console.warn('[WS] closed', e.code, e.reason)
      if (e.code !== 1000) {
        status.value = 'reconnecting'
        scheduleReconnect()
      } else {
        status.value = 'disconnected'
      }
    }

    ws.onerror = (e) => {
      console.error('[WS] error', e)
    }

    ws.onmessage = (e) => {
      try {
        const msg: WsMessage = JSON.parse(e.data)
        dispatch(msg)
      } catch (err) {
        console.warn('[WS] invalid message', e.data)
      }
    }
  }

  function disconnect() {
    if (reconnectTimer) {
      clearTimeout(reconnectTimer)
      reconnectTimer = null
    }
    if (ws) {
      ws.close(1000, 'user disconnect')
      ws = null
    }
    connected.value = false
    status.value = 'disconnected'
  }

  function scheduleReconnect() {
    if (reconnectTimer) return
    const delay = RECONNECT_DELAYS[Math.min(reconnectAttempt, RECONNECT_DELAYS.length - 1)]
    reconnectAttempt++
    console.info(`[WS] reconnecting in ${delay}ms (attempt ${reconnectAttempt})`)
    reconnectTimer = setTimeout(() => {
      reconnectTimer = null
      connect()
    }, delay)
  }

  function dispatch(msg: WsMessage) {
    const modemsStore = useModemsStore()
    const smsStore = useSmsStore()
    const simsStore = useSimsStore()

    switch (msg.type) {
      case 'hello':
        console.info('[WS] server version:', msg.data?.server_version)
        break

      case 'modem.added':
      case 'modem.updated':
        modemsStore.handleModemState(msg.data as ModemState)
        break

      case 'modem.removed':
        modemsStore.handleModemRemoved((msg.data as { device_id: string }).device_id)
        break

      case 'signal.sample':
        modemsStore.handleSignalSample(msg.data as SignalSample)
        break

      case 'sim.updated': {
        // 刷新 sims & modems
        simsStore.fetchSims()
        modemsStore.fetchModems()
        break
      }

      case 'sms.received': {
        const smsData = msg.data as { device_id: string; sms: any }
        const peer = smsData.sms?.peer || ''
        const text = smsData.sms?.text || smsData.sms?.body || ''

        // 全局通知
        ElNotification({
          title: `新短信 ${peer}`,
          message: text.length > 80 ? text.slice(0, 80) + '...' : text,
          type: 'info',
          duration: 5000,
          onClick: () => {
            router.push({ name: 'sms', query: { peer } })
          },
        })

        // 如果当前正在看这个 peer 的会话，刷新
        const currentPeer = smsStore.currentMessages[0]?.peer
        if (currentPeer === peer) {
          smsStore.refreshCurrentThread(peer)
        } else {
          // 标记未读
          smsStore.markUnread(peer)
        }

        // 刷新 threads 列表
        smsStore.fetchThreads()
        break
      }

      case 'sms.state_changed': {
        // 刷新当前消息
        if (smsStore.currentMessages.length > 0) {
          const peer = smsStore.currentMessages[0].peer
          smsStore.refreshCurrentThread(peer)
        }
        break
      }

      case 'ussd.state':
        // 由 UssdView 自己通过 watch 处理
        break

      default:
        console.debug('[WS] unhandled message type:', msg.type)
    }
  }

  return {
    connected,
    status,
    connect,
    disconnect,
  }
}
