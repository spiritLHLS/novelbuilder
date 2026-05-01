import { ref, computed } from 'vue'
import { ElMessage } from 'element-plus'
import { referenceApi, streamSearchNovels } from '@/api'
import type { NovelSearchResult, FetchBookInfo, FetchChapterInfo, NovelSiteCatalog, ResolvedNovelURL } from '@/api'
import { useDownloadStore } from '@/stores/download'

export function useReferenceFetch(projectId: string, onImportDone: () => Promise<void>) {
  const downloadStore = useDownloadStore()

  const showFetchDialog = ref(false)
  const fetchStep = ref<'search' | 'results' | 'chapters' | 'importing'>('search')

  const PAGE_SIZE = 10
  const searchKeyword = ref('')
  const searchLoading = ref(false)
  const searchResults = ref<NovelSearchResult[]>([])
  const searchPage = ref(0)
  const searchStreamStatus = ref('')
  const siteCatalogLoading = ref(false)
  const siteCatalog = ref<NovelSiteCatalog | null>(null)
  const selectedSearchSites = ref<string[]>([])
  const directBookURL = ref('')
  const urlResolving = ref(false)
  const resolvedSourceMeta = ref<ResolvedNovelURL | null>(null)
  let _searchAbort: AbortController | null = null

  const totalPages = computed(() => Math.ceil(searchResults.value.length / PAGE_SIZE))
  const pagedResults = computed(() =>
    searchResults.value.slice(searchPage.value * PAGE_SIZE, (searchPage.value + 1) * PAGE_SIZE)
  )

  const selectedBook = ref<NovelSearchResult | null>(null)
  const bookInfo = ref<FetchBookInfo | null>(null)
  const bookInfoLoading = ref(false)
  const fetchGenre = ref('')
  const selectedChapterRange = ref<[number, number]>([0, 0])

  const importingBookTitle = ref('')
  const importStartedTotal = ref(0)

  const fetchDialogTitle = computed(() => {
    if (fetchStep.value === 'search') return '搜索参考书'
    if (fetchStep.value === 'results') return `搜索结果：${searchKeyword.value}`
    if (fetchStep.value === 'chapters') return bookInfo.value?.title ?? '选择章节'
    return '下载已启动'
  })

  const flatChapters = computed((): FetchChapterInfo[] => {
    if (!bookInfo.value) return []
    return bookInfo.value.volumes.flatMap(v => v.chapters)
  })

  const chapterRangeMarks = computed(() => {
    const total = flatChapters.value.length
    if (total === 0) return {}
    return { 0: '1', [total - 1]: String(total) }
  })

  function openFetchDialog() {
    if (_searchAbort) { _searchAbort.abort(); _searchAbort = null }
    fetchStep.value = 'search'
    searchKeyword.value = ''
    searchLoading.value = false
    searchResults.value = []
    searchStreamStatus.value = ''
    bookInfo.value = null
    selectedBook.value = null
    fetchGenre.value = ''
    directBookURL.value = ''
    resolvedSourceMeta.value = null
    showFetchDialog.value = true
    void ensureNovelSiteCatalog(true)
  }

  function handleFetchDialogClose(done: () => void) {
    if (_searchAbort) {
      _searchAbort.abort()
      _searchAbort = null
    }
    done()
  }

  function resetSearchSiteSelection() {
    selectedSearchSites.value = [...(siteCatalog.value?.sites ?? [])]
  }

  async function ensureNovelSiteCatalog(resetSelection = false) {
    if (siteCatalogLoading.value) return
    if (siteCatalog.value && !resetSelection) return
    siteCatalogLoading.value = true
    try {
      const res = await referenceApi.listNovelSites(projectId)
      const data = res.data as NovelSiteCatalog
      siteCatalog.value = data
      const available = data.sites ?? []
      if (resetSelection || selectedSearchSites.value.length === 0) {
        selectedSearchSites.value = [...available]
      } else {
        const allowed = new Set(available)
        selectedSearchSites.value = selectedSearchSites.value.filter(site => allowed.has(site))
        if (selectedSearchSites.value.length === 0) {
          selectedSearchSites.value = [...available]
        }
      }
    } catch (e: any) {
      siteCatalog.value = null
      selectedSearchSites.value = []
      ElMessage.warning(e?.response?.data?.error || '加载书源列表失败，将使用后端默认站点集合')
    } finally {
      siteCatalogLoading.value = false
    }
  }

  function selectedSearchSitesPayload(): string[] | null {
    const available = siteCatalog.value?.sites ?? []
    if (available.length === 0) return null
    if (selectedSearchSites.value.length === available.length) return null
    return [...selectedSearchSites.value]
  }

  async function doSearch() {
    const kw = searchKeyword.value.trim()
    if (!kw) return
    if (siteCatalog.value && selectedSearchSites.value.length === 0) {
      ElMessage.warning('请至少选择一个搜索站点')
      return
    }
    if (_searchAbort) _searchAbort.abort()
    _searchAbort = new AbortController()
    const signal = _searchAbort.signal
    const sites = selectedSearchSitesPayload()
    searchLoading.value = true
    searchResults.value = []
    searchPage.value = 0
    searchStreamStatus.value = '正在连接各站点…'
    fetchStep.value = 'results'
    let siteCount = 0
    try {
      for await (const event of streamSearchNovels(projectId, kw, { signal, sites })) {
        if (event.type === 'batch') {
          searchResults.value.push(...event.results)
          siteCount++
          searchStreamStatus.value = `已从 ${siteCount} 个站点获取 ${searchResults.value.length} 条结果…`
        } else if (event.type === 'done') {
          searchStreamStatus.value = `搜索完成，共 ${siteCount} 个站点，${event.total} 条结果`
        } else if (event.type === 'error') {
          ElMessage.error(`搜索出错：${event.message}`)
        }
      }
    } catch (e: any) {
      if (signal.aborted) return
      try {
        const res = await referenceApi.searchNovels(projectId, kw, sites)
        searchResults.value = (res.data as any).results ?? []
        searchStreamStatus.value = `共 ${searchResults.value.length} 条结果`
      } catch (e2: any) {
        ElMessage.error(e2?.response?.data?.error || '搜索失败，请稍后重试')
      }
    } finally {
      searchLoading.value = false
    }
  }

  async function selectBook(book: NovelSearchResult) {
    resolvedSourceMeta.value = null
    selectedBook.value = book
    bookInfo.value = null
    bookInfoLoading.value = true
    fetchStep.value = 'chapters'
    try {
      const res = await referenceApi.getBookInfo(projectId, book.site, book.book_id)
      bookInfo.value = res.data as FetchBookInfo
      const total = bookInfo.value.total_chapters
      selectedChapterRange.value = [0, Math.max(total - 1, 0)]
    } catch (e: any) {
      ElMessage.error(e?.response?.data?.error || '获取章节列表失败')
    } finally {
      bookInfoLoading.value = false
    }
  }

  async function resolveBookURLImport() {
    const rawURL = directBookURL.value.trim()
    if (!rawURL) return
    urlResolving.value = true
    try {
      const res = await referenceApi.resolveNovelURL(projectId, rawURL)
      const resolved = res.data as ResolvedNovelURL
      resolvedSourceMeta.value = resolved
      await selectBook({
        site: resolved.site,
        book_id: resolved.book_id,
        book_url: resolved.url,
        cover_url: '',
        title: resolved.source_name || 'URL 导入',
        author: '',
        latest_chapter: '',
        update_date: '',
        word_count: '',
      })
      resolvedSourceMeta.value = resolved
    } catch (e: any) {
      ElMessage.error(e?.response?.data?.error || '解析书籍 URL 失败')
    } finally {
      urlResolving.value = false
    }
  }

  async function startFetchImport() {
    if (!selectedBook.value || !bookInfo.value) return
    const flat = flatChapters.value
    const [startIdx, endIdx] = selectedChapterRange.value
    const chapterIds = flat.slice(startIdx, endIdx + 1).map(c => c.chapter_id)
    if (chapterIds.length === 0) { ElMessage.warning('请至少选择一章'); return }

    importingBookTitle.value = bookInfo.value.title
    importStartedTotal.value = chapterIds.length
    fetchStep.value = 'importing'

    try {
      const res = await referenceApi.startFetchImport(projectId, {
        site: selectedBook.value.site,
        book_id: selectedBook.value.book_id,
        title: bookInfo.value.title,
        author: bookInfo.value.author,
        genre: fetchGenre.value,
        chapter_ids: chapterIds,
      })
      const data: any = res.data
      downloadStore.addTask(
        data.ref_id,
        projectId,
        bookInfo.value.title,
        data.fetch_total ?? chapterIds.length,
      )
      await onImportDone()
    } catch (e: any) {
      ElMessage.error(e?.response?.data?.error || '启动下载失败')
      fetchStep.value = 'chapters'
    }
  }

  return {
    showFetchDialog,
    fetchStep,
    searchKeyword,
    searchLoading,
    searchResults,
    searchPage,
    searchStreamStatus,
    siteCatalogLoading,
    siteCatalog,
    selectedSearchSites,
    directBookURL,
    urlResolving,
    resolvedSourceMeta,
    totalPages,
    pagedResults,
    selectedBook,
    bookInfo,
    bookInfoLoading,
    fetchGenre,
    selectedChapterRange,
    importingBookTitle,
    importStartedTotal,
    fetchDialogTitle,
    flatChapters,
    chapterRangeMarks,
    openFetchDialog,
    handleFetchDialogClose,
    resetSearchSiteSelection,
    ensureNovelSiteCatalog,
    doSearch,
    selectBook,
    resolveBookURLImport,
    startFetchImport,
  }
}
