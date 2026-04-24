<script setup lang="ts">
// TODO 阶段 3：eSIM profile 管理
</script>

<template>
  <div class="page-container">
    <h2>eSIM 管理</h2>
    <p style="color: var(--el-text-color-secondary); margin-top: 12px">
      管理 Sticker eSIM Profile：查看、启用/禁用、设置别名
    </p>

    <el-divider />

    <el-card shadow="never">
      <el-empty description="TODO 阶段 3 实现" :image-size="120">
        <template #description>
          <p>eSIM Profile 管理</p>
          <p style="font-size: 12px; color: var(--el-text-color-secondary)">
            列出 eSIM 卡 → 查看 Profile → 启用/禁用/重命名
          </p>
        </template>
      </el-empty>
    </el-card>
  </div>
</template>
