# OhMySMS Frontend

基于 **Vue 3 + Vite + TypeScript + Pinia + Element Plus** 的前端应用，用于树莓派 4G 模块短信管理。

## 快速开始

```bash
# 安装依赖
npm install

# 启动开发服务器（默认代理到 localhost:8080）
npm run dev

# 构建生产版本
npm run build

# 预览生产构建
npm run preview

# 类型检查
npm run type-check
```

## 环境变量

| 变量 | 默认值 | 说明 |
|------|--------|------|
| `VITE_BACKEND_URL` | `http://localhost:8080` | 后端地址，dev server 代理目标 |

可在项目根目录创建 `.env.local` 覆盖：

```env
VITE_BACKEND_URL=http://192.168.1.100:8080
```

## 目录结构

```
src/
├── api/           API 请求层（axios 封装，按模块拆分）
├── assets/        静态资源
├── components/    可复用组件
├── composables/   组合式函数（WebSocket、Auth 等）
├── layouts/       页面布局
├── router/        路由配置
├── stores/        Pinia 状态管理
├── styles/        全局样式 + Element Plus 主题覆盖
├── types/         TypeScript 类型定义
└── views/         页面视图
```

## 与 Makefile 配合

```bash
make frontend-install   # npm install
make frontend-dev       # npm run dev
make frontend-build     # npm run build + 复制到 deploy/build/web/
```

## 技术栈版本

- Vue 3.5+
- Vite 6+
- TypeScript 5.7+
- Pinia 2.3+
- Element Plus 2.9+
- Vue Router 4.5+
- Axios 1.7+
