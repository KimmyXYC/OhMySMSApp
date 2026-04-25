# Frontend

基于 Vue 3 + Vite + TypeScript + Pinia + Element Plus 的管理界面。

## 开发

```bash
npm install
npm run dev
```

## 构建

```bash
npm run build
```

## 预览

```bash
npm run preview
```

## 类型检查

```bash
npm run type-check
```

## 环境变量

默认后端地址：

```env
VITE_BACKEND_URL=http://localhost:8080
```

如需覆盖，可在 `.env.local` 中设置：

```env
VITE_BACKEND_URL=http://example-host:8080
```

## 目录结构

```text
src/
├── api/
├── components/
├── composables/
├── router/
├── stores/
├── types/
└── views/
```

## 与仓库根 Makefile 配合

```bash
make frontend-install
make frontend-dev
make frontend-build
```
