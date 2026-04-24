<script setup lang="ts">
// TODO 阶段 3：完整短信会话界面
</script>

<template>
  <div class="page-container">
    <h2>短信</h2>
    <p style="color: var(--el-text-color-secondary); margin-top: 12px">
      会话视图 + 发送短信
    </p>

    <el-divider />

    <el-row :gutter="16">
      <!-- 左侧：会话列表 -->
      <el-col :xs="24" :sm="10" :md="8">
        <el-card shadow="never">
          <template #header>会话列表</template>
          <el-empty description="TODO 阶段 3 实现" :image-size="80" />
        </el-card>
      </el-col>

      <!-- 右侧：消息详情 -->
      <el-col :xs="24" :sm="14" :md="16">
        <el-card shadow="never" style="min-height: 400px">
          <template #header>消息</template>
          <el-empty description="选择一个会话查看消息" :image-size="80" />
        </el-card>
      </el-col>
    </el-row>
  </div>
</template>
