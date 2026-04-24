import { ref, onUnmounted } from 'vue'
import { useAuthStore } from '@/stores/auth'
import { useModemsStore } from '@/stores/modems'
import { useSmsStore } from '@/stores/sms'
import { useSimsStore } from '@/stores/sims'
import type { WsMessage, Modem, Sms, SignalSample, Sim } from '@/types/api'

const RECONNECT_DELAYS = [1000, 2000, 5000, 10000, 30000] // ms

/** 全局单例 */
let ws: WebSocket | null = null
let reconnectTimer: ReturnType<typeof setTimeout> | null = null
let reconnectAttempt = 0

const connected = ref(false)

export function useWebSocket() {
  function getWsUrl(): string {
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    const token = useAuthStore().token
    return `${proto}//${location.host}/ws?token=${encodeURIComponent(token || '')}`
  }

  function connect() {
    if (ws && (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING)) {
      return
    }

    const url = getWsUrl()
    ws = new WebSocket(url)

    ws.onopen = () => {
      connected.value = true
      reconnectAttempt = 0
      console.info('[WS] connected')
    }

    ws.onclose = (e) => {
      connected.value = false
      console.warn('[WS] closed', e.code, e.reason)
      if (e.code !== 1000) {
        scheduleReconnect()
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
      case 'modem.added':
      case 'modem.updated':
        modemsStore.upsertModem(msg.payload as Modem)
        break
      case 'modem.removed':
        modemsStore.removeModem((msg.payload as { id: number }).id)
        break
      case 'signal.update': {
        const signal = msg.payload as SignalSample
        const modem = modemsStore.modems.find((m) => m.id === signal.modem_id)
        if (modem) {
          modem.signal = signal
        }
        break
      }
      case 'sms.new':
        smsStore.addIncomingSms(msg.payload as Sms)
        break
      case 'sim.updated':
        simsStore.upsertSim(msg.payload as Sim)
        break
      default:
        console.debug('[WS] unhandled message type:', msg.type)
    }
  }

  onUnmounted(() => {
    // 组件卸载时不断开全局连接，只在 logout 时断开
  })

  return {
    connected,
    connect,
    disconnect,
  }
}
