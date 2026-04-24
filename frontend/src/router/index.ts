import { createRouter, createWebHistory, type RouteRecordRaw } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'login',
    component: () => import('@/views/LoginView.vue'),
    meta: { requiresAuth: false },
  },
  {
    path: '/',
    component: () => import('@/layouts/MainLayout.vue'),
    meta: { requiresAuth: true },
    children: [
      {
        path: '',
        name: 'dashboard',
        component: () => import('@/views/DashboardView.vue'),
      },
      {
        path: 'modem/:id',
        name: 'modem-detail',
        component: () => import('@/views/ModemDetailView.vue'),
        props: true,
      },
      {
        path: 'sms',
        name: 'sms',
        component: () => import('@/views/SmsView.vue'),
      },
      {
        path: 'ussd',
        name: 'ussd',
        component: () => import('@/views/UssdView.vue'),
      },
      {
        path: 'esim',
        name: 'esim',
        component: () => import('@/views/ESimView.vue'),
      },
    ],
  },
  // 兜底
  {
    path: '/:pathMatch(.*)*',
    redirect: '/',
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

// 全局前置守卫
router.beforeEach((to, _from, next) => {
  const auth = useAuthStore()

  if (to.meta.requiresAuth !== false && !auth.isAuthenticated) {
    next({ name: 'login', query: { redirect: to.fullPath } })
  } else if (to.name === 'login' && auth.isAuthenticated) {
    next({ name: 'dashboard' })
  } else {
    next()
  }
})

export default router
