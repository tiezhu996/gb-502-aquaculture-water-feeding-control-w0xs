<script setup lang="ts">
import { computed } from 'vue'
import { WarningFilled } from '@element-plus/icons-vue'
import { useAuth } from '@/hooks/useAuth'
import RestrictionTag from './RestrictionTag.vue'
import type { FeedingRestriction } from '@/types/models'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{ restriction: FeedingRestriction }>()
const emit = defineEmits<{ manage: [restriction: FeedingRestriction] }>()

const { canOperate, canReview } = useAuth()
const actionable = computed(
  () => (props.restriction.status === 'active' && canOperate()) || (props.restriction.status === 'disposed' && canReview()),
)
const actionText = computed(() => (props.restriction.status === 'disposed' ? '复核解除' : '提交处置'))
const effectText = computed(() =>
  props.restriction.status === 'disposed'
    ? '处置已提交，在主管复核解除前继续阻断计划批准与新建投喂执行'
    : '正在阻断计划批准与新建投喂执行',
)
</script>

<template>
  <el-alert class="restriction-alert" type="error" :closable="false" show-icon :icon="WarningFilled">
    <template #title>
      <div class="restriction-alert-body">
        <div class="restriction-alert-main">
          <RestrictionTag :status="restriction.status" />
          <span class="restriction-text">
            <strong>停喂限制</strong>：{{ restriction.triggerReason }}
          </span>
        </div>
        <div class="restriction-alert-meta">
          <span>触发 {{ formatDateTime(restriction.triggerMeasuredAt) }}</span>
          <span>责任人：{{ restriction.triggeredBy }}</span>
          <span>{{ effectText }}</span>
        </div>
      </div>
    </template>
    <template #default>
      <div class="restriction-alert-actions">
        <span v-if="restriction.status === 'disposed'" class="disposition-preview">
          处置（{{ restriction.disposedBy }}）：{{ restriction.dispositionNote }}
        </span>
        <el-button v-if="actionable" size="small" type="danger" plain @click="emit('manage', restriction)">
          {{ actionText }}
        </el-button>
      </div>
    </template>
  </el-alert>
</template>

<style scoped>
.restriction-alert { margin-bottom: 14px; align-items: flex-start; }
.restriction-alert-body { display: flex; flex-direction: column; gap: 4px; }
.restriction-alert-main { display: flex; align-items: center; gap: 8px; }
.restriction-text { font-size: 13px; }
.restriction-alert-meta { display: flex; flex-wrap: wrap; gap: 4px 16px; color: #8a5558; font-size: 12px; }
.restriction-alert-actions { display: flex; align-items: center; gap: 12px; margin-top: 6px; }
.disposition-preview { color: #8a5558; font-size: 12px; }
</style>
