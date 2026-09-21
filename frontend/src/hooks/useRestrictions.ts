import { computed, ref } from 'vue'
import { restrictionApi } from '@/api/restrictions'
import type { FeedingRestriction } from '@/types/models'
import type { RestrictionStatus } from '@/types/enums'
import { errorMessage } from '@/utils/errors'
import { ElMessage } from 'element-plus'

// 停喂安全闸门共享状态：加载全部限制后在内存过滤未解除记录。
// 数据库保证同一养殖池至多一条未解除限制，因此 openByPond 可直接按 pondId 索引。
export function useRestrictions() {
  const restrictions = ref<FeedingRestriction[]>([])
  const loading = ref(false)

  const isOpen = (item: FeedingRestriction) => item.status === 'active' || item.status === 'handled'
  const openRestrictions = computed(() => restrictions.value.filter(isOpen))
  const openByPond = computed(() => {
    const map = new Map<number, FeedingRestriction>()
    for (const item of openRestrictions.value) {
      if (!map.has(item.pondId)) map.set(item.pondId, item)
    }
    return map
  })

  async function load(status?: RestrictionStatus) {
    loading.value = true
    try {
      const result = await restrictionApi.list({ page: 1, pageSize: 200, status })
      restrictions.value = result.items
    } catch (error) {
      ElMessage.error(errorMessage(error))
    } finally {
      loading.value = false
    }
  }

  return { restrictions, openRestrictions, openByPond, loading, load }
}
