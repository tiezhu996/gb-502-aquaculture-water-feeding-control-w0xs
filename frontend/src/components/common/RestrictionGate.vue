<script setup lang="ts">
import { computed, ref } from 'vue'
import { ElMessage } from 'element-plus'
import { CircleClose, Lock, Unlock } from '@element-plus/icons-vue'
import { restrictionApi } from '@/api/restrictions'
import { useAuth } from '@/hooks/useAuth'
import type { FeedingRestriction } from '@/types/models'
import { restrictionStatusLabels } from '@/types/enums'
import { formatDateTime } from '@/utils/format'
import { errorMessage } from '@/utils/errors'

const props = withDefaults(defineProps<{
  restriction: FeedingRestriction
  variant?: 'tag' | 'banner'
}>(), {
  variant: 'tag',
})

const emit = defineEmits<{ changed: [restriction: FeedingRestriction] }>()

const { canOperate, canReview } = useAuth()
const detailOpen = ref(false)
const saving = ref(false)
const actionMode = ref<'' | 'handle' | 'release'>('')
const handleNote = ref('')
const releaseNote = ref('')

const isActive = computed(() => props.restriction.status === 'active')
const isHandled = computed(() => props.restriction.status === 'handled')
const pondName = computed(() => props.restriction.pond?.name || `#${props.restriction.pondId}`)
// 责任人：处置前为池塘负责人，处置后为处置操作员。
const owner = computed(() => {
  if (isHandled.value) return props.restriction.handledBy || '—'
  return props.restriction.pond?.manager || '未指定'
})

function openDetail() {
  detailOpen.value = true
  actionMode.value = ''
  handleNote.value = ''
  releaseNote.value = ''
}

async function submitHandle() {
  if (handleNote.value.trim().length < 2) {
    ElMessage.warning('请填写至少 2 个字的处置说明')
    return
  }
  saving.value = true
  try {
    const updated = await restrictionApi.handle(props.restriction.id, handleNote.value)
    ElMessage.success('处置说明已提交，停喂限制等待主管复核解除')
    actionMode.value = ''
    emit('changed', updated)
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    saving.value = false
  }
}

async function submitRelease() {
  if (releaseNote.value.trim().length < 2) {
    ElMessage.warning('请填写复核依据')
    return
  }
  saving.value = true
  try {
    const updated = await restrictionApi.release(props.restriction.id, releaseNote.value)
    ElMessage.success('停喂限制已解除，投喂流程恢复')
    detailOpen.value = false
    emit('changed', updated)
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <span>
    <el-tooltip v-if="variant === 'tag'" :content="`停喂闸门：${restrictionStatusLabels[restriction.status]}（点击查看）`" placement="top">
      <el-button class="restriction-chip" :type="isActive ? 'danger' : 'warning'" size="small" round @click="openDetail">
        <el-icon><Lock /></el-icon>{{ restrictionStatusLabels[restriction.status] }}
      </el-button>
    </el-tooltip>
    <el-alert
      v-else
      :title="`停喂安全闸门 · ${restrictionStatusLabels[restriction.status]}`"
      :type="isActive ? 'error' : 'warning'"
      :closable="false"
      show-icon
      class="restriction-banner"
    >
      <template #default>
        <div class="banner-body">
          <p class="banner-reason">{{ restriction.triggerReason }}</p>
          <p class="banner-meta">触发读数：{{ formatDateTime(restriction.triggerReading?.measuredAt) }} · 责任人：{{ owner }}</p>
          <el-button link type="primary" size="small" @click="openDetail">查看详情与处置</el-button>
        </div>
      </template>
    </el-alert>

    <el-dialog v-model="detailOpen" title="停喂安全闸门" width="560px">
      <el-descriptions :column="1" border size="small">
        <el-descriptions-item label="养殖池">{{ pondName }}</el-descriptions-item>
        <el-descriptions-item label="闸门状态">
          <el-tag :type="isActive ? 'danger' : 'warning'" effect="dark" round>{{ restrictionStatusLabels[restriction.status] }}</el-tag>
        </el-descriptions-item>
        <el-descriptions-item label="异常原因">{{ restriction.triggerReason }}</el-descriptions-item>
        <el-descriptions-item label="触发读数时间">{{ formatDateTime(restriction.triggerReading?.measuredAt) }}</el-descriptions-item>
        <el-descriptions-item label="责任人">{{ owner }}</el-descriptions-item>
        <el-descriptions-item v-if="isHandled || restriction.status === 'released'" label="处置说明">
          {{ restriction.handleNote }}（{{ restriction.handledBy }} · {{ formatDateTime(restriction.handledAt) }}）
        </el-descriptions-item>
        <el-descriptions-item v-if="restriction.status === 'released'" label="解除复核依据">
          {{ restriction.releaseNote }}（{{ restriction.releasedBy }} · {{ formatDateTime(restriction.releasedAt) }}）
        </el-descriptions-item>
      </el-descriptions>

      <el-alert
        v-if="isActive"
        title="闸门关闭期间：投喂计划不能批准、不能新建投喂执行。操作员处置后须由主管复核解除。"
        type="error" :closable="false" show-icon class="gate-hint"
      />
      <el-alert
        v-else-if="isHandled"
        title="仅当异常处置后重新采集的最新水质读数恢复正常，主管填写复核依据后才能解除。"
        type="warning" :closable="false" show-icon class="gate-hint"
      />

      <div v-if="actionMode === 'handle'" class="gate-action">
        <el-input v-model="handleNote" type="textarea" :rows="4" placeholder="记录现场复核、应急增氧/换水等处置措施" />
        <div class="gate-actions">
          <el-button @click="actionMode = ''">取消</el-button>
          <el-button type="primary" :loading="saving" @click="submitHandle">提交处置说明</el-button>
        </div>
      </div>
      <div v-else-if="actionMode === 'release'" class="gate-action">
        <el-input v-model="releaseNote" type="textarea" :rows="4" placeholder="填写复核依据：正常读数时间、指标与现场确认结论" />
        <div class="gate-actions">
          <el-button @click="actionMode = ''">取消</el-button>
          <el-button type="success" :icon="Unlock" :loading="saving" @click="submitRelease">确认解除</el-button>
        </div>
      </div>
      <div v-else class="gate-actions">
        <el-button v-if="isActive && canOperate()" type="primary" :icon="CircleClose" @click="actionMode = 'handle'">提交处置说明</el-button>
        <el-button v-if="isHandled && canReview()" type="success" :icon="Unlock" @click="actionMode = 'release'">解除停喂</el-button>
      </div>
    </el-dialog>
  </span>
</template>

<style scoped>
.restriction-chip {
  font-weight: 600;
}

.restriction-banner {
  margin-bottom: 12px;
}

.banner-body {
  display: flex;
  flex-direction: column;
  gap: 2px;
}

.banner-reason {
  margin: 0;
  font-weight: 600;
}

.banner-meta {
  margin: 0;
  font-size: 12px;
  opacity: 0.85;
}

.gate-hint {
  margin-top: 12px;
}

.gate-action {
  margin-top: 12px;
}

.gate-actions {
  margin-top: 12px;
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
