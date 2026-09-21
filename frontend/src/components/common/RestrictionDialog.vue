<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import { restrictionApi } from '@/api/restrictions'
import { useAuth } from '@/hooks/useAuth'
import RestrictionTag from './RestrictionTag.vue'
import type { FeedingRestriction } from '@/types/models'
import { errorMessage } from '@/utils/errors'
import { formatDateTime } from '@/utils/format'

const props = defineProps<{ modelValue: boolean; restriction: FeedingRestriction | null }>()
const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  saved: [restriction: FeedingRestriction]
}>()

const { canOperate, canReview } = useAuth()
const note = ref('')
const saving = ref(false)

const visible = computed({
  get: () => props.modelValue,
  set: (value) => emit('update:modelValue', value),
})

watch(
  () => props.modelValue,
  (open) => {
    if (open) note.value = ''
  },
)

const canDispose = computed(() => props.restriction?.status === 'active' && canOperate())
const canRelease = computed(() => props.restriction?.status === 'disposed' && canReview())

async function submitDispose() {
  if (!props.restriction || note.value.trim().length < 5) {
    ElMessage.warning('请填写至少 5 个字的处置说明')
    return
  }
  saving.value = true
  try {
    const result = await restrictionApi.dispose(props.restriction.id, note.value)
    ElMessage.success('处置说明已提交，等待主管依据后续正常读数复核解除')
    emit('saved', result)
    visible.value = false
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    saving.value = false
  }
}

async function submitRelease() {
  if (!props.restriction || note.value.trim().length < 5) {
    ElMessage.warning('请填写至少 5 个字的复核依据')
    return
  }
  saving.value = true
  try {
    const result = await restrictionApi.release(props.restriction.id, note.value)
    ElMessage.success('停喂限制已解除，投喂审批与执行恢复')
    emit('saved', result)
    visible.value = false
  } catch (error) {
    ElMessage.error(errorMessage(error))
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <el-dialog v-model="visible" :title="canRelease ? '主管复核并解除停喂' : '提交水质异常处置说明'" width="560px">
    <template v-if="restriction">
      <div class="restriction-summary">
        <RestrictionTag :status="restriction.status" />
        <div class="restriction-meta">
          <p><strong>{{ restriction.pond?.name || `池塘 #${restriction.pondId}` }}</strong></p>
          <p>触发原因：{{ restriction.triggerReason }}</p>
          <p>触发时间：{{ formatDateTime(restriction.triggerMeasuredAt) }} · 责任人：{{ restriction.triggeredBy }}</p>
        </div>
      </div>
      <el-alert
        v-if="canRelease"
        title="仅在触发异常之后已录入正常水质读数时，复核解除才会通过；请在下方填写复测数据与现场核实情况。"
        type="warning"
        :closable="false"
        show-icon
      />
      <el-alert
        v-else
        title="提交后限制进入「待复核解除」，在主管解除前仍会阻断计划批准和新建投喂执行。"
        type="info"
        :closable="false"
        show-icon
      />
      <el-form-item class="dialog-field" :label="canRelease ? '复核依据' : '处置说明'">
        <el-input v-model="note" type="textarea" :rows="4" :placeholder="canRelease ? '例如：复测溶解氧 7.1、氨氮 0.06，现场摄食恢复' : '例如：已开启增氧机、换水 30%，持续监测氨氮'" />
      </el-form-item>
      <div v-if="restriction.dispositionNote && restriction.status === 'disposed'" class="prior-note">
        <small>操作员处置（{{ restriction.disposedBy }} · {{ formatDateTime(restriction.disposedAt || '') }}）</small>
        <p>{{ restriction.dispositionNote }}</p>
      </div>
    </template>
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button v-if="canDispose" type="primary" :loading="saving" @click="submitDispose">提交处置</el-button>
      <el-button v-else-if="canRelease" type="success" :loading="saving" @click="submitRelease">复核解除</el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.restriction-summary {
  display: flex;
  gap: 12px;
  margin-bottom: 16px;
  padding: 13px;
  background: #fdf3f4;
  border-left: 3px solid var(--red);
}
.restriction-meta p { margin: 2px 0; color: #5f4647; font-size: 13px; line-height: 1.6; }
.prior-note { margin-top: 10px; padding: 10px 12px; background: var(--canvas); border-radius: 6px; }
.prior-note small { color: var(--muted); }
.prior-note p { margin: 4px 0 0; font-size: 13px; line-height: 1.6; }
</style>
