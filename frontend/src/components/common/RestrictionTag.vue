<script setup lang="ts">
import { computed } from 'vue'
import type { RestrictionStatus } from '@/types/enums'
import { restrictionStatusLabels } from '@/types/enums'

const props = defineProps<{ status: RestrictionStatus; size?: 'small' | 'default' }>()
const type = computed(() => (props.status === 'active' ? 'danger' : props.status === 'disposed' ? 'warning' : 'success'))
</script>

<template>
  <el-tag :type="type" effect="dark" :size="size || 'small'">
    <span class="risk-dot" />{{ restrictionStatusLabels[status] }}
  </el-tag>
</template>
