import { ref, computed } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { referenceApi } from '@/api'
import type { ReferenceChapter } from '@/api'

export function useReferenceChapters() {
  const showChaptersDialog = ref(false)
  const chapterRefId = ref('')
  const chapterRefTitle = ref('')
  const chapters = ref<ReferenceChapter[]>([])
  const chaptersLoading = ref(false)
  const chapterSearch = ref('')
  const chapterSearchPage = ref(1)
  const CHAPTER_PAGE_SIZE = 10
  const selectedChapterIds = ref<string[]>([])
  const chapterSelectAll = ref(false)
  const deletingChapters = ref(false)
  const chapterTableRef = ref<any>(null)
  const deletedCount = ref(0)

  const filteredChapters = computed(() => {
    const q = chapterSearch.value.trim().toLowerCase()
    if (!q) return chapters.value
    return chapters.value.filter(c => c.title.toLowerCase().includes(q))
  })

  const pagedChapters = computed(() => {
    const start = (chapterSearchPage.value - 1) * CHAPTER_PAGE_SIZE
    return filteredChapters.value.slice(start, start + CHAPTER_PAGE_SIZE)
  })

  const isChapterIndeterminate = computed(() => {
    const pageIds = pagedChapters.value.map(c => c.id)
    const selected = selectedChapterIds.value.filter(id => pageIds.includes(id))
    return selected.length > 0 && selected.length < pageIds.length
  })

  async function openChaptersDialog(row: any) {
    chapterRefId.value = row.id
    chapterRefTitle.value = row.title || row.id
    chapterSearch.value = ''
    chapterSearchPage.value = 1
    selectedChapterIds.value = []
    deletedCount.value = 0
    showChaptersDialog.value = true
    await loadChapters()
  }

  async function loadChapters() {
    chaptersLoading.value = true
    try {
      const res = await referenceApi.listChapters(chapterRefId.value)
      chapters.value = (res.data as any).data || []
    } catch (e: any) {
      ElMessage.error('加载章节失败：' + (e?.response?.data?.error || e?.message))
    } finally {
      chaptersLoading.value = false
    }
  }

  function handleChapterSelectionChange(rows: ReferenceChapter[]) {
    selectedChapterIds.value = rows.map(r => r.id)
    const pageIds = pagedChapters.value.map(c => c.id)
    chapterSelectAll.value = pageIds.every(id => selectedChapterIds.value.includes(id))
  }

  function toggleSelectAllChapters(val: boolean) {
    if (!chapterTableRef.value) return
    if (val) {
      pagedChapters.value.forEach(row => chapterTableRef.value.toggleRowSelection(row, true))
    } else {
      pagedChapters.value.forEach(row => chapterTableRef.value.toggleRowSelection(row, false))
    }
  }

  async function deleteSingleChapter(id: string) {
    try {
      await referenceApi.deleteChapter(id)
      chapters.value = chapters.value.filter(c => c.id !== id)
      deletedCount.value++
      ElMessage.success('章节已删除')
    } catch {
      ElMessage.error('删除失败')
    }
  }

  async function batchDeleteChapters() {
    if (selectedChapterIds.value.length === 0) return
    try {
      await ElMessageBox.confirm(
        `确定删除选中的 ${selectedChapterIds.value.length} 章吗？`,
        '批量删除',
        { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' },
      )
    } catch { return }
    deletingChapters.value = true
    try {
      await referenceApi.batchDeleteChapters(chapterRefId.value, selectedChapterIds.value)
      const deletedSet = new Set(selectedChapterIds.value)
      deletedCount.value += deletedSet.size
      chapters.value = chapters.value.filter(c => !deletedSet.has(c.id))
      selectedChapterIds.value = []
      ElMessage.success('批量删除成功')
    } catch {
      ElMessage.error('批量删除失败')
    } finally {
      deletingChapters.value = false
    }
  }

  return {
    showChaptersDialog,
    chapterRefId,
    chapterRefTitle,
    chapters,
    chaptersLoading,
    chapterSearch,
    chapterSearchPage,
    CHAPTER_PAGE_SIZE,
    selectedChapterIds,
    chapterSelectAll,
    deletingChapters,
    chapterTableRef,
    deletedCount,
    filteredChapters,
    pagedChapters,
    isChapterIndeterminate,
    openChaptersDialog,
    loadChapters,
    handleChapterSelectionChange,
    toggleSelectAllChapters,
    deleteSingleChapter,
    batchDeleteChapters,
  }
}
