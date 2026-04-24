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
        redirect: '/dashboard',
      },
      {
        path: 'dashboard',
        name: 'dashboard',
        component: () => import('@/views/DashboardView.vue'),
      },
      {
        path: 'modems/:deviceId',
        name: 'modem-detail',
        component: () => import('@/views/ModemDetailView.vue'),
        props: true,
      },
      {
        path: 'sims',
        name: 'sims',
        component: () => import('@/views/SimsView.vue'),
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
      {
        path: 'settings',
        name: 'settings',
        component: () => import('@/views/SettingsView.vue'),
      },
    ],
  },
  {
    path: '/:pathMatch(.*)*',
    redirect: '/dashboard',
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

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
