import { client, type ApiEnvelope } from './client'
import type { FeedingRestriction, PageQuery, PageResult } from '@/types/models'

export const restrictionApi = {
  async list(params: PageQuery = {}) {
    const response = await client.get<ApiEnvelope<PageResult<FeedingRestriction>>>('/restrictions', { params })
    return response.data.data
  },
  async get(id: number) {
    const response = await client.get<ApiEnvelope<FeedingRestriction>>(`/restrictions/${id}`)
    return response.data.data
  },
  // 返回池塘当前生效中的停喂限制；无限制时为 null。
  async activeForPond(pondId: number) {
    const response = await client.get<ApiEnvelope<FeedingRestriction | null>>(`/ponds/${pondId}/restriction`)
    return response.data.data
  },
  async dispose(id: number, dispositionNote: string) {
    const response = await client.patch<ApiEnvelope<FeedingRestriction>>(`/restrictions/${id}/dispose`, { dispositionNote })
    return response.data.data
  },
  async release(id: number, releaseBasis: string) {
    const response = await client.patch<ApiEnvelope<FeedingRestriction>>(`/restrictions/${id}/release`, { releaseBasis })
    return response.data.data
  },
}
