<script setup lang="ts">
// TODO 阶段 3：USSD 多轮对话界面
</script>

<template>
  <div class="page-container">
    <h2>USSD</h2>
    <p style="color: var(--el-text-color-secondary); margin-top: 12px">
      发起 USSD 请求并进行多轮对话
    </p>

    <el-divider />

    <el-card shadow="never">
      <el-empty description="TODO 阶段 3 实现" :image-size="120">
        <template #description>
          <p>USSD 多轮对话界面</p>
          <p style="font-size: 12px; color: var(--el-text-color-secondary)">
            支持选择模块 → 输入 USSD 码 → 查看/回复响应
          </p>
        </template>
      </el-empty>
    </el-card>
  </div>
</template>
